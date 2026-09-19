package serverplugin

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"testing"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"

	"github.com/helantianshen/oss-sync/internal/blog"
	"github.com/helantianshen/oss-sync/internal/models"
)

func TestManagerApplyHookTransformsContent(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(t.TempDir()+"/hooks.db"), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatal(err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })
	if err := db.AutoMigrate(&models.ServerPlugin{}, &models.VaultPluginSetting{}); err != nil {
		t.Fatal(err)
	}
	manager, err := NewManager(t.Context(), db, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = manager.Close(t.Context()) })
	manifest := Manifest{
		ID: "filter-plugin", Name: "Filter plugin", Version: "1.0.0", APIVersion: CurrentAPIVersion,
		Routes: []RouteSpec{{Method: "GET", Path: "/hello", Public: true}},
		Hooks:  []HookSpec{{Name: "blog.content"}},
	}
	manifestJSON, err := json.Marshal(manifest)
	if err != nil {
		t.Fatal(err)
	}
	response := PluginResponse{Status: 200, BodyBase64: base64.StdEncoding.EncodeToString([]byte("filtered content"))}
	archive := makePackageArchive(t, map[string][]byte{
		"manifest.json": manifestJSON,
		"plugin.wasm":   makeWasmModuleWithAlloc(mustJSONBytes(t, response), 256, 1024),
	})
	if _, err := manager.Install(t.Context(), bytes.NewReader(archive), int64(len(archive))); err != nil {
		t.Fatal(err)
	}
	if err := manager.Enable(t.Context(), manifest.ID); err != nil {
		t.Fatal(err)
	}
	got, err := manager.ApplyHook(t.Context(), "blog.content", blog.PluginHookPayload{VaultID: "vault-1", Content: "original"})
	if err != nil {
		t.Fatal(err)
	}
	if got != "filtered content" {
		t.Fatalf("ApplyHook() = %q, want filtered content", got)
	}
}

func mustJSONBytes(t *testing.T, value PluginResponse) []byte {
	t.Helper()
	raw, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

func TestValidateManifestRejectsUnknownHook(t *testing.T) {
	manifest := Manifest{
		ID: "hook-plugin", Name: "Hook plugin", Version: "1.0.0", APIVersion: CurrentAPIVersion,
		Routes: []RouteSpec{{Method: "GET", Path: "/hello", Public: true}},
		Hooks:  []HookSpec{{Name: "unknown"}},
	}
	if err := ValidateManifest(manifest); err == nil {
		t.Fatal("ValidateManifest() error = nil, want unknown hook rejection")
	}
}
