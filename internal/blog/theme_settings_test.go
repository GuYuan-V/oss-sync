package blog

import "testing"

func TestSupportsPublicBlog_allowsPapertrailButNotDefault(t *testing.T) {
	dataDir := t.TempDir()
	if SupportsPublicBlog(dataDir, "default") {
		t.Fatal("default theme must not support public blog")
	}
	if !SupportsPublicBlog(dataDir, "papertrail") {
		t.Fatal("papertrail theme must support public blog")
	}
}

func TestValidateThemeName_acceptsChineseAndRejectsPathCharacters(t *testing.T) {
	if err := ValidateThemeName("中文模板"); err != nil {
		t.Fatalf("Chinese theme name rejected: %v", err)
	}
	for _, name := range []string{"../escape", "模板/子目录", "-invalid", "带 空格"} {
		if err := ValidateThemeName(name); err == nil {
			t.Fatalf("unsafe theme name %q accepted", name)
		}
	}
}

func TestValidateThemeConfig_returnsCleanDeclaredValues_whenInputIsValid(t *testing.T) {
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

	got, err := ValidateThemeConfig(fields, raw)

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
	fields := []ThemeSettingField{{Key: "logo_url", Label: "Logo", Type: "url", MaxLength: 512}}

	_, err := ValidateThemeConfig(fields, map[string]any{"logo_url": "javascript:alert(1)"})

	if err == nil {
		t.Fatal("ValidateThemeConfig() error = nil, want URL validation error")
	}
}

func TestValidateThemeConfig_preservesPapertrailShape_whenLegacyValuesAreSaved(t *testing.T) {
	fields := []ThemeSettingField{
		{Key: "blog_name", Label: "博客名称", Type: "text", MaxLength: 120},
		{Key: "description", Label: "博客介绍", Type: "textarea", MaxLength: 500},
		{Key: "logo_url", Label: "Logo", Type: "url", MaxLength: 512},
		{Key: "logo_size", Label: "Logo 大小", Type: "text", MaxLength: 3},
		{Key: "buttons", Label: "按钮", Type: "group", MaxItems: 5, Fields: []ThemeSettingField{
			{Key: "label", Label: "名称", Type: "text", MaxLength: 40, Required: true},
			{Key: "url", Label: "URL", Type: "url", MaxLength: 512, Required: true},
			{Key: "icon_url", Label: "图标", Type: "url", MaxLength: 512},
		}},
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

	got, err := ValidateThemeConfig(fields, raw)

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
