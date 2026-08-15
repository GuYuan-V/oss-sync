package settingspolicy

import (
	"path/filepath"
	"testing"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"

	"github.com/oss/oss-server/internal/models"
)

func newRuntimePolicyDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "settings-policy.db")), &gorm.Config{})
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
	if err := db.AutoMigrate(&models.SystemSetting{}, &models.UserSetting{}, &models.Vault{}); err != nil {
		t.Fatalf("migrate settings policy fixtures: %v", err)
	}
	return db
}

func TestEffectiveForUser_whenPreferencesExceedCeilings_clampsEveryValue(t *testing.T) {
	// Given
	db := newRuntimePolicyDB(t)
	if err := db.Create(&models.SystemSetting{
		ID: 1, MaxLongPollWaitSec: 20, MaxSyncDebounceSec: 60, MaxRecycleBinDays: 90,
		MaxVaultStorageBytes: 900, MaxUploadSizeBytes: 800,
	}).Error; err != nil {
		t.Fatalf("create system settings: %v", err)
	}
	if err := db.Create(&models.UserSetting{
		UserID: 7, LongPollWaitSec: 25, SyncDebounceSec: 100, DefaultRecycleBinDays: 120,
		VaultStorageBytes: 1000, UploadSizeBytes: 1000,
	}).Error; err != nil {
		t.Fatalf("create user settings: %v", err)
	}

	// When
	effective, err := EffectiveForUser(db, 7, 2000)

	// Then
	if err != nil {
		t.Fatalf("EffectiveForUser: %v", err)
	}
	want := Effective{
		LongPollWaitSec: 20, SyncDebounceSec: 60, RecycleBinDays: 90,
		VaultStorageBytes: 900, UploadSizeBytes: 800,
	}
	if effective != want {
		t.Errorf("effective settings = %+v, want %+v", effective, want)
	}
}

func TestEffectiveForVault_whenVaultExists_resolvesOwnerPreferences(t *testing.T) {
	// Given
	db := newRuntimePolicyDB(t)
	if err := db.Create(&models.SystemSetting{ID: 1, MaxLongPollWaitSec: 20}).Error; err != nil {
		t.Fatalf("create system settings: %v", err)
	}
	if err := db.Create(&models.UserSetting{UserID: 9, LongPollWaitSec: 18}).Error; err != nil {
		t.Fatalf("create user settings: %v", err)
	}
	if err := db.Create(&models.Vault{ID: "vault-policy", OwnerID: 9, Name: "Policy"}).Error; err != nil {
		t.Fatalf("create vault: %v", err)
	}

	// When
	effective, err := EffectiveForVault(db, "vault-policy", 2000)

	// Then
	if err != nil {
		t.Fatalf("EffectiveForVault: %v", err)
	}
	if effective.LongPollWaitSec != 18 {
		t.Errorf("long poll wait = %d, want 18", effective.LongPollWaitSec)
	}
}

func TestCustomFragmentsEnabled_whenMissingSystemSetting_returnsFalse(t *testing.T) {
	// Given
	db := newRuntimePolicyDB(t)

	// When
	enabled := CustomFragmentsEnabled(db)

	// Then
	if enabled {
		t.Fatalf("enabled = %v, want false", enabled)
	}
}

func TestCustomFragmentsEnabled_whenSystemSettingIsEnabled_returnsTrue(t *testing.T) {
	// Given
	db := newRuntimePolicyDB(t)
	if err := db.Create(&models.SystemSetting{
		ID: 1, RegistrationEnabled: true, CustomFragmentsEnabled: true,
	}).Error; err != nil {
		t.Fatalf("create system settings: %v", err)
	}

	// When
	enabled := CustomFragmentsEnabled(db)

	// Then
	if !enabled {
		t.Fatalf("enabled = %v, want true", enabled)
	}
}

func TestCustomFragmentsEnabled_whenDatabaseError_returnsFalse(t *testing.T) {
	// Given
	db := newRuntimePolicyDB(t)
	if err := db.Migrator().DropTable(&models.SystemSetting{}); err != nil {
		t.Fatalf("drop system_settings: %v", err)
	}

	// When
	enabled := CustomFragmentsEnabled(db)

	// Then
	if enabled {
		t.Fatalf("enabled = %v, want false", enabled)
	}
}
