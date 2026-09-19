package serverplugin

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"

	"github.com/helantianshen/oss-sync/internal/models"
)

func TestHostRPCProvidesDatabaseAndCoreModels(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(t.TempDir()+"/host.db"), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatal(err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })
	if err := db.AutoMigrate(&models.User{}); err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&models.User{Username: "rpc-user", PasswordHash: "hash"}).Error; err != nil {
		t.Fatal(err)
	}
	manager := &Manager{db: db}

	params := map[string]json.RawMessage{
		"query": json.RawMessage(`"SELECT username FROM users WHERE username = ?"`),
		"args":  json.RawMessage(`["rpc-user"]`),
	}
	rows, err := manager.hostCall(context.Background(), "plugin", "db.query", params)
	if err != nil {
		t.Fatalf("db.query() error = %v", err)
	}
	if len(rows.([]map[string]any)) != 1 || rows.([]map[string]any)[0]["username"] != "rpc-user" {
		t.Fatalf("db.query() = %#v", rows)
	}

	modelsResult, err := manager.hostCall(context.Background(), "plugin", "host.models", nil)
	if err != nil {
		t.Fatalf("host.models() error = %v", err)
	}
	if len(modelsResult.([]string)) == 0 {
		t.Fatal("host.models() returned no core models")
	}
}

func TestPluginMigrationsAreIdempotent(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(t.TempDir()+"/migration.db"), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatal(err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })
	if err := db.AutoMigrate(&models.ServerPluginMigration{}); err != nil {
		t.Fatal(err)
	}
	manager := &Manager{db: db}
	migrations := []RegisteredMigration{{
		ID:         "create_plugin_table",
		Statements: []string{"CREATE TABLE plugin_rpc_values (value TEXT NOT NULL)"},
	}}
	if err := manager.applyMigrations(context.Background(), "plugin", migrations); err != nil {
		t.Fatalf("first migration error = %v", err)
	}
	if err := manager.applyMigrations(context.Background(), "plugin", migrations); err != nil {
		t.Fatalf("second migration error = %v", err)
	}
	var count int64
	if err := db.Model(&models.ServerPluginMigration{}).Where("plugin_id = ?", "plugin").Count(&count).Error; err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("migration records = %d, want 1", count)
	}
}

func TestTypedHostServicesCreateAndReadVault(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(t.TempDir()+"/services.db"), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatal(err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })
	if err := db.AutoMigrate(&models.Vault{}, &models.VaultSetting{}, &models.VaultSyncState{}); err != nil {
		t.Fatal(err)
	}
	manager := &Manager{db: db}
	created, err := manager.hostVaultCreate(context.Background(), map[string]json.RawMessage{
		"name":        json.RawMessage(`"Plugin Vault"`),
		"description": json.RawMessage(`"owned by extension"`),
	})
	if err != nil {
		t.Fatal(err)
	}
	if created.ID == "" || created.Name != "Plugin Vault" {
		t.Fatalf("created vault = %+v", created)
	}
	loaded, err := manager.hostVaultGet(context.Background(), map[string]json.RawMessage{"vault_id": json.RawMessage(fmt.Sprintf("%q", created.ID))})
	if err != nil {
		t.Fatal(err)
	}
	if loaded.ID != created.ID || loaded.Description != created.Description {
		t.Fatalf("loaded vault = %+v", loaded)
	}
}

func TestRegistrationAcceptsArbitraryHostExtensions(t *testing.T) {
	registration := ExtensionRegistration{
		Hooks:      []RegisteredHook{{Name: "orders.before_save", Kind: "filter", Callback: "orders.before_save"}},
		Routes:     []RegisteredRoute{{Method: "POST", Path: "/orders/*", Callback: "orders.create", Auth: "admin"}},
		Middleware: []RegisteredMiddleware{{Name: "audit", Stage: "before", PathPrefix: "/api/"}},
		AdminPages: []RegisteredAdminPage{{Slug: "orders", Label: "Orders", Callback: "orders.admin"}},
		Tasks:      []RegisteredTask{{Name: "sync_orders", Schedule: "@hourly", Callback: "orders.sync"}},
		Migrations: []RegisteredMigration{{ID: "orders_v1", Statements: []string{"CREATE TABLE orders (id INTEGER)"}}},
	}
	if err := validateRegistration(registration); err != nil {
		t.Fatalf("validateRegistration() error = %v", err)
	}
}
