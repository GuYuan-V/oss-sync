package serverplugin

import (
	"archive/zip"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"
)

// Package 持有已校验的插件包及其解包后的文件
type Package struct {
	Manifest      Manifest
	ManifestBytes []byte
	ManifestHash  string
	Files         map[string][]byte
	PayloadHash   string
	PayloadSize   int64
	Wasm          []byte
	WasmHash      string
}

// ParsePackage 校验 ZIP 约束、manifest 与平台入口并返回解包内容
func ParsePackage(reader io.ReaderAt, size int64) (Package, error) {
	if reader == nil || size <= 0 || size > MaxArchiveBytes {
		return Package{}, fmt.Errorf("%w: archive size is invalid", ErrInvalidPackage)
	}
	archive, err := zip.NewReader(reader, size)
	if err != nil {
		return Package{}, fmt.Errorf("%w: %v", ErrInvalidPackage, err)
	}
	if len(archive.File) == 0 || len(archive.File) > MaxPluginFiles {
		return Package{}, fmt.Errorf("%w: archive file count is invalid", ErrInvalidPackage)
	}

	packageData := Package{Files: make(map[string][]byte, len(archive.File))}
	var total int64
	for _, file := range archive.File {
		if file.FileInfo().IsDir() || file.Mode()&os.ModeSymlink != 0 || !validPluginFilePath(file.Name) {
			return Package{}, fmt.Errorf("%w: unsafe archive entry %q", ErrInvalidPackage, file.Name)
		}
		if _, exists := packageData.Files[file.Name]; exists {
			return Package{}, fmt.Errorf("%w: duplicate archive entry %q", ErrInvalidPackage, file.Name)
		}
		limit := int64(MaxPluginFileBytes)
		if file.Name == "manifest.json" {
			limit = MaxManifestBytes
		}
		if file.Name == "plugin.wasm" {
			limit = MaxWasmBytes
		}
		content, err := readArchiveEntry(file, limit)
		if err != nil {
			return Package{}, err
		}
		total += int64(len(content))
		if total > MaxExtractedBytes {
			return Package{}, fmt.Errorf("%w: extracted archive is too large", ErrInvalidPackage)
		}
		packageData.Files[file.Name] = content
	}

	packageData.ManifestBytes = packageData.Files["manifest.json"]
	manifest, err := ParseManifest(packageData.ManifestBytes)
	if err != nil {
		return Package{}, err
	}
	entrypoint, err := manifestEntrypoint(manifest)
	if err != nil {
		return Package{}, fmt.Errorf("%w: %v", ErrInvalidPackage, err)
	}
	if _, exists := packageData.Files[entrypoint]; !exists {
		return Package{}, fmt.Errorf("%w: entrypoint %q is missing", ErrInvalidPackage, entrypoint)
	}
	if manifestRuntime(manifest) == RuntimeWASM {
		for name := range packageData.Files {
			if name == "manifest.json" || name == "plugin.wasm" || declaredResourceFile(manifest, name) {
				continue
			}
			return Package{}, fmt.Errorf("%w: WASM archive entry %q is not declared as a resource", ErrInvalidPackage, name)
		}
	}
	packageData.Manifest = manifest
	if err := validateResourceFiles(manifest, packageData.Files); err != nil {
		return Package{}, err
	}
	packageData.PayloadSize = total - int64(len(packageData.ManifestBytes))
	packageData.Wasm = packageData.Files["plugin.wasm"]
	manifestDigest := sha256.Sum256(packageData.ManifestBytes)
	packageData.ManifestHash = hex.EncodeToString(manifestDigest[:])
	wasmDigest := sha256.Sum256(packageData.Wasm)
	packageData.WasmHash = hex.EncodeToString(wasmDigest[:])
	packageData.PayloadHash = packageHash(packageData.Files)
	if manifestRuntime(manifest) == RuntimeWASM {
		packageData.PayloadHash = packageData.WasmHash
		packageData.PayloadSize = int64(len(packageData.Wasm))
	}
	return packageData, nil
}

func validateResourceFiles(manifest Manifest, files map[string][]byte) error {
	for _, resource := range manifest.BlogThemes {
		if _, ok := files[resource.Path+"/template.html"]; !ok {
			return fmt.Errorf("%w: blog theme resource %q is missing template.html", ErrInvalidPackage, resource.ID)
		}
	}
	for _, resource := range manifest.ConsoleThemes {
		if _, ok := files[resource.Path+"/theme.css"]; !ok {
			return fmt.Errorf("%w: console theme resource %q is missing theme.css", ErrInvalidPackage, resource.ID)
		}
	}
	return nil
}

func declaredResourceFile(manifest Manifest, name string) bool {
	for _, resource := range append(append([]ThemeResource{}, manifest.BlogThemes...), manifest.ConsoleThemes...) {
		if strings.HasPrefix(name, resource.Path+"/") {
			return true
		}
	}
	return false
}
func readArchiveEntry(file *zip.File, maxBytes int64) ([]byte, error) {
	if file.UncompressedSize64 > uint64(maxBytes) {
		return nil, fmt.Errorf("%w: archive entry %q is too large", ErrInvalidPackage, file.Name)
	}
	rc, err := file.Open()
	if err != nil {
		return nil, fmt.Errorf("%w: open archive entry %q: %v", ErrInvalidPackage, file.Name, err)
	}
	defer rc.Close()
	content, err := io.ReadAll(io.LimitReader(rc, maxBytes+1))
	if err != nil {
		return nil, fmt.Errorf("%w: read archive entry %q: %v", ErrInvalidPackage, file.Name, err)
	}
	if int64(len(content)) > maxBytes {
		return nil, fmt.Errorf("%w: archive entry %q is too large", ErrInvalidPackage, file.Name)
	}
	return content, nil
}

func packageHash(files map[string][]byte) string {
	names := make([]string, 0, len(files))
	for name := range files {
		if name != "manifest.json" {
			names = append(names, name)
		}
	}
	sort.Strings(names)
	hash := sha256.New()
	for _, name := range names {
		_, _ = hash.Write([]byte(name))
		_, _ = hash.Write([]byte{0})
		_, _ = hash.Write(files[name])
	}
	return hex.EncodeToString(hash.Sum(nil))
}
