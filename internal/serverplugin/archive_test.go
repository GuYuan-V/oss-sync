package serverplugin

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"errors"
	"testing"
)

func TestParsePackageRejectsTraversalEntry(t *testing.T) {
	archiveBytes := makePackageArchive(t, map[string][]byte{
		"manifest.json":  []byte(validManifestJSON),
		"../plugin.wasm": []byte("wasm"),
	})

	_, err := ParsePackage(bytes.NewReader(archiveBytes), int64(len(archiveBytes)))
	if !errors.Is(err, ErrInvalidPackage) {
		t.Fatalf("ParsePackage() error = %v, want ErrInvalidPackage", err)
	}
}

func TestParsePackageRejectsUnexpectedEntry(t *testing.T) {
	archiveBytes := makePackageArchive(t, map[string][]byte{
		"manifest.json": []byte(validManifestJSON),
		"plugin.wasm":   []byte("wasm"),
		"readme.txt":    []byte("not part of v1"),
	})

	_, err := ParsePackage(bytes.NewReader(archiveBytes), int64(len(archiveBytes)))
	if !errors.Is(err, ErrInvalidPackage) {
		t.Fatalf("ParsePackage() error = %v, want ErrInvalidPackage", err)
	}
}

func TestParsePackageAcceptsExecutableAssets(t *testing.T) {
	manifest, err := json.Marshal(Manifest{
		ID:          "executable-world",
		Name:        "Executable world",
		Version:     "1.0.0",
		APIVersion:  CurrentAPIVersion,
		Runtime:     RuntimeExecutable,
		Entrypoints: map[string]string{"any": "bin/plugin.exe"},
		Routes:      []RouteSpec{{Method: "GET", Path: "/hello", Public: true}},
	})
	if err != nil {
		t.Fatal(err)
	}
	archiveBytes := makePackageArchive(t, map[string][]byte{
		"manifest.json":   manifest,
		"bin/plugin.exe":  []byte("trusted executable"),
		"assets/template": []byte("hello"),
	})

	packageData, err := ParsePackage(bytes.NewReader(archiveBytes), int64(len(archiveBytes)))
	if err != nil {
		t.Fatalf("ParsePackage() error = %v", err)
	}
	if packageData.Manifest.Runtime != RuntimeExecutable || string(packageData.Files["assets/template"]) != "hello" {
		t.Fatalf("parsed executable package = %+v", packageData)
	}
}

func TestParsePackageAcceptsDeclaredThemeResources(t *testing.T) {
	manifest, err := json.Marshal(Manifest{
		ID: "resource-world", Name: "Resource world", Version: "1.0.0", APIVersion: CurrentAPIVersion,
		Runtime: RuntimeExecutable, Entrypoints: map[string]string{"any": "bin/plugin.exe"},
		Routes:        []RouteSpec{{Method: "GET", Path: "/hello", Public: true}},
		BlogThemes:    []ThemeResource{{ID: "clean", Name: "Clean", Path: "blog/clean"}},
		ConsoleThemes: []ThemeResource{{ID: "clean", Name: "Console", Path: "console/clean"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	archiveBytes := makePackageArchive(t, map[string][]byte{
		"manifest.json": manifest, "bin/plugin.exe": []byte("trusted executable"),
		"blog/clean/template.html": []byte("{{.Title}}"), "console/clean/theme.css": []byte(":root{}"),
	})
	if _, err := ParsePackage(bytes.NewReader(archiveBytes), int64(len(archiveBytes))); err != nil {
		t.Fatalf("ParsePackage() error = %v", err)
	}
}

const validManifestJSON = `{"id":"hello-world","name":"Hello world","version":"1.0.0","api_version":1,"routes":[{"method":"GET","path":"/hello","public":true}]}`

func makePackageArchive(t *testing.T, files map[string][]byte) []byte {
	t.Helper()
	var buf bytes.Buffer
	writer := zip.NewWriter(&buf)
	for name, content := range files {
		entry, err := writer.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := entry.Write(content); err != nil {
			t.Fatal(err)
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}
