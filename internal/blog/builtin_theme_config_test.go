package blog

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"

	"github.com/helantianshen/oss-sync/internal/config"
	"github.com/helantianshen/oss-sync/internal/models"
)

// 内置主题必须按自身声明的键过滤 VaultSetting.ThemeConfig
// 该字段是 vault 级且与主题无关，切换主题时其他主题写入的配置不得残留
func TestFilterBuiltinThemeConfigDropsForeignKeys(t *testing.T) {
	manifest := `{"id":"papertrail-settings","name":"Papertrail","version":"1.0.0","settings":[
		{"key":"blog_name"},{"key":"description"},{"key":"logo_url"},{"key":"buttons"},
		{"key":"banner_url"},{"key":"mobile_banner_url"}]}`
	h := newBuiltinThemeConfigHandler(t, manifest)

	base := models.JSONMap{
		"blog_name":     "我的博客",
		"buttons":       []any{map[string]any{"text": "B站", "url": "https://b23.tv"}},
		"foreign_theme": "来自其他主题的配置",
		"lolimeow_only": map[string]any{"k": "v"},
	}
	filtered := h.filterBuiltinThemeConfig("vault-1", base)

	if _, ok := filtered["foreign_theme"]; ok {
		t.Error("undeclared key must be dropped")
	}
	if _, ok := filtered["lolimeow_only"]; ok {
		t.Error("other theme's key must be dropped")
	}
	if filtered["blog_name"] != "我的博客" {
		t.Errorf("blog_name = %v, want 我的博客", filtered["blog_name"])
	}
	// buttons 是 papertrail 自己声明的键，必须保留
	if _, ok := filtered["buttons"]; !ok {
		t.Error("declared key buttons must be preserved")
	}
}

// 未注入声明键提供者时不过滤 base，避免公开博客因缺失白名单而空白
func TestFilterBuiltinThemeConfigKeepsBaseWithoutProvider(t *testing.T) {
	db := newBuiltinThemeConfigDB(t, `{"id":"papertrail-settings","settings":[{"key":"blog_name"}]}`)
	h := &Handler{DB: db}
	base := models.JSONMap{"blog_name": "x", "foreign": "y"}
	filtered := h.filterBuiltinThemeConfig("vault-1", base)
	if len(filtered) != 2 {
		t.Errorf("filtered = %v, want base unchanged without provider", filtered)
	}
}

// 空 base 与非内置主题路径不触发过滤
func TestFilterBuiltinThemeConfigEmptyBase(t *testing.T) {
	h := newBuiltinThemeConfigHandler(t, `{"id":"papertrail-settings","settings":[{"key":"blog_name"}]}`)
	// 空 base 得到空配置
	if got := h.filterBuiltinThemeConfig("vault-1", nil); len(got) != 0 {
		t.Errorf("nil base = %v, want empty", got)
	}
	if got := h.filterBuiltinThemeConfig("vault-1", models.JSONMap{}); len(got) != 0 {
		t.Errorf("empty base = %v, want empty", got)
	}
}

// 公开博客读取配置时，内置主题同样经过键过滤
func TestPublicThemeConfigFiltersBuiltinTheme(t *testing.T) {
	manifest := `{"id":"papertrail-settings","settings":[{"key":"blog_name"},{"key":"buttons"}]}`
	h := newBuiltinThemeConfigHandler(t, manifest)
	h.Cfg = newThemeConfigHandlerCfg(t)

	base := models.JSONMap{"blog_name": "公开博客", "buttons": []any{}, "foreign": "x"}
	got := h.publicThemeConfig("vault-1", "papertrail", base)
	if _, ok := got["foreign"]; ok {
		t.Error("public config must not expose keys the theme does not declare")
	}
	if got["blog_name"] != "公开博客" {
		t.Errorf("blog_name = %v, want 公开博客", got)
	}
}

// newBuiltinThemeConfigDB 建内存库并写入 papertrail-settings 插件记录
func newBuiltinThemeConfigDB(t *testing.T, manifest string) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatal(err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })
	if err := db.AutoMigrate(&models.ServerPlugin{}); err != nil {
		t.Fatal(err)
	}
	plugin := models.ServerPlugin{
		ID: "papertrail-settings", Name: "Papertrail", Version: "1.0.0",
		Enabled: true, ManifestJSON: manifest,
	}
	if err := db.Create(&plugin).Error; err != nil {
		t.Fatal(err)
	}
	return db
}

// newBuiltinThemeConfigHandler 建带声明键提供者的 Handler
// 提供者从 manifest 解析键集合，模拟宿主在 router 中的注入
func newBuiltinThemeConfigHandler(t *testing.T, manifest string) *Handler {
	t.Helper()
	db := newBuiltinThemeConfigDB(t, manifest)
	var fields []blogThemeSettingFieldStub
	if err := json.Unmarshal([]byte(manifest), &struct {
		Settings *[]blogThemeSettingFieldStub `json:"settings"`
	}{Settings: &fields}); err != nil {
		t.Fatal(err)
	}
	keys := make(map[string]struct{}, len(fields))
	for _, field := range fields {
		if field.Key != "" {
			keys[field.Key] = struct{}{}
		}
	}
	h := &Handler{DB: db}
	h.SetBuiltinSettingKeysProvider(func() map[string]struct{} { return keys })
	return h
}

type blogThemeSettingFieldStub struct {
	Key string `json:"key"`
}

// newThemeConfigHandlerCfg 给出指向临时数据目录的配置，使内置主题判定不落空
func newThemeConfigHandlerCfg(t *testing.T) *config.Config {
	t.Helper()
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "themes"), 0o750); err != nil {
		t.Fatal(err)
	}
	cfg := &config.Config{}
	cfg.Storage.DataDir = dir
	return cfg
}

// 内置主题的配置主存储是 VaultPluginSetting，公开博客必须读到它而非只读 base
func TestFilterBuiltinThemeConfigReadsPluginSetting(t *testing.T) {
	manifest := `{"id":"papertrail-settings","settings":[{"key":"blog_name"},{"key":"buttons"}]}`
	h := newBuiltinThemeConfigHandler(t, manifest)
	if err := h.DB.AutoMigrate(&models.VaultPluginSetting{}); err != nil {
		t.Fatal(err)
	}
	// 配置经 syncBuiltinThemeConfig 同步进 base 后，过滤不应把它剔掉
	base := models.JSONMap{"blog_name": "来自插件存储", "buttons": []any{}, "foreign": "x"}
	got := h.filterBuiltinThemeConfig("vault-1", base)
	if got["blog_name"] != "来自插件存储" {
		t.Errorf("blog_name = %v, want 来自插件存储", got["blog_name"])
	}
	if _, ok := got["foreign"]; ok {
		t.Error("undeclared key must still be dropped")
	}
}
