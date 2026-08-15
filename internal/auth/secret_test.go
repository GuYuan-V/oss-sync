package auth

import (
	"testing"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"

	"github.com/oss/oss-server/internal/config"
	"github.com/oss/oss-server/internal/database"
	"github.com/oss/oss-server/internal/models"
)

func TestEnsureDatabaseJWTSecretPersistsStableRandomValue(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatal(err)
	}
	if err := database.AutoMigrate(db); err != nil {
		t.Fatal(err)
	}
	first := &config.Config{Auth: config.AuthConfig{AllowAnonymousRegistration: true, JWTSecret: "checked-in-secret"}}
	if err := EnsureDatabaseJWTSecret(db, first); err != nil {
		t.Fatal(err)
	}
	if first.Auth.JWTSecret == "" || first.Auth.JWTSecret == "checked-in-secret" {
		t.Fatalf("secret was not database-generated: %q", first.Auth.JWTSecret)
	}
	second := &config.Config{Auth: config.AuthConfig{JWTSecret: "another-config-secret"}}
	if err := EnsureDatabaseJWTSecret(db, second); err != nil {
		t.Fatal(err)
	}
	if second.Auth.JWTSecret != first.Auth.JWTSecret {
		t.Fatal("database JWT secret changed after restart")
	}
	var stored models.SystemSetting
	if err := db.First(&stored, systemSettingsID).Error; err != nil {
		t.Fatal(err)
	}
	if stored.JWTSecret != first.Auth.JWTSecret {
		t.Fatal("database JWT secret was not persisted")
	}
}
