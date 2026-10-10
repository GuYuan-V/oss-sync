package webui

import (
	"errors"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	"github.com/helantianshen/oss-sync/internal/models"
	"github.com/helantianshen/oss-sync/internal/vaultaccess"
)

// memberRow 是仓库成员列表一行的显示数据
type memberRow struct {
	UserID   uint
	Username string
	Role     string
}

// addMember 添加或更新仓库成员角色：POST /dashboard/vaults/:vault_id/members
// 仅 owner 或 manager 可操作；角色只接受 manager 与 participant
// 目标用户不存在、目标为所有者或写库失败时通过 query 参数回传错误
func (h *Handler) addMember(c *gin.Context) {
	vault, role, ok := h.resolveVaultPage(c)
	if !ok {
		return
	}
	if !vaultaccess.CanManage(role) {
		c.Redirect(http.StatusSeeOther, "/dashboard/vaults/"+vault.ID+"/members?error="+url.QueryEscape(h.t(c, "err.no_permission")))
		return
	}
	username := strings.TrimSpace(c.PostForm("username"))
	newRole := c.PostForm("role")
	if username == "" || (newRole != "manager" && newRole != "participant") {
		c.Redirect(http.StatusSeeOther, "/dashboard/vaults/"+vault.ID+"/members?error="+url.QueryEscape(h.t(c, "err.invalid_member_username_or_role")))
		return
	}
	var user models.User
	if err := h.DB.Where("username = ?", username).First(&user).Error; err != nil {
		c.Redirect(http.StatusSeeOther, "/dashboard/vaults/"+vault.ID+"/members?error="+url.QueryEscape(h.t(c, "err.invite_user_not_found")))
		return
	}
	if user.ID == vault.OwnerID {
		c.Redirect(http.StatusSeeOther, "/dashboard/vaults/"+vault.ID+"/members?error="+url.QueryEscape(h.t(c, "err.owner_already_full_access")))
		return
	}
	var member models.VaultMember
	err := h.DB.Where("vault_id = ? AND user_id = ?", vault.ID, user.ID).First(&member).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		err = h.DB.Create(&models.VaultMember{VaultID: vault.ID, UserID: user.ID, Role: newRole}).Error
	} else if err == nil {
		err = h.DB.Model(&member).Update("role", newRole).Error
	}
	if err != nil {
		c.Redirect(http.StatusSeeOther, "/dashboard/vaults/"+vault.ID+"/members?error="+url.QueryEscape(h.t(c, "err.save_member_failed")))
		return
	}
	c.Redirect(http.StatusSeeOther, "/dashboard/vaults/"+vault.ID+"/members?saved=1")
}

// updateMemberRole 修改仓库成员角色：POST /dashboard/vaults/:vault_id/members/:user_id/role
// 仅 owner 或 manager 可操作，不能修改所有者角色；成员不存在时同样视为失败
func (h *Handler) updateMemberRole(c *gin.Context) {
	vault, role, ok := h.resolveVaultPage(c)
	if !ok {
		return
	}
	if !vaultaccess.CanManage(role) {
		c.Redirect(http.StatusSeeOther, "/dashboard/vaults/"+vault.ID+"/members?error="+url.QueryEscape(h.t(c, "err.no_permission")))
		return
	}
	userID, err := strconv.ParseUint(c.Param("user_id"), 10, 64)
	newRole := c.PostForm("role")
	if err != nil || (newRole != "manager" && newRole != "participant") {
		c.Redirect(http.StatusSeeOther, "/dashboard/vaults/"+vault.ID+"/members?error="+url.QueryEscape(h.t(c, "err.invalid_member_role")))
		return
	}
	if uint(userID) == vault.OwnerID {
		c.Redirect(http.StatusSeeOther, "/dashboard/vaults/"+vault.ID+"/members?error="+url.QueryEscape(h.t(c, "err.cannot_change_owner_role")))
		return
	}
	result := h.DB.Model(&models.VaultMember{}).
		Where("vault_id = ? AND user_id = ?", vault.ID, uint(userID)).Update("role", newRole)
	if result.Error != nil || result.RowsAffected == 0 {
		c.Redirect(http.StatusSeeOther, "/dashboard/vaults/"+vault.ID+"/members?error="+url.QueryEscape(h.t(c, "err.update_member_failed")))
		return
	}
	c.Redirect(http.StatusSeeOther, "/dashboard/vaults/"+vault.ID+"/members?saved=1")
}

// removeMember 移除仓库成员：POST /dashboard/vaults/:vault_id/members/:user_id/delete
// 在事务内先删除该成员的设备-仓库绑定，再删除成员关系；不能移除所有者
// 任一删除失败或成员不存在时整事务回滚并通过 query 参数回传错误
func (h *Handler) removeMember(c *gin.Context) {
	vault, role, ok := h.resolveVaultPage(c)
	if !ok {
		return
	}
	if !vaultaccess.CanManage(role) {
		c.Redirect(http.StatusSeeOther, "/dashboard/vaults/"+vault.ID+"/members?error="+url.QueryEscape(h.t(c, "err.no_permission")))
		return
	}
	userID, err := strconv.ParseUint(c.Param("user_id"), 10, 64)
	if err != nil {
		c.Redirect(http.StatusSeeOther, "/dashboard/vaults/"+vault.ID+"/members?error="+url.QueryEscape(h.t(c, "err.invalid_member")))
		return
	}
	if uint(userID) == vault.OwnerID {
		c.Redirect(http.StatusSeeOther, "/dashboard/vaults/"+vault.ID+"/members?error="+url.QueryEscape(h.t(c, "err.cannot_remove_owner")))
		return
	}
	if err := h.DB.Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("user_id = ? AND vault_id = ?", uint(userID), vault.ID).
			Delete(&models.DeviceVault{}).Error; err != nil {
			return err
		}
		result := tx.Where("vault_id = ? AND user_id = ?", vault.ID, uint(userID)).Delete(&models.VaultMember{})
		if result.Error != nil || result.RowsAffected == 0 {
			return gorm.ErrRecordNotFound
		}
		return nil
	}); err != nil {
		c.Redirect(http.StatusSeeOther, "/dashboard/vaults/"+vault.ID+"/members?error="+url.QueryEscape(h.t(c, "err.remove_member_failed")))
		return
	}
	c.Redirect(http.StatusSeeOther, "/dashboard/vaults/"+vault.ID+"/members?saved=1")
}
