package serverplugin

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/tetratelabs/wazero"
	"gorm.io/gorm"

	"github.com/helantianshen/oss-sync/internal/models"
)

// PluginInfo is the safe management view of an installed server plugin.
type PluginInfo struct {
	Manifest
	Enabled            bool
	LastError          string
	WasmHash           string
	ManifestHash       string
	WasmSize           int64
	PayloadHash        string
	PayloadSize        int64
	InstalledAt        time.Time
	UpdatedAt          time.Time
	Associations       []PluginAssociation
	AssociationSummary string
	Builtin            bool
}

type PluginAssociation struct {
	Kind       string
	TargetID   string
	TargetName string
}

// Manager owns installed plugin files and compiled enabled modules.
type Manager struct {
	db      *gorm.DB
	root    string
	runtime *Runtime

	mu            sync.RWMutex
	lifecycleMu   sync.Mutex
	modules       map[string]pluginInstance
	registrations map[string]ExtensionRegistration
	scheduler     pluginTaskScheduler
}

type pluginInstance interface {
	Invoke(context.Context, PluginRequest) (PluginResponse, error)
	Close(context.Context) error
	Healthy() bool
	Registration() ExtensionRegistration
}

type wasmPluginInstance struct {
	runtime      *Runtime
	module       wazero.CompiledModule
	registration ExtensionRegistration
}

func (p *wasmPluginInstance) Invoke(ctx context.Context, request PluginRequest) (PluginResponse, error) {
	return p.runtime.Invoke(ctx, p.module, request)
}

func (p *wasmPluginInstance) Close(ctx context.Context) error {
	return p.module.Close(ctx)
}

func (p *wasmPluginInstance) Healthy() bool {
	return true
}

func (p *wasmPluginInstance) Registration() ExtensionRegistration {
	return p.registration
}

func NewManager(ctx context.Context, db *gorm.DB, dataDir string) (*Manager, error) {
	if ctx == nil {
		return nil, errors.New("plugin manager context is nil")
	}
	if db == nil {
		return nil, errors.New("plugin manager database is nil")
	}
	root := filepath.Join(dataDir, "plugins")
	if err := os.MkdirAll(root, 0o750); err != nil {
		return nil, fmt.Errorf("create plugin root: %w", err)
	}
	runtime, err := NewRuntime(ctx)
	if err != nil {
		return nil, err
	}
	return &Manager{
		db: db, root: root, runtime: runtime,
		modules:       make(map[string]pluginInstance),
		registrations: make(map[string]ExtensionRegistration),
	}, nil
}

func (m *Manager) LoadEnabled(ctx context.Context) error {
	var records []models.ServerPlugin
	if err := m.db.Where("enabled = ?", true).Order("id asc").Find(&records).Error; err != nil {
		return fmt.Errorf("load enabled plugins: %w", err)
	}
	ordered, err := m.orderEnabledRecords(records)
	if err != nil {
		return err
	}
	var loadErrors []error
	for _, record := range ordered {
		if err := m.loadRecord(ctx, record); err != nil {
			loadErrors = append(loadErrors, fmt.Errorf("plugin %s: %w", record.ID, err))
			if saveErr := m.saveLastError(record.ID, err); saveErr != nil {
				loadErrors = append(loadErrors, fmt.Errorf("plugin %s: save load error: %w", record.ID, saveErr))
			}
		}
	}
	return errors.Join(loadErrors...)
}

func (m *Manager) orderEnabledRecords(records []models.ServerPlugin) ([]models.ServerPlugin, error) {
	byID := make(map[string]models.ServerPlugin, len(records))
	dependencies := make(map[string][]string, len(records))
	for _, record := range records {
		byID[record.ID] = record
		manifest, err := ParseManifest([]byte(record.ManifestJSON))
		if err != nil {
			return nil, fmt.Errorf("decode plugin %s manifest: %w", record.ID, err)
		}
		for _, dependency := range registrationFromManifest(manifest).Dependencies {
			dependencies[record.ID] = append(dependencies[record.ID], dependency.PluginID)
		}
	}
	state := make(map[string]uint8, len(records))
	ordered := make([]models.ServerPlugin, 0, len(records))
	var visit func(string) error
	visit = func(id string) error {
		switch state[id] {
		case 1:
			return fmt.Errorf("plugin dependency cycle includes %s", id)
		case 2:
			return nil
		}
		state[id] = 1
		for _, dependencyID := range dependencies[id] {
			if _, exists := byID[dependencyID]; !exists {
				continue
			}
			if err := visit(dependencyID); err != nil {
				return err
			}
		}
		state[id] = 2
		ordered = append(ordered, byID[id])
		return nil
	}
	for _, record := range records {
		if err := visit(record.ID); err != nil {
			return nil, err
		}
	}
	return ordered, nil
}

func (m *Manager) List() ([]PluginInfo, error) {
	var records []models.ServerPlugin
	if err := m.db.Order("id asc").Find(&records).Error; err != nil {
		return nil, fmt.Errorf("list server plugins: %w", err)
	}
	plugins := make([]PluginInfo, 0, len(records))
	for _, record := range records {
		info, err := infoFromRecord(record)
		if err != nil {
			return nil, fmt.Errorf("decode plugin %s: %w", record.ID, err)
		}
		info.Associations = pluginAssociations(m.db, record.ID)
		labels := make([]string, 0, len(info.Associations))
		for _, association := range info.Associations {
			labels = append(labels, association.TargetName)
		}
		info.AssociationSummary = strings.Join(labels, ", ")
		m.mu.RLock()
		registration, registered := m.registrations[record.ID]
		m.mu.RUnlock()
		if registered {
			info.Settings = registration.Settings
			info.Hooks = legacyHookSpecs(registration.Hooks)
		}
		if info.Runtime == "" {
			info.Runtime = RuntimeWASM
		}
		info.Builtin = record.Builtin
		plugins = append(plugins, info)
	}
	return plugins, nil
}

func pluginAssociations(db *gorm.DB, pluginID string) []PluginAssociation {
	var rows []models.ServerPluginAssociation
	if err := db.Where("plugin_id = ?", pluginID).Order("kind asc, target_id asc").Find(&rows).Error; err != nil {
		return []PluginAssociation{}
	}
	associations := make([]PluginAssociation, 0, len(rows))
	for _, row := range rows {
		associations = append(associations, PluginAssociation{Kind: row.Kind, TargetID: row.TargetID, TargetName: row.TargetName})
	}
	return associations
}

func (m *Manager) Install(ctx context.Context, reader io.ReaderAt, size int64) (PluginInfo, error) {
	m.lifecycleMu.Lock()
	defer m.lifecycleMu.Unlock()
	packageData, err := ParsePackage(reader, size)
	if err != nil {
		return PluginInfo{}, err
	}
	if err := m.validatePackage(ctx, packageData); err != nil {
		return PluginInfo{}, err
	}

	var existing models.ServerPlugin
	if err := m.db.Where("id = ?", packageData.Manifest.ID).First(&existing).Error; err == nil {
		return PluginInfo{}, ErrPluginExists
	} else if !errors.Is(err, gorm.ErrRecordNotFound) {
		return PluginInfo{}, fmt.Errorf("check existing plugin: %w", err)
	}
	target, err := writePackage(m.root, packageData)
	if err != nil {
		return PluginInfo{}, err
	}
	if manifestRuntime(packageData.Manifest) == RuntimeExecutable {
		instance, startErr := m.openInstance(ctx, target, packageData)
		if startErr != nil {
			return PluginInfo{}, cleanupInstalledPackage(target, startErr)
		}
		if closeErr := instance.Close(ctx); closeErr != nil {
			return PluginInfo{}, cleanupInstalledPackage(target, fmt.Errorf("close validation plugin: %w", closeErr))
		}
	}
	manifestJSON, err := manifestJSON(packageData.Manifest)
	if err != nil {
		return PluginInfo{}, cleanupInstalledPackage(target, err)
	}
	record := models.ServerPlugin{
		ID:           packageData.Manifest.ID,
		Name:         packageData.Manifest.Name,
		Version:      packageData.Manifest.Version,
		Description:  packageData.Manifest.Description,
		APIVersion:   packageData.Manifest.APIVersion,
		Runtime:      manifestRuntime(packageData.Manifest),
		ManifestJSON: manifestJSON,
		ManifestHash: packageData.ManifestHash,
		WasmHash:     packageData.WasmHash,
		WasmSize:     int64(len(packageData.Wasm)),
		PayloadHash:  packageData.PayloadHash,
		PayloadSize:  packageData.PayloadSize,
		InstalledAt:  time.Now().UTC(),
	}
	if err := m.db.Create(&record).Error; err != nil {
		cleanupErr := os.RemoveAll(target)
		if errors.Is(err, gorm.ErrDuplicatedKey) {
			return PluginInfo{}, errors.Join(ErrPluginExists, cleanupError(cleanupErr))
		}
		return PluginInfo{}, errors.Join(fmt.Errorf("save plugin record: %w", err), cleanupError(cleanupErr))
	}
	return infoFromRecord(record)
}

// InstallOrReuse installs a package or reuses an identical installed package.
// This lets multiple themes depend on the same administrator-approved plugin.
func (m *Manager) InstallOrReuse(ctx context.Context, reader io.ReaderAt, size int64) (PluginInfo, error) {
	packageData, err := ParsePackage(reader, size)
	if err != nil {
		return PluginInfo{}, err
	}
	var existing models.ServerPlugin
	err = m.db.Where("id = ?", packageData.Manifest.ID).First(&existing).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return m.Install(ctx, reader, size)
	}
	if err != nil {
		return PluginInfo{}, fmt.Errorf("check existing plugin: %w", err)
	}
	if !samePackage(existing, packageData) {
		return PluginInfo{}, ErrPluginExists
	}
	return infoFromRecord(existing)
}

// Upgrade replaces an installed plugin package atomically and preserves its enabled state.
func (m *Manager) Upgrade(ctx context.Context, reader io.ReaderAt, size int64) (PluginInfo, error) {
	m.lifecycleMu.Lock()
	defer m.lifecycleMu.Unlock()
	return m.upgrade(ctx, reader, size)
}

func (m *Manager) upgrade(ctx context.Context, reader io.ReaderAt, size int64) (PluginInfo, error) {
	packageData, err := ParsePackage(reader, size)
	if err != nil {
		return PluginInfo{}, err
	}
	if err := m.validatePackage(ctx, packageData); err != nil {
		return PluginInfo{}, err
	}
	record, err := m.record(packageData.Manifest.ID)
	if err != nil {
		return PluginInfo{}, err
	}
	if record.Builtin {
		return PluginInfo{}, errors.New("built-in plugin cannot be upgraded")
	}
	if samePackage(record, packageData) {
		return infoFromRecord(record)
	}
	wasEnabled := record.Enabled
	if wasEnabled {
		if err := m.disable(ctx, record.ID); err != nil {
			return PluginInfo{}, err
		}
	}
	pluginDir := filepath.Join(m.root, record.ID)
	tombstone, err := stagePluginDeletion(m.root, pluginDir)
	if err != nil {
		return PluginInfo{}, err
	}
	target, err := writePackage(m.root, packageData)
	if err != nil {
		_ = os.Rename(tombstone, pluginDir)
		return PluginInfo{}, err
	}
	instance, err := m.openInstance(ctx, target, packageData)
	if err != nil {
		_ = os.RemoveAll(target)
		_ = os.Rename(tombstone, pluginDir)
		return PluginInfo{}, err
	}
	registration := instance.Registration()
	if err := validateRegistration(registration); err != nil {
		_ = instance.Close(ctx)
		_ = os.RemoveAll(target)
		_ = os.Rename(tombstone, pluginDir)
		return PluginInfo{}, err
	}
	if err := m.checkDependencies(registration.Dependencies); err != nil {
		_ = instance.Close(ctx)
		_ = os.RemoveAll(target)
		_ = os.Rename(tombstone, pluginDir)
		return PluginInfo{}, err
	}
	if err := m.applyMigrations(ctx, record.ID, registration.Migrations); err != nil {
		_ = instance.Close(ctx)
		_ = os.RemoveAll(target)
		_ = os.Rename(tombstone, pluginDir)
		return PluginInfo{}, err
	}
	if err := invokeInstanceLifecycle(ctx, instance, registration.Lifecycle.Upgrade, "upgrade"); err != nil {
		_ = instance.Close(ctx)
		_ = os.RemoveAll(target)
		_ = os.Rename(tombstone, pluginDir)
		return PluginInfo{}, err
	}
	if err := instance.Close(ctx); err != nil {
		_ = os.RemoveAll(target)
		_ = os.Rename(tombstone, pluginDir)
		return PluginInfo{}, err
	}
	manifestJSON, err := manifestJSON(packageData.Manifest)
	if err != nil {
		_ = os.RemoveAll(target)
		_ = os.Rename(tombstone, pluginDir)
		return PluginInfo{}, err
	}
	updates := map[string]any{
		"name": packageData.Manifest.Name, "version": packageData.Manifest.Version,
		"description": packageData.Manifest.Description, "api_version": packageData.Manifest.APIVersion,
		"runtime": manifestRuntime(packageData.Manifest), "manifest_json": manifestJSON,
		"manifest_hash": packageData.ManifestHash, "wasm_hash": packageData.WasmHash,
		"wasm_size": int64(len(packageData.Wasm)), "payload_hash": packageData.PayloadHash,
		"payload_size": packageData.PayloadSize, "last_error": "",
	}
	if err := m.db.Model(&models.ServerPlugin{}).Where("id = ?", record.ID).Updates(updates).Error; err != nil {
		_ = os.RemoveAll(target)
		_ = os.Rename(tombstone, pluginDir)
		return PluginInfo{}, err
	}
	if err := os.RemoveAll(tombstone); err != nil {
		return PluginInfo{}, err
	}
	if wasEnabled {
		if err := m.enable(ctx, record.ID); err != nil {
			return PluginInfo{}, err
		}
	}
	return m.info(record.ID)
}

func (m *Manager) info(id string) (PluginInfo, error) {
	record, err := m.record(id)
	if err != nil {
		return PluginInfo{}, err
	}
	return infoFromRecord(record)
}

func (m *Manager) validatePackage(ctx context.Context, packageData Package) error {
	if manifestRuntime(packageData.Manifest) != RuntimeWASM {
		return nil
	}
	compiled, err := m.runtime.Compile(ctx, packageData.Wasm)
	if err != nil {
		return err
	}
	if err := compiled.Close(ctx); err != nil {
		return fmt.Errorf("close validation module: %w", err)
	}
	return nil
}

func (m *Manager) openInstance(ctx context.Context, dir string, packageData Package) (pluginInstance, error) {
	switch manifestRuntime(packageData.Manifest) {
	case RuntimeWASM:
		compiled, err := m.runtime.Compile(ctx, packageData.Wasm)
		if err != nil {
			return nil, err
		}
		return &wasmPluginInstance{
			runtime: m.runtime, module: compiled,
			registration: registrationFromManifest(packageData.Manifest),
		}, nil
	case RuntimeExecutable:
		return startExecutablePlugin(
			ctx,
			dir,
			packageData.Manifest,
			func(callCtx context.Context, method string, params map[string]json.RawMessage) (any, error) {
				return m.hostCall(callCtx, packageData.Manifest.ID, method, params)
			},
		)
	default:
		return nil, fmt.Errorf("unsupported plugin runtime %q", packageData.Manifest.Runtime)
	}
}

func (m *Manager) invokeCallback(
	ctx context.Context,
	pluginID string,
	callback string,
	request PluginRequest,
) (PluginResponse, error) {
	m.mu.RLock()
	instance := m.modules[pluginID]
	m.mu.RUnlock()
	if instance == nil || !instance.Healthy() {
		return PluginResponse{}, fmt.Errorf("plugin %s is unavailable", pluginID)
	}
	request.Callback = callback
	return instance.Invoke(ctx, request)
}

func legacyHookSpecs(hooks []RegisteredHook) []HookSpec {
	result := make([]HookSpec, 0, len(hooks))
	for _, hook := range hooks {
		result = append(result, HookSpec{Name: hook.Name, ID: hook.ID, Label: hook.Label})
	}
	return result
}

func samePackage(record models.ServerPlugin, packageData Package) bool {
	recordRuntime := record.Runtime
	if recordRuntime == "" {
		recordRuntime = RuntimeWASM
	}
	if recordRuntime != manifestRuntime(packageData.Manifest) || record.ManifestHash != packageData.ManifestHash {
		return false
	}
	if record.PayloadHash != "" {
		return record.PayloadHash == packageData.PayloadHash && record.PayloadSize == packageData.PayloadSize
	}
	return record.WasmHash == packageData.WasmHash && record.WasmSize == int64(len(packageData.Wasm))
}
