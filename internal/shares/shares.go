// 分享管理
package shares

import (
	"crypto/rand"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	"github.com/oss/oss-server/internal/auth"
	"github.com/oss/oss-server/internal/config"
	"github.com/oss/oss-server/internal/models"
	"github.com/oss/oss-server/internal/vaultaccess"
)

type Handler struct {
	DB  *gorm.DB
	Cfg *config.Config
}

func New(db *gorm.DB, cfg *config.Config) *Handler {
	return &Handler{DB: db, Cfg: cfg}
}

const (
	shareIDLen    = 6
	maxIDAttempts = 8
)

var base62Alphabet = []byte("0123456789ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz")

type createRequest struct {
	VaultID            string `json:"vault_id"`
	TargetPath         string `json:"target_path" binding:"required"`
	IsFolder           bool   `json:"is_folder"`
	AllowCopy          bool   `json:"allow_copy"`
	RecursiveBacklinks bool   `json:"recursive_backlinks"`
}

type updateRequest struct {
	AllowCopy *bool `json:"allow_copy" binding:"required"`
}

type shareOut struct {
	ShareID    string `json:"share_id"`
	VaultID    string `json:"vault_id"`
	TargetPath string `json:"target_path"`
	IsFolder   bool   `json:"is_folder"`
	AllowCopy  bool   `json:"allow_copy"`
	Views      int    `json:"views"`
	URL        string `json:"url"`
	CreatedAt  string `json:"created_at"`
}

type createResponse struct {
	ShareID    string     `json:"share_id"`
	URL        string     `json:"url"`
	TargetPath string     `json:"target_path"`
	IsFolder   bool       `json:"is_folder"`
	Extra      []shareOut `json:"extra,omitempty"`
}

func (h *Handler) Register(r *gin.Engine) {
	g := r.Group("/api/shares", auth.Middleware(h.DB, h.Cfg))
	{
		g.POST("", h.Create)
		g.GET("", h.List)
		g.GET("/:id", h.Get)
		g.PATCH("/:id", h.Update)
		g.DELETE("/:id", h.Delete)
	}
}

// CreateWeb 是网页控制台使用的分享创建服务，校验文件存在并生成分享 ID。
func (h *Handler) CreateWeb(userID uint, vaultID, targetPath string, isFolder, allowCopy bool) (string, error) {
	targetPath = strings.TrimSpace(targetPath)
	if targetPath == "" || !isSafeSharePath(targetPath) {
		return "", fmt.Errorf("路径包含非法内容")
	}
	so, err := h.createOne(userID, vaultID, targetPath, isFolder, allowCopy)
	if err != nil {
		return "", err
	}
	return so.ShareID, nil
}

func (h *Handler) Create(c *gin.Context) {
	u, ok := auth.RequireUser(c)
	if !ok {
		return
	}
	var req createRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	req.TargetPath = strings.TrimSpace(req.TargetPath)
	if req.TargetPath == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "target_path is required"})
		return
	}
	if !isSafeSharePath(req.TargetPath) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "target_path contains illegal segments"})
		return
	}
	vault, role, err := h.resolveVault(u.ID, strings.TrimSpace(req.VaultID))
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "vault not found"})
		return
	}
	if !vaultaccess.CanManage(role) {
		c.JSON(http.StatusForbidden, gin.H{"error": "insufficient vault permission"})
		return
	}
	vaultID := vault.ID

	extra := []shareOut{}
	if req.RecursiveBacklinks && !req.IsFolder {
		links, err := h.collectBacklinks(vault.OwnerID, vaultID, req.TargetPath)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
		for _, p := range links {
			so, err := h.createOne(vault.OwnerID, vaultID, p, false, req.AllowCopy)
			if err != nil {
				continue
			}
			extra = append(extra, so)
		}
	}

	so, err := h.createOne(vault.OwnerID, vaultID, req.TargetPath, req.IsFolder, req.AllowCopy)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, createResponse{
		ShareID:    so.ShareID,
		URL:        so.URL,
		TargetPath: so.TargetPath,
		IsFolder:   so.IsFolder,
		Extra:      extra,
	})
}

func (h *Handler) createOne(
	userID uint,
	vaultID, targetPath string,
	isFolder, allowCopy bool,
) (shareOut, error) {
	if !isFolder {
		var cnt int64
		h.DB.Model(&models.File{}).
			Where(
				"user_id = ? AND vault_id = ? AND path = ? AND is_deleted = ?",
				userID, vaultID, targetPath, false,
			).
			Count(&cnt)
		if cnt == 0 {
			return shareOut{}, fmt.Errorf("file not found: %s", targetPath)
		}
	}

	var shareID string
	var lastErr error
	for attempt := 0; attempt < maxIDAttempts; attempt++ {
		id, err := genShareID()
		if err != nil {
			lastErr = err
			continue
		}
		rec := models.Share{
			ShareID:    id,
			UserID:     userID,
			VaultID:    vaultID,
			TargetPath: targetPath,
			IsFolder:   isFolder,
			AllowCopy:  allowCopy,
		}
		err = h.DB.Create(&rec).Error
		if err == nil {
			shareID = id
			lastErr = nil
			break
		}
		lastErr = err
	}
	if lastErr != nil {
		return shareOut{}, lastErr
	}
	return shareOut{
		ShareID:    shareID,
		VaultID:    vaultID,
		TargetPath: targetPath,
		IsFolder:   isFolder,
		AllowCopy:  allowCopy,
		URL:        "/p/" + shareID,
	}, nil
}

func (h *Handler) List(c *gin.Context) {
	u, ok := auth.RequireUser(c)
	if !ok {
		return
	}
	var rows []models.Share
	query := h.DB.Where("1 = 0")
	if vaultID := strings.TrimSpace(c.Query("vault_id")); vaultID != "" {
		vault, _, err := h.resolveVault(u.ID, vaultID)
		if err != nil {
			c.JSON(http.StatusNotFound, gin.H{"error": "vault not found"})
			return
		}
		query = h.DB.Where("vault_id = ?", vault.ID)
	} else {
		var owned []models.Vault
		if err := h.DB.Where("owner_id = ?", u.ID).Find(&owned).Error; err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		ids := make([]string, 0, len(owned))
		for _, vault := range owned {
			ids = append(ids, vault.ID)
		}
		var members []models.VaultMember
		if err := h.DB.Where("user_id = ?", u.ID).Find(&members).Error; err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		for _, member := range members {
			ids = append(ids, member.VaultID)
		}
		if len(ids) > 0 {
			query = h.DB.Where("vault_id IN ?", ids)
		}
	}
	query.Order("created_at desc").Find(&rows)
	out := make([]shareOut, 0, len(rows))
	for _, r := range rows {
		out = append(out, toOut(r))
	}
	c.JSON(http.StatusOK, gin.H{"shares": out})
}

func (h *Handler) Get(c *gin.Context) {
	u, ok := auth.RequireUser(c)
	if !ok {
		return
	}
	id := c.Param("id")
	var s models.Share
	if err := h.DB.Where("share_id = ?", id).First(&s).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "share not found"})
		return
	}
	_, role, err := h.resolveVault(u.ID, s.VaultID)
	if err != nil || !vaultaccess.CanManage(role) {
		c.JSON(http.StatusNotFound, gin.H{"error": "share not found"})
		return
	}
	c.JSON(http.StatusOK, toOut(s))
}

func (h *Handler) Update(c *gin.Context) {
	u, ok := auth.RequireUser(c)
	if !ok {
		return
	}
	var req updateRequest
	if err := c.ShouldBindJSON(&req); err != nil || req.AllowCopy == nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "allow_copy is required", "code": "invalid_share_update"})
		return
	}
	var share models.Share
	if err := h.DB.Where("share_id = ?", c.Param("id")).First(&share).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "share not found", "code": "share_not_found"})
		return
	}
	_, role, err := h.resolveVault(u.ID, share.VaultID)
	if err != nil || !vaultaccess.CanManage(role) {
		c.JSON(http.StatusNotFound, gin.H{"error": "share not found", "code": "share_not_found"})
		return
	}
	share.AllowCopy = *req.AllowCopy
	if err := h.DB.Model(&share).Update("allow_copy", share.AllowCopy).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "update share failed", "code": "share_update_failed"})
		return
	}
	c.JSON(http.StatusOK, toOut(share))
}

func (h *Handler) Delete(c *gin.Context) {
	u, ok := auth.RequireUser(c)
	if !ok {
		return
	}
	id := c.Param("id")
	var share models.Share
	if err := h.DB.Where("share_id = ?", id).First(&share).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "share not found"})
		return
	}
	_, role, err := h.resolveVault(u.ID, share.VaultID)
	if err != nil || !vaultaccess.CanManage(role) {
		c.JSON(http.StatusNotFound, gin.H{"error": "share not found"})
		return
	}
	res := h.DB.Where("share_id = ?", id).Delete(&models.Share{})
	if res.Error != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": res.Error.Error()})
		return
	}
	if res.RowsAffected == 0 {
		c.JSON(http.StatusNotFound, gin.H{"error": "share not found"})
		return
	}
	c.Status(http.StatusNoContent)
}

func toOut(s models.Share) shareOut {
	created := ""
	if !s.CreatedAt.IsZero() {
		created = s.CreatedAt.Format("2006-01-02T15:04:05Z07:00")
	}
	return shareOut{
		ShareID:    s.ShareID,
		VaultID:    s.VaultID,
		TargetPath: s.TargetPath,
		IsFolder:   s.IsFolder,
		AllowCopy:  s.AllowCopy,
		Views:      s.Views,
		URL:        "/p/" + s.ShareID,
		CreatedAt:  created,
	}
}

func (h *Handler) resolveVault(userID uint, requested string) (models.Vault, string, error) {
	var vault models.Vault
	if requested != "" {
		return vaultaccess.Resolve(h.DB, userID, requested)
	}
	if err := h.DB.Where("owner_id = ?", userID).Order("is_default desc, created_at asc").First(&vault).Error; err == nil {
		return vault, vaultaccess.RoleOwner, nil
	} else if !errors.Is(err, gorm.ErrRecordNotFound) {
		return models.Vault{}, "", err
	}
	var member models.VaultMember
	if err := h.DB.Where("user_id = ?", userID).Order("created_at asc").First(&member).Error; err != nil {
		return models.Vault{}, "", err
	}
	return vaultaccess.Resolve(h.DB, userID, member.VaultID)
}

func genShareID() (string, error) {
	b := make([]byte, shareIDLen)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	out := make([]byte, shareIDLen)
	for i, x := range b {
		out[i] = base62Alphabet[int(x)%len(base62Alphabet)]
	}
	return string(out), nil
}

