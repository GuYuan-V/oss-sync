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

// selectedConsoleTheme 返回当前用户生效的服务器网页主题名
// 读取失败或主题已不存在时回退为内置主题
func (h *Handler) selectedConsoleTheme(userID uint) string {
	name, _ := h.selectedConsoleThemeState(userID)
	return name
}

// selectedConsoleThemeState 返回生效主题名与失效的已保存主题名
// 第二个返回值非空表示用户此前选择的主题已不可用，供页面提示回退原因
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

// selectedWebLanguage 返回当前用户的网页界面语言
// 用户设置缺失或取值不是 en 时一律回退为中文
func (h *Handler) selectedWebLanguage(userID uint) string {
	var setting models.UserSetting
	if err := h.DB.Where("user_id = ?", userID).First(&setting).Error; err != nil || setting.WebLanguage != "en" {
		return "zh"
	}
	return "en"
}

// saveConsoleTheme 保存用户的服务器网页主题：POST /dashboard/account/theme
// 主题名必须来自已启用插件提供的主题，或存在于数据目录；插件管理器缺失时只校验数据目录
// 校验或写库失败时重定向回个人中心并带上错误
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

// consoleThemeAsset 提供服务器主题的静态资源：GET /ui/themes/:theme/*filepath
// 内置主题从嵌入资源输出，自定义主题解析到数据目录；越界或不存在时返回 404
// CSS 文件显式声明 Content-Type，并始终禁止 MIME 嗅探
func (h *Handler) consoleThemeAsset(c *gin.Context) {
	name := c.Param("theme")
	rel := strings.TrimPrefix(c.Param("filepath"), "/")
	c.Header("X-Content-Type-Options", "nosniff")
	if content, ok := consoletheme.ReadBuiltinAsset(name, rel); ok {
		if strings.EqualFold(filepath.Ext(rel), ".css") {
			c.Header("Content-Type", "text/css; charset=utf-8")
		}
		c.Data(http.StatusOK, "application/octet-stream", content)
		return
	}
	path, err := consoletheme.AssetPath(h.Cfg.Storage.DataDir, name, rel)
	if err != nil {
		c.Status(http.StatusNotFound)
		return
	}
	if strings.EqualFold(filepath.Ext(path), ".css") {
		c.Header("Content-Type", "text/css; charset=utf-8")
	}
	c.File(path)
}
