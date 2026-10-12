package serverplugin

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"

	"github.com/helantianshen/oss-sync/internal/blog"
	"github.com/helantianshen/oss-sync/internal/models"
)

// TestLolimeowPackageInstallsAndEnables 用宿主真实安装链路验证 LoliMeow 插件包
func TestLolimeowPackageInstallsAndEnables(t *testing.T) {
	zipPath := os.Getenv("LOLIMEOW_PLUGIN_ZIP")
	if zipPath == "" {
		t.Skip("LOLIMEOW_PLUGIN_ZIP not set")
	}
	raw, err := os.ReadFile(zipPath)
	if err != nil {
		t.Fatalf("read plugin zip: %v", err)
	}

	db, err := gorm.Open(sqlite.Open(t.TempDir()+"/plugins.db"), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatal(err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })
	if err := db.AutoMigrate(&models.ServerPlugin{}, &models.ServerPluginMigration{}); err != nil {
		t.Fatal(err)
	}

	ctx := context.Background()
	dataDir := t.TempDir()
	manager, err := NewManager(ctx, db, dataDir)
	if err != nil {
		t.Fatalf("new manager: %v", err)
	}
	t.Cleanup(func() { _ = manager.Close(ctx) })

	installed, err := manager.Install(ctx, bytes.NewReader(raw), int64(len(raw)))
	if err != nil {
		t.Fatalf("install plugin: %v", err)
	}
	if installed.ID != "lolimeow" {
		t.Fatalf("plugin id = %q", installed.ID)
	}
	t.Logf("installed id=%s name=%s version=%s runtime=%s", installed.ID, installed.Name, installed.Version, installed.Runtime)

	if err := manager.Enable(ctx, installed.ID); err != nil {
		t.Fatalf("enable plugin: %v", err)
	}

	registration, ok := manager.RegistrationFor("lolimeow")
	if !ok {
		t.Fatal("registration missing after enable")
	}
	if len(registration.Hooks) != 1 || registration.Hooks[0].Name != "blog.data" {
		t.Fatalf("hooks = %+v", registration.Hooks)
	}
	if len(registration.AdminPages) != 1 || registration.AdminPages[0].Slug != "theme" {
		t.Fatalf("admin pages = %+v", registration.AdminPages)
	}
	// Go 侧不得声明 settings：SDK 的 SettingField 没有 choices，
	// 一旦注册就会覆盖 manifest.registration.settings 使下拉退化为输入框
	if len(registration.Settings) != 0 {
		t.Fatalf("registration must not declare settings, got %d", len(registration.Settings))
	}

	// 设置声明来自 manifest，且保留 choices 与默认选项
	var record models.ServerPlugin
	if err := db.Where("id = ?", "lolimeow").First(&record).Error; err != nil {
		t.Fatalf("load installed record: %v", err)
	}
	installedManifest, err := ParseManifest([]byte(record.ManifestJSON))
	if err != nil {
		t.Fatalf("parse installed manifest: %v", err)
	}
	if len(installedManifest.Settings) != 47 {
		t.Fatalf("manifest settings = %d, want 47", len(installedManifest.Settings))
	}
	choiceCount := 0
	for _, field := range installedManifest.Registration.Settings {
		if field.Type == "choice" {
			choiceCount++
			if len(field.Choices) < 2 {
				t.Fatalf("choice field %s has %d options", field.Key, len(field.Choices))
			}
		}
	}
	if choiceCount != 15 {
		t.Fatalf("choice fields = %d, want 15", choiceCount)
	}

	// blog.data 钩子可被调用并返回配置
	data, err := manager.ApplyHookData(ctx, "blog.data", blog.PluginDataPayload{
		VaultID: "vault-test",
		Path:    "Posts/a.md",
	})
	if err != nil {
		t.Fatalf("apply blog.data: %v", err)
	}
	entry, ok := data["lolimeow"].(map[string]any)
	if !ok {
		t.Fatalf("plugin data entry = %T", data["lolimeow"])
	}
	if _, ok := entry["config"].(map[string]any); !ok {
		t.Fatalf("config missing in blog.data result: %+v", entry)
	}

	// 管理页渲染出控制台片段
	content, err := manager.RenderAdminPage(ctx, "lolimeow", "theme")
	if err != nil {
		t.Fatalf("render admin page: %v", err)
	}
	if content == "" {
		t.Fatal("admin page content is empty")
	}

	// 停用后注册信息消失，主题资源被清理
	if err := manager.Disable(ctx, "lolimeow"); err != nil {
		t.Fatalf("disable plugin: %v", err)
	}
	if _, ok := manager.RegistrationFor("lolimeow"); ok {
		t.Fatal("registration must disappear after disable")
	}
	themeDir := filepath.Join(dataDir, "themes", "lolimeow--lolimeow")
	if _, err := os.Stat(themeDir); !os.IsNotExist(err) {
		t.Fatalf("theme resource must be removed on disable: %v", err)
	}

	// 重新启用：进程可重启，资源重新物化
	if err := manager.Enable(ctx, "lolimeow"); err != nil {
		t.Fatalf("re-enable plugin: %v", err)
	}
	if _, err := os.Stat(filepath.Join(themeDir, "template.html")); err != nil {
		t.Fatalf("theme template not re-materialized: %v", err)
	}
}
