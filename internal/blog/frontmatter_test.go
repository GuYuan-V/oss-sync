package blog

import (
	"bytes"
	"html/template"
	"strings"
	"testing"
	"time"
)

func TestSplitFrontmatterParsesScalarBlock(t *testing.T) {
	raw := "---\ntitle: 一篇笔记\ndescription: 用于列表的摘要\npublished: 2026-09-22\ncategory: 随笔\ntags: [笔记, 生活]\nimage: 附件/封面.webp\n---\n正文第一行\n"
	fm, body := splitFrontmatter(raw)
	if fm.title != "一篇笔记" || fm.description != "用于列表的摘要" || fm.published != "2026-09-22" || fm.category != "随笔" || fm.image != "附件/封面.webp" {
		t.Fatalf("frontmatter = %+v", fm)
	}
	if len(fm.tags) != 2 || fm.tags[0] != "笔记" || fm.tags[1] != "生活" {
		t.Fatalf("tags = %v", fm.tags)
	}
	if body != "正文第一行\n" {
		t.Fatalf("body = %q", body)
	}
}

func TestSplitFrontmatterParsesIndentedTags(t *testing.T) {
	fm, _ := splitFrontmatter("---\ntags:\n  - sport\n  - 教程\n---\n正文")
	if len(fm.tags) != 2 || fm.tags[0] != "sport" || fm.tags[1] != "教程" {
		t.Fatalf("tags = %v", fm.tags)
	}
}

func TestSplitFrontmatterKeepsMalformedBlockAsContent(t *testing.T) {
	for _, raw := range []string{
		"---\ntitle: 未闭合\n正文",
		"--- junk\ntitle: 起始行不合法\n---\n正文",
		"引言在前\n---\ntitle: 不在开头\n---\n正文",
	} {
		fm, body := splitFrontmatter(raw)
		if body != raw {
			t.Fatalf("body = %q, want original %q", body, raw)
		}
		if fm.title != "" && !strings.HasPrefix(raw, "---\n") {
			t.Fatalf("fm = %+v for %q", fm, raw)
		}
	}
	// 未闭合时 title 可能已被解析，但正文必须保留原文
	if _, body := splitFrontmatter("---\ntitle: 未闭合\n正文"); body != "---\ntitle: 未闭合\n正文" {
		t.Fatalf("unclosed body = %q", body)
	}
}

func TestBuildArticleMetaDerivesFromBody(t *testing.T) {
	modified := time.Date(2026, 9, 20, 10, 0, 0, 0, time.UTC)
	title, meta := buildArticleMeta(frontmatter{}, "# 标题\n\n第一段正文。\n\n更多内容。", "folder/示例笔记.md", modified, nil)
	if title != "示例笔记" {
		t.Fatalf("title = %q", title)
	}
	if meta.Summary != "第一段正文。" {
		t.Fatalf("summary = %q", meta.Summary)
	}
	if meta.Date != "2026-09-20" {
		t.Fatalf("date = %q", meta.Date)
	}
	if meta.WordCount == 0 || meta.ReadingMinutes != 1 {
		t.Fatalf("words = %d minutes = %d", meta.WordCount, meta.ReadingMinutes)
	}
}

func TestBuildArticleMetaResolvesCoverFromAttachment(t *testing.T) {
	fm := frontmatter{image: "附件/封面.webp"}
	_, meta := buildArticleMeta(fm, "正文", "a.md", time.Time{}, func(ref string) string {
		return "/assets/s1?ref=" + ref
	})
	if meta.CoverURL != "/assets/s1?ref=附件/封面.webp" {
		t.Fatalf("cover = %q", meta.CoverURL)
	}
	// 远程封面直接使用，不经过附件解析
	_, remote := buildArticleMeta(frontmatter{image: "https://example.com/c.webp"}, "正文", "a.md", time.Time{}, nil)
	if remote.CoverURL != "https://example.com/c.webp" {
		t.Fatalf("remote cover = %q", remote.CoverURL)
	}
}

// 自定义主题（如 Shirone）依赖的模板字段契约：renderParams 必须始终提供这些字段，
// 缺失任一字段都会因 missingkey=error 导致自定义主题渲染失败并回退内置主题
func TestCustomThemeTemplateContract(t *testing.T) {
	const tpl = `{{.BannerURL}}|{{.MobileBannerURL}}|{{.ArticlePost.Summary}}|{{.ArticlePost.Date}}|{{.ArticlePost.Category}}|{{.ArticlePost.WordCount}}|{{.ArticlePost.ReadingMinutes}}|{{.ArticlePost.CoverURL}}|{{range .ArticlePost.Tags}}{{.}};{{end}}
{{range .HomePosts}}{{.Title}}|{{.Category}}|{{.WordCount}}|{{.CoverURL}}|{{range .Tags}}{{.}},{{end}};{{end}}`
	parsed, err := template.New("custom-theme").Option("missingkey=error").Parse(tpl)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	params := renderParams{
		BannerURL:       "https://example.com/banner.webp",
		MobileBannerURL: "https://example.com/banner-m.webp",
		ArticlePost: ArticleMeta{
			Summary: "摘要", Date: "2026-09-22", Category: "随笔",
			Tags: []string{"笔记"}, CoverURL: "/assets/s1?ref=cover.webp",
			WordCount: 800, ReadingMinutes: 2,
		},
		HomePosts: []HomePost{{
			Title: "一篇笔记", Category: "随笔", WordCount: 800,
			CoverURL: "/assets/s1?ref=cover.webp", Tags: []string{"笔记"},
		}},
	}
	var buf bytes.Buffer
	if err := parsed.Execute(&buf, params); err != nil {
		t.Fatalf("execute: %v", err)
	}
	out := buf.String()
	for _, want := range []string{
		"https://example.com/banner.webp", "https://example.com/banner-m.webp",
		"摘要", "2026-09-22", "随笔", "800", "2", "/assets/s1?ref=cover.webp", "笔记;",
		"一篇笔记|随笔|800|/assets/s1?ref=cover.webp|笔记,",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("output missing %q:\n%s", want, out)
		}
	}
}
