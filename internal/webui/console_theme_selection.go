package webui

import (
	"net/http"
	"net/url"
	"path/filepath"
	"strings"

	"github.com/gin-gonic/gin"

	"github.com/helantianshen/oss-sync/internal/consoletheme"
	"github.com/helantianshen/oss-sync/internal/models"
)

func (h *Handler) selectedConsoleTheme(userID uint) string {
	name, _ := h.selectedConsoleThemeState(userID)
	return name
}

func (h *Handler) selectedConsoleThemeState(userID uint) (string, string) {
	var setting models.UserSetting
	if err := h.DB.Where("user_id = ?", userID).First(&setting).Error; err != nil {
		return consoletheme.BuiltinDefault, ""
	}
	name := setting.ConsoleThemeName
	if name == "" || !consoletheme.Exists(h.Cfg.Storage.DataDir, name) {
		if name != "" && name != consoletheme.BuiltinDefault {
			return consoletheme.BuiltinDefault, name
		}
		return consoletheme.BuiltinDefault, ""
	}
	return name, ""
}

func (h *Handler) selectedWebLanguage(userID uint) string {
	var setting models.UserSetting
	if err := h.DB.Where("user_id = ?", userID).First(&setting).Error; err != nil || setting.WebLanguage != "en" {
		return "zh"
	}
	return "en"
}

func (h *Handler) saveConsoleTheme(c *gin.Context) {
	user := h.webUser(c)
	name := strings.TrimSpace(c.PostForm("console_theme_name"))
	valid := false
	if h.pluginManager != nil {
		_, options := h.pluginManager.EnabledThemeOptions()
		for _, option := range options {
			if option.Name == name {
				valid = true
				break
			}
		}
	} else {
		valid = consoletheme.Exists(h.Cfg.Storage.DataDir, name)
	}
	if !valid {
		c.Redirect(http.StatusSeeOther, "/dashboard/account?error="+url.QueryEscape("服务器网页主题不存在"))
		return
	}
	if err := h.DB.Model(&models.UserSetting{}).Where("user_id = ?", user.ID).Update("console_theme_name", name).Error; err != nil {
		c.Redirect(http.StatusSeeOther, "/dashboard/account?error="+url.QueryEscape("保存服务器网页主题失败"))
		return
	}
	c.Redirect(http.StatusSeeOther, "/dashboard/account?theme_saved=1#console-theme")
}

func (h *Handler) consoleThemeAsset(c *gin.Context) {
	name := c.Param("theme")
	rel := strings.TrimPrefix(c.Param("filepath"), "/")
	path, err := consoletheme.AssetPath(h.Cfg.Storage.DataDir, name, rel)
	if err != nil {
		c.Status(http.StatusNotFound)
		return
	}
	c.Header("X-Content-Type-Options", "nosniff")
	if strings.EqualFold(filepath.Ext(path), ".css") {
		c.Header("Content-Type", "text/css; charset=utf-8")
	}
	c.File(path)
}
