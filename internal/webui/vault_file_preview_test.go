package webui

import "testing"

func TestPreviewKind(t *testing.T) {
	cases := map[string]string{
		"Notes/a.md":       "markdown",
		"b.markdown":       "markdown",
		"img/pic.PNG":      "image",
		"cover.webp":       "image",
		"photo.jpeg":       "image",
		"doc.pdf":          "pdf",
		"vector.svg":       "",
		"page.html":        "",
		"archive.zip":      "",
		"data.bin":         "",
	}
	for path, want := range cases {
		if got := previewKind(path); got != want {
			t.Errorf("previewKind(%q) = %q, want %q", path, got, want)
		}
	}
}

// TestInlineContentTypeRejectsExecutable 固定内联白名单：位图与 PDF 允许内联，
// SVG、HTML 等可被浏览器当作脚本执行的类型必须拒绝，避免控制台同源 XSS
func TestInlineContentTypeRejectsExecutable(t *testing.T) {
	allowed := map[string]string{
		"a.png":  "image/png",
		"a.jpg":  "image/jpeg",
		"a.jpeg": "image/jpeg",
		"a.gif":  "image/gif",
		"a.webp": "image/webp",
		"a.avif": "image/avif",
		"a.bmp":  "image/bmp",
		"a.ico":  "image/x-icon",
		"a.pdf":  "application/pdf",
	}
	for path, want := range allowed {
		got, ok := inlineContentType(path)
		if !ok || got != want {
			t.Errorf("inlineContentType(%q) = (%q,%v), want (%q,true)", path, got, ok, want)
		}
	}
	for _, path := range []string{"a.svg", "a.html", "a.htm", "a.xml", "a.js", "a.txt", "a.md", "a.zip"} {
		if got, ok := inlineContentType(path); ok {
			t.Errorf("inlineContentType(%q) = (%q,true), want not inlineable", path, got)
		}
	}
}
