package markdown

import (
	"bytes"
	"fmt"
	"strings"

	"github.com/yuin/goldmark"
	gast "github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/parser"
	"github.com/yuin/goldmark/renderer"
	"github.com/yuin/goldmark/text"
	"github.com/yuin/goldmark/util"
)

type highlight struct {
	gast.BaseInline
	Raw string
}

var kindHighlight = gast.NewNodeKind("Highlight")

func (n *highlight) Kind() gast.NodeKind {
	return kindHighlight
}

func (n *highlight) Text(source []byte) []byte {
	return []byte(n.Raw)
}

func (n *highlight) Dump(source []byte, level int) {
	gast.DumpHelper(n, source, level, map[string]string{"Raw": n.Raw}, nil)
}

type highlightParser struct{}

func (p *highlightParser) Trigger() []byte {
	return []byte{'='}
}

func (p *highlightParser) Parse(parent gast.Node, block text.Reader, pc parser.Context) gast.Node {
	line, _ := block.PeekLine()
	if len(line) < 5 || !bytes.HasPrefix(line, []byte("==")) {
		return nil
	}
	end := bytes.Index(line[2:], []byte("=="))
	if end <= 0 {
		return nil
	}
	raw := string(line[2 : end+2])
	block.Advance(end + 4)
	return &highlight{Raw: raw}
}

type highlightRenderer struct{}

func (r *highlightRenderer) RegisterFuncs(reg renderer.NodeRendererFuncRegisterer) {
	reg.Register(kindHighlight, r.render)
}

func (r *highlightRenderer) render(w util.BufWriter, source []byte, node gast.Node, entering bool) (gast.WalkStatus, error) {
	if !entering {
		return gast.WalkContinue, nil
	}
	marked := node.(*highlight)
	content := htmlEscape(marked.Raw)
	if strings.HasPrefix(marked.Raw, "`") && strings.HasSuffix(marked.Raw, "`") && len(marked.Raw) > 2 {
		content = "<code>" + htmlEscape(marked.Raw[1:len(marked.Raw)-1]) + "</code>"
	}
	_, err := fmt.Fprintf(w, "<mark>%s</mark>", content)
	return gast.WalkContinue, err
}

type highlightExtension struct{}

func (e *highlightExtension) Extend(markdown goldmark.Markdown) {
	markdown.Parser().AddOptions(parser.WithInlineParsers(
		util.Prioritized(&highlightParser{}, 90),
	))
	markdown.Renderer().AddOptions(renderer.WithNodeRenderers(
		util.Prioritized(&highlightRenderer{}, 450),
	))
}
