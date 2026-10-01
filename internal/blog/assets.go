package blog

import (
	"embed"
	"errors"
	"net/http"
	"net/url"
	"path"
	"slices"
	"strings"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	"github.com/helantianshen/oss-sync/internal/filestore"
	"github.com/helantianshen/oss-sync/internal/markdown"
	"github.com/helantianshen/oss-sync/internal/models"
)

//go:embed assets/default/* assets/development-template/* assets/papertrail/* assets/scaffold/*
var themeAssetsFS embed.FS

type blogAssetResolver struct {
	shareID      string
	markdownPath string
}

// NewAssetResolver 将 Markdown 资源路径映射为分享资源地址
func NewAssetResolver(shareID string) markdown.AssetResolver {
	return blogAssetResolver{shareID: shareID}
}

func (r blogAssetResolver) ResolveAsset(reference string) string {
	if isRemoteReference(reference) {
		return reference
	}
	escaped := strings.ReplaceAll(url.QueryEscape(reference), "+", "%20")
	escaped = strings.ReplaceAll(escaped, "%2F", "/")
	assetURL := "/assets/" + r.shareID + "?ref=" + escaped
	if r.markdownPath != "" {
		assetURL += "&source=" + url.QueryEscape(r.markdownPath)
	}
	return assetURL
}

func (h *Handler) handleSharedAsset(c *gin.Context) {
	var share models.Share
	if err := h.DB.Where("share_id = ?", c.Param("share_id")).First(&share).Error; err != nil {
		c.Status(http.StatusNotFound)
		return
	}
	reference := strings.TrimSpace(c.Query("ref"))
	if reference == "" || isRemoteReference(reference) {
		c.Status(http.StatusNotFound)
		return
	}
	file, err := h.resolveSharedAsset(share, reference, c.Query("source"))
	if err != nil {
		c.Status(http.StatusNotFound)
		return
	}
	abs := filestore.DiskPath(h.Cfg.Storage.DataDir, file)
	c.Header("X-Content-Type-Options", "nosniff")
	c.Header("Content-Security-Policy", "sandbox; default-src 'none'; img-src data:; style-src 'unsafe-inline'; font-src data:")
	c.File(abs)
}

func (h *Handler) resolveSharedAsset(share models.Share, reference, source string) (models.File, error) {
	if !share.IsFolder {
		if source != "" && source != share.TargetPath || !h.markdownReferencesAsset(share.UserID, share.VaultID, share.TargetPath, reference) {
			return models.File{}, gorm.ErrRecordNotFound
		}
		return h.resolveAssetFile(share.UserID, share.VaultID, share.TargetPath, reference)
	}
	prefix := strings.TrimSuffix(share.TargetPath, "/") + "/"
	var files []models.File
	query := h.DB.Where(
		"user_id = ? AND vault_id = ? AND path LIKE ? ESCAPE '\\' AND is_deleted = ? AND type = ?",
		share.UserID, share.VaultID, likePrefix(prefix), false, "markdown",
	)
	if source != "" {
		query = query.Where("path = ?", source)
	}
	if err := query.Order("path asc").Find(&files).Error; err != nil {
		return models.File{}, err
	}
	for _, file := range files {
		if h.markdownReferencesAsset(share.UserID, share.VaultID, file.Path, reference) {
			asset, err := h.resolveAssetFile(share.UserID, share.VaultID, file.Path, reference)
			if !errors.Is(err, gorm.ErrRecordNotFound) {
				return asset, err
			}
		}
	}
	return models.File{}, gorm.ErrRecordNotFound
}

func (h *Handler) markdownReferencesAsset(userID uint, vaultID, markdownPath, reference string) bool {
	var file models.File
	if err := h.DB.Where(
		"user_id = ? AND vault_id = ? AND path = ? AND is_deleted = ? AND type = ?",
		userID, vaultID, markdownPath, false, "markdown",
	).First(&file).Error; err != nil {
		return false
	}
	abs := filestore.DiskPath(h.Cfg.Storage.DataDir, file)
	raw, err := readFile(abs)
	if err != nil {
		return false
	}
	references, err := markdown.ReferencedAssets(string(raw))
	if err != nil {
		return false
	}
	// 仅出现在 frontmatter image 中的封面也属于该文章的引用
	if fm, _ := splitFrontmatter(string(raw)); fm.image != "" && fm.image == reference {
		return true
	}
	return slices.Contains(references, reference)
}

func (h *Handler) resolveAssetFile(userID uint, vaultID, markdownPath, reference string) (models.File, error) {
	if decoded, err := url.PathUnescape(reference); err == nil {
		reference = decoded
	}
	lookup := func(candidate string) (models.File, error) {
		var file models.File
		if candidate == ".." || strings.HasPrefix(candidate, "../") || strings.Contains(candidate, "\\") {
			return file, gorm.ErrRecordNotFound
		}
		err := h.DB.Where("user_id = ? AND vault_id = ? AND is_deleted = ? AND type = ? AND path = ?",
			userID, vaultID, false, "attachment", candidate).First(&file).Error
		return file, err
	}
	// 显式相对路径只相对文章解析，其他路径优先兼容仓库根目录引用
	explicitRelative := strings.HasPrefix(reference, "./") || strings.HasPrefix(reference, "../")
	if !explicitRelative {
		file, err := lookup(path.Clean(strings.TrimPrefix(reference, "/")))
		if !errors.Is(err, gorm.ErrRecordNotFound) {
			return file, err
		}
	}
	if !strings.HasPrefix(reference, "/") {
		file, err := lookup(path.Clean(path.Join(path.Dir(markdownPath), reference)))
		if !errors.Is(err, gorm.ErrRecordNotFound) {
			return file, err
		}
	}
	if explicitRelative || strings.Contains(reference, "/") {
		return models.File{}, gorm.ErrRecordNotFound
	}
	// Obsidian 裸文件名引用允许跨目录匹配，SQL 通配符必须作为文件名字符处理
	escaped := strings.NewReplacer("\\", "\\\\", "%", "\\%", "_", "\\_").Replace(reference)
	var file models.File
	err := h.DB.Where(
		"user_id = ? AND vault_id = ? AND is_deleted = ? AND type = ? AND path LIKE ? ESCAPE '\\'",
		userID, vaultID, false, "attachment", "%/"+escaped,
	).Order("m_time desc").Order("path asc").First(&file).Error
	return file, err
}

func (h *Handler) serveDefaultTheme(c *gin.Context, filename string) bool {
	if filename != "style.css" && filename != "theme.js" && filename != "template.html" {
		return false
	}
	content, err := themeAssetsFS.ReadFile("assets/default/" + filename)
	if err != nil {
		return false
	}
	contentType := "application/javascript; charset=utf-8"
	if filename == "style.css" {
		contentType = "text/css; charset=utf-8"
	}
	if filename == "template.html" {
		contentType = "text/html; charset=utf-8"
	}
	c.Data(http.StatusOK, contentType, content)
	return true
}

// serveBuiltinTheme 提供内置主题（default / papertrail）的静态资源
func (h *Handler) serveBuiltinTheme(c *gin.Context, theme, filename string) bool {
	switch theme {
	case "default":
		return h.serveDefaultTheme(c, filename)
	case "papertrail":
		if filename != "style.css" && filename != "theme.js" && filename != "template.html" {
			return false
		}
		content, err := themeAssetsFS.ReadFile("assets/papertrail/" + filename)
		if err != nil {
			return false
		}
		contentType := "application/javascript; charset=utf-8"
		if filename == "style.css" {
			contentType = "text/css; charset=utf-8"
		}
		if filename == "template.html" {
			contentType = "text/html; charset=utf-8"
		}
		c.Data(http.StatusOK, contentType, content)
		return true
	}
	return false
}

func isRemoteReference(reference string) bool {
	lower := strings.ToLower(reference)
	return strings.HasPrefix(lower, "http://") || strings.HasPrefix(lower, "https://") || strings.HasPrefix(lower, "data:")
}
