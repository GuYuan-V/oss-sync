package serverplugin

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

// writePackage 把插件包写入暂存目录后整体改名为插件目录
// 目标目录已存在返回 ErrPluginExists；失败时清理暂存目录
// Windows 下为无后缀的可执行入口额外写入 .exe 副本
func writePackage(root string, packageData Package) (target string, retErr error) {
	if err := os.MkdirAll(root, 0o750); err != nil {
		return "", fmt.Errorf("create plugin root: %w", err)
	}
	tmp, err := os.MkdirTemp(root, ".install-")
	if err != nil {
		return "", fmt.Errorf("create plugin staging directory: %w", err)
	}
	complete := false
	defer func() {
		if !complete {
			if cleanupErr := os.RemoveAll(tmp); cleanupErr != nil {
				retErr = errors.Join(retErr, fmt.Errorf("cleanup plugin staging directory: %w", cleanupErr))
			}
		}
	}()
	for name, content := range packageData.Files {
		path := filepath.Join(tmp, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
			return "", fmt.Errorf("create plugin file directory: %w", err)
		}
		mode := os.FileMode(0o600)
		if manifestEntrypointFile(packageData.Manifest, name) {
			mode = 0o700
		}
		if err := os.WriteFile(path, content, mode); err != nil {
			return "", fmt.Errorf("write plugin file: %w", err)
		}
		// Windows 需要 .exe 后缀才能启动可执行文件，为无后缀入口写入别名
		if runtime.GOOS == "windows" && manifestRuntime(packageData.Manifest) == RuntimeExecutable {
			if entrypoint, err := manifestEntrypoint(packageData.Manifest); err == nil && entrypoint == name && !strings.HasSuffix(name, ".exe") {
				if err := os.WriteFile(path+".exe", content, 0o700); err != nil {
					return "", fmt.Errorf("write plugin executable alias: %w", err)
				}
			}
		}
	}
	target = filepath.Join(root, packageData.Manifest.ID)
	if _, err := os.Stat(target); err == nil {
		return "", ErrPluginExists
	} else if !errors.Is(err, os.ErrNotExist) {
		return "", fmt.Errorf("check existing plugin: %w", err)
	}
	if err := os.Rename(tmp, target); err != nil {
		return "", fmt.Errorf("install plugin package: %w", err)
	}
	complete = true
	return target, nil
}

// executableEntrypoint 返回可执行运行时的入口文件名
// WASM 或无入口时返回空串
func executableEntrypoint(manifest Manifest) string {
	entrypoint, _ := manifestEntrypoint(manifest)
	return entrypoint
}

// manifestEntrypointFile 判断文件是否为运行时入口
// Windows 上无后缀入口与补 .exe 后的副本都算入口；WASM 以 executableEntrypoint 为准
func manifestEntrypointFile(manifest Manifest, name string) bool {
	for _, entrypoint := range manifest.Entrypoints {
		if entrypoint == name {
			return true
		}
		// Windows 上无后缀的可执行文件会自动补 .exe，写入时也要匹配
		if runtime.GOOS == "windows" && !strings.HasSuffix(entrypoint, ".exe") && entrypoint+".exe" == name {
			return true
		}
	}
	return manifestRuntime(manifest) == RuntimeWASM && name == executableEntrypoint(manifest)
}

// readInstalledPackage 从磁盘读取已安装插件包并重算哈希
// 拒绝符号链接与越界路径，逐文件受大小上限约束，文件数与总量超限报错
// WASM 的载荷哈希取 wasm 内容哈希，其余运行时取全量文件哈希
func readInstalledPackage(root, id string) (Package, error) {
	if !pluginIDPattern.MatchString(id) {
		return Package{}, fmt.Errorf("%w: invalid plugin id", ErrPluginNotFound)
	}
	dir := filepath.Join(root, id)
	manifestBytes, err := readInstalledFile(filepath.Join(dir, "manifest.json"), MaxManifestBytes)
	if err != nil {
		return Package{}, fmt.Errorf("%w: read plugin manifest: %v", ErrPluginNotFound, err)
	}
	manifest, err := ParseManifest(manifestBytes)
	if err != nil {
		return Package{}, err
	}
	if manifest.ID != id {
		return Package{}, fmt.Errorf("%w: installed package id does not match directory", ErrInvalidPackage)
	}
	files := map[string][]byte{"manifest.json": manifestBytes}
	var total int64 = int64(len(manifestBytes))
	fileCount := 1
	err = filepath.WalkDir(dir, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() || path == filepath.Join(dir, "manifest.json") {
			return nil
		}
		if entry.Type()&os.ModeSymlink != 0 {
			return errors.New("installed plugin symlink is not allowed")
		}
		rel, err := filepath.Rel(dir, path)
		if err != nil {
			return err
		}
		name := filepath.ToSlash(rel)
		if !validPluginFilePath(name) {
			return errors.New("installed plugin file path is invalid")
		}
		limit := int64(MaxPluginFileBytes)
		if name == "plugin.wasm" {
			limit = MaxWasmBytes
		}
		content, err := readInstalledFile(path, limit)
		if err != nil {
			return err
		}
		files[name] = content
		fileCount++
		if fileCount > MaxPluginFiles {
			return errors.New("installed plugin has too many files")
		}
		total += int64(len(content))
		if total > MaxExtractedBytes {
			return errors.New("installed plugin files are too large")
		}
		return nil
	})
	if err != nil {
		return Package{}, fmt.Errorf("%w: read plugin files: %v", ErrPluginNotFound, err)
	}
	entrypoint, err := manifestEntrypoint(manifest)
	if err != nil {
		return Package{}, fmt.Errorf("%w: %v", ErrInvalidPackage, err)
	}
	if _, exists := files[entrypoint]; !exists {
		return Package{}, fmt.Errorf("%w: entrypoint is missing", ErrInvalidPackage)
	}
	if err := validateResourceFiles(manifest, files); err != nil {
		return Package{}, err
	}
	manifestDigest := sha256.Sum256(manifestBytes)
	wasm := files["plugin.wasm"]
	wasmDigest := sha256.Sum256(wasm)
	payloadSize := int64(0)
	for name, content := range files {
		if name != "manifest.json" {
			payloadSize += int64(len(content))
		}
	}
	wasmHash := hex.EncodeToString(wasmDigest[:])
	payloadHash := packageHash(files)
	if manifestRuntime(manifest) == RuntimeWASM {
		payloadHash = wasmHash
		payloadSize = int64(len(wasm))
	}
	return Package{
		Manifest:      manifest,
		ManifestBytes: manifestBytes,
		ManifestHash:  hex.EncodeToString(manifestDigest[:]),
		Files:         files,
		PayloadHash:   payloadHash,
		PayloadSize:   payloadSize,
		Wasm:          wasm,
		WasmHash:      wasmHash,
	}, nil
}

// readInstalledFile 读取单个插件文件
// 仅接受不超过 maxBytes 的常规文件，符号链接一律拒绝
func readInstalledFile(path string, maxBytes int64) ([]byte, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return nil, err
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() || info.Size() > maxBytes {
		return nil, errors.New("installed plugin file is invalid or too large")
	}
	return os.ReadFile(path)
}
