package auth

import (
	"fmt"
	"os"

	"gorm.io/gorm"

	"github.com/oss/oss-server/internal/config"
	"github.com/oss/oss-server/internal/models"
)

// EnsureBootstrapAdmin 通过 OSS_ADMIN_PASSWORD 预置管理员。
//
// 新数据库不再在终端交互询问管理员密码：若设置了 OSS_ADMIN_PASSWORD
// 则直接创建预置管理员；否则不做任何事，第一个网页注册账户自动成为管理员。
func EnsureBootstrapAdmin(db *gorm.DB, cfg *config.Config) (bool, error) {
	var count int64
	if err := db.Model(&models.User{}).Where("role = ?", "admin").Count(&count).Error; err != nil {
		return false, fmt.Errorf("检查管理员账户失败: %w", err)
	}
	if count > 0 {
		return false, nil
	}

	password := os.Getenv("OSS_ADMIN_PASSWORD")
	if password == "" {
		// 不设置预置密码时，交给第一个网页注册用户成为管理员。
		return false, nil
	}
	username := cfg.Auth.EffectiveBootstrapAdminUsername()
	if err := ValidateAccountInput(username, password); err != nil {
		return false, fmt.Errorf("管理员初始化失败: %w", err)
	}
	if _, err := CreateAccount(db, username, password, "admin"); err != nil {
		return false, fmt.Errorf("创建初始管理员失败: %w", err)
	}
	return true, nil
}
