package blog

import (
	"strings"
	"testing"
)

func TestSandboxArticleHTMLIsolatesScripts(t *testing.T) {
	content := `<p>hello</p><script>window.parent.document.cookie</script>`
	got := sandboxArticleHTML(content, "/themes/default")

	if !strings.Contains(got, `sandbox="allow-scripts"`) {
		t.Errorf("sandbox attribute missing allow-scripts: %q", got)
	}
	if strings.Contains(got, "allow-same-origin") {
		t.Errorf("must not grant allow-same-origin, got %q", got)
	}
	if !strings.Contains(got, `srcdoc="`) {
		t.Errorf("srcdoc missing: %q", got)
	}
	if !strings.Contains(got, "/themes/default/style.css") {
		t.Errorf("theme stylesheet missing: %q", got)
	}
	// 属性值里的双引号必须转义，否则会提前结束 srcdoc
	if strings.Count(got, `srcdoc="`) != 1 {
		t.Errorf("srcdoc attribute must be a single quoted value: %q", got)
	}
}

func TestSandboxDocumentCarriesContentClass(t *testing.T) {
	doc := sandboxDocument(`<link rel="stylesheet" href="/themes/default/style.css">`, `<p>body</p>`)
	for _, want := range []string{
		`<!doctype html>`,
		`<meta charset="utf-8">`,
		`/themes/default/style.css`,
		`<body class="oss-content">`,
		`<p>body</p>`,
	} {
		if !strings.Contains(doc, want) {
			t.Errorf("sandboxDocument() missing %q in %q", want, doc)
		}
	}
}

func TestSandboxArticleHTMLWithoutTheme(t *testing.T) {
	got := sandboxArticleHTML(`<p>x</p>`, "")
	if strings.Contains(got, "style.css") {
		t.Errorf("empty theme base must not emit a stylesheet link: %q", got)
	}
	if !strings.Contains(got, `sandbox="allow-scripts"`) {
		t.Errorf("sandbox must still be applied: %q", got)
	}
}

func TestHTMLEscapeAttributeCoversSpacesAndQuotes(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{`a b`, `a&#32;b`},
		{`"q"`, `&quot;q&quot;`},
		{`<x>`, `&lt;x&gt;`},
		{`a&b`, `a&amp;b`},
		{"a\nb", "a&#10;b"},
	}
	for _, tc := range cases {
		if got := htmlEscapeAttribute(tc.in); got != tc.want {
			t.Errorf("htmlEscapeAttribute(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}
