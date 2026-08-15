package server

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"testing"

	"github.com/gin-gonic/gin"

	"github.com/oss/oss-server/internal/models"
)

// loginAsDevice 使用设备请求头登录，返回响应码和 JSON body。
func loginAsDevice(t *testing.T, router *gin.Engine, username, password, clientID, deviceName string) (int, map[string]any) {
	t.Helper()
	raw, _ := jsonMarshal(map[string]string{"username": username, "password": password})
	req := httptest.NewRequest(http.MethodPost, "/api/auth/login", bytes.NewReader(raw))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-OSS-Client-ID", clientID)
	req.Header.Set("X-OSS-Device-Name", url.QueryEscape(deviceName))
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	return w.Code, decodeMap(w.Body.Bytes())
}

// approveAs 以指定 token 批准设备并设置仓库授权。
func approveAs(t *testing.T, router *gin.Engine, token, clientID string, vaultIDs []string, userID *uint) (int, map[string]any) {
	t.Helper()
	body := map[string]any{"status": "approved", "name": clientID, "vault_ids": vaultIDs}
	if userID != nil {
		body["user_id"] = *userID
	}
	raw, _ := jsonMarshal(body)
	req := httptest.NewRequest(http.MethodPut, "/api/devices/"+url.PathEscape(clientID)+"/authorization", bytes.NewReader(raw))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	return w.Code, decodeMap(w.Body.Bytes())
}

// requestAsDevice 以指定设备身份调用 API。
func requestAsDevice(t *testing.T, router *gin.Engine, method, path, token, clientID string, body any) (int, map[string]any) {
	t.Helper()
	var rdr io.Reader
	if body != nil {
		raw, _ := jsonMarshal(body)
		rdr = bytes.NewReader(raw)
	}
	req := httptest.NewRequest(method, path, rdr)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	req.Header.Set("X-OSS-Client-ID", clientID)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	return w.Code, decodeMap(w.Body.Bytes())
}

func jsonMarshal(v any) ([]byte, error) {
	b, err := json.Marshal(v)
	return b, err
}

func TestDevicePendingBlocksSyncUntilApproved(t *testing.T) {
	srv, _, _ := newTestServer(t)
	router := srv.Router()
	code, reg := doJSON(t, router, http.MethodPost, "/api/auth/register", "", map[string]string{"username": "dev-user", "password": "password123"})
	if code != http.StatusOK {
		t.Fatalf("register: %d %v", code, reg)
	}
	token := reg["token"].(string)
	vaultID := createVaultViaAPI(t, router, token, "Vault")

	code, login := loginAsDevice(t, router, "dev-user", "password123", "dev-1", "Laptop 1")
	if code != http.StatusOK || login["device_status"] != "pending" {
		t.Fatalf("login registers pending device: %d %v", code, login)
	}

	code, status := doJSON(t, router, http.MethodGet, "/api/auth/device-status?client_id=dev-1", token, nil)
	if code != http.StatusOK || status["status"] != "pending" {
		t.Fatalf("device-status: %d %v", code, status)
	}

	// pending 设备不能访问同步端点。
	code, body := requestAsDevice(t, router, http.MethodGet,
		"/api/vaults/"+url.PathEscape(vaultID)+"/sync/manifest?after=0&client_id=dev-1",
		token, "dev-1", nil)
	if code != http.StatusForbidden || body["code"] != "device_pending" {
		t.Fatalf("pending manifest: %d %v", code, body)
	}

	// 批准后可以同步。
	code, _ = approveAs(t, router, token, "dev-1", []string{vaultID}, nil)
	if code != http.StatusOK {
		t.Fatalf("approve: %d", code)
	}
	code, status = doJSON(t, router, http.MethodGet, "/api/auth/device-status?client_id=dev-1", token, nil)
	if code != http.StatusOK || status["status"] != "approved" {
		t.Fatalf("device-status after approve: %d %v", code, status)
	}
	code, renamed := doJSON(t, router, http.MethodPatch, "/api/devices/dev-1", token,
		map[string]string{"name": "Renamed after approval"})
	if code != http.StatusConflict {
		t.Fatalf("approved device rename: %d %v, want 409", code, renamed)
	}
	code, _ = requestAsDevice(t, router, http.MethodGet,
		"/api/vaults/"+url.PathEscape(vaultID)+"/sync/manifest?after=0&client_id=dev-1",
		token, "dev-1", nil)
	if code != http.StatusOK {
		t.Fatalf("approved manifest: %d", code)
	}
}

func TestDeviceVaultAuthorizationScopesAccess(t *testing.T) {
	srv, _, _ := newTestServer(t)
	router := srv.Router()
	code, reg := doJSON(t, router, http.MethodPost, "/api/auth/register", "", map[string]string{"username": "scope-user", "password": "password123"})
	if code != http.StatusOK {
		t.Fatalf("register: %d", code)
	}
	token := reg["token"].(string)
	vaultA := createVaultViaAPI(t, router, token, "Vault A")
	vaultB := createVaultViaAPI(t, router, token, "Vault B")

	if code, _ = approveAs(t, router, token, "scope-dev", []string{vaultA}, nil); code != http.StatusOK {
		t.Fatalf("approve scope-dev: %d", code)
	}
	manifest := func(vaultID string) (int, string) {
		code, body := requestAsDevice(t, router, http.MethodGet,
			"/api/vaults/"+url.PathEscape(vaultID)+"/sync/manifest?after=0&client_id=scope-dev",
			token, "scope-dev", nil)
		c, _ := body["code"].(string)
		return code, c
	}
	if code, _ := manifest(vaultA); code != http.StatusOK {
		t.Fatalf("authorized vault should sync: %d", code)
	}
	if code, c := manifest(vaultB); code != http.StatusForbidden || c != "device_not_authorized" {
		t.Fatalf("unauthorized vault must be rejected: %d %v", code, c)
	}

	// 移除仓库授权后立即拒绝。
	if code, _ = approveAs(t, router, token, "scope-dev", []string{}, nil); code != http.StatusOK {
		t.Fatalf("clear authorization: %d", code)
	}
	if code, c := manifest(vaultA); code != http.StatusForbidden || c != "device_not_authorized" {
		t.Fatalf("revoked vault authorization still allowed: %d %v", code, c)
	}
}

func TestDeviceFilteredVaultListing(t *testing.T) {
	srv, _, _ := newTestServer(t)
	router := srv.Router()
	code, reg := doJSON(t, router, http.MethodPost, "/api/auth/register", "", map[string]string{"username": "filter-user", "password": "password123"})
	if code != http.StatusOK {
		t.Fatalf("register: %d", code)
	}
	token := reg["token"].(string)
	vaultA := createVaultViaAPI(t, router, token, "Vault A")
	createVaultViaAPI(t, router, token, "Vault B")
	if code, _ = approveAs(t, router, token, "filter-dev", []string{vaultA}, nil); code != http.StatusOK {
		t.Fatalf("approve: %d", code)
	}

	code, body := requestAsDevice(t, router, http.MethodGet, "/api/vaults", token, "filter-dev", nil)
	if code != http.StatusOK {
		t.Fatalf("device vault list: %d", code)
	}
	rows := body["vaults"].([]any)
	if len(rows) != 1 || rows[0].(map[string]any)["id"] != vaultA {
		t.Fatalf("device must only see authorized vaults: %v", body)
	}
}

func TestUserCannotAuthorizeInaccessibleVault(t *testing.T) {
	srv, _, _ := newTestServer(t)
	router := srv.Router()
	code, owner := doJSON(t, router, http.MethodPost, "/api/auth/register", "", map[string]string{"username": "owner-a", "password": "password123"})
	if code != http.StatusOK {
		t.Fatalf("register owner: %d", code)
	}
	ownerToken := owner["token"].(string)
	vaultID := createVaultViaAPI(t, router, ownerToken, "Private")

	code, other := doJSON(t, router, http.MethodPost, "/api/auth/register", "", map[string]string{"username": "owner-b", "password": "password123"})
	if code != http.StatusOK {
		t.Fatalf("register other: %d", code)
	}
	otherToken := other["token"].(string)
	code, body := approveAs(t, router, otherToken, "other-dev", []string{vaultID}, nil)
	if code != http.StatusBadRequest || body["code"] != "invalid_vault_authorization" {
		t.Fatalf("approving an inaccessible vault must fail: %d %v", code, body)
	}
}

func TestAdminCanApproveAnotherUsersDevice(t *testing.T) {
	srv, db, _ := newTestServer(t)
	router := srv.Router()
	adminToken := createAdminToken(t, srv, db, "platform-admin", "password123")

	code, reg := doJSON(t, router, http.MethodPost, "/api/auth/register", "", map[string]string{"username": "member-x", "password": "password123"})
	if code != http.StatusOK {
		t.Fatalf("register member: %d", code)
	}
	memberToken := reg["token"].(string)
	vaultID := createVaultViaAPI(t, router, memberToken, "Member Vault")

	// member 登录登记设备。
	code, login := loginAsDevice(t, router, "member-x", "password123", "member-dev", "Member PC")
	if code != http.StatusOK || login["device_status"] != "pending" {
		t.Fatalf("member login: %d %v", code, login)
	}
	var member models.User
	if err := db.Where("username = ?", "member-x").First(&member).Error; err != nil {
		t.Fatal(err)
	}
	// 管理员跨用户批准。
	code, _ = approveAs(t, router, adminToken, "member-dev", []string{vaultID}, &member.ID)
	if code != http.StatusOK {
		t.Fatalf("admin approve: %d", code)
	}
	code, _ = requestAsDevice(t, router, http.MethodGet,
		"/api/vaults/"+url.PathEscape(vaultID)+"/sync/manifest?after=0&client_id=member-dev",
		memberToken, "member-dev", nil)
	if code != http.StatusOK {
		t.Fatalf("member device sync after admin approval: %d", code)
	}
}

func TestAdminResetPasswordInvalidatesOldToken(t *testing.T) {
	srv, db, _ := newTestServer(t)
	router := srv.Router()
	adminToken := createAdminToken(t, srv, db, "root-admin", "password123")
	code, reg := doJSON(t, router, http.MethodPost, "/api/auth/register", "", map[string]string{"username": "reset-me", "password": "password123"})
	if code != http.StatusOK {
		t.Fatalf("register: %d", code)
	}
	memberToken := reg["token"].(string)

	var member models.User
	if err := db.Where("username = ?", "reset-me").First(&member).Error; err != nil {
		t.Fatal(err)
	}
	code, _ = doJSON(t, router, http.MethodPut, "/api/admin/users/"+itoa(member.ID)+"/password", adminToken, map[string]string{
		"new_password": "fresh-password-456", "confirm_password": "fresh-password-456",
	})
	if code != http.StatusOK {
		t.Fatalf("admin reset password: %d", code)
	}
	// 旧 token 立即失效。
	code, _ = doJSON(t, router, http.MethodGet, "/api/vaults", memberToken, nil)
	if code != http.StatusUnauthorized {
		t.Fatalf("old token should be invalid: %d", code)
	}
}

func TestSelfPasswordChangeReturnsFreshToken(t *testing.T) {
	srv, _, _ := newTestServer(t)
	router := srv.Router()
	code, reg := doJSON(t, router, http.MethodPost, "/api/auth/register", "", map[string]string{"username": "self-change", "password": "password123"})
	if code != http.StatusOK {
		t.Fatalf("register: %d", code)
	}
	token := reg["token"].(string)

	code, body := doJSON(t, router, http.MethodPost, "/api/account/password", token, map[string]string{
		"old_password": "wrong-password", "new_password": "new-pass-123", "confirm_password": "new-pass-123",
	})
	if code != http.StatusUnauthorized || body["code"] != "wrong_old_password" {
		t.Fatalf("wrong old password must fail: %d %v", code, body)
	}

	code, body = doJSON(t, router, http.MethodPost, "/api/account/password", token, map[string]string{
		"old_password": "password123", "new_password": "new-pass-123", "confirm_password": "new-pass-123",
	})
	if code != http.StatusOK || body["token"] == "" {
		t.Fatalf("change password: %d %v", code, body)
	}
	// 旧 token 失效，新 token 可用。
	if code, _ = doJSON(t, router, http.MethodGet, "/api/vaults", token, nil); code != http.StatusUnauthorized {
		t.Fatalf("old token after password change: %d", code)
	}
	if code, _ = doJSON(t, router, http.MethodGet, "/api/vaults", body["token"].(string), nil); code != http.StatusOK {
		t.Fatalf("fresh token after password change: %d", code)
	}
}

func TestLastAdminCannotBeDemotedOrDeleted(t *testing.T) {
	srv, db, _ := newTestServer(t)
	router := srv.Router()
	adminToken := createAdminToken(t, srv, db, "solo-admin", "password123")
	var admin models.User
	if err := db.Where("username = ?", "solo-admin").First(&admin).Error; err != nil {
		t.Fatal(err)
	}

	code, body := doJSON(t, router, http.MethodPatch, "/api/admin/users/"+itoa(admin.ID), adminToken, map[string]any{"role": "user"})
	if code != http.StatusConflict || body["code"] != "last_admin" {
		t.Fatalf("demote last admin: %d %v", code, body)
	}
	code, _ = doJSON(t, router, http.MethodDelete, "/api/admin/users/"+itoa(admin.ID), adminToken, nil)
	if code != http.StatusConflict {
		t.Fatalf("delete last admin: %d", code)
	}
}

func TestPluginCreatedVaultGrantsCurrentApprovedDevice(t *testing.T) {
	srv, _, _ := newTestServer(t)
	router := srv.Router()
	code, reg := doJSON(t, router, http.MethodPost, "/api/auth/register", "", map[string]string{"username": "creator", "password": "password123"})
	if code != http.StatusOK {
		t.Fatalf("register: %d", code)
	}
	token := reg["token"].(string)
	createVaultViaAPI(t, router, token, "First")
	if code, _ = approveAs(t, router, token, "creator-dev", []string{}, nil); code != http.StatusOK {
		t.Fatalf("approve: %d", code)
	}

	// 插件创建仓库，自动授权当前已批准设备。
	req := httptest.NewRequest(http.MethodPost, "/api/vaults", bytes.NewBufferString(`{"name":"Plugin Vault"}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("X-OSS-Client-ID", "creator-dev")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("plugin create vault: %d %s", w.Code, w.Body.String())
	}
	vaultID := decodeMap(w.Body.Bytes())["id"].(string)

	code, body := requestAsDevice(t, router, http.MethodGet, "/api/vaults", token, "creator-dev", nil)
	if code != http.StatusOK {
		t.Fatalf("device vault list: %d", code)
	}
	found := false
	for _, row := range body["vaults"].([]any) {
		if row.(map[string]any)["id"] == vaultID {
			found = true
		}
	}
	if !found {
		t.Fatalf("plugin-created vault not authorized for the device: %v", body)
	}
}

func itoa(n uint) string {
	return strconv.FormatUint(uint64(n), 10)
}
