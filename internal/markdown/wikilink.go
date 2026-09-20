// Package markdown 提供公开渲染用的 Goldmark 扩展。
package markdown

import (
	"fmt"
	"strings"

	"github.com/yuin/goldmark"
	gast "github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/extension"
	"github.com/yuin/goldmark/parser"
	"github.com/yuin/goldmark/renderer"
	"github.com/yuin/goldmark/renderer/html"
	"github.com/yuin/goldmark/text"
	"github.com/yuin/goldmark/util"
)

// Wikilink 是 Obsidian [[link]] 行内 AST 节点。
type Wikilink struct {
	gast.BaseInline
	RawText string
}

func (n *Wikilink) Kind() gast.NodeKind {
	return KindWikilink
}

func (n *Wikilink) Text(source []byte) []byte {
	return []byte(n.RawText)
}

func (n *Wikilink) Dump(source []byte, level int) {
	m := map[string]string{
		"Text": n.RawText,
	}
	gast.DumpHelper(n, source, level, m, nil)
}

// KindWikilink 标识 Goldmark AST 中的 Wikilink 节点。
var KindWikilink = gast.NewNodeKind("Wikilink")

type wikilinkParser struct{}

func newWikilinkParser() parser.InlineParser {
	return &wikilinkParser{}
}

func (p *wikilinkParser) Trigger() []byte {
	return []byte{'['}
}

// Parse 识别单行 Obsidian wikilink，其余输入交由 Goldmark 处理。
func (p *wikilinkParser) Parse(parent gast.Node, block text.Reader, pc parser.Context) gast.Node {
	line, segment := block.PeekLine()
	if len(line) < 2 || line[0] != '[' || line[1] != '[' {
		return nil
	}
	end := -1
	for i := 2; i < len(line); i++ {
		if line[i] == ']' && i+1 < len(line) && line[i+1] == ']' {
			end = i
			break
		}
	}
	if end < 0 {
		return nil
	}
	inner := string(line[2:end])
	if inner == "" {
		return nil
	}
	block.Advance(end + 2)
	_ = segment
	return &Wikilink{RawText: inner}
}

type wikilinkHTMLRenderer struct {
	resolver LinkResolver
}

// LinkResolver 将 wikilink 文本映射为分享 ID，未解析时返回空值。
type LinkResolver interface {
	Resolve(linkText string) (shareID string)
}

func newWikilinkHTMLRenderer(resolver LinkResolver) renderer.NodeRenderer {
	return &wikilinkHTMLRenderer{resolver: resolver}
}

func (r *wikilinkHTMLRenderer) RegisterFuncs(reg renderer.NodeRendererFuncRegisterer) {
	reg.Register(KindWikilink, r.renderWikilink)
}

func (r *wikilinkHTMLRenderer) renderWikilink(w util.BufWriter, source []byte, node gast.Node, entering bool) (gast.WalkStatus, error) {
	if !entering {
		return gast.WalkContinue, nil
	}
	n, ok := node.(*Wikilink)
	if !ok {
		return gast.WalkContinue, nil
	}
	rawText := n.RawText
	linkText := rawText
	displayText := rawText
	if idx := strings.IndexByte(rawText, '|'); idx >= 0 {
		linkText = rawText[:idx]
		displayText = rawText[idx+1:]
	}

	shareID := ""
	if r.resolver != nil {
		shareID = r.resolver.Resolve(linkText)
	}
	if shareID == "" {
		if strings.HasPrefix(linkText, "#") {
			heading := strings.TrimSpace(strings.TrimPrefix(linkText, "#"))
			label := heading
			if displayText != rawText {
				label = displayText
			}
			fmt.Fprintf(w, `<a href="#" data-local-heading="%s">%s</a>`, htmlEscape(heading), htmlEscape(label))
			return gast.WalkContinue, nil
		}
		fmt.Fprintf(w, `<span class="unshared-link">%s(未分享)</span>`, htmlEscape(displayText))
		return gast.WalkContinue, nil
	}
	fmt.Fprintf(w, `<a href="/p/%s" target="_blank">%s</a>`, shareID, htmlEscape(displayText))
	return gast.WalkContinue, nil
}

func htmlEscape(s string) string {
	return string(util.EscapeHTML([]byte(s)))
}

type wikilinkExtension struct {
	resolver LinkResolver
	assets   AssetResolver
}

func (e *wikilinkExtension) Extend(m goldmark.Markdown) {
	// Wikilink 解析优先于 Goldmark 内置链接解析注册。
	m.Parser().AddOptions(parser.WithInlineParsers(
		util.Prioritized(&imageEmbedParser{}, 50),
		util.Prioritized(newWikilinkParser(), 100),
	), parser.WithASTTransformers(
		util.Prioritized(&imageTransformer{resolver: e.assets}, 500),
	))
	m.Renderer().AddOptions(renderer.WithNodeRenderers(
		util.Prioritized(newWikilinkHTMLRenderer(e.resolver), 500),
		util.Prioritized(newImageHTMLRenderer(e.assets), 500),
	))
}

// NewWikilinkExtension 创建 wikilink 扩展，resolver 为 nil 时所有链接渲染为未分享占位。
func NewWikilinkExtension(resolver LinkResolver) goldmark.Extender {
	return &wikilinkExtension{resolver: resolver}
}

// NewMarkdown 创建带 wikilink 扩展的 Goldmark 实例。
func NewMarkdown(resolver LinkResolver) goldmark.Markdown {
	return newMarkdown(resolver, nil)
}

func newMarkdown(resolver LinkResolver, assets AssetResolver) goldmark.Markdown {
	return goldmark.New(
		goldmark.WithExtensions(extension.GFM, &wikilinkExtension{resolver: resolver, assets: assets}, &highlightExtension{}),
		goldmark.WithRendererOptions(html.WithHardWraps()),
	)
}

func RenderMarkdownWithAssets(resolver LinkResolver, assets AssetResolver, source string) (string, error) {
	md := newMarkdown(resolver, assets)
	var buf strings.Builder
	if err := md.Convert([]byte(source), &buf); err != nil {
		return "", err
	}
	return buf.String(), nil
}

// RenderMarkdown 用默认扩展集将 Markdown 渲染为 HTML。
func RenderMarkdown(resolver LinkResolver, source string) (string, error) {
	md := NewMarkdown(resolver)
	var buf strings.Builder
	if err := md.Convert([]byte(source), &buf); err != nil {
		return "", err
	}
	return buf.String(), nil
}
