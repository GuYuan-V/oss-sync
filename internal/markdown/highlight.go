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

// highlight 表示 ==高亮== 行内节点，Raw 为去掉标记后的原始文本
type highlight struct {
	gast.BaseInline
	Raw string
}

// kindHighlight 是 highlight 节点在 AST 中的类型标识
var kindHighlight = gast.NewNodeKind("Highlight")

func (n *highlight) Kind() gast.NodeKind {
	return kindHighlight
}

// Text 返回高亮节点的原始文本，用于 goldmark 树遍历与转储
func (n *highlight) Text(source []byte) []byte {
	return []byte(n.Raw)
}

// Dump 输出节点调试信息，Raw 字段参与树形转储
func (n *highlight) Dump(source []byte, level int) {
	gast.DumpHelper(n, source, level, map[string]string{"Raw": n.Raw}, nil)
}

// highlightParser 解析行首的 == 高亮标记
type highlightParser struct{}

// Trigger 返回触发行内解析的首字节
func (p *highlightParser) Trigger() []byte {
	return []byte{'='}
}

// Parse 解析单段高亮文本，仅在行首存在配对的 == 标记时生成节点，
// 标记不完整或内容为空时返回 nil 交由后续解析器处理
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

// highlightRenderer 将高亮节点渲染为 mark 元素
type highlightRenderer struct{}

// RegisterFuncs 注册高亮节点的渲染函数
func (r *highlightRenderer) RegisterFuncs(reg renderer.NodeRendererFuncRegisterer) {
	reg.Register(kindHighlight, r.render)
}

// render 输出 mark 元素，内容整体转义，反引号包裹的文本额外套上 code 元素
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

// highlightExtension 将高亮解析器与渲染器注册到 goldmark 实例
type highlightExtension struct{}

// Extend 向 markdown 实例注入高亮行内解析器与节点渲染器
func (e *highlightExtension) Extend(markdown goldmark.Markdown) {
	markdown.Parser().AddOptions(parser.WithInlineParsers(
		util.Prioritized(&highlightParser{}, 90),
	))
	markdown.Renderer().AddOptions(renderer.WithNodeRenderers(
		util.Prioritized(&highlightRenderer{}, 450),
	))
}
