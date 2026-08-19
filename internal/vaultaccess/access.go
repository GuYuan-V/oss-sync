// Package vaultaccess centralizes Vault authorization so every API uses the
// same owner / manager / participant rules.
package vaultaccess

import (
	"errors"

	"gorm.io/gorm"

	"github.com/oss/oss-server/internal/models"
)

const (
	RoleOwner       = "owner"
	RoleManager     = "manager"
	RoleParticipant = "participant"
	// RoleAdmin 表示管理员通过平台权限访问任意仓库。
	RoleAdmin = "admin"
)

func ValidMemberRole(role string) bool {
	return role == RoleManager || role == RoleParticipant
}

// CanManage 判断角色是否可管理仓库（成员、分享、博客设置、恢复、永久删除）。
func CanManage(role string) bool {
	return role == RoleOwner || role == RoleManager || role == RoleAdmin
}

func CanDelete(role string) bool {
	return role == RoleOwner || role == RoleAdmin
}

// Resolve returns an active Vault and the caller's role. A missing membership
// is intentionally reported as not found to avoid exposing Vault IDs.
func Resolve(db *gorm.DB, userID uint, vaultID string) (models.Vault, string, error) {
	var vault models.Vault
	if err := db.Where("id = ?", vaultID).First(&vault).Error; err != nil {
		return models.Vault{}, "", err
	}
	if vault.OwnerID == userID {
		return vault, RoleOwner, nil
	}
	var member models.VaultMember
	if err := db.Where("vault_id = ? AND user_id = ?", vaultID, userID).First(&member).Error; err != nil {
		return models.Vault{}, "", err
	}
	if !ValidMemberRole(member.Role) {
		return models.Vault{}, "", gorm.ErrRecordNotFound
	}
	return vault, member.Role, nil
}

func IsNotFound(err error) bool {
	return errors.Is(err, gorm.ErrRecordNotFound)
}
