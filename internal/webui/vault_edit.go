package webui

import (
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"github.com/gin-gonic/gin"

	"github.com/helantianshen/oss-sync/internal/filestore"
	"github.com/helantianshen/oss-sync/internal/models"
)

// fileContentWriter 由同步层实现，供控制台内置编辑器复用真实文件写入管线
// 写入在服务器进程内完成，与客户端同步共享同一套并发锁、修订推进与历史记录
type fileContentWriter interface {
	WriteFileContent(userID uint, vaultID, path string, content []byte, mtime int64) (models.File, error)
}

type vaultFileEditData struct {
	VaultID     string
	Path        string
	Name        string
	Directory   string
	Breadcrumbs []breadcrumbRow
	Content     string
	Saved       bool
	Error       string
}

// isEditableTextFile 判断文件是否可用内置纯文本编辑器编辑
func isEditableTextFile(path string) bool {
	if isTextFile(path) {
		return true
	}
	return strings.HasSuffix(strings.ToLower(path), ".svg")
}

// editFilePage 渲染内置纯文本编辑器，无需任何插件
func (h *Handler) editFilePage(c *gin.Context) {
	vault, _, ok := h.resolveVaultPage(c)
	if !ok {
		return
	}
	filePath, valid := normalizeWebPath(c.Query("path"))
	if !valid || !isEditableTextFile(filePath) {
		c.String(http.StatusBadRequest, "editable text path required")
		return
	}
	var file models.File
	if err := h.DB.Where("user_id = ? AND vault_id = ? AND path = ? AND is_deleted = ?",
		vault.OwnerID, vault.ID, filePath, false).First(&file).Error; err != nil {
		c.String(http.StatusNotFound, "file not found")
		return
	}
	raw, err := os.ReadFile(filestore.DiskPath(h.Cfg.Storage.DataDir, file))
	if err != nil {
		c.String(http.StatusNotFound, "file content missing")
		return
	}
	directory := filepath.ToSlash(filepath.Dir(filePath))
	if directory == "." {
		directory = ""
	}
	data := vaultFileEditData{
		VaultID:     vault.ID,
		Path:        filePath,
		Name:        filepath.Base(filePath),
		Directory:   directory,
		Breadcrumbs: buildVaultBreadcrumbs(directory),
		Content:     string(raw),
		Saved:       c.Query("saved") == "1",
		Error:       c.Query("error"),
	}
	ld := layoutData{}
	h.setVaultLayout(&ld, vault)
	h.renderVault(c, ld, "vault-file-edit", h.t(c, "page.vault_file_edit", vault.Name, filepath.Base(filePath)), data)
}

// saveFileEdit 保存内置编辑器提交的内容，走真实同步写入管线
func (h *Handler) saveFileEdit(c *gin.Context) {
	vault, _, ok := h.resolveVaultPage(c)
	if !ok {
		return
	}
	if !h.validCSRF(c) {
		c.String(http.StatusForbidden, "invalid csrf token")
		return
	}
	filePath, valid := normalizeWebPath(c.PostForm("path"))
	if !valid || !isEditableTextFile(filePath) {
		c.String(http.StatusBadRequest, "editable text path required")
		return
	}
	if h.fileWriter == nil {
		c.String(http.StatusServiceUnavailable, "file editing unavailable")
		return
	}
	// textarea 提交的换行统一为 LF，避免 CRLF 造成无意义的内容变化
	content := strings.ReplaceAll(c.PostForm("content"), "\r\n", "\n")
	editURL := "/dashboard/vaults/" + vault.ID + "/files/edit?path=" + url.QueryEscape(filePath)
	if _, err := h.fileWriter.WriteFileContent(vault.OwnerID, vault.ID, filePath, []byte(content), 0); err != nil {
		c.Redirect(http.StatusSeeOther, editURL+"&error="+url.QueryEscape(h.t(c, "vault.edit_save_failed")))
		return
	}
	c.Redirect(http.StatusSeeOther, editURL+"&saved=1")
}
