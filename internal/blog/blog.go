// Package blog 提供公开分享页面和主题资源：
//
//   - GET /p/:share_id          单篇分享渲染
//   - GET /p/:share_id/*subpath 文件夹分享（subpath 空→目录树；命中文件→渲染）
//   - GET /themes/:theme/*      静态主题资源
package blog

import (
	"bytes"
	"embed"
<<<<<<< HEAD
=======
	"encoding/json"
>>>>>>> 3b7aaacb143eff9df5a728b914a633fc58e70a6b
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

	"github.com/oss/oss-server/internal/config"
	"github.com/oss/oss-server/internal/filestore"
	"github.com/oss/oss-server/internal/markdown"
	"github.com/oss/oss-server/internal/models"
<<<<<<< HEAD
	"github.com/oss/oss-server/internal/settingspolicy"
=======
>>>>>>> 3b7aaacb143eff9df5a728b914a633fc58e70a6b
)

//go:embed templates/*.html
var templatesFS embed.FS

type Handler struct {
	DB  *gorm.DB
	Cfg *config.Config
	tpl *template.Template
}

func New(db *gorm.DB, cfg *config.Config) (*Handler, error) {
	tpl, err := template.ParseFS(templatesFS, "templates/*.html")
	if err != nil {
		return nil, fmt.Errorf("parse blog templates: %w", err)
	}
	return &Handler{DB: db, Cfg: cfg, tpl: tpl}, nil
}

// Register 挂载无需登录的公开分享路由。
func (h *Handler) Register(r *gin.Engine) {
<<<<<<< HEAD
	r.GET("/", h.handleHome)
	r.GET("/b/:vault_id", h.handleVaultBlog)
=======
>>>>>>> 3b7aaacb143eff9df5a728b914a633fc58e70a6b
	r.GET("/p/:share_id", h.handleSingle)
	r.GET("/p/:share_id/*subpath", h.handleFolder)
	r.GET("/assets/:share_id", h.handleSharedAsset)
	r.GET("/themes/:theme/*filepath", h.handleThemeAsset)
}

// shareResolver 实现 markdown.LinkResolver。
// 索引按文件名匹配分享；同名时使用最近创建的分享。
type shareResolver struct {
	index map[string]string // basename(无 .md) -> share_id
}

var _ markdown.LinkResolver = (*shareResolver)(nil)

func (r *shareResolver) Resolve(linkText string) string {
	if r == nil {
		return ""
	}
	return r.index[linkText]
}

// buildResolver 构建当前 Vault 中可公开访问的双链索引。
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

	// 文件夹内的文章没有独立 share_id，双链统一指向文件夹分享。
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
	ThemeName     string
	ThemeBaseURL  string
	ThemeConfigJS template.JS
	CustomHeader  template.HTML
	CustomFooter  template.HTML
	ContentHTML   template.HTML
	IsFolder      bool
	FolderTitle   string
	FooterNotice  template.HTML
<<<<<<< HEAD
	// papertrail 博客字段。
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
		ThemeName:     setting.ThemeName,
		ThemeBaseURL:  themeBaseURL(setting.ThemeName),
		ThemeConfigJS: template.JS(mustJSON(setting.ThemeConfig)),
		CustomHeader:  customHeader,
		CustomFooter:  customFooter,
		ShareID:       share.ShareID,
		AllowCopy:     share.AllowCopy,
		BlogHomeURL:   blogHomeURL,
		BlogName:      cfg.BlogName,
		Description:   cfg.Description,
		LogoURL:       cfg.LogoURL,
		LogoSize:      cfg.LogoSize,
		LogoShape:     cfg.LogoShape,
		Buttons:       cfg.Buttons,
	}
}

func trimByRunes(value string, maxLen int) string {
	value = strings.TrimSpace(value)
	runes := []rune(value)
	if len(runes) > maxLen {
		return string(runes[:maxLen])
	}
	return string(runes)
=======
>>>>>>> 3b7aaacb143eff9df5a728b914a633fc58e70a6b
}

// loadVaultSettings 优先读取 Vault 配置，并兼容旧版用户级配置。
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
<<<<<<< HEAD
	if IsBuiltinTheme(p.ThemeName) {
		if p.ThemeName == "papertrail" {
			h.renderBuiltinTheme(c, p, "papertrail")
			return
		}
		p.ThemeName = "default"
		p.ThemeBaseURL = "/themes/default"
	} else {
=======
	if p.ThemeName != "default" {
>>>>>>> 3b7aaacb143eff9df5a728b914a633fc58e70a6b
		if custom, err := h.customThemeTemplate(p.ThemeName); err == nil {
			var rendered bytes.Buffer
			if err := custom.Execute(&rendered, p); err == nil {
				c.Header("Content-Type", "text/html; charset=utf-8")
				_, _ = c.Writer.Write(rendered.Bytes())
				return
			}
		}
		// An invalid or incomplete custom theme must not make a published note
		// unavailable. Fall back to the built-in page and assets instead.
		p.ThemeName = "default"
		p.ThemeBaseURL = "/themes/default"
	}
	c.Header("Content-Type", "text/html; charset=utf-8")
	if err := h.tpl.ExecuteTemplate(c.Writer, "base.html", p); err != nil {
		_ = err
	}
}

<<<<<<< HEAD
// renderBuiltinTheme 使用内置模板渲染（papertrail 等）。
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

=======
>>>>>>> 3b7aaacb143eff9df5a728b914a633fc58e70a6b
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
	html, err := markdown.RenderMarkdownWithAssets(resolver, blogAssetResolver{shareID: share.ShareID}, raw)
	if err != nil {
		c.String(http.StatusInternalServerError, "render failed: %v", err)
		return
	}

	us, _ := h.loadVaultSettings(share.UserID, share.VaultID)
<<<<<<< HEAD
	params := h.shareRenderParams(share, us)
	params.Title = basenameNoExt(f.Path) + " · OSS"
	params.ContentHTML = template.HTML(html)
=======
	themeConfigJSON, _ := json.Marshal(us.ThemeConfig)
	params := renderParams{
		Title:         basenameNoExt(f.Path) + " · OSS",
		ThemeName:     us.ThemeName,
		ThemeBaseURL:  themeBaseURL(us.ThemeName),
		ThemeConfigJS: template.JS(themeConfigJSON),
		CustomHeader:  template.HTML(us.CustomHeader),
		CustomFooter:  template.HTML(us.CustomFooter),
		ContentHTML:   template.HTML(html),
	}
>>>>>>> 3b7aaacb143eff9df5a728b914a633fc58e70a6b
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
<<<<<<< HEAD
	params := h.shareRenderParams(share, us)
	params.Title = "Folder · " + share.TargetPath
	params.IsFolder = true
	params.FolderTitle = share.TargetPath
	params.ContentHTML = template.HTML(b.String())
=======
	themeConfigJSON, _ := json.Marshal(us.ThemeConfig)
	params := renderParams{
		Title:         "Folder · " + share.TargetPath,
		ThemeName:     us.ThemeName,
		ThemeBaseURL:  themeBaseURL(us.ThemeName),
		ThemeConfigJS: template.JS(themeConfigJSON),
		CustomHeader:  template.HTML(us.CustomHeader),
		CustomFooter:  template.HTML(us.CustomFooter),
		IsFolder:      true,
		FolderTitle:   share.TargetPath,
		ContentHTML:   template.HTML(b.String()),
	}
>>>>>>> 3b7aaacb143eff9df5a728b914a633fc58e70a6b
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
	html, err := markdown.RenderMarkdownWithAssets(resolver, blogAssetResolver{shareID: share.ShareID}, raw)
	if err != nil {
		c.String(http.StatusInternalServerError, "render failed: %v", err)
		return
	}

	us, _ := h.loadVaultSettings(share.UserID, share.VaultID)
<<<<<<< HEAD
	params := h.shareRenderParams(share, us)
	params.Title = basenameNoExt(f.Path) + " · " + share.TargetPath
	params.ContentHTML = template.HTML(html)
=======
	themeConfigJSON, _ := json.Marshal(us.ThemeConfig)
	params := renderParams{
		Title:         basenameNoExt(f.Path) + " · " + share.TargetPath,
		ThemeName:     us.ThemeName,
		ThemeBaseURL:  themeBaseURL(us.ThemeName),
		ThemeConfigJS: template.JS(themeConfigJSON),
		CustomHeader:  template.HTML(us.CustomHeader),
		CustomFooter:  template.HTML(us.CustomFooter),
		ContentHTML:   template.HTML(html),
	}
>>>>>>> 3b7aaacb143eff9df5a728b914a633fc58e70a6b
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
<<<<<<< HEAD
	if h.serveBuiltinTheme(c, theme, fp) {
		return
	}
	if theme == "default" {
		// default 是内置只读主题，不允许从磁盘加载同名自定义目录。
		c.Status(http.StatusNotFound)
=======
	if theme == "default" && h.serveDefaultTheme(c, fp) {
>>>>>>> 3b7aaacb143eff9df5a728b914a633fc58e70a6b
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

// likePrefix escapes SQL LIKE metacharacters before adding the only wildcard
// we intend: descendants of the selected folder. Both SQLite and PostgreSQL
// understand the explicit backslash ESCAPE clause used by callers.
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
