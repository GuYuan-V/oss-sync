package blog

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"gorm.io/gorm"

	"github.com/helantianshen/oss-sync/internal/config"
	"github.com/helantianshen/oss-sync/internal/models"
)

func TestSupportsPublicBlog_allowsPapertrailButNotDefault(t *testing.T) {
	dataDir := t.TempDir()
	if SupportsPublicBlog(dataDir, "default") {
		t.Fatal("default theme must not support public blog")
	}
	if !SupportsPublicBlog(dataDir, "papertrail") {
		t.Fatal("papertrail theme must support public blog")
	}
}

func TestPluginThemeMetadata_declaresPublicSettings(t *testing.T) {
	dataDir := t.TempDir()
	themeDir := filepath.Join(dataDir, "themes", "theme-plugin--clean")
	if err := os.MkdirAll(themeDir, 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(themeDir, ".oss-plugin-resource.json"), []byte(`{"plugin_id":"theme-plugin","resource_id":"clean","name":"Clean"}`), 0o640); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(themeDir, "theme.json"), []byte(`{"supports_public_blog":true,"public_settings":["blog_name","description","blog_name"," "]}`), 0o640); err != nil {
		t.Fatal(err)
	}
	if !SupportsPublicBlog(dataDir, "theme-plugin--clean") {
		t.Fatal("plugin theme should support public blog")
	}
	if got := themePluginID(dataDir, "theme-plugin--clean"); got != "theme-plugin" {
		t.Fatalf("plugin ID = %q", got)
	}
	if got := publicThemeSettings(dataDir, "theme-plugin--clean"); !slices.Equal(got, []string{"blog_name", "description"}) {
		t.Fatalf("public settings = %#v", got)
	}
}

func TestPublicThemeConfig_mergesOnlyDeclaredPluginSettings(t *testing.T) {
	dataDir := t.TempDir()
	themeDir := filepath.Join(dataDir, "themes", "theme-plugin--clean")
	if err := os.MkdirAll(themeDir, 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(themeDir, ".oss-plugin-resource.json"), []byte(`{"plugin_id":"theme-plugin","resource_id":"clean","name":"Clean"}`), 0o640); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(themeDir, "theme.json"), []byte(`{"supports_public_blog":true,"public_settings":["blog_name","banner_url","private"]}`), 0o640); err != nil {
		t.Fatal(err)
	}
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })
	if err := db.AutoMigrate(&models.ServerPlugin{}, &models.VaultPluginSetting{}); err != nil {
		t.Fatal(err)
	}
	manifest := `{"id":"theme-plugin","settings":[{"key":"blog_name"}],"registration":{"settings":[{"key":"banner_url"}]},"blog_themes":[{"id":"clean"}]}`
	if err := db.Create(&models.ServerPlugin{ID: "theme-plugin", Name: "Theme plugin", Version: "1.0.0", ManifestJSON: manifest, Enabled: true}).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&models.VaultPluginSetting{
		VaultID:  "vault-1",
		PluginID: "theme-plugin",
		Config:   models.JSONMap{"blog_name": "Plugin blog", "banner_url": "/banner.svg", "private": "hidden"},
	}).Error; err != nil {
		t.Fatal(err)
	}
	h := &Handler{DB: db, Cfg: &config.Config{Storage: config.StorageConfig{DataDir: dataDir}}}
	got := h.publicThemeConfig("vault-1", "theme-plugin--clean", models.JSONMap{"blog_name": "Vault blog"})
	if got["blog_name"] != "Plugin blog" || got["banner_url"] != "/banner.svg" {
		t.Fatalf("public values = %#v", got)
	}
	if _, ok := got["private"]; ok {
		t.Fatalf("private setting leaked: %#v", got)
	}
}

func TestPublicBlogRoute_rendersOnlyWhitelistedThemePluginSettings(t *testing.T) {
	dataDir := t.TempDir()
	themeDir := filepath.Join(dataDir, "themes", "theme-plugin--clean")
	if err := os.MkdirAll(themeDir, 0o750); err != nil {
		t.Fatal(err)
	}
	files := map[string]string{
		".oss-plugin-resource.json": `{"plugin_id":"theme-plugin","resource_id":"clean","name":"Clean"}`,
		"theme.json":                `{"supports_public_blog":true,"public_settings":["blog_name","banner_url","private"]}`,
		"template.html":             `{{.BlogName}}|{{.BannerURL}}|{{.ThemeConfigJS}}`,
	}
	for name, content := range files {
		if err := os.WriteFile(filepath.Join(themeDir, name), []byte(content), 0o640); err != nil {
			t.Fatal(err)
		}
	}
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })
	if err := db.AutoMigrate(&models.Vault{}, &models.VaultSetting{}, &models.ServerPlugin{}, &models.VaultPluginSetting{}); err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&models.Vault{ID: "vault-1", OwnerID: 1, Name: "Vault"}).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&models.VaultSetting{VaultID: "vault-1", ThemeName: "theme-plugin--clean", ThemeConfig: models.JSONMap{"base_value": "Vault value"}, IsPublicBlog: true}).Error; err != nil {
		t.Fatal(err)
	}
	manifest := `{"id":"theme-plugin","settings":[{"key":"blog_name"}],"registration":{"settings":[{"key":"banner_url"}]},"blog_themes":[{"id":"clean"}]}`
	if err := db.Create(&models.ServerPlugin{ID: "theme-plugin", Name: "Theme plugin", Version: "1.0.0", ManifestJSON: manifest, Enabled: true}).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&models.VaultPluginSetting{VaultID: "vault-1", PluginID: "theme-plugin", Config: models.JSONMap{
		"blog_name": "Plugin blog", "banner_url": "/banner.svg", "private": "secret",
	}}).Error; err != nil {
		t.Fatal(err)
	}
	h, err := New(db, &config.Config{Storage: config.StorageConfig{DataDir: dataDir}})
	if err != nil {
		t.Fatal(err)
	}
	router := gin.New()
	h.Register(router)
	response := httptest.NewRecorder()
	router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/b/vault-1", nil))
	if response.Code != http.StatusOK {
		t.Fatalf("public blog status = %d, want %d: %s", response.Code, http.StatusOK, response.Body)
	}
	body := response.Body.String()
	for _, want := range []string{"Plugin blog", "/banner.svg", "Vault value"} {
		if !strings.Contains(body, want) {
			t.Errorf("public blog is missing %q: %s", want, body)
		}
	}
	if strings.Contains(body, "secret") || strings.Contains(body, `"private"`) {
		t.Fatalf("private plugin setting leaked to public theme: %s", body)
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
	cfg := ParseBlogThemeConfig(got)
	if cfg.BlogName != "Paper notes" || cfg.Description != "A notebook" || cfg.LogoURL != "/logo.svg" || cfg.LogoSize != 128 {
		t.Fatalf("parsed config = %#v", cfg)
	}
	if len(cfg.Buttons) != 1 || cfg.Buttons[0].Label != "Home" || cfg.Buttons[0].Position != 1 {
		t.Fatalf("parsed buttons = %#v", cfg.Buttons)
	}
}

func TestParseBlogThemeConfigRejectsOutOfRangeLogoSize(t *testing.T) {
	if got := ParseBlogThemeConfig(map[string]any{"logo_size": "9"}).LogoSize; got != 0 {
		t.Fatalf("logo size = %d, want 0", got)
	}
	if got := ParseBlogThemeConfig(map[string]any{"logo_size": "193"}).LogoSize; got != 0 {
		t.Fatalf("logo size = %d, want 0", got)
	}
}
