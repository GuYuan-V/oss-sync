package recycle

import (
	"database/sql"
	"path/filepath"
	"testing"
	"time"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"

	"github.com/helantianshen/oss-sync/internal/models"
)

func TestRetentionDays_whenVaultOverrideExceedsAdministratorCeiling_clampsToCeiling(t *testing.T) {
	t.Parallel()

	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "recycle.db")), &gorm.Config{})
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
	if err := db.AutoMigrate(
		&models.User{},
		&models.SystemSetting{},
		&models.UserSetting{},
		&models.Vault{},
		&models.VaultSetting{},
	); err != nil {
		t.Fatalf("migrate recycle fixtures: %v", err)
	}
	fixtures := []any{
		&models.User{ID: 1, Username: "owner"},
		&models.SystemSetting{ID: 1, DefaultRecycleBinDays: 30, MaxRecycleBinDays: 90},
		&models.UserSetting{UserID: 1, DefaultRecycleBinDays: 120},
		&models.Vault{ID: "vault-1", OwnerID: 1, Name: "Notes"},
		&models.VaultSetting{VaultID: "vault-1", RecycleBinDays: 180},
	}
	for _, fixture := range fixtures {
		if err := db.Create(fixture).Error; err != nil {
			t.Fatalf("create recycle fixture: %v", err)
		}
	}

	days, err := RetentionDays(db, "vault-1")

	if err != nil {
		t.Fatalf("RetentionDays: %v", err)
	}
	if days != 90 {
		t.Errorf("retention days = %d, want 90", days)
	}
}

func TestCanRestore_whenRetentionBoundaryPasses_marksEntryExpired(t *testing.T) {
	deletedAt := time.Date(2026, time.September, 16, 12, 0, 0, 0, time.UTC)
	file := models.File{DeletedAt: sql.NullTime{Time: deletedAt, Valid: true}}

	beforeExpiry := CanRestore(file, 1, deletedAt.Add(24*time.Hour-time.Second))
	atExpiry := CanRestore(file, 1, deletedAt.Add(24*time.Hour))

	if !beforeExpiry {
		t.Error("entry should remain restorable before its retention deadline")
	}
	if atExpiry {
		t.Error("entry should expire at its retention deadline")
	}
}
