package webui

import (
	"path/filepath"
	"strings"
)

// previewKind 返回文件在控制台可内联预览的类型：markdown、image、pdf；不可预览返回空串
func previewKind(path string) string {
	if isMarkdownFile(path) {
		return "markdown"
	}
	if _, ok := inlineContentType(path); ok {
		switch strings.ToLower(filepath.Ext(path)) {
		case ".pdf":
			return "pdf"
		default:
			return "image"
		}
	}
	return ""
}

// inlineContentType 返回可安全内联到浏览器的 MIME 类型；不允许内联时第二个返回值为 false。
// 仅位图与 PDF 允许内联；SVG、HTML 等可执行内容一律走下载，避免同源脚本注入
func inlineContentType(path string) (string, bool) {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".png":
		return "image/png", true
	case ".jpg", ".jpeg":
		return "image/jpeg", true
	case ".gif":
		return "image/gif", true
	case ".webp":
		return "image/webp", true
	case ".avif":
		return "image/avif", true
	case ".bmp":
		return "image/bmp", true
	case ".ico":
		return "image/x-icon", true
	case ".pdf":
		return "application/pdf", true
	}
	return "", false
}
