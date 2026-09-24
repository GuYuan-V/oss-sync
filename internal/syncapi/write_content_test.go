package syncapi

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"

	"github.com/helantianshen/oss-sync/internal/config"
	"github.com/helantianshen/oss-sync/internal/filestore"
	"github.com/helantianshen/oss-sync/internal/history"
	"github.com/helantianshen/oss-sync/internal/models"
)

func newWriteContentHandler(t *testing.T) (*Handler, string) {
	t.Helper()
	dir := t.TempDir()
	db, err := gorm.Open(sqlite.Open(filepath.Join(dir, "t.db")), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	if sqlDB, err := db.DB(); err == nil {
		t.Cleanup(func() { _ = sqlDB.Close() })
	}
	if err := db.AutoMigrate(&models.User{}, &models.UserSetting{}, &models.SystemSetting{}, &models.Vault{}, &models.File{}, &models.VaultSyncState{}, &models.Collaboration{}, &models.FileHistory{}); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	cfg := &config.Config{
		Storage: config.StorageConfig{DataDir: dir},
		Server:  config.ServerConfig{MaxFileSizeMB: 100},
	}
	return New(db, cfg), dir
}

func TestWriteFileContent_persistsBlobAndAdvancesRevision(t *testing.T) {
	h, dir := newWriteContentHandler(t)
	if err := h.DB.Create(&models.Vault{ID: "v1", OwnerID: 7, Name: "V"}).Error; err != nil {
		t.Fatalf("create vault: %v", err)
	}

	saved, err := h.WriteFileContent(7, "v1", "Notes/a.md", []byte("# Hi"), 0)
	if err != nil {
		t.Fatalf("write: %v", err)
	}
	if saved.Revision <= 0 {
		t.Fatalf("revision not assigned: %d", saved.Revision)
	}
	if saved.Size != int64(len("# Hi")) {
		t.Fatalf("size = %d, want %d", saved.Size, len("# Hi"))
	}

	key := filestore.VaultStorageKey("v1", "Notes/a.md")
	blob, err := os.ReadFile(filepath.Join(dir, filepath.FromSlash(key)))
	if err != nil {
		t.Fatalf("read blob: %v", err)
	}
	if string(blob) != "# Hi" {
		t.Fatalf("blob content = %q, want %q", blob, "# Hi")
	}

	var state models.VaultSyncState
	if err := h.DB.Where("vault_id = ?", "v1").First(&state).Error; err != nil {
		t.Fatalf("load sync state: %v", err)
	}
	if state.HeadRevision != saved.Revision {
		t.Fatalf("head revision %d != saved revision %d", state.HeadRevision, saved.Revision)
	}

	second, err := h.WriteFileContent(7, "v1", "Notes/a.md", []byte("# Hi again"), 0)
	if err != nil {
		t.Fatalf("second write: %v", err)
	}
	if second.Revision <= saved.Revision {
		t.Fatalf("revision did not advance: %d <= %d", second.Revision, saved.Revision)
	}
	if second.ID != saved.ID {
		t.Fatalf("expected in-place update, got new id %d vs %d", second.ID, saved.ID)
	}
}

func TestWriteFileContent_wakesRevisionWaiters(t *testing.T) {
	h, _ := newWriteContentHandler(t)
	if err := h.DB.Create(&models.Vault{ID: "v1", OwnerID: 7, Name: "V"}).Error; err != nil {
		t.Fatalf("create vault: %v", err)
	}
	signal := h.revisionChannel("v1")
	woken := make(chan struct{})
	go func() {
		<-signal
		close(woken)
	}()

	if _, err := h.WriteFileContent(7, "v1", "a.md", []byte("x"), 0); err != nil {
		t.Fatalf("write: %v", err)
	}
	select {
	case <-woken:
	case <-time.After(2 * time.Second):
		t.Fatal("long-poll waiters were not woken by WriteFileContent")
	}
}

func TestWriteFileContent_rejectsIllegalPath(t *testing.T) {
	h, _ := newWriteContentHandler(t)
	if _, err := h.WriteFileContent(7, "v1", "../escape.md", []byte("x"), 0); err == nil {
		t.Fatal("expected error for path outside the vault")
	}
}

func TestWriteFileContent_identicalContentIsNoOp(t *testing.T) {
	h, _ := newWriteContentHandler(t)
	if err := h.DB.Create(&models.Vault{ID: "v1", OwnerID: 7, Name: "V"}).Error; err != nil {
		t.Fatalf("create vault: %v", err)
	}
	first, err := h.WriteFileContent(7, "v1", "note.md", []byte("same"), 0)
	if err != nil {
		t.Fatalf("first write: %v", err)
	}
	second, err := h.WriteFileContent(7, "v1", "note.md", []byte("same"), 0)
	if err != nil {
		t.Fatalf("second write: %v", err)
	}
	if second.Revision != first.Revision {
		t.Fatalf("identical content bumped revision: %d -> %d", first.Revision, second.Revision)
	}
	// 内容未变化不应新增历史：创建记 1 条，重复保存不再新增
	var historyCount int64
	if err := h.DB.Model(&models.FileHistory{}).Where("vault_id = ? AND file_path = ?", "v1", "note.md").Count(&historyCount).Error; err != nil {
		t.Fatalf("count history: %v", err)
	}
	if historyCount != 1 {
		t.Fatalf("history rows = %d, want 1 (create only)", historyCount)
	}
}

func TestWriteFileContent_recordsHistoryOnChange(t *testing.T) {
	h, _ := newWriteContentHandler(t)
	if err := h.DB.Create(&models.Vault{ID: "v1", OwnerID: 7, Name: "V"}).Error; err != nil {
		t.Fatalf("create vault: %v", err)
	}
	if _, err := h.WriteFileContent(7, "v1", "note.md", []byte("v1"), 0); err != nil {
		t.Fatalf("create write: %v", err)
	}
	if _, err := h.WriteFileContent(7, "v1", "note.md", []byte("v2 changed"), 0); err != nil {
		t.Fatalf("modify write: %v", err)
	}
	var rows []models.FileHistory
	if err := h.DB.Where("vault_id = ? AND file_path = ?", "v1", "note.md").Order("version asc").Find(&rows).Error; err != nil {
		t.Fatalf("load history: %v", err)
	}
	if len(rows) != 2 {
		t.Fatalf("history rows = %d, want 2 (create + modify)", len(rows))
	}
	if rows[0].Action != history.ActionCreate || rows[1].Action != history.ActionModify {
		t.Fatalf("history actions = %q,%q, want create,modify", rows[0].Action, rows[1].Action)
	}
}
