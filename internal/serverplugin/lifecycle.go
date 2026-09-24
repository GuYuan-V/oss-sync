package serverplugin

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"gorm.io/gorm"

	"github.com/helantianshen/oss-sync/internal/models"
)

func (m *Manager) Enable(ctx context.Context, id string) error {
	m.lifecycleMu.Lock()
	defer m.lifecycleMu.Unlock()
	return m.enable(ctx, id)
}

func (m *Manager) enable(ctx context.Context, id string) (resultErr error) {
	record, err := m.record(id)
	if err != nil {
		return err
	}
	if record.Builtin {
		return errors.New("built-in plugin cannot be enabled or disabled")
	}
	m.mu.Lock()
	current := m.modules[id]
	if record.Enabled && current != nil && current.Healthy() {
		m.mu.Unlock()
		return nil
	}
	if current != nil {
		delete(m.modules, id)
		m.removeRegistration(id)
	}
	m.mu.Unlock()
	if current != nil {
		if err := current.Close(ctx); err != nil {
			return m.enableError(id, fmt.Errorf("close failed plugin before restart: %w", err))
		}
	}
	packageData, err := readInstalledPackage(m.root, id)
	if err != nil {
		return m.enableError(id, err)
	}
	if err := verifyInstalledPackage(packageData, record); err != nil {
		return m.enableError(id, err)
	}
	instance, err := m.openInstance(ctx, filepath.Join(m.root, id), packageData)
	if err != nil {
		return m.enableError(id, err)
	}
	defer func() {
		if resultErr == nil {
			return
		}
		m.mu.Lock()
		delete(m.modules, id)
		m.removeRegistration(id)
		scheduler := m.scheduler
		m.mu.Unlock()
		if scheduler != nil {
			scheduler.RemovePluginTasks(id)
		}
		closeCtx, cancel := context.WithTimeout(context.Background(), processShutdownTimeout)
		defer cancel()
		resultErr = errors.Join(resultErr, instance.Close(closeCtx),
			m.db.Model(&models.ServerPlugin{}).Where("id = ?", id).Update("enabled", false).Error)
	}()
	registration := instance.Registration()
	if err := validateRegistration(registration); err != nil {
		return m.enableError(id, err)
	}
	if err := m.checkDependencies(registration.Dependencies); err != nil {
		return m.enableError(id, err)
	}
	if err := m.syncThemeResources(id, packageData); err != nil {
		return m.enableError(id, err)
	}
	if err := m.db.Model(&models.ServerPlugin{}).Where("id = ?", id).
		Updates(map[string]any{"enabled": true, "last_error": ""}).Error; err != nil {
		return fmt.Errorf("enable plugin: %w", err)
	}
	m.mu.Lock()
	m.modules[id] = instance
	m.setRegistration(id, registration)
	m.mu.Unlock()
	if err := m.registerPluginTasks(id, registration); err != nil {
		return m.enableError(id, err)
	}
	if err := invokeInstanceLifecycle(ctx, instance, registration.Lifecycle.Activate, "activate"); err != nil {
		return m.enableError(id, err)
	}
	return nil
}

func (m *Manager) Disable(ctx context.Context, id string) error {
	m.lifecycleMu.Lock()
	defer m.lifecycleMu.Unlock()
	return m.disable(ctx, id)
}

func (m *Manager) disable(ctx context.Context, id string) error {
	record, err := m.record(id)
	if err != nil {
		return err
	}
	if record.Builtin {
		return errors.New("built-in plugin cannot be disabled")
	}
	m.mu.Lock()
	instance := m.modules[id]
	scheduler := m.scheduler
	delete(m.modules, id)
	m.removeRegistration(id)
	m.mu.Unlock()
	if scheduler != nil {
		scheduler.RemovePluginTasks(id)
	}
	var lifecycleErr error
	if instance != nil {
		lifecycleErr = invokeInstanceLifecycle(ctx, instance, instance.Registration().Lifecycle.Deactivate, "deactivate")
	}
	resourceErr := m.removeOwnedThemeResources(id)
	dbErr := m.db.Model(&models.ServerPlugin{}).Where("id = ?", id).
		Updates(map[string]any{"enabled": false, "last_error": ""}).Error
	var closeErr error
	if instance != nil {
		closeErr = instance.Close(ctx)
	}
	return errors.Join(lifecycleErr, resourceErr, dbErr, closeErr)
}

func (m *Manager) Delete(id string) error {
	m.lifecycleMu.Lock()
	defer m.lifecycleMu.Unlock()
	record, err := m.record(id)
	if err != nil {
		return err
	}
	if record.Builtin {
		return errors.New("built-in plugin cannot be deleted")
	}
	m.mu.RLock()
	loaded := m.modules[id] != nil
	m.mu.RUnlock()
	if record.Enabled || loaded {
		return ErrPluginEnabled
	}
	packageData, err := readInstalledPackage(m.root, id)
	if err != nil {
		return err
	}
	deleteCtx, cancel := context.WithTimeout(context.Background(), processShutdownTimeout)
	defer cancel()
	instance, err := m.openInstance(deleteCtx, filepath.Join(m.root, id), packageData)
	if err != nil {
		return fmt.Errorf("start plugin for uninstall: %w", err)
	}
	if err := invokeInstanceLifecycle(deleteCtx, instance, instance.Registration().Lifecycle.Uninstall, "uninstall"); err != nil {
		return errors.Join(fmt.Errorf("uninstall plugin %s: %w", id, err), instance.Close(deleteCtx))
	}
	if err := instance.Close(deleteCtx); err != nil {
		return fmt.Errorf("close plugin after uninstall: %w", err)
	}
	if err := m.removeOwnedThemeResources(id); err != nil {
		return err
	}
	pluginDir := filepath.Join(m.root, id)
	tombstone, err := stagePluginDeletion(m.root, pluginDir)
	if err != nil {
		return err
	}
	if err := m.db.Delete(&record).Error; err != nil {
		restoreErr := os.Rename(tombstone, pluginDir)
		return errors.Join(fmt.Errorf("delete plugin record: %w", err), restorePluginError(restoreErr))
	}
	if err := os.RemoveAll(tombstone); err != nil {
		return fmt.Errorf("delete plugin files: %w", err)
	}
	return nil
}

func (m *Manager) Close(ctx context.Context) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	var closeErrors []error
	for id, instance := range m.modules {
		if err := instance.Close(ctx); err != nil {
			closeErrors = append(closeErrors, fmt.Errorf("close plugin %s: %w", id, err))
		}
	}
	m.modules = make(map[string]pluginInstance)
	m.registrations = make(map[string]ExtensionRegistration)
	if err := m.runtime.Close(ctx); err != nil {
		closeErrors = append(closeErrors, err)
	}
	return errors.Join(closeErrors...)
}

func (m *Manager) loadRecord(ctx context.Context, record models.ServerPlugin) error {
	packageData, err := readInstalledPackage(m.root, record.ID)
	if err != nil {
		return err
	}
	if err := verifyInstalledPackage(packageData, record); err != nil {
		return err
	}
	instance, err := m.openInstance(ctx, filepath.Join(m.root, record.ID), packageData)
	if err != nil {
		return err
	}
	registration := instance.Registration()
	if err := validateRegistration(registration); err != nil {
		return errors.Join(err, instance.Close(ctx))
	}
	if err := m.checkDependencies(registration.Dependencies); err != nil {
		return errors.Join(err, instance.Close(ctx))
	}
	if err := m.applyMigrations(ctx, record.ID, registration.Migrations); err != nil {
		return errors.Join(err, instance.Close(ctx))
	}
	if err := m.syncThemeResources(record.ID, packageData); err != nil {
		return errors.Join(err, instance.Close(ctx))
	}
	m.mu.Lock()
	old := m.modules[record.ID]
	m.modules[record.ID] = instance
	m.setRegistration(record.ID, registration)
	m.mu.Unlock()
	if old != nil {
		return old.Close(ctx)
	}
	return nil
}

func verifyInstalledPackage(packageData Package, record models.ServerPlugin) error {
	if !samePackage(record, packageData) {
		return fmt.Errorf("%w: installed package hash or size changed", ErrInvalidPackage)
	}
	return nil
}

func (m *Manager) record(id string) (models.ServerPlugin, error) {
	if !pluginIDPattern.MatchString(id) {
		return models.ServerPlugin{}, ErrPluginNotFound
	}
	var record models.ServerPlugin
	if err := m.db.Where("id = ?", id).First(&record).Error; errors.Is(err, gorm.ErrRecordNotFound) {
		return models.ServerPlugin{}, ErrPluginNotFound
	} else if err != nil {
		return models.ServerPlugin{}, fmt.Errorf("load plugin %s: %w", id, err)
	}
	return record, nil
}

func (m *Manager) saveLastError(id string, pluginErr error) error {
	if err := m.db.Model(&models.ServerPlugin{}).Where("id = ?", id).Update("last_error", pluginErr.Error()).Error; err != nil {
		return fmt.Errorf("save plugin error: %w", err)
	}
	return nil
}

func (m *Manager) enableError(id string, pluginErr error) error {
	if saveErr := m.saveLastError(id, pluginErr); saveErr != nil {
		return errors.Join(pluginErr, saveErr)
	}
	return pluginErr
}

func stagePluginDeletion(root, pluginDir string) (string, error) {
	tombstone, err := os.MkdirTemp(root, ".delete-")
	if err != nil {
		return "", fmt.Errorf("create plugin deletion staging directory: %w", err)
	}
	if err := os.Remove(tombstone); err != nil {
		return "", errors.Join(
			fmt.Errorf("prepare plugin deletion staging directory: %w", err),
			os.RemoveAll(tombstone),
		)
	}
	if err := os.Rename(pluginDir, tombstone); err != nil {
		return "", errors.Join(fmt.Errorf("stage plugin files for deletion: %w", err), os.RemoveAll(tombstone))
	}
	return tombstone, nil
}

func restorePluginError(err error) error {
	if err == nil {
		return nil
	}
	return fmt.Errorf("restore plugin files after failed delete: %w", err)
}

// applyMigrations 将待定迁移批次放在单个事务内执行，同时兼容 SQLite 与 PostgreSQL
// 后续生命周期钩子可能产生外部副作用，已提交的迁移必须保持向后兼容
func (m *Manager) applyMigrations(ctx context.Context, pluginID string, migrations []RegisteredMigration) error {
	if len(migrations) == 0 {
		return nil
	}
	return m.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		for _, migration := range migrations {
			var applied models.ServerPluginMigration
			err := tx.Where("plugin_id = ? AND id = ?", pluginID, migration.ID).First(&applied).Error
			if err == nil {
				continue
			}
			if !errors.Is(err, gorm.ErrRecordNotFound) {
				return fmt.Errorf("load plugin migration %s: %w", migration.ID, err)
			}
			for _, statement := range migration.Statements {
				if err := tx.Exec(statement).Error; err != nil {
					return fmt.Errorf("execute plugin migration %s: %w", migration.ID, err)
				}
			}
			if err := tx.Create(&models.ServerPluginMigration{PluginID: pluginID, ID: migration.ID}).Error; err != nil {
				return err
			}
		}
		return nil
	})
}

func (m *Manager) checkDependencies(dependencies []RegisteredDependency) error {
	for _, dependency := range dependencies {
		var record models.ServerPlugin
		if err := m.db.Where("id = ? AND enabled = ?", dependency.PluginID, true).First(&record).Error; err != nil {
			return fmt.Errorf("plugin dependency %s is not enabled: %w", dependency.PluginID, err)
		}
		if !versionMatches(record.Version, dependency.Constraint) {
			return fmt.Errorf("plugin dependency %s version %s does not satisfy %s", dependency.PluginID, record.Version, dependency.Constraint)
		}
	}
	return nil
}

func versionMatches(version, constraint string) bool {
	constraint = strings.TrimSpace(constraint)
	if constraint == "" || constraint == "*" {
		return true
	}
	operator := "="
	wanted := constraint
	for _, candidate := range []string{">=", "<=", ">", "<", "="} {
		if strings.HasPrefix(constraint, candidate) {
			operator = candidate
			wanted = strings.TrimSpace(strings.TrimPrefix(constraint, candidate))
			break
		}
	}
	comparison := compareVersion(version, wanted)
	switch operator {
	case ">=":
		return comparison >= 0
	case "<=":
		return comparison <= 0
	case ">":
		return comparison > 0
	case "<":
		return comparison < 0
	default:
		return comparison == 0
	}
}

func compareVersion(left, right string) int {
	leftParts := strings.Split(strings.TrimPrefix(left, "v"), ".")
	rightParts := strings.Split(strings.TrimPrefix(right, "v"), ".")
	for index := 0; index < 3; index++ {
		leftValue := versionPart(leftParts, index)
		rightValue := versionPart(rightParts, index)
		if leftValue < rightValue {
			return -1
		}
		if leftValue > rightValue {
			return 1
		}
	}
	return 0
}

func versionPart(parts []string, index int) int {
	if index >= len(parts) {
		return 0
	}
	value, _ := strconv.Atoi(strings.SplitN(parts[index], "-", 2)[0])
	return value
}

func invokeInstanceLifecycle(ctx context.Context, instance pluginInstance, callback, phase string) error {
	if callback == "" {
		return nil
	}
	response, err := instance.Invoke(ctx, PluginRequest{
		Method:   "LIFECYCLE",
		Path:     "/lifecycle/" + phase,
		Callback: callback,
	})
	if err != nil {
		return err
	}
	if response.Status >= 400 {
		return fmt.Errorf("lifecycle callback returned status %d", response.Status)
	}
	return nil
}
