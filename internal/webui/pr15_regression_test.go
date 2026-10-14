package webui

import (
	"errors"
	"github.com/helantianshen/oss-sync/internal/models"
	"gorm.io/gorm"
	"net/http"
	"net/url"
	"testing"
)

func TestBuiltinThemeSettingsSaveIsAtomic(t *testing.T) {
	for _, failSync := range []bool{false, true} {
		t.Run(map[bool]string{false: "preserve foreign config", true: "rollback on sync failure"}[failSync], func(t *testing.T) {
			db, cfg, _ := newWebUITestDB(t)
			if err := db.AutoMigrate(&models.VaultPluginSetting{}); err != nil {
				t.Fatal(err)
			}
			user := createTestUserWithHash(t, db, "pr15-owner", "user")
			vault := models.Vault{ID: "pr15-vault", OwnerID: user.ID, Name: "blog"}
			if err := db.Create(&vault).Error; err != nil {
				t.Fatal(err)
			}
			if err := db.Create(&models.VaultSetting{VaultID: vault.ID, ThemeName: "papertrail", ThemeConfig: models.JSONMap{"foreign": "keep"}}).Error; err != nil {
				t.Fatal(err)
			}
			h, err := New(db, cfg)
			if err != nil {
				t.Fatal(err)
			}
			if failSync {
				if err := db.Callback().Update().Before("gorm:update").Register("pr15_fail_sync", func(tx *gorm.DB) {
					if tx.Statement.Table == "vault_settings" {
						tx.AddError(errors.New("injected sync failure"))
					}
				}); err != nil {
					t.Fatal(err)
				}
			}
			session, csrf := issueWebSession(t, cfg, user)
			response := doWebRequest(t, h, http.MethodPost, "/dashboard/plugins/papertrail-settings/settings", url.Values{"_csrf": {csrf}, "vault_id": {vault.ID}, "setting_blog_name": {"new name"}}, session, csrf, false)
			location, err := url.Parse(response.Header().Get("Location"))
			if err != nil {
				t.Fatal(err)
			}
			if response.Code != http.StatusSeeOther {
				t.Fatalf("status %d", response.Code)
			}
			var theme models.VaultSetting
			if err := db.First(&theme, "vault_id = ?", vault.ID).Error; err != nil {
				t.Fatal(err)
			}
			if theme.ThemeConfig["foreign"] != "keep" {
				t.Fatal("foreign key lost")
			}
			var count int64
			if err := db.Model(&models.VaultPluginSetting{}).Where("vault_id = ?", vault.ID).Count(&count).Error; err != nil {
				t.Fatal(err)
			}
			if failSync {
				if location.Query().Get("saved") == "1" || location.Query().Get("error") == "" {
					t.Fatalf("sync failure reported as success: %s", location)
				}
				if count != 0 {
					t.Fatal("plugin config was not rolled back")
				}
			} else {
				if location.Query().Get("saved") != "1" || count != 1 || theme.ThemeConfig["blog_name"] != "new name" {
					t.Fatalf("save failed: %s %v", location, theme.ThemeConfig)
				}
			}
		})
	}
}
