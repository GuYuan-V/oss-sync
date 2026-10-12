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

// 按 key 合并设置字段：manifest 的下拉声明不能被 Go 侧漏字段的 registration 覆盖
func TestMergeSettingFieldsPreservesManifestChoices(t *testing.T) {
	base := []blog.ThemeSettingField{
		{Key: "theme", Label: "主题", Type: "select", Choices: []string{"light", "dark"}, Placeholder: "选择主题"},
		{Key: "title", Label: "标题", Type: "text", MaxLength: 64, Required: true},
		{Key: "legacy", Label: "旧字段", Type: "text"},
	}
	// Go 侧只声明了 key 与 label，其余字段为零值
	override := []blog.ThemeSettingField{
		{Key: "theme", Label: "主题模式"},
		{Key: "extra", Label: "新增字段", Type: "text"},
	}

	merged := mergeSettingFields(base, override)
	byKey := map[string]blog.ThemeSettingField{}
	for _, f := range merged {
		byKey[f.Key] = f
	}

	if len(merged) != 4 {
		t.Fatalf("merged count = %d, want 4", len(merged))
	}
	// 同名键的 Choices 与 Placeholder 必须保留，否则下拉框退化成输入框
	if got := byKey["theme"].Choices; len(got) != 2 || got[0] != "light" || got[1] != "dark" {
		t.Errorf("theme choices = %v, want [light dark]", got)
	}
	if got := byKey["theme"].Placeholder; got != "选择主题" {
		t.Errorf("theme placeholder = %q, want 选择主题", got)
	}
	if got := byKey["theme"].Label; got != "主题模式" {
		t.Errorf("theme label = %q, want 主题模式 from override", got)
	}
	// override 未声明的字段保持 base 原值
	if got := byKey["title"].MaxLength; got != 64 {
		t.Errorf("title max length = %d, want 64", got)
	}
	if !byKey["title"].Required {
		t.Error("title required should be preserved")
	}
	// base 独有键与 override 新增键都保留
	if _, ok := byKey["legacy"]; !ok {
		t.Error("base-only key legacy should be kept")
	}
	if _, ok := byKey["extra"]; !ok {
		t.Error("override-only key extra should be appended")
	}
}

// 嵌套 group 的子字段按同一规则递归合并
func TestMergeSettingFieldsNestedGroup(t *testing.T) {
	base := []blog.ThemeSettingField{
		{
			Key: "group", Label: "分组", Type: "group", MaxItems: 5,
			Fields: []blog.ThemeSettingField{
				{Key: "inner", Label: "内层", Type: "select", Choices: []string{"a", "b"}},
				{Key: "keep", Label: "保留", Type: "text"},
			},
		},
	}
	override := []blog.ThemeSettingField{
		{
			Key: "group",
			Fields: []blog.ThemeSettingField{
				{Key: "inner", Label: "内层改"},
			},
		},
	}

	merged := mergeSettingFields(base, override)
	if len(merged) != 1 {
		t.Fatalf("merged count = %d, want 1", len(merged))
	}
	group := merged[0]
	if group.MaxItems != 5 {
		t.Errorf("group max items = %d, want 5", group.MaxItems)
	}
	if len(group.Fields) != 2 {
		t.Fatalf("nested fields = %d, want 2", len(group.Fields))
	}
	inner := group.Fields[0]
	if inner.Label != "内层改" {
		t.Errorf("inner label = %q, want 内层改", inner.Label)
	}
	if len(inner.Choices) != 2 {
		t.Errorf("inner choices = %v, want [a b] preserved", inner.Choices)
	}
	if group.Fields[1].Key != "keep" {
		t.Errorf("nested base-only field = %q, want keep", group.Fields[1].Key)
	}
}
