package auth_test

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"

	"github.com/oss/oss-server/internal/auth"
	"github.com/oss/oss-server/internal/config"
	"github.com/oss/oss-server/internal/database"
	"github.com/oss/oss-server/internal/models"
)

func TestEnsureBootstrapAdminFromEnvironment(t *testing.T) {
	db := newAuthTestDB(t)
	t.Setenv("OSS_ADMIN_PASSWORD", "initial-password-123")
	cfg := &config.Config{
		Auth: config.AuthConfig{
			JWTSecret:              "test-secret",
			JWTTTLHours:            1,
			BootstrapAdminUsername: "console-admin",
		},
	}

	created, err := auth.EnsureBootstrapAdmin(db, cfg, nil, &bytes.Buffer{})
	if err != nil {
		t.Fatalf("bootstrap admin: %v", err)
	}
	if !created {
		t.Fatal("expected a new administrator")
	}

	user, err := auth.AuthenticateCredentials(db, "console-admin", "initial-password-123")
	if err != nil {
		t.Fatalf("authenticate bootstrapped admin: %v", err)
	}
	if user.Role != "admin" {
		t.Fatalf("role = %q, want admin", user.Role)
	}
	var vaultCount int64
	if err := db.Model(&models.Vault{}).Where("owner_id = ?", user.ID).Count(&vaultCount).Error; err != nil {
		t.Fatal(err)
	}
	if vaultCount != 0 {
		t.Fatalf("bootstrap must not create a vault: count = %d", vaultCount)
	}

	t.Setenv("OSS_ADMIN_PASSWORD", "")
	created, err = auth.EnsureBootstrapAdmin(db, cfg, nil, &bytes.Buffer{})
	if err != nil {
		t.Fatalf("second bootstrap: %v", err)
	}
	if created {
		t.Fatal("existing administrator must not be recreated")
	}
}

func TestEnsureBootstrapAdminRequiresSecretWithoutTerminal(t *testing.T) {
	db := newAuthTestDB(t)
	t.Setenv("OSS_ADMIN_PASSWORD", "")
	cfg := &config.Config{
		Auth: config.AuthConfig{
			JWTSecret:              "test-secret",
			JWTTTLHours:            1,
			BootstrapAdminUsername: "admin",
		},
	}
	input, err := os.Open(os.DevNull)
	if err != nil {
		t.Fatal(err)
	}
	defer input.Close()

	created, err := auth.EnsureBootstrapAdmin(db, cfg, input, &bytes.Buffer{})
	if err == nil || !strings.Contains(err.Error(), "OSS_ADMIN_PASSWORD") {
		t.Fatalf("expected non-interactive password guidance, got created=%v err=%v", created, err)
	}
}

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

func newAuthTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "auth.db")), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := database.AutoMigrate(db); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	return db
}
