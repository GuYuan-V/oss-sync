package webui

import (
	"github.com/helantianshen/oss-sync/internal/models"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

func TestReviewPapertrailSettingsPreservePublicBlog(t *testing.T) {
	db, cfg, _ := newWebUITestDB(t)
	user := createTestUserWithHash(t, db, "review-blog-owner", "user")
	vault := models.Vault{ID: "review-public-blog", OwnerID: user.ID, Name: "Review blog"}
	if e := db.Create(&vault).Error; e != nil {
		t.Fatal(e)
	}
	if e := db.Create(&models.VaultSetting{VaultID: vault.ID, ThemeName: "papertrail", IsPublicBlog: true}).Error; e != nil {
		t.Fatal(e)
	}
	h, e := New(db, cfg)
	if e != nil {
		t.Fatal(e)
	}
	session, csrf := issueWebSession(t, cfg, user)
	get := doWebRequest(t, h, http.MethodGet, "/dashboard/vaults/"+vault.ID+"/settings", nil, session, csrf, false)
	if get.Code != 200 {
		t.Fatalf("GET status %d", get.Code)
	}
	field := regexp.MustCompile(`<input[^>]*name="is_public_blog"[^>]*>`).FindString(get.Body.String())
	if !strings.Contains(field, "checked") {
		t.Errorf("enabled Papertrail blog rendered unchecked: %s", field)
	}
	// Submit the checkbox state returned by the settings page, as a browser would.
	form := url.Values{"_csrf": {csrf}, "theme_name": {"papertrail"}, "recycle_bin_days": {"30"}}
	if strings.Contains(field, "checked") {
		form.Set("is_public_blog", "on")
	}
	post := doWebRequest(t, h, http.MethodPost, "/dashboard/vaults/"+vault.ID+"/settings", form, session, csrf, false)
	if post.Code != 303 {
		t.Fatalf("POST status %d", post.Code)
	}
	var saved models.VaultSetting
	db.First(&saved, "vault_id = ?", vault.ID)
	if !saved.IsPublicBlog {
		t.Error("saving unrelated retention setting silently disabled public blog")
	}
}

func TestReviewPluginSettingsRedirectPreservesVault(t *testing.T) {
	db, cfg, _ := newWebUITestDB(t)
	db.AutoMigrate(&models.VaultPluginSetting{})
	user := createTestUserWithHash(t, db, "review-settings-owner", "user")
	vault := models.Vault{ID: "review-vault", OwnerID: user.ID, Name: "Review vault"}
	db.Create(&vault)
	db.Create(&models.VaultSetting{VaultID: vault.ID, ThemeName: "papertrail"})
	h, e := New(db, cfg)
	if e != nil {
		t.Fatal(e)
	}
	session, csrf := issueWebSession(t, cfg, user)
	form := url.Values{"_csrf": {csrf}, "vault_id": {vault.ID}}
	post := doWebRequest(t, h, http.MethodPost, "/dashboard/plugins/papertrail-settings/settings", form, session, csrf, false)
	location := post.Header().Get("Location")
	parsed, e := url.Parse(location)
	if e != nil {
		t.Fatal(e)
	}
	if post.Code != 303 || parsed.Query().Get("vault_id") != vault.ID || parsed.Query().Get("saved") != "1" {
		t.Fatalf("broken redirect: %d %s, parsed vault_id=%q saved=%q", post.Code, location, parsed.Query().Get("vault_id"), parsed.Query().Get("saved"))
	}
}

func TestReviewLegacyVaultBackupRemainsDownloadable(t *testing.T) {
	db, cfg, _ := newWebUITestDB(t)
	db.AutoMigrate(&models.VaultBackup{})
	user := createTestUserWithHash(t, db, "review-backup-admin", "admin")
	t.Chdir(t.TempDir())
	legacyPath := filepath.Join("backups", "vaults", "old-vault.zip")
	os.MkdirAll(filepath.Dir(legacyPath), 0700)
	os.WriteFile(legacyPath, []byte("legacy backup fixture"), 0600)
	db.Create(&models.VaultBackup{ID: "legacy-backup", VaultID: "deleted-vault", OwnerID: user.ID, VaultName: "Deleted vault", FileName: "old-vault.zip"})
	h, e := New(db, cfg)
	if e != nil {
		t.Fatal(e)
	}
	session, csrf := issueWebSession(t, cfg, user)
	response := doWebRequest(t, h, http.MethodGet, "/dashboard/admin/backups/legacy-backup/download", nil, session, csrf, false)
	if response.Code != 200 {
		t.Fatalf("existing pre-upgrade backup unavailable: status=%d body=%s", response.Code, response.Body.String())
	}
	deleteResponse := doWebRequest(t, h, http.MethodPost, "/dashboard/admin/backups/legacy-backup/delete", url.Values{"_csrf": {csrf}}, session, csrf, false)
	if deleteResponse.Code != http.StatusSeeOther {
		t.Fatalf("legacy delete returned %d: %s", deleteResponse.Code, deleteResponse.Body.String())
	}
	if _, err := os.Stat(legacyPath); !os.IsNotExist(err) {
		t.Fatalf("legacy archive not removed: %v", err)
	}
	var count int64
	if err := db.Model(&models.VaultBackup{}).Where("id = ?", "legacy-backup").Count(&count).Error; err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Fatal("deleted backup record remains")
	}

}
