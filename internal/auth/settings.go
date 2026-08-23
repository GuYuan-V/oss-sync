// 认证设置
package auth

import (
	"errors"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/oss/oss-server/internal/models"
)

const systemSettingsID uint = 1

// EnsureRegistrationSetting 创建新数据库的注册开关；已有记录不会被部署配置覆盖。
func EnsureRegistrationSetting(db *gorm.DB, defaultEnabled bool) error {
	var setting models.SystemSetting
	err := db.First(&setting, systemSettingsID).Error
	if err == nil {
		return nil
	}
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		return err
	}
	return db.Clauses(clause.OnConflict{DoNothing: true}).Create(&models.SystemSetting{
		ID:                  systemSettingsID,
		RegistrationEnabled: defaultEnabled,
	}).Error
}

func RegistrationEnabled(db *gorm.DB, defaultEnabled bool) (bool, error) {
	if err := EnsureRegistrationSetting(db, defaultEnabled); err != nil {
		return false, err
	}
	var setting models.SystemSetting
	if err := db.First(&setting, systemSettingsID).Error; err != nil {
		return false, err
	}
	return setting.RegistrationEnabled, nil
}

func SetRegistrationEnabled(db *gorm.DB, enabled bool) error {
	if err := EnsureRegistrationSetting(db, true); err != nil {
		return err
	}
	result := db.Model(&models.SystemSetting{}).
		Where("id = ?", systemSettingsID).
		Update("registration_enabled", enabled)
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return errors.New("系统设置不存在")
	}
	return nil
}

