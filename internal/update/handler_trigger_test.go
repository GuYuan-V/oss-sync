package update

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"

	"github.com/oss/oss-server/internal/config"
	"github.com/oss/oss-server/internal/version"
)

func newHandlerTestSetup(t *testing.T) (*Handler, *Manager, string, func()) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	origVer := version.Version
	t.Cleanup(func() { version.Version = origVer })
	version.Version = "1.0.0"
	dataDir := t.TempDir()
	exePath := filepath.Join(t.TempDir(), "oss-server")
	if err := os.WriteFile(exePath, []byte("old-binary"), 0o755); err != nil {
		t.Fatal(err)
	}
	cfg := &config.Config{
		Server:  config.ServerConfig{Host: "127.0.0.1", Port: 0, Mode: gin.TestMode},
		Storage: config.StorageConfig{DataDir: dataDir},
		Update:  config.UpdateConfig{GitHubRepo: "fake/oss-sync"},
	}
	mgr, err := NewManager(dataDir)
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	up, err := NewUpdater(cfg, Options{ExecPath: exePath, Verifier: func(string, string) error { return nil }})
	if err != nil {
		t.Fatalf("NewUpdater: %v", err)
	}
	svc := NewService(mgr, up, cfg)
	// mock helper launch to avoid real process
	origLaunch := launchHelperFn
	launchHelperFn = func(string, string) error { return nil }
	t.Cleanup(func() { launchHelperFn = origLaunch })
	origVerify := verifyStagedFileFn
	verifyStagedFileFn = func(string, string, string) error { return nil }
	t.Cleanup(func() { verifyStagedFileFn = origVerify })

	db, err := gorm.Open(sqlite.Open(filepath.Join(dataDir, "test.db")), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	if sqlDB, err := db.DB(); err == nil {
		t.Cleanup(func() { _ = sqlDB.Close() })
	}
	h := NewHandlerWithService(db, cfg, up, mgr, svc)
	return h, mgr, exePath, func() { _ = db }
}

func TestHandler_Trigger_Returns202WithManagerCandidate(t *testing.T) {
	h, mgr, _, _ := newHandlerTestSetup(t)
	candID := newCheckedForHandlerTest(t, mgr)
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	body, _ := json.Marshal(map[string]string{"check_id": candID, "version": "9.9.9"})
	c.Request = httptest.NewRequest(http.MethodPost, "/api/admin/update", bytes.NewReader(body))
	c.Request.Header.Set("Content-Type", "application/json")
	shutdownCalled := make(chan struct{}, 1)
	h.svc.SetOnShutdown(func() { shutdownCalled <- struct{}{} })
	h.trigger(c)
	if w.Code != http.StatusAccepted {
		t.Fatalf("trigger status %d body %s want 202", w.Code, w.Body.String())
	}
	var resp map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp["ok"] != true {
		t.Errorf("ok %v", resp["ok"])
	}
	if resp["code"] != "accepted" {
		t.Errorf("code %v", resp["code"])
	}
	op, ok := resp["operation"].(map[string]any)
	if !ok || op["id"] == "" {
		t.Errorf("operation missing %v", resp["operation"])
	}
	select {
	case <-shutdownCalled:
	case <-time.After(600 * time.Millisecond):
		t.Error("shutdown not signaled after helper launch success")
	}
}

func TestHandler_Trigger_MissingCheckID(t *testing.T) {
	h, _, _, _ := newHandlerTestSetup(t)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	body, _ := json.Marshal(map[string]string{"check_id": "nonexistent", "version": "9.9.9"})
	c.Request = httptest.NewRequest(http.MethodPost, "/api/admin/update", bytes.NewReader(body))
	c.Request.Header.Set("Content-Type", "application/json")
	h.trigger(c)
	if w.Code != http.StatusNotFound {
		t.Fatalf("missing check_id: got %d body %s want 404", w.Code, w.Body.String())
	}
}

func TestHandler_Trigger_StaleCheckID(t *testing.T) {
	h, mgr, _, _ := newHandlerTestSetup(t)
	// issue with 1ms TTL then expire
	assetName, _ := AssetName("9.9.9", runtime.GOOS, runtime.GOARCH)
	content := fakeExecBytes()
	l := strings.ToLower(assetName)
	var serveContent []byte
	if strings.HasSuffix(l, ".tar.gz") {
		serveContent = makeTarGz(t, map[string][]byte{"oss-server": content})
	} else if strings.HasSuffix(l, ".zip") {
		serveContent = makeZip(t, map[string][]byte{"oss-server": content})
	} else {
		serveContent = content
	}
	digest := digestOfBytes(serveContent)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Length", strconv.Itoa(len(serveContent)))
		_, _ = w.Write(serveContent)
	}))
	t.Cleanup(srv.Close)
	cand, _ := NewCandidate("v9.9.9", runtime.GOOS, runtime.GOARCH, srv.URL+"/"+assetName, "https://example.com/releases/tag/v9.9.9", int64(len(serveContent)), 1001, 2001, digest)
	cc, _ := mgr.IssueChecked(*cand, 1*time.Millisecond)
	time.Sleep(5 * time.Millisecond)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	body, _ := json.Marshal(map[string]string{"check_id": cc.ID, "version": "9.9.9"})
	c.Request = httptest.NewRequest(http.MethodPost, "/api/admin/update", bytes.NewReader(body))
	c.Request.Header.Set("Content-Type", "application/json")
	h.trigger(c)
	if w.Code != http.StatusGone {
		t.Fatalf("stale check: got %d body %s want 410", w.Code, w.Body.String())
	}
}

func TestHandler_Trigger_MismatchedVersion(t *testing.T) {
	h, mgr, _, _ := newHandlerTestSetup(t)
	candID := newCheckedForHandlerTest(t, mgr)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	body, _ := json.Marshal(map[string]string{"check_id": candID, "version": "9.9.10"})
	c.Request = httptest.NewRequest(http.MethodPost, "/api/admin/update", bytes.NewReader(body))
	c.Request.Header.Set("Content-Type", "application/json")
	h.trigger(c)
	if w.Code != http.StatusConflict {
		t.Fatalf("mismatched version: got %d body %s want 409", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "check_mismatch") {
		t.Errorf("body should contain check_mismatch, got %s", w.Body.String())
	}
}

func TestHandler_Trigger_HelperLaunchFailureNoShutdown(t *testing.T) {
	h, mgr, _, _ := newHandlerTestSetup(t)
	candID := newCheckedForHandlerTest(t, mgr)
	origLaunch := launchHelperFn
	launchHelperFn = func(string, string) error { return os.ErrInvalid }
	t.Cleanup(func() { launchHelperFn = origLaunch })
	shutdownCalled := false
	h.svc.SetOnShutdown(func() { shutdownCalled = true })
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	body, _ := json.Marshal(map[string]string{"check_id": candID, "version": "9.9.9"})
	c.Request = httptest.NewRequest(http.MethodPost, "/api/admin/update", bytes.NewReader(body))
	c.Request.Header.Set("Content-Type", "application/json")
	h.trigger(c)
	if w.Code == http.StatusAccepted {
		t.Fatalf("helper launch failure should not return 202, got %d", w.Code)
	}
	time.Sleep(350 * time.Millisecond)
	if shutdownCalled {
		t.Error("shutdown should not be called on helper launch failure")
	}
}

func TestHandler_Trigger_RequiresVersion(t *testing.T) {
	h, mgr, _, _ := newHandlerTestSetup(t)
	candID := newCheckedForHandlerTest(t, mgr)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	body, _ := json.Marshal(map[string]string{"check_id": candID})
	c.Request = httptest.NewRequest(http.MethodPost, "/api/admin/update", bytes.NewReader(body))
	c.Request.Header.Set("Content-Type", "application/json")
	h.trigger(c)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("missing version: got %d body %s want 400", w.Code, w.Body.String())
	}
}

func TestHandler_Check_CreatesDurableCheckID(t *testing.T) {
	origVer := version.Version
	version.Version = "1.0.0"
	t.Cleanup(func() { version.Version = origVer })
	dataDir := t.TempDir()
	exePath := filepath.Join(t.TempDir(), "oss-server")
	_ = os.WriteFile(exePath, []byte("old-binary"), 0o755)
	cfg := &config.Config{
		Server:  config.ServerConfig{Host: "127.0.0.1", Port: 0, Mode: gin.TestMode},
		Storage: config.StorageConfig{DataDir: dataDir},
		Update:  config.UpdateConfig{GitHubRepo: "fake/oss-sync"},
	}
	mgr, _ := NewManager(dataDir)
	// mock GitHub release
	content := fakeExecBytes()
	assetName, _ := AssetName("9.9.9", runtime.GOOS, runtime.GOARCH)
	var serveContent []byte
	l := strings.ToLower(assetName)
	if strings.HasSuffix(l, ".tar.gz") {
		serveContent = makeTarGz(t, map[string][]byte{"oss-server": content})
	} else if strings.HasSuffix(l, ".zip") {
		serveContent = makeZip(t, map[string][]byte{"oss-server": content})
	} else {
		serveContent = content
	}
	digest := digestOfBytes(serveContent)
	var ghSrv *httptest.Server
	mux := http.NewServeMux()
	mux.HandleFunc("/repos/fake/oss-sync/releases/latest", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"id":1001,"tag_name":"v9.9.9","html_url":"https://example.com/releases/tag/v9.9.9","draft":false,"prerelease":false,"assets":[{"id":2001,"name":"` + assetName + `","browser_download_url":"https://example.com/` + assetName + `","url":"","size":` + strconv.Itoa(len(serveContent)) + `,"digest":"` + digest + `"}]}`))
	})
	ghSrv = httptest.NewServer(mux)
	t.Cleanup(ghSrv.Close)
	up, _ := NewUpdater(cfg, Options{ExecPath: exePath, APIBase: ghSrv.URL, HTTPClient: ghSrv.Client(), Verifier: func(string, string) error { return nil }})
	svc := NewService(mgr, up, cfg)
	origLaunch := launchHelperFn
	launchHelperFn = func(string, string) error { return nil }
	t.Cleanup(func() { launchHelperFn = origLaunch })
	origVerify := verifyStagedFileFn
	verifyStagedFileFn = func(string, string, string) error { return nil }
	t.Cleanup(func() { verifyStagedFileFn = origVerify })
	db, _ := gorm.Open(sqlite.Open(filepath.Join(dataDir, "test.db")), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if sqlDB, err := db.DB(); err == nil {
		t.Cleanup(func() { _ = sqlDB.Close() })
	}
	h := NewHandlerWithService(db, cfg, up, mgr, svc)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodGet, "/api/admin/update/check", nil)
	h.check(c)
	if w.Code != http.StatusOK {
		t.Fatalf("check: %d body %s", w.Code, w.Body.String())
	}
	var resp map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp["check_id"] == "" {
		t.Errorf("check_id missing %v", resp)
	}
	if resp["candidate"] == nil {
		t.Errorf("candidate missing")
	}
	// verify durable: ValidateChecked should succeed
	checkID := resp["check_id"].(string)
	if _, err := mgr.ValidateChecked(checkID); err != nil {
		t.Errorf("durable check not found: %v", err)
	}
	_ = exePath
}

func newCheckedForHandlerTest(t *testing.T, mgr *Manager) string {
	t.Helper()
	assetName, _ := AssetName("9.9.9", runtime.GOOS, runtime.GOARCH)
	content := fakeExecBytes()
	l := strings.ToLower(assetName)
	var serveContent []byte
	if strings.HasSuffix(l, ".tar.gz") {
		serveContent = makeTarGz(t, map[string][]byte{"oss-server": content})
	} else if strings.HasSuffix(l, ".zip") {
		serveContent = makeZip(t, map[string][]byte{"oss-server": content})
	} else {
		serveContent = content
	}
	digest := digestOfBytes(serveContent)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Length", strconv.Itoa(len(serveContent)))
		_, _ = w.Write(serveContent)
	}))
	t.Cleanup(srv.Close)
	assetURL := srv.URL + "/" + assetName
	releaseURL := "https://example.com/releases/tag/v9.9.9"
	c, err := NewCandidate("v9.9.9", runtime.GOOS, runtime.GOARCH, assetURL, releaseURL, int64(len(serveContent)), 1001, 2001, digest)
	if err != nil {
		t.Fatalf("NewCandidate: %v", err)
	}
	cc, err := mgr.IssueChecked(*c, 60000000000)
	if err != nil {
		t.Fatalf("IssueChecked: %v", err)
	}
	return cc.ID
}
