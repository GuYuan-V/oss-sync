package server

import (
	"archive/zip"
	"bytes"
	"io"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/oss/oss-server/internal/auth"
	"github.com/oss/oss-server/internal/consoletheme"
	"github.com/oss/oss-server/internal/models"
)

func TestConsoleThemeSelection_loadsStylesheet_whenUserChoosesCustomTheme(t *testing.T) {
	// Given
	t.Chdir(t.TempDir())
	srv, db, dataDir := newTestServer(t)
	router := srv.Router()
	if _, err := auth.CreateAccount(db, "theme-user", "password123", "user"); err != nil {
		t.Fatal(err)
	}
	_, err := consoletheme.Scaffold(dataDir, "default", "ocean")
	if err != nil {
		t.Fatal(err)
	}
	if err := consoletheme.SaveFile(dataDir, "ocean", "theme.css", []byte(":root{--cobalt:navy}")); err != nil {
		t.Fatal(err)
	}
	session, csrf := webLogin(t, router, "theme-user", "password123")

	// When
	account := doForm(t, router, http.MethodGet, "/dashboard/account", nil, session, csrf)
	saved := doForm(t, router, http.MethodPost, "/dashboard/account/theme",
		url.Values{"console_theme_name": {"ocean"}}, session, csrf)
	page := doForm(t, router, http.MethodGet, "/dashboard/account", nil, session, csrf)
	asset := doForm(t, router, http.MethodGet, "/ui/themes/ocean/theme.css", nil)

	// Then
	if account.Code != http.StatusOK || !strings.Contains(account.Body.String(), `<option value="ocean"`) {
		t.Fatalf("account theme selector: %d body=%s", account.Code, account.Body)
	}
	if saved.Code != http.StatusSeeOther {
		t.Fatalf("save console theme: %d body=%s", saved.Code, saved.Body)
	}
	var setting models.UserSetting
	if err := db.Joins("JOIN users ON users.id = user_settings.user_id").
		Where("users.username = ?", "theme-user").First(&setting).Error; err != nil {
		t.Fatal(err)
	}
	if setting.ConsoleThemeName != "ocean" {
		t.Fatalf("console theme = %q, want ocean", setting.ConsoleThemeName)
	}
	if !strings.Contains(page.Body.String(), `href="/ui/themes/ocean/theme.css"`) {
		t.Fatalf("selected stylesheet missing: %s", page.Body)
	}
	if asset.Code != http.StatusOK || asset.Header().Get("Content-Type") != "text/css; charset=utf-8" ||
		!strings.Contains(asset.Body.String(), "--cobalt:navy") {
		t.Fatalf("theme asset: %d type=%q body=%s", asset.Code, asset.Header().Get("Content-Type"), asset.Body)
	}
}

func TestAdminConsoleThemeManagement_supportsPackageLifecycle(t *testing.T) {
	// Given
	t.Chdir(t.TempDir())
	srv, db, _ := newTestServer(t)
	router := srv.Router()
	if _, err := auth.CreateAccount(db, "theme-root", "root-password-123", "admin"); err != nil {
		t.Fatal(err)
	}
	adminSession, adminCSRF := webLogin(t, router, "theme-root", "root-password-123")

	// When
	list := doForm(t, router, http.MethodGet, "/dashboard/admin/console-themes", nil, adminSession, adminCSRF)
	scaffold := doForm(t, router, http.MethodPost, "/dashboard/admin/console-themes/scaffold",
		url.Values{"base": {"default"}, "name": {"ledger"}}, adminSession, adminCSRF)
	edit := doForm(t, router, http.MethodPost, "/dashboard/admin/console-themes/ledger/files/save",
		url.Values{"path": {"theme.css"}, "content": {":root{--paper:beige}"}}, adminSession, adminCSRF)
	upload := doMultipart(t, router, "/dashboard/admin/console-themes/upload",
		map[string]string{"name": "uploaded-console"}, "file", "theme.zip",
		zipTheme(t, map[string]string{"theme.css": ":root{--ink:black}"}), adminSession, adminCSRF)
	download := doForm(t, router, http.MethodGet, "/dashboard/admin/console-themes/ledger/download", nil, adminSession, adminCSRF)

	// Then
	if list.Code != http.StatusOK || !strings.Contains(list.Body.String(), "default") ||
		!strings.Contains(list.Body.String(), `data-modal-open="console-theme-guide"`) {
		t.Fatalf("console theme list: %d body=%s", list.Code, list.Body)
	}
	for action, response := range map[string]int{
		"scaffold": scaffold.Code,
		"edit":     edit.Code,
		"upload":   upload.Code,
	} {
		if response != http.StatusSeeOther {
			t.Fatalf("%s status = %d", action, response)
		}
	}
	if download.Code != http.StatusOK {
		t.Fatalf("download status = %d body=%s", download.Code, download.Body)
	}
	reader, err := zip.NewReader(bytes.NewReader(download.Body.Bytes()), int64(download.Body.Len()))
	if err != nil {
		t.Fatal(err)
	}
	foundEditedCSS := false
	for _, file := range reader.File {
		if file.Name != "theme.css" {
			continue
		}
		rc, err := file.Open()
		if err != nil {
			t.Fatal(err)
		}
		content, err := io.ReadAll(rc)
		_ = rc.Close()
		if err != nil {
			t.Fatal(err)
		}
		foundEditedCSS = string(content) == ":root{--paper:beige}"
	}
	if !foundEditedCSS {
		t.Fatal("downloaded package does not contain edited theme.css")
	}
}

func TestAdminConsoleThemeDelete_rejectsThemeSelectedByUser(t *testing.T) {
	// Given
	t.Chdir(t.TempDir())
	srv, db, dataDir := newTestServer(t)
	router := srv.Router()
	if _, err := auth.CreateAccount(db, "delete-root", "root-password-123", "admin"); err != nil {
		t.Fatal(err)
	}
	user, err := auth.CreateAccount(db, "selected-user", "password123", "user")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := consoletheme.Scaffold(dataDir, "default", "selected"); err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&models.UserSetting{}).Where("user_id = ?", user.ID).
		Update("console_theme_name", "selected").Error; err != nil {
		t.Fatal(err)
	}
	session, csrf := webLogin(t, router, "delete-root", "root-password-123")

	// When
	response := doForm(t, router, http.MethodPost, "/dashboard/admin/console-themes/selected/delete", nil, session, csrf)

	// Then
	if response.Code != http.StatusSeeOther || !strings.Contains(response.Header().Get("Location"), "error=") {
		t.Fatalf("delete selected theme: %d location=%q", response.Code, response.Header().Get("Location"))
	}
	if !consoletheme.Exists(dataDir, "selected") {
		t.Fatal("selected console theme was deleted")
	}
}
