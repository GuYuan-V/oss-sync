package database

import (
	"path/filepath"
	"testing"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"

	"github.com/oss/oss-server/internal/models"
<<<<<<< HEAD
	"github.com/oss/oss-server/internal/settingspolicy"
)

type legacySystemSetting struct {
	ID                    uint `gorm:"primaryKey"`
	RegistrationEnabled   bool `gorm:"not null"`
	JWTSecret             string
	PublicHomeVaultID     string `gorm:"size:36"`
	DefaultRecycleBinDays int    `gorm:"not null;default:30"`
	SyncMode              string `gorm:"size:32;not null;default:'user_choice'"`
	MaxLongPollWaitSec    int    `gorm:"not null;default:30"`
	MaxSyncDebounceSec    int    `gorm:"not null;default:300"`
	MaxRecycleBinDays     int    `gorm:"not null;default:3650"`
	MaxVaultStorageBytes  int64  `gorm:"not null;default:0"`
	MaxUploadSizeBytes    int64  `gorm:"not null;default:0"`
}

func (legacySystemSetting) TableName() string {
	return "system_settings"
}

=======
)

>>>>>>> 3b7aaacb143eff9df5a728b914a633fc58e70a6b
func TestAutoMigrateCreatesDefaultVaultAndBackfillsLegacyRows(t *testing.T) {
	db, err := gorm.Open(
		sqlite.Open(filepath.Join(t.TempDir(), "migration.db")),
		&gorm.Config{Logger: logger.Default.LogMode(logger.Silent)},
	)
	if err != nil {
		t.Fatal(err)
	}
<<<<<<< HEAD
	if sqlDB, err := db.DB(); err == nil {
		t.Cleanup(func() { _ = sqlDB.Close() })
	}
=======
>>>>>>> 3b7aaacb143eff9df5a728b914a633fc58e70a6b
	if err := AutoMigrate(db); err != nil {
		t.Fatal(err)
	}

	user := models.User{Username: "legacy", PasswordHash: "hash", Role: "user"}
	if err := db.Create(&user).Error; err != nil {
		t.Fatal(err)
	}
	file := models.File{
		UserID: user.ID,
		Path:   "Notes/Legacy.md",
		Type:   "markdown",
		Hash:   "legacy-hash",
		Size:   42,
		MTime:  1,
	}
	if err := db.Create(&file).Error; err != nil {
		t.Fatal(err)
	}
	share := models.Share{
		ShareID:    "legacy",
		UserID:     user.ID,
		TargetPath: file.Path,
	}
	if err := db.Create(&share).Error; err != nil {
		t.Fatal(err)
	}

	if err := AutoMigrate(db); err != nil {
		t.Fatal(err)
	}

	var vault models.Vault
	if err := db.Where("owner_id = ? AND is_default = ?", user.ID, true).First(&vault).Error; err != nil {
		t.Fatalf("default vault: %v", err)
	}
	var migratedFile models.File
	if err := db.First(&migratedFile, file.ID).Error; err != nil {
		t.Fatal(err)
	}
	if migratedFile.VaultID != vault.ID || migratedFile.Revision <= 0 {
		t.Fatalf("file was not backfilled: %+v", migratedFile)
	}
	var migratedShare models.Share
	if err := db.First(&migratedShare, "share_id = ?", share.ShareID).Error; err != nil {
		t.Fatal(err)
	}
	if migratedShare.VaultID != vault.ID {
		t.Fatalf("share vault=%q want %q", migratedShare.VaultID, vault.ID)
	}
	var state models.VaultSyncState
	if err := db.First(&state, "vault_id = ?", vault.ID).Error; err != nil {
		t.Fatal(err)
	}
	if state.HeadRevision != migratedFile.Revision {
		t.Fatalf("head=%d file revision=%d", state.HeadRevision, migratedFile.Revision)
	}
	if vault.StorageUsed != file.Size {
		t.Fatalf("storage_used=%d want %d", vault.StorageUsed, file.Size)
	}

	firstRevision := migratedFile.Revision
	if err := AutoMigrate(db); err != nil {
		t.Fatal(err)
	}
	if err := db.First(&migratedFile, file.ID).Error; err != nil {
		t.Fatal(err)
	}
	if migratedFile.Revision != firstRevision {
		t.Fatalf("migration is not idempotent: revision=%d want %d", migratedFile.Revision, firstRevision)
	}
}

func TestAutoMigrateDoesNotCreateVaultForEmptyAccount(t *testing.T) {
	db, err := gorm.Open(
		sqlite.Open(filepath.Join(t.TempDir(), "empty-account.db")),
		&gorm.Config{Logger: logger.Default.LogMode(logger.Silent)},
	)
	if err != nil {
		t.Fatal(err)
	}
<<<<<<< HEAD
	if sqlDB, err := db.DB(); err == nil {
		t.Cleanup(func() { _ = sqlDB.Close() })
	}
=======
>>>>>>> 3b7aaacb143eff9df5a728b914a633fc58e70a6b
	if err := AutoMigrate(db); err != nil {
		t.Fatal(err)
	}
	user := models.User{Username: "empty", PasswordHash: "hash", Role: "user"}
	if err := db.Create(&user).Error; err != nil {
		t.Fatal(err)
	}

	if err := AutoMigrate(db); err != nil {
		t.Fatal(err)
	}
	var vaultCount int64
	if err := db.Model(&models.Vault{}).Where("owner_id = ?", user.ID).Count(&vaultCount).Error; err != nil {
		t.Fatal(err)
	}
	if vaultCount != 0 {
		t.Fatalf("migration created %d vaults for an empty account, want 0", vaultCount)
	}
}
<<<<<<< HEAD

func TestAutoMigrate_whenPolicySettingsExist_preservesValuesAndInheritance(t *testing.T) {
	// Given
	db, err := gorm.Open(
		sqlite.Open(filepath.Join(t.TempDir(), "policy-settings.db")),
		&gorm.Config{Logger: logger.Default.LogMode(logger.Silent)},
	)
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatalf("get sqlite connection: %v", err)
	}
	t.Cleanup(func() {
		if err := sqlDB.Close(); err != nil {
			t.Errorf("close sqlite: %v", err)
		}
	})
	if err := AutoMigrate(db); err != nil {
		t.Fatalf("initial migration: %v", err)
	}
	user := models.User{Username: "policy-owner", PasswordHash: "hash", Role: "user"}
	if err := db.Create(&user).Error; err != nil {
		t.Fatalf("create user: %v", err)
	}
	system := models.SystemSetting{
		ID: 1, RegistrationEnabled: true, JWTSecret: "stable-secret", DefaultRecycleBinDays: 45,
		MaxLongPollWaitSec: 20, MaxSyncDebounceSec: 60, MaxRecycleBinDays: 90,
		MaxVaultStorageBytes: 1000, MaxUploadSizeBytes: 800,
	}
	preference := models.UserSetting{
		UserID: user.ID, LongPollWaitSec: 18, SyncDebounceSec: 12, DefaultRecycleBinDays: 40,
		VaultStorageBytes: 700, UploadSizeBytes: 600,
	}
	vault := models.Vault{ID: "preserved-vault", OwnerID: user.ID, Name: "Preserved", StorageQuota: 0}
	fixtures := []any{
		&system,
		&preference,
		&vault,
		&models.VaultSetting{VaultID: vault.ID, RecycleBinDays: 30},
		&models.VaultSyncState{VaultID: vault.ID, HeadRevision: 7},
	}
	for _, fixture := range fixtures {
		if err := db.Create(fixture).Error; err != nil {
			t.Fatalf("create policy fixture: %v", err)
		}
	}

	// When
	if err := AutoMigrate(db); err != nil {
		t.Fatalf("repeat migration: %v", err)
	}

	// Then
	var migratedSystem models.SystemSetting
	if err := db.First(&migratedSystem, 1).Error; err != nil {
		t.Fatalf("load system settings: %v", err)
	}
	if migratedSystem.JWTSecret != "stable-secret" || migratedSystem.MaxRecycleBinDays != 90 || !migratedSystem.RegistrationEnabled {
		t.Errorf("system settings changed during migration: %+v", migratedSystem)
	}
	var migratedPreference models.UserSetting
	if err := db.Where("user_id = ?", user.ID).First(&migratedPreference).Error; err != nil {
		t.Fatalf("load user settings: %v", err)
	}
	if migratedPreference.LongPollWaitSec != 18 || migratedPreference.UploadSizeBytes != 600 {
		t.Errorf("user settings changed during migration: %+v", migratedPreference)
	}
	var migratedVault models.Vault
	if err := db.Where("id = ?", vault.ID).First(&migratedVault).Error; err != nil {
		t.Fatalf("load vault: %v", err)
	}
	if migratedVault.StorageQuota != 0 {
		t.Errorf("inherited vault quota = %d, want 0", migratedVault.StorageQuota)
	}
	effective, err := settingspolicy.EffectiveForVault(db, vault.ID, 2000)
	if err != nil {
		t.Fatalf("resolve migrated policy: %v", err)
	}
	want := settingspolicy.Effective{
		LongPollWaitSec: 18, SyncDebounceSec: 12, RecycleBinDays: 40,
		VaultStorageBytes: 700, UploadSizeBytes: 600,
	}
	if effective != want {
		t.Errorf("effective migrated policy = %+v, want %+v", effective, want)
	}
}

func TestAutoMigrate_whenSystemSettingsAreLegacy_addsCustomFragmentsEnabledWithDefaultFalse(t *testing.T) {
	// Given
	db, err := gorm.Open(
		sqlite.Open(filepath.Join(t.TempDir(), "legacy-system-settings.db")),
		&gorm.Config{Logger: logger.Default.LogMode(logger.Silent)},
	)
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatalf("get sqlite connection: %v", err)
	}
	t.Cleanup(func() {
		if err := sqlDB.Close(); err != nil {
			t.Errorf("close sqlite: %v", err)
		}
	})

	if err := db.AutoMigrate(&legacySystemSetting{}); err != nil {
		t.Fatalf("legacy migration: %v", err)
	}
	if err := db.Create(&legacySystemSetting{
		ID:                    1,
		RegistrationEnabled:   true,
		JWTSecret:             "legacy-secret",
		DefaultRecycleBinDays: 45,
		MaxLongPollWaitSec:    20,
		MaxSyncDebounceSec:    60,
		MaxRecycleBinDays:     90,
		MaxVaultStorageBytes:  1000,
		MaxUploadSizeBytes:    800,
	}).Error; err != nil {
		t.Fatalf("seed legacy system_settings row: %v", err)
	}

	if err := AutoMigrate(db); err != nil {
		t.Fatalf("migration: %v", err)
	}

	// Then
	var migrated models.SystemSetting
	if err := db.First(&migrated, 1).Error; err != nil {
		t.Fatalf("load system settings: %v", err)
	}
	if migrated.JWTSecret != "legacy-secret" {
		t.Fatalf("JWT secret should be preserved, got %q", migrated.JWTSecret)
	}
	if migrated.DefaultRecycleBinDays != 45 || migrated.MaxLongPollWaitSec != 20 || migrated.MaxSyncDebounceSec != 60 {
		t.Fatalf("system settings changed during migration: %+v", migrated)
	}
	if migrated.CustomFragmentsEnabled {
		t.Fatal("legacy migration should default custom fragments to false")
	}
}
=======
>>>>>>> 3b7aaacb143eff9df5a728b914a633fc58e70a6b
