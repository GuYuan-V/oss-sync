// 仓库服务
package vaults

import (
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"gorm.io/gorm"

	"github.com/oss/oss-server/internal/auth"
	"github.com/oss/oss-server/internal/config"
	"github.com/oss/oss-server/internal/deviceauth"
	"github.com/oss/oss-server/internal/models"
	"github.com/oss/oss-server/internal/settingspolicy"
	"github.com/oss/oss-server/internal/vaultaccess"
	"github.com/oss/oss-server/internal/vaultbackup"
)

type Handler struct {
	DB  *gorm.DB
	Cfg *config.Config
}

func New(db *gorm.DB, cfg *config.Config) *Handler { return &Handler{DB: db, Cfg: cfg} }

func (h *Handler) Register(r *gin.Engine) {
	g := r.Group("/api/vaults", auth.Middleware(h.DB, h.Cfg))
	{
		g.GET("", h.List)
		g.POST("", h.Create)
		g.GET("/:vault_id", h.Get)
		g.PATCH("/:vault_id", h.Update)
		g.DELETE("/:vault_id", h.Delete)
		g.GET("/:vault_id/members", h.ListMembers)
		g.POST("/:vault_id/members", h.AddMember)
		g.PATCH("/:vault_id/members/:user_id", h.UpdateMember)
		g.DELETE("/:vault_id/members/:user_id", h.RemoveMember)
	}
}

type createRequest struct {
	Name        string `json:"name" binding:"required,min=1,max=128"`
	Description string `json:"description" binding:"max=512"`
}

type updateRequest struct {
	Name        *string `json:"name"`
	Description *string `json:"description"`
}

type vaultOut struct {
	ID           string `json:"id"`
	Name         string `json:"name"`
	Description  string `json:"description"`
	IsDefault    bool   `json:"is_default"`
	AccessRole   string `json:"access_role"`
	StorageQuota int64  `json:"storage_quota"`
	StorageUsed  int64  `json:"storage_used"`
	HeadRevision int64  `json:"head_revision"`
	CreatedAt    string `json:"created_at"`
	UpdatedAt    string `json:"updated_at"`
}

type memberOut struct {
	UserID   uint   `json:"user_id"`
	Username string `json:"username"`
	Role     string `json:"role"`
}

type memberRequest struct {
	Username string `json:"username" binding:"required,min=3,max=64"`
	Role     string `json:"role" binding:"required"`
}

type memberRoleRequest struct {
	Role string `json:"role" binding:"required"`
}

func (h *Handler) List(c *gin.Context) {
	u, ok := auth.RequireUser(c)
	if !ok {
		return
	}
	did, ok := auth.RequireDeviceID(c, c.GetHeader(deviceauth.ClientIDHeader), c.Query("client_id"))
	if !ok {
		return
	}
	if err := deviceauth.CheckApproved(h.DB, u.ID, string(did)); err != nil {
		h.writeDeviceError(c, err)
		return
	}
	var owned []models.Vault
	if err := h.DB.Where("owner_id = ?", u.ID).Order("is_default desc, created_at asc").Find(&owned).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	var memberships []models.VaultMember
	if err := h.DB.Where("user_id = ?", u.ID).Find(&memberships).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	var accesses []models.DeviceVaultAccess
	if err := h.DB.Where("user_id = ? AND client_id = ?", u.ID, string(did)).
		Find(&accesses).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	authorized := make(map[string]struct{}, len(accesses))
	for _, access := range accesses {
		authorized[access.VaultID] = struct{}{}
	}
	out := make([]vaultOut, 0, len(owned)+len(memberships))
	for _, vault := range owned {
		if _, ok := authorized[vault.ID]; !ok {
			continue
		}
		out = append(out, h.toOut(vault, vaultaccess.RoleOwner))
	}
	for _, member := range memberships {
		var vault models.Vault
		if err := h.DB.Where("id = ?", member.VaultID).First(&vault).Error; err == nil {
			if _, ok := authorized[vault.ID]; !ok {
				continue
			}
			out = append(out, h.toOut(vault, member.Role))
		}
	}
	c.JSON(http.StatusOK, gin.H{"vaults": out})
}

func (h *Handler) Create(c *gin.Context) {
	u, ok := auth.RequireUser(c)
	if !ok {
		return
	}
	did, ok := auth.RequireDeviceID(c, c.GetHeader(deviceauth.ClientIDHeader), c.Query("client_id"))
	if !ok {
		return
	}
	if err := deviceauth.CheckApproved(h.DB, u.ID, string(did)); err != nil {
		h.writeDeviceError(c, err)
		return
	}
	var req createRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	req.Name = strings.TrimSpace(req.Name)
	if req.Name == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "name is required"})
		return
	}
	effective, err := settingspolicy.EffectiveForUser(
		h.DB,
		u.ID,
		int64(h.Cfg.Server.MaxFileSizeMB)<<20,
	)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	vault := models.Vault{
		ID: uuid.NewString(), OwnerID: u.ID, Name: req.Name,
		Description: strings.TrimSpace(req.Description), StorageQuota: effective.VaultStorageBytes,
	}
	if err := h.DB.Transaction(func(tx *gorm.DB) error {
		var activeVaultCount int64
		if err := tx.Model(&models.Vault{}).Where("owner_id = ?", u.ID).Count(&activeVaultCount).Error; err != nil {
			return err
		}
		vault.IsDefault = activeVaultCount == 0
		if err := tx.Create(&vault).Error; err != nil {
			return err
		}
		if err := tx.Create(&models.VaultSetting{VaultID: vault.ID}).Error; err != nil {
			return err
		}
		return tx.Create(&models.VaultSyncState{VaultID: vault.ID}).Error
	}); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	if err := deviceauth.GrantAccess(h.DB, u.ID, string(did), vault.ID, u.ID); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusCreated, h.toOut(vault, vaultaccess.RoleOwner))
}

func (h *Handler) Get(c *gin.Context) {
	u, ok := auth.RequireUser(c)
	if !ok {
		return
	}
	vault, role, ok := h.requireAccess(c, u.ID)
	if !ok {
		return
	}
	c.JSON(http.StatusOK, h.toOut(vault, role))
}

func (h *Handler) Update(c *gin.Context) {
	u, ok := auth.RequireUser(c)
	if !ok {
		return
	}
	vault, role, ok := h.requireAccess(c, u.ID)
	if !ok {
		return
	}
	if !vaultaccess.CanManage(role) {
		h.writeForbidden(c)
		return
	}
	var req updateRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	updates := map[string]any{}
	if req.Name != nil {
		name := strings.TrimSpace(*req.Name)
		if name == "" || len(name) > 128 {
			c.JSON(http.StatusBadRequest, gin.H{"error": "name must be 1-128 characters"})
			return
		}
		updates["name"] = name
		vault.Name = name
	}
	if req.Description != nil {
		description := strings.TrimSpace(*req.Description)
		if len(description) > 512 {
			c.JSON(http.StatusBadRequest, gin.H{"error": "description must be at most 512 characters"})
			return
		}
		updates["description"] = description
		vault.Description = description
	}
	if len(updates) > 0 {
		if err := h.DB.Model(&models.Vault{}).Where("id = ?", vault.ID).Updates(updates).Error; err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
	}
	c.JSON(http.StatusOK, h.toOut(vault, role))
}

// Delete first writes a ZIP archive below ./backups/vaults, then permanently
// removes the Vault, shares, membership data, revisions and stored content.
func (h *Handler) Delete(c *gin.Context) {
	u, ok := auth.RequireUser(c)
	if !ok {
		return
	}
	vault, role, ok := h.requireAccess(c, u.ID)
	if !ok {
		return
	}
	if !vaultaccess.CanDelete(role) {
		h.writeForbidden(c)
		return
	}
	if vault.IsDefault {
		c.JSON(http.StatusConflict, gin.H{"error": "default vault cannot be deleted"})
		return
	}
	backup, err := vaultbackup.Purge(h.DB, h.Cfg.Storage.DataDir, vault)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "backup or permanent deletion failed: " + err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"backup_id": backup.ID, "message": "vault permanently deleted after backup"})
}

func (h *Handler) ListMembers(c *gin.Context) {
	u, ok := auth.RequireUser(c)
	if !ok {
		return
	}
	vault, role, ok := h.requireAccess(c, u.ID)
	if !ok {
		return
	}
	if !vaultaccess.CanManage(role) {
		h.writeForbidden(c)
		return
	}
	rows, err := h.memberRows(vault)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"members": rows, "current_role": role})
}

func (h *Handler) AddMember(c *gin.Context) {
	u, ok := auth.RequireUser(c)
	if !ok {
		return
	}
	vault, actorRole, ok := h.requireAccess(c, u.ID)
	if !ok {
		return
	}
	if !vaultaccess.CanManage(actorRole) {
		h.writeForbidden(c)
		return
	}
	var req memberRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	req.Username, req.Role = strings.TrimSpace(req.Username), strings.ToLower(strings.TrimSpace(req.Role))
	if !vaultaccess.ValidMemberRole(req.Role) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "role must be manager or participant"})
		return
	}
	if actorRole == vaultaccess.RoleManager && req.Role != vaultaccess.RoleParticipant {
		h.writeForbidden(c)
		return
	}
	var target models.User
	if err := h.DB.Where("username = ?", req.Username).First(&target).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "user not found"})
		return
	}
	if target.ID == vault.OwnerID {
		c.JSON(http.StatusConflict, gin.H{"error": "vault owner already has access"})
		return
	}
	member := models.VaultMember{VaultID: vault.ID, UserID: target.ID, Role: req.Role}
	var existing models.VaultMember
	err := h.DB.Where("vault_id = ? AND user_id = ?", vault.ID, target.ID).First(&existing).Error
	if err == nil {
		if actorRole == vaultaccess.RoleManager && existing.Role != vaultaccess.RoleParticipant {
			h.writeForbidden(c)
			return
		}
		if err := h.DB.Model(&existing).Update("role", req.Role).Error; err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
	} else if errors.Is(err, gorm.ErrRecordNotFound) {
		if err := h.DB.Create(&member).Error; err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
	} else {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.Status(http.StatusNoContent)
}

func (h *Handler) UpdateMember(c *gin.Context) {
	u, ok := auth.RequireUser(c)
	if !ok {
		return
	}
	vault, actorRole, ok := h.requireAccess(c, u.ID)
	if !ok {
		return
	}
	if !vaultaccess.CanManage(actorRole) {
		h.writeForbidden(c)
		return
	}
	var req memberRoleRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	req.Role = strings.ToLower(strings.TrimSpace(req.Role))
	if !vaultaccess.ValidMemberRole(req.Role) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "role must be manager or participant"})
		return
	}
	var member models.VaultMember
	if err := h.DB.Where("vault_id = ? AND user_id = ?", vault.ID, c.Param("user_id")).First(&member).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "member not found"})
		return
	}
	if actorRole == vaultaccess.RoleManager && (member.Role != vaultaccess.RoleParticipant || req.Role != vaultaccess.RoleParticipant) {
		h.writeForbidden(c)
		return
	}
	if err := h.DB.Model(&member).Update("role", req.Role).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.Status(http.StatusNoContent)
}

func (h *Handler) RemoveMember(c *gin.Context) {
	u, ok := auth.RequireUser(c)
	if !ok {
		return
	}
	vault, actorRole, ok := h.requireAccess(c, u.ID)
	if !ok {
		return
	}
	if !vaultaccess.CanManage(actorRole) {
		h.writeForbidden(c)
		return
	}
	var member models.VaultMember
	if err := h.DB.Where("vault_id = ? AND user_id = ?", vault.ID, c.Param("user_id")).First(&member).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "member not found"})
		return
	}
	if actorRole == vaultaccess.RoleManager && member.Role != vaultaccess.RoleParticipant {
		h.writeForbidden(c)
		return
	}
	if err := h.DB.Where("user_id = ? AND vault_id = ?", member.UserID, vault.ID).Delete(&models.DeviceVault{}).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	if err := h.DB.Where("user_id = ? AND vault_id = ?", member.UserID, vault.ID).Delete(&models.DeviceVaultAccess{}).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	if err := h.DB.Delete(&member).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.Status(http.StatusNoContent)
}

func (h *Handler) requireAccess(c *gin.Context, userID uint) (models.Vault, string, bool) {
	vault, role, err := vaultaccess.Resolve(h.DB, userID, c.Param("vault_id"))
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "vault not found"})
		return models.Vault{}, "", false
	}
	return vault, role, true
}

func (h *Handler) memberRows(vault models.Vault) ([]memberOut, error) {
	var members []models.VaultMember
	if err := h.DB.Where("vault_id = ?", vault.ID).Order("created_at asc").Find(&members).Error; err != nil {
		return nil, err
	}
	ids := make([]uint, 0, len(members)+1)
	ids = append(ids, vault.OwnerID)
	for _, member := range members {
		ids = append(ids, member.UserID)
	}
	var users []models.User
	if err := h.DB.Where("id IN ?", ids).Find(&users).Error; err != nil {
		return nil, err
	}
	byID := map[uint]string{}
	for _, user := range users {
		byID[user.ID] = user.Username
	}
	rows := []memberOut{{UserID: vault.OwnerID, Username: byID[vault.OwnerID], Role: vaultaccess.RoleOwner}}
	for _, member := range members {
		rows = append(rows, memberOut{UserID: member.UserID, Username: byID[member.UserID], Role: member.Role})
	}
	return rows, nil
}

func (h *Handler) writeForbidden(c *gin.Context) {
	c.JSON(http.StatusForbidden, gin.H{"error": "insufficient vault permission"})
}

func (h *Handler) writeDeviceError(c *gin.Context, err error) {
	switch {
	case errors.Is(err, deviceauth.ErrRevoked):
		c.JSON(http.StatusForbidden, gin.H{"error": "this device has been revoked", "code": "device_revoked"})
	case errors.Is(err, deviceauth.ErrDevicePending):
		c.JSON(http.StatusForbidden, gin.H{"error": "this device is pending authorization", "code": "device_pending"})
	case errors.Is(err, deviceauth.ErrDeviceUnknown):
		c.JSON(http.StatusForbidden, gin.H{"error": "device is not registered", "code": "device_unknown"})
	default:
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
	}
}

func (h *Handler) toOut(vault models.Vault, accessRole string) vaultOut {
	var state models.VaultSyncState
	_ = h.DB.Where("vault_id = ?", vault.ID).First(&state).Error
	return vaultOut{ID: vault.ID, Name: vault.Name, Description: vault.Description, IsDefault: vault.IsDefault,
		AccessRole: accessRole, StorageQuota: vault.StorageQuota, StorageUsed: vault.StorageUsed,
		HeadRevision: state.HeadRevision, CreatedAt: vault.CreatedAt.Format(time.RFC3339), UpdatedAt: vault.UpdatedAt.Format(time.RFC3339)}
}

