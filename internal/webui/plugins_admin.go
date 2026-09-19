package webui

import (
	"bytes"
	"errors"
	"html/template"
	"io"
	"net/http"
	"net/url"
	"strings"

	"github.com/gin-gonic/gin"

	"github.com/helantianshen/oss-sync/internal/blog"
	"github.com/helantianshen/oss-sync/internal/consoletheme"
	"github.com/helantianshen/oss-sync/internal/markdown"
	"github.com/helantianshen/oss-sync/internal/models"
	"github.com/helantianshen/oss-sync/internal/serverplugin"
)

type adminPluginsData struct {
	Plugins   []serverplugin.PluginInfo
	GuideHTML template.HTML
	Error     string
	Saved     bool
}

func (h *Handler) adminPluginPage(c *gin.Context) {
	if h.pluginManager == nil {
		c.Status(http.StatusServiceUnavailable)
		return
	}
	content, err := h.pluginManager.RenderAdminPage(c.Request.Context(), c.Param("id"), c.Param("slug"))
	if err != nil {
		c.Status(http.StatusBadGateway)
		return
	}
	c.Data(http.StatusOK, "text/html; charset=utf-8", []byte(content))
}

func (h *Handler) adminPluginsPage(c *gin.Context) {
	d := adminPluginsData{Error: c.Query("error"), Saved: c.Query("saved") == "1"}
	guideName := "assets/plugin-guide.md"
	if h.userLang(c) == "zh" {
		guideName = "assets/plugin-guide.zh.md"
	}
	if source, err := webFS.ReadFile(guideName); err == nil {
		if guide, renderErr := markdown.RenderMarkdown(nil, string(source)); renderErr == nil {
			d.GuideHTML = template.HTML(guide)
		}
	}
	if h.pluginManager == nil {
		d.Error = h.t(c, "admin.plugins_unavailable")
		h.render(c, http.StatusServiceUnavailable, "admin-plugins", h.t(c, "page.admin_plugins"), "admin", "admin-plugins", d)
		return
	}
	plugins, err := h.pluginManager.List()
	if err != nil {
		d.Error = h.t(c, "admin.plugins_load_failed")
		h.render(c, http.StatusInternalServerError, "admin-plugins", h.t(c, "page.admin_plugins"), "admin", "admin-plugins", d)
		return
	}
	d.Plugins = plugins
	h.render(c, http.StatusOK, "admin-plugins", h.t(c, "page.admin_plugins"), "admin", "admin-plugins", d)
}

func (h *Handler) adminPluginUpload(c *gin.Context) {
	if h.pluginManager == nil {
		h.redirectPluginError(c, "admin.plugins_unavailable")
		return
	}
	file, err := c.FormFile("file")
	if err != nil {
		h.redirectPluginError(c, "admin.plugin_file_required")
		return
	}
	if !strings.HasSuffix(strings.ToLower(file.Filename), ".zip") {
		h.redirectPluginError(c, "admin.plugin_zip_required")
		return
	}
	reader, err := file.Open()
	if err != nil {
		h.redirectPluginError(c, "admin.plugin_read_failed")
		return
	}
	defer reader.Close()
	content, err := io.ReadAll(io.LimitReader(reader, serverplugin.MaxArchiveBytes+1))
	if err != nil {
		h.redirectPluginError(c, "admin.plugin_read_failed")
		return
	}
	if len(content) > serverplugin.MaxArchiveBytes {
		h.redirectPluginError(c, "admin.plugin_too_large")
		return
	}
	info, err := h.pluginManager.Install(c.Request.Context(), bytes.NewReader(content), int64(len(content)))
	if err != nil {
		if errors.Is(err, serverplugin.ErrPluginExists) {
			info, err = h.pluginManager.Upgrade(c.Request.Context(), bytes.NewReader(content), int64(len(content)))
			if err != nil {
				h.redirectPluginError(c, "admin.plugin_install_failed")
				return
			}
		} else {
			h.redirectPluginError(c, "admin.plugin_install_failed")
			return
		}
	}
	if err := h.pluginManager.Enable(c.Request.Context(), info.ID); err != nil {
		h.redirectPluginError(c, "admin.plugin_enable_failed")
		return
	}
	c.Redirect(http.StatusSeeOther, "/dashboard/admin/plugins?saved=1")
}

func (h *Handler) adminPluginEnable(c *gin.Context) {
	if h.pluginManager == nil {
		h.redirectPluginError(c, "admin.plugins_unavailable")
		return
	}
	if err := h.pluginManager.Enable(c.Request.Context(), c.Param("id")); err != nil {
		h.redirectPluginError(c, "admin.plugin_enable_failed")
		return
	}
	c.Redirect(http.StatusSeeOther, "/dashboard/admin/plugins?saved=1")
}

func (h *Handler) adminPluginDisable(c *gin.Context) {
	if h.pluginManager == nil {
		h.redirectPluginError(c, "admin.plugins_unavailable")
		return
	}
	if err := h.pluginManager.Disable(c.Request.Context(), c.Param("id")); err != nil {
		h.redirectPluginError(c, "admin.plugin_disable_failed")
		return
	}
	c.Redirect(http.StatusSeeOther, "/dashboard/admin/plugins?saved=1")
}

func (h *Handler) adminPluginDelete(c *gin.Context) {
	if h.pluginManager == nil {
		h.redirectPluginError(c, "admin.plugins_unavailable")
		return
	}
	var links []models.ServerPluginAssociation
	if err := h.DB.Where("plugin_id = ?", c.Param("id")).Find(&links).Error; err != nil {
		h.redirectPluginError(c, "admin.plugin_delete_failed")
		return
	}
	for _, link := range links {
		if link.Kind == "blog_theme" {
			if _, err := blog.DeleteTheme(h.DB, h.Cfg.Storage.DataDir, link.TargetID); err != nil {
				h.redirectPluginError(c, "admin.plugin_delete_association_failed")
				return
			}
		}
		if link.Kind == "console_theme" {
			if err := consoletheme.Delete(h.Cfg.Storage.DataDir, link.TargetID); err != nil && !errors.Is(err, consoletheme.ErrNotFound) {
				h.redirectPluginError(c, "admin.plugin_delete_association_failed")
				return
			}
		}
	}
	if err := h.pluginManager.Delete(c.Param("id")); err != nil {
		h.redirectPluginError(c, "admin.plugin_delete_failed")
		return
	}
	if err := h.DB.Where("plugin_id = ?", c.Param("id")).Delete(&models.ServerPluginAssociation{}).Error; err != nil {
		h.redirectPluginError(c, "admin.plugin_delete_failed")
		return
	}
	c.Redirect(http.StatusSeeOther, "/dashboard/admin/plugins?saved=1")
}

func (h *Handler) redirectPluginError(c *gin.Context, key string) {
	c.Redirect(http.StatusSeeOther, "/dashboard/admin/plugins?error="+url.QueryEscape(h.t(c, key)))
}
