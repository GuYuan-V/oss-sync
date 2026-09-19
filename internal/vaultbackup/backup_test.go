package vaultbackup

import (
	"archive/zip"
	"os"
	"path/filepath"
	"testing"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"

	"github.com/helantianshen/oss-sync/internal/models"
)

func TestCreate_whenDataDirectoryIsConfigured_writesArchiveInsidePersistentStorage(t *testing.T) {
	// Given
	dataDir := t.TempDir()
	db, err := gorm.Open(sqlite.Open(filepath.Join(dataDir, "backup.db")), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatalf("open database: %v", err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatalf("get database handle: %v", err)
	}
	t.Cleanup(func() {
		if err := sqlDB.Close(); err != nil {
			t.Errorf("close database: %v", err)
		}
	})
	if err := db.AutoMigrate(&models.Vault{}, &models.VaultSetting{}, &models.VaultMember{}, &models.File{}, &models.VaultBackup{}); err != nil {
		t.Fatalf("migrate backup fixtures: %v", err)
	}
	vault := models.Vault{ID: "vault-backup", OwnerID: 1, Name: "Persistent backup"}
	if err := db.Create(&vault).Error; err != nil {
		t.Fatalf("create Vault fixture: %v", err)
	}

	// When
	backup, err := Create(db, dataDir, vault)

	// Then
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	archivePath, err := Path(dataDir, backup.FileName)
	if err != nil {
		t.Fatalf("Path: %v", err)
	}
	wantRoot := filepath.Join(dataDir, "backups", "vaults")
	if filepath.Dir(archivePath) != wantRoot {
		t.Fatalf("backup directory = %q, want %q", filepath.Dir(archivePath), wantRoot)
	}
	if _, err := os.Stat(archivePath); err != nil {
		t.Fatalf("stat backup archive: %v", err)
	}
	reader, err := zip.OpenReader(archivePath)
	if err != nil {
		t.Fatalf("open backup archive: %v", err)
	}
	defer reader.Close()
	if len(reader.File) != 1 || reader.File[0].Name != "manifest.json" {
		t.Fatalf("backup entries = %+v, want manifest.json", reader.File)
	}
}
