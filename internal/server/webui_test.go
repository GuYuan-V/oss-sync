package server

import (
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
		page.Header().Get("Content-Security-Policy") == "" {
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
	if user.Role != "user" {
		t.Fatalf("registered role = %q, want user", user.Role)
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
	if code != http.StatusOK || body["token"] == "" || body["role"] != "user" {
		t.Fatalf("plugin login after web registration: status=%d body=%v", code, body)
	}
}

func TestAdminPanelControlsRegistration(t *testing.T) {
	t.Chdir(t.TempDir())
	srv, db, _ := newTestServer(t)
	router := srv.Router()
	if _, err := auth.CreateAccount(db, "admin", "admin-password-123", "admin"); err != nil {
		t.Fatal(err)
	}

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

	opened := doForm(t, router, http.MethodPost, "/admin/registration", url.Values{
		"registration_enabled": {"on"},
	}, session)
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

	download := doForm(t, router, http.MethodGet, "/admin/backups/"+backup.ID+"/download", nil, session)
	if download.Code != http.StatusOK || download.Body.String() != "archive" {
		t.Fatalf("backup download: status=%d body=%q", download.Code, download.Body.String())
	}
	deleted := doForm(t, router, http.MethodPost, "/admin/backups/"+backup.ID+"/delete", nil, session)
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

func TestAdminPanelRejectsRegularUser(t *testing.T) {
	srv, db, _ := newTestServer(t)
	router := srv.Router()
	if _, err := auth.CreateAccount(db, "member", "password123", "user"); err != nil {
		t.Fatal(err)
	}

	login := doForm(t, router, http.MethodPost, "/admin/login", url.Values{
		"username": {"member"},
		"password": {"password123"},
	}, nil)
	if login.Code != http.StatusUnauthorized ||
		!strings.Contains(login.Body.String(), "管理员账号或密码不正确") {
		t.Fatalf("regular user admin login: status=%d body=%s", login.Code, login.Body)
	}
}

func TestAdminPlatformManagesVaultMembers(t *testing.T) {
	srv, db, dataDir := newTestServer(t)
	router := srv.Router()
	if _, err := auth.CreateAccount(db, "platform-admin", "admin-password-123", "admin"); err != nil {
		t.Fatal(err)
	}
	ownerToken := registerAndLogin(t, router, "platform-owner", "password123")
	vaultID := defaultVaultIDFromAPI(t, router, ownerToken)
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
	if session == nil {
		t.Fatal("admin session missing")
	}

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
	updated := doForm(t, router, http.MethodPost, "/admin/vaults/"+vaultID+"/members/"+strconv.FormatUint(uint64(user.ID), 10)+"/role", url.Values{"role": {"manager"}}, session)
	if updated.Code != http.StatusSeeOther {
		t.Fatalf("update member role: %d", updated.Code)
	}
	if err := db.Where("vault_id = ? AND user_id = ?", vaultID, user.ID).First(&member).Error; err != nil || member.Role != "manager" {
		t.Fatalf("manager membership: %#v err=%v", member, err)
	}
	removed := doForm(t, router, http.MethodPost, "/admin/vaults/"+vaultID+"/members/"+strconv.FormatUint(uint64(user.ID), 10)+"/delete", nil, session)
	if removed.Code != http.StatusSeeOther {
		t.Fatalf("remove member: %d", removed.Code)
	}
	if err := db.Where("vault_id = ? AND user_id = ?", vaultID, user.ID).First(&member).Error; err == nil {
		t.Fatal("membership remains")
	}
}

func doForm(
	t *testing.T,
	router *gin.Engine,
	method string,
	path string,
	form url.Values,
	cookie *http.Cookie,
) *httptest.ResponseRecorder {
	t.Helper()
	body := ""
	if form != nil {
		body = form.Encode()
	}
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	if form != nil {
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	}
	if cookie != nil {
		req.AddCookie(cookie)
	}
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, req)
	return recorder
}
