package blog

import (
	"bytes"
	"context"
	"html/template"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
)

// stubDataRunner 模拟宿主的 blog.data 钩子收集器
type stubDataRunner struct {
	data map[string]any
	last PluginDataPayload
}

func (s *stubDataRunner) ApplyHookData(_ context.Context, _ string, payload PluginDataPayload) (map[string]any, error) {
	s.last = payload
	return s.data, nil
}

func firstValue(values map[string][]string, key string) string {
	if len(values[key]) == 0 {
		return ""
	}
	return values[key][0]
}

func TestCollectPluginDataPassesContextAndData(t *testing.T) {
	runner := &stubDataRunner{data: map[string]any{"vip": map[string]any{"Unlocked": true}}}
	h := &Handler{pluginData: runner}
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest("GET", "/p/s1?preview=1", nil)
	c.Request.Header.Set("X-Demo", "demo-value")
	c.Request.AddCookie(&http.Cookie{Name: "vip_session", Value: "member-1"})
	c.Request.RemoteAddr = "192.0.2.25:1234"
	got := h.collectPluginData(c, renderParams{VaultID: "v1", ShareID: "s1", FilePath: "Posts/a.md", ThemeName: "shirone"})
	if got["vip"].(map[string]any)["Unlocked"] != true {
		t.Fatalf("plugin data not passed through: %#v", got)
	}
	if runner.last.VaultID != "v1" || runner.last.ShareID != "s1" || runner.last.Path != "Posts/a.md" ||
		runner.last.Method != "GET" || firstValue(runner.last.Query, "preview") != "1" ||
		firstValue(runner.last.Headers, "X-Demo") != "demo-value" || runner.last.Cookies["vip_session"] != "member-1" ||
		runner.last.ClientIP != "192.0.2.25" {
		t.Fatalf("payload context missing: %#v", runner.last)
	}
}

func TestCollectPluginDataNilRunnerReturnsEmpty(t *testing.T) {
	h := &Handler{}
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest("GET", "/p/s1", nil)
	if got := h.collectPluginData(c, renderParams{}); got == nil || len(got) != 0 {
		t.Fatalf("nil runner should yield empty map, got %#v", got)
	}
}

// TestCustomThemePluginDataContract 固定模板对插件数据的访问契约：
// 已装插件字段可直接读取；safeHTML 注入富文本；未装插件用 hasPlugin/with 守卫且不报错
func TestCustomThemePluginDataContract(t *testing.T) {
	const tpl = `{{with index .PluginData "vip-access"}}{{if .Unlocked}}FULL{{else}}PAYWALL{{end}}{{end}}|` +
		`{{safeHTML (pluginField .PluginData "html-widget" "Html")}}|` +
		`{{if hasPlugin .PluginData "ghost-plugin"}}HAS{{else}}NONE{{end}}|` +
		`{{with index .PluginData "ghost-plugin"}}{{.X}}{{end}}|` +
		`{{with index .PluginData "comments-plugin"}}{{if .Enabled}}{{range .List}}{{.Author}};{{end}}{{end}}{{end}}`
	parsed, err := template.New("custom-theme").Funcs(customThemeFuncs()).Option("missingkey=zero").Parse(tpl)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	params := renderParams{PluginData: map[string]any{
		"vip-access":  map[string]any{"Unlocked": true},
		"html-widget": map[string]any{"Html": "<b>hi</b>"},
		"comments-plugin": map[string]any{
			"Enabled": true,
			"List":    []any{map[string]any{"Author": "a"}, map[string]any{"Author": "b"}},
		},
	}}
	var buf bytes.Buffer
	if err := parsed.Execute(&buf, params); err != nil {
		t.Fatalf("execute: %v", err)
	}
	got := buf.String()
	want := "FULL|<b>hi</b>|NONE||a;b;"
	if got != want {
		t.Fatalf("output = %q, want %q", got, want)
	}
	if strings.Contains(got, "&lt;b&gt;") {
		t.Fatal("safeHTML must not escape trusted plugin html")
	}
}

// TestCustomThemeMissingPluginKeyDoesNotErrorUnderZero 保证未装插件的守卫访问不触发整页失败
func TestCustomThemeMissingPluginKeyDoesNotErrorUnderZero(t *testing.T) {
	parsed, err := template.New("custom-theme").Funcs(customThemeFuncs()).Option("missingkey=zero").
		Parse(`{{with index .PluginData "absent-plugin"}}{{.Whatever}}{{end}}ok`)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	var buf bytes.Buffer
	if err := parsed.Execute(&buf, renderParams{PluginData: map[string]any{}}); err != nil {
		t.Fatalf("missing plugin key must not error: %v", err)
	}
	if buf.String() != "ok" {
		t.Fatalf("output = %q", buf.String())
	}
}

func TestSanitizeHeaderValue(t *testing.T) {
	if got := sanitizeHeaderValue("theme: bad\r\nInjected: x"); strings.ContainsAny(got, "\r\n") {
		t.Fatalf("header value retains CRLF: %q", got)
	}
	long := strings.Repeat("x", 500)
	if got := sanitizeHeaderValue(long); len(got) != 200 {
		t.Fatalf("header value not truncated: len=%d", len(got))
	}
}
