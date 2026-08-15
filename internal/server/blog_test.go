package server

import (
<<<<<<< HEAD
	"errors"
=======
>>>>>>> 3b7aaacb143eff9df5a728b914a633fc58e70a6b
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
<<<<<<< HEAD
	"regexp"
=======
>>>>>>> 3b7aaacb143eff9df5a728b914a633fc58e70a6b
	"strings"
	"testing"

	"github.com/oss/oss-server/internal/models"
<<<<<<< HEAD
	"gorm.io/gorm"
=======
>>>>>>> 3b7aaacb143eff9df5a728b914a633fc58e70a6b
)

func TestShareAndBlogFlow(t *testing.T) {
	srv, _, _ := newTestServer(t)
	router := srv.Router()
	token := registerAndLogin(t, router, "bob", "password123")
<<<<<<< HEAD
	uploadFile(t, router, token, "Notes/Go.md", "# Go\n\n## Introduction\n\nLink: [[Rust]]", 1700000000000)
=======
	uploadFile(t, router, token, "Notes/Go.md", "# Go\nLink: [[Rust]]", 1700000000000)
>>>>>>> 3b7aaacb143eff9df5a728b914a633fc58e70a6b

	code, body := doJSON(t, router, "POST", "/api/shares", token, map[string]any{
		"target_path": "Notes/Go.md", "is_folder": false, "allow_copy": true, "recursive_backlinks": false,
	})
	if code != http.StatusOK {
		t.Fatalf("create share: %d %v", code, body)
	}
	shareID := body["share_id"].(string)
	code, body = doJSON(t, router, "GET", "/api/shares", token, nil)
	if code != http.StatusOK || len(body["shares"].([]any)) != 1 {
		t.Errorf("list shares: status=%d body=%v", code, body)
	}

	req := httptest.NewRequest("GET", "/p/"+shareID, nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), `unshared-link`) {
		t.Errorf("blog render: status=%d body=%s", w.Code, w.Body.String())
	}
<<<<<<< HEAD
	if !strings.Contains(w.Body.String(), `data-theme-key="oss-blog-theme"`) ||
		!strings.Contains(w.Body.String(), "theme-switcher") {
		t.Error("expected blog theme switching markup")
	}
	for _, marker := range []string{`data-reading-toc`, `data-copy-article`, `data-back-to-top`} {
		if !strings.Contains(w.Body.String(), marker) {
			t.Errorf("default share missing reading utility %s", marker)
		}
	}
	for _, marker := range []string{`data-toc-open`, `data-toc-close`, `data-toc-backdrop`, `reading-toc__disclosure`} {
		if !strings.Contains(w.Body.String(), marker) {
			t.Errorf("default share missing mobile TOC marker %s", marker)
		}
=======
	if !strings.Contains(w.Body.String(), `<script>window.__THEME_CONFIG__`) {
		t.Error("expected ThemeConfig injection")
>>>>>>> 3b7aaacb143eff9df5a728b914a633fc58e70a6b
	}

	req = httptest.NewRequest("GET", "/p/ZZZZZZ", nil)
	w = httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Code != http.StatusNotFound || !strings.Contains(w.Body.String(), "已被作者移除") {
		t.Errorf("expected removed page, got status=%d", w.Code)
	}
	code, _ = doJSON(t, router, "DELETE", "/api/shares/"+shareID, token, nil)
	if code != http.StatusNoContent {
		t.Errorf("delete share: %d", code)
	}
}

func TestResolvedWikilinkRender(t *testing.T) {
	srv, _, _ := newTestServer(t)
	router := srv.Router()
	token := registerAndLogin(t, router, "carol", "password123")
	uploadFile(t, router, token, "Notes/Rust.md", "# Rust\nRust is cool", 1700000000000)
	uploadFile(t, router, token, "Notes/Go.md", "# Go\nSee [[Rust]]", 1700000000001)

	_, body := doJSON(t, router, "POST", "/api/shares", token, map[string]any{
		"target_path": "Notes/Go.md", "is_folder": false, "allow_copy": false,
	})
	goID := body["share_id"].(string)
	_, body = doJSON(t, router, "POST", "/api/shares", token, map[string]any{
		"target_path": "Notes/Rust.md", "is_folder": false, "allow_copy": false,
	})
	rustID := body["share_id"].(string)
	req := httptest.NewRequest("GET", "/p/"+goID, nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	want := `<a href="/p/` + rustID + `" target="_blank">Rust</a>`
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), want) {
		t.Errorf("expected resolved wikilink %q, got status=%d body=%s", want, w.Code, w.Body.String())
	}
}

func TestRecursiveBacklinkSharing(t *testing.T) {
	srv, _, _ := newTestServer(t)
	router := srv.Router()
	token := registerAndLogin(t, router, "dave", "password123")
	uploadFile(t, router, token, "Notes/Rust.md", "# Rust", 1700000000000)
	uploadFile(t, router, token, "Notes/Go.md", "# Go\n[[Rust]] and [[Missing]]", 1700000000001)

	code, body := doJSON(t, router, "POST", "/api/shares", token, map[string]any{
		"target_path": "Notes/Go.md", "is_folder": false, "allow_copy": false, "recursive_backlinks": true,
	})
	if code != http.StatusOK {
		t.Fatalf("create share: %d %v", code, body)
	}
	extra := body["extra"].([]any)
	if len(extra) != 1 || extra[0].(map[string]any)["target_path"] != "Notes/Rust.md" {
		t.Errorf("expected one Rust backlink share, got %v", extra)
	}
}

func TestSharedBlogServesReferencedImage(t *testing.T) {
	srv, _, _ := newTestServer(t)
	router := srv.Router()
	token := registerAndLogin(t, router, "images", "password123")
	uploadFile(t, router, token, "Notes/Post.md", "# Post\n![[Pasted image.png]]", 1700000000000)
	uploadFile(t, router, token, "static/Pasted image.png", "image-bytes", 1700000000001)

	_, body := doJSON(t, router, "POST", "/api/shares", token, map[string]any{
		"target_path": "Notes/Post.md", "is_folder": false, "allow_copy": false,
	})
	shareID := body["share_id"].(string)
	req := httptest.NewRequest(http.MethodGet, "/p/"+shareID, nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if !strings.Contains(w.Body.String(), `/assets/`+shareID+`?ref=Pasted%20image.png`) {
		t.Fatalf("shared page image URL missing: %s", w.Body.String())
	}

	req = httptest.NewRequest(http.MethodGet, "/assets/"+shareID+"?ref=Pasted%20image.png", nil)
	w = httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Code != http.StatusOK || w.Body.String() != "image-bytes" {
		t.Errorf("asset response: status=%d body=%q", w.Code, w.Body.String())
	}
}

func TestSharedBlogServesStandardMarkdownImage(t *testing.T) {
	srv, _, _ := newTestServer(t)
	router := srv.Router()
	token := registerAndLogin(t, router, "standardimage", "password123")
	uploadFile(t, router, token, "Notes/Post.md", "![diagram](static/diagram.png)", 1700000000000)
	uploadFile(t, router, token, "static/diagram.png", "diagram-bytes", 1700000000001)

	_, body := doJSON(t, router, "POST", "/api/shares", token, map[string]any{
		"target_path": "Notes/Post.md", "is_folder": false,
	})
	shareID := body["share_id"].(string)
	req := httptest.NewRequest(http.MethodGet, "/assets/"+shareID+"?ref=static/diagram.png", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Code != http.StatusOK || w.Body.String() != "diagram-bytes" {
		t.Errorf("standard image response: status=%d body=%q", w.Code, w.Body.String())
	}
}

func TestSharedBlogResolvesQualifiedImagePathExactly(t *testing.T) {
	srv, _, _ := newTestServer(t)
	router := srv.Router()
	token := registerAndLogin(t, router, "qualifiedimage", "password123")
	uploadFile(t, router, token, "Post.md", "![diagram](public/diagram.png)", 1700000000000)
	uploadFile(t, router, token, "public/diagram.png", "public-bytes", 1700000000001)
	uploadFile(t, router, token, "private/diagram.png", "private-bytes", 1700000000002)

	_, body := doJSON(t, router, "POST", "/api/shares", token, map[string]any{
		"target_path": "Post.md", "is_folder": false,
	})
	shareID := body["share_id"].(string)
	req := httptest.NewRequest(http.MethodGet, "/assets/"+shareID+"?ref=public/diagram.png", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Code != http.StatusOK || w.Body.String() != "public-bytes" {
		t.Errorf("qualified image response: status=%d body=%q", w.Code, w.Body.String())
	}
}

func TestSharedBlogRejectsUnreferencedImage(t *testing.T) {
	srv, _, _ := newTestServer(t)
	router := srv.Router()
	token := registerAndLogin(t, router, "privateimage", "password123")
	uploadFile(t, router, token, "Post.md", "# Public", 1700000000000)
	uploadFile(t, router, token, "secret.png", "secret", 1700000000001)
	_, body := doJSON(t, router, "POST", "/api/shares", token, map[string]any{
		"target_path": "Post.md", "is_folder": false,
	})
	shareID := body["share_id"].(string)
	req := httptest.NewRequest(http.MethodGet, "/assets/"+shareID+"?ref=secret.png", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Code != http.StatusNotFound {
		t.Errorf("unreferenced asset: got %d want 404", w.Code)
	}
}

func TestSharedBlogDoesNotProxyRemoteImage(t *testing.T) {
	srv, _, _ := newTestServer(t)
	router := srv.Router()
	token := registerAndLogin(t, router, "remoteimage", "password123")
	uploadFile(t, router, token, "Post.md", "![remote](https://example.com/image.png)", 1700000000000)
	_, body := doJSON(t, router, "POST", "/api/shares", token, map[string]any{
		"target_path": "Post.md", "is_folder": false,
	})
	shareID := body["share_id"].(string)
	req := httptest.NewRequest(http.MethodGet, "/p/"+shareID, nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if !strings.Contains(w.Body.String(), `src="https://example.com/image.png"`) {
		t.Fatalf("remote image URL was rewritten: %s", w.Body.String())
	}
}

func TestDefaultThemeCSSAvailable(t *testing.T) {
	srv, _, _ := newTestServer(t)
	req := httptest.NewRequest(http.MethodGet, "/themes/default/style.css", nil)
	w := httptest.NewRecorder()
	srv.Router().ServeHTTP(w, req)
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), ".oss-content") {
		t.Errorf("default CSS: status=%d body=%q", w.Code, w.Body.String())
	}
}

<<<<<<< HEAD
func TestPublicRoutesDoNotRenderCustomFragmentsWhenPolicyDisabled(t *testing.T) {
	t.Chdir(t.TempDir())
	srv, db, _ := newTestServer(t)
	router := srv.Router()
	setCustomFragmentsEnabledForTest(t, db, true)

	ownerToken := registerAndLogin(t, router, "disabled-policy-owner", "password123")
	vaultID := defaultVaultIDFromAPI(t, router, ownerToken)
	uploadViaV1(t, router, ownerToken, "Posts/Hello.md", "# Hello\n")
	uploadViaV1(t, router, ownerToken, "Folder/Nested.md", "# Nested\n")

	code, body := doJSON(t, router, http.MethodPost, "/api/shares", ownerToken, map[string]any{
		"vault_id": vaultID, "target_path": "Posts/Hello.md", "allow_copy": true,
	})
	if code != http.StatusOK {
		t.Fatalf("create article share: %d %v", code, body)
	}
	shareID := body["share_id"].(string)

	code, body = doJSON(t, router, http.MethodPost, "/api/shares", ownerToken, map[string]any{
		"vault_id": vaultID, "target_path": "Folder", "is_folder": true,
	})
	if code != http.StatusOK {
		t.Fatalf("create folder share: %d %v", code, body)
	}
	folderShareID := body["share_id"].(string)

	header := "POLICY_DISABLED_HEADER_MARKER"
	footer := "POLICY_DISABLED_FOOTER_MARKER"
	var setting models.VaultSetting
	if err := db.Where("vault_id = ?", vaultID).First(&setting).Error; err != nil {
		if !errors.Is(err, gorm.ErrRecordNotFound) {
			t.Fatalf("load vault setting: %v", err)
		}
		if err := db.Create(&models.VaultSetting{
			VaultID:           vaultID,
			ThemeName:         "default",
			RecycleBinDays:    0,
			IsPublicBlog:      true,
			CustomHeader:      header,
			CustomFooter:      footer,
			KeepDirectoryTree: true,
		}).Error; err != nil {
			t.Fatalf("seed vault custom fragments: %v", err)
		}
	} else if err := db.Model(&models.VaultSetting{}).Where("vault_id = ?", vaultID).Updates(map[string]any{
		"theme_name":       "default",
		"recycle_bin_days": 0,
		"is_public_blog":   true,
		"custom_header":    header,
		"custom_footer":    footer,
	}).Error; err != nil {
		t.Fatalf("seed vault custom fragments: %v", err)
	}

	setCustomFragmentsEnabledForTest(t, db, false)

	article := doForm(t, router, http.MethodGet, "/p/"+shareID, nil, nil)
	if article.Code != http.StatusOK {
		t.Fatalf("public article: %d body=%s", article.Code, article.Body)
	}
	if strings.Contains(article.Body.String(), header) || strings.Contains(article.Body.String(), footer) {
		t.Fatal("custom fragments leaked in article page when policy is disabled")
	}

	folderRoot := doForm(t, router, http.MethodGet, "/p/"+folderShareID+"/", nil, nil)
	if folderRoot.Code != http.StatusOK {
		t.Fatalf("folder share index: %d body=%s", folderRoot.Code, folderRoot.Body)
	}
	if strings.Contains(folderRoot.Body.String(), header) || strings.Contains(folderRoot.Body.String(), footer) {
		t.Fatal("custom fragments leaked in folder share index when policy is disabled")
	}

	folderDoc := doForm(t, router, http.MethodGet, "/p/"+folderShareID+"/Nested.md", nil, nil)
	if folderDoc.Code != http.StatusOK {
		t.Fatalf("folder share document: %d body=%s", folderDoc.Code, folderDoc.Body)
	}
	if strings.Contains(folderDoc.Body.String(), header) || strings.Contains(folderDoc.Body.String(), footer) {
		t.Fatal("custom fragments leaked in folder share document when policy is disabled")
	}

	b := doForm(t, router, http.MethodGet, "/b/"+vaultID, nil, nil)
	if b.Code != http.StatusOK {
		t.Fatalf("public blog home: %d body=%s", b.Code, b.Body)
	}
	if strings.Contains(b.Body.String(), header) || strings.Contains(b.Body.String(), footer) {
		t.Fatal("custom fragments leaked in public vault home when policy is disabled")
	}

	home := doForm(t, router, http.MethodGet, "/", nil, nil)
	if home.Code != http.StatusOK {
		t.Fatalf("public index: %d body=%s", home.Code, home.Body)
	}
	if strings.Contains(home.Body.String(), header) || strings.Contains(home.Body.String(), footer) {
		t.Fatal("custom fragments leaked in public index when policy is disabled")
	}
}

func TestPublicHomeEmptyStateLinksItsLayoutStyles(t *testing.T) {
	srv, _, _ := newTestServer(t)
	router := srv.Router()

	// 尚无公开 Vault 时，/ 渲染安全的空目录页。
	home := httptest.NewRecorder()
	router.ServeHTTP(home, httptest.NewRequest(http.MethodGet, "/", nil))
	if home.Code != http.StatusOK || !strings.Contains(home.Body.String(), "暂无公开博客") {
		t.Fatalf("empty public directory: status=%d body=%s", home.Code, home.Body.String())
	}

	// 页面引用的样式表必须包含占位页实际使用的布局类，
	// 否则首页会以浏览器默认样式呈现（无布局的裸 HTML）。
	match := regexp.MustCompile(`href="([^"]+\.css(?:\?[^"]*)?)"`).FindStringSubmatch(home.Body.String())
	if len(match) != 2 {
		t.Fatalf("no stylesheet link found in home page: %s", home.Body.String())
	}
	css := httptest.NewRecorder()
	router.ServeHTTP(css, httptest.NewRequest(http.MethodGet, match[1], nil))
	if css.Code != http.StatusOK {
		t.Fatalf("linked stylesheet %s: status=%d", match[1], css.Code)
	}
	for _, sel := range []string{".shell--auth", ".masthead", ".public-home__empty", ".button--primary"} {
		if !strings.Contains(css.Body.String(), sel) {
			t.Errorf("linked stylesheet %s does not define %s used by home page", match[1], sel)
		}
	}
}

=======
>>>>>>> 3b7aaacb143eff9df5a728b914a633fc58e70a6b
func TestCustomThemeRendersVaultBlogAndServesAssets(t *testing.T) {
	srv, db, dataDir := newTestServer(t)
	router := srv.Router()
	token := registerAndLogin(t, router, "custom-theme", "password123")
	vaultID := defaultVaultIDFromAPI(t, router, token)
	uploadFile(t, router, token, "Post.md", "# Custom title\n\nTheme body", 1700000000000)

	themeDir := filepath.Join(dataDir, "themes", "paper-cut")
	if err := os.MkdirAll(themeDir, 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(themeDir, "template.html"), []byte(`<!doctype html><title>{{.Title}}</title><main data-theme="paper-cut">{{.ContentHTML}}</main><link rel="stylesheet" href="{{.ThemeBaseURL}}/style.css">`), 0o640); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(themeDir, "style.css"), []byte("main[data-theme] { color: teal; }"), 0o640); err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&models.VaultSetting{}).Where("vault_id = ?", vaultID).Update("theme_name", "paper-cut").Error; err != nil {
		t.Fatal(err)
	}

	_, share := doJSON(t, router, http.MethodPost, "/api/shares", token, map[string]any{"target_path": "Post.md", "is_folder": false})
	shareID := share["share_id"].(string)
	page := httptest.NewRecorder()
	router.ServeHTTP(page, httptest.NewRequest(http.MethodGet, "/p/"+shareID, nil))
	if page.Code != http.StatusOK || !strings.Contains(page.Body.String(), `data-theme="paper-cut"`) || !strings.Contains(page.Body.String(), "Theme body") {
		t.Fatalf("custom page: status=%d body=%s", page.Code, page.Body.String())
	}
	asset := httptest.NewRecorder()
	router.ServeHTTP(asset, httptest.NewRequest(http.MethodGet, "/themes/paper-cut/style.css", nil))
	if asset.Code != http.StatusOK || !strings.Contains(asset.Body.String(), "teal") {
		t.Fatalf("custom CSS: status=%d body=%q", asset.Code, asset.Body.String())
	}
}

func TestFolderShareEscapesLikeWildcardsAndLinkAttributes(t *testing.T) {
	srv, _, _ := newTestServer(t)
	router := srv.Router()
	token := registerAndLogin(t, router, "folder-safe", "password123")
	uploadFile(t, router, token, `A_/safe.md`, "# safe", 1700000000000)
<<<<<<< HEAD
	uploadFile(t, router, token, `A_/quote#.md`, "# quote", 1700000000001)
=======
	uploadFile(t, router, token, `A_/quote".md`, "# quote", 1700000000001)
>>>>>>> 3b7aaacb143eff9df5a728b914a633fc58e70a6b
	uploadFile(t, router, token, `AB/secret.md`, "# secret", 1700000000002)
	code, body := doJSON(t, router, http.MethodPost, "/api/shares", token, map[string]any{"target_path": "A_", "is_folder": true})
	if code != http.StatusOK {
		t.Fatalf("create folder share: %d %v", code, body)
	}
	shareID := body["share_id"].(string)
	req := httptest.NewRequest(http.MethodGet, "/p/"+shareID+"/", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	page := w.Body.String()
	if w.Code != http.StatusOK || strings.Contains(page, "secret") {
		t.Fatalf("folder share leaked wildcard sibling: %d %s", w.Code, page)
	}
<<<<<<< HEAD
	if !strings.Contains(page, "%23") || strings.Contains(page, `href="/p/`+shareID+`/quote#`) {
=======
	if !strings.Contains(page, "%22") || strings.Contains(page, `href="/p/`+shareID+`/quote"`) {
>>>>>>> 3b7aaacb143eff9df5a728b914a633fc58e70a6b
		t.Fatalf("folder link was not safely escaped: %s", page)
	}
}
