package webui

import (
	"archive/zip"
	"bytes"
	"html/template"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"

	"github.com/oss/oss-server/internal/consoletheme"
	"github.com/oss/oss-server/internal/markdown"
	"github.com/oss/oss-server/internal/models"
)

type consoleThemeFile struct {
	Path    string
	Content string
}

type consoleThemeRow struct {
	consoletheme.Info
	Files []consoleThemeFile
}

type adminConsoleThemesData struct {
	Themes    []consoleThemeRow
	GuideHTML template.HTML
	Error     string
	Saved     bool
}

func (h *Handler) adminConsoleThemesPage(c *gin.Context) {
	d := adminConsoleThemesData{Error: c.Query("error"), Saved: c.Query("saved") == "1"}
	themes, err := consoletheme.List(h.Cfg.Storage.DataDir)
	if err != nil {
		d.Error = h.t(c, "err.load_console_themes_failed")
		h.render(c, http.StatusInternalServerError, "admin-console-themes", h.t(c, "page.admin_console_themes"), "admin", "admin-console-themes", d)
		return
	}
	for _, theme := range themes {
		row := consoleThemeRow{Info: theme}
		if theme.Source != "builtin" {
			row.Files = h.consoleThemeEditableFiles(theme.Name)
		}
		d.Themes = append(d.Themes, row)
	}
	if source, err := webFS.ReadFile("assets/console-theme-guide.md"); err == nil {
		if guide, renderErr := markdown.RenderMarkdown(nil, string(source)); renderErr == nil {
			d.GuideHTML = template.HTML(guide)
		}
	}
	h.render(c, http.StatusOK, "admin-console-themes", h.t(c, "page.admin_console_themes"), "admin", "admin-console-themes", d)
}

func (h *Handler) consoleThemeEditableFiles(name string) []consoleThemeFile {
	paths, err := consoletheme.ListFiles(h.Cfg.Storage.DataDir, name)
	if err != nil {
		return nil
	}
	files := make([]consoleThemeFile, 0, len(paths))
	for _, path := range paths {
		content, err := consoletheme.ReadFile(h.Cfg.Storage.DataDir, name, path)
		if err != nil {
			continue
		}
		files = append(files, consoleThemeFile{Path: path, Content: string(content)})
		if len(files) == 64 {
			break
		}
	}
	return files
}

func (h *Handler) adminConsoleThemeUpload(c *gin.Context) {
	name := strings.TrimSpace(c.PostForm("name"))
	file, err := c.FormFile("file")
	if err != nil {
		h.redirectConsoleThemeError(c, h.t(c, "err.missing_zip"))
		return
	}
	if name == "" {
		name = strings.TrimSuffix(file.Filename, ".zip")
	}
	opened, err := file.Open()
	if err != nil {
		h.redirectConsoleThemeError(c, h.t(c, "err.cannot_read_upload"))
		return
	}
	defer opened.Close()
	content, err := io.ReadAll(io.LimitReader(opened, 33<<20))
	if err != nil || len(content) > 32<<20 {
		h.redirectConsoleThemeError(c, h.t(c, "err.console_theme_zip_limit"))
		return
	}
	if err := consoletheme.Upload(h.Cfg.Storage.DataDir, name, bytes.NewReader(content), int64(len(content))); err != nil {
		h.redirectConsoleThemeError(c, err.Error())
		return
	}
	c.Redirect(http.StatusSeeOther, "/dashboard/admin/console-themes?saved=1")
}

func (h *Handler) adminConsoleThemeScaffold(c *gin.Context) {
	base := strings.TrimSpace(c.PostForm("base"))
	name := strings.TrimSpace(c.PostForm("name"))
	if _, err := consoletheme.Scaffold(h.Cfg.Storage.DataDir, base, name); err != nil {
		h.redirectConsoleThemeError(c, err.Error())
		return
	}
	c.Redirect(http.StatusSeeOther, "/dashboard/admin/console-themes?saved=1")
}

func (h *Handler) adminConsoleThemeFileSave(c *gin.Context) {
	name := c.Param("name")
	path := strings.TrimSpace(c.PostForm("path"))
	if err := consoletheme.SaveFile(h.Cfg.Storage.DataDir, name, path, []byte(c.PostForm("content"))); err != nil {
		h.redirectConsoleThemeError(c, err.Error())
		return
	}
	c.Redirect(http.StatusSeeOther, "/dashboard/admin/console-themes?saved=1")
}

func (h *Handler) adminConsoleThemeDownload(c *gin.Context) {
	name := c.Param("name")
	if consoletheme.IsBuiltin(name) {
		h.redirectConsoleThemeError(c, h.t(c, "err.builtin_console_theme_no_download"))
		return
	}
	var archive bytes.Buffer
	writer := zip.NewWriter(&archive)
	if err := consoletheme.CreateZip(h.Cfg.Storage.DataDir, name, writer); err != nil {
		if closeErr := writer.Close(); closeErr != nil {
			c.Status(http.StatusInternalServerError)
			return
		}
		h.redirectConsoleThemeError(c, err.Error())
		return
	}
	if err := writer.Close(); err != nil {
		c.Status(http.StatusInternalServerError)
		return
	}
	c.Header("Content-Disposition", "attachment; filename="+strconv.Quote(name+".zip"))
	c.Data(http.StatusOK, "application/zip", archive.Bytes())
}

func (h *Handler) adminConsoleThemeDelete(c *gin.Context) {
	name := c.Param("name")
	var selected int64
	if err := h.DB.Model(&models.UserSetting{}).Where("console_theme_name = ?", name).Count(&selected).Error; err != nil {
		h.redirectConsoleThemeError(c, h.t(c, "err.console_theme_check_failed"))
		return
	}
	if selected > 0 {
		h.redirectConsoleThemeError(c, h.t(c, "err.console_theme_in_use"))
		return
	}
	if err := consoletheme.Delete(h.Cfg.Storage.DataDir, name); err != nil {
		h.redirectConsoleThemeError(c, err.Error())
		return
	}
	c.Redirect(http.StatusSeeOther, "/dashboard/admin/console-themes?saved=1")
}

func (h *Handler) redirectConsoleThemeError(c *gin.Context, message string) {
	c.Redirect(http.StatusSeeOther, "/dashboard/admin/console-themes?error="+url.QueryEscape(message))
}
