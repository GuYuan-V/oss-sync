// 控制台主题选择
package webui

import (
	"net/http"
	"net/url"
	"path/filepath"
	"strings"

	"github.com/gin-gonic/gin"

	"github.com/oss/oss-server/internal/consoletheme"
	"github.com/oss/oss-server/internal/models"
)

func (h *Handler) selectedConsoleTheme(userID uint) string {
	var setting models.UserSetting
	if err := h.DB.Where("user_id = ?", userID).First(&setting).Error; err != nil {
		return consoletheme.BuiltinDefault
	}
	name := setting.ConsoleThemeName
	if name == "" || !consoletheme.Exists(h.Cfg.Storage.DataDir, name) {
		return consoletheme.BuiltinDefault
	}
	return name
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
	if !consoletheme.Exists(h.Cfg.Storage.DataDir, name) {
		c.Redirect(http.StatusSeeOther, "/dashboard/account?error="+url.QueryEscape("服务器网页主题不存在"))
		return
	}
	if err := h.DB.Model(&models.UserSetting{}).Where("user_id = ?", user.ID).
		Update("console_theme_name", name).Error; err != nil {
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

