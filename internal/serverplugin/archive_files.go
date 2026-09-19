package serverplugin

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

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

func executableEntrypoint(manifest Manifest) string {
	entrypoint, _ := manifestEntrypoint(manifest)
	return entrypoint
}

func manifestEntrypointFile(manifest Manifest, name string) bool {
	for _, entrypoint := range manifest.Entrypoints {
		if entrypoint == name {
			return true
		}
	}
	return manifestRuntime(manifest) == RuntimeWASM && name == executableEntrypoint(manifest)
}

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
