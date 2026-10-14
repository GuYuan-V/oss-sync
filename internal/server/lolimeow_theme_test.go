package server

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/helantianshen/oss-sync/internal/models"
)

// lolimeowThemeDir 返回插件仓库中的 LoliMeow 主题资源目录，未设置环境变量时跳过
func lolimeowThemeDir(t *testing.T) string {
	t.Helper()
	dir := os.Getenv("LOLIMEOW_THEME_DIR")
	if dir == "" {
		t.Skip("LOLIMEOW_THEME_DIR not set")
	}
	if _, err := os.Stat(filepath.Join(dir, "template.html")); err != nil {
		t.Fatalf("theme template missing: %v", err)
	}
	return dir
}

// copyThemeTree 递归复制主题资源到目标目录
func copyThemeTree(t *testing.T, source, target string) {
	t.Helper()
	if err := os.MkdirAll(target, 0o750); err != nil {
		t.Fatal(err)
	}
	entries, err := os.ReadDir(source)
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		from := filepath.Join(source, entry.Name())
		to := filepath.Join(target, entry.Name())
		if entry.IsDir() {
			copyThemeTree(t, from, to)
			continue
		}
		raw, err := os.ReadFile(from)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(to, raw, 0o640); err != nil {
			t.Fatal(err)
		}
	}
}

// TestLolimeowThemeRendersThroughHost 用宿主真实渲染链路验证移植的 LoliMeow 模板
func TestLolimeowThemeRendersThroughHost(t *testing.T) {
	srv, db, dataDir := newTestServer(t)
	router := srv.Router()
	const themeName = "lolimeow--lolimeow"

	source := lolimeowThemeDir(t)
	copyThemeTree(t, source, filepath.Join(dataDir, "themes", themeName))

	token := registerAndLogin(t, router, "lolimeow-theme", "password123")
	vaultID := defaultVaultIDFromAPI(t, router, token)
	uploadFile(t, router, token, "Posts/hello.md",
		"---\ntitle: 你好\ndescription: 第一篇摘要\ncategory: 随笔\ntags: [go, blog]\n---\n\n# 标题一\n\n正文内容一\n\n## 标题二\n\n正文内容二\n",
		1700000000000)
	uploadFile(t, router, token, "Posts/second.md",
		"---\ntitle: 第二篇\ncategory: 技术\ntags: [oss]\n---\n\n第二篇正文\n",
		1700000001000)
	uploadFile(t, router, token, "Notes/folder.md", "# 目录文章\n\n目录正文\n", 1700000002000)

	if err := db.Model(&models.VaultSetting{}).Where("vault_id = ?", vaultID).
		Update("theme_name", themeName).Error; err != nil {
		t.Fatal(err)
	}

	// 单篇文章
	_, share := doJSON(t, router, http.MethodPost, "/api/shares", token, map[string]any{"target_path": "Posts/hello.md", "is_folder": false})
	shareID := share["share_id"].(string)
	page := httptest.NewRecorder()
	router.ServeHTTP(page, httptest.NewRequest(http.MethodGet, "/p/"+shareID, nil))
	if page.Code != http.StatusOK {
		t.Fatalf("article page: status=%d body=%s", page.Code, page.Body.String())
	}
	if fallback := page.Header().Get("X-Theme-Fallback"); fallback != "" {
		t.Fatalf("article theme fell back to default: %s", fallback)
	}
	body := page.Body.String()
	for _, want := range []string{
		"boxmoe_header_banner", "single-title", "你好", "随笔",
		"single-content", "标题一", "post-toc-container", "single-tags",
		"assets/css/theme.min.css", "assets/css/style.css",
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("article page missing %q", want)
		}
	}

	// 文件夹目录
	_, folder := doJSON(t, router, http.MethodPost, "/api/shares", token, map[string]any{"target_path": "Notes", "is_folder": true})
	folderID := folder["share_id"].(string)
	folderPage := httptest.NewRecorder()
	router.ServeHTTP(folderPage, httptest.NewRequest(http.MethodGet, "/p/"+folderID+"/", nil))
	if folderPage.Code != http.StatusOK {
		t.Fatalf("folder page: status=%d body=%s", folderPage.Code, folderPage.Body.String())
	}
	if fallback := folderPage.Header().Get("X-Theme-Fallback"); fallback != "" {
		t.Fatalf("folder theme fell back to default: %s", fallback)
	}
	if !strings.Contains(folderPage.Body.String(), "oss-tree-list") {
		t.Fatal("folder page missing tree list")
	}

	// 博客首页只收录已单篇分享的文章
	if _, second := doJSON(t, router, http.MethodPost, "/api/shares", token, map[string]any{"target_path": "Posts/second.md", "is_folder": false}); second["share_id"] == nil {
		t.Fatalf("create second share: %v", second)
	}
	if err := db.Model(&models.VaultSetting{}).Where("vault_id = ?", vaultID).
		Update("is_public_blog", true).Error; err != nil {
		t.Fatal(err)
	}
	home := httptest.NewRecorder()
	router.ServeHTTP(home, httptest.NewRequest(http.MethodGet, "/b/"+vaultID, nil))
	if home.Code != http.StatusOK {
		t.Fatalf("home page: status=%d body=%s", home.Code, home.Body.String())
	}
	if fallback := home.Header().Get("X-Theme-Fallback"); fallback != "" {
		t.Fatalf("home theme fell back to default: %s", fallback)
	}
	homeBody := home.Body.String()
	for _, want := range []string{"你好", "第二篇", "第一篇摘要", "post-list", "lolimeow-posts"} {
		if !strings.Contains(homeBody, want) {
			t.Fatalf("home page missing %q", want)
		}
	}
	if strings.Contains(homeBody, "oss-tree-list") {
		t.Fatal("home page must not render the folder tree")
	}

	// 评论区已整体移除：文章页不得出现评论容器、表单或锚点
	for _, forbidden := range []string{
		`id="comments-container"`,
		`id="commentform"`,
		`id="comment"`,
		`class="post-comments"`,
		`class="comment-form`,
	} {
		if strings.Contains(body, forbidden) {
			t.Fatalf("comment markup must be removed, found %q", forbidden)
		}
	}

	// 静态资源可达且非空
	for _, asset := range []string{
		"/themes/" + themeName + "/assets/css/theme.min.css",
		"/themes/" + themeName + "/assets/css/style.css",
		"/themes/" + themeName + "/assets/css/fancybox.min.css",
		"/themes/" + themeName + "/assets/js/boxmoe.js",
		"/themes/" + themeName + "/assets/js/theme.min.js",
		"/themes/" + themeName + "/assets/js/lib.min.js",
		"/themes/" + themeName + "/assets/images/banner.jpg",
		"/themes/" + themeName + "/assets/images/favicon.ico",
		"/themes/" + themeName + "/assets/images/top/meow.gif",
		"/themes/" + themeName + "/assets/images/random/1.jpg",
		"/themes/" + themeName + "/assets/fonts/fontawesome-webfont.woff2",
		"/themes/" + themeName + "/theme.js",
		"/themes/" + themeName + "/style.css",
		"/themes/" + themeName + "/theme.json",
	} {
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, asset, nil))
		if rec.Code != http.StatusOK {
			t.Fatalf("asset %s: status=%d", asset, rec.Code)
		}
		if rec.Body.Len() == 0 {
			t.Fatalf("asset %s is empty", asset)
		}
	}
}
