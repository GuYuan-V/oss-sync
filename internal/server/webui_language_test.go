package server

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/oss/oss-server/internal/models"
)

// TestWebConsoleLanguageDefaultChinese 验证新账户默认以中文渲染控制台。
func TestWebConsoleLanguageDefaultChinese(t *testing.T) {
	t.Chdir(t.TempDir())
	srv, _, _ := newTestServer(t)
	router := srv.Router()
	registerAndLogin(t, router, "lang-zh-owner", "password123")
	session, _ := webLogin(t, router, "lang-zh-owner", "password123")

	req := httptest.NewRequest(http.MethodGet, "/dashboard/account", nil)
	req.AddCookie(session)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("account page status=%d", w.Code)
	}
	body := w.Body.String()
	if !strings.Contains(body, `<html lang="zh-CN"`) {
		t.Errorf("default account page should render zh-CN")
	}
	if !strings.Contains(body, "网页语言") {
		t.Errorf("default account page missing Chinese copy")
	}
	if strings.Contains(body, "Web language") {
		t.Errorf("default account page must not render English copy")
	}
}

// TestWebConsoleLanguageSwitchPersistsAcrossPages 验证保存英文偏好后，
// 个人中心与概览页都按英文渲染，且偏好持久化到账号。
func TestWebConsoleLanguageSwitchPersistsAcrossPages(t *testing.T) {
	t.Chdir(t.TempDir())
	srv, db, _ := newTestServer(t)
	router := srv.Router()
	registerAndLogin(t, router, "lang-en-owner", "password123")
	session, csrf := webLogin(t, router, "lang-en-owner", "password123")

	resp := doForm(t, router, http.MethodPost, "/dashboard/account/language",
		url.Values{"web_language": {"en"}}, session, csrf)
	if resp.Code != http.StatusSeeOther {
		t.Fatalf("save language status=%d", resp.Code)
	}

	var setting models.UserSetting
	var user models.User
	if err := db.Where("username = ?", "lang-en-owner").First(&user).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Where("user_id = ?", user.ID).First(&setting).Error; err != nil {
		t.Fatal(err)
	}
	if setting.WebLanguage != "en" {
		t.Fatalf("persisted web_language=%q, want en", setting.WebLanguage)
	}

	req := httptest.NewRequest(http.MethodGet, "/dashboard/account", nil)
	req.AddCookie(session)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if !strings.Contains(w.Body.String(), `<html lang="en"`) ||
		!strings.Contains(w.Body.String(), "Web language") ||
		strings.Contains(w.Body.String(), ">网页语言<") {
		t.Errorf("account page not fully English: %s", w.Body.String())
	}

	req = httptest.NewRequest(http.MethodGet, "/dashboard", nil)
	req.AddCookie(session)
	w = httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if !strings.Contains(w.Body.String(), `<html lang="en"`) ||
		!strings.Contains(w.Body.String(), "<h1>Home</h1>") ||
		strings.Contains(w.Body.String(), "<h1>首页</h1>") {
		t.Errorf("overview page not fully English: %s", w.Body.String())
	}
}

func TestWebConsolePluginLanguageHintDoesNotChangeAccountPreference(t *testing.T) {
	t.Chdir(t.TempDir())
	srv, db, _ := newTestServer(t)
	router := srv.Router()
	registerAndLogin(t, router, "plugin-language-owner", "password123")
	session, _ := webLogin(t, router, "plugin-language-owner", "password123")

	request := httptest.NewRequest(http.MethodGet, "/dashboard?web_language=en", nil)
	request.AddCookie(session)
	page := httptest.NewRecorder()
	router.ServeHTTP(page, request)

	if page.Code != http.StatusOK || !strings.Contains(page.Body.String(), `<html lang="zh-CN"`) {
		t.Fatalf("plugin language hint page: status=%d body=%s", page.Code, page.Body.String())
	}
	var setting models.UserSetting
	var user models.User
	if err := db.Where("username = ?", "plugin-language-owner").First(&user).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Where("user_id = ?", user.ID).First(&setting).Error; err != nil {
		t.Fatal(err)
	}
	if setting.WebLanguage != "zh" {
		t.Fatalf("web language=%q, want zh", setting.WebLanguage)
	}

	request = httptest.NewRequest(http.MethodGet, "/dashboard/vaults", nil)
	request.AddCookie(session)
	page = httptest.NewRecorder()
	router.ServeHTTP(page, request)
	if page.Code != http.StatusOK || !strings.Contains(page.Body.String(), `<html lang="zh-CN"`) || !strings.Contains(page.Body.String(), "我的仓库") {
		t.Fatalf("secondary navigation page: status=%d body=%s", page.Code, page.Body.String())
	}
}

// TestWebConsoleLanguageRejectsInvalidValue 验证非法语言值被拒绝且不修改偏好。
func TestWebConsoleLanguageRejectsInvalidValue(t *testing.T) {
	t.Chdir(t.TempDir())
	srv, db, _ := newTestServer(t)
	router := srv.Router()
	registerAndLogin(t, router, "lang-bad-owner", "password123")
	session, csrf := webLogin(t, router, "lang-bad-owner", "password123")

	resp := doForm(t, router, http.MethodPost, "/dashboard/account/language",
		url.Values{"web_language": {"fr"}}, session, csrf)
	if resp.Code != http.StatusSeeOther || !strings.Contains(resp.Header().Get("Location"), "error=") {
		t.Fatalf("invalid language: status=%d location=%q", resp.Code, resp.Header().Get("Location"))
	}
	var setting models.UserSetting
	if err := db.Where("user_id = ?", 1).First(&setting).Error; err != nil {
		t.Fatal(err)
	}
	if setting.WebLanguage != "zh" {
		t.Errorf("invalid value changed web_language to %q, want zh", setting.WebLanguage)
	}
}

// TestWebConsoleLoginPageStaysChineseDefault 验证未登录页（登录/注册）保持中文默认。
func TestWebConsoleLoginPageStaysChineseDefault(t *testing.T) {
	t.Chdir(t.TempDir())
	srv, _, _ := newTestServer(t)
	router := srv.Router()

	w := httptest.NewRecorder()
	router.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/login", nil))
	if w.Code != http.StatusOK {
		t.Fatalf("login page status=%d", w.Code)
	}
	if !strings.Contains(w.Body.String(), `<html lang="zh-CN"`) ||
		!strings.Contains(w.Body.String(), "登录你的同步账本。") {
		t.Errorf("login page should stay Chinese default: %s", w.Body.String())
	}
}

// TestWebConsoleEnglishModePreservesUserContent 验证英文模式下用户内容
// （中文仓库名、中文 Markdown 正文）原样渲染，不被翻译。
func TestWebConsoleEnglishModePreservesUserContent(t *testing.T) {
	t.Chdir(t.TempDir())
	srv, _, _ := newTestServer(t)
	router := srv.Router()
	ownerToken := registerAndLogin(t, router, "lang-content-owner", "password123")

	defaultVault := defaultVaultIDFromAPI(t, router, ownerToken)
	createVaultViaAPI(t, router, ownerToken, "我的笔记")
	uploadViaV1(t, router, ownerToken, "笔记.md", "# 标题\n\n这是中文正文")

	session, csrf := webLogin(t, router, "lang-content-owner", "password123")
	if resp := doForm(t, router, http.MethodPost, "/dashboard/account/language",
		url.Values{"web_language": {"en"}}, session, csrf); resp.Code != http.StatusSeeOther {
		t.Fatalf("save language status=%d", resp.Code)
	}

	req := httptest.NewRequest(http.MethodGet, "/dashboard/vaults", nil)
	req.AddCookie(session)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	body := w.Body.String()
	if !strings.Contains(body, `<html lang="en"`) {
		t.Errorf("vaults page not English: %s", body)
	}
	if !strings.Contains(body, "我的笔记") {
		t.Errorf("Chinese vault name must render verbatim: %s", body)
	}
	if !strings.Contains(body, "My vaults") {
		t.Errorf("vaults page missing English chrome: %s", body)
	}

	preview := "http://unused/dashboard/vaults/" + defaultVault + "/files/preview?" +
		url.Values{"path": {"笔记.md"}}.Encode()
	req = httptest.NewRequest(http.MethodGet, preview, nil)
	req.AddCookie(session)
	w = httptest.NewRecorder()
	router.ServeHTTP(w, req)
	body = w.Body.String()
	if w.Code != http.StatusOK {
		t.Fatalf("preview status=%d body=%s", w.Code, body)
	}
	if !strings.Contains(body, `<html lang="en"`) {
		t.Errorf("preview page not English: %s", body)
	}
	if !strings.Contains(body, "这是中文正文") {
		t.Errorf("Chinese markdown body must render verbatim: %s", body)
	}
}
