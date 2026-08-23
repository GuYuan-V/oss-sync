// 模板管理
package admin

import (
	"archive/zip"
	"bytes"
	"errors"
	"io"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	"github.com/oss/oss-server/internal/blog"
	"github.com/oss/oss-server/internal/models"
)

// themesRouter 注册模板管理 API。
func (h *Handler) themesRouter(g *gin.RouterGroup) {
	g.GET("/themes", h.listThemes)
	g.POST("/themes", h.uploadTheme)
	g.POST("/themes/scaffold", h.scaffoldTheme)
	g.GET("/themes/:name/download", h.downloadTheme)
	g.GET("/themes/:name/files", h.listThemeFiles)
	g.PUT("/themes/:name/files", h.saveThemeFile)
	g.DELETE("/themes/:name", h.deleteTheme)
}

func (h *Handler) listThemes(c *gin.Context) {
	themes, err := blog.ListThemes(h.DB, h.Cfg.Storage.DataDir)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"themes": themes})
}

func (h *Handler) uploadTheme(c *gin.Context) {
	themeName := strings.TrimSpace(c.PostForm("name"))
	file, err := c.FormFile("file")
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "缺少 ZIP 文件"})
		return
	}
	if themeName == "" {
		base := file.Filename
		if idx := strings.LastIndex(base, "."); idx > 0 {
			base = base[:idx]
		}
		themeName = strings.TrimSpace(base)
	}
	if err := blog.ValidateThemeName(themeName); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	fh, err := file.Open()
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "无法读取上传文件"})
		return
	}
	defer fh.Close()
	content, err := io.ReadAll(io.LimitReader(fh, 33<<20))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "读取上传文件失败"})
		return
	}
	if len(content) > 32<<20 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "ZIP 文件过大"})
		return
	}
	if err := blog.UploadTheme(h.Cfg.Storage.DataDir, themeName, bytes.NewReader(content), int64(len(content))); err != nil {
		status := http.StatusBadRequest
		if errors.Is(err, blog.ErrThemeExists) {
			status = http.StatusConflict
		}
		c.JSON(status, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"name": themeName})
}

func (h *Handler) scaffoldTheme(c *gin.Context) {
	var req struct {
		Base    string `json:"base" binding:"required"`
		Name    string `json:"name" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if _, err := blog.ScaffoldTheme(h.Cfg.Storage.DataDir, req.Base, req.Name); err != nil {
		status := http.StatusBadRequest
		if errors.Is(err, blog.ErrThemeExists) {
			status = http.StatusConflict
		}
		c.JSON(status, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"name": req.Name})
}

func (h *Handler) downloadTheme(c *gin.Context) {
	themeName := c.Param("name")
	if blog.IsBuiltinTheme(themeName) {
		c.JSON(http.StatusForbidden, gin.H{"error": blog.ErrThemeNotDownloadable.Error()})
		return
	}
	c.Header("Content-Disposition", "attachment; filename="+quoteName(themeName+".zip"))
	c.Header("Content-Type", "application/zip")
	zw := zip.NewWriter(c.Writer)
	if err := blog.CreateThemeZip(h.Cfg.Storage.DataDir, themeName, zw); err != nil {
		_ = zw.Close()
		c.JSON(http.StatusForbidden, gin.H{"error": err.Error()})
		return
	}
	if err := zw.Close(); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
}

func (h *Handler) listThemeFiles(c *gin.Context) {
	themeName := c.Param("name")
	files, err := blog.ListThemeFiles(h.Cfg.Storage.DataDir, themeName)
	if err != nil {
		status := http.StatusForbidden
		if blog.IsThemeReadOnly(err) {
			status = http.StatusForbidden
		}
		c.JSON(status, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"files": files})
}

func (h *Handler) saveThemeFile(c *gin.Context) {
	themeName := c.Param("name")
	relPath := strings.TrimSpace(c.Query("path"))
	if relPath == "" {
		relPath = strings.TrimSpace(c.PostForm("path"))
	}
	var req struct {
		Path    string `json:"path"`
		Content string `json:"content"`
	}
	_ = c.ShouldBindJSON(&req)
	if relPath == "" {
		relPath = req.Path
	}
	content := req.Content
	if content == "" {
		content = c.PostForm("content")
	}
	if relPath == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "path is required"})
		return
	}
	if err := blog.SaveThemeFile(h.Cfg.Storage.DataDir, themeName, relPath, []byte(content)); err != nil {
		status := http.StatusForbidden
		if blog.IsThemeReadOnly(err) {
			status = http.StatusForbidden
		} else if !strings.Contains(err.Error(), "只允许编辑文本文件") {
			status = http.StatusBadRequest
		}
		c.JSON(status, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"saved": true})
}

func (h *Handler) deleteTheme(c *gin.Context) {
	themeName := c.Param("name")
	used, err := blog.DeleteTheme(h.DB, h.Cfg.Storage.DataDir, themeName)
	if err != nil {
		status := http.StatusForbidden
		switch {
		case blog.IsThemeNotDeletable(err):
			status = http.StatusForbidden
		case blog.IsThemeInUse(err):
			status = http.StatusConflict
		default:
			status = http.StatusBadRequest
		}
		c.JSON(status, gin.H{"error": err.Error(), "used_by": used})
		return
	}
	c.JSON(http.StatusOK, gin.H{"deleted": true})
}

func quoteName(name string) string {
	return `"` + strings.ReplaceAll(name, `"`, "") + `"`
}

var _ = models.VaultSetting{}
var _ = gorm.ErrRecordNotFound

