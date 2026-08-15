package server

import (
<<<<<<< HEAD
	"encoding/json"
=======
>>>>>>> 3b7aaacb143eff9df5a728b914a633fc58e70a6b
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	"github.com/oss/oss-server/internal/auth"
	"github.com/oss/oss-server/internal/models"
	"github.com/oss/oss-server/internal/vaultbackup"
)

func TestWebRegistrationCreatesPluginLoginAccount(t *testing.T) {
	srv, db, _ := newTestServer(t)
	router := srv.Router()

	page := doForm(t, router, http.MethodGet, "/register", nil, nil)
	if page.Code != http.StatusOK ||
		!strings.Contains(page.Body.String(), "创建账户") ||
<<<<<<< HEAD
		!strings.Contains(page.Header().Get("Content-Security-Policy"), "connect-src 'self'") {
=======
		page.Header().Get("Content-Security-Policy") == "" {
>>>>>>> 3b7aaacb143eff9df5a728b914a633fc58e70a6b
		t.Fatalf("registration page: status=%d headers=%v body=%s", page.Code, page.Header(), page.Body)
	}

	result := doForm(t, router, http.MethodPost, "/register", url.Values{
		"username":         {"web-user"},
		"password":         {"password123"},
		"password_confirm": {"password123"},
	}, nil)
	if result.Code != http.StatusOK || !strings.Contains(result.Body.String(), "账户已写入同步账本") {
		t.Fatalf("web registration: status=%d body=%s", result.Code, result.Body)
	}

	var user models.User
	if err := db.Where("username = ?", "web-user").First(&user).Error; err != nil {
		t.Fatalf("query registered user: %v", err)
	}
<<<<<<< HEAD
	// 首个注册用户自动成为管理员。
	if user.Role != "admin" {
		t.Fatalf("registered role = %q, want admin", user.Role)
=======
	if user.Role != "user" {
		t.Fatalf("registered role = %q, want user", user.Role)
>>>>>>> 3b7aaacb143eff9df5a728b914a633fc58e70a6b
	}
	var vaultCount int64
	if err := db.Model(&models.Vault{}).Where("owner_id = ?", user.ID).Count(&vaultCount).Error; err != nil {
		t.Fatal(err)
	}
	if vaultCount != 0 {
		t.Fatalf("web registration created %d vaults, want 0", vaultCount)
	}

	code, body := doJSON(t, router, http.MethodPost, "/api/auth/login", "", map[string]string{
		"username": "web-user",
		"password": "password123",
	})
<<<<<<< HEAD
	if code != http.StatusOK || body["token"] == "" || body["role"] != "admin" {
=======
	if code != http.StatusOK || body["token"] == "" || body["role"] != "user" {
>>>>>>> 3b7aaacb143eff9df5a728b914a633fc58e70a6b
		t.Fatalf("plugin login after web registration: status=%d body=%v", code, body)
	}
}

<<<<<<< HEAD
func TestDashboardMetricsRequiresSessionAndReturnsLiveFields(t *testing.T) {
	srv, db, _ := newTestServer(t)
	router := srv.Router()

	anonymous := doForm(t, router, http.MethodGet, "/dashboard/metrics", nil, nil)
	if anonymous.Code != http.StatusSeeOther || anonymous.Header().Get("Location") != "/login" {
		t.Fatalf("anonymous metrics: status=%d location=%q", anonymous.Code, anonymous.Header().Get("Location"))
	}

	if _, err := auth.CreateAccount(db, "metrics-user", "password123", "user"); err != nil {
		t.Fatal(err)
	}
	session, csrf := webLogin(t, router, "metrics-user", "password123")
	response := doForm(t, router, http.MethodGet, "/dashboard/metrics", nil, session, csrf)
	if response.Code != http.StatusOK || !strings.HasPrefix(response.Header().Get("Content-Type"), "application/json") {
		t.Fatalf("metrics response: status=%d headers=%v", response.Code, response.Header())
	}
	var metrics map[string]json.RawMessage
	if err := json.Unmarshal(response.Body.Bytes(), &metrics); err != nil {
		t.Fatalf("decode metrics response: %v", err)
	}
	for _, field := range []string{
		"cpu_model_name", "cpu_usage_percent", "memory_used_bytes", "memory_total_bytes",
		"memory_usage_percent", "disk_used_bytes", "disk_total_bytes", "vault_storage_used", "vault_storage_quota",
	} {
		if _, ok := metrics[field]; !ok {
			t.Fatalf("metrics response is missing %q: %s", field, response.Body.String())
		}
	}
}

func TestAdminSystemControlsRegistration(t *testing.T) {
=======
func TestAdminPanelControlsRegistration(t *testing.T) {
>>>>>>> 3b7aaacb143eff9df5a728b914a633fc58e70a6b
	t.Chdir(t.TempDir())
	srv, db, _ := newTestServer(t)
	router := srv.Router()
	if _, err := auth.CreateAccount(db, "admin", "admin-password-123", "admin"); err != nil {
		t.Fatal(err)
	}

<<<<<<< HEAD
	login := doForm(t, router, http.MethodPost, "/login", url.Values{
		"username": {"admin"},
		"password": {"admin-password-123"},
	}, nil)
	if login.Code != http.StatusSeeOther || login.Header().Get("Location") != "/dashboard" {
		t.Fatalf("admin login: status=%d location=%q body=%s", login.Code, login.Header().Get("Location"), login.Body)
	}
	session, csrf := webCookies(t, login)
	if session == nil || !session.HttpOnly || session.SameSite != http.SameSiteLaxMode {
		t.Fatalf("web session cookie is missing security flags: %#v", session)
	}
	if csrf == nil || csrf.Value == "" {
		t.Fatal("csrf cookie missing")
	}

	dashboard := doForm(t, router, http.MethodGet, "/dashboard/admin/system", nil, session, csrf)
	if dashboard.Code != http.StatusOK ||
		!strings.Contains(dashboard.Body.String(), "允许新用户注册") ||
		!strings.Contains(dashboard.Body.String(), "admin") {
		t.Fatalf("admin system page: status=%d body=%s", dashboard.Code, dashboard.Body)
	}

	closed := doForm(t, router, http.MethodPost, "/dashboard/admin/system", validAdminSystemForm(), session, csrf)
=======
	login := doForm(t, router, http.MethodPost, "/admin/login", url.Values{
		"username": {"admin"},
		"password": {"admin-password-123"},
	}, nil)
	if login.Code != http.StatusSeeOther || login.Header().Get("Location") != "/admin" {
		t.Fatalf("admin login: status=%d location=%q body=%s", login.Code, login.Header().Get("Location"), login.Body)
	}
	var session *http.Cookie
	for _, cookie := range login.Result().Cookies() {
		if cookie.Name == "oss_admin_session" {
			session = cookie
			break
		}
	}
	if session == nil || !session.HttpOnly || session.SameSite != http.SameSiteStrictMode {
		t.Fatalf("admin session cookie is missing security flags: %#v", session)
	}

	dashboard := doForm(t, router, http.MethodGet, "/admin", nil, session)
	if dashboard.Code != http.StatusOK ||
		!strings.Contains(dashboard.Body.String(), "允许新用户注册") ||
		!strings.Contains(dashboard.Body.String(), "admin") {
		t.Fatalf("admin dashboard: status=%d body=%s", dashboard.Code, dashboard.Body)
	}

	closed := doForm(t, router, http.MethodPost, "/admin/registration", url.Values{}, session)
>>>>>>> 3b7aaacb143eff9df5a728b914a633fc58e70a6b
	if closed.Code != http.StatusSeeOther {
		t.Fatalf("close registration: status=%d body=%s", closed.Code, closed.Body)
	}
	enabled, err := auth.RegistrationEnabled(db, true)
	if err != nil {
		t.Fatal(err)
	}
	if enabled {
		t.Fatal("registration remained open after admin update")
	}

	code, status := doJSON(t, router, http.MethodGet, "/api/auth/status", "", nil)
	if code != http.StatusOK || status["registration_enabled"] != false {
		t.Fatalf("API status after close: status=%d body=%v", code, status)
	}
	blocked := doForm(t, router, http.MethodPost, "/register", url.Values{
		"username":         {"late-user"},
		"password":         {"password123"},
		"password_confirm": {"password123"},
	}, nil)
	if blocked.Code != http.StatusForbidden || !strings.Contains(blocked.Body.String(), "新用户注册已暂停") {
		t.Fatalf("registration after close: status=%d body=%s", blocked.Code, blocked.Body)
	}

<<<<<<< HEAD
	openForm := validAdminSystemForm()
	openForm.Set("registration_enabled", "on")
	opened := doForm(t, router, http.MethodPost, "/dashboard/admin/system", openForm, session, csrf)
=======
	opened := doForm(t, router, http.MethodPost, "/admin/registration", url.Values{
		"registration_enabled": {"on"},
	}, session)
>>>>>>> 3b7aaacb143eff9df5a728b914a633fc58e70a6b
	if opened.Code != http.StatusSeeOther {
		t.Fatalf("open registration: status=%d body=%s", opened.Code, opened.Body)
	}
	enabled, err = auth.RegistrationEnabled(db, false)
	if err != nil || !enabled {
		t.Fatalf("registration was not reopened: enabled=%v err=%v", enabled, err)
	}

	backup := models.VaultBackup{ID: "backup-test", VaultID: "deleted-vault", OwnerID: 1, VaultName: "Deleted notes", FileName: "vault-backup-test.zip", Size: 7}
	backupPath, err := vaultbackup.Path(backup.FileName)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(backupPath), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(backupPath, []byte("archive"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&backup).Error; err != nil {
		t.Fatal(err)
	}

<<<<<<< HEAD
	download := doForm(t, router, http.MethodGet, "/dashboard/admin/backups/"+backup.ID+"/download", nil, session, csrf)
	if download.Code != http.StatusOK || download.Body.String() != "archive" {
		t.Fatalf("backup download: status=%d body=%q", download.Code, download.Body.String())
	}
	deleted := doForm(t, router, http.MethodPost, "/dashboard/admin/backups/"+backup.ID+"/delete", nil, session, csrf)
=======
	download := doForm(t, router, http.MethodGet, "/admin/backups/"+backup.ID+"/download", nil, session)
	if download.Code != http.StatusOK || download.Body.String() != "archive" {
		t.Fatalf("backup download: status=%d body=%q", download.Code, download.Body.String())
	}
	deleted := doForm(t, router, http.MethodPost, "/admin/backups/"+backup.ID+"/delete", nil, session)
>>>>>>> 3b7aaacb143eff9df5a728b914a633fc58e70a6b
	if deleted.Code != http.StatusSeeOther {
		t.Fatalf("backup delete: %d", deleted.Code)
	}
	if _, err := os.Stat(backupPath); !os.IsNotExist(err) {
		t.Fatalf("backup archive remains: %v", err)
	}
	if err := db.Where("id = ?", backup.ID).First(&models.VaultBackup{}).Error; err == nil {
		t.Fatal("backup record remains")
	}
}

<<<<<<< HEAD
func TestAdminSystemSyncMode_whenSaved_persistsGlobalPolicy(t *testing.T) {
	t.Chdir(t.TempDir())

	// Given
	srv, db, _ := newTestServer(t)
	router := srv.Router()
	if _, err := auth.CreateAccount(db, "sync-admin", "admin-password-123", "admin"); err != nil {
		t.Fatal(err)
	}
	session, csrf := webLogin(t, router, "sync-admin", "admin-password-123")
	form := validAdminSystemForm()
	form.Set("sync_mode", "long_poll")

	// When
	response := doForm(t, router, http.MethodPost, "/dashboard/admin/system", form, session, csrf)

	// Then
	if response.Code != http.StatusSeeOther {
		t.Fatalf("save system sync mode: status=%d body=%s", response.Code, response.Body)
	}
	var setting models.SystemSetting
	if err := db.First(&setting, 1).Error; err != nil {
		t.Fatal(err)
	}
	if setting.SyncMode != "long_poll" {
		t.Fatalf("system sync mode = %q, want long_poll", setting.SyncMode)
	}
}

func TestAdminSystemCustomFragmentsToggle_roundTrip(t *testing.T) {
	t.Chdir(t.TempDir())

	srv, db, _ := newTestServer(t)
	router := srv.Router()
	if _, err := auth.CreateAccount(db, "custom-fragment-admin", "admin-password-123", "admin"); err != nil {
		t.Fatal(err)
	}
	session, csrf := webLogin(t, router, "custom-fragment-admin", "admin-password-123")

	open := validAdminSystemForm()
	open.Set("custom_fragments_enabled", "on")
	if res := doForm(t, router, http.MethodPost, "/dashboard/admin/system", open, session, csrf); res.Code != http.StatusSeeOther {
		t.Fatalf("enable custom fragments: status=%d body=%s", res.Code, res.Body)
	}
	var setting models.SystemSetting
	if err := db.First(&setting, 1).Error; err != nil {
		t.Fatal(err)
	}
	if !setting.CustomFragmentsEnabled {
		t.Fatalf("custom_fragments_enabled should be enabled, got %#v", setting.CustomFragmentsEnabled)
	}

	page := doForm(t, router, http.MethodGet, "/dashboard/admin/system", nil, session, csrf)
	if page.Code != http.StatusOK || !strings.Contains(page.Body.String(), `name="custom_fragments_enabled" checked`) {
		t.Fatalf("custom fragments not checked in admin page after enable: %d body=%s", page.Code, page.Body)
	}

	close := validAdminSystemForm()
	close.Del("custom_fragments_enabled")
	if res := doForm(t, router, http.MethodPost, "/dashboard/admin/system", close, session, csrf); res.Code != http.StatusSeeOther {
		t.Fatalf("disable custom fragments: status=%d body=%s", res.Code, res.Body)
	}
	if err := db.First(&setting, 1).Error; err != nil {
		t.Fatal(err)
	}
	if setting.CustomFragmentsEnabled {
		t.Fatal("custom_fragments_enabled should be disabled")
	}

	page = doForm(t, router, http.MethodGet, "/dashboard/admin/system", nil, session, csrf)
	if page.Code != http.StatusOK {
		t.Fatalf("admin system page after disable: %d body=%s", page.Code, page.Body)
	}
	if strings.Contains(page.Body.String(), `name="custom_fragments_enabled" checked`) {
		t.Fatal("custom_fragments_enabled checkbox should not be checked after disable")
	}

	if res := doForm(t, router, http.MethodPost, "/dashboard/admin/system", open, session, csrf); res.Code != http.StatusSeeOther {
		t.Fatalf("enable custom fragments again: status=%d body=%s", res.Code, res.Body)
	}
	if err := db.First(&setting, 1).Error; err != nil {
		t.Fatal(err)
	}
	if !setting.CustomFragmentsEnabled {
		t.Fatal("custom_fragments_enabled should be re-enabled")
	}
}

=======
>>>>>>> 3b7aaacb143eff9df5a728b914a633fc58e70a6b
func TestAdminPanelRejectsRegularUser(t *testing.T) {
	srv, db, _ := newTestServer(t)
	router := srv.Router()
	if _, err := auth.CreateAccount(db, "member", "password123", "user"); err != nil {
		t.Fatal(err)
	}

<<<<<<< HEAD
	// 普通用户可登录统一入口，但无法进入管理后台。
	login := doForm(t, router, http.MethodPost, "/login", url.Values{
		"username": {"member"},
		"password": {"password123"},
	}, nil)
	if login.Code != http.StatusSeeOther || login.Header().Get("Location") != "/dashboard" {
		t.Fatalf("regular user login: status=%d location=%q body=%s", login.Code, login.Header().Get("Location"), login.Body)
	}
	session, _ := webCookies(t, login)
	if session == nil {
		t.Fatal("session cookie missing")
	}
	denied := doForm(t, router, http.MethodGet, "/dashboard/admin", nil, session)
	if denied.Code != http.StatusSeeOther || denied.Header().Get("Location") != "/dashboard" {
		t.Fatalf("regular user admin access: status=%d location=%q", denied.Code, denied.Header().Get("Location"))
=======
	login := doForm(t, router, http.MethodPost, "/admin/login", url.Values{
		"username": {"member"},
		"password": {"password123"},
	}, nil)
	if login.Code != http.StatusUnauthorized ||
		!strings.Contains(login.Body.String(), "管理员账号或密码不正确") {
		t.Fatalf("regular user admin login: status=%d body=%s", login.Code, login.Body)
>>>>>>> 3b7aaacb143eff9df5a728b914a633fc58e70a6b
	}
}

func TestAdminPlatformManagesVaultMembers(t *testing.T) {
<<<<<<< HEAD
	srv, db, _ := newTestServer(t)
=======
	srv, db, dataDir := newTestServer(t)
>>>>>>> 3b7aaacb143eff9df5a728b914a633fc58e70a6b
	router := srv.Router()
	if _, err := auth.CreateAccount(db, "platform-admin", "admin-password-123", "admin"); err != nil {
		t.Fatal(err)
	}
	ownerToken := registerAndLogin(t, router, "platform-owner", "password123")
	vaultID := defaultVaultIDFromAPI(t, router, ownerToken)
<<<<<<< HEAD
	code, _ := doJSON(t, router, http.MethodPost, "/api/auth/register", "", map[string]string{"username": "platform-member", "password": "password123"})
	if code != http.StatusOK {
		t.Fatalf("register member: %d", code)
	}

	login := doForm(t, router, http.MethodPost, "/login", url.Values{"username": {"platform-admin"}, "password": {"admin-password-123"}}, nil)
	session, csrf := webCookies(t, login)
=======
	code, memberLogin := doJSON(t, router, http.MethodPost, "/api/auth/register", "", map[string]string{"username": "platform-member", "password": "password123"})
	if code != http.StatusOK {
		t.Fatalf("register member: %d %v", code, memberLogin)
	}

	login := doForm(t, router, http.MethodPost, "/admin/login", url.Values{"username": {"platform-admin"}, "password": {"admin-password-123"}}, nil)
	var session *http.Cookie
	for _, cookie := range login.Result().Cookies() {
		if cookie.Name == "oss_admin_session" {
			session = cookie
		}
	}
>>>>>>> 3b7aaacb143eff9df5a728b914a633fc58e70a6b
	if session == nil {
		t.Fatal("admin session missing")
	}

<<<<<<< HEAD
	// 管理员进入任意仓库的协作成员页；Vault 角色仍由下方独立路由管理。
	page := doForm(t, router, http.MethodGet, "/dashboard/vaults/"+vaultID+"/members", nil, session, csrf)
	if page.Code != http.StatusOK || !strings.Contains(page.Body.String(), "已授权协作成员") {
		t.Fatalf("member page: %d %s", page.Code, page.Body.String())
	}
	added := doForm(t, router, http.MethodPost, "/dashboard/vaults/"+vaultID+"/members", url.Values{"username": {"platform-member"}, "role": {"participant"}}, session, csrf)
=======
	page := doForm(t, router, http.MethodGet, "/admin/vaults/"+vaultID, nil, session)
	if page.Code != http.StatusOK || !strings.Contains(page.Body.String(), "已授权成员") || !strings.Contains(page.Body.String(), "创建开发模板") {
		t.Fatalf("member page: %d %s", page.Code, page.Body.String())
	}
	createdTheme := doForm(t, router, http.MethodPost, "/admin/vaults/"+vaultID+"/theme/development", url.Values{"theme_name": {"field-notes"}}, session)
	if createdTheme.Code != http.StatusSeeOther {
		t.Fatalf("create development theme: %d body=%s", createdTheme.Code, createdTheme.Body.String())
	}
	for _, filename := range []string{"template.html", "style.css", "theme.js", "README.md"} {
		if _, err := os.Stat(filepath.Join(dataDir, "themes", "field-notes", filename)); err != nil {
			t.Fatalf("development theme file %s: %v", filename, err)
		}
	}
	var setting models.VaultSetting
	if err := db.Where("vault_id = ?", vaultID).First(&setting).Error; err != nil || setting.ThemeName != "field-notes" {
		t.Fatalf("development theme was not enabled: %#v err=%v", setting, err)
	}
	added := doForm(t, router, http.MethodPost, "/admin/vaults/"+vaultID+"/members", url.Values{"username": {"platform-member"}, "role": {"participant"}}, session)
>>>>>>> 3b7aaacb143eff9df5a728b914a633fc58e70a6b
	if added.Code != http.StatusSeeOther {
		t.Fatalf("add member: %d", added.Code)
	}
	var user models.User
	if err := db.Where("username = ?", "platform-member").First(&user).Error; err != nil {
		t.Fatal(err)
	}
	var member models.VaultMember
	if err := db.Where("vault_id = ? AND user_id = ?", vaultID, user.ID).First(&member).Error; err != nil || member.Role != "participant" {
		t.Fatalf("participant membership: %#v err=%v", member, err)
	}
<<<<<<< HEAD
	updated := doForm(t, router, http.MethodPost, "/dashboard/vaults/"+vaultID+"/members/"+strconv.FormatUint(uint64(user.ID), 10)+"/role", url.Values{"role": {"manager"}}, session, csrf)
=======
	updated := doForm(t, router, http.MethodPost, "/admin/vaults/"+vaultID+"/members/"+strconv.FormatUint(uint64(user.ID), 10)+"/role", url.Values{"role": {"manager"}}, session)
>>>>>>> 3b7aaacb143eff9df5a728b914a633fc58e70a6b
	if updated.Code != http.StatusSeeOther {
		t.Fatalf("update member role: %d", updated.Code)
	}
	if err := db.Where("vault_id = ? AND user_id = ?", vaultID, user.ID).First(&member).Error; err != nil || member.Role != "manager" {
		t.Fatalf("manager membership: %#v err=%v", member, err)
	}
<<<<<<< HEAD
	removed := doForm(t, router, http.MethodPost, "/dashboard/vaults/"+vaultID+"/members/"+strconv.FormatUint(uint64(user.ID), 10)+"/delete", nil, session, csrf)
=======
	removed := doForm(t, router, http.MethodPost, "/admin/vaults/"+vaultID+"/members/"+strconv.FormatUint(uint64(user.ID), 10)+"/delete", nil, session)
>>>>>>> 3b7aaacb143eff9df5a728b914a633fc58e70a6b
	if removed.Code != http.StatusSeeOther {
		t.Fatalf("remove member: %d", removed.Code)
	}
	if err := db.Where("vault_id = ? AND user_id = ?", vaultID, user.ID).First(&member).Error; err == nil {
		t.Fatal("membership remains")
	}
}

<<<<<<< HEAD
// webCookies 从登录响应中提取会话与 CSRF cookie。
func webCookies(t *testing.T, res *httptest.ResponseRecorder) (*http.Cookie, *http.Cookie) {
	t.Helper()
	var session, csrf *http.Cookie
	for _, cookie := range res.Result().Cookies() {
		if cookie.Name == "oss_web_session" {
			session = cookie
		}
		if cookie.Name == "oss_csrf" {
			csrf = cookie
		}
	}
	return session, csrf
}

=======
>>>>>>> 3b7aaacb143eff9df5a728b914a633fc58e70a6b
func doForm(
	t *testing.T,
	router *gin.Engine,
	method string,
	path string,
	form url.Values,
<<<<<<< HEAD
	cookies ...*http.Cookie,
) *httptest.ResponseRecorder {
	t.Helper()
	var session *http.Cookie
	var csrf *http.Cookie
	for _, c := range cookies {
		if c == nil {
			continue
		}
		if c.Name == "oss_web_session" || c.Name == "oss_admin_session" {
			session = c
		}
		if c.Name == "oss_csrf" {
			csrf = c
		}
	}
=======
	cookie *http.Cookie,
) *httptest.ResponseRecorder {
	t.Helper()
>>>>>>> 3b7aaacb143eff9df5a728b914a633fc58e70a6b
	body := ""
	if form != nil {
		body = form.Encode()
	}
<<<<<<< HEAD
	if form == nil && csrf != nil && method != http.MethodGet && method != http.MethodHead {
		form = url.Values{"_csrf": {csrf.Value}}
		body = form.Encode()
	}
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	if form != nil {
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		// 状态修改请求必须携带 CSRF token。
		if csrf != nil && method != http.MethodGet && method != http.MethodHead {
			form.Set("_csrf", csrf.Value)
			req = httptest.NewRequest(method, path, strings.NewReader(form.Encode()))
			req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		}
	}
	if session != nil {
		req.AddCookie(session)
	}
	if csrf != nil {
		req.AddCookie(csrf)
=======
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	if form != nil {
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	}
	if cookie != nil {
		req.AddCookie(cookie)
>>>>>>> 3b7aaacb143eff9df5a728b914a633fc58e70a6b
	}
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, req)
	return recorder
}
<<<<<<< HEAD

func validAdminSystemForm() url.Values {
	return url.Values{
		"sync_mode":                {"user_choice"},
		"default_recycle_bin_days": {"30"},
		"max_long_poll_wait_sec":   {"30"},
		"max_sync_debounce_sec":    {"300"},
		"max_recycle_bin_days":     {"3650"},
		"max_vault_storage_mb":     {"0"},
		"max_upload_size_mb":       {"100"},
	}
}
=======
>>>>>>> 3b7aaacb143eff9df5a728b914a633fc58e70a6b
