package server

import (
	"archive/zip"
	"bytes"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	"github.com/oss/oss-server/internal/auth"
	"github.com/oss/oss-server/internal/blog"
	"github.com/oss/oss-server/internal/models"
)

// zipTheme 构造包含 template.html 的主题 ZIP。
func zipTheme(t *testing.T, files map[string]string) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for name, content := range files {
		w, err := zw.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		w.Write([]byte(content))
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func TestAdminThemeManagement(t *testing.T) {
	t.Chdir(t.TempDir())
	srv, db, dataDir := newTestServer(t)
	router := srv.Router()
	if _, err := auth.CreateAccount(db, "root", "root-password-123", "admin"); err != nil {
		t.Fatal(err)
	}
	session, csrf := webLogin(t, router, "root", "root-password-123")

	// 模板列表页显示内置模板。
	list := doForm(t, router, http.MethodGet, "/dashboard/admin/themes", nil, session, csrf)
	if list.Code != http.StatusOK ||
		!strings.Contains(list.Body.String(), "default") ||
		!strings.Contains(list.Body.String(), "papertrail") {
		t.Fatalf("theme list: %d body=%s", list.Code, list.Body)
	}
	if strings.Contains(list.Body.String(), "使用仓库") {
		t.Fatal("theme list must not expose vault usage")
	}
	if !strings.Contains(list.Body.String(), `data-modal-open="theme-guide"`) ||
		!strings.Contains(list.Body.String(), `id="theme-guide" role="dialog"`) ||
		!strings.Contains(list.Body.String(), `aria-modal="true"`) ||
		!strings.Contains(list.Body.String(), `aria-labelledby="theme-guide-title"`) {
		t.Fatal("theme guide trigger or accessible dialog contract is missing")
	}

	// 脚手架：从 papertrail 创建副本。
	scaffold := doForm(t, router, http.MethodPost, "/dashboard/admin/themes/scaffold",
		url.Values{"base": {"papertrail"}, "name": {"my-blog"}}, session, csrf)
	if scaffold.Code != http.StatusSeeOther {
		t.Fatalf("scaffold: %d body=%s", scaffold.Code, scaffold.Body)
	}
	for _, f := range []string{"template.html", "style.css", "theme.js"} {
		if _, err := os.Stat(filepath.Join(dataDir, "themes", "my-blog", f)); err != nil {
			t.Fatalf("scaffold file %s: %v", f, err)
		}
	}
	editor := doForm(t, router, http.MethodGet, "/dashboard/admin/themes", nil, session, csrf)
	if !strings.Contains(editor.Body.String(), `name="path" value="template.html"`) ||
		!strings.Contains(editor.Body.String(), `data-theme-file="template.html"`) {
		t.Fatalf("theme editor does not preload individual files: %s", editor.Body)
	}

	// 编辑副本的 template.html。
	edit := doForm(t, router, http.MethodPost, "/dashboard/admin/themes/my-blog/files/save",
		url.Values{"path": {"template.html"}, "content": {"<html>edited</html>"}}, session, csrf)
	if edit.Code != http.StatusSeeOther {
		t.Fatalf("edit theme: %d body=%s", edit.Code, edit.Body)
	}
	content, err := os.ReadFile(filepath.Join(dataDir, "themes", "my-blog", "template.html"))
	if err != nil || string(content) != "<html>edited</html>" {
		t.Fatalf("edited content: %q err=%v", content, err)
	}

	// 下载副本 ZIP。
	download := doForm(t, router, http.MethodGet, "/dashboard/admin/themes/my-blog/download", nil, session, csrf)
	if download.Code != http.StatusOK {
		t.Fatalf("download theme: %d", download.Code)
	}
	zr, err := zip.NewReader(bytes.NewReader(download.Body.Bytes()), int64(download.Body.Len()))
	if err != nil {
		t.Fatalf("download not zip: %v", err)
	}
	found := false
	for _, f := range zr.File {
		if f.Name == "template.html" {
			rc, _ := f.Open()
			content, _ := io.ReadAll(rc)
			rc.Close()
			if string(content) == "<html>edited</html>" {
				found = true
			}
		}
	}
	if !found {
		t.Fatal("downloaded zip missing edited template.html")
	}

	// 上传主题 ZIP。
	upload := doMultipart(t, router, "/dashboard/admin/themes/upload",
		map[string]string{"name": "uploaded-theme"}, "file", "template.html",
		zipTheme(t, map[string]string{"template.html": "<html>uploaded</html>", "style.css": "body{}"}), session, csrf)
	if upload.Code != http.StatusSeeOther {
		t.Fatalf("upload theme: %d body=%s", upload.Code, upload.Body)
	}
	if _, err := os.Stat(filepath.Join(dataDir, "themes", "uploaded-theme", "style.css")); err != nil {
		t.Fatalf("uploaded theme file: %v", err)
	}

	// 已有自定义模板也可作为副本来源。
	list = doForm(t, router, http.MethodGet, "/dashboard/admin/themes", nil, session, csrf)
	if !strings.Contains(list.Body.String(), `<option value="uploaded-theme">uploaded-theme</option>`) {
		t.Fatalf("custom theme missing from clone sources: %s", list.Body)
	}
	customClone := doForm(t, router, http.MethodPost, "/dashboard/admin/themes/scaffold",
		url.Values{"base": {"uploaded-theme"}, "name": {"uploaded-copy"}}, session, csrf)
	if customClone.Code != http.StatusSeeOther {
		t.Fatalf("clone custom theme: %d body=%s", customClone.Code, customClone.Body)
	}
	clonedStyle, err := os.ReadFile(filepath.Join(dataDir, "themes", "uploaded-copy", "style.css"))
	if err != nil || string(clonedStyle) != "body{}" {
		t.Fatalf("cloned custom style: %q err=%v", clonedStyle, err)
	}

	// 上传路径穿越 ZIP 应被拒绝。
	evil := zipTheme(t, map[string]string{"../evil.txt": "x", "template.html": "ok"})
	badUpload := doMultipart(t, router, "/dashboard/admin/themes/upload",
		map[string]string{"name": "evil-theme"}, "file", "evil.zip", evil, session, csrf)
	if badUpload.Code != http.StatusSeeOther ||
		!strings.Contains(badUpload.Header().Get("Location"), "error=") {
		t.Fatalf("evil zip upload not rejected: %d %q", badUpload.Code, badUpload.Header().Get("Location"))
	}
	if _, err := os.Stat(filepath.Join(dataDir, "themes", "evil-theme")); err == nil {
		t.Fatal("evil theme directory created")
	}

	// 内置模板不可下载/删除。
	builtinDownload := doForm(t, router, http.MethodGet, "/dashboard/admin/themes/default/download", nil, session, csrf)
	if builtinDownload.Code != http.StatusSeeOther ||
		!strings.Contains(builtinDownload.Header().Get("Location"), "error=") {
		t.Fatalf("builtin download not blocked: %d", builtinDownload.Code)
	}
	builtinDelete := doForm(t, router, http.MethodPost, "/dashboard/admin/themes/default/delete", nil, session, csrf)
	if builtinDelete.Code != http.StatusSeeOther ||
		!strings.Contains(builtinDelete.Header().Get("Location"), "error=") {
		t.Fatalf("builtin delete not blocked: %d", builtinDelete.Code)
	}
	// 内置模板编辑 API 返回 403（用管理员 Bearer token）。
	adminToken := ""
	code, loginBody := doJSON(t, router, http.MethodPost, "/api/auth/login", "",
		map[string]string{"username": "root", "password": "root-password-123"})
	if code == http.StatusOK {
		adminToken, _ = loginBody["token"].(string)
	}
	apiReadOnly, _ := doJSON(t, router, http.MethodPut,
		"/api/admin/themes/default/files?path=template.html", adminToken, map[string]string{"content": "x"})
	if apiReadOnly != http.StatusForbidden {
		t.Fatalf("api builtin edit: %d, want 403", apiReadOnly)
	}
}

func TestPaperTrailHomeAndBlogPages(t *testing.T) {
	t.Chdir(t.TempDir())
	srv, db, _ := newTestServer(t)
	router := srv.Router()

	// 尚无公开 Vault 时显示空目录（不含私密内容）。
	home := doForm(t, router, http.MethodGet, "/", nil, nil)
	if home.Code != http.StatusOK || !strings.Contains(home.Body.String(), "暂无公开博客") {
		t.Fatalf("empty public directory: %d", home.Code)
	}

	// 创建 owner + 仓库 + 文章 + 分享。
	ownerToken := registerAndLogin(t, router, "blog-owner", "password123")
	vaultID := defaultVaultIDFromAPI(t, router, ownerToken)
	uploadViaV1(t, router, ownerToken, "Posts/Hello.md", "# 你好世界\n\n这是我的第一篇文章。\n")
	uploadViaV1(t, router, ownerToken, "Posts/Private.md", "# 私密文章\n")

	// 创建两篇分享（Hello 公开，Private 删除目标验证过滤）。
	code, body := doJSON(t, router, http.MethodPost, "/api/shares", ownerToken, map[string]any{
		"vault_id": vaultID, "target_path": "Posts/Hello.md", "allow_copy": true,
	})
	if code != http.StatusOK {
		t.Fatalf("share hello: %d %v", code, body)
	}
	shareID := body["share_id"].(string)

	// 管理员登录配置仓库。
	if _, err := auth.CreateAccount(db, "blog-root", "root-password-123", "admin"); err != nil {
		t.Fatal(err)
	}
	session, csrf := webLogin(t, router, "blog-root", "root-password-123")

	// 默认模板没有专属设置字段。
	defaultThemeSettings := doForm(t, router, http.MethodGet, "/dashboard/vaults/"+vaultID+"/theme-settings", nil, session, csrf)
	if defaultThemeSettings.Code != http.StatusOK || !strings.Contains(defaultThemeSettings.Body.String(), "没有可配置项") {
		t.Fatalf("default theme settings page: %d body=%s", defaultThemeSettings.Code, defaultThemeSettings.Body)
	}

	// 先通过仓库设置选择 papertrail 并启用公开入口。
	vaultSettings := doForm(t, router, http.MethodPost, "/dashboard/vaults/"+vaultID+"/settings",
		url.Values{
			"theme_name":       {"papertrail"},
			"recycle_bin_days": {"0"},
			"is_public_blog":   {"on"},
		}, session, csrf)
	if vaultSettings.Code != http.StatusSeeOther {
		t.Fatalf("save vault theme: %d body=%s", vaultSettings.Code, vaultSettings.Body)
	}

	// 通用主题设置页按 settings.json 动态显示并保存博客信息。
	pt := doForm(t, router, http.MethodGet, "/dashboard/vaults/"+vaultID+"/theme-settings", nil, session, csrf)
	if pt.Code != http.StatusOK ||
		!strings.Contains(pt.Body.String(), `name="setting_blog_name"`) ||
		!strings.Contains(pt.Body.String(), "papertrail 设置") ||
		!strings.Contains(pt.Body.String(), `data-theme-setting-group`) ||
		!strings.Contains(pt.Body.String(), `data-group-add`) ||
		strings.Contains(pt.Body.String(), ` name="group_buttons_label"`) {
		t.Fatalf("theme settings page: %d body=%s", pt.Code, pt.Body)
	}
	ptSave := doForm(t, router, http.MethodPost, "/dashboard/vaults/"+vaultID+"/theme-settings",
		url.Values{
			"setting_blog_name":      {"我的笔记"},
			"setting_description":    {"公开的学习记录"},
			"setting_logo_url":       {"https://example.com/logo.svg"},
			"setting_logo_size":      {"128"},
			"group_buttons_label":    {"关于"},
			"group_buttons_url":      {"/p/xyz"},
			"group_buttons_icon_url": {""},
		}, session, csrf)
	if ptSave.Code != http.StatusSeeOther {
		t.Fatalf("save theme settings: %d body=%s", ptSave.Code, ptSave.Body)
	}
	ptSaved := doForm(t, router, http.MethodGet, "/dashboard/vaults/"+vaultID+"/theme-settings", nil, session, csrf)
	if ptSaved.Code != http.StatusOK ||
		strings.Count(ptSaved.Body.String(), ` name="group_buttons_label"`) != 1 ||
		!strings.Contains(ptSaved.Body.String(), `data-group-remove`) {
		t.Fatalf("saved theme settings rows: %d body=%s", ptSaved.Code, ptSaved.Body)
	}
	legacyRoute := doForm(t, router, http.MethodGet, "/dashboard/vaults/"+vaultID+"/papertrail", nil, session, csrf)
	if legacyRoute.Code != http.StatusMovedPermanently ||
		legacyRoute.Header().Get("Location") != "/dashboard/vaults/"+vaultID+"/theme-settings" {
		t.Fatalf("legacy papertrail route: %d location=%q", legacyRoute.Code, legacyRoute.Header().Get("Location"))
	}
	var setting models.VaultSetting
	if err := db.Where("vault_id = ?", vaultID).First(&setting).Error; err != nil {
		t.Fatal(err)
	}
	if setting.ThemeName != "papertrail" {
		t.Fatalf("theme = %q, want papertrail", setting.ThemeName)
	}
	cfg := blog.ParsePaperTrailConfig(setting.ThemeConfig)
	if cfg.BlogName != "我的笔记" || cfg.LogoSize != 128 || len(cfg.Buttons) != 1 || cfg.Buttons[0].Label != "关于" {
		t.Fatalf("papertrail config: %+v", cfg)
	}

	// 服务器首页直接列出所有已公开博客，不依赖管理员选择单个首页仓库。
	home2 := doForm(t, router, http.MethodGet, "/", nil, nil)
	if home2.Code != http.StatusOK ||
		!strings.Contains(home2.Body.String(), "我的笔记") ||
		!strings.Contains(home2.Body.String(), "公开的学习记录") ||
		!strings.Contains(home2.Body.String(), `/b/`+vaultID) {
		t.Fatalf("public blog directory: %d body=%s", home2.Code, home2.Body)
	}
	if strings.Contains(home2.Body.String(), "你好世界") || strings.Contains(home2.Body.String(), "私密文章") {
		t.Fatal("server blog directory leaked article content")
	}
	blogHome := doForm(t, router, http.MethodGet, "/b/"+vaultID, nil, nil)
	if blogHome.Code != http.StatusOK || !strings.Contains(blogHome.Body.String(), `--pt-hero-logo-size: 128px`) {
		t.Fatalf("papertrail homepage logo size: %d body=%s", blogHome.Code, blogHome.Body)
	}

	// 文章页正常渲染。
	article := doForm(t, router, http.MethodGet, "/p/"+shareID, nil, nil)
	if article.Code != http.StatusOK ||
		!strings.Contains(article.Body.String(), "你好世界") ||
		!strings.Contains(article.Body.String(), "oss-blog-theme") ||
		!strings.Contains(article.Body.String(), `href="/b/`+vaultID+`"`) ||
		!strings.Contains(article.Body.String(), `data-reading-toc`) ||
		!strings.Contains(article.Body.String(), `reading-toc__disclosure`) ||
		!strings.Contains(article.Body.String(), `data-toc-open`) ||
		!strings.Contains(article.Body.String(), `data-toc-close`) ||
		!strings.Contains(article.Body.String(), `data-toc-backdrop`) ||
		!strings.Contains(article.Body.String(), `data-copy-article`) ||
		!strings.Contains(article.Body.String(), `data-back-to-top`) {
		t.Fatalf("article: %d body=%s", article.Code, article.Body)
	}
	if strings.Contains(article.Body.String(), `href="/p/xyz"`) {
		t.Fatalf("article navigation must not render homepage custom buttons: %s", article.Body)
	}

	// /b/:vault_id 公开入口（已标记公开）。
	blogEntry := doForm(t, router, http.MethodGet, "/b/"+vaultID, nil, nil)
	if blogEntry.Code != http.StatusOK ||
		!strings.Contains(blogEntry.Body.String(), "我的笔记") ||
		!strings.Contains(blogEntry.Body.String(), "公开的学习记录") ||
		!strings.Contains(blogEntry.Body.String(), "https://example.com/logo.svg") ||
		!strings.Contains(blogEntry.Body.String(), "关于") ||
		!strings.Contains(blogEntry.Body.String(), "你好世界") {
		t.Fatalf("blog entry: %d", blogEntry.Code)
	}
	if strings.Count(blogEntry.Body.String(), `href="/p/xyz"`) != 1 {
		t.Fatalf("blog home must render the configured button exactly once: %s", blogEntry.Body)
	}
	// 未标记公开的仓库入口 404。
	otherVaultID := "nonexistent"
	vaultEntry := doForm(t, router, http.MethodGet, "/b/"+otherVaultID, nil, nil)
	if vaultEntry.Code != http.StatusNotFound {
		t.Fatalf("unmarked vault blog entry: %d, want 404", vaultEntry.Code)
	}
}

func TestPublicBlogRoutesRenderCustomFragmentsWithoutScriptAndExcludeFromHome(t *testing.T) {
	t.Chdir(t.TempDir())
	srv, db, _ := newTestServer(t)
	router := srv.Router()
	setCustomFragmentsEnabledForTest(t, db, true)

	ownerToken := registerAndLogin(t, router, "fragment-owner", "password123")
	vaultID := defaultVaultIDFromAPI(t, router, ownerToken)
	uploadViaV1(t, router, ownerToken, "Notes/hello.md", "# Hello\n")
	code, body := doJSON(t, router, http.MethodPost, "/api/shares", ownerToken, map[string]any{
		"vault_id":    vaultID,
		"target_path": "Notes/hello.md",
		"allow_copy":  true,
	})
	if code != http.StatusOK {
		t.Fatalf("create share: %d %v", code, body)
	}
	shareID := body["share_id"].(string)

	if _, err := auth.CreateAccount(db, "fragment-admin", "root-password-123", "admin"); err != nil {
		t.Fatal(err)
	}
	session, csrf := webLogin(t, router, "fragment-admin", "root-password-123")

	// 先设置含有危险链接与属性的自定义片段。
	customHeader := "HEADER_MARKER\n\n<a href=\"https://example.com\">Safe Link</a> <a href=\"javascript:alert(1)\">Unsafe</a>"
	customFooter := "FOOTER_MARKER\n\n<img src=\"javascript:alert(1)\" onerror=\"alert(1)\" />"
	res := doForm(t, router, http.MethodPost, "/dashboard/vaults/"+vaultID+"/settings", url.Values{
		"theme_name":       {"default"},
		"recycle_bin_days": {"0"},
		"is_public_blog":   {"on"},
		"custom_header":    {customHeader},
		"custom_footer":    {customFooter},
	}, session, csrf)
	if res.Code != http.StatusSeeOther {
		t.Fatalf("save custom fragments: %d body=%s", res.Code, res.Body)
	}

	article := doForm(t, router, http.MethodGet, "/p/"+shareID, nil, nil)
	if article.Code != http.StatusOK {
		t.Fatalf("public article: %d body=%s", article.Code, article.Body)
	}
	if !strings.Contains(article.Body.String(), "HEADER_MARKER") || !strings.Contains(article.Body.String(), "FOOTER_MARKER") {
		t.Fatalf("custom fragment marker missing from article page: %s", article.Body)
	}
	if strings.Contains(article.Body.String(), "javascript:") || strings.Contains(article.Body.String(), "onerror=") {
		t.Fatalf("unsafe article fragment not sanitized: %s", article.Body)
	}
	if strings.Index(article.Body.String(), "HEADER_MARKER") < strings.Index(article.Body.String(), "</head>") {
		t.Fatalf("custom header must render in the body, not head: %s", article.Body)
	}

	home := doForm(t, router, http.MethodGet, "/b/"+vaultID, nil, nil)
	if home.Code != http.StatusOK {
		t.Fatalf("public blog home: %d", home.Code)
	}
	if !strings.Contains(home.Body.String(), "HEADER_MARKER") || !strings.Contains(home.Body.String(), "FOOTER_MARKER") {
		t.Fatalf("custom fragment marker missing from blog home: %s", home.Body)
	}
	if strings.Contains(home.Body.String(), "javascript:") || strings.Contains(home.Body.String(), "onerror=") {
		t.Fatalf("unsafe blog home fragment not sanitized: %s", home.Body)
	}
	if strings.Index(home.Body.String(), "HEADER_MARKER") < strings.Index(home.Body.String(), "</head>") {
		t.Fatalf("custom header must render in the body, not head: %s", home.Body)
	}

	if err := db.Model(&models.VaultSetting{}).Where("vault_id = ?", vaultID).Update("theme_name", "papertrail").Error; err != nil {
		t.Fatal(err)
	}
	for name, path := range map[string]string{"Papertrail article": "/p/" + shareID, "Papertrail home": "/b/" + vaultID} {
		page := doForm(t, router, http.MethodGet, path, nil, nil)
		if page.Code != http.StatusOK || !strings.Contains(page.Body.String(), "HEADER_MARKER") || !strings.Contains(page.Body.String(), "FOOTER_MARKER") {
			t.Fatalf("%s: status=%d body=%s", name, page.Code, page.Body)
		}
		if strings.Index(page.Body.String(), "HEADER_MARKER") < strings.Index(page.Body.String(), "</head>") {
			t.Fatalf("%s renders custom header in head: %s", name, page.Body)
		}
	}

	uploadViaV1(t, router, ownerToken, "Folder/nested.md", "# Nested\n")
	code, body = doJSON(t, router, http.MethodPost, "/api/shares", ownerToken, map[string]any{
		"vault_id": vaultID, "target_path": "Folder", "is_folder": true,
	})
	if code != http.StatusOK {
		t.Fatalf("create folder share: %d %v", code, body)
	}
	folderShareID := body["share_id"].(string)
	for name, path := range map[string]string{"folder index": "/p/" + folderShareID + "/", "folder article": "/p/" + folderShareID + "/nested.md"} {
		page := doForm(t, router, http.MethodGet, path, nil, nil)
		if page.Code != http.StatusOK || !strings.Contains(page.Body.String(), "HEADER_MARKER") || !strings.Contains(page.Body.String(), "FOOTER_MARKER") {
			t.Fatalf("%s: status=%d body=%s", name, page.Code, page.Body)
		}
	}

	publicIndex := doForm(t, router, http.MethodGet, "/", nil, nil)
	if publicIndex.Code != http.StatusOK {
		t.Fatalf("public index: %d", publicIndex.Code)
	}
	if strings.Contains(publicIndex.Body.String(), "HEADER_MARKER") || strings.Contains(publicIndex.Body.String(), "FOOTER_MARKER") {
		t.Fatal("custom fragment leaked on public index route")
	}
}

func TestPublicBlogRoutesDoNotRenderCustomFragmentsWhenPolicyDisabled(t *testing.T) {
	t.Chdir(t.TempDir())
	srv, db, _ := newTestServer(t)
	router := srv.Router()
	setCustomFragmentsEnabledForTest(t, db, true)

	ownerToken := registerAndLogin(t, router, "policy-disabled-owner", "password123")
	vaultID := defaultVaultIDFromAPI(t, router, ownerToken)
	if _, err := auth.CreateAccount(db, "policy-disabled-admin", "root-password-123", "admin"); err != nil {
		t.Fatal(err)
	}
	session, csrf := webLogin(t, router, "policy-disabled-admin", "root-password-123")

	res := doForm(t, router, http.MethodPost, "/dashboard/vaults/"+vaultID+"/settings", url.Values{
		"theme_name":       {"default"},
		"recycle_bin_days": {"0"},
		"is_public_blog":   {"on"},
		"custom_header":    {"SECRET_HEADER_MARKER"},
		"custom_footer":    {"SECRET_FOOTER_MARKER"},
	}, session, csrf)
	if res.Code != http.StatusSeeOther {
		t.Fatalf("save custom fragments while enabled: %d body=%s", res.Code, res.Body)
	}

	uploadViaV1(t, router, ownerToken, "Notes/hello.md", "# Hello\n")
	code, body := doJSON(t, router, http.MethodPost, "/api/shares", ownerToken, map[string]any{
		"vault_id":    vaultID,
		"target_path": "Notes/hello.md",
		"allow_copy":  true,
	})
	if code != http.StatusOK {
		t.Fatalf("create share: %d %v", code, body)
	}
	shareID := body["share_id"].(string)

	setCustomFragmentsEnabledForTest(t, db, false)

	article := doForm(t, router, http.MethodGet, "/p/"+shareID, nil, nil)
	if article.Code != http.StatusOK {
		t.Fatalf("public article when disabled: %d", article.Code)
	}
	if strings.Contains(article.Body.String(), "SECRET_HEADER_MARKER") || strings.Contains(article.Body.String(), "SECRET_FOOTER_MARKER") {
		t.Fatal("secret custom fragments should not render when policy is disabled")
	}

	entry := doForm(t, router, http.MethodGet, "/b/"+vaultID, nil, nil)
	if entry.Code != http.StatusOK {
		t.Fatalf("public blog entry when disabled: %d", entry.Code)
	}
	if strings.Contains(entry.Body.String(), "SECRET_HEADER_MARKER") || strings.Contains(entry.Body.String(), "SECRET_FOOTER_MARKER") {
		t.Fatal("secret custom fragments should not render on blog pages when policy is disabled")
	}

	index := doForm(t, router, http.MethodGet, "/", nil, nil)
	if index.Code != http.StatusOK {
		t.Fatalf("public index when disabled: %d", index.Code)
	}
	if strings.Contains(index.Body.String(), "SECRET_HEADER_MARKER") || strings.Contains(index.Body.String(), "SECRET_FOOTER_MARKER") {
		t.Fatal("secret custom fragments should not render on public index when policy is disabled")
	}
}

func TestPublicHomeListsPublishedVaultsWithoutLegacySelection(t *testing.T) {
	// Given
	srv, db, _ := newTestServer(t)
	router := srv.Router()
	token := registerAndLogin(t, router, "public-directory-owner", "password123")
	vaultID := defaultVaultIDFromAPI(t, router, token)
	if err := db.Model(&models.VaultSetting{}).Where("vault_id = ?", vaultID).Updates(map[string]any{
		"is_public_blog": true,
		"theme_config": models.JSONMap{
			"blog_name":   "公开笔记",
			"description": "按 Vault 发布的博客",
		},
	}).Error; err != nil {
		t.Fatal(err)
	}

	// When
	home := doForm(t, router, http.MethodGet, "/", nil, nil)

	// Then
	if home.Code != http.StatusOK ||
		!strings.Contains(home.Body.String(), "公开笔记") ||
		!strings.Contains(home.Body.String(), "按 Vault 发布的博客") ||
		!strings.Contains(home.Body.String(), `/b/`+vaultID) {
		t.Fatalf("public home: %d body=%s", home.Code, home.Body)
	}
}

func TestThemeScaffoldRejectsBuiltinName(t *testing.T) {
	t.Chdir(t.TempDir())
	srv, db, _ := newTestServer(t)
	router := srv.Router()
	if _, err := auth.CreateAccount(db, "root", "root-password-123", "admin"); err != nil {
		t.Fatal(err)
	}
	session, csrf := webLogin(t, router, "root", "root-password-123")
	res := doForm(t, router, http.MethodPost, "/dashboard/admin/themes/scaffold",
		url.Values{"base": {"default"}, "name": {"default"}}, session, csrf)
	if res.Code != http.StatusSeeOther ||
		!strings.Contains(res.Header().Get("Location"), "error=") {
		t.Fatalf("scaffold to builtin name not rejected: %d %q", res.Code, res.Header().Get("Location"))
	}
}

// doMultipart 发送 multipart 表单（ZIP 上传）。
func doMultipart(
	t *testing.T,
	router *gin.Engine,
	path string,
	fields map[string]string,
	fileField, fileName string,
	fileContent []byte,
	session, csrf *http.Cookie,
) *httptest.ResponseRecorder {
	t.Helper()
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	for k, v := range fields {
		_ = mw.WriteField(k, v)
	}
	_ = mw.WriteField("_csrf", csrf.Value)
	fw, err := mw.CreateFormFile(fileField, fileName)
	if err != nil {
		t.Fatal(err)
	}
	fw.Write(fileContent)
	mw.Close()

	req := httptest.NewRequest(http.MethodPost, path, &buf)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	req.AddCookie(session)
	req.AddCookie(csrf)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	return w
}

var _ = fmt.Sprintf
