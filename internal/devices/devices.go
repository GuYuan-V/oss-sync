// 设备管理
package devices

import (
	"database/sql"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	"github.com/oss/oss-server/internal/auth"
	"github.com/oss/oss-server/internal/config"
	"github.com/oss/oss-server/internal/deviceauth"
	"github.com/oss/oss-server/internal/models"
	"github.com/oss/oss-server/internal/vaultaccess"
)

var ErrRevoked = deviceauth.ErrRevoked

const (
	ClientIDHeader   = deviceauth.ClientIDHeader
	DeviceNameHeader = deviceauth.DeviceNameHeader
)

type Handler struct {
	DB  *gorm.DB
	Cfg *config.Config
	now func() time.Time
}

func New(db *gorm.DB, cfg *config.Config) *Handler {
	return &Handler{DB: db, Cfg: cfg, now: time.Now}
}

func (h *Handler) Register(r *gin.Engine) {
	g := r.Group("/api/devices", auth.Middleware(h.DB, h.Cfg))
	{
		g.GET("", h.List)
		g.PATCH("/:client_id", h.Rename)
		g.PUT("/:client_id/authorization", h.Authorize)
		g.DELETE("/:client_id", h.Revoke)
	}
}

type VaultCursorOut struct {
	VaultID        string `json:"vault_id"`
	VaultName      string `json:"vault_name"`
	LastCursor     int64  `json:"last_cursor"`
	HeadRevision   int64  `json:"head_revision"`
	PendingChanges int64  `json:"pending_changes"`
	LastSyncAt     string `json:"last_sync_at,omitempty"`
}

// AccessOut 表示设备被授权访问的仓库。
type AccessOut struct {
	VaultID   string `json:"vault_id"`
	VaultName string `json:"vault_name"`
	GrantedAt string `json:"granted_at,omitempty"`
}

type DeviceOut struct {
	ClientID   string           `json:"client_id"`
	Name       string           `json:"name"`
	Status     string           `json:"status"`
	LastSeenAt string           `json:"last_seen_at"`
	CreatedAt  string           `json:"created_at"`
	RevokedAt  string           `json:"revoked_at,omitempty"`
	Stale      bool             `json:"stale"`
	IsCurrent  bool             `json:"is_current"`
	Vaults     []VaultCursorOut `json:"vaults"`
	Accesses   []AccessOut      `json:"accesses"`
}

func (h *Handler) List(c *gin.Context) {
	user, ok := auth.RequireUser(c)
	if !ok {
		return
	}
	did, hasDID := auth.CurrentDeviceID(c)
	if hasDID {
		if _, ok := auth.RequireDeviceID(c, c.GetHeader(deviceauth.ClientIDHeader)); !ok {
			return
		}
		if err := deviceauth.Touch(h.DB, user.ID, "", string(did), deviceauth.DecodeDeviceName(c.GetHeader(deviceauth.DeviceNameHeader)), nil, h.now()); err != nil &&
			!errors.Is(err, deviceauth.ErrRevoked) {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
	}
	currentID := ""
	if hasDID {
		currentID = string(did)
	}

	query := h.DB
	if c.Query("scope") != "all" || user.Role != "admin" {
		query = query.Where("user_id = ?", user.ID)
	} else if userID := parseUintQuery(c.Query("user_id")); userID > 0 {
		query = query.Where("user_id = ?", userID)
	}
	if c.Query("include_revoked") != "true" {
		query = query.Where("revoked_at IS NULL")
	}
	var rows []models.ClientDevice
	if err := query.Order("last_seen_at desc, created_at desc").Find(&rows).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	staleBefore := h.now().AddDate(0, 0, -h.Cfg.Sync.EffectiveDeviceStaleDays())
	out := make([]DeviceOut, 0, len(rows))
	for _, row := range rows {
		device := DeviceOut{
			ClientID:   row.ClientID,
			Name:       row.Name,
			Status:     deviceauth.EffectiveStatus(row.RevokedAt, row.Status),
			LastSeenAt: formatTime(row.LastSeenAt),
			CreatedAt:  formatTime(row.CreatedAt),
			Stale:      row.RevokedAt.Valid || row.LastSeenAt.Before(staleBefore),
			IsCurrent:  row.ClientID == currentID,
			Vaults:     []VaultCursorOut{},
			Accesses:   []AccessOut{},
		}
		if row.RevokedAt.Valid {
			device.RevokedAt = formatTime(row.RevokedAt.Time)
		}
		var bindings []models.DeviceVault
		if err := h.DB.Where("user_id = ? AND client_id = ?", row.UserID, row.ClientID).
			Order("vault_id asc").Find(&bindings).Error; err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		for _, binding := range bindings {
			var vault models.Vault
			if err := h.DB.Unscoped().Where("id = ? AND owner_id = ?", binding.VaultID, row.UserID).
				First(&vault).Error; err != nil {
				continue
			}
			var state models.VaultSyncState
			_ = h.DB.Where("vault_id = ?", binding.VaultID).First(&state).Error
			pending := state.HeadRevision - binding.LastCursor
			if pending < 0 {
				pending = 0
			}
			device.Vaults = append(device.Vaults, VaultCursorOut{
				VaultID:        binding.VaultID,
				VaultName:      vault.Name,
				LastCursor:     binding.LastCursor,
				HeadRevision:   state.HeadRevision,
				PendingChanges: pending,
				LastSyncAt:     formatTime(binding.LastSyncAt),
			})
		}
		var accesses []models.DeviceVaultAccess
		if err := h.DB.Where("user_id = ? AND client_id = ?", row.UserID, row.ClientID).
			Order("vault_id asc").Find(&accesses).Error; err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		for _, access := range accesses {
			var vault models.Vault
			_ = h.DB.Unscoped().Where("id = ?", access.VaultID).First(&vault).Error
			device.Accesses = append(device.Accesses, AccessOut{
				VaultID:   access.VaultID,
				VaultName: vault.Name,
				GrantedAt: formatTime(access.GrantedAt),
			})
		}
		out = append(out, device)
	}
	c.JSON(http.StatusOK, gin.H{
		"devices":          out,
		"stale_after_days": h.Cfg.Sync.EffectiveDeviceStaleDays(),
	})
}

type renameRequest struct {
	Name   string `json:"name"`
	UserID *uint  `json:"user_id"`
}

func (h *Handler) Rename(c *gin.Context) {
	user, ok := auth.RequireUser(c)
	if !ok {
		return
	}
	clientID := deviceauth.NormalizeClientID(c.Param("client_id"))
	var req renameRequest
	if clientID == "" || c.ShouldBindJSON(&req) != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid device rename request"})
		return
	}
	req.Name = strings.TrimSpace(req.Name)
	if req.Name == "" || len([]rune(req.Name)) > 128 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "name must be 1-128 characters"})
		return
	}
	targetUserID := user.ID
	if req.UserID != nil && *req.UserID != user.ID {
		if user.Role != "admin" {
			c.JSON(http.StatusForbidden, gin.H{"error": "insufficient permission", "code": "not_admin"})
			return
		}
		targetUserID = *req.UserID
	}
	var device models.ClientDevice
	if err := h.DB.Where("user_id = ? AND client_id = ?", targetUserID, clientID).First(&device).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"error": "device not found"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	if device.Status != deviceauth.DeviceStatusPending {
		c.JSON(http.StatusConflict, gin.H{"error": "approved device name is locked"})
		return
	}
	if err := h.DB.Model(&device).Update("name", req.Name).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"client_id": clientID, "name": req.Name})
}

// authorizeRequest 批准设备并设置仓库授权，或吊销设备。
type authorizeRequest struct {
	UserID   *uint    `json:"user_id"`
	Name     string   `json:"name"`
	Status   string   `json:"status"`
	VaultIDs []string `json:"vault_ids"`
}

// Authorize 处理 PUT /api/devices/:client_id/authorization。
// 用户批准自己的设备并选择授权仓库；管理员可跨用户管理。
func (h *Handler) Authorize(c *gin.Context) {
	actor, ok := auth.RequireUser(c)
	if !ok {
		return
	}
	clientID := deviceauth.NormalizeClientID(c.Param("client_id"))
	if clientID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid client id"})
		return
	}
	var req authorizeRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid authorization request"})
		return
	}
	req.Status = strings.ToLower(strings.TrimSpace(req.Status))
	if req.Status != deviceauth.DeviceStatusApproved && req.Status != deviceauth.DeviceStatusRevoked {
		c.JSON(http.StatusBadRequest, gin.H{"error": "status must be approved or revoked"})
		return
	}

	targetUserID := actor.ID
	if req.UserID != nil && *req.UserID != actor.ID {
		if actor.Role != "admin" {
			c.JSON(http.StatusForbidden, gin.H{"error": "insufficient permission", "code": "not_admin"})
			return
		}
		targetUserID = *req.UserID
	}
	var target models.User
	if err := h.DB.First(&target, targetUserID).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "user not found"})
		return
	}

	// 校验授权仓库均属于目标用户可访问的仓库。
	vaultIDs := make([]string, 0, len(req.VaultIDs))
	if req.Status == deviceauth.DeviceStatusApproved {
		for _, raw := range req.VaultIDs {
			vaultID := strings.TrimSpace(raw)
			if vaultID == "" {
				continue
			}
			if _, _, err := vaultaccess.Resolve(h.DB, targetUserID, vaultID); err != nil {
				c.JSON(http.StatusBadRequest, gin.H{
					"error": "device authorization contains an inaccessible vault",
					"code":  "invalid_vault_authorization",
				})
				return
			}
			vaultIDs = append(vaultIDs, vaultID)
		}
	}

	now := h.now()
	err := h.DB.Transaction(func(tx *gorm.DB) error {
		var device models.ClientDevice
		err := tx.Where("user_id = ? AND client_id = ?", targetUserID, clientID).
			First(&device).Error
		create := errors.Is(err, gorm.ErrRecordNotFound)
		if err != nil && !create {
			return err
		}
		if create {
			device = models.ClientDevice{
				UserID:     targetUserID,
				ClientID:   clientID,
				Name:       truncateRunes(strings.TrimSpace(req.Name), 128),
				Status:     req.Status,
				LastSeenAt: now,
			}
			if req.Status == deviceauth.DeviceStatusApproved {
				device.ApprovedAt = now
				device.ApprovedByUserID = &actor.ID
			}
			return tx.Create(&device).Error
		}

		updates := map[string]any{
			"status":       req.Status,
			"last_seen_at": now,
		}
		if name := strings.TrimSpace(req.Name); device.Status == deviceauth.DeviceStatusPending && name != "" {
			updates["name"] = truncateRunes(name, 128)
		}
		if req.Status == deviceauth.DeviceStatusApproved {
			updates["approved_at"] = now
			updates["approved_by_user_id"] = actor.ID
			updates["revoked_at"] = nil
		} else {
			updates["revoked_at"] = now
		}
		return tx.Model(&models.ClientDevice{}).Where("id = ?", device.ID).Updates(updates).Error
	})
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	if req.Status == deviceauth.DeviceStatusApproved {
		if err := deviceauth.ReplaceVaultAccesses(h.DB, targetUserID, clientID, vaultIDs, actor.ID); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
	} else {
		if err := deviceauth.RevokeAllDeviceAccesses(h.DB, targetUserID, clientID); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
	}

	status, name, err := deviceauth.GetDevice(h.DB, targetUserID, clientID)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "device not found"})
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"client_id": clientID,
		"user_id":   targetUserID,
		"name":      name,
		"status":    status,
		"vault_ids": vaultIDs,
	})
}

func (h *Handler) Revoke(c *gin.Context) {
	user, ok := auth.RequireUser(c)
	if !ok {
		return
	}
	clientID := deviceauth.NormalizeClientID(c.Param("client_id"))
	if clientID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid client id"})
		return
	}
	if did, hasDID := auth.CurrentDeviceID(c); hasDID {
		if _, ok := auth.RequireDeviceID(c, c.GetHeader(deviceauth.ClientIDHeader)); !ok {
			return
		}
		if clientID == string(did) {
			c.JSON(http.StatusConflict, gin.H{"error": "current device cannot revoke itself"})
			return
		}
	}
	now := h.now()
	result := h.DB.Model(&models.ClientDevice{}).
		Where("user_id = ? AND client_id = ? AND revoked_at IS NULL", user.ID, clientID).
		Update("status", deviceauth.DeviceStatusRevoked).Update("revoked_at", now)
	if result.Error != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": result.Error.Error()})
		return
	}
	if result.RowsAffected == 0 {
		c.JSON(http.StatusNotFound, gin.H{"error": "device not found"})
		return
	}
	_ = deviceauth.RevokeAllDeviceAccesses(h.DB, user.ID, clientID)
	c.Status(http.StatusNoContent)
}

// MinActiveCursor 返回某个 Vault 活跃设备的最小同步游标和活跃设备数。
func MinActiveCursor(
	db *gorm.DB,
	vaultID string,
	staleBefore time.Time,
) (int64, int64, error) {
	type cursorAggregate struct {
		Count     int64
		MinCursor sql.NullInt64
	}
	var aggregate cursorAggregate
	err := db.Table("device_vaults AS dv").
		Select("COUNT(*) AS count, MIN(dv.last_cursor) AS min_cursor").
		Joins(
			"JOIN client_devices AS cd ON cd.user_id = dv.user_id AND cd.client_id = dv.client_id",
		).
		Where(
			"dv.vault_id = ? AND cd.revoked_at IS NULL AND cd.last_seen_at >= ?",
			vaultID,
			staleBefore,
		).
		Scan(&aggregate).Error
	if err != nil {
		return 0, 0, err
	}
	if !aggregate.MinCursor.Valid {
		return 0, aggregate.Count, nil
	}
	return aggregate.MinCursor.Int64, aggregate.Count, nil
}

func truncateRunes(value string, limit int) string {
	runes := []rune(value)
	if len(runes) <= limit {
		return value
	}
	return string(runes[:limit])
}

func parseUintQuery(value string) uint {
	n, _ := strconv.ParseUint(value, 10, 64)
	return uint(n)
}

func formatTime(value time.Time) string {
	if value.IsZero() {
		return ""
	}
	return value.UTC().Format(time.RFC3339)
}

