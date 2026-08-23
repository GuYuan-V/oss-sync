// 密钥管理
package auth

import (
	"crypto/rand"
	"encoding/base64"
	"fmt"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/oss/oss-server/internal/config"
	"github.com/oss/oss-server/internal/models"
)

// EnsureDatabaseJWTSecret loads the stable server signing key from the DB. On
// an empty database it generates 48 random bytes and persists them atomically.
// Config-file and environment JWT values are intentionally ignored here.
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

