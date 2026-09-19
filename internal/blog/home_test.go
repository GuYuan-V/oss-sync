package blog

import "testing"

func TestExtractPostMetaUsesFilenameWhenFirstHeadingIsDirectory(t *testing.T) {
	raw := "---\ntags:\n  - sport\n---\n### 目录\n\n[[# 基础使用教程]]\n\n# 基础使用教程\n"
	title, summary := extractPostMeta(raw, "sport/sport使用手册.md")

	if title != "sport使用手册" {
		t.Fatalf("title = %q, want filename without extension", title)
	}
	if summary != "[[# 基础使用教程]]" {
		t.Fatalf("summary = %q, want first non-heading paragraph", summary)
	}
}

func TestExtractPostMetaUsesExplicitFrontmatterTitle(t *testing.T) {
	title, _ := extractPostMeta("---\ntitle: Custom title\n---\nBody", "folder/file.md")
	if title != "Custom title" {
		t.Fatalf("title = %q, want frontmatter title", title)
	}
}

func TestExtractPostMetaDoesNotTreatBodyRuleAsFrontmatter(t *testing.T) {
	title, summary := extractPostMeta("Introduction\n\n---\n\nMore", "folder/file.md")
	if title != "file" || summary != "Introduction" {
		t.Fatalf("title = %q, summary = %q", title, summary)
	}
}
