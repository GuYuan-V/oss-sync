package webui

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	"github.com/helantianshen/oss-sync/internal/blog"
	"github.com/helantianshen/oss-sync/internal/models"
	"github.com/helantianshen/oss-sync/internal/serverplugin"
)

func TestAdminPluginsTemplateContainsLifecycleControls(t *testing.T) {
	raw, err := webFS.ReadFile("templates/admin_plugins.html")
	if err != nil {
		t.Fatal(err)
	}
	page := string(raw)
	for _, want := range []string{
		`action="/dashboard/admin/plugins/upload"`,
		`enctype="multipart/form-data"`,
		`/dashboard/admin/plugins/{{.ID}}/enable`,
		`/dashboard/admin/plugins/{{.ID}}/disable`,
		`/dashboard/admin/plugins/{{.ID}}/delete`,
		`name="file"`,
		`data-modal-open="plugin-guide"`,
		`{{.Data.GuideHTML}}`,
	} {
		if !strings.Contains(page, want) {
			t.Fatalf("admin plugin template missing %q", want)
		}
	}
}

func TestVaultPluginSettingsHTTPPersistsPerVaultValues(t *testing.T) {
	db, cfg, _ := newWebUITestDB(t)
	if err := db.AutoMigrate(&models.ServerPlugin{}, &models.ServerPluginAssociation{}, &models.VaultPluginSetting{}); err != nil {
		t.Fatal(err)
	}
	admin := createTestUserWithHash(t, db, "settings-admin", "admin")
	vault := models.Vault{ID: "vault-plugin-settings", OwnerID: admin.ID, Name: "Settings Vault"}
	if err := db.Create(&vault).Error; err != nil {
		t.Fatal(err)
	}
	manifest := serverplugin.Manifest{
		ID: "settings-world", Name: "Settings world", Version: "1.0.0", APIVersion: serverplugin.CurrentAPIVersion,
		Routes:   []serverplugin.RouteSpec{{Method: http.MethodGet, Path: "/hello", Public: true}},
		Settings: []blog.ThemeSettingField{{Key: "endpoint", Label: "Endpoint", Type: "url", MaxLength: 500, Required: true}},
	}
	manifestJSON, err := json.Marshal(manifest)
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&models.ServerPlugin{ID: manifest.ID, Name: manifest.Name, Version: manifest.Version, APIVersion: manifest.APIVersion, ManifestJSON: string(manifestJSON), ManifestHash: "manifest", WasmHash: "wasm", WasmSize: 1, Enabled: true}).Error; err != nil {
		t.Fatal(err)
	}
	h, err := New(db, cfg)
	if err != nil {
		t.Fatal(err)
	}
	session, csrf := issueWebSession(t, cfg, admin)

	get := doWebRequest(t, h, http.MethodGet, "/dashboard/plugins/"+manifest.ID+"/settings?vault_id="+vault.ID, nil, session, csrf, false)
	if get.Code != http.StatusOK || !strings.Contains(get.Body.String(), "Endpoint") || !strings.Contains(get.Body.String(), "Settings world") {
		t.Fatalf("plugin settings page = %d %s", get.Code, get.Body.String())
	}
	form := url.Values{"_csrf": {csrf}, "setting_endpoint": {"https://example.com/api"}, "vault_id": {vault.ID}}
	post := doWebRequest(t, h, http.MethodPost, "/dashboard/plugins/"+manifest.ID+"/settings", form, session, csrf, false)
	if post.Code != http.StatusSeeOther || !strings.Contains(post.Header().Get("Location"), "saved=1") {
		t.Fatalf("save plugin settings = %d %q", post.Code, post.Header().Get("Location"))
	}
	var setting models.VaultPluginSetting
	if err := db.Where("vault_id = ? AND plugin_id = ?", vault.ID, manifest.ID).First(&setting).Error; err != nil {
		t.Fatal(err)
	}
	if setting.Config["endpoint"] != "https://example.com/api" {
		t.Fatalf("stored plugin settings = %#v", setting.Config)
	}
}

func TestGlobalNavigationShowsPapertrailForAccessibleVault(t *testing.T) {
	db, cfg, _ := newWebUITestDB(t)
	admin := createTestUserWithHash(t, db, "papertrail-nav", "user")
	vault := models.Vault{ID: "vault-papertrail-nav", OwnerID: admin.ID, Name: "Papertrail Vault"}
	if err := db.Create(&vault).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&models.VaultSetting{VaultID: vault.ID, ThemeName: "papertrail"}).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&models.VaultMember{}, &models.VaultPluginSetting{}); err != nil {
		t.Fatal(err)
	}
	h, err := New(db, cfg)
	if err != nil {
		t.Fatal(err)
	}
	ld := layoutData{}
	h.setPluginNavigationForUser(&ld, admin)
	if len(ld.PluginSettings) != 1 || ld.PluginSettings[0].ID != "papertrail-settings" {
		t.Fatalf("global plugin navigation = %#v, want Papertrail settings", ld.PluginSettings)
	}

	session, csrf := issueWebSession(t, cfg, admin)
	get := doWebRequest(t, h, http.MethodGet, "/dashboard/plugins/papertrail-settings/settings", nil, session, csrf, false)
	if get.Code != http.StatusOK || !strings.Contains(get.Body.String(), vault.Name) {
		t.Fatalf("global Papertrail settings page = %d %s", get.Code, get.Body.String())
	}
}

func TestGlobalNavigationHidesPapertrailWithoutSelectedVault(t *testing.T) {
	db, cfg, _ := newWebUITestDB(t)
	user := createTestUserWithHash(t, db, "papertrail-no-vault", "user")
	h, err := New(db, cfg)
	if err != nil {
		t.Fatal(err)
	}
	ld := layoutData{}
	h.setPluginNavigationForUser(&ld, user)
	if len(ld.PluginSettings) != 0 {
		t.Fatalf("navigation without a Vault = %#v, want no Papertrail entry", ld.PluginSettings)
	}
}

func TestAdminPluginsHTTPUploadAndLifecycle(t *testing.T) {
	db, cfg, dataDir := newWebUITestDB(t)
	if err := db.AutoMigrate(&models.ServerPlugin{}, &models.ServerPluginAssociation{}); err != nil {
		t.Fatal(err)
	}
	manager, err := serverplugin.NewManager(t.Context(), db, dataDir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := manager.Close(t.Context()); err != nil {
			t.Errorf("close plugin manager: %v", err)
		}
	})
	h, err := New(db, cfg)
	if err != nil {
		t.Fatal(err)
	}
	h.SetPluginManager(manager)

	admin := createTestUserWithHash(t, db, "plugin-admin", "admin")
	session, csrf := issueWebSession(t, cfg, admin)
	archiveBytes := makeAdminPluginArchive(t)

	var uploadBody bytes.Buffer
	upload := multipart.NewWriter(&uploadBody)
	part, err := upload.CreateFormFile("file", "hello-world.zip")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := part.Write(archiveBytes); err != nil {
		t.Fatal(err)
	}
	if err := upload.WriteField("_csrf", csrf); err != nil {
		t.Fatal(err)
	}
	if err := upload.Close(); err != nil {
		t.Fatal(err)
	}
	uploadContentType := upload.FormDataContentType()
	request := httptest.NewRequest(http.MethodPost, "/dashboard/admin/plugins/upload", &uploadBody)
	request.Header.Set("Content-Type", uploadContentType)
	response := serveAdminPluginRequest(
		t,
		h,
		request,
		session,
		csrf,
	)
	if response.Code != http.StatusSeeOther {
		t.Fatalf("upload status = %d, want %d", response.Code, http.StatusSeeOther)
	}
	if location := response.Header().Get("Location"); !strings.Contains(location, "saved=1") {
		t.Fatalf("upload redirect = %q", location)
	}

	plugins, err := manager.List()
	if err != nil {
		t.Fatal(err)
	}
	if len(plugins) != 1 || plugins[0].ID != "hello-world" || !plugins[0].Enabled {
		t.Fatalf("installed plugins = %+v, want automatically enabled plugin", plugins)
	}

	for _, action := range []string{"enable", "disable"} {
		request := httptest.NewRequest(http.MethodPost, "/dashboard/admin/plugins/hello-world/"+action, strings.NewReader("_csrf="+csrf))
		request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		response = serveAdminPluginRequest(t, h, request, session, csrf)
		if response.Code != http.StatusSeeOther {
			t.Fatalf("%s status = %d, want %d", action, response.Code, http.StatusSeeOther)
		}
		plugins, err = manager.List()
		if err != nil {
			t.Fatal(err)
		}
		wantEnabled := action == "enable"
		if len(plugins) != 1 || plugins[0].Enabled != wantEnabled {
			t.Fatalf("after %s plugins = %+v", action, plugins)
		}
	}

	request = httptest.NewRequest(http.MethodPost, "/dashboard/admin/plugins/hello-world/delete", strings.NewReader("_csrf="+csrf))
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	response = serveAdminPluginRequest(t, h, request, session, csrf)
	if response.Code != http.StatusSeeOther {
		t.Fatalf("delete status = %d, want %d", response.Code, http.StatusSeeOther)
	}
	plugins, err = manager.List()
	if err != nil {
		t.Fatal(err)
	}
	if len(plugins) != 0 {
		t.Fatalf("plugins after delete = %+v", plugins)
	}
	if _, err := os.Stat(filepath.Join(dataDir, "plugins", "hello-world")); !os.IsNotExist(err) {
		t.Fatalf("plugin directory still exists, stat error = %v", err)
	}
}

func serveAdminPluginRequest(t *testing.T, h *Handler, request *http.Request, session *http.Cookie, csrf string) *httptest.ResponseRecorder {
	t.Helper()
	request.AddCookie(session)
	request.AddCookie(&http.Cookie{Name: csrfCookieName(), Value: csrf})
	request.Header.Set("X-CSRF-Token", csrf)
	gin.SetMode(gin.TestMode)
	router := gin.New()
	h.Register(router)
	record := httptest.NewRecorder()
	router.ServeHTTP(record, request)
	return record
}

func makeAdminPluginArchive(t *testing.T) []byte {
	t.Helper()
	manifest, err := json.Marshal(serverplugin.Manifest{
		ID: "hello-world", Name: "Hello world", Version: "1.0.0", APIVersion: serverplugin.CurrentAPIVersion,
		Routes: []serverplugin.RouteSpec{{Method: http.MethodGet, Path: "/hello", Public: true}},
	})
	if err != nil {
		t.Fatal(err)
	}
	var archive bytes.Buffer
	writer := zip.NewWriter(&archive)
	manifestEntry, err := writer.Create("manifest.json")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := manifestEntry.Write(manifest); err != nil {
		t.Fatal(err)
	}
	wasmEntry, err := writer.Create("plugin.wasm")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := wasmEntry.Write(adminPluginWASM()); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	return archive.Bytes()
}

func adminPluginWASM() []byte {
	wasm := []byte{0x00, 0x61, 0x73, 0x6d, 0x01, 0x00, 0x00, 0x00}
	wasm = append(wasm, wasmSection(1, []byte{
		0x03,
		0x60, 0x00, 0x01, 0x7f,
		0x60, 0x01, 0x7f, 0x01, 0x7f,
		0x60, 0x02, 0x7f, 0x7f, 0x01, 0x7e,
	})...)
	wasm = append(wasm, wasmSection(3, []byte{0x03, 0x00, 0x01, 0x02})...)
	wasm = append(wasm, wasmSection(5, []byte{0x01, 0x00, 0x01})...)
	exports := []byte{0x04}
	exports = append(exports, wasmExport("memory", 0x02, 0)...)
	exports = append(exports, wasmExport("oss_abi_version", 0x00, 0)...)
	exports = append(exports, wasmExport("oss_alloc", 0x00, 1)...)
	exports = append(exports, wasmExport("oss_handle", 0x00, 2)...)
	wasm = append(wasm, wasmSection(7, exports)...)
	code := []byte{0x03}
	code = append(code, wasmFunctionBody([]byte{0x41, 0x01})...)
	code = append(code, wasmFunctionBody([]byte{0x41, 0x00})...)
	code = append(code, wasmFunctionBody([]byte{0x42, 0x00})...)
	return append(wasm, wasmSection(10, code)...)
}

func wasmSection(id byte, content []byte) []byte {
	return append([]byte{id, byte(len(content))}, content...)
}

func wasmExport(name string, kind, index byte) []byte {
	return append(append([]byte{byte(len(name))}, []byte(name)...), kind, index)
}

func wasmFunctionBody(instructions []byte) []byte {
	body := append([]byte{0x00}, append(instructions, 0x0b)...)
	return append([]byte{byte(len(body))}, body...)
}
