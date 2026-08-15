package webui

import (
	"fmt"
	"html/template"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/gin-gonic/gin"

	"github.com/oss/oss-server/internal/blog"
	"github.com/oss/oss-server/internal/filestore"
	"github.com/oss/oss-server/internal/markdown"
	"github.com/oss/oss-server/internal/models"
	"github.com/oss/oss-server/internal/shares"
)

const (
	renderedPreviewMode = "rendered"
	sourcePreviewMode   = "source"
)

type vaultMarkdownPreviewData struct {
	VaultID     string
	Path        string
	Name        string
	Directory   string
	Breadcrumbs []breadcrumbRow
	Mode        string
	ContentText string
	ContentHTML template.HTML
}

func (h *Handler) previewMarkdownFile(c *gin.Context) {
	vault, _, ok := h.resolveVaultPage(c)
	if !ok {
		return
	}
	filePath, valid := normalizeWebPath(c.Query("path"))
	if !valid || !isMarkdownFile(filePath) {
		c.String(http.StatusBadRequest, "markdown path required")
		return
	}

	var file models.File
	if err := h.DB.Where("user_id = ? AND vault_id = ? AND path = ? AND is_deleted = ?",
		vault.OwnerID, vault.ID, filePath, false).First(&file).Error; err != nil {
		c.String(http.StatusNotFound, "file not found")
		return
	}
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

	var contentHTML template.HTML
	var contentText string
	if mode == sourcePreviewMode {
		contentText = string(raw)
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
		contentHTML = template.HTML(rendered)
	}

	directory := filepath.ToSlash(filepath.Dir(filePath))
	if directory == "." {
		directory = ""
	}
	ld := layoutData{}
	h.setVaultLayout(&ld, vault)
	h.renderVault(c, ld, "vault-preview", h.t(c, "page.vault_preview", vault.Name, filepath.Base(filePath)), vaultMarkdownPreviewData{
		VaultID: vault.ID, Path: filePath, Name: filepath.Base(filePath), Directory: directory, Mode: mode,
		Breadcrumbs: buildVaultBreadcrumbs(directory), ContentHTML: contentHTML, ContentText: contentText,
	})
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
