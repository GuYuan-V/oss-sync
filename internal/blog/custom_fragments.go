// 自定义片段
package blog

import (
	"html"
	"html/template"
	"net/url"
	"strings"

	"github.com/oss/oss-server/internal/markdown"
	htmlparse "golang.org/x/net/html"
)

const maxCustomFragmentRunes = 2000

func renderSafeCustomFragment(raw string) template.HTML {
	trimmed := trimByRunes(raw, maxCustomFragmentRunes)
	if trimmed == "" {
		return ""
	}

	htmlText, err := markdown.RenderMarkdown(nil, trimmed)
	if err != nil {
		return template.HTML(html.EscapeString(trimmed))
	}

	return template.HTML(sanitizeBlogHTML(htmlText))
}

func renderSafeCustomFragmentEnabled(raw string, enabled bool) template.HTML {
	if !enabled {
		return ""
	}
	return renderSafeCustomFragment(raw)
}

func sanitizeBlogHTML(raw string) string {
	root := strings.TrimSpace(raw)
	if root == "" {
		return ""
	}

	context := &htmlparse.Node{Type: htmlparse.ElementNode, Data: "div"}
	nodes, err := htmlparse.ParseFragment(strings.NewReader(root), context)
	if err != nil {
		return html.EscapeString(root)
	}

	var out strings.Builder
	for _, node := range nodes {
		writeSanitizedNode(&out, node)
	}
	return out.String()
}

func writeSanitizedNode(out *strings.Builder, node *htmlparse.Node) {
	switch node.Type {
	case htmlparse.ElementNode:
		tag := strings.ToLower(node.Data)
		if !isAllowedCustomTag(tag) {
			for child := node.FirstChild; child != nil; child = child.NextSibling {
				writeSanitizedNode(out, child)
			}
			return
		}

		out.WriteString("<")
		out.WriteString(tag)
		for _, attr := range node.Attr {
			if key, value, ok := sanitizeCustomAttr(tag, attr.Key, attr.Val); ok {
				out.WriteString(" ")
				out.WriteString(key)
				out.WriteString("=\"")
				out.WriteString(html.EscapeString(value))
				out.WriteString("\"")
			}
		}

		if isVoidTag(tag) {
			out.WriteString(" />")
			return
		}

		out.WriteString(">")
		for child := node.FirstChild; child != nil; child = child.NextSibling {
			writeSanitizedNode(out, child)
		}
		out.WriteString("</")
		out.WriteString(tag)
		out.WriteString(">")
	case htmlparse.TextNode:
		out.WriteString(html.EscapeString(node.Data))
	case htmlparse.DocumentNode:
		for child := node.FirstChild; child != nil; child = child.NextSibling {
			writeSanitizedNode(out, child)
		}
	}
}

func isAllowedCustomTag(tag string) bool {
	switch tag {
	case "a", "abbr", "b", "blockquote", "br", "code", "del", "em", "h1", "h2", "h3", "h4", "h5", "h6", "hr", "i", "img", "li", "ol", "p", "pre", "strong", "sub", "sup", "table", "tbody", "td", "th", "thead", "tr", "u", "ul", "span", "div", "small":
		return true
	}
	return false
}

func isVoidTag(tag string) bool {
	switch tag {
	case "br", "hr", "img", "source", "track", "meta", "link", "base", "input":
		return true
	default:
		return false
	}
}

func sanitizeCustomAttr(tag, name, value string) (string, string, bool) {
	key := strings.TrimSpace(strings.ToLower(name))
	if key == "" || strings.HasPrefix(key, "on") {
		return "", "", false
	}
	if strings.HasPrefix(key, "aria-") {
		return key, strings.TrimSpace(value), true
	}

	switch key {
	case "class", "id", "title", "alt", "role", "name":
		return key, strings.TrimSpace(value), true
	case "loading":
		v := strings.ToLower(strings.TrimSpace(value))
		if v == "eager" || v == "lazy" || v == "auto" {
			return key, v, true
		}
	case "width", "height":
		v := strings.TrimSpace(value)
		if v == "" || isPositiveInteger(v) {
			return key, v, true
		}
	case "target":
		s := strings.ToLower(strings.TrimSpace(value))
		if s == "_blank" || s == "_self" || s == "_parent" || s == "_top" {
			return key, s, true
		}
	case "rel":
		s := strings.ToLower(strings.TrimSpace(value))
		if s == "noopener" || s == "noreferrer" || s == "noopener noreferrer" {
			return key, s, true
		}
	case "href", "src":
		if safeURL := sanitizeCustomURL(value); safeURL != "" {
			return key, safeURL, true
		}
	}

	_ = tag
	return "", "", false
}

func isPositiveInteger(value string) bool {
	for _, r := range value {
		if r < '0' || r > '9' {
			return false
		}
	}
	return value != ""
}

func sanitizeCustomURL(raw string) string {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return ""
	}
	lower := strings.ToLower(trimmed)
	if strings.HasPrefix(lower, "javascript:") || strings.HasPrefix(lower, "vbscript:") || strings.HasPrefix(lower, "data:") {
		return ""
	}
	if strings.Contains(lower, "\x00") {
		return ""
	}
	parsed, err := url.Parse(trimmed)
	if err != nil {
		return ""
	}
	if strings.Contains(trimmed, ":") && parsed.Scheme == "" {
		return ""
	}
	if parsed.Scheme == "" {
		return trimmed
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return ""
	}
	return parsed.String()
}

