package collaboration

import (
	"testing"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"

	"github.com/helantianshen/oss-sync/internal/models"
)

// newHistoryViewDB 建含 User/Vault/File/Collaboration 的临时库，返回 owner 与被邀请协作者的 ID
func newHistoryViewDB(t *testing.T) (*gorm.DB, uint, uint, uint) {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(t.TempDir()+"/history-view.db"), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&models.User{}, &models.Vault{}, &models.VaultMember{}, &models.File{}, &models.Collaboration{}); err != nil {
		t.Fatal(err)
	}
	// Windows 下 TempDir 清理要求文件句柄已释放
	t.Cleanup(func() {
		if sqlDB, err := db.DB(); err == nil {
			_ = sqlDB.Close()
		}
	})
	owner := models.User{ID: 1, Username: "owner", Role: "user", PasswordHash: "x"}
	guest := models.User{ID: 2, Username: "guest", Role: "user", PasswordHash: "x"}
	if err := db.Create(&owner).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&guest).Error; err != nil {
		t.Fatal(err)
	}
	vault := models.Vault{ID: "v1", Name: "v1", OwnerID: owner.ID}
	if err := db.Create(&vault).Error; err != nil {
		t.Fatal(err)
	}
	file := models.File{ID: 7, UserID: owner.ID, VaultID: vault.ID, Path: "Doc.md", Type: "markdown"}
	if err := db.Create(&file).Error; err != nil {
		t.Fatal(err)
	}
	return db, owner.ID, guest.ID, file.ID
}

// SetHistoryView 由 owner 开启与关闭；协作者本人与非管理员成员无权修改
func TestSetHistoryViewPermission(t *testing.T) {
	db, ownerID, guestID, _ := newHistoryViewDB(t)
	svc := New(db)

	row, err := svc.Invite(ownerID, "v1", "Doc.md", "guest", false)
	if err != nil {
		t.Fatal(err)
	}
	if row.AllowHistoryView {
		t.Fatal("邀请时未授权，初始值应为 false")
	}

	if err := svc.SetHistoryView(ownerID, row.ID, true); err != nil {
		t.Fatal(err)
	}
	var updated models.Collaboration
	if err := db.First(&updated, row.ID).Error; err != nil {
		t.Fatal(err)
	}
	if !updated.AllowHistoryView {
		t.Fatal("owner 开启后应持久化为 true")
	}

	if err := svc.SetHistoryView(ownerID, row.ID, false); err != nil {
		t.Fatal(err)
	}
	if err := db.First(&updated, row.ID).Error; err != nil {
		t.Fatal(err)
	}
	if updated.AllowHistoryView {
		t.Fatal("owner 关闭后应回落到 false")
	}

	// 协作者本人不是 owner 也不是 manager，必须被拒绝
	if err := svc.SetHistoryView(guestID, row.ID, true); err == nil {
		t.Fatal("协作者不应能修改自己的历史查看权限")
	}
	if err := db.First(&updated, row.ID).Error; err != nil {
		t.Fatal(err)
	}
	if updated.AllowHistoryView {
		t.Fatal("被拒绝的修改不得落库")
	}
}

// manager 角色可代 owner 调整权限，非成员普通用户不可
func TestSetHistoryViewManagerAllowed(t *testing.T) {
	db, ownerID, _, _ := newHistoryViewDB(t)
	manager := models.User{ID: 3, Username: "manager", Role: "user", PasswordHash: "x"}
	stranger := models.User{ID: 4, Username: "stranger", Role: "user", PasswordHash: "x"}
	if err := db.Create(&manager).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&stranger).Error; err != nil {
		t.Fatal(err)
	}
	member := models.VaultMember{VaultID: "v1", UserID: manager.ID, Role: "manager"}
	if err := db.Create(&member).Error; err != nil {
		t.Fatal(err)
	}
	svc := New(db)
	row, err := svc.Invite(ownerID, "v1", "Doc.md", "manager", true)
	if err != nil {
		t.Fatal(err)
	}
	if !row.AllowHistoryView {
		t.Fatal("邀请时授权应记录为 true")
	}

	if err := svc.SetHistoryView(manager.ID, row.ID, false); err != nil {
		t.Fatalf("manager 应可调整权限: %v", err)
	}
	if err := svc.SetHistoryView(stranger.ID, row.ID, true); err == nil {
		t.Fatal("非成员不应能调整权限")
	}
}
