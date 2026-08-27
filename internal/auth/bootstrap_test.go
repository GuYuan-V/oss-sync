package auth_test

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"

	"github.com/oss/oss-server/internal/auth"
	"github.com/oss/oss-server/internal/config"
	"github.com/oss/oss-server/internal/database"
)

func TestRegistrationSettingPersistsAdminChoice(t *testing.T) {
	db := newAuthTestDB(t)
	if err := auth.EnsureRegistrationSetting(db, true); err != nil {
		t.Fatal(err)
	}
	if err := auth.SetRegistrationEnabled(db, false); err != nil {
		t.Fatal(err)
	}

	enabled, err := auth.RegistrationEnabled(db, true)
	if err != nil {
		t.Fatal(err)
	}
	if enabled {
		t.Fatal("saved registration choice was overwritten by startup default")
	}
}

func TestRegistrationSettingHonorsClosedSeed(t *testing.T) {
	db := newAuthTestDB(t)
	if err := auth.EnsureRegistrationSetting(db, false); err != nil {
		t.Fatal(err)
	}
	enabled, err := auth.RegistrationEnabled(db, true)
	if err != nil {
		t.Fatal(err)
	}
	if enabled {
		t.Fatal("new setting did not honor the closed seed")
	}
}

func TestValidateAccountInputRejectsBcryptOverflow(t *testing.T) {
	err := auth.ValidateAccountInput("valid-user", strings.Repeat("密", 25))
	if err == nil || !strings.Contains(err.Error(), "72") {
		t.Fatalf("expected bcrypt byte limit error, got %v", err)
	}
}

func TestChangePasswordInvalidatesOldToken(t *testing.T) {
	db := newAuthTestDB(t)
	user, err := auth.CreateAccount(db, "change-me", "password123", "user")
	if err != nil {
		t.Fatal(err)
	}
	cfg := &config.Config{Auth: config.AuthConfig{JWTSecret: "test-secret", JWTTTLHours: 1}}
	oldToken, _, err := auth.IssueToken(cfg, *user)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := auth.AuthenticateToken(db, cfg, oldToken); err != nil {
		t.Fatalf("old token should be valid before password change: %v", err)
	}

	if err := auth.ChangePassword(db, user.ID, "password123", "new-password-123"); err != nil {
		t.Fatalf("change password: %v", err)
	}
	if _, err := auth.AuthenticateToken(db, cfg, oldToken); err == nil {
		t.Fatal("old token remained valid after password change")
	}
	// 旧密码不能再登录。
	if _, err := auth.AuthenticateCredentials(db, "change-me", "password123"); err == nil {
		t.Fatal("old password still authenticates")
	}
	if _, err := auth.AuthenticateCredentials(db, "change-me", "new-password-123"); err != nil {
		t.Fatalf("new password should authenticate: %v", err)
	}
}

func newAuthTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "auth.db")), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if sqlDB, err := db.DB(); err == nil {
		t.Cleanup(func() { _ = sqlDB.Close() })
	}
	if err := database.AutoMigrate(db); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	return db
}
