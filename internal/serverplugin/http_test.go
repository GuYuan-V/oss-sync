package serverplugin

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"

	"github.com/helantianshen/oss-sync/internal/auth"
	"github.com/helantianshen/oss-sync/internal/config"
	"github.com/helantianshen/oss-sync/internal/models"
)

func TestPluginManagerServesEnabledPublicRoute(t *testing.T) {
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

	response := PluginResponse{Status: 200, BodyBase64: base64.StdEncoding.EncodeToString([]byte("hello"))}
	manifest := Manifest{
		ID: "hello-world", Name: "Hello world", Version: "1.0.0", APIVersion: CurrentAPIVersion,
		Routes: []RouteSpec{{Method: "GET", Path: "/hello", Public: true}},
	}
	manifestBytes, err := json.Marshal(manifest)
	if err != nil {
		t.Fatal(err)
	}
	wasm := testResponseModule(t, response)
	archiveBytes := makePackageArchive(t, map[string][]byte{
		"manifest.json": manifestBytes,
		"plugin.wasm":   wasm,
	})
	if _, err := manager.Install(t.Context(), bytes.NewReader(archiveBytes), int64(len(archiveBytes))); err != nil {
		t.Fatal(err)
	}
	if err := manager.Enable(t.Context(), manifest.ID); err != nil {
		t.Fatal(err)
	}

	gin.SetMode(gin.TestMode)
	router := gin.New()
	manager.RegisterRoutes(router, &config.Config{})
	record := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/plugins/hello-world/hello?name=world", nil)
	router.ServeHTTP(record, request)
	if record.Code != http.StatusOK || record.Body.String() != "hello" {
		t.Fatalf("plugin response = %d %q", record.Code, record.Body.String())
	}
}

func TestPluginManagerKeepsAuthenticatedRouteOutOfPublicNamespace(t *testing.T) {
	manifest := Manifest{
		ID: "private-world", Name: "Private world", Version: "1.0.0", APIVersion: CurrentAPIVersion,
		Routes: []RouteSpec{{Method: "GET", Path: "/private", Public: false}},
	}
	if routeExists(manifest, http.MethodGet, "/private", true) {
		t.Fatal("authenticated plugin route must not be available in public namespace")
	}
	if !routeExists(manifest, http.MethodGet, "/private", false) {
		t.Fatal("authenticated plugin route should be available to authenticated namespace")
	}
}

func TestPluginManagerServesAuthenticatedRouteForBearerUser(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(t.TempDir()+"/plugins-authenticated.db"), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
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
	if err := db.AutoMigrate(&models.User{}, &models.UserSetting{}, &models.ServerPlugin{}); err != nil {
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
	manifest := Manifest{
		ID: "private-world", Name: "Private world", Version: "1.0.0", APIVersion: CurrentAPIVersion,
		Routes: []RouteSpec{{Method: http.MethodGet, Path: "/private", Public: false}},
	}
	manifestBytes, err := json.Marshal(manifest)
	if err != nil {
		t.Fatal(err)
	}
	wasm := testResponseModule(t, PluginResponse{Status: http.StatusOK, BodyBase64: base64.StdEncoding.EncodeToString([]byte("private"))})
	archiveBytes := makePackageArchive(t, map[string][]byte{
		"manifest.json": manifestBytes,
		"plugin.wasm":   wasm,
	})
	if _, err := manager.Install(t.Context(), bytes.NewReader(archiveBytes), int64(len(archiveBytes))); err != nil {
		t.Fatal(err)
	}
	if err := manager.Enable(t.Context(), manifest.ID); err != nil {
		t.Fatal(err)
	}

	user, err := auth.CreateAccount(db, "plugin-user", "pass12345", "user")
	if err != nil {
		t.Fatal(err)
	}
	cfg := &config.Config{Auth: config.AuthConfig{JWTSecret: "test-plugin-jwt-secret", JWTTTLHours: 1}}
	token, _, err := auth.IssueToken(cfg, *user)
	if err != nil {
		t.Fatal(err)
	}

	gin.SetMode(gin.TestMode)
	router := gin.New()
	manager.RegisterRoutes(router, cfg)

	unauthenticated := httptest.NewRecorder()
	router.ServeHTTP(unauthenticated, httptest.NewRequest(http.MethodGet, "/api/plugins/private-world/private", nil))
	if unauthenticated.Code != http.StatusUnauthorized {
		t.Fatalf("unauthenticated plugin route = %d, want %d", unauthenticated.Code, http.StatusUnauthorized)
	}

	request := httptest.NewRequest(http.MethodGet, "/api/plugins/private-world/private", nil)
	request.Header.Set("Authorization", "Bearer "+token)
	record := httptest.NewRecorder()
	router.ServeHTTP(record, request)
	if record.Code != http.StatusOK || record.Body.String() != "private" {
		t.Fatalf("authenticated plugin response = %d %q", record.Code, record.Body.String())
	}

	publicRequest := httptest.NewRequest(http.MethodGet, "/plugins/private-world/private", nil)
	publicRecord := httptest.NewRecorder()
	router.ServeHTTP(publicRecord, publicRequest)
	if publicRecord.Code != http.StatusNotFound {
		t.Fatalf("private route in public namespace = %d, want %d", publicRecord.Code, http.StatusNotFound)
	}
}
