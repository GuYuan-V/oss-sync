// 启动初始化
package auth

import (
	"fmt"

	"gorm.io/gorm"

	"github.com/oss/oss-server/internal/config"
	"github.com/oss/oss-server/internal/models"
)

// EnsureBootstrapAdmin 保留兼容签名，但不再通过环境变量预置管理员。
// 管理员仅由第一个网页注册账户自动成为 admin（ResolveRegistrationRole）。
func EnsureBootstrapAdmin(db *gorm.DB, _ *config.Config) (bool, error) {
	var count int64
	if err := db.Model(&models.User{}).Where("role = ?", "admin").Count(&count).Error; err != nil {
		return false, fmt.Errorf("检查管理员账户失败: %w", err)
	}
	if count > 0 {
		return false, nil
	}
	// 已移除 OSS_ADMIN_PASSWORD 预置路径，始终交给首个注册用户。
	return false, nil
}
