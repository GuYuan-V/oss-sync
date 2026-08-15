package database

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
<<<<<<< HEAD
	"time"
=======
>>>>>>> 3b7aaacb143eff9df5a728b914a633fc58e70a6b

	"github.com/glebarez/sqlite"
	"github.com/google/uuid"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
<<<<<<< HEAD
	"gorm.io/gorm/clause"
=======
>>>>>>> 3b7aaacb143eff9df5a728b914a633fc58e70a6b
	"gorm.io/gorm/logger"

	"github.com/oss/oss-server/internal/config"
	"github.com/oss/oss-server/internal/models"
)

// Init 根据配置初始化 GORM 连接。
// SQLite 的 DSN 是文件路径；上层会确保父目录存在。
func Init(cfg *config.Config) (*gorm.DB, error) {
	switch cfg.Database.Driver {
	case "sqlite":
		return initSQLite(cfg)
	case "postgres":
		return initPostgres(cfg)
	default:
		return nil, fmt.Errorf("不支持的数据库驱动: %s", cfg.Database.Driver)
	}
}

func initSQLite(cfg *config.Config) (*gorm.DB, error) {
	dsn := cfg.Database.DSN
	if dir := filepath.Dir(dsn); dir != "" && dir != "." {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return nil, fmt.Errorf("创建 SQLite 目录 %s 失败: %w", dir, err)
		}
	}
	gormLogLevel := logger.Warn
	if config.Env() == "dev" {
		gormLogLevel = logger.Info
	}
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{
		Logger: logger.Default.LogMode(gormLogLevel),
	})
	if err != nil {
		return nil, fmt.Errorf("打开 SQLite 失败: %w", err)
	}
	return db, nil
}

func initPostgres(cfg *config.Config) (*gorm.DB, error) {
	gormLogLevel := logger.Warn
	if config.Env() == "dev" {
		gormLogLevel = logger.Info
	}
	db, err := gorm.Open(postgres.Open(cfg.Database.DSN), &gorm.Config{
		Logger: logger.Default.LogMode(gormLogLevel),
	})
	if err != nil {
		return nil, fmt.Errorf("连接 PostgreSQL 失败: %w", err)
	}
	return db, nil
}

// AutoMigrate 注册模型，并补齐旧数据的默认 Vault 和 revision。
func AutoMigrate(db *gorm.DB) error {
	if err := db.AutoMigrate(
		&models.User{},
		&models.SystemSetting{},
		&models.Vault{},
		&models.VaultMember{},
		&models.VaultBackup{},
		&models.VaultSetting{},
		&models.VaultSyncState{},
		&models.ClientDevice{},
		&models.DeviceVault{},
<<<<<<< HEAD
		&models.DeviceVaultAccess{},
=======
>>>>>>> 3b7aaacb143eff9df5a728b914a633fc58e70a6b
		&models.StorageIssue{},
		&models.UserSetting{},
		&models.File{},
		&models.Share{},
		&models.Collaboration{},
<<<<<<< HEAD
		&models.FileHistory{},
=======
>>>>>>> 3b7aaacb143eff9df5a728b914a633fc58e70a6b
	); err != nil {
		return fmt.Errorf("AutoMigrate 失败: %w", err)
	}
	if err := backfillLegacyVaults(db); err != nil {
		return err
	}
<<<<<<< HEAD
	if err := backfillVaultRevisions(db); err != nil {
		return err
	}
	if err := backfillDeviceStates(db); err != nil {
		return err
	}
	return backfillVaultSettings(db)
=======
	return backfillVaultRevisions(db)
>>>>>>> 3b7aaacb143eff9df5a728b914a633fc58e70a6b
}

// backfillLegacyVaults 只为升级前没有 VaultID 的历史内容创建承载 Vault。
// 没有历史内容的新账户保持零 Vault，等待用户在插件中明确创建。
func backfillLegacyVaults(db *gorm.DB) error {
	var users []models.User
	if err := db.Find(&users).Error; err != nil {
		return fmt.Errorf("查询用户以初始化默认 Vault 失败: %w", err)
	}
	for _, user := range users {
		if err := db.Transaction(func(tx *gorm.DB) error {
			var vault models.Vault
			err := tx.Where("owner_id = ? AND is_default = ?", user.ID, true).First(&vault).Error
			if errors.Is(err, gorm.ErrRecordNotFound) {
				err = tx.Where("owner_id = ?", user.ID).Order("created_at asc").First(&vault).Error
			}
			if errors.Is(err, gorm.ErrRecordNotFound) {
				needsVault, err := hasVaultlessLegacyContent(tx, user.ID)
				if err != nil {
					return err
				}
				if !needsVault {
					return nil
				}
				vault = models.Vault{
					ID:        uuid.NewString(),
					OwnerID:   user.ID,
					Name:      "Default",
					IsDefault: true,
				}
				if err := tx.Create(&vault).Error; err != nil {
					return err
				}
			} else if err != nil {
				return err
			}
			if !vault.IsDefault {
				if err := tx.Model(&models.Vault{}).Where("id = ?", vault.ID).
					Update("is_default", true).Error; err != nil {
					return err
				}
			}
			if err := tx.FirstOrCreate(
				&models.VaultSetting{},
				models.VaultSetting{VaultID: vault.ID},
			).Error; err != nil {
				return err
			}
			if err := tx.FirstOrCreate(
				&models.VaultSyncState{},
				models.VaultSyncState{VaultID: vault.ID},
			).Error; err != nil {
				return err
			}
			if err := tx.Model(&models.File{}).
				Where("user_id = ? AND (vault_id = '' OR vault_id IS NULL)", user.ID).
				Update("vault_id", vault.ID).Error; err != nil {
				return err
			}
			if err := tx.Model(&models.Share{}).
				Where("user_id = ? AND (vault_id = '' OR vault_id IS NULL)", user.ID).
				Update("vault_id", vault.ID).Error; err != nil {
				return err
			}
			return tx.Model(&models.Collaboration{}).
				Where("owner_id = ? AND (vault_id = '' OR vault_id IS NULL)", user.ID).
				Update("vault_id", vault.ID).Error
		}); err != nil {
			return fmt.Errorf("为用户 %d 创建默认 Vault 失败: %w", user.ID, err)
		}
	}
	return nil
}

func hasVaultlessLegacyContent(tx *gorm.DB, userID uint) (bool, error) {
	queries := []struct {
		model any
		where string
	}{
		{model: &models.File{}, where: "user_id = ? AND (vault_id = '' OR vault_id IS NULL)"},
		{model: &models.Share{}, where: "user_id = ? AND (vault_id = '' OR vault_id IS NULL)"},
		{model: &models.Collaboration{}, where: "owner_id = ? AND (vault_id = '' OR vault_id IS NULL)"},
	}
	for _, query := range queries {
		var count int64
		if err := tx.Model(query.model).Where(query.where, userID).Count(&count).Error; err != nil {
			return false, err
		}
		if count > 0 {
			return true, nil
		}
	}
	return false, nil
}

func backfillVaultRevisions(db *gorm.DB) error {
	var vaults []models.Vault
	if err := db.Unscoped().Find(&vaults).Error; err != nil {
		return fmt.Errorf("查询 Vault 以回填 revision 失败: %w", err)
	}
	for _, vault := range vaults {
		if err := db.Transaction(func(tx *gorm.DB) error {
			var state models.VaultSyncState
			if err := tx.FirstOrCreate(
				&state,
				models.VaultSyncState{VaultID: vault.ID},
			).Error; err != nil {
				return err
			}

			var maxRevision int64
			if err := tx.Model(&models.File{}).
				Where("vault_id = ?", vault.ID).
				Select("COALESCE(MAX(revision), 0)").
				Scan(&maxRevision).Error; err != nil {
				return err
			}
			head := state.HeadRevision
			if maxRevision > head {
				head = maxRevision
			}

			var legacyFiles []models.File
			if err := tx.Where("vault_id = ? AND revision = 0", vault.ID).
				Order("id asc").
				Find(&legacyFiles).Error; err != nil {
				return err
			}
			for _, file := range legacyFiles {
				head++
				if err := tx.Model(&models.File{}).Where("id = ?", file.ID).
					Update("revision", head).Error; err != nil {
					return err
				}
			}

			var storageUsed int64
			if err := tx.Model(&models.File{}).
				Where("vault_id = ? AND is_deleted = ?", vault.ID, false).
				Select("COALESCE(SUM(size), 0)").
				Scan(&storageUsed).Error; err != nil {
				return err
			}
			if err := tx.Model(&models.Vault{}).Unscoped().Where("id = ?", vault.ID).
				Update("storage_used", storageUsed).Error; err != nil {
				return err
			}
			return tx.Model(&models.VaultSyncState{}).Where("vault_id = ?", vault.ID).
				Update("head_revision", head).Error
		}); err != nil {
			return fmt.Errorf("回填 Vault %s revision 失败: %w", vault.ID, err)
		}
	}
	return nil
}
<<<<<<< HEAD

// backfillDeviceStates 为旧版设备补齐状态，并为已有同步绑定补齐仓库授权。
// 未吊销的旧设备回填为 approved，避免升级后把现有设备锁在外面。
func backfillDeviceStates(db *gorm.DB) error {
	if err := db.Model(&models.ClientDevice{}).
		Where("status = '' AND revoked_at IS NULL").
		Update("status", "approved").Error; err != nil {
		return fmt.Errorf("回填已批准设备失败: %w", err)
	}
	if err := db.Model(&models.ClientDevice{}).
		Where("status = '' AND revoked_at IS NOT NULL").
		Update("status", "revoked").Error; err != nil {
		return fmt.Errorf("回填已吊销设备失败: %w", err)
	}

	var bindings []models.DeviceVault
	if err := db.Find(&bindings).Error; err != nil {
		return fmt.Errorf("查询旧设备同步绑定失败: %w", err)
	}
	for _, binding := range bindings {
		grantedAt := binding.CreatedAt
		if grantedAt.IsZero() {
			grantedAt = time.Now()
		}
		if err := db.Clauses(clause.OnConflict{DoNothing: true}).
			Create(&models.DeviceVaultAccess{
				UserID:          binding.UserID,
				ClientID:        binding.ClientID,
				VaultID:         binding.VaultID,
				GrantedByUserID: binding.UserID,
				GrantedAt:       grantedAt,
			}).Error; err != nil {
			return fmt.Errorf("回填设备仓库授权失败: %w", err)
		}
	}
	return nil
}

// backfillVaultSettings 为已有仓库补齐新默认值。
func backfillVaultSettings(db *gorm.DB) error {
	if err := db.Model(&models.VaultSetting{}).
		Where("recycle_bin_days = 0 OR recycle_bin_days IS NULL").
		Update("recycle_bin_days", 30).Error; err != nil {
		return fmt.Errorf("回填仓库回收站保留期失败: %w", err)
	}
	return nil
}
=======
>>>>>>> 3b7aaacb143eff9df5a728b914a633fc58e70a6b
