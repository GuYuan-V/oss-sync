package serverplugin

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/helantianshen/oss-sync/internal/models"
)

// maxEditablePluginFileBytes 是在线编辑单文件的内容上限
const maxEditablePluginFileBytes = 1 << 20

// EditablePluginFile 是插件包中可在线编辑的文本文件
type EditablePluginFile struct {
	Path    string
	Content string
}

// EditableFiles 返回插件包内可编辑的文本文件
// 按路径排序，最多返回 64 个
func (m *Manager) EditableFiles(id string) ([]EditablePluginFile, error) {
	packageData, err := readInstalledPackage(m.root, id)
	if err != nil {
		return nil, err
	}
	paths := make([]string, 0, len(packageData.Files))
	for path, content := range packageData.Files {
		if !isEditablePluginFile(packageData.Manifest, path, content) {
			continue
		}
		paths = append(paths, path)
	}
	sort.Strings(paths)
	if len(paths) > 64 {
		paths = paths[:64]
	}
	files := make([]EditablePluginFile, 0, len(paths))
	for _, path := range paths {
		files = append(files, EditablePluginFile{Path: path, Content: string(packageData.Files[path])})
	}
	return files, nil
}

// SaveTextFile 覆盖插件包内一个可编辑文本文件并重新加载插件
// 内容须为不超过 1 MiB 的合法 UTF-8；内置插件与入口文件不可写
// 编辑前先停用插件，校验失败时回滚文件内容、元数据与启用状态
func (m *Manager) SaveTextFile(ctx context.Context, id, path, content string) (resultErr error) {
	if ctx == nil {
		return errors.New("plugin editor context is nil")
	}
	if !pluginIDPattern.MatchString(id) || !validPluginFilePath(path) {
		return fmt.Errorf("invalid plugin file path")
	}
	if len(content) > maxEditablePluginFileBytes || !utf8.ValidString(content) || strings.ContainsRune(content, '\x00') {
		return fmt.Errorf("plugin text file is too large or invalid")
	}

	m.lifecycleMu.Lock()
	defer m.lifecycleMu.Unlock()

	record, err := m.record(id)
	if err != nil {
		return err
	}
	if record.Builtin {
		return errors.New("built-in plugin files are read-only")
	}
	before, err := readInstalledPackage(m.root, id)
	if err != nil {
		return err
	}
	old, exists := before.Files[path]
	if !exists || !isEditablePluginFile(before.Manifest, path, old) {
		return errors.New("plugin file is not editable")
	}
	wasEnabled := record.Enabled
	committed := false
	defer func() {
		if committed {
			return
		}
		recovery, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		if err := writeInstalledTextFile(m.root, id, path, old); err != nil {
			resultErr = errors.Join(resultErr, fmt.Errorf("restore plugin file: %w", err))
			return
		}
		record.Enabled = false
		if err := m.db.WithContext(recovery).Save(&record).Error; err != nil {
			resultErr = errors.Join(resultErr, fmt.Errorf("restore plugin metadata: %w", err))
			return
		}
		if wasEnabled {
			if err := m.enable(recovery, id); err != nil {
				resultErr = errors.Join(resultErr, fmt.Errorf("restore enabled plugin: %w", err))
			}
		}
	}()
	if wasEnabled {
		if err := m.disable(ctx, id); err != nil {
			return err
		}
	}

	if err := writeInstalledTextFile(m.root, id, path, []byte(content)); err != nil {
		return err
	}
	after, err := readInstalledPackage(m.root, id)
	if err != nil {
		return err
	}
	if err := m.validatePackage(ctx, after); err != nil {
		return err
	}
	if manifestRuntime(after.Manifest) == RuntimeExecutable {
		instance, err := m.openInstance(ctx, filepath.Join(m.root, id), after)
		if err != nil {
			return err
		}
		if err := instance.Close(ctx); err != nil {
			return err
		}
	}
	manifestJSON, err := manifestJSON(after.Manifest)
	if err != nil {
		return err
	}
	updates := map[string]any{
		"name":          after.Manifest.Name,
		"version":       after.Manifest.Version,
		"description":   after.Manifest.Description,
		"api_version":   after.Manifest.APIVersion,
		"runtime":       manifestRuntime(after.Manifest),
		"manifest_json": manifestJSON,
		"manifest_hash": after.ManifestHash,
		"wasm_hash":     after.WasmHash,
		"wasm_size":     int64(len(after.Wasm)),
		"payload_hash":  after.PayloadHash,
		"payload_size":  after.PayloadSize,
		"last_error":    "",
	}
	if err := m.db.Model(&models.ServerPlugin{}).Where("id = ?", id).Updates(updates).Error; err != nil {
		return err
	}
	if wasEnabled {
		if err := m.enable(ctx, id); err != nil {
			return err
		}
	}
	committed = true
	return nil
}

// isEditablePluginFile 判断文件是否允许在线编辑
// 排除入口文件与 plugin.wasm，仅接受白名单扩展名的合法 UTF-8 文本
func isEditablePluginFile(manifest Manifest, path string, content []byte) bool {
	if path == "" || manifestEntrypointFile(manifest, path) || path == "plugin.wasm" {
		return false
	}
	ext := strings.ToLower(filepath.Ext(path))
	switch ext {
	case ".css", ".html", ".js", ".json", ".md", ".svg", ".txt", ".xml", ".yaml", ".yml":
		return utf8.Valid(content) && !strings.ContainsRune(string(content), '\x00')
	default:
		return false
	}
}

// writeInstalledTextFile 以临时文件加改名的方式原子替换已安装文件
// 目标必须是插件目录内的常规文件，符号链接一律拒绝
func writeInstalledTextFile(root, id, path string, content []byte) error {
	dir := filepath.Join(root, id)
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return err
	}
	target := filepath.Join(dir, filepath.FromSlash(path))
	if rel, err := filepath.Rel(dir, target); err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(os.PathSeparator)) {
		return errors.New("invalid plugin file path")
	}
	if info, err := os.Lstat(target); err == nil && info.Mode()&fs.ModeSymlink != 0 {
		return errors.New("plugin file symlink is not allowed")
	}
	tmp, err := os.CreateTemp(filepath.Dir(target), ".plugin-edit-")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)
	if err := tmp.Chmod(0o600); err != nil {
		_ = tmp.Close()
		return err
	}
	if _, err := tmp.Write(content); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmpName, target)
}
