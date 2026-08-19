// Package recycle 管理软删除文件的回收站存储与恢复。
//
// 删除文件时正文移动到 data/vaults/<vault>/recycle/ 下，File 记录保留墓碑；
// 恢复时移回 files 目录，过期后由定时任务物理清除。
package recycle

import (
	"errors"
	"os"
	"path/filepath"
	"strconv"

	"gorm.io/gorm"

	"github.com/oss/oss-server/internal/models"
	"github.com/oss/oss-server/internal/settingspolicy"
)

// RetentionDays 返回仓库的回收站保留天数，0 表示继承系统默认值。
func RetentionDays(db *gorm.DB, vaultID string) (int, error) {
	var setting models.VaultSetting
	err := db.Where("vault_id = ?", vaultID).First(&setting).Error
	if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
		return 0, err
	}
	effective, err := settingspolicy.EffectiveForVault(db, vaultID, 0)
	if err != nil {
		return 0, err
	}
	days := effective.RecycleBinDays
	if setting.RecycleBinDays > 0 && setting.RecycleBinDays < days {
		days = setting.RecycleBinDays
	}
	return days, nil
}

// Key 返回某 File 在回收站中的存储键。
func Key(vaultID string, fileID uint) string {
	return filepath.ToSlash(filepath.Join("vaults", vaultID, "recycle", strconv.FormatUint(uint64(fileID), 10)))
}

// DiskPath 返回回收站正文的绝对路径。
func DiskPath(dataDir string, file models.File) string {
	if file.StorageKey == "" {
		return ""
	}
	return filepath.Join(dataDir, filepath.FromSlash(file.StorageKey))
}

// MoveIn 将 contentPath 移入回收站，返回回收站存储键。
func MoveIn(dataDir, vaultID string, fileID uint, contentPath string) (string, error) {
	key := Key(vaultID, fileID)
	dest := filepath.Join(dataDir, filepath.FromSlash(key))
	if err := os.MkdirAll(filepath.Dir(dest), 0o750); err != nil {
		return "", err
	}
	if err := os.Rename(contentPath, dest); err != nil {
		if os.IsNotExist(err) {
			return key, nil
		}
		return "", err
	}
	return key, nil
}

// MoveOut 将回收站正文移回 contentPath。
func MoveOut(dataDir string, file models.File, contentPath string) error {
	src := DiskPath(dataDir, file)
	if src == "" {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(contentPath), 0o750); err != nil {
		return err
	}
	if err := os.Rename(src, contentPath); err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	return nil
}

// Remove 删除回收站正文，忽略不存在的情况。
func Remove(dataDir string, file models.File) error {
	abs := DiskPath(dataDir, file)
	if abs == "" {
		return nil
	}
	err := os.Remove(abs)
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

// CleanupVault 删除某仓库的全部回收站正文。
func CleanupVault(dataDir, vaultID string) error {
	return os.RemoveAll(filepath.Join(dataDir, "vaults", vaultID, "recycle"))
}
