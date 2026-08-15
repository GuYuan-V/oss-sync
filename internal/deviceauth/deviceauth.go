// Package deviceauth 提供设备登记、状态与仓库授权等纯逻辑，
// 不依赖 HTTP 和 auth，供 auth、devices、syncapi 等包共享。
package deviceauth

import (
	"database/sql"
	"errors"
	"net/url"
	"strings"
	"time"

	"gorm.io/gorm"

	"github.com/oss/oss-server/internal/models"
)

// 设备请求头名称。
const (
	ClientIDHeader   = "X-OSS-Client-ID"
	DeviceNameHeader = "X-OSS-Device-Name"
)

// 设备状态常量。
const (
	DeviceStatusPending  = "pending"
	DeviceStatusApproved = "approved"
	DeviceStatusRevoked  = "revoked"
)

var (
	// ErrRevoked 设备已吊销。
	ErrRevoked = errors.New("device has been revoked")
	// ErrDevicePending 设备尚未批准。
	ErrDevicePending = errors.New("device is pending authorization")
	// ErrDeviceUnknown 设备未登记。
	ErrDeviceUnknown = errors.New("device is not registered")
	// ErrVaultNotAuthorized 设备未被授权访问该仓库。
	ErrVaultNotAuthorized = errors.New("device is not authorized for vault")
)

// RegisterDevice 在登录时登记或更新设备。新设备创建为 pending；
// 已吊销设备返回 ErrRevoked；迁移前遗留的空状态设备保留为 approved。
func RegisterDevice(db *gorm.DB, userID uint, clientID, deviceName string, now time.Time) (string, error) {
	clientID = NormalizeClientID(clientID)
	if clientID == "" {
		return "", errors.New("invalid client id")
	}
	deviceName = truncateRunes(strings.TrimSpace(deviceName), 128)

	var device models.ClientDevice
	err := db.Where("user_id = ? AND client_id = ?", userID, clientID).First(&device).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		device = models.ClientDevice{
			UserID:     userID,
			ClientID:   clientID,
			Name:       deviceName,
			Status:     DeviceStatusPending,
			LastSeenAt: now,
		}
		if err := db.Create(&device).Error; err != nil {
			return "", err
		}
		return device.Status, nil
	}
	if err != nil {
		return "", err
	}
	if device.RevokedAt.Valid || device.Status == DeviceStatusRevoked {
		return device.Status, ErrRevoked
	}

	updates := map[string]any{"last_seen_at": now}
	if strings.TrimSpace(device.Name) == "" && deviceName != "" {
		updates["name"] = deviceName
	}
	if device.Status == "" {
		// 迁移前遗留设备默认 approved，避免锁住现有用户。
		updates["status"] = DeviceStatusApproved
	}
	if err := db.Model(&models.ClientDevice{}).Where("id = ?", device.ID).Updates(updates).Error; err != nil {
		return "", err
	}
	return device.Status, nil
}

// GetDevice 返回设备当前状态与服务端确认的设备名。
func GetDevice(db *gorm.DB, userID uint, clientID string) (string, string, error) {
	clientID = NormalizeClientID(clientID)
	var device models.ClientDevice
	if err := db.Where("user_id = ? AND client_id = ?", userID, clientID).
		First(&device).Error; err != nil {
		return "", "", err
	}
	status := device.Status
	if status == "" {
		if device.RevokedAt.Valid {
			status = DeviceStatusRevoked
		} else {
			status = DeviceStatusApproved
		}
	}
	return status, device.Name, nil
}

// CheckApproved 校验设备已批准且未吊销（用于仓库创建等无仓库上下文操作）。
func CheckApproved(db *gorm.DB, userID uint, clientID string) error {
	clientID = NormalizeClientID(clientID)
	if clientID == "" {
		return errors.New("invalid client id")
	}
	var device models.ClientDevice
	err := db.Select("status", "revoked_at").
		Where("user_id = ? AND client_id = ?", userID, clientID).
		First(&device).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return ErrDeviceUnknown
	}
	if err != nil {
		return err
	}
	if device.RevokedAt.Valid || device.Status == DeviceStatusRevoked {
		return ErrRevoked
	}
	if device.Status != DeviceStatusApproved {
		return ErrDevicePending
	}
	return nil
}

// CheckVaultAccess 校验设备已批准、未吊销且被授权访问该仓库。
func CheckVaultAccess(db *gorm.DB, userID uint, clientID, vaultID string) error {
	if err := CheckApproved(db, userID, clientID); err != nil {
		return err
	}
	var access models.DeviceVaultAccess
	err := db.Where(
		"user_id = ? AND client_id = ? AND vault_id = ?",
		userID, clientID, vaultID,
	).First(&access).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return ErrVaultNotAuthorized
	}
	return err
}

// CheckActive 兼容旧调用：只校验设备未吊销。
func CheckActive(db *gorm.DB, userID uint, clientID string) error {
	clientID = NormalizeClientID(clientID)
	if clientID == "" {
		return errors.New("invalid client id")
	}
	var device models.ClientDevice
	err := db.Select("id", "revoked_at").
		Where("user_id = ? AND client_id = ?", userID, clientID).
		First(&device).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil
	}
	if err != nil {
		return err
	}
	if device.RevokedAt.Valid {
		return ErrRevoked
	}
	return nil
}

// Touch 记录设备活动并维护设备对仓库的同步游标。
func Touch(
	db *gorm.DB,
	userID uint,
	vaultID, clientID, deviceName string,
	acknowledgedCursor *int64,
	now time.Time,
) error {
	clientID = NormalizeClientID(clientID)
	if clientID == "" {
		return errors.New("invalid client id")
	}
	deviceName = strings.TrimSpace(deviceName)
	deviceName = truncateRunes(deviceName, 128)

	return db.Transaction(func(tx *gorm.DB) error {
		var device models.ClientDevice
		err := tx.Where("user_id = ? AND client_id = ?", userID, clientID).First(&device).Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			device = models.ClientDevice{
				UserID:     userID,
				ClientID:   clientID,
				Name:       deviceName,
				Status:     DeviceStatusPending,
				LastSeenAt: now,
			}
			if err := tx.Create(&device).Error; err != nil {
				return err
			}
		} else if err != nil {
			return err
		} else {
			if device.RevokedAt.Valid {
				return ErrRevoked
			}
			updates := map[string]any{"last_seen_at": now}
			if device.Name == "" && deviceName != "" {
				updates["name"] = deviceName
			}
			if err := tx.Model(&models.ClientDevice{}).Where("id = ?", device.ID).
				Updates(updates).Error; err != nil {
				return err
			}
		}

		if vaultID == "" {
			return nil
		}
		var binding models.DeviceVault
		err = tx.Where(
			"user_id = ? AND client_id = ? AND vault_id = ?",
			userID, clientID, vaultID,
		).First(&binding).Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			binding = models.DeviceVault{
				UserID:   userID,
				ClientID: clientID,
				VaultID:  vaultID,
			}
			if acknowledgedCursor != nil {
				binding.LastCursor = *acknowledgedCursor
				binding.LastSyncAt = now
			}
			return tx.Create(&binding).Error
		}
		if err != nil {
			return err
		}
		if acknowledgedCursor == nil {
			return nil
		}
		return tx.Model(&models.DeviceVault{}).Where("id = ?", binding.ID).Updates(map[string]any{
			"last_cursor": gorm.Expr(
				"CASE WHEN last_cursor < ? THEN ? ELSE last_cursor END",
				*acknowledgedCursor,
				*acknowledgedCursor,
			),
			"last_sync_at": now,
		}).Error
	})
}

// ReplaceVaultAccesses 事务内替换设备的仓库授权列表。
func ReplaceVaultAccesses(
	db *gorm.DB,
	userID uint,
	clientID string,
	vaultIDs []string,
	grantedBy uint,
) error {
	clientID = NormalizeClientID(clientID)
	if clientID == "" {
		return errors.New("invalid client id")
	}
	return db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("user_id = ? AND client_id = ?", userID, clientID).
			Delete(&models.DeviceVaultAccess{}).Error; err != nil {
			return err
		}
		for _, vaultID := range vaultIDs {
			access := models.DeviceVaultAccess{
				UserID:          userID,
				ClientID:        clientID,
				VaultID:         vaultID,
				GrantedByUserID: grantedBy,
				GrantedAt:       time.Now(),
			}
			if err := tx.Create(&access).Error; err != nil {
				return err
			}
		}
		return nil
	})
}

// GrantAccess 为单个仓库增加设备授权（幂等）。
func GrantAccess(db *gorm.DB, userID uint, clientID, vaultID string, grantedBy uint) error {
	clientID = NormalizeClientID(clientID)
	if clientID == "" {
		return errors.New("invalid client id")
	}
	access := models.DeviceVaultAccess{
		UserID:          userID,
		ClientID:        clientID,
		VaultID:         vaultID,
		GrantedByUserID: grantedBy,
		GrantedAt:       time.Now(),
	}
	return db.Where(
		"user_id = ? AND client_id = ? AND vault_id = ?",
		userID, clientID, vaultID,
	).FirstOrCreate(&access).Error
}

// RevokeAccess 撤销设备对单个仓库的授权。
func RevokeAccess(db *gorm.DB, userID uint, clientID, vaultID string) error {
	return db.Where(
		"user_id = ? AND client_id = ? AND vault_id = ?",
		userID, clientID, vaultID,
	).Delete(&models.DeviceVaultAccess{}).Error
}

// RevokeAllDeviceAccesses 撤销设备全部仓库授权（吊销设备时调用）。
func RevokeAllDeviceAccesses(db *gorm.DB, userID uint, clientID string) error {
	return db.Where("user_id = ? AND client_id = ?", userID, clientID).
		Delete(&models.DeviceVaultAccess{}).Error
}

// NormalizeClientID 校验并规整客户端设备 ID。
func NormalizeClientID(value string) string {
	value = strings.TrimSpace(value)
	if value == "" || len(value) > 64 {
		return ""
	}
	for _, r := range value {
		if !(r == '-' || r == '_' || r == '.' ||
			(r >= '0' && r <= '9') ||
			(r >= 'a' && r <= 'z') ||
			(r >= 'A' && r <= 'Z')) {
			return ""
		}
	}
	return value
}

// DecodeDeviceName 解码请求头中 URL 编码的设备名。
func DecodeDeviceName(value string) string {
	decoded, err := url.QueryUnescape(value)
	if err != nil {
		return strings.TrimSpace(value)
	}
	return strings.TrimSpace(decoded)
}

func truncateRunes(value string, limit int) string {
	runes := []rune(value)
	if len(runes) <= limit {
		return value
	}
	return string(runes[:limit])
}

// ValidStatus 校验设备状态取值。
func ValidStatus(status string) bool {
	return status == DeviceStatusPending ||
		status == DeviceStatusApproved ||
		status == DeviceStatusRevoked
}

// EffectiveStatus 兼容迁移前遗留的状态字段。
func EffectiveStatus(revokedAt sql.NullTime, status string) string {
	switch status {
	case DeviceStatusPending, DeviceStatusApproved, DeviceStatusRevoked:
		return status
	}
	if revokedAt.Valid {
		return DeviceStatusRevoked
	}
	return DeviceStatusApproved
}
