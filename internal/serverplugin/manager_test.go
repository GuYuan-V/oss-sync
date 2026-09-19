package serverplugin

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"

	"github.com/helantianshen/oss-sync/internal/models"
)

func TestPluginManagerPersistsInstallEnableDisableLifecycle(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(t.TempDir()+"/plugins.db"), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatal(err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := sqlDB.Close(); err != nil {
			t.Errorf("close sqlite: %v", err)
		}
	})
	if err := db.AutoMigrate(&models.ServerPlugin{}); err != nil {
		t.Fatal(err)
	}

	manager, err := NewManager(t.Context(), db, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := manager.Close(t.Context()); err != nil {
			t.Errorf("close plugin manager: %v", err)
		}
	})

	wasm := testResponseModule(t, PluginResponse{Status: 200})
	archiveBytes := makePackageArchive(t, map[string][]byte{
		"manifest.json": []byte(validManifestJSON),
		"plugin.wasm":   wasm,
	})
	info, err := manager.Install(t.Context(), bytes.NewReader(archiveBytes), int64(len(archiveBytes)))
	if err != nil {
		t.Fatalf("Install() error = %v", err)
	}
	if info.ID != "hello-world" || info.Enabled {
		t.Fatalf("installed info = %+v", info)
	}

	if err := manager.Enable(t.Context(), info.ID); err != nil {
		t.Fatalf("Enable() error = %v", err)
	}
	if err := manager.Disable(t.Context(), info.ID); err != nil {
		t.Fatalf("Disable() error = %v", err)
	}
	list, err := manager.List()
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}
	if len(list) != 1 || list[0].ID != info.ID || list[0].Enabled {
		t.Fatalf("List() = %+v", list)
	}
}

func TestPluginManagerLoadsEnabledPluginAfterRestart(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(t.TempDir()+"/plugins-restart.db"), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatal(err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := sqlDB.Close(); err != nil {
			t.Errorf("close sqlite: %v", err)
		}
	})
	if err := db.AutoMigrate(&models.ServerPlugin{}); err != nil {
		t.Fatal(err)
	}
	dataDir := t.TempDir()
	first, err := NewManager(t.Context(), db, dataDir)
	if err != nil {
		t.Fatal(err)
	}
	wasm := testResponseModule(t, PluginResponse{Status: 200})
	archiveBytes := makePackageArchive(t, map[string][]byte{
		"manifest.json": []byte(validManifestJSON),
		"plugin.wasm":   wasm,
	})
	if _, err := first.Install(t.Context(), bytes.NewReader(archiveBytes), int64(len(archiveBytes))); err != nil {
		t.Fatal(err)
	}
	if err := first.Enable(t.Context(), "hello-world"); err != nil {
		t.Fatal(err)
	}
	if err := first.Close(t.Context()); err != nil {
		t.Fatal(err)
	}

	second, err := NewManager(t.Context(), db, dataDir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := second.Close(t.Context()); err != nil {
			t.Errorf("close restarted plugin manager: %v", err)
		}
	})
	if err := second.LoadEnabled(t.Context()); err != nil {
		t.Fatalf("LoadEnabled() error = %v", err)
	}
	second.mu.RLock()
	loaded := second.modules["hello-world"] != nil
	second.mu.RUnlock()
	if !loaded {
		t.Fatal("enabled plugin was not reloaded")
	}
}

func TestPluginManagerRejectsTamperedInstalledManifest(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(t.TempDir()+"/plugins-tamper.db"), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatal(err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := sqlDB.Close(); err != nil {
			t.Errorf("close sqlite: %v", err)
		}
	})
	if err := db.AutoMigrate(&models.ServerPlugin{}); err != nil {
		t.Fatal(err)
	}
	dataDir := t.TempDir()
	manager, err := NewManager(t.Context(), db, dataDir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := manager.Close(t.Context()); err != nil {
			t.Errorf("close plugin manager: %v", err)
		}
	})
	wasm := testResponseModule(t, PluginResponse{Status: 200})
	archiveBytes := makePackageArchive(t, map[string][]byte{
		"manifest.json": []byte(validManifestJSON),
		"plugin.wasm":   wasm,
	})
	if _, err := manager.Install(t.Context(), bytes.NewReader(archiveBytes), int64(len(archiveBytes))); err != nil {
		t.Fatal(err)
	}
	if _, err := manager.InstallOrReuse(t.Context(), bytes.NewReader(archiveBytes), int64(len(archiveBytes))); err != nil {
		t.Fatalf("InstallOrReuse() identical package error = %v", err)
	}
	tampered := `{"id":"hello-world","name":"Tampered","version":"1.0.0","api_version":1,"routes":[{"method":"GET","path":"/admin","public":true}]}`
	if err := os.WriteFile(filepath.Join(dataDir, "plugins", "hello-world", "manifest.json"), []byte(tampered), 0o600); err != nil {
		t.Fatal(err)
	}

	err = manager.Enable(t.Context(), "hello-world")
	if !errors.Is(err, ErrInvalidPackage) {
		t.Fatalf("Enable() error = %v, want ErrInvalidPackage", err)
	}
	plugins, err := manager.List()
	if err != nil {
		t.Fatal(err)
	}
	if len(plugins) != 1 || plugins[0].LastError == "" || plugins[0].Enabled {
		t.Fatalf("tampered plugin state = %+v, want disabled plugin with last error", plugins)
	}
}

func TestPluginManagerRestoresFilesWhenDeleteRecordFails(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(t.TempDir()+"/plugins-delete.db"), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatal(err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := sqlDB.Close(); err != nil {
			t.Errorf("close sqlite: %v", err)
		}
	})
	if err := db.AutoMigrate(&models.ServerPlugin{}); err != nil {
		t.Fatal(err)
	}
	if err := db.Exec(`CREATE TRIGGER reject_server_plugin_delete BEFORE DELETE ON server_plugins BEGIN SELECT RAISE(ABORT, 'delete blocked for test'); END`).Error; err != nil {
		t.Fatal(err)
	}

	dataDir := t.TempDir()
	manager, err := NewManager(t.Context(), db, dataDir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := manager.Close(t.Context()); err != nil {
			t.Errorf("close plugin manager: %v", err)
		}
	})
	wasm := testResponseModule(t, PluginResponse{Status: 200})
	archiveBytes := makePackageArchive(t, map[string][]byte{
		"manifest.json": []byte(validManifestJSON),
		"plugin.wasm":   wasm,
	})
	if _, err := manager.Install(t.Context(), bytes.NewReader(archiveBytes), int64(len(archiveBytes))); err != nil {
		t.Fatal(err)
	}

	err = manager.Delete("hello-world")
	if err == nil {
		t.Fatal("Delete() error = nil, want database delete failure")
	}
	pluginDir := filepath.Join(dataDir, "plugins", "hello-world")
	if _, err := os.Stat(pluginDir); err != nil {
		t.Fatalf("plugin directory after failed delete: %v", err)
	}
	var record models.ServerPlugin
	if err := db.Where("id = ?", "hello-world").First(&record).Error; err != nil {
		t.Fatalf("plugin record after failed delete: %v", err)
	}
}
