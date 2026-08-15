package consoletheme

import (
	"archive/zip"
	"bytes"
	"io"
	"os"
	"path/filepath"
	"testing"
)

func TestList_includesBuiltinAndCustomThemes(t *testing.T) {
	// Given
	dataDir := t.TempDir()
	dir := filepath.Join(dataDir, "console-themes", "custom")
	if err := os.MkdirAll(dir, 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "theme.css"), []byte("body{}"), 0o640); err != nil {
		t.Fatal(err)
	}

	// When
	themes, err := List(dataDir)

	// Then
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}
	if len(themes) != 2 || themes[0].Name != "default" || themes[1].Name != "custom" {
		t.Fatalf("themes = %#v", themes)
	}
}

func TestScaffold_copiesBuiltinOrCustomTheme(t *testing.T) {
	// Given
	dataDir := t.TempDir()
	sourceDir := filepath.Join(dataDir, "console-themes", "source", "assets")
	if err := os.MkdirAll(sourceDir, 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(filepath.Dir(sourceDir), "theme.css"), []byte("body{color:red}"), 0o640); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(sourceDir, "grid.svg"), []byte("<svg></svg>"), 0o640); err != nil {
		t.Fatal(err)
	}

	// When
	builtinDir, builtinErr := Scaffold(dataDir, "default", "default-copy")
	customDir, customErr := Scaffold(dataDir, "source", "source-copy")

	// Then
	if builtinErr != nil || customErr != nil {
		t.Fatalf("Scaffold() errors = %v, %v", builtinErr, customErr)
	}
	if _, err := os.Stat(filepath.Join(builtinDir, "theme.css")); err != nil {
		t.Fatalf("builtin copy missing theme.css: %v", err)
	}
	got, err := os.ReadFile(filepath.Join(customDir, "assets", "grid.svg"))
	if err != nil || string(got) != "<svg></svg>" {
		t.Fatalf("custom asset = %q, error = %v", got, err)
	}
}

func TestUpload_acceptsSafePackageAndRejectsTraversal(t *testing.T) {
	// Given
	dataDir := t.TempDir()
	valid := zipBytes(t, map[string]string{
		"theme.css":       ":root{--canvas:white}",
		"assets/mark.svg": "<svg></svg>",
	})
	unsafe := zipBytes(t, map[string]string{
		"theme.css":   "body{}",
		"../evil.css": "x",
	})

	// When
	validErr := Upload(dataDir, "uploaded", bytes.NewReader(valid), int64(len(valid)))
	unsafeErr := Upload(dataDir, "unsafe", bytes.NewReader(unsafe), int64(len(unsafe)))

	// Then
	if validErr != nil {
		t.Fatalf("Upload(valid) error = %v", validErr)
	}
	if unsafeErr == nil {
		t.Fatal("Upload(unsafe) error = nil")
	}
	if _, err := os.Stat(filepath.Join(dataDir, "console-themes", "unsafe")); !os.IsNotExist(err) {
		t.Fatalf("unsafe package left a directory: %v", err)
	}
}

func TestSaveFile_editsTextAndRejectsEscapedPath(t *testing.T) {
	// Given
	dataDir := t.TempDir()
	dir := filepath.Join(dataDir, "console-themes", "custom")
	if err := os.MkdirAll(dir, 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "theme.css"), []byte("body{}"), 0o640); err != nil {
		t.Fatal(err)
	}

	// When
	saveErr := SaveFile(dataDir, "custom", "theme.css", []byte("body{color:blue}"))
	escapeErr := SaveFile(dataDir, "custom", "../outside.css", []byte("x"))

	// Then
	if saveErr != nil {
		t.Fatalf("SaveFile() error = %v", saveErr)
	}
	if escapeErr == nil {
		t.Fatal("SaveFile(escaped path) error = nil")
	}
	got, err := ReadFile(dataDir, "custom", "theme.css")
	if err != nil || string(got) != "body{color:blue}" {
		t.Fatalf("ReadFile() = %q, %v", got, err)
	}
}

func TestCreateZipAndDelete_roundTripsCustomTheme(t *testing.T) {
	// Given
	dataDir := t.TempDir()
	if _, err := Scaffold(dataDir, "default", "custom"); err != nil {
		t.Fatal(err)
	}
	var archive bytes.Buffer
	writer := zip.NewWriter(&archive)

	// When
	zipErr := CreateZip(dataDir, "custom", writer)
	closeErr := writer.Close()
	deleteErr := Delete(dataDir, "custom")

	// Then
	if zipErr != nil || closeErr != nil || deleteErr != nil {
		t.Fatalf("round trip errors = %v, %v, %v", zipErr, closeErr, deleteErr)
	}
	reader, err := zip.NewReader(bytes.NewReader(archive.Bytes()), int64(archive.Len()))
	if err != nil {
		t.Fatal(err)
	}
	foundCSS := false
	for _, file := range reader.File {
		if file.Name != "theme.css" {
			continue
		}
		rc, err := file.Open()
		if err != nil {
			t.Fatal(err)
		}
		_, err = io.ReadAll(rc)
		_ = rc.Close()
		if err != nil {
			t.Fatal(err)
		}
		foundCSS = true
	}
	if !foundCSS {
		t.Fatal("ZIP missing theme.css")
	}
	if _, err := os.Stat(filepath.Join(dataDir, "console-themes", "custom")); !os.IsNotExist(err) {
		t.Fatalf("custom theme still exists: %v", err)
	}
}

func zipBytes(t *testing.T, files map[string]string) []byte {
	t.Helper()
	var buffer bytes.Buffer
	writer := zip.NewWriter(&buffer)
	for name, content := range files {
		entry, err := writer.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := entry.Write([]byte(content)); err != nil {
			t.Fatal(err)
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	return buffer.Bytes()
}
