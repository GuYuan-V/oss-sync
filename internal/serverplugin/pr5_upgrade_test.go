package serverplugin

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/helantianshen/oss-sync/internal/models"
	"gorm.io/gorm"
)

type recoveryScheduler struct{ tasks map[string]bool }

func (s *recoveryScheduler) AddPluginTask(_, name, _ string, _ func()) error {
	if name == "fail-task" {
		return errors.New("test task registration failed")
	}
	s.tasks[name] = true
	return nil
}
func (s *recoveryScheduler) RemovePluginTasks(string) { s.tasks = map[string]bool{} }

func TestUpgradeRestoresOldPackageAndTasksAtEveryFailurePhase(t *testing.T) {
	for _, phase := range []string{"dependency", "migration", "upgrade-hook", "activate-hook", "task-registration", "metadata-write", "canceled-request"} {
		t.Run(phase, func(t *testing.T) {
			m := reviewManager(t)
			scheduler := &recoveryScheduler{tasks: map[string]bool{}}
			if err := m.RegisterTasks(scheduler); err != nil {
				t.Fatal(err)
			}
			mf := Manifest{ID: "restore-test", Name: "Restore test", Version: "1.0.0", APIVersion: 1, Routes: []RouteSpec{{Method: "GET", Path: "/hello", Public: true}}, Registration: &ExtensionRegistration{
				Tasks: []RegisteredTask{{Name: "old-task", Schedule: "@hourly", Callback: "old-callback"}},
			}}
			pack := func(manifest Manifest, status int) []byte {
				raw, err := json.Marshal(manifest)
				if err != nil {
					t.Fatal(err)
				}
				return makePackageArchive(t, map[string][]byte{"manifest.json": raw, "plugin.wasm": testResponseModule(t, PluginResponse{Status: status})})
			}
			initial := pack(mf, 200)
			if _, err := m.Install(t.Context(), bytes.NewReader(initial), int64(len(initial))); err != nil {
				t.Fatal(err)
			}
			if err := m.Enable(t.Context(), mf.ID); err != nil {
				t.Fatal(err)
			}
			original, err := m.record(mf.ID)
			if err != nil {
				t.Fatal(err)
			}
			mf.Version = "2.0.0"
			registration := &ExtensionRegistration{Tasks: []RegisteredTask{{Name: "new-task", Schedule: "@hourly"}}}
			status := 200
			switch phase {
			case "dependency":
				registration.Dependencies = []RegisteredDependency{{PluginID: "missing-dependency"}}
			case "migration":
				registration.Migrations = []RegisteredMigration{
					{ID: "first", Statements: []string{"CREATE TABLE pr5_partial_migration (id INTEGER)"}},
					{ID: "fail", Statements: []string{"THIS IS NOT SQL"}},
				}
			case "upgrade-hook":
				registration.Lifecycle.Upgrade = "upgrade"
				status = 500
			case "activate-hook":
				registration.Lifecycle.Activate = "activate"
				status = 500
			case "task-registration":
				registration.Tasks = append(registration.Tasks, RegisteredTask{Name: "fail-task", Schedule: "@hourly"})
			}
			mf.Registration = registration
			next := pack(mf, status)
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			if phase == "metadata-write" {
				err := m.db.Callback().Update().Before("gorm:update").Register("pr5:fail-metadata", func(tx *gorm.DB) {
					if changes, ok := tx.Statement.Dest.(map[string]any); ok && changes["version"] == "2.0.0" {
						tx.AddError(errors.New("test metadata write failure"))
					}
				})
				if err != nil {
					t.Fatal(err)
				}
			}
			if phase == "canceled-request" {
				err := m.db.Callback().Update().After("gorm:update").Register("pr5:cancel-request", func(tx *gorm.DB) {
					if changes, ok := tx.Statement.Dest.(map[string]any); ok && changes["enabled"] == false {
						cancel()
					}
				})
				if err != nil {
					t.Fatal(err)
				}
			}
			if _, err := m.Upgrade(ctx, bytes.NewReader(next), int64(len(next))); err == nil {
				t.Fatal("expected upgrade failure")
			}
			restored, err := m.record(mf.ID)
			if err != nil {
				t.Fatal(err)
			}
			if !restored.Enabled || restored.Version != original.Version || restored.ManifestHash != original.ManifestHash || restored.WasmHash != original.WasmHash {
				t.Fatalf("metadata was not restored: %+v", restored)
			}
			installed, err := readInstalledPackage(m.root, mf.ID)
			if err != nil {
				t.Fatal(err)
			}
			if err := verifyInstalledPackage(installed, original); err != nil {
				t.Fatalf("old files not restored: %v", err)
			}
			if !scheduler.tasks["old-task"] || len(scheduler.tasks) != 1 {
				t.Fatalf("tasks not restored: %#v", scheduler.tasks)
			}
			response, err := m.invokeCallback(context.Background(), mf.ID, "old-callback", PluginRequest{Method: "TASK", Path: "/old"})
			if err != nil || response.Status != 200 {
				t.Fatalf("old runtime cannot serve: %v %+v", err, response)
			}
			if phase == "migration" {
				if m.db.Migrator().HasTable("pr5_partial_migration") {
					t.Fatal("partial migration batch was committed")
				}
				var count int64
				mustReview(t, m.db.Model(&models.ServerPluginMigration{}).Where("plugin_id = ?", mf.ID).Count(&count))
				if count != 0 {
					t.Fatal("failed migration batch left applied markers")
				}
			}
		})
	}
}
