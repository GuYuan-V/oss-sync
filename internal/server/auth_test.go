package server

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"gorm.io/gorm"

	"github.com/oss/oss-server/internal/auth"
	"github.com/oss/oss-server/internal/models"
)

func TestAuthMiddleware_NoToken(t *testing.T) {
	srv, _, _ := newTestServer(t)
	code, _ := doJSON(t, srv.Router(), "POST", "/api/sync/check", "", map[string]any{
		"mode": "full", "files": []any{},
	})
	if code != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d", code)
	}
}

func TestRegistrationOpenByDefault(t *testing.T) {
	srv, _, _ := newTestServer(t)
	router := srv.Router()
	code, first := doJSON(t, router, "POST", "/api/auth/register", "", map[string]string{
		"username": "first", "password": "password123",
	})
	if code != http.StatusOK || first["role"] != "admin" {
		t.Fatalf("first public register must become admin: status=%d body=%v", code, first)
	}
	code, second := doJSON(t, router, "POST", "/api/auth/register", "", map[string]string{
		"username": "second", "password": "password123",
	})
	if code != http.StatusOK || second["role"] != "user" {
		t.Errorf("second public register: status=%d body=%v", code, second)
	}
}

func TestRegistrationClosedBlocksAnonymous(t *testing.T) {
	srv, db, _ := newTestServer(t)
	router := srv.Router()
	if err := auth.SetRegistrationEnabled(db, false); err != nil {
		t.Fatal(err)
	}

	code, body := doJSON(t, router, "POST", "/api/auth/register", "", map[string]string{
		"username": "member", "password": "password123",
	})
	if code != http.StatusForbidden || body["code"] != "registration_closed" {
		t.Errorf("closed public register: status=%d body=%v", code, body)
	}
}

func TestAnonymousRegistrationCannotCreateAdmin(t *testing.T) {
	srv, db, _ := newTestServer(t)
	router := srv.Router()
	// 已有管理员后，匿名注册携带 role=admin 也不能提权。
	createAdminToken(t, srv, db, "platform-admin", "password123")

	code, body := doJSON(t, router, "POST", "/api/auth/register", "", map[string]string{
		"username": "member", "password": "password123", "role": "admin",
	})
	if code != http.StatusOK || body["role"] != "user" {
		t.Errorf("anonymous admin escalation: status=%d body=%v", code, body)
	}
}

func TestPublicRegistrationCreatesUserSettingsWithoutVault(t *testing.T) {
	srv, db, _ := newTestServer(t)
	code, body := doJSON(t, srv.Router(), "POST", "/api/auth/register", "", map[string]string{
		"username": "owner", "password": "password123", "role": "user",
	})
	if code != http.StatusOK || body["role"] == "" {
		t.Errorf("public register: status=%d body=%v", code, body)
	}
	var settingsCount int64
	db.Model(&models.UserSetting{}).Count(&settingsCount)
	if settingsCount != 1 {
		t.Errorf("default user settings count: got %d want 1", settingsCount)
	}
	var vaultCount int64
	db.Model(&models.Vault{}).Count(&vaultCount)
	if vaultCount != 0 {
		t.Errorf("registration created %d vaults, want 0", vaultCount)
	}
}

func TestVaultCreationRequiresExplicitRequest(t *testing.T) {
	srv, _, _ := newTestServer(t)
	router := srv.Router()
	code, registered := doJSON(t, router, http.MethodPost, "/api/auth/register", "", map[string]string{
		"username": "manual-vault",
		"password": "password123",
	})
	if code != http.StatusOK {
		t.Fatalf("register: status=%d body=%v", code, registered)
	}
	token := registered["token"].(string)

	code, listed := doJSON(t, router, http.MethodGet, "/api/vaults", token, nil)
	rows, ok := listed["vaults"].([]any)
	if code != http.StatusOK || !ok || len(rows) != 0 {
		t.Fatalf("vaults before explicit create: status=%d body=%v", code, listed)
	}

	code, first := doJSON(t, router, http.MethodPost, "/api/vaults", token, map[string]string{
		"name": "Notes",
	})
	if code != http.StatusCreated || first["is_default"] != true {
		t.Fatalf("first explicit vault: status=%d body=%v", code, first)
	}
	code, second := doJSON(t, router, http.MethodPost, "/api/vaults", token, map[string]string{
		"name": "Archive",
	})
	if code != http.StatusCreated || second["is_default"] != false {
		t.Fatalf("second explicit vault: status=%d body=%v", code, second)
	}
}

func TestLoginAcceptsCorrectPassword(t *testing.T) {
	srv, _, _ := newTestServer(t)
	router := srv.Router()
	registerAndLogin(t, router, "owner", "password123")
	code, body := doJSON(t, router, "POST", "/api/auth/login", "", map[string]string{
		"username": "owner", "password": "password123",
	})
	if code != http.StatusOK || body["token"] == "" || body["role"] == "" {
		t.Errorf("login: status=%d body=%v", code, body)
	}
}

func TestLoginRejectsWrongPassword(t *testing.T) {
	srv, _, _ := newTestServer(t)
	router := srv.Router()
	registerAndLogin(t, router, "owner", "password123")
	code, _ := doJSON(t, router, "POST", "/api/auth/login", "", map[string]string{
		"username": "owner", "password": "wrong-password",
	})
	if code != http.StatusUnauthorized {
		t.Errorf("wrong password: got %d want 401", code)
	}
}

func TestRegisterRejectsInvalidCredentials(t *testing.T) {
	srv, _, _ := newTestServer(t)
	code, _ := doJSON(t, srv.Router(), "POST", "/api/auth/register", "", map[string]string{
		"username": "ab", "password": "short",
	})
	if code != http.StatusBadRequest {
		t.Errorf("invalid registration: got %d want 400", code)
	}
}

func TestAdminCanRegisterUser(t *testing.T) {
	srv, db, _ := newTestServer(t)
	router := srv.Router()
	adminToken := createAdminToken(t, srv, db, "owner", "password123")
	code, body := doJSON(t, router, "POST", "/api/auth/register", adminToken, map[string]string{
		"username": "member", "password": "password123",
	})
	if code != http.StatusOK || body["role"] != "user" {
		t.Errorf("admin register: status=%d body=%v", code, body)
	}
}

func TestAdminCanRegisterAdmin(t *testing.T) {
	srv, db, _ := newTestServer(t)
	router := srv.Router()
	adminToken := createAdminToken(t, srv, db, "owner", "password123")
	code, body := doJSON(t, router, "POST", "/api/auth/register", adminToken, map[string]string{
		"username": "second-admin", "password": "password123", "role": "admin",
	})
	if code != http.StatusOK || body["role"] != "admin" {
		t.Errorf("admin register admin: status=%d body=%v", code, body)
	}
}

func TestNonAdminCannotRegisterUser(t *testing.T) {
	srv, db, _ := newTestServer(t)
	router := srv.Router()
	createAdminToken(t, srv, db, "admin", "password123")
	memberToken := registerAndLogin(t, router, "member", "password123")

	code, _ := doJSON(t, router, "POST", "/api/auth/register", memberToken, map[string]string{
		"username": "blocked", "password": "password123",
	})
	if code != http.StatusForbidden {
		t.Errorf("non-admin register: got %d want 403", code)
	}
}

func TestAuthStatusTracksFirstAdmin(t *testing.T) {
	srv, db, _ := newTestServer(t)
	router := srv.Router()
	code, body := doJSON(t, router, "GET", "/api/auth/status", "", nil)
	if code != http.StatusOK ||
		body["needs_first_admin"] != true ||
		body["registration_mode"] != "open" {
		t.Fatalf("status before admin bootstrap: %d %v", code, body)
	}
	createAdminToken(t, srv, db, "owner", "password123")
	code, body = doJSON(t, router, "GET", "/api/auth/status", "", nil)
	if code != http.StatusOK ||
		body["needs_first_admin"] != false ||
		body["registration_mode"] != "open" {
		t.Fatalf("status after admin bootstrap: %d %v", code, body)
	}
}

func TestAuthStatusReportsRegistrationToggle(t *testing.T) {
	srv, db, _ := newTestServer(t)
	router := srv.Router()
	code, body := doJSON(t, router, "GET", "/api/auth/status", "", nil)
	if code != http.StatusOK ||
		body["registration_enabled"] != true ||
		body["registration_mode"] != "open" {
		t.Fatalf("open registration status: %d %v", code, body)
	}
	if err := auth.SetRegistrationEnabled(db, false); err != nil {
		t.Fatal(err)
	}
	code, body = doJSON(t, router, "GET", "/api/auth/status", "", nil)
	if code != http.StatusOK ||
		body["registration_enabled"] != false ||
		body["registration_mode"] != "closed" {
		t.Fatalf("closed registration status: %d %v", code, body)
	}
}

func TestBasicAuthRejectsWrongPassword(t *testing.T) {
	srv, _, _ := newTestServer(t)
	router := srv.Router()
	registerAndLogin(t, router, "owner", "password123")
	req := httptest.NewRequest("POST", "/api/sync/check", strings.NewReader(`{"mode":"full","files":[]}`))
	req.Header.Set("Content-Type", "application/json")
	req.SetBasicAuth("owner", "wrong-password")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Code != http.StatusUnauthorized {
		t.Errorf("wrong basic password: got %d want 401", w.Code)
	}
}

func createAdminToken(t *testing.T, srv *Server, db *gorm.DB, username, password string) string {
	t.Helper()
	user, err := auth.CreateAccount(db, username, password, "admin")
	if err != nil {
		t.Fatalf("create admin: %v", err)
	}
	token, _, err := auth.IssueToken(srv.Cfg, *user)
	if err != nil {
		t.Fatalf("issue admin token: %v", err)
	}
	return token
}
