package webui

import (
	"github.com/gin-gonic/gin"
	"github.com/helantianshen/oss-sync/internal/config"
	"github.com/helantianshen/oss-sync/internal/consoletheme"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/glebarez/sqlite"
	"github.com/helantianshen/oss-sync/internal/models"
	"gorm.io/gorm"
)

// TestBuiltinSettingFallback 保证切换模板后首次打开设置能读取博客正在使用的值，并且写入不越过 Vault
func TestBuiltinSettingFallback(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "settings.db")), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	sqlDB, _ := db.DB()
	t.Cleanup(func() { _ = sqlDB.Close() })
	if err := db.AutoMigrate(&models.VaultSetting{}, &models.VaultPluginSetting{}, &models.ServerPlugin{}); err != nil {
		t.Fatal(err)
	}
	h := &Handler{DB: db}
	for pluginID, theme := range builtinPluginThemes {
		setting := models.VaultSetting{VaultID: theme, ThemeName: theme, ThemeConfig: models.JSONMap{"blog_name": "Existing blog", "unrelated": "keep"}}
		if err := db.Create(&setting).Error; err != nil {
			t.Fatal(err)
		}
		_, values, err := h.loadPluginSettings(pluginID, theme)
		if err != nil || values["blog_name"] != "Existing blog" {
			t.Errorf("%s: config=%v, err=%v", pluginID, values, err)
		}
		if err := db.Transaction(func(tx *gorm.DB) error {
			return h.syncBuiltinThemeConfig(tx, theme, pluginID, models.JSONMap{"blog_name": theme})
		}); err != nil {
			t.Fatal(err)
		}
		if err := db.First(&setting, "vault_id = ?", theme).Error; err != nil {
			t.Fatal(err)
		}
		if setting.ThemeConfig["blog_name"] != theme || setting.ThemeConfig["unrelated"] != "keep" {
			t.Fatal(setting.ThemeConfig)
		}
	}
}

// TestBuiltinConsoleAssets 检查内置皮肤在真实静态资源处理器中的类型声明和路径边界
func TestBuiltinConsoleAssets(t *testing.T) {
	h := &Handler{Cfg: &config.Config{Storage: config.StorageConfig{DataDir: t.TempDir()}}}
	for _, name := range consoletheme.BuiltinNames {
		t.Run(name, func(t *testing.T) {
			w := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(w)
			c.Request = httptest.NewRequest("GET", "/ui/themes/"+name+"/theme.css", nil)
			c.Params = gin.Params{{Key: "theme", Value: name}, {Key: "filepath", Value: "/theme.css"}}
			h.consoleThemeAsset(c)
			if w.Code != 200 || w.Header().Get("Content-Type") != "text/css; charset=utf-8" || w.Header().Get("X-Content-Type-Options") != "nosniff" || w.Body.Len() == 0 {
				t.Fatalf("%d: %v", w.Code, w.Header())
			}
			if _, ok := consoletheme.ReadBuiltinAsset(name, "../default/theme.css"); ok {
				t.Fatal("path traversal accepted")
			}
			copied, err := consoletheme.Scaffold(h.Cfg.Storage.DataDir, name, name+"-copy")
			if err != nil {
				t.Fatal(err)
			}
			css, err := os.ReadFile(filepath.Join(copied, "theme.css"))
			if err != nil || string(css) != w.Body.String() {
				t.Fatalf("copy differs from served resource: %v", err)
			}
		})
	}
}
