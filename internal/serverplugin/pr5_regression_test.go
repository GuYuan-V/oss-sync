package serverplugin

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/helantianshen/oss-sync/internal/auth"
	"github.com/helantianshen/oss-sync/internal/blog"
	"github.com/helantianshen/oss-sync/internal/config"
	"github.com/helantianshen/oss-sync/internal/filestore"
	"github.com/helantianshen/oss-sync/internal/models"
	"github.com/helantianshen/oss-sync/pkg/ossplugin"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func reviewManager(t *testing.T) *Manager {
	t.Helper()
	db, e := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "review.db")), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if e != nil {
		t.Fatal(e)
	}
	sqlDB, _ := db.DB()
	t.Cleanup(func() { sqlDB.Close() })
	if e = db.AutoMigrate(&models.User{}, &models.UserSetting{}, &models.Vault{}, &models.VaultMember{}, &models.ServerPlugin{}, &models.ServerPluginMigration{}, &models.VaultPluginSetting{}); e != nil {
		t.Fatal(e)
	}
	m, e := NewManager(context.Background(), db, t.TempDir())
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { m.Close(context.Background()) })
	return m
}

type reviewInstance struct {
	calls        int
	last         PluginRequest
	registration ExtensionRegistration
}

func (p *reviewInstance) Invoke(_ context.Context, r PluginRequest) (PluginResponse, error) {
	p.calls++
	p.last = r
	b, _ := json.Marshal(r.Settings)
	return PluginResponse{Status: 200, BodyBase64: base64.StdEncoding.EncodeToString(b)}, nil
}
func (p *reviewInstance) Close(context.Context) error         { return nil }
func (p *reviewInstance) Healthy() bool                       { return true }
func (p *reviewInstance) Registration() ExtensionRegistration { return p.registration }

func TestReviewHookRejectsForeignVault(t *testing.T) {
	m := reviewManager(t)
	user, e := auth.CreateAccount(m.db, "review-outsider", "pass12345", "user")
	if e != nil {
		t.Fatal(e)
	}
	m.db.Create(&models.Vault{ID: "other-private-vault", OwnerID: user.ID + 1, Name: "Private"})
	m.db.Create(&models.VaultPluginSetting{VaultID: "other-private-vault", PluginID: "test-plugin", Config: models.JSONMap{"private_value": "other-vault-setting"}})
	inst := &reviewInstance{}
	m.modules["test-plugin"] = inst
	m.registrations["test-plugin"] = ExtensionRegistration{Hooks: []RegisteredHook{{Name: "editor.command", Kind: "filter"}}}
	cfg := &config.Config{Auth: config.AuthConfig{JWTSecret: "review-test-secret", JWTTTLHours: 1}}
	token, _, e := auth.IssueToken(cfg, *user)
	if e != nil {
		t.Fatal(e)
	}
	gin.SetMode(gin.TestMode)
	r := gin.New()
	m.RegisterRoutes(r, cfg)
	req := httptest.NewRequest("POST", "/api/plugin-hooks/editor.command", strings.NewReader(`{"vault_id":"other-private-vault","content":"x"}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code == http.StatusOK || inst.calls != 0 {
		t.Fatalf("foreign-vault call accepted: status=%d calls=%d body=%s", w.Code, inst.calls, w.Body.String())
	}
}
func TestReviewRelativeExecutableDirectory(t *testing.T) {
	dir, e := os.MkdirTemp(".", "review-relative-")
	if e != nil {
		t.Fatal(e)
	}
	defer os.RemoveAll(dir)
	if e = os.WriteFile(filepath.Join(dir, "plugin.exe"), readTestBinary(t), 0700); e != nil {
		t.Fatal(e)
	}
	manifest := Manifest{ID: "relative-plugin", Name: "Relative", Version: "1.0.0", APIVersion: 1, Runtime: RuntimeExecutable, Entrypoints: map[string]string{"any": "plugin.exe"}, Args: []string{"-test.run=^TestExecutablePluginProcessChild$"}}
	p, e := startExecutablePlugin(context.Background(), dir, manifest, nil)
	if e != nil {
		t.Fatalf("executable in existing relative directory failed: %v", e)
	}
	p.Close(context.Background())
}
func TestReviewHostModelListReturnsRows(t *testing.T) {
	m := reviewManager(t)
	if e := m.db.Create(&models.User{Username: "present", PasswordHash: "hash"}).Error; e != nil {
		t.Fatal(e)
	}
	rows, e := m.hostModelList(context.Background(), map[string]json.RawMessage{"model": json.RawMessage(`"users"`)})
	if e != nil {
		t.Fatal(e)
	}
	if len(rows.([]ossplugin.User)) != 1 {
		t.Fatalf("database has 1 user but list returned %#v", rows)
	}
}
func TestReviewUpgradeFailureKeepsOldEnabledPlugin(t *testing.T) {
	m := reviewManager(t)
	manifest := Manifest{ID: "review-upgrade", Name: "Review", Version: "1.0.0", APIVersion: 1, Routes: []RouteSpec{{Method: "GET", Path: "/hello", Public: true}}}
	pack := func(mf Manifest) []byte {
		raw, e := json.Marshal(mf)
		if e != nil {
			t.Fatal(e)
		}
		return makePackageArchive(t, map[string][]byte{"manifest.json": raw, "plugin.wasm": testResponseModule(t, PluginResponse{Status: 200})})
	}
	first := pack(manifest)
	if _, e := m.Install(context.Background(), bytes.NewReader(first), int64(len(first))); e != nil {
		t.Fatal(e)
	}
	if e := m.Enable(context.Background(), manifest.ID); e != nil {
		t.Fatal(e)
	}
	manifest.Version = "2.0.0"
	manifest.Registration = &ExtensionRegistration{Migrations: []RegisteredMigration{{ID: "fail", Statements: []string{"THIS IS NOT SQL"}}}}
	next := pack(manifest)
	_, e := m.Upgrade(context.Background(), bytes.NewReader(next), int64(len(next)))
	if e == nil {
		t.Fatal("expected migration failure")
	}
	record, _ := m.record(manifest.ID)
	if !record.Enabled || m.modules[manifest.ID] == nil {
		t.Fatalf("failed upgrade disabled original plugin: version=%s enabled=%v loaded=%v err=%v", record.Version, record.Enabled, m.modules[manifest.ID] != nil, e)
	}
}
func TestReviewInvokeDeadlineCoversPipeWrite(t *testing.T) {
	rd, wr, e := os.Pipe()
	if e != nil {
		t.Fatal(e)
	}
	defer rd.Close()
	defer wr.Close()
	p := &executablePlugin{stdin: wr, pending: map[string]chan processResult{}, done: make(chan struct{}), ready: make(chan error, 1)}
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	done := make(chan error, 1)
	go func() {
		_, e := p.Invoke(ctx, PluginRequest{Method: "POST", Path: "/blocked", BodyBase64: strings.Repeat("a", 128*1024)})
		done <- e
	}()
	select {
	case <-done:
	case <-time.After(300 * time.Millisecond):
		rd.Close()
		<-done
		t.Fatal("Invoke remained blocked on stdin after context deadline")
	}
}

func TestReviewShareCreateSetsOwner(t *testing.T) {
	m := reviewManager(t)
	m.db.AutoMigrate(&models.Share{}, &models.File{})
	vault := models.Vault{ID: "share-vault", OwnerID: 42, Name: "Shared vault"}
	m.db.Create(&vault)
	m.db.Create(&models.File{UserID: vault.OwnerID, VaultID: vault.ID, Path: "note.md", Type: "markdown"})
	share, e := m.hostShareCreate(context.Background(), map[string]json.RawMessage{"vault_id": json.RawMessage(`"share-vault"`), "target_path": json.RawMessage(`"note.md"`)})
	if e != nil {
		t.Fatal(e)
	}
	if share.UserID != vault.OwnerID {
		t.Fatalf("plugin-created share user_id=%d, actual vault/file owner=%d; public handler queries files by share.UserID", share.UserID, vault.OwnerID)
	}
}

func TestReviewSDKWirePreservesShareFields(t *testing.T) {
	m := reviewManager(t)
	m.db.AutoMigrate(&models.Share{})
	expected := models.Share{ShareID: "share-123", UserID: 42, VaultID: "vault-123", TargetPath: "note.md", AllowCopy: true}
	m.db.Create(&expected)
	value, e := m.hostModelList(context.Background(), map[string]json.RawMessage{"model": json.RawMessage(`"shares"`)})
	if e != nil {
		t.Fatal(e)
	}
	raw, e := json.Marshal(value)
	if e != nil {
		t.Fatal(e)
	}
	var decoded []ossplugin.Share
	if e = json.Unmarshal(raw, &decoded); e != nil {
		t.Fatal(e)
	}
	if len(decoded) != 1 || decoded[0].ShareID != expected.ShareID || decoded[0].VaultID != expected.VaultID || decoded[0].AllowCopy != true {
		t.Fatalf("SDK loses wire fields: JSON=%s decoded=%+v", raw, decoded)
	}
}
func TestReviewPluginCreatedShareCanBeOpened(t *testing.T) {
	m := reviewManager(t)
	m.db.AutoMigrate(&models.Share{}, &models.File{}, &models.VaultSetting{}, &models.SystemSetting{})
	vault := models.Vault{ID: "share-vault", OwnerID: 42, Name: "Shared vault"}
	m.db.Create(&vault)
	key := filestore.VaultStorageKey(vault.ID, "note.md")
	root := filepath.Dir(m.root)
	filePath := filepath.Join(root, key)
	os.MkdirAll(filepath.Dir(filePath), 0755)
	os.WriteFile(filePath, []byte("# Shared note"), 0600)
	m.db.Create(&models.File{UserID: 42, VaultID: vault.ID, Path: "note.md", Type: "markdown", StorageKey: key})
	m.db.Create(&models.VaultSetting{VaultID: vault.ID, ThemeName: "default"})
	share, e := m.hostShareCreate(context.Background(), map[string]json.RawMessage{"vault_id": json.RawMessage(`"share-vault"`), "target_path": json.RawMessage(`"note.md"`)})
	if e != nil {
		t.Fatal(e)
	}
	cfg := &config.Config{Storage: config.StorageConfig{DataDir: root}}
	h, e := blog.New(m.db, cfg)
	if e != nil {
		t.Fatal(e)
	}
	router := gin.New()
	h.Register(router)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, httptest.NewRequest("GET", "/p/"+share.ShareID, nil))
	if w.Code != 200 {
		t.Fatalf("valid plugin-created share cannot open: status=%d body=%s", w.Code, w.Body.String())
	}
}

func mustReview(t *testing.T, result *gorm.DB) {
	t.Helper()
	if result.Error != nil {
		t.Fatal(result.Error)
	}
}
