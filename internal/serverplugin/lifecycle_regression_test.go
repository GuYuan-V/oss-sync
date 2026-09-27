package serverplugin

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gorm.io/gorm"
)

func installLifecycleFixture(t *testing.T, m *Manager, mf Manifest) {
	t.Helper()
	raw, err := json.Marshal(mf)
	if err != nil {
		t.Fatal(err)
	}
	data := makePackageArchive(t, map[string][]byte{"manifest.json": raw, "plugin.wasm": testResponseModule(t, PluginResponse{Status: 200})})
	if _, err = m.Install(t.Context(), bytes.NewReader(data), int64(len(data))); err != nil {
		t.Fatal(err)
	}
}
func TestEnableRunsDeclaredMigrations(t *testing.T) {
	m := reviewManager(t)
	mf := Manifest{ID: "review-migration", Name: "Review", Version: "1.0.0", APIVersion: 1, Routes: []RouteSpec{{Method: "GET", Path: "/hello", Public: true}}, Registration: &ExtensionRegistration{Migrations: []RegisteredMigration{{ID: "init", Statements: []string{"CREATE TABLE review_required_table (id INTEGER)"}}}}}
	installLifecycleFixture(t, m, mf)
	if err := m.Enable(t.Context(), mf.ID); err != nil {
		t.Fatal(err)
	}
	if !m.db.Migrator().HasTable("review_required_table") {
		t.Fatal("Enable returned success but migration table is missing")
	}
}
func TestThemeResourcePreservesUnownedDirectory(t *testing.T) {
	m := reviewManager(t)
	target := filepath.Join(filepath.Dir(m.root), "themes", "review-theme--clean")
	if err := os.MkdirAll(target, 0750); err != nil {
		t.Fatal(err)
	}
	original := filepath.Join(target, "user-original.txt")
	if err := os.WriteFile(original, []byte("user content"), 0640); err != nil {
		t.Fatal(err)
	}
	err := m.materializeThemeResource("review-theme", ThemeResource{ID: "clean", Name: "Clean", Path: "theme"}, map[string][]byte{"theme/template.html": []byte("new template")}, false)
	if err == nil {
		t.Fatal("expected an ownership conflict")
	}
	if _, statErr := os.Stat(original); statErr != nil {
		t.Fatalf("unowned file removed: write error=%v, stat=%v", err, statErr)
	}
}
func TestEditorRollbackRestoresMetadata(t *testing.T) {
	m := reviewManager(t)
	mf := Manifest{ID: "review-editor", Name: "Review", Version: "1.0.0", APIVersion: 1, Routes: []RouteSpec{{Method: "GET", Path: "/hello", Public: true}}}
	installLifecycleFixture(t, m, mf)
	if err := m.Enable(t.Context(), mf.ID); err != nil {
		t.Fatal(err)
	}
	mf.Registration = &ExtensionRegistration{Dependencies: []RegisteredDependency{{PluginID: "missing-dependency"}}}
	raw, err := json.Marshal(mf)
	if err != nil {
		t.Fatal(err)
	}
	if err = m.SaveTextFile(t.Context(), mf.ID, "manifest.json", string(raw)); err == nil {
		t.Fatal("expected missing dependency error")
	}
	record, err := m.record(mf.ID)
	if err != nil {
		t.Fatal(err)
	}
	restored, err := readInstalledPackage(m.root, mf.ID)
	if err != nil {
		t.Fatal(err)
	}
	if err = verifyInstalledPackage(restored, record); err != nil {
		t.Fatalf("rollback left restored file incompatible with DB and plugin disabled=%v: %v", !record.Enabled, err)
	}
	if !record.Enabled {
		t.Fatal("original plugin was not re-enabled")
	}
}

func TestEnableRejectsFailedMigration(t *testing.T) {
	m := reviewManager(t)
	mf := Manifest{ID: "failed-migration", Name: "Failed", Version: "1.0.0", APIVersion: 1, Routes: []RouteSpec{{Method: "GET", Path: "/hello", Public: true}}, Registration: &ExtensionRegistration{Migrations: []RegisteredMigration{{ID: "init", Statements: []string{"CREATE TABLE partial_table (id INTEGER)", "INVALID SQL"}}}}}
	installLifecycleFixture(t, m, mf)
	if err := m.Enable(t.Context(), mf.ID); err == nil {
		t.Fatal("expected migration error")
	}
	record, err := m.record(mf.ID)
	if err != nil {
		t.Fatal(err)
	}
	if record.Enabled || m.modules[mf.ID] != nil || m.db.Migrator().HasTable("partial_table") {
		t.Fatal("failed migration left enabled plugin or partial schema")
	}
}

func TestThemeResourceInstallsAndReplacesOwnedDirectory(t *testing.T) {
	for _, console := range []bool{false, true} {
		m := reviewManager(t)
		resource := ThemeResource{ID: "clean", Name: "Clean", Path: "theme"}
		for _, content := range []string{"first", "second"} {
			if err := m.materializeThemeResource("review-theme", resource, map[string][]byte{"theme/test.txt": []byte(content)}, console); err != nil {
				t.Fatal(err)
			}
		}
		root := "themes"
		if console {
			root = "console-themes"
		}
		data, err := os.ReadFile(filepath.Join(filepath.Dir(m.root), root, resource.Key("review-theme"), "test.txt"))
		if err != nil || string(data) != "second" {
			t.Fatalf("owned replacement failed: %q %v", data, err)
		}
	}
}

func TestEditorRollbackSurvivesCanceledRequest(t *testing.T) {
	m := reviewManager(t)
	mf := Manifest{ID: "cancel-editor", Name: "Original", Version: "1.0.0", APIVersion: 1, Routes: []RouteSpec{{Method: "GET", Path: "/hello", Public: true}}}
	installLifecycleFixture(t, m, mf)
	if err := m.Enable(t.Context(), mf.ID); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	if err := m.db.Callback().Update().After("gorm:update").Register("review:cancel-editor", func(tx *gorm.DB) {
		if changes, ok := tx.Statement.Dest.(map[string]any); ok && changes["name"] == "Edited" {
			cancel()
		}
	}); err != nil {
		t.Fatal(err)
	}
	mf.Name = "Edited"
	raw, err := json.Marshal(mf)
	if err != nil {
		t.Fatal(err)
	}
	if err = m.SaveTextFile(ctx, mf.ID, "manifest.json", string(raw)); err == nil {
		t.Fatal("expected cancellation")
	}
	record, err := m.record(mf.ID)
	if err != nil {
		t.Fatal(err)
	}
	restored, err := readInstalledPackage(m.root, mf.ID)
	if err != nil {
		t.Fatal(err)
	}
	if err = verifyInstalledPackage(restored, record); err != nil {
		t.Fatal(err)
	}
	if !record.Enabled || record.Name != "Original" || m.modules[mf.ID] == nil {
		t.Fatal("canceled edit failed to recover original plugin")
	}
}

func TestEditorReportsRecoveryFailure(t *testing.T) {
	m := reviewManager(t)
	mf := Manifest{ID: "recovery-editor", Name: "Original", Version: "1.0.0", APIVersion: 1, Routes: []RouteSpec{{Method: "GET", Path: "/hello", Public: true}}}
	installLifecycleFixture(t, m, mf)
	if err := m.Enable(t.Context(), mf.ID); err != nil {
		t.Fatal(err)
	}
	if err := m.db.Callback().Update().Before("gorm:update").Register("review:fail-restore", func(tx *gorm.DB) {
		if tx.Statement.ReflectValue.IsValid() && tx.Statement.Schema != nil && tx.Statement.Schema.Table == "server_plugins" {
			if _, ok := tx.Statement.Dest.(map[string]any); !ok {
				tx.AddError(context.DeadlineExceeded)
			}
		}
	}); err != nil {
		t.Fatal(err)
	}
	err := m.SaveTextFile(t.Context(), mf.ID, "manifest.json", "invalid json")
	if err == nil || !strings.Contains(err.Error(), "restore plugin metadata") {
		t.Fatalf("missing recovery error: %v", err)
	}
}
