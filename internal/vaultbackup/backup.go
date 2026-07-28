// Package vaultbackup creates portable, administrator-managed archives before
// a Vault is permanently removed.
package vaultbackup

import (
	"archive/zip"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"github.com/oss/oss-server/internal/filestore"
	"github.com/oss/oss-server/internal/models"
)

const rootDirName = "backups/vaults"

type manifest struct {
	Format   string               `json:"format"`
	Created  time.Time            `json:"created_at"`
	Vault    models.Vault         `json:"vault"`
	Settings models.VaultSetting  `json:"settings"`
	Members  []models.VaultMember `json:"members"`
	Files    []models.File        `json:"files"`
}

// Root is intentionally relative to the server process working directory.
// It therefore remains a project-run-directory archive even when file storage
// itself is redirected elsewhere.
func Root() string { return rootDirName }

func Path(fileName string) (string, error) {
	if fileName == "" || filepath.Base(fileName) != fileName || !strings.HasSuffix(fileName, ".zip") {
		return "", fmt.Errorf("invalid backup file name")
	}
	return filepath.Join(Root(), fileName), nil
}

func Create(db *gorm.DB, dataDir string, vault models.Vault) (models.VaultBackup, error) {
	var setting models.VaultSetting
	if err := db.Where("vault_id = ?", vault.ID).First(&setting).Error; err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
		return models.VaultBackup{}, err
	}
	var members []models.VaultMember
	if err := db.Where("vault_id = ?", vault.ID).Order("created_at asc").Find(&members).Error; err != nil {
		return models.VaultBackup{}, err
	}
	var files []models.File
	if err := db.Where("vault_id = ?", vault.ID).Order("path asc").Find(&files).Error; err != nil {
		return models.VaultBackup{}, err
	}
	root := Root()
	if err := os.MkdirAll(root, 0o700); err != nil {
		return models.VaultBackup{}, fmt.Errorf("create backup directory: %w", err)
	}

	id := uuid.NewString()
	fileName := fmt.Sprintf("vault-%s-%s.zip", time.Now().UTC().Format("20060102T150405Z"), id)
	archivePath, err := Path(fileName)
	if err != nil {
		return models.VaultBackup{}, err
	}
	tmp, err := os.CreateTemp(root, ".oss-vault-backup-*.zip")
	if err != nil {
		return models.VaultBackup{}, err
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath)
	zw := zip.NewWriter(tmp)
	if err := writeJSON(zw, "manifest.json", manifest{
		Format: "oss-vault-backup/v1", Created: time.Now().UTC(), Vault: vault,
		Settings: setting, Members: members, Files: files,
	}); err != nil {
		_ = zw.Close()
		_ = tmp.Close()
		return models.VaultBackup{}, err
	}
	for _, file := range files {
		if file.IsDeleted {
			continue
		}
		if err := addFile(zw, filestore.DiskPath(dataDir, file), "files/"+filepath.ToSlash(file.Path)); err != nil {
			_ = zw.Close()
			_ = tmp.Close()
			return models.VaultBackup{}, err
		}
	}
	if err := zw.Close(); err != nil {
		_ = tmp.Close()
		return models.VaultBackup{}, err
	}
	if err := tmp.Close(); err != nil {
		return models.VaultBackup{}, err
	}
	if err := os.Rename(tmpPath, archivePath); err != nil {
		return models.VaultBackup{}, err
	}
	info, err := os.Stat(archivePath)
	if err != nil {
		return models.VaultBackup{}, err
	}
	backup := models.VaultBackup{
		ID: id, VaultID: vault.ID, OwnerID: vault.OwnerID, VaultName: vault.Name,
		FileName: fileName, Size: info.Size(),
	}
	if err := db.Create(&backup).Error; err != nil {
		_ = os.Remove(archivePath)
		return models.VaultBackup{}, err
	}
	return backup, nil
}

func Purge(db *gorm.DB, dataDir string, vault models.Vault) (models.VaultBackup, error) {
	backup, err := Create(db, dataDir, vault)
	if err != nil {
		return models.VaultBackup{}, err
	}
	var legacyFiles []models.File
	if err := db.Where("vault_id = ? AND storage_key = ''", vault.ID).Find(&legacyFiles).Error; err != nil {
		return models.VaultBackup{}, err
	}
	if err := db.Transaction(func(tx *gorm.DB) error {
		for _, model := range []any{
			&models.VaultMember{}, &models.VaultSetting{}, &models.VaultSyncState{},
			&models.DeviceVault{}, &models.StorageIssue{}, &models.Share{}, &models.Collaboration{}, &models.File{},
		} {
			if err := tx.Where("vault_id = ?", vault.ID).Delete(model).Error; err != nil {
				return err
			}
		}
		return tx.Unscoped().Delete(&models.Vault{}, "id = ?", vault.ID).Error
	}); err != nil {
		return models.VaultBackup{}, err
	}
	if err := os.RemoveAll(filepath.Join(dataDir, "vaults", vault.ID)); err != nil {
		return backup, fmt.Errorf("remove vault files: %w", err)
	}
	for _, file := range legacyFiles {
		if err := os.Remove(filestore.DiskPath(dataDir, file)); err != nil && !os.IsNotExist(err) {
			return backup, fmt.Errorf("remove legacy vault file: %w", err)
		}
	}
	return backup, nil
}

func writeJSON(zw *zip.Writer, name string, value any) error {
	w, err := zw.Create(name)
	if err != nil {
		return err
	}
	return json.NewEncoder(w).Encode(value)
}

func addFile(zw *zip.Writer, source, archiveName string) error {
	f, err := os.Open(source)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	defer f.Close()
	w, err := zw.Create(archiveName)
	if err != nil {
		return err
	}
	_, err = io.Copy(w, f)
	return err
}
