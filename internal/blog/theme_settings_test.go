package blog

import (
	"os"
	"path/filepath"
	"testing"
)

func TestThemeSettings_loadsDeclaredFields_whenThemeIsPapertrail(t *testing.T) {
	// Given
	dataDir := t.TempDir()

	// When
	fields, err := ThemeSettings(dataDir, "papertrail")

	// Then
	if err != nil {
		t.Fatalf("ThemeSettings() error = %v", err)
	}
	if len(fields) != 6 {
		t.Fatalf("field count = %d, want 6", len(fields))
	}
	if fields[0].Key != "blog_name" || fields[0].Type != "text" {
		t.Fatalf("first field = %#v", fields[0])
	}
	if fields[3].Key != "logo_size" || fields[3].Type != "text" {
		t.Fatalf("logo size field = %#v", fields[3])
	}
	if fields[4].Key != "logo_shape" || fields[4].Type != "choice" || len(fields[4].Choices) != 2 {
		t.Fatalf("logo shape field = %#v", fields[4])
	}
	if fields[5].Key != "buttons" || fields[5].Type != "group" || len(fields[5].Fields) != 3 {
		t.Fatalf("group field = %#v", fields[5])
	}
}

func TestThemeSettings_returnsEmpty_whenThemeHasNoDeclaration(t *testing.T) {
	// When
	fields, err := ThemeSettings(t.TempDir(), "default")

	// Then
	if err != nil {
		t.Fatalf("ThemeSettings() error = %v", err)
	}
	if len(fields) != 0 {
		t.Fatalf("fields = %#v, want empty", fields)
	}
}

func TestThemeSettings_loadsDeclaration_whenThemeIsCustom(t *testing.T) {
	// Given
	dataDir := t.TempDir()
	dir := filepath.Join(dataDir, "themes", "custom")
	if err := os.MkdirAll(dir, 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "template.html"), []byte("{{.ContentHTML}}"), 0o640); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "settings.json"), []byte(`{
		"settings":[{"key":"subtitle","label":"副标题","type":"text","max_length":80}]
	}`), 0o640); err != nil {
		t.Fatal(err)
	}

	// When
	fields, err := ThemeSettings(dataDir, "custom")

	// Then
	if err != nil {
		t.Fatalf("ThemeSettings() error = %v", err)
	}
	if len(fields) != 1 || fields[0].Key != "subtitle" {
		t.Fatalf("fields = %#v", fields)
	}
}

func TestThemeSettings_rejectsInvalidDeclaration(t *testing.T) {
	tests := []struct {
		name   string
		schema string
	}{
		{name: "unknown type", schema: `{"settings":[{"key":"x","label":"X","type":"number","max_length":10}]}`},
		{name: "invalid key", schema: `{"settings":[{"key":"Bad-Key","label":"X","type":"text","max_length":10}]}`},
		{name: "duplicate key", schema: `{"settings":[{"key":"x","label":"X","type":"text","max_length":10},{"key":"x","label":"Y","type":"text","max_length":10}]}`},
		{name: "nested group", schema: `{"settings":[{"key":"rows","label":"Rows","type":"group","max_items":2,"fields":[{"key":"nested","label":"Nested","type":"group","max_items":2}]}]}`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Given
			dataDir := t.TempDir()
			dir := filepath.Join(dataDir, "themes", "custom")
			if err := os.MkdirAll(dir, 0o750); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(dir, "settings.json"), []byte(tt.schema), 0o640); err != nil {
				t.Fatal(err)
			}

			// When
			_, err := ThemeSettings(dataDir, "custom")

			// Then
			if err == nil {
				t.Fatal("ThemeSettings() error = nil, want invalid schema error")
			}
		})
	}
}

func TestValidateThemeConfig_returnsCleanDeclaredValues_whenInputIsValid(t *testing.T) {
	// Given
	fields := []ThemeSettingField{
		{Key: "blog_name", Label: "博客名称", Type: "text", MaxLength: 120},
		{Key: "logo_url", Label: "Logo URL", Type: "url", MaxLength: 512},
		{
			Key: "buttons", Label: "按钮", Type: "group", MaxItems: 2,
			Fields: []ThemeSettingField{
				{Key: "label", Label: "名称", Type: "text", MaxLength: 40, Required: true},
				{Key: "url", Label: "URL", Type: "url", MaxLength: 512, Required: true},
			},
		},
	}
	raw := map[string]any{
		"blog_name": "  Notes  ",
		"logo_url":  "/logo.svg",
		"ignored":   "drop me",
		"buttons": []any{
			map[string]any{"label": "Home", "url": "/"},
			map[string]any{"label": "Docs", "url": "https://example.com/docs"},
			map[string]any{"label": "Extra", "url": "/extra"},
		},
	}

	// When
	got, err := ValidateThemeConfig(fields, raw)

	// Then
	if err != nil {
		t.Fatalf("ValidateThemeConfig() error = %v", err)
	}
	if got["blog_name"] != "Notes" || got["logo_url"] != "/logo.svg" {
		t.Fatalf("scalar values = %#v", got)
	}
	if _, exists := got["ignored"]; exists {
		t.Fatal("unknown setting was retained")
	}
	buttons, ok := got["buttons"].([]any)
	if !ok || len(buttons) != 2 {
		t.Fatalf("buttons = %#v, want two rows", got["buttons"])
	}
	first, ok := buttons[0].(map[string]any)
	if !ok || first["position"] != 1 {
		t.Fatalf("first button = %#v", buttons[0])
	}
}

func TestValidateThemeConfig_rejectsInvalidURL(t *testing.T) {
	// Given
	fields := []ThemeSettingField{{Key: "logo_url", Label: "Logo", Type: "url", MaxLength: 512}}

	// When
	_, err := ValidateThemeConfig(fields, map[string]any{"logo_url": "javascript:alert(1)"})

	// Then
	if err == nil {
		t.Fatal("ValidateThemeConfig() error = nil, want URL validation error")
	}
}

func TestValidateThemeConfig_preservesPapertrailShape_whenLegacyValuesAreSaved(t *testing.T) {
	// Given
	fields, err := ThemeSettings(t.TempDir(), "papertrail")
	if err != nil {
		t.Fatal(err)
	}
	raw := map[string]any{
		"logo_url":    "/logo.svg",
		"logo_size":   "128",
		"blog_name":   "Paper notes",
		"description": "A notebook",
		"buttons": []any{
			map[string]any{"label": "Home", "url": "/", "icon_url": "/home.svg", "position": float64(1)},
		},
	}

	// When
	got, err := ValidateThemeConfig(fields, raw)

	// Then
	if err != nil {
		t.Fatalf("ValidateThemeConfig() error = %v", err)
	}
	cfg := ParsePaperTrailConfig(got)
	if cfg.BlogName != "Paper notes" || cfg.Description != "A notebook" || cfg.LogoURL != "/logo.svg" || cfg.LogoSize != 128 {
		t.Fatalf("parsed config = %#v", cfg)
	}
	if len(cfg.Buttons) != 1 || cfg.Buttons[0].Label != "Home" || cfg.Buttons[0].Position != 1 {
		t.Fatalf("parsed buttons = %#v", cfg.Buttons)
	}
}

func TestParsePaperTrailConfigRejectsOutOfRangeLogoSize(t *testing.T) {
	if got := ParsePaperTrailConfig(map[string]any{"logo_size": "9"}).LogoSize; got != 0 {
		t.Fatalf("logo size = %d, want 0", got)
	}
	if got := ParsePaperTrailConfig(map[string]any{"logo_size": "193"}).LogoSize; got != 0 {
		t.Fatalf("logo size = %d, want 0", got)
	}
}
