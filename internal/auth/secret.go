// 基于数据库的签名密钥管理。
package auth

import (
	"crypto/rand"
	"encoding/base64"
	"fmt"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/helantianshen/oss-sync/internal/config"
	"github.com/helantianshen/oss-sync/internal/models"
)

// EnsureDatabaseJWTSecret 从数据库加载稳定的服务端签名密钥。空库时生成 48 字节随机值并原子持久化，
// 此处不采用配置文件与环境变量中的 JWT 取值。
func EnsureDatabaseJWTSecret(db *gorm.DB, cfg *config.Config) error {
	if err := EnsureRegistrationSetting(db, cfg.Auth.AllowAnonymousRegistration); err != nil {
		return err
	}
	return db.Transaction(func(tx *gorm.DB) error {
		var setting models.SystemSetting
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&setting, systemSettingsID).Error; err != nil {
			return err
		}
		if setting.JWTSecret == "" {
			secret, err := randomJWTSecret()
			if err != nil {
				return err
			}
			if err := tx.Model(&models.SystemSetting{}).Where("id = ?", systemSettingsID).
				Update("jwt_secret", secret).Error; err != nil {
				return err
			}
			setting.JWTSecret = secret
		}
		cfg.Auth.JWTSecret = setting.JWTSecret
		return nil
	})
}

func randomJWTSecret() (string, error) {
	b := make([]byte, 48)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("generate JWT secret: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}
