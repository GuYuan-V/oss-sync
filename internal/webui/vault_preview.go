package webui

import (
	"fmt"
	"html/template"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"

	"github.com/helantianshen/oss-sync/internal/blog"
	"github.com/helantianshen/oss-sync/internal/filestore"
	"github.com/helantianshen/oss-sync/internal/markdown"
	"github.com/helantianshen/oss-sync/internal/models"
	"github.com/helantianshen/oss-sync/internal/shares"
)

const (
	renderedPreviewMode = "rendered"
	sourcePreviewMode   = "source"
)

type vaultFilePreviewData struct {
	VaultID     string
	Path        string
	Name        string
	Directory   string
	Breadcrumbs []breadcrumbRow
	Kind        string
	Mode        string
	ContentText string
	ContentHTML template.HTML
}

func (h *Handler) previewFile(c *gin.Context) {
	vault, _, ok := h.resolveVaultPage(c)
	if !ok {
		return
	}
	filePath, valid := normalizeWebPath(c.Query("path"))
	kind := ""
	if valid {
		kind = previewKind(filePath)
	}
	if !valid || kind == "" {
		c.String(http.StatusBadRequest, "previewable path required")
		return
	}

	var file models.File
	if err := h.DB.Where("user_id = ? AND vault_id = ? AND path = ? AND is_deleted = ?",
		vault.OwnerID, vault.ID, filePath, false).First(&file).Error; err != nil {
		c.String(http.StatusNotFound, "file not found")
		return
	}

	directory := filepath.ToSlash(filepath.Dir(filePath))
	if directory == "." {
		directory = ""
	}
	data := vaultFilePreviewData{
		VaultID: vault.ID, Path: filePath, Name: filepath.Base(filePath), Directory: directory,
		Breadcrumbs: buildVaultBreadcrumbs(directory), Kind: kind,
	}

	if kind == "markdown" {
		fh, err := os.Open(filestore.DiskPath(h.Cfg.Storage.DataDir, file))
		if err != nil {
			c.String(http.StatusNotFound, "file content missing")
			return
		}
		defer fh.Close()
		raw, err := io.ReadAll(fh)
		if err != nil {
			c.String(http.StatusInternalServerError, "file content unreadable")
			return
		}
		mode := strings.ToLower(strings.TrimSpace(c.Query("mode")))
		if mode != sourcePreviewMode {
			mode = renderedPreviewMode
		}
		data.Mode = mode
		if mode == sourcePreviewMode {
			data.ContentText = string(raw)
		} else {
			shareID, err := h.previewMarkdownShareID(vault, filePath)
			if err != nil {
				c.String(http.StatusInternalServerError, "markdown preview unavailable")
				return
			}
			rendered, err := markdown.RenderMarkdownWithAssets(nil, blog.NewAssetResolver(shareID), string(raw))
			if err != nil {
				c.String(http.StatusInternalServerError, "markdown preview unavailable")
				return
			}
			data.ContentHTML = template.HTML(rendered)
		}
	}

	ld := layoutData{}
	h.setVaultLayout(&ld, vault)
	h.renderVault(c, ld, "vault-preview", h.t(c, "page.vault_preview", vault.Name, filepath.Base(filePath)), data)
}

// sandboxPreviewFile 以隔离源、禁用脚本的方式提供 HTML/SVG 原始内容，仅供沙箱 iframe 预览
// CSP sandbox 指令不含 allow-scripts，响应处于不透明源且不执行脚本，无法读取控制台会话
func (h *Handler) sandboxPreviewFile(c *gin.Context) {
	vault, _, ok := h.resolveVaultPage(c)
	if !ok {
		return
	}
	path, valid := normalizeWebPath(c.Query("path"))
	if !valid {
		c.String(http.StatusBadRequest, "invalid path")
		return
	}
	ctype, ok := sandboxContentType(path)
	if !ok {
		c.String(http.StatusBadRequest, "not a sandboxed previewable type")
		return
	}
	var file models.File
	if err := h.DB.Where("user_id = ? AND vault_id = ? AND path = ? AND is_deleted = ?",
		vault.OwnerID, vault.ID, path, false).First(&file).Error; err != nil {
		c.String(http.StatusNotFound, "file not found")
		return
	}
	fh, err := os.Open(filestore.DiskPath(h.Cfg.Storage.DataDir, file))
	if err != nil {
		c.String(http.StatusNotFound, "file content missing")
		return
	}
	defer fh.Close()
	c.Header("Content-Security-Policy", "sandbox; default-src 'none'; img-src data:; style-src 'unsafe-inline'; font-src data:")
	c.Header("X-Content-Type-Options", "nosniff")
	c.Header("Content-Type", ctype)
	c.Header("Content-Disposition", "inline; filename="+strconv.Quote(filepath.Base(path)))
	c.Header("Cache-Control", "no-store")
	if info, statErr := fh.Stat(); statErr == nil {
		c.Header("Content-Length", strconv.FormatInt(info.Size(), 10))
	}
	c.Status(http.StatusOK)
	_, _ = io.Copy(c.Writer, fh)
}

func isMarkdownFile(path string) bool {
	lower := strings.ToLower(path)
	return strings.HasSuffix(lower, ".md") || strings.HasSuffix(lower, ".markdown")
}

func (h *Handler) previewMarkdownShareID(vault models.Vault, filePath string) (string, error) {
	var existing models.Share
	if err := h.DB.Where(
		"user_id = ? AND vault_id = ? AND target_path = ? AND is_folder = ?",
		vault.OwnerID,
		vault.ID,
		filePath,
		false,
	).First(&existing).Error; err == nil {
		if existing.ShareID != "" {
			return existing.ShareID, nil
		}
	}

	shareHandler := shares.New(h.DB, h.Cfg)
	shareID, err := shareHandler.CreateWeb(vault.OwnerID, vault.ID, filePath, false, false)
	if err != nil {
		return "", fmt.Errorf("create preview share: %w", err)
	}
	return shareID, nil
}
