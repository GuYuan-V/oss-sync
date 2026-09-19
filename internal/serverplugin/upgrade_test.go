package serverplugin

import (
	"bytes"
	"encoding/json"
	"testing"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"

	"github.com/helantianshen/oss-sync/internal/models"
)

func TestPluginManagerUpgradesInstalledPackage(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(t.TempDir()+"/upgrade.db"), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatal(err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })
	if err := db.AutoMigrate(&models.ServerPlugin{}, &models.ServerPluginMigration{}); err != nil {
		t.Fatal(err)
	}
	manager, err := NewManager(t.Context(), db, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = manager.Close(t.Context()) })
	wasm := testResponseModule(t, PluginResponse{Status: 200})
	manifestV1 := Manifest{ID: "upgrade-world", Name: "Upgrade world", Version: "1.0.0", APIVersion: CurrentAPIVersion, Routes: []RouteSpec{{Method: "GET", Path: "/hello", Public: true}}}
	manifestV2 := manifestV1
	manifestV2.Version = "2.0.0"
	archive := func(manifest Manifest) []byte {
		raw, marshalErr := json.Marshal(manifest)
		if marshalErr != nil {
			t.Fatal(marshalErr)
		}
		return makePackageArchive(t, map[string][]byte{"manifest.json": raw, "plugin.wasm": wasm})
	}
	first := archive(manifestV1)
	if _, err := manager.Install(t.Context(), bytes.NewReader(first), int64(len(first))); err != nil {
		t.Fatal(err)
	}
	second := archive(manifestV2)
	info, err := manager.Upgrade(t.Context(), bytes.NewReader(second), int64(len(second)))
	if err != nil {
		t.Fatal(err)
	}
	if info.Version != "2.0.0" {
		t.Fatalf("upgraded version = %q", info.Version)
	}
}
