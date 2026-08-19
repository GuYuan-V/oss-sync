// Package blog 公开博客首页与仓库公开入口。
package blog

import (
	"encoding/json"
	"fmt"
	"html/template"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/oss/oss-server/internal/filestore"
	"github.com/oss/oss-server/internal/models"
	"github.com/oss/oss-server/internal/settingspolicy"
)

// PaperTrailConfig 是 papertrail 博客设置的结构化配置。
type PaperTrailConfig struct {
	LogoURL     string             `json:"logo_url"`
	LogoSize    int                `json:"logo_size"`
	LogoShape   string             `json:"logo_shape"`
	BlogName    string             `json:"blog_name"`
	Description string             `json:"description"`
	Buttons     []PaperTrailButton `json:"buttons"`
}

// PaperTrailButton 博客自定义按钮。
type PaperTrailButton struct {
	Label    string `json:"label"`
	URL      string `json:"url"`
	IconURL  string `json:"icon_url"`
	Position int    `json:"position"`
}

// ParsePaperTrailConfig 从 ThemeConfig 解析结构化配置。
func ParsePaperTrailConfig(themeConfig map[string]any) PaperTrailConfig {
	cfg := PaperTrailConfig{}
	if themeConfig == nil {
		return cfg
	}
	if v, ok := themeConfig["logo_size"].(string); ok {
		cfg.LogoSize = parsePaperTrailLogoSize(v)
	} else if v, ok := themeConfig["logo_size"].(float64); ok {
		cfg.LogoSize = parsePaperTrailLogoSize(fmt.Sprintf("%d", int(v)))
	} else if v, ok := themeConfig["logo_size"].(int); ok {
		cfg.LogoSize = parsePaperTrailLogoSize(fmt.Sprintf("%d", v))
	}
	if v, ok := themeConfig["logo_url"].(string); ok {
		cfg.LogoURL = v
	}
	if v, ok := themeConfig["logo_shape"].(string); ok && (v == "square" || v == "circle") {
		cfg.LogoShape = v
	}
	if v, ok := themeConfig["blog_name"].(string); ok {
		cfg.BlogName = v
	}
	if v, ok := themeConfig["description"].(string); ok {
		cfg.Description = v
	}
	if rawButtons, ok := themeConfig["buttons"].([]any); ok {
		for _, raw := range rawButtons {
			m, ok := raw.(map[string]any)
			if !ok {
				continue
			}
			btn := PaperTrailButton{}
			if v, ok := m["label"].(string); ok {
				btn.Label = v
			}
			if v, ok := m["url"].(string); ok {
				btn.URL = v
			}
			if v, ok := m["icon_url"].(string); ok {
				btn.IconURL = v
			}
			if v, ok := m["position"].(float64); ok {
				btn.Position = int(v)
			} else if v, ok := m["position"].(int); ok {
				btn.Position = v
			}
			if btn.Label != "" && btn.URL != "" {
				cfg.Buttons = append(cfg.Buttons, btn)
			}
		}
	}
	return cfg
}

func parsePaperTrailLogoSize(raw string) int {
	value, err := strconv.Atoi(strings.TrimSpace(raw))
	if err != nil {
		return 0
	}
	if value < 10 || value > 192 {
		return 0
	}
	return value
}

// HomePost 首页文章条目。
type HomePost struct {
	Title   string
	Summary string
	URL     string
	Date    string
	Time    time.Time
}

// PublicBlog is one discoverable Vault on the unauthenticated server homepage.
type PublicBlog struct {
	Name        string
	Description string
	LogoURL     string
	LogoShape   string
	URL         string
}

type publicHomeData struct {
	Blogs []PublicBlog
}

// handleHome lists every Vault that explicitly enabled its public blog.
func (h *Handler) handleHome(c *gin.Context) {
	var settings []models.VaultSetting
	if err := h.DB.Where("is_public_blog = ?", true).Order("updated_at desc").Find(&settings).Error; err != nil {
		c.String(http.StatusInternalServerError, "load public blogs failed")
		return
	}
	blogs := make([]PublicBlog, 0, len(settings))
	for _, setting := range settings {
		var vault models.Vault
		if err := h.DB.Where("id = ?", setting.VaultID).First(&vault).Error; err != nil {
			continue
		}
		cfg := ParsePaperTrailConfig(setting.ThemeConfig)
		description := cfg.Description
		if description == "" {
			description = vault.Description
		}
		blogs = append(blogs, PublicBlog{
			Name:        blogTitle(cfg, vault.Name),
			Description: description,
			LogoURL:     cfg.LogoURL,
			LogoShape:   cfg.LogoShape,
			URL:         "/b/" + vault.ID,
		})
	}
	h.renderPublicHome(c, publicHomeData{Blogs: blogs})
}

func blogTitle(cfg PaperTrailConfig, fallback string) string {
	if cfg.BlogName != "" {
		return cfg.BlogName
	}
	return fallback
}

func (h *Handler) renderPublicHome(c *gin.Context, data publicHomeData) {
	c.Header("Content-Type", "text/html; charset=utf-8")
	c.Status(http.StatusOK)
	if err := h.tpl.ExecuteTemplate(c.Writer, "public_home.html", data); err != nil {
		c.String(http.StatusInternalServerError, "render failed")
	}
}

// homePosts 列出仓库中已单篇分享且目标仍存在的 Markdown 文章。
func (h *Handler) homePosts(userID uint, vaultID string) []HomePost {
	var shares []models.Share
	if err := h.DB.Where(
		"user_id = ? AND vault_id = ? AND is_folder = ?",
		userID, vaultID, false,
	).Order("created_at desc").Limit(100).Find(&shares).Error; err != nil {
		return nil
	}
	var posts []HomePost
	for _, share := range shares {
		var f models.File
		if err := h.DB.Where(
			"user_id = ? AND vault_id = ? AND path = ? AND is_deleted = ? AND type = ?",
			userID, vaultID, share.TargetPath, false, "markdown",
		).First(&f).Error; err != nil {
			continue
		}
		abs := filestore.DiskPath(h.Cfg.Storage.DataDir, f)
		raw, err := readFile(abs)
		if err != nil {
			continue
		}
		title, summary := extractPostMeta(string(raw), share.TargetPath)
		posts = append(posts, HomePost{
			Title:   title,
			Summary: summary,
			URL:     "/p/" + share.ShareID,
			Date:    f.UpdatedAt.Format("2006-01-02"),
			Time:    f.UpdatedAt,
		})
	}
	return posts
}

// extractPostMeta 提取文章标题与摘要（首个标题行 + 首个非空段落截断）。
func extractPostMeta(raw, fallbackTitle string) (string, string) {
	title := fallbackTitle
	summary := ""
	lines := strings.Split(raw, "\n")
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if title == fallbackTitle && strings.HasPrefix(trimmed, "#") {
			title = strings.TrimSpace(strings.TrimLeft(trimmed, "# "))
		}
		if summary == "" && trimmed != "" && !strings.HasPrefix(trimmed, "#") {
			summary = trimmed
		}
		if title != fallbackTitle && summary != "" {
			break
		}
	}
	if len(summary) > 120 {
		summary = summary[:120] + "…"
	}
	return title, summary
}

// handleVaultBlog 处理 /b/:vault_id 公开博客入口。
func (h *Handler) handleVaultBlog(c *gin.Context) {
	vaultID := c.Param("vault_id")
	var vault models.Vault
	if err := h.DB.Where("id = ?", vaultID).First(&vault).Error; err != nil {
		c.Status(http.StatusNotFound)
		return
	}
	var vs models.VaultSetting
	if err := h.DB.Where("vault_id = ?", vaultID).First(&vs).Error; err != nil {
		c.Status(http.StatusNotFound)
		return
	}
	if !vs.IsPublicBlog {
		c.Status(http.StatusNotFound)
		return
	}
	posts := h.homePosts(vault.OwnerID, vaultID)
	cfg := ParsePaperTrailConfig(vs.ThemeConfig)
	customEnabled := settingspolicy.CustomFragmentsEnabled(h.DB)
	params := renderParams{
		Title:         blogTitle(cfg, vault.Name),
		ThemeName:     vs.ThemeName,
		ThemeBaseURL:  themeBaseURL(vs.ThemeName),
		ThemeConfigJS: template.JS(mustJSON(vs.ThemeConfig)),
		CustomHeader:  renderSafeCustomFragmentEnabled(vs.CustomHeader, customEnabled),
		CustomFooter:  renderSafeCustomFragmentEnabled(vs.CustomFooter, customEnabled),
		IsHome:        true,
		BlogName:      cfg.BlogName,
		Description:   cfg.Description,
		LogoURL:       cfg.LogoURL,
		LogoSize:      cfg.LogoSize,
		LogoShape:     cfg.LogoShape,
		Buttons:       cfg.Buttons,
		HomePosts:     posts,
		BlogHomeURL:   "/b/" + vaultID,
	}
	h.renderTemplate(c, params)
}

// renderParams 扩展：博客首页与文章页字段。
// 字段在原 renderParams 上扩展（见 blog.go）。

func mustJSON(v any) string {
	raw, err := json.Marshal(v)
	if err != nil {
		return "{}"
	}
	return string(raw)
}
