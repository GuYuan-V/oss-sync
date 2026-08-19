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

	"github.com/oss/oss-server/internal/blog"
	"github.com/oss/oss-server/internal/markdown"
)

// themeRow 模板管理页行。
type themeRow struct {
	Name      string
	Source    string
	FileCount int
	Size      int64
	Files     []themeFile
}

type themeFile struct {
	Path    string
	Content string
}

type adminThemesData struct {
	Themes    []themeRow
	GuideHTML template.HTML
	Error     string
	Saved     bool
}

func (h *Handler) adminThemesPage(c *gin.Context) {
	d := adminThemesData{
		Error: c.Query("error"), Saved: c.Query("saved") == "1",
	}
	if source, err := webFS.ReadFile("assets/theme-guide.md"); err == nil {
		if guide, renderErr := markdown.RenderMarkdown(nil, string(source)); renderErr == nil {
			d.GuideHTML = template.HTML(guide)
		}
	}
	themes, err := blog.ListThemes(h.DB, h.Cfg.Storage.DataDir)
	if err != nil {
		d.Error = h.t(c, "err.load_theme_list_failed")
		h.render(c, http.StatusInternalServerError, "admin-themes", h.t(c, "page.admin_themes"), "admin", "admin-themes", d)
		return
	}
	for _, th := range themes {
		row := themeRow{
			Name: th.Name, Source: string(th.Source),
			FileCount: th.FileCount, Size: th.Size,
		}
		if th.Source != blog.SourceBuiltin {
			row.Files = h.themeEditableFiles(th.Name)
		}
		d.Themes = append(d.Themes, row)
	}
	h.render(c, http.StatusOK, "admin-themes", h.t(c, "page.admin_themes"), "admin", "admin-themes", d)
}

func (h *Handler) themeEditableFiles(name string) []themeFile {
	paths, err := blog.ListThemeFiles(h.Cfg.Storage.DataDir, name)
	if err != nil {
		return nil
	}
	files := make([]themeFile, 0, len(paths))
	for _, path := range paths {
		content, err := blog.ReadThemeFile(h.Cfg.Storage.DataDir, name, path)
		if err != nil {
			continue
		}
		files = append(files, themeFile{Path: path, Content: string(content)})
		if len(files) == 64 {
			break
		}
	}
	return files
}

func (h *Handler) adminThemeUpload(c *gin.Context) {
	name := strings.TrimSpace(c.PostForm("name"))
	file, err := c.FormFile("file")
	if err != nil {
		c.Redirect(http.StatusSeeOther, "/dashboard/admin/themes?error="+url.QueryEscape(h.t(c, "err.missing_zip")))
		return
	}
	if name == "" {
		base := file.Filename
		if idx := strings.LastIndex(base, "."); idx > 0 {
			base = base[:idx]
		}
		name = strings.TrimSpace(base)
	}
	fh, err := file.Open()
	if err != nil {
		c.Redirect(http.StatusSeeOther, "/dashboard/admin/themes?error="+url.QueryEscape(h.t(c, "err.cannot_read_upload")))
		return
	}
	defer fh.Close()
	content, err := io.ReadAll(io.LimitReader(fh, 33<<20))
	if err != nil {
		c.Redirect(http.StatusSeeOther, "/dashboard/admin/themes?error="+url.QueryEscape(h.t(c, "err.read_upload_failed")))
		return
	}
	if err := blog.UploadTheme(h.Cfg.Storage.DataDir, name, bytes.NewReader(content), int64(len(content))); err != nil {
		c.Redirect(http.StatusSeeOther, "/dashboard/admin/themes?error="+url.QueryEscape(err.Error()))
		return
	}
	c.Redirect(http.StatusSeeOther, "/dashboard/admin/themes?saved=1")
}

func (h *Handler) adminThemeScaffold(c *gin.Context) {
	base := strings.TrimSpace(c.PostForm("base"))
	name := strings.TrimSpace(c.PostForm("name"))
	if _, err := blog.ScaffoldTheme(h.Cfg.Storage.DataDir, base, name); err != nil {
		c.Redirect(http.StatusSeeOther, "/dashboard/admin/themes?error="+url.QueryEscape(err.Error()))
		return
	}
	c.Redirect(http.StatusSeeOther, "/dashboard/admin/themes?saved=1")
}

func (h *Handler) adminThemeDownload(c *gin.Context) {
	name := c.Param("name")
	if blog.IsBuiltinTheme(name) {
		c.Redirect(http.StatusSeeOther, "/dashboard/admin/themes?error="+url.QueryEscape(h.t(c, "err.builtin_theme_no_download")))
		return
	}
	var archive bytes.Buffer
	writer := zip.NewWriter(&archive)
	if err := blog.CreateThemeZip(h.Cfg.Storage.DataDir, name, writer); err != nil {
		if closeErr := writer.Close(); closeErr != nil {
			c.Status(http.StatusInternalServerError)
			return
		}
		c.Redirect(http.StatusSeeOther, "/dashboard/admin/themes?error="+url.QueryEscape(err.Error()))
		return
	}
	if err := writer.Close(); err != nil {
		c.Status(http.StatusInternalServerError)
		return
	}
	c.Header("Content-Disposition", "attachment; filename="+strconv.Quote(name+".zip"))
	c.Data(http.StatusOK, "application/zip", archive.Bytes())
}

func (h *Handler) adminThemeDelete(c *gin.Context) {
	name := c.Param("name")
	used, err := blog.DeleteTheme(h.DB, h.Cfg.Storage.DataDir, name)
	if err != nil {
		msg := err.Error()
		if len(used) > 0 {
			msg = h.t(c, "err.theme_in_use", strings.Join(used, ", "))
		}
		c.Redirect(http.StatusSeeOther, "/dashboard/admin/themes?error="+url.QueryEscape(msg))
		return
	}
	c.Redirect(http.StatusSeeOther, "/dashboard/admin/themes?saved=1")
}

func (h *Handler) adminThemeFileSave(c *gin.Context) {
	name := c.Param("name")
	relPath := strings.TrimSpace(c.PostForm("path"))
	content := c.PostForm("content")
	if err := blog.SaveThemeFile(h.Cfg.Storage.DataDir, name, relPath, []byte(content)); err != nil {
		c.Redirect(http.StatusSeeOther, "/dashboard/admin/themes?error="+url.QueryEscape(err.Error()))
		return
	}
	c.Redirect(http.StatusSeeOther, "/dashboard/admin/themes?saved=1")
}
