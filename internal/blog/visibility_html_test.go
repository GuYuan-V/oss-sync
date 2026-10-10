package blog

import (
	"strings"
	"testing"

	"github.com/helantianshen/oss-sync/internal/markdown"
)

// 只允许 public/password/login 三种可见性
func TestShareVisibilityValidation(t *testing.T) {
	valid := []string{"public", "password", "login"}
	for _, v := range valid {
		if !isValidShareVisibility(v) {
			t.Errorf("visibility %q should be valid", v)
		}
	}
	invalid := []string{"", "private", "protected", "PUBLIC"}
	for _, v := range invalid {
		if isValidShareVisibility(v) {
			t.Errorf("visibility %q should be invalid", v)
		}
	}
}

// 文章内 HTML 在允许时保留标签，默认转义
func TestMarkdownUnsafePreservesHTML(t *testing.T) {
	html, err := markdown.RenderMarkdownWithAssetsUnsafe(nil, nil, `<script>alert(1)</script><strong>bold</strong>`)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(html, `<script>`) {
		t.Errorf("unsafe render must preserve script tags, got %q", html)
	}
	if !strings.Contains(html, `<strong>bold</strong>`) {
		t.Errorf("unsafe render must preserve strong tags, got %q", html)
	}
}

func TestMarkdownSafeEscapesHTML(t *testing.T) {
	html, err := markdown.RenderMarkdownWithAssets(nil, nil, `<script>alert(1)</script><strong>bold</strong>`)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(html, `<script>`) {
		t.Errorf("safe render must escape script tags, got %q", html)
	}
}
