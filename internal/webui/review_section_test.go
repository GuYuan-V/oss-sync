package webui

import (
	"encoding/json"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/helantianshen/oss-sync/internal/blog"
	"github.com/helantianshen/oss-sync/internal/models"
	"github.com/helantianshen/oss-sync/internal/serverplugin"
)

// 分区保存只落当前分组的字段，其余分组的既有值必须保留
func TestReviewPR13SectionSave(t *testing.T) {
	db, cfg, _ := newWebUITestDB(t)
	if err := db.AutoMigrate(&models.ServerPlugin{}, &models.ServerPluginAssociation{}, &models.VaultPluginSetting{}); err != nil {
		t.Fatal(err)
	}
	admin := createTestUserWithHash(t, db, "section-admin", "admin")
	vault := models.Vault{ID: "vault-section", OwnerID: admin.ID, Name: "Section Vault"}
	if err := db.Create(&vault).Error; err != nil {
		t.Fatal(err)
	}
	manifest := serverplugin.Manifest{
		ID: "section-world", Name: "Section world", Version: "1.0.0", APIVersion: serverplugin.CurrentAPIVersion,
		Settings: []blog.ThemeSettingField{
			{Key: "first", Label: "First", Type: "text", MaxLength: 200, Section: "a"},
			{Key: "second", Label: "Second", Type: "text", MaxLength: 200, Section: "b", Required: true},
		},
	}
	manifestJSON, err := json.Marshal(manifest)
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&models.ServerPlugin{
		ID: manifest.ID, Name: manifest.Name, Version: manifest.Version, APIVersion: manifest.APIVersion,
		ManifestJSON: string(manifestJSON), ManifestHash: "manifest", WasmHash: "wasm", WasmSize: 1, Enabled: true,
	}).Error; err != nil {
		t.Fatal(err)
	}
	h, err := New(db, cfg)
	if err != nil {
		t.Fatal(err)
	}
	session, csrf := issueWebSession(t, cfg, admin)
	path := "/dashboard/plugins/" + manifest.ID + "/settings"

	// 首次提交完整配置，两个分组的字段都落库
	post := doWebRequest(t, h, http.MethodPost, path, url.Values{
		"_csrf":          {csrf},
		"vault_id":       {vault.ID},
		"setting_first":  {"alpha"},
		"setting_second": {"beta"},
	}, session, csrf, false)
	if post.Code != http.StatusSeeOther {
		t.Fatalf("initial save = %d %s", post.Code, post.Body.String())
	}

	// 只提交 a 分组，b 分组的必填字段既未被展示也不在表单中，其既有值必须保留
	post = doWebRequest(t, h, http.MethodPost, path, url.Values{
		"_csrf":         {csrf},
		"vault_id":      {vault.ID},
		"section":       {"a"},
		"setting_first": {"updated"},
	}, session, csrf, false)
	if post.Code != http.StatusSeeOther {
		t.Fatalf("section save = %d %s", post.Code, post.Body.String())
	}

	var setting models.VaultPluginSetting
	if err := db.Where("vault_id = ? AND plugin_id = ?", vault.ID, manifest.ID).First(&setting).Error; err != nil {
		t.Fatal(err)
	}
	if got := setting.Config["first"]; got != "updated" {
		t.Errorf("first = %v, want updated", got)
	}
	if got := setting.Config["second"]; got != "beta" {
		t.Errorf("second = %v, want beta preserved", got)
	}
}

// 分区保存成功后仍停留在该分区，便于继续编辑
func TestReviewPR13SectionRedirect(t *testing.T) {
	db, cfg, _ := newWebUITestDB(t)
	if err := db.AutoMigrate(&models.ServerPlugin{}, &models.ServerPluginAssociation{}, &models.VaultPluginSetting{}); err != nil {
		t.Fatal(err)
	}
	admin := createTestUserWithHash(t, db, "section-redirect-admin", "admin")
	vault := models.Vault{ID: "vault-section-redirect", OwnerID: admin.ID, Name: "Redirect Vault"}
	if err := db.Create(&vault).Error; err != nil {
		t.Fatal(err)
	}
	manifest := serverplugin.Manifest{
		ID: "section-redirect", Name: "Section redirect", Version: "1.0.0", APIVersion: serverplugin.CurrentAPIVersion,
		Settings: []blog.ThemeSettingField{
			{Key: "only", Label: "Only", Type: "text", MaxLength: 200, Section: "a"},
		},
	}
	manifestJSON, err := json.Marshal(manifest)
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&models.ServerPlugin{
		ID: manifest.ID, Name: manifest.Name, Version: manifest.Version, APIVersion: manifest.APIVersion,
		ManifestJSON: string(manifestJSON), ManifestHash: "manifest", WasmHash: "wasm", WasmSize: 1, Enabled: true,
	}).Error; err != nil {
		t.Fatal(err)
	}
	h, err := New(db, cfg)
	if err != nil {
		t.Fatal(err)
	}
	session, csrf := issueWebSession(t, cfg, admin)
	post := doWebRequest(t, h, http.MethodPost, "/dashboard/plugins/"+manifest.ID+"/settings", url.Values{
		"_csrf":         {csrf},
		"vault_id":      {vault.ID},
		"section":       {"a"},
		"setting_only":  {"value"},
	}, session, csrf, false)
	if post.Code != http.StatusSeeOther {
		t.Fatalf("save = %d %s", post.Code, post.Body.String())
	}
	location := post.Header().Get("Location")
	if !strings.Contains(location, "section=a") {
		t.Errorf("redirect %q should keep section=a", location)
	}
}
