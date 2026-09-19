package serverplugin

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"

	"github.com/helantianshen/oss-sync/internal/blog"
	"github.com/helantianshen/oss-sync/internal/config"
	"github.com/helantianshen/oss-sync/internal/models"
)

func TestPluginManagerRunsExecutablePluginThroughLifecycle(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(t.TempDir()+"/executable.db"), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
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

	dataDir := t.TempDir()
	manager, err := NewManager(t.Context(), db, dataDir)
	if err != nil {
		t.Fatal(err)
	}
	manifest := Manifest{
		ID:          "executable-world",
		Name:        "Executable world",
		Version:     "1.0.0",
		APIVersion:  CurrentAPIVersion,
		Runtime:     RuntimeExecutable,
		Entrypoints: map[string]string{"any": "plugin.exe"},
		Routes:      []RouteSpec{{Method: http.MethodGet, Path: "/hello", Public: true}},
		Hooks:       []HookSpec{{Name: "blog.content"}},
	}
	manifestBytes, err := json.Marshal(manifest)
	if err != nil {
		t.Fatal(err)
	}
	archiveBytes := makePackageArchive(t, map[string][]byte{
		"manifest.json": manifestBytes,
		"plugin.exe":    buildExamplePlugin(t),
	})
	info, err := manager.Install(t.Context(), bytes.NewReader(archiveBytes), int64(len(archiveBytes)))
	if err != nil {
		t.Fatalf("Install() error = %v", err)
	}
	if info.Runtime != RuntimeExecutable || info.PayloadSize == 0 {
		t.Fatalf("installed executable info = %+v", info)
	}
	if err := manager.Enable(t.Context(), manifest.ID); err != nil {
		t.Fatalf("Enable() error = %v", err)
	}

	gin.SetMode(gin.TestMode)
	router := gin.New()
	manager.RegisterRoutes(router, &config.Config{})
	request := httptest.NewRequest(http.MethodGet, "/plugins/executable-world/hello", nil)
	record := httptest.NewRecorder()
	router.ServeHTTP(record, request)
	if record.Code != http.StatusOK || record.Body.String() != "hello from SDK" {
		t.Fatalf("executable route response = %d %q", record.Code, record.Body.String())
	}
	dynamicRequest := httptest.NewRequest(http.MethodGet, "/dynamic-hello", nil)
	dynamicRecord := httptest.NewRecorder()
	router.ServeHTTP(dynamicRecord, dynamicRequest)
	if dynamicRecord.Code != http.StatusOK || dynamicRecord.Body.String() != "/dynamic-hello" {
		t.Fatalf("dynamic executable route response = %d %q", dynamicRecord.Code, dynamicRecord.Body.String())
	}

	content, err := manager.ApplyHook(t.Context(), "blog.content", blog.PluginHookPayload{Content: "original"})
	if err != nil {
		t.Fatalf("ApplyHook() error = %v", err)
	}
	if content != "executable:original" {
		t.Fatalf("ApplyHook() = %q, want executable:original", content)
	}
	if err := manager.Close(context.Background()); err != nil {
		t.Fatalf("close first manager: %v", err)
	}

	restarted, err := NewManager(context.Background(), db, dataDir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = restarted.Close(context.Background()) })
	if err := restarted.LoadEnabled(context.Background()); err != nil {
		t.Fatalf("LoadEnabled() error = %v", err)
	}
	content, err = restarted.ApplyHook(context.Background(), "blog.content", blog.PluginHookPayload{Content: "restarted"})
	if err != nil {
		t.Fatalf("restarted ApplyHook() error = %v", err)
	}
	if content != "executable:restarted" {
		t.Fatalf("restarted ApplyHook() = %q, want executable:restarted", content)
	}
	if err := restarted.Disable(context.Background(), manifest.ID); err != nil {
		t.Fatalf("Disable() error = %v", err)
	}
	if err := restarted.Delete(manifest.ID); err != nil {
		t.Fatalf("Delete() error = %v", err)
	}
}

// Package the real SDK example instead of the entire test runner: race-instrumented
// test binaries can exceed the production package limit as regression coverage grows.
func buildExamplePlugin(t *testing.T) []byte {
	t.Helper()
	binary := filepath.Join(t.TempDir(), "plugin.exe")
	cmd := exec.CommandContext(t.Context(), "go", "build", "-o", binary, "../../examples/server-plugin-echo")
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("build SDK example: %v\n%s", err, output)
	}
	content, err := os.ReadFile(binary)
	if err != nil {
		t.Fatal(err)
	}
	return content
}
