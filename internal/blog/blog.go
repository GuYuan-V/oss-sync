// Package blog 提供公开 Vault 目录、分享笔记与主题资源的渲染
package blog

import (
	"bytes"
	"context"
	"embed"
	"errors"
	"fmt"
	"html/template"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	"github.com/helantianshen/oss-sync/internal/config"
	"github.com/helantianshen/oss-sync/internal/filestore"
	"github.com/helantianshen/oss-sync/internal/markdown"
	"github.com/helantianshen/oss-sync/internal/models"
	"github.com/helantianshen/oss-sync/internal/settingspolicy"
)

//go:embed templates/*.html
var templatesFS embed.FS

type Handler struct {
	DB          *gorm.DB
	Cfg         *config.Config
	tpl         *template.Template
	pluginHooks PluginHookRunner
	pluginData  PluginDataHookRunner
}

// PluginHookRunner 描述博客渲染所需的宿主插件钩子契约
type PluginHookRunner interface {
	ApplyHook(context.Context, string, PluginHookPayload) (string, error)
}

// PluginDataHookRunner 收集插件为模板注入的展示数据。
// 每个注册了 blog.data 钩子的插件返回一段 JSON，宿主以插件 ID 为键合并到 .PluginData
type PluginDataHookRunner interface {
	ApplyHookData(context.Context, string, PluginDataPayload) (map[string]any, error)
}

// PluginDataPayload 是 blog.data 钩子的输入，插件据此返回当前页面应展示的数据。
// HTTP 上下文允许受信插件自建会员 Cookie、评论身份和任意访问策略，不要求宿主预定义业务模型
type PluginDataPayload struct {
	VaultID    string              `json:"vault_id"`
	Theme      string              `json:"theme"`
	ShareID    string              `json:"share_id"`
	Path       string              `json:"path"`
	IsHome     bool                `json:"is_home"`
	IsFolder   bool                `json:"is_folder"`
	Method     string              `json:"method"`
	RequestURL string              `json:"request_url"`
	Query      map[string][]string `json:"query,omitempty"`
	Headers    map[string][]string `json:"headers,omitempty"`
	Cookies    map[string]string   `json:"cookies,omitempty"`
	ClientIP   string              `json:"client_ip,omitempty"`
}

type PluginHookPayload struct {
	VaultID  string         `json:"vault_id"`
	Theme    string         `json:"theme"`
	Content  string         `json:"content"`
	Metadata map[string]any `json:"metadata,omitempty"`
	Settings map[string]any `json:"settings,omitempty"`
}

// SetPluginHooks 接入受信服务端插件钩子，供博客渲染调用
func (h *Handler) SetPluginHooks(runner PluginHookRunner) {
	h.pluginHooks = runner
}

// SetPluginDataHooks 接入插件展示数据注入，模板通过 index .PluginData "插件ID" 或 pluginField 读取
func (h *Handler) SetPluginDataHooks(runner PluginDataHookRunner) {
	h.pluginData = runner
}

func New(db *gorm.DB, cfg *config.Config) (*Handler, error) {
	tpl, err := template.ParseFS(templatesFS, "templates/*.html")
	if err != nil {
		return nil, fmt.Errorf("parse blog templates: %w", err)
	}
	return &Handler{DB: db, Cfg: cfg, tpl: tpl}, nil
}

// Register 挂载无需登录的公开分享路由
func (h *Handler) Register(r *gin.Engine) {
	r.GET("/", h.handleHome)
	r.GET("/b/:vault_id", h.handleVaultBlog)
	r.GET("/p/:share_id", h.handleSingle)
	r.GET("/p/:share_id/*subpath", h.handleFolder)
	r.GET("/assets/:share_id", h.handleSharedAsset)
	r.GET("/themes/:theme/*filepath", h.handleThemeAsset)
}

// shareResolver 实现 markdown.LinkResolver
// 索引按文件名匹配分享；同名时使用最近创建的分享
type shareResolver struct {
	index map[string]string // 键为去掉 .md 后缀的文件名，值为 share_id
}

var _ markdown.LinkResolver = (*shareResolver)(nil)

func (r *shareResolver) Resolve(linkText string) string {
	if r == nil {
		return ""
	}
	return r.index[linkText]
}

// buildResolver 构建当前 Vault 中可公开访问的双链索引
func (h *Handler) buildResolver(userID uint, vaultID string) *shareResolver {
	type shareRow struct {
		ShareID    string
		TargetPath string
		IsFolder   bool
		CreatedAt  time.Time
	}
	var rows []shareRow
	h.DB.Model(&models.Share{}).
		Select("share_id", "target_path", "is_folder", "created_at").
		Where("user_id = ? AND vault_id = ?", userID, vaultID).
		Find(&rows)

	latest := map[string]shareRow{}
	for _, r := range rows {
		base := basenameNoExt(r.TargetPath)
		if base == "" {
			continue
		}
		if cur, ok := latest[base]; !ok || r.CreatedAt.After(cur.CreatedAt) {
			latest[base] = r
		}
	}

	// 文件夹内的文章没有独立 share_id，双链统一指向文件夹分享
	for _, r := range rows {
		if !r.IsFolder {
			continue
		}
		var files []models.File
		prefix := strings.TrimSuffix(r.TargetPath, "/") + "/"
		h.DB.Where(
			"user_id = ? AND vault_id = ? AND path LIKE ? ESCAPE '\\' AND is_deleted = ? AND type = ?",
			userID, vaultID, likePrefix(prefix), false, "markdown",
		).Find(&files)
		for _, f := range files {
			base := basenameNoExt(f.Path)
			if base == "" {
				continue
			}
			if _, ok := latest[base]; !ok {
				latest[base] = r
			}
		}
	}

	idx := make(map[string]string, len(latest))
	for base, r := range latest {
		idx[base] = r.ShareID
	}
	return &shareResolver{index: idx}
}

func basenameNoExt(p string) string {
	base := path.Base(p)
	return strings.TrimSuffix(base, filepath.Ext(base))
}

type renderParams struct {
	Title         string
	ArticleTitle  string
	ThemeName     string
	VaultID       string
	ThemeBaseURL  string
	ThemeConfigJS template.JS
	CustomHeader  template.HTML
	CustomFooter  template.HTML
	ContentHTML   template.HTML
	IsFolder      bool
	FolderTitle   string
	FooterNotice  template.HTML
	// papertrail 博客字段
	IsHome      bool
	ShareID     string
	AllowCopy   bool
	BlogHomeURL string
	BlogName    string
	Description string
	LogoURL     string
	LogoSize    int
	LogoShape   string
	Buttons     []PaperTrailButton
	HomePosts   []HomePost
	// 自定义主题可用的横幅与文章元数据
	BannerURL       string
	MobileBannerURL string
	ArticlePost     ArticleMeta
	// 插件注入的展示数据；插件 ID 可含连字符，模板用 index/pluginField 读取；
	// 已注册插件注入空对象，未设字段取零值，避免整页回退
	PluginData map[string]any
	// FilePath 是当前文章在仓库内的相对路径，仅用于插件数据钩子上下文，不直接渲染
	FilePath string
}

func (h *Handler) shareRenderParams(share models.Share, setting *models.VaultSetting) renderParams {
	cfg := ParsePaperTrailConfig(setting.ThemeConfig)
	blogHomeURL := ""
	if setting.IsPublicBlog {
		blogHomeURL = "/b/" + share.VaultID
	}
	var customHeader, customFooter template.HTML
	if settingspolicy.CustomFragmentsEnabled(h.DB) {
		customHeader = renderSafeCustomFragment(setting.CustomHeader)
		customFooter = renderSafeCustomFragment(setting.CustomFooter)
	}
	return renderParams{
		ThemeName:       setting.ThemeName,
		VaultID:         share.VaultID,
		ThemeBaseURL:    themeBaseURL(setting.ThemeName),
		ThemeConfigJS:   template.JS(mustJSON(setting.ThemeConfig)),
		CustomHeader:    customHeader,
		CustomFooter:    customFooter,
		ShareID:         share.ShareID,
		AllowCopy:       share.AllowCopy,
		BlogHomeURL:     blogHomeURL,
		BlogName:        cfg.BlogName,
		Description:     cfg.Description,
		LogoURL:         cfg.LogoURL,
		LogoSize:        cfg.LogoSize,
		LogoShape:       cfg.LogoShape,
		Buttons:         cfg.Buttons,
		BannerURL:       cfg.BannerURL,
		MobileBannerURL: cfg.MobileBannerURL,
	}
}

func trimByRunes(value string, maxLen int) string {
	value = strings.TrimSpace(value)
	runes := []rune(value)
	if len(runes) > maxLen {
		return string(runes[:maxLen])
	}
	return string(runes)
}

// loadVaultSettings 优先读取 Vault 配置，并兼容旧版用户级配置
func (h *Handler) loadVaultSettings(userID uint, vaultID string) (*models.VaultSetting, error) {
	var vs models.VaultSetting
	if err := h.DB.Where("vault_id = ?", vaultID).First(&vs).Error; err == nil {
		if vs.ThemeName == "" {
			vs.ThemeName = "default"
		}
		return &vs, nil
	} else if !errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, err
	}
	var us models.UserSetting
	if err := h.DB.Where("user_id = ?", userID).First(&us).Error; err != nil {
		return &models.VaultSetting{VaultID: vaultID, ThemeName: "default"}, nil
	}
	return &models.VaultSetting{
		VaultID:           vaultID,
		ThemeName:         us.ThemeName,
		ThemeConfig:       us.ThemeConfig,
		CustomHeader:      us.CustomHeader,
		CustomFooter:      us.CustomFooter,
		KeepDirectoryTree: us.KeepDirectoryTree,
	}, nil
}

func (h *Handler) renderTemplate(c *gin.Context, p renderParams) {
	// 先收集插件展示数据，内容过滤与主题渲染都可能依赖其中的上下文
	p.PluginData = h.collectPluginData(c, p)
	if h.pluginHooks != nil && p.ContentHTML != "" {
		filtered, err := h.pluginHooks.ApplyHook(c.Request.Context(), "blog.content", PluginHookPayload{
			VaultID: p.VaultID,
			Theme:   p.ThemeName,
			Content: string(p.ContentHTML),
		})
		if err == nil {
			p.ContentHTML = template.HTML(filtered)
		}
	}
	if h.pluginHooks != nil && p.ContentHTML != "" {
		filtered, err := h.pluginHooks.ApplyHook(c.Request.Context(), "theme.render", PluginHookPayload{
			VaultID: p.VaultID,
			Theme:   p.ThemeName,
			Content: string(p.ContentHTML),
		})
		if err == nil {
			p.ContentHTML = template.HTML(filtered)
		}
	}
	if IsBuiltinTheme(p.ThemeName) {
		if p.ThemeName == "papertrail" {
			h.renderBuiltinTheme(c, p, "papertrail")
			return
		}
		p.ThemeName = "default"
		p.ThemeBaseURL = "/themes/default"
	} else {
		custom, err := h.customThemeTemplate(p.ThemeName)
		if err == nil {
			var rendered bytes.Buffer
			if execErr := custom.Execute(&rendered, p); execErr == nil {
				c.Header("Content-Type", "text/html; charset=utf-8")
				_, _ = c.Writer.Write(rendered.Bytes())
				return
			} else {
				err = execErr
			}
		}
		// 自定义主题无效或不完整时不得影响已发布笔记，回退到内置页面与资源；
		// 通过响应头暴露回退原因，避免“静默变默认主题”难以排查
		if err != nil {
			c.Header("X-Theme-Fallback", sanitizeHeaderValue(p.ThemeName+": "+err.Error()))
		}
		p.ThemeName = "default"
		p.ThemeBaseURL = "/themes/default"
	}
	c.Header("Content-Type", "text/html; charset=utf-8")
	if err := h.tpl.ExecuteTemplate(c.Writer, "base.html", p); err != nil {
		_ = err
	}
}

// renderBuiltinTheme 使用内置模板渲染（papertrail 等）
func (h *Handler) renderBuiltinTheme(c *gin.Context, p renderParams, themeName string) {
	raw, err := themeAssetsFS.ReadFile("assets/" + themeName + "/template.html")
	if err != nil {
		p.ThemeName = "default"
		p.ThemeBaseURL = "/themes/default"
		c.Header("Content-Type", "text/html; charset=utf-8")
		_ = h.tpl.ExecuteTemplate(c.Writer, "base.html", p)
		return
	}
	tpl, err := template.New("builtin-" + themeName).Option("missingkey=zero").Parse(string(raw))
	if err != nil {
		p.ThemeName = "default"
		p.ThemeBaseURL = "/themes/default"
		c.Header("Content-Type", "text/html; charset=utf-8")
		_ = h.tpl.ExecuteTemplate(c.Writer, "base.html", p)
		return
	}
	var rendered bytes.Buffer
	if err := tpl.Execute(&rendered, p); err != nil {
		p.ThemeName = "default"
		p.ThemeBaseURL = "/themes/default"
		c.Header("Content-Type", "text/html; charset=utf-8")
		_ = h.tpl.ExecuteTemplate(c.Writer, "base.html", p)
		return
	}
	c.Header("Content-Type", "text/html; charset=utf-8")
	_, _ = c.Writer.Write(rendered.Bytes())
}

func (h *Handler) renderRemoved(c *gin.Context) {
	c.Header("Content-Type", "text/html; charset=utf-8")
	c.Status(http.StatusNotFound)
	_ = h.tpl.ExecuteTemplate(c.Writer, "removed.html", nil)
}

func (h *Handler) handleSingle(c *gin.Context) {
	shareID := c.Param("share_id")
	var share models.Share
	if err := h.DB.Where("share_id = ?", shareID).First(&share).Error; err != nil {
		h.renderRemoved(c)
		return
	}
	if share.IsFolder {
		c.Redirect(http.StatusFound, "/p/"+shareID+"/")
		return
	}

	_ = h.DB.Model(&models.Share{}).Where("share_id = ?", shareID).
		UpdateColumn("views", gorm.Expr("views + 1")).Error

	var f models.File
	if err := h.DB.Where(
		"user_id = ? AND vault_id = ? AND path = ? AND is_deleted = ?",
		share.UserID, share.VaultID, share.TargetPath, false,
	).First(&f).Error; err != nil {
		h.renderRemoved(c)
		return
	}
	abs := filestore.DiskPath(h.Cfg.Storage.DataDir, f)
	raw, err := readFileUTF8(abs)
	if err != nil {
		h.renderRemoved(c)
		return
	}

	resolver := h.buildResolver(share.UserID, share.VaultID)
	assetResolver := blogAssetResolver{shareID: share.ShareID}
	// 有效 frontmatter 作为元数据使用并从正文隐藏，损坏区块保留原文
	fm, body := splitFrontmatter(raw)
	html, err := markdown.RenderMarkdownWithAssets(resolver, assetResolver, body)
	if err != nil {
		c.String(http.StatusInternalServerError, "render failed: %v", err)
		return
	}

	us, _ := h.loadVaultSettings(share.UserID, share.VaultID)
	params := h.shareRenderParams(share, us)
	params.ArticleTitle, params.ArticlePost = buildArticleMeta(fm, body, f.Path, f.UpdatedAt, assetResolver.ResolveAsset)
	params.FilePath = f.Path
	params.Title = params.ArticleTitle + " · OSS"
	params.ContentHTML = template.HTML(html)
	h.renderTemplate(c, params)
}

func (h *Handler) handleFolder(c *gin.Context) {
	shareID := c.Param("share_id")
	subpath := strings.TrimPrefix(c.Param("subpath"), "/")
	subpath = strings.TrimSuffix(subpath, "/")

	var share models.Share
	if err := h.DB.Where("share_id = ?", shareID).First(&share).Error; err != nil {
		h.renderRemoved(c)
		return
	}
	if !share.IsFolder {
		c.Redirect(http.StatusFound, "/p/"+shareID)
		return
	}

	_ = h.DB.Model(&models.Share{}).Where("share_id = ?", shareID).
		UpdateColumn("views", gorm.Expr("views + 1")).Error

	prefix := strings.TrimSuffix(share.TargetPath, "/") + "/"
	var files []models.File
	h.DB.Where(
		"user_id = ? AND vault_id = ? AND path LIKE ? ESCAPE '\\' AND is_deleted = ? AND type = ?",
		share.UserID, share.VaultID, likePrefix(prefix), false, "markdown",
	).
		Order("path asc").
		Find(&files)

	if subpath == "" {
		h.renderFolderTree(c, share, files)
		return
	}

	targetPath := prefix + subpath
	for _, f := range files {
		if f.Path == targetPath {
			h.renderFolderFile(c, share, f)
			return
		}
	}
	h.renderRemoved(c)
}

func (h *Handler) renderFolderTree(c *gin.Context, share models.Share, files []models.File) {
	var b strings.Builder
	b.WriteString("<ul class=\"oss-tree-list\">")
	for _, f := range files {
		rel := strings.TrimPrefix(f.Path, strings.TrimSuffix(share.TargetPath, "/")+"/")
		href := "/p/" + share.ShareID + "/" + escapedRelativeURL(rel)
		title := strings.TrimSuffix(path.Base(f.Path), filepath.Ext(f.Path))
		b.WriteString(fmt.Sprintf(`<li><a href="%s">%s</a></li>`, htmlEscape(href), htmlEscape(title)))
	}
	b.WriteString("</ul>")

	us, _ := h.loadVaultSettings(share.UserID, share.VaultID)
	params := h.shareRenderParams(share, us)
	params.Title = "Folder · " + share.TargetPath
	params.IsFolder = true
	params.FolderTitle = share.TargetPath
	params.ContentHTML = template.HTML(b.String())
	h.renderTemplate(c, params)
}

func (h *Handler) renderFolderFile(c *gin.Context, share models.Share, f models.File) {
	abs := filestore.DiskPath(h.Cfg.Storage.DataDir, f)
	raw, err := readFileUTF8(abs)
	if err != nil {
		h.renderRemoved(c)
		return
	}
	resolver := h.buildResolver(share.UserID, share.VaultID)
	assetResolver := blogAssetResolver{shareID: share.ShareID}
	fm, body := splitFrontmatter(raw)
	html, err := markdown.RenderMarkdownWithAssets(resolver, assetResolver, body)
	if err != nil {
		c.String(http.StatusInternalServerError, "render failed: %v", err)
		return
	}

	us, _ := h.loadVaultSettings(share.UserID, share.VaultID)
	params := h.shareRenderParams(share, us)
	params.ArticleTitle, params.ArticlePost = buildArticleMeta(fm, body, f.Path, f.UpdatedAt, assetResolver.ResolveAsset)
	params.FilePath = f.Path
	params.Title = params.ArticleTitle + " · " + share.TargetPath
	params.ContentHTML = template.HTML(html)
	h.renderTemplate(c, params)
}

func (h *Handler) handleThemeAsset(c *gin.Context) {
	theme := c.Param("theme")
	fp := c.Param("filepath")
	if err := ValidateThemeName(theme); err != nil || !validThemeAssetPath(fp) {
		c.String(http.StatusBadRequest, "invalid theme or path")
		return
	}
	fp = strings.TrimPrefix(fp, "/")
	if h.serveBuiltinTheme(c, theme, fp) {
		return
	}
	if theme == "default" {
		// default 是内置只读主题，不允许从磁盘加载同名自定义目录
		c.Status(http.StatusNotFound)
		return
	}
	abs := filepath.Join(h.Cfg.Storage.DataDir, "themes", theme, fp)
	c.File(abs)
}

func themeBaseURL(themeName string) string {
	if ValidateThemeName(themeName) != nil {
		themeName = "default"
	}
	return "/themes/" + themeName
}

func validThemeAssetPath(raw string) bool {
	path := strings.TrimPrefix(raw, "/")
	if path == "" || strings.Contains(path, "\\") || strings.ContainsRune(path, '\x00') {
		return false
	}
	clean := filepath.ToSlash(filepath.Clean(path))
	return clean != "." && !strings.HasPrefix(clean, "../") && clean != ".." && clean == path
}

func readFileUTF8(abs string) (string, error) {
	b, err := readFile(abs)
	if err != nil {
		return "", err
	}
	return string(b), nil
}

func readFile(abs string) ([]byte, error) {
	return osReadFile(abs)
}

var osReadFile = func(p string) ([]byte, error) {
	return os.ReadFile(p)
}

func htmlEscape(s string) string {
	return template.HTMLEscapeString(s)
}

// likePrefix 转义 SQL LIKE 元字符后只追加一个通配符，用于匹配所选目录的后代；调用方的反斜杠 ESCAPE 子句同时适用于 SQLite 与 PostgreSQL
func likePrefix(prefix string) string {
	replacer := strings.NewReplacer("\\", "\\\\", "%", "\\%", "_", "\\_")
	return replacer.Replace(prefix) + "%"
}

func escapedRelativeURL(rel string) string {
	parts := strings.Split(rel, "/")
	for i, part := range parts {
		parts[i] = url.PathEscape(part)
	}
	return strings.Join(parts, "/")
}
