package server

import (
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	"github.com/oss/oss-server/internal/auth"
)

// consoleHeadingPages 覆盖全部 20 个已登录控制台模板，断言标题标签契约：
// 布局顶栏和页面标题使用 h1，面板标题使用 h2；视觉字号由 CSS 独立控制。
func TestConsoleHeadingHierarchy(t *testing.T) {
	t.Chdir(t.TempDir())
	srv, db, _ := newTestServer(t)
	router := srv.Router()
	// 先建管理员，避免后续注册账户被自动提升为 admin。
	if _, err := auth.CreateAccount(db, "root", "admin-password-123", "admin"); err != nil {
		t.Fatal(err)
	}
	if _, err := auth.CreateAccount(db, "nobody", "password123", "user"); err != nil {
		t.Fatal(err)
	}
	userSession, userCSRF := webLogin(t, router, "nobody", "password123")
	adminSession, adminCSRF := webLogin(t, router, "root", "admin-password-123")

	ownerToken := registerAndLogin(t, router, "owner", "password123")
	vaultID := defaultVaultIDFromAPI(t, router, ownerToken)
	ownerSession, ownerCSRF := webLogin(t, router, "owner", "password123")

	pages := []struct {
		path    string
		session *http.Cookie
		csrf    *http.Cookie
	}{
		{"/dashboard", userSession, userCSRF},
		{"/dashboard/vaults", userSession, userCSRF},
		{"/dashboard/vaults/new", userSession, userCSRF},
		{"/dashboard/devices", userSession, userCSRF},
		{"/dashboard/account", userSession, userCSRF},
		{"/dashboard/vaults/" + vaultID, ownerSession, ownerCSRF},
		{"/dashboard/vaults/" + vaultID + "/shares", ownerSession, ownerCSRF},
		{"/dashboard/vaults/" + vaultID + "/recycle", ownerSession, ownerCSRF},
		{"/dashboard/vaults/" + vaultID + "/history", ownerSession, ownerCSRF},
		{"/dashboard/vaults/" + vaultID + "/members", ownerSession, ownerCSRF},
		{"/dashboard/vaults/" + vaultID + "/settings", ownerSession, ownerCSRF},
		{"/dashboard/vaults/" + vaultID + "/theme-settings", ownerSession, ownerCSRF},
		{"/dashboard/admin", adminSession, adminCSRF},
		{"/dashboard/admin/vaults", adminSession, adminCSRF},
		{"/dashboard/admin/vaults/" + vaultID, adminSession, adminCSRF},
		{"/dashboard/admin/devices", adminSession, adminCSRF},
		{"/dashboard/admin/data", adminSession, adminCSRF},
		{"/dashboard/admin/system", adminSession, adminCSRF},
		{"/dashboard/admin/themes", adminSession, adminCSRF},
		{"/dashboard/admin/console-themes", adminSession, adminCSRF},
	}
	for _, pg := range pages {
		t.Run(pg.path, func(t *testing.T) {
			res := doForm(t, router, http.MethodGet, pg.path, nil, pg.session, pg.csrf)
			if res.Code != http.StatusOK {
				t.Fatalf("%s: status=%d body=%s", pg.path, res.Code, res.Body)
			}
			body := res.Body.String()
			if got := strings.Count(body, "<h1"); got != 2 {
				t.Errorf("%s: h1 count = %d, want 2 (layout topbar + page heading)", pg.path, got)
			}
			block := dashboardHeadingBlock(t, body)
			if got := strings.Count(block, "<h1"); got != 1 {
				t.Errorf("%s: .dashboard-heading h1 count = %d, want 1", pg.path, got)
			}
			if strings.Contains(block, "<h2") || strings.Contains(block, "<h3") {
				t.Errorf("%s: .dashboard-heading must hold only the h1 page heading", pg.path)
			}
			if got := strings.Count(stripModals(body), "<h2"); got < 1 {
				t.Errorf("%s: h2 panel heading count = %d, want >= 1", pg.path, got)
			}
			if got := strings.Count(body, "<h3"); got != 0 {
				t.Errorf("%s: h3 count = %d, want 0", pg.path, got)
			}
		})
	}
}

// TestConsoleEmptyPanelStates 断言空面板使用 .panel-empty 标记，并在有数据时消失。
func TestConsoleEmptyPanelStates(t *testing.T) {
	t.Chdir(t.TempDir())
	srv, db, _ := newTestServer(t)
	router := srv.Router()
	if _, err := auth.CreateAccount(db, "root", "admin-password-123", "admin"); err != nil {
		t.Fatal(err)
	}
	if _, err := auth.CreateAccount(db, "nobody", "password123", "user"); err != nil {
		t.Fatal(err)
	}
	userSession, userCSRF := webLogin(t, router, "nobody", "password123")
	adminSession, adminCSRF := webLogin(t, router, "root", "admin-password-123")

	// 尚无任何数据时，各空面板渲染 .panel-empty。
	assertPanelEmptyCount(t, router, "/dashboard/admin/vaults", adminSession, adminCSRF, 1)
	assertPanelEmptyCount(t, router, "/dashboard/admin/devices", adminSession, adminCSRF, 1)
	assertPanelEmptyCount(t, router, "/dashboard/admin/data", adminSession, adminCSRF, 3)
	assertPanelEmptyCount(t, router, "/dashboard", userSession, userCSRF, 2)
	assertPanelEmptyCount(t, router, "/dashboard/vaults", userSession, userCSRF, 1)
	assertPanelEmptyCount(t, router, "/dashboard/devices", userSession, userCSRF, 1)

	// 创建仓库后：仓库级空面板。
	ownerToken := registerAndLogin(t, router, "owner", "password123")
	vaultID := defaultVaultIDFromAPI(t, router, ownerToken)
	ownerSession, ownerCSRF := webLogin(t, router, "owner", "password123")
	assertPanelEmptyCount(t, router, "/dashboard/vaults/"+vaultID, ownerSession, ownerCSRF, 1)
	assertPanelEmptyCount(t, router, "/dashboard/vaults/"+vaultID+"/history", ownerSession, ownerCSRF, 1)
	assertPanelEmptyCount(t, router, "/dashboard/vaults/"+vaultID+"/recycle", ownerSession, ownerCSRF, 1)
	assertPanelEmptyCount(t, router, "/dashboard/vaults/"+vaultID+"/shares", ownerSession, ownerCSRF, 1)

	// 有数据后空状态消失（数据驱动切换）。
	vaultsPage := doForm(t, router, http.MethodGet, "/dashboard/vaults", nil, ownerSession, ownerCSRF)
	if strings.Contains(vaultsPage.Body.String(), `class="panel-empty"`) {
		t.Error("vaults page with 1 vault must not render .panel-empty")
	}
	adminVaults := doForm(t, router, http.MethodGet, "/dashboard/admin/vaults", nil, adminSession, adminCSRF)
	if strings.Contains(adminVaults.Body.String(), `class="panel-empty"`) {
		t.Error("admin vaults page with 1 vault must not render .panel-empty")
	}
}

// TestConsoleThemeControlsUseSharedButton 断言侧边栏/登录/注册的主题按钮
// 使用共享 button 基类，并保留 data-theme-pref、aria-pressed 与认证页 hero h1。
func TestConsoleThemeControlsUseSharedButton(t *testing.T) {
	t.Chdir(t.TempDir())
	srv, db, _ := newTestServer(t)
	router := srv.Router()
	if _, err := auth.CreateAccount(db, "console-user", "password123", "user"); err != nil {
		t.Fatal(err)
	}
	const wantClass = `class="button theme-switcher__btn"`

	login := doForm(t, router, http.MethodGet, "/login", nil, nil)
	if got := strings.Count(login.Body.String(), wantClass); got != 3 {
		t.Errorf("login theme buttons with shared class = %d, want 3", got)
	}
	register := doForm(t, router, http.MethodGet, "/register", nil, nil)
	if got := strings.Count(register.Body.String(), wantClass); got != 3 {
		t.Errorf("register theme buttons with shared class = %d, want 3", got)
	}

	session, csrf := webLogin(t, router, "console-user", "password123")
	dashboard := doForm(t, router, http.MethodGet, "/dashboard", nil, session, csrf)
	if got := strings.Count(dashboard.Body.String(), wantClass); got != 3 {
		t.Errorf("sidebar theme buttons with shared class = %d, want 3", got)
	}

	for name, body := range map[string]string{
		"login":     login.Body.String(),
		"register":  register.Body.String(),
		"dashboard": dashboard.Body.String(),
	} {
		if strings.Contains(body, `class="theme-switcher__btn"`) {
			t.Errorf("%s: theme button missing shared button classes", name)
		}
		for _, attr := range []string{
			`data-theme-pref="auto"`, `data-theme-pref="light"`, `data-theme-pref="dark"`,
			`aria-pressed="false"`,
		} {
			if !strings.Contains(body, attr) {
				t.Errorf("%s: missing %s", name, attr)
			}
		}
	}

	// 认证页 hero h1 保持不变。
	if !strings.Contains(login.Body.String(), `<h1>登录你的同步账本。</h1>`) ||
		!strings.Contains(login.Body.String(), `<h1>一个账户，多台设备。</h1>`) {
		t.Error("login hero h1 headings changed")
	}
	if !strings.Contains(register.Body.String(), `<h1>为你的笔记建立一个同步身份。</h1>`) {
		t.Error("register hero h1 heading changed")
	}
}

// TestConsoleCSSSelectorsForConsistency 断言控制台 CSS 的选择器契约。
func TestConsoleCSSSelectorsForConsistency(t *testing.T) {
	srv, _, _ := newTestServer(t)
	router := srv.Router()
	res := doForm(t, router, http.MethodGet, "/ui/assets/console.css", nil, nil)
	if res.Code != http.StatusOK {
		t.Fatalf("console.css: %d", res.Code)
	}
	css := res.Body.String()
	for _, want := range []string{
		".dashboard-heading h1",
		"font-size: clamp(28px, 3.5vw, 34px)",
		".gate-panel h2, .ledger-panel h2",
		"font-size: clamp(21px, 3vw, 24px)",
		".panel-empty",
		"align-items: center",
		"justify-items: start",
		"text-align: left",
		"min-height: 96px",
		".overview-grid { grid-template-columns: repeat(2, minmax(0, 1fr)); }",
		".masthead { flex-wrap: wrap; min-height: 76px;",
		".masthead-actions .theme-switcher { width: 100%; }",
		".button",
		".button.is-active",
		`:root[data-theme="dark"] .button.is-active`,
		".text-button:focus-visible",
		".theme-switcher__btn",
		".table-wrap { overflow-x: auto; }",
		".table-wrap table { min-width: 640px; }",
	} {
		if !strings.Contains(css, want) {
			t.Errorf("console.css missing %q", want)
		}
	}
	for _, gone := range []string{
		".dashboard-heading h2",
		".gate-panel h3",
		".ledger-panel h3",
		".button--compact",
		".theme-switcher__btn.is-active",
	} {
		if strings.Contains(css, gone) {
			t.Errorf("console.css must not contain stale selector %q", gone)
		}
	}
	for _, rule := range []struct {
		selector     string
		declarations []string
	}{
		{".theme-switcher__btn", []string{"min-height: 34px", "padding: 7px 8px", "font-size: 12px"}},
		{".vault-empty", []string{"text-align: center"}},
		{".device-auth-actions", []string{"display: flex", "flex-wrap: wrap", "gap: 12px"}},
		{".device-auth-form--empty .vault-empty", []string{"display: flex", "align-items: center", "min-height: 28px", "margin: 0", "padding: 0", "line-height: 1.2"}},
		{"tbody td", []string{"vertical-align: middle"}},
	} {
		if !cssRuleCarries(css, rule.selector, rule.declarations...) {
			t.Errorf("console.css: %s missing declarations %v", rule.selector, rule.declarations)
		}
	}
	// 网格直接子项必须允许收缩（min-width: 0），把宽表格的横向滚动
	// 交给 .table-wrap，否则 390px 视口下文档会被撑出横向溢出。
	for _, sel := range []string{".control-grid > *", ".gate-panel > *"} {
		if !cssRuleCarriesMinWidthZero(css, sel) {
			t.Errorf("console.css: %s must carry min-width: 0 so wide children shrink inside their grid", sel)
		}
	}
}

// TestConsoleDrawerAriaExpandedContract 断言移动端抽屉切换按钮的 aria-expanded
// 与抽屉开关状态同步：打开时置为 true，每个关闭路径（closeDrawer）重置为 false。
func TestConsoleDrawerAriaExpandedContract(t *testing.T) {
	srv, _, _ := newTestServer(t)
	router := srv.Router()
	res := doForm(t, router, http.MethodGet, "/ui/assets/app.js", nil, nil)
	if res.Code != http.StatusOK {
		t.Fatalf("app.js: %d", res.Code)
	}
	app := res.Body.String()
	if !strings.Contains(app, `setAttribute("aria-expanded", String(open))`) {
		t.Error("app.js: drawer toggle must set aria-expanded from the classList.toggle result")
	}
	if !strings.Contains(app, `setAttribute("aria-expanded", "false")`) {
		t.Error("app.js: closeDrawer must reset aria-expanded to false on every close path")
	}
}

// TestConsoleModalHeadingsRemainH2 断言模态框标题仍是 h2。
func TestConsoleModalHeadingsRemainH2(t *testing.T) {
	t.Chdir(t.TempDir())
	srv, db, _ := newTestServer(t)
	router := srv.Router()
	if _, err := auth.CreateAccount(db, "root", "admin-password-123", "admin"); err != nil {
		t.Fatal(err)
	}
	if _, err := auth.CreateAccount(db, "member", "password123", "user"); err != nil {
		t.Fatal(err)
	}
	adminSession, adminCSRF := webLogin(t, router, "root", "admin-password-123")

	// 非最后管理员 → 渲染重置密码模态框。
	users := doForm(t, router, http.MethodGet, "/dashboard/admin", nil, adminSession, adminCSRF)
	if !strings.Contains(users.Body.String(), `<h2 id="reset-title-`) {
		t.Error("admin users reset modal must keep its h2 title")
	}

	// 脚手架自定义模板 → 渲染编辑模态框。
	scaffold := doForm(t, router, http.MethodPost, "/dashboard/admin/themes/scaffold", url.Values{
		"base": {"default"}, "name": {"test-theme"},
	}, adminSession, adminCSRF)
	if scaffold.Code != http.StatusSeeOther {
		t.Fatalf("theme scaffold: %d body=%s", scaffold.Code, scaffold.Body)
	}
	themes := doForm(t, router, http.MethodGet, "/dashboard/admin/themes", nil, adminSession, adminCSRF)
	if !strings.Contains(themes.Body.String(), `<h2 id="theme-edit-title-test-theme">`) {
		t.Error("admin themes edit modal must keep its h2 title")
	}
}

// assertPanelEmptyCount 断言页面中 .panel-empty 的出现次数。
func assertPanelEmptyCount(t *testing.T, router *gin.Engine, path string, session, csrf *http.Cookie, want int) {
	t.Helper()
	res := doForm(t, router, http.MethodGet, path, nil, session, csrf)
	if res.Code != http.StatusOK {
		t.Fatalf("%s: status=%d body=%s", path, res.Code, res.Body)
	}
	if got := strings.Count(res.Body.String(), `class="panel-empty"`); got != want {
		t.Errorf("%s: .panel-empty count = %d, want %d", path, got, want)
	}
}

// stripModals 返回去掉 .modal 块后的正文，用于隔离页头/面板标题断言。
func stripModals(body string) string {
	if i := strings.Index(body, `<div class="modal"`); i >= 0 {
		return body[:i]
	}
	return body
}

// dashboardHeadingBlock 返回 .dashboard-heading 区块的原始 HTML。
func dashboardHeadingBlock(t *testing.T, body string) string {
	t.Helper()
	const open = `<section class="dashboard-heading">`
	start := strings.Index(body, open)
	if start < 0 {
		t.Fatalf("missing .dashboard-heading section")
	}
	end := strings.Index(body[start:], "</section>")
	if end < 0 {
		t.Fatalf("unterminated .dashboard-heading section")
	}
	return body[start : start+end]
}

// cssRuleCarriesMinWidthZero 断言包含 selector 的规则块同时声明 min-width: 0，
// 这是网格直接子项允许收缩、把横向溢出交给 .table-wrap 的契约。
func cssRuleCarriesMinWidthZero(css, selector string) bool {
	return cssRuleCarries(css, selector, "min-width: 0")
}

// cssRuleCarries 断言包含 selector 的同一规则块包含所有指定声明。
func cssRuleCarries(css, selector string, declarations ...string) bool {
	for _, block := range strings.Split(css, "}") {
		if !strings.Contains(block, selector) {
			continue
		}
		matched := true
		for _, declaration := range declarations {
			if !strings.Contains(block, declaration) {
				matched = false
				break
			}
		}
		if matched {
			return true
		}
	}
	return false
}
