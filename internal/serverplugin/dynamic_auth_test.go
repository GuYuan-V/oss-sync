package serverplugin

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"

	"github.com/helantianshen/oss-sync/internal/auth"
	"github.com/helantianshen/oss-sync/internal/config"
	"github.com/helantianshen/oss-sync/internal/models"
)

func editorAuthCfg(t *testing.T, m *Manager) *config.Config {
	t.Helper()
	if err := m.db.AutoMigrate(&models.SystemSetting{}); err != nil {
		t.Fatalf("migrate system settings: %v", err)
	}
	cfg := &config.Config{Auth: config.AuthConfig{JWTSecret: "test-secret-32-bytes-long-000000", JWTTTLHours: 1, WebSessionTTLHours: 24}}
	if err := auth.EnsureDatabaseJWTSecret(m.db, cfg); err != nil {
		t.Fatalf("ensure jwt secret: %v", err)
	}
	return cfg
}

func webSessionCtx(method, path string, cookies, headers map[string]string) *gin.Context {
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(method, path, nil)
	for name, value := range cookies {
		c.Request.AddCookie(&http.Cookie{Name: name, Value: value})
	}
	for name, value := range headers {
		c.Request.Header.Set(name, value)
	}
	return c
}

func TestAuthenticatePluginRequest_webSessionCookieAndCSRF(t *testing.T) {
	m := reviewManager(t)
	cfg := editorAuthCfg(t, m)
	user, err := auth.CreateAccount(m.db, "editor-user", "pass12345", "user")
	if err != nil {
		t.Fatalf("create account: %v", err)
	}
	token, _, err := auth.IssueWebToken(cfg, *user)
	if err != nil {
		t.Fatalf("issue web token: %v", err)
	}

	get := webSessionCtx("GET", "/md-editor", map[string]string{webSessionCookie: token}, nil)
	if !m.authenticatePluginRequest(get, cfg) {
		t.Fatal("GET with web session cookie should authenticate")
	}
	if u := auth.CurrentUser(get); u == nil || u.Username != "editor-user" {
		t.Fatalf("current user not set from cookie: %+v", u)
	}

	postNoCSRF := webSessionCtx("POST", "/md-editor/save", map[string]string{webSessionCookie: token}, nil)
	if m.authenticatePluginRequest(postNoCSRF, cfg) {
		t.Fatal("POST via cookie without CSRF must be rejected")
	}

	postCSRF := webSessionCtx("POST", "/md-editor/save",
		map[string]string{webSessionCookie: token, webCSRFCookie: "csrf-abc"},
		map[string]string{"X-CSRF-Token": "csrf-abc"})
	if !m.authenticatePluginRequest(postCSRF, cfg) {
		t.Fatal("POST via cookie with matching CSRF must pass")
	}

	postBadCSRF := webSessionCtx("POST", "/md-editor/save",
		map[string]string{webSessionCookie: token, webCSRFCookie: "csrf-abc"},
		map[string]string{"X-CSRF-Token": "wrong"})
	if m.authenticatePluginRequest(postBadCSRF, cfg) {
		t.Fatal("POST with mismatched CSRF must be rejected")
	}
}

func TestAuthenticatePluginRequest_rejectsMissingCredentials(t *testing.T) {
	m := reviewManager(t)
	cfg := editorAuthCfg(t, m)
	c := webSessionCtx("GET", "/md-editor", nil, nil)
	if m.authenticatePluginRequest(c, cfg) {
		t.Fatal("request without credentials must be rejected")
	}
}
