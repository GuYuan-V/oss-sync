package blog

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestScaffoldTheme_copiesEntireDirectory_whenBaseIsCustom(t *testing.T) {
	// Given
	dataDir := t.TempDir()
	sourceDir := filepath.Join(dataDir, "themes", "source")
	if err := os.MkdirAll(filepath.Join(sourceDir, "assets"), 0o750); err != nil {
		t.Fatal(err)
	}
	files := map[string]string{
		"template.html":   "<main>{{.ContentHTML}}</main>",
		"style.css":       "body { color: black; }",
		"theme.js":        "console.log('theme');",
		"settings.json":   `{"settings":[]}`,
		"assets/mark.svg": "<svg></svg>",
	}
	for name, content := range files {
		if err := os.WriteFile(filepath.Join(sourceDir, filepath.FromSlash(name)), []byte(content), 0o640); err != nil {
			t.Fatal(err)
		}
	}

	// When
	targetDir, err := ScaffoldTheme(dataDir, "source", "copy")

	// Then
	if err != nil {
		t.Fatalf("ScaffoldTheme() error = %v", err)
	}
	for name, want := range files {
		got, readErr := os.ReadFile(filepath.Join(targetDir, filepath.FromSlash(name)))
		if readErr != nil {
			t.Fatalf("read copied %s: %v", name, readErr)
		}
		if string(got) != want {
			t.Errorf("copied %s = %q, want %q", name, got, want)
		}
	}
	marker, err := os.ReadFile(filepath.Join(targetDir, ".oss-theme-source"))
	if err != nil {
		t.Fatal(err)
	}
	if string(marker) != "cloned" {
		t.Fatalf("source marker = %q, want cloned", marker)
	}
}

func TestScaffoldTheme_copiesAssetsAndReadme_whenBaseIsBuiltin(t *testing.T) {
	// Given
	dataDir := t.TempDir()

	// When
	targetDir, err := ScaffoldTheme(dataDir, "papertrail", "paper-copy")

	// Then
	if err != nil {
		t.Fatalf("ScaffoldTheme() error = %v", err)
	}
	for _, name := range []string{"template.html", "style.css", "theme.js", "README.md"} {
		if _, statErr := os.Stat(filepath.Join(targetDir, name)); statErr != nil {
			t.Errorf("expected copied %s: %v", name, statErr)
		}
	}
	marker, err := os.ReadFile(filepath.Join(targetDir, ".oss-theme-source"))
	if err != nil {
		t.Fatal(err)
	}
	if string(marker) != "scaffolded" {
		t.Fatalf("source marker = %q, want scaffolded", marker)
	}
}

func TestScaffoldTheme_backfillsRenderableTemplate_whenDefaultHasNoTemplate(t *testing.T) {
	// Given
	dataDir := t.TempDir()

	// When
	targetDir, err := ScaffoldTheme(dataDir, "default", "default-copy")

	// Then
	if err != nil {
		t.Fatalf("ScaffoldTheme() error = %v", err)
	}
	got, err := os.ReadFile(filepath.Join(targetDir, "template.html"))
	if err != nil {
		t.Fatal(err)
	}
	want, err := themeAssetsFS.ReadFile("assets/development-template/template.html")
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(want) {
		t.Fatal("default clone did not use the bundled development template")
	}
}

func TestScaffoldTheme_rejectsInvalidSourceOrTarget(t *testing.T) {
	tests := []struct {
		name    string
		base    string
		target  string
		prepare func(t *testing.T, dataDir string)
		wantErr error
	}{
		{name: "missing source", base: "missing", target: "copy"},
		{name: "builtin target", base: "default", target: "papertrail"},
		{name: "invalid target", base: "default", target: "../copy"},
		{
			name:   "existing target",
			base:   "default",
			target: "copy",
			prepare: func(t *testing.T, dataDir string) {
				t.Helper()
				if err := os.MkdirAll(filepath.Join(dataDir, "themes", "copy"), 0o750); err != nil {
					t.Fatal(err)
				}
			},
			wantErr: errThemeExists,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Given
			dataDir := t.TempDir()
			if tt.prepare != nil {
				tt.prepare(t, dataDir)
			}

			// When
			_, err := ScaffoldTheme(dataDir, tt.base, tt.target)

			// Then
			if err == nil {
				t.Fatal("ScaffoldTheme() error = nil, want error")
			}
			if tt.wantErr != nil && !errors.Is(err, tt.wantErr) {
				t.Fatalf("ScaffoldTheme() error = %v, want %v", err, tt.wantErr)
			}
		})
	}
}
