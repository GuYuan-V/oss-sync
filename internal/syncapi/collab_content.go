// 协作文本
package syncapi

import (
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"

	"github.com/gin-gonic/gin"

	"github.com/oss/oss-server/internal/collaboration"
	"github.com/oss/oss-server/internal/models"
	"github.com/oss/oss-server/internal/vaultaccess"
)

// CollabContent 返回协作原文件正文，不要求协作者成为仓库成员。
func (h *Handler) CollabContent(c *gin.Context) {
	user, _, ok := h.requireCollaborationDevice(c)
	if !ok {
		return
	}
	fileID, err := strconv.ParseUint(c.Param("file_id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid file id", "code": "invalid_file_id"})
		return
	}

	var file models.File
	if err := h.DB.Where("id = ? AND vault_id = ?", uint(fileID), c.Param("vault_id")).First(&file).Error; err != nil {
		file, err = h.acceptedCollaborationFile(user.ID, uint(fileID))
		if err != nil {
			c.JSON(http.StatusNotFound, gin.H{"error": "file not found", "code": "file_not_found"})
			return
		}
	}
	if !h.canReadCollaborationContent(user, file) {
		c.JSON(http.StatusForbidden, gin.H{"error": "你不是该文件的协作者", "code": "collaboration_forbidden"})
		return
	}
	if file.IsDeleted {
		c.JSON(http.StatusGone, gin.H{"error": "file has been deleted", "code": "file_deleted"})
		return
	}

	fh, err := os.Open(h.fileDiskPath(file))
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "file missing on disk", "code": "file_missing"})
		return
	}
	defer fh.Close()
	stat, err := fh.Stat()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error(), "code": "file_stat_failed"})
		return
	}
	c.Header("Content-Type", "application/octet-stream")
	c.Header("Content-Length", strconv.FormatInt(stat.Size(), 10))
	c.Header("Content-Disposition", fmt.Sprintf("attachment; filename=%q", filepath.Base(file.Path)))
	c.Header("X-OSS-Hash", file.Hash)
	c.Header("X-OSS-MTime", strconv.FormatInt(file.MTime, 10))
	c.Header("X-OSS-Revision", strconv.FormatInt(file.Revision, 10))
	c.Status(http.StatusOK)
	if _, err := io.Copy(c.Writer, fh); err != nil {
		_ = c.Error(err)
	}
}

func (h *Handler) acceptedCollaborationFile(collaboratorID, fileID uint) (models.File, error) {
	var row models.Collaboration
	if err := h.DB.Where("file_id = ? AND collaborator_id = ? AND status = ?",
		fileID, collaboratorID, collaboration.StatusAccepted).First(&row).Error; err != nil {
		return models.File{}, err
	}
	var file models.File
	if err := h.DB.Where("id = ? AND vault_id = ?", fileID, row.VaultID).First(&file).Error; err != nil {
		return models.File{}, err
	}
	return file, nil
}

func (h *Handler) canReadCollaborationContent(user *models.User, file models.File) bool {
	if user.Role == "admin" {
		return true
	}
	if _, role, err := vaultaccess.Resolve(h.DB, user.ID, file.VaultID); err == nil && vaultaccess.CanManage(role) {
		return true
	}
	var count int64
	err := h.DB.Model(&models.Collaboration{}).
		Where("vault_id = ? AND file_id = ? AND collaborator_id = ? AND status = ?",
			file.VaultID, file.ID, user.ID, collaboration.StatusAccepted).
		Count(&count).Error
	return err == nil && count > 0
}

