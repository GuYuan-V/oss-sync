package server

import (
	"net/http"
	"strings"
	"testing"
)

// TestWebConsoleAttachmentPreview 覆盖控制台内图片/PDF 预览与内联服务的安全边界
func TestWebConsoleAttachmentPreview(t *testing.T) {
	t.Chdir(t.TempDir())
	srv, _, _ := newTestServer(t)
	router := srv.Router()

	ownerToken := registerAndLogin(t, router, "preview-owner", "password123")
	vaultID := defaultVaultIDFromAPI(t, router, ownerToken)
	uploadViaV1(t, router, ownerToken, "pic.png", "png-bytes")
	uploadViaV1(t, router, ownerToken, "doc.pdf", "%PDF-1.4 fake")
	uploadViaV1(t, router, ownerToken, "vector.svg", "<svg xmlns='http://www.w3.org/2000/svg'></svg>")

	session, csrf := webLogin(t, router, "preview-owner", "password123")

	// 文件列表：图片与 SVG 均可点击预览（SVG 走隔离沙箱）
	list := doForm(t, router, http.MethodGet, "/dashboard/vaults/"+vaultID, nil, session, csrf)
	if list.Code != http.StatusOK {
		t.Fatalf("file list: %d", list.Code)
	}
	if !strings.Contains(list.Body.String(), "/dashboard/vaults/"+vaultID+"/files/preview?path=pic.png") {
		t.Fatalf("image should link to preview: %s", list.Body)
	}
	if !strings.Contains(list.Body.String(), "files/preview?path=vector.svg") {
		t.Fatalf("svg should link to sandboxed preview: %s", list.Body)
	}

	// 图片预览页内联 <img>
	img := doForm(t, router, http.MethodGet, "/dashboard/vaults/"+vaultID+"/files/preview?path=pic.png", nil, session, csrf)
	if img.Code != http.StatusOK ||
		!strings.Contains(img.Body.String(), "file-preview--image") ||
		!strings.Contains(img.Body.String(), "path=pic.png") ||
		!strings.Contains(img.Body.String(), "inline=1") {
		t.Fatalf("image preview page: %d body=%s", img.Code, img.Body)
	}

	// PDF 预览页内联 <iframe>
	pdf := doForm(t, router, http.MethodGet, "/dashboard/vaults/"+vaultID+"/files/preview?path=doc.pdf", nil, session, csrf)
	if pdf.Code != http.StatusOK ||
		!strings.Contains(pdf.Body.String(), "file-preview--pdf") ||
		!strings.Contains(pdf.Body.String(), "inline=1") {
		t.Fatalf("pdf preview page: %d body=%s", pdf.Code, pdf.Body)
	}

	// SVG 以隔离沙箱 iframe 预览：预览页返回 200 并引用沙箱端点
	svgPreview := doForm(t, router, http.MethodGet, "/dashboard/vaults/"+vaultID+"/files/preview?path=vector.svg", nil, session, csrf)
	if svgPreview.Code != http.StatusOK ||
		!strings.Contains(svgPreview.Body.String(), "file-preview--sandbox") ||
		!strings.Contains(svgPreview.Body.String(), "files/sandbox?path=vector.svg") {
		t.Fatalf("svg preview must render sandboxed: %d body=%s", svgPreview.Code, svgPreview.Body)
	}
	// 沙箱端点：正确 MIME + CSP sandbox + nosniff（隔离源、禁用脚本）
	svgRaw := doForm(t, router, http.MethodGet, "/dashboard/vaults/"+vaultID+"/files/sandbox?path=vector.svg", nil, session, csrf)
	if svgRaw.Code != http.StatusOK ||
		!strings.Contains(svgRaw.Header().Get("Content-Type"), "image/svg+xml") ||
		!strings.Contains(svgRaw.Header().Get("Content-Security-Policy"), "sandbox") ||
		svgRaw.Header().Get("X-Content-Type-Options") != "nosniff" {
		t.Fatalf("svg sandbox endpoint: %d ct=%q csp=%q", svgRaw.Code,
			svgRaw.Header().Get("Content-Type"), svgRaw.Header().Get("Content-Security-Policy"))
	}

	// 图片内联下载：正确 MIME + inline + nosniff
	inlineImg := doForm(t, router, http.MethodGet, "/dashboard/vaults/"+vaultID+"/files/download?path=pic.png&inline=1", nil, session, csrf)
	if inlineImg.Code != http.StatusOK ||
		inlineImg.Header().Get("Content-Type") != "image/png" ||
		!strings.Contains(inlineImg.Header().Get("Content-Disposition"), "inline") ||
		inlineImg.Header().Get("X-Content-Type-Options") != "nosniff" {
		t.Fatalf("inline image headers: %d ct=%q cd=%q nosniff=%q", inlineImg.Code,
			inlineImg.Header().Get("Content-Type"), inlineImg.Header().Get("Content-Disposition"),
			inlineImg.Header().Get("X-Content-Type-Options"))
	}

	// SVG 即使请求 inline 也强制下载，绝不以 image/svg 内联（防同源脚本执行）
	inlineSVG := doForm(t, router, http.MethodGet, "/dashboard/vaults/"+vaultID+"/files/download?path=vector.svg&inline=1", nil, session, csrf)
	if inlineSVG.Code != http.StatusOK ||
		!strings.Contains(inlineSVG.Header().Get("Content-Disposition"), "attachment") ||
		strings.Contains(inlineSVG.Header().Get("Content-Type"), "svg") {
		t.Fatalf("svg must be forced to download: %d ct=%q cd=%q", inlineSVG.Code,
			inlineSVG.Header().Get("Content-Type"), inlineSVG.Header().Get("Content-Disposition"))
	}
}
