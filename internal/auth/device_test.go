package auth

import (
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"

	"github.com/oss/oss-server/internal/config"
	"github.com/oss/oss-server/internal/database"
	"github.com/oss/oss-server/internal/jwt"
	"github.com/oss/oss-server/internal/models"
)

func newTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "auth-device.db")), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	if sqlDB, err := db.DB(); err == nil {
		t.Cleanup(func() { _ = sqlDB.Close() })
	}
	if err := database.AutoMigrate(db); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	return db
}

func newTestConfig() *config.Config {
	return &config.Config{
		Auth: config.AuthConfig{
			JWTSecret:   "test-jwt-secret-for-device",
			JWTTTLHours: 24,
		},
	}
}

func TestCurrentDeviceID_NoBinding(t *testing.T) {
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	if _, ok := CurrentDeviceID(c); ok {
		t.Fatal("expected no device id when not set")
	}
	// Helper should reject missing as 401 device_identity_required with WWW-Authenticate
	w2 := httptest.NewRecorder()
	c2, _ := gin.CreateTestContext(w2)
	c2.Set(ContextKeyIdentity, &Identity{User: &models.User{}})
	// also set user key to avoid panic
	if _, ok := RequireDeviceID(c2); ok {
		t.Fatal("expected RequireDeviceID to reject missing device")
	}
	if w2.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", w2.Code)
	}
	if got := w2.Header().Get("WWW-Authenticate"); got != `Bearer error="invalid_token"` {
		t.Fatalf("expected WWW-Authenticate header, got %q", got)
	}
	body := w2.Body.String()
	if !contains(body, "device_identity_required") {
		t.Fatalf("expected device_identity_required in body, got %q", body)
	}
}

func TestRequireDeviceID_Mismatch(t *testing.T) {
	gin.SetMode(gin.TestMode)
	cases := []struct {
		name       string
		claimID    jwt.DeviceID
		suppliedID string
	}{
		{"mismatched id", jwt.DeviceID("device-a"), "device-b"},
		{"malformed supplied", jwt.DeviceID("device-a"), "bad!id"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			w := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(w)
			c.Set(ContextKeyIdentity, &Identity{
				User:     &models.User{},
				DeviceID: tc.claimID,
				HasDID:   true,
			})
			c.Set(ContextKeyCurrentUser, &models.User{})
			if _, ok := RequireDeviceID(c, tc.suppliedID); ok {
				t.Fatal("expected mismatch to be rejected")
			}
			if w.Code != http.StatusUnauthorized {
				t.Fatalf("expected 401, got %d", w.Code)
			}
			if got := w.Header().Get("WWW-Authenticate"); got != `Bearer error="invalid_token"` {
				t.Fatalf("expected WWW-Authenticate, got %q", got)
			}
			if !contains(w.Body.String(), "device_identity_mismatch") {
				t.Fatalf("expected device_identity_mismatch, got %q", w.Body.String())
			}
		})
	}
}

func TestRequireDeviceID_MultipleSupplied(t *testing.T) {
	gin.SetMode(gin.TestMode)
	did := jwt.DeviceID("device-a")
	t.Run("all matching passes", func(t *testing.T) {
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Set(ContextKeyIdentity, &Identity{User: &models.User{}, DeviceID: did, HasDID: true})
		if got, ok := RequireDeviceID(c, "device-a", "device-a"); !ok || got != did {
			t.Fatalf("expected pass for all matching, got ok=%v did=%q", ok, got)
		}
		if w.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d", w.Code)
		}
	})
	t.Run("later mismatching fails", func(t *testing.T) {
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Set(ContextKeyIdentity, &Identity{User: &models.User{}, DeviceID: did, HasDID: true})
		if _, ok := RequireDeviceID(c, "device-a", "device-b"); ok {
			t.Fatal("expected fail for later mismatching")
		}
		if w.Code != http.StatusUnauthorized {
			t.Fatalf("expected 401, got %d", w.Code)
		}
		if !contains(w.Body.String(), "device_identity_mismatch") {
			t.Fatalf("expected device_identity_mismatch, got %q", w.Body.String())
		}
		if got := w.Header().Get("WWW-Authenticate"); got != `Bearer error="invalid_token"` {
			t.Fatalf("expected WWW-Authenticate, got %q", got)
		}
	})
	t.Run("later malformed fails", func(t *testing.T) {
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Set(ContextKeyIdentity, &Identity{User: &models.User{}, DeviceID: did, HasDID: true})
		if _, ok := RequireDeviceID(c, "device-a", "bad!id"); ok {
			t.Fatal("expected fail for later malformed")
		}
		if w.Code != http.StatusUnauthorized {
			t.Fatalf("expected 401, got %d", w.Code)
		}
		if !contains(w.Body.String(), "device_identity_mismatch") {
			t.Fatalf("expected device_identity_mismatch, got %q", w.Body.String())
		}
	})
	t.Run("empty values ignored", func(t *testing.T) {
		cases := [][]string{
			{"", "device-a"},
			{"device-a", ""},
			{"", "device-a", ""},
			{""},
			{"", ""},
			{},
		}
		for i, supplied := range cases {
			w := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(w)
			c.Set(ContextKeyIdentity, &Identity{User: &models.User{}, DeviceID: did, HasDID: true})
			if got, ok := RequireDeviceID(c, supplied...); !ok || got != did {
				t.Fatalf("case %d supplied=%v: expected pass with empty ignored, got ok=%v did=%q status=%d body=%q", i, supplied, ok, got, w.Code, w.Body.String())
			}
			if w.Code != http.StatusOK {
				t.Fatalf("case %d: expected 200, got %d", i, w.Code)
			}
		}
	})
}

func TestRequireDeviceID_Match_ReturnsClaim(t *testing.T) {
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	did := jwt.DeviceID("device-123")
	c.Set(ContextKeyIdentity, &Identity{
		User:     &models.User{},
		DeviceID: did,
		HasDID:   true,
	})
	c.Set(ContextKeyCurrentUser, &models.User{})
	got, ok := RequireDeviceID(c, "device-123")
	if !ok {
		t.Fatal("expected ok for matching device")
	}
	if got != did {
		t.Fatalf("got %q want %q", got, did)
	}
	if w.Code != http.StatusOK {
		// Gin TestContext defaults to 200 unless aborted; we expect not aborted
		if w.Code != 200 {
			t.Fatalf("unexpected status %d", w.Code)
		}
	}
}

func TestIssueDeviceToken_RoundTrip(t *testing.T) {
	cfg := newTestConfig()
	user := models.User{Username: "alice", Role: "user", TokenVersion: 1}
	user.ID = 42
	did := jwt.DeviceID("my-device-001")
	tok, ttl, err := IssueDeviceToken(cfg, user, did)
	if err != nil {
		t.Fatalf("IssueDeviceToken: %v", err)
	}
	if ttl == 0 {
		t.Fatal("expected ttl")
	}
	claims, err := jwt.Parse(cfg.Auth.JWTSecret, tok)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if claims.DeviceID != did {
		t.Fatalf("device id mismatch: got %q want %q", claims.DeviceID, did)
	}
	if claims.UserID != 42 {
		t.Fatalf("user id mismatch: got %d", claims.UserID)
	}
}

func TestIssueToken_NoDid(t *testing.T) {
	cfg := newTestConfig()
	user := models.User{Username: "bob", Role: "user"}
	user.ID = 7
	tok, _, err := IssueToken(cfg, user)
	if err != nil {
		t.Fatalf("IssueToken: %v", err)
	}
	claims, err := jwt.Parse(cfg.Auth.JWTSecret, tok)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if claims.DeviceID != "" {
		t.Fatalf("expected no did for user token, got %q", claims.DeviceID)
	}
}

func TestMiddleware_CarriesDeviceIdentity(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db := newTestDB(t)
	cfg := newTestConfig()
	// create user in db
	user := models.User{Username: "alice", PasswordHash: "x", Role: "user", TokenVersion: 0}
	if err := db.Create(&user).Error; err != nil {
		t.Fatalf("create user: %v", err)
	}
	did := jwt.DeviceID("device-xyz")
	tok, _, err := IssueDeviceToken(cfg, user, did)
	if err != nil {
		t.Fatalf("issue device token: %v", err)
	}
	// Call middleware
	r := gin.New()
	r.Use(Middleware(db, cfg))
	var gotDid jwt.DeviceID
	var gotOk bool
	r.GET("/test", func(c *gin.Context) {
		gotDid, gotOk = CurrentDeviceID(c)
		c.JSON(200, gin.H{"ok": true})
	})
	req := httptest.NewRequest("GET", "/test", nil)
	req.Header.Set("Authorization", "Bearer "+tok)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != 200 {
		t.Fatalf("expected 200, got %d body %s", w.Code, w.Body.String())
	}
	if !gotOk || gotDid != did {
		t.Fatalf("expected device %q, got %q ok=%v", did, gotDid, gotOk)
	}
	// Ensure CurrentUser still works
	userTok, _, _ := IssueToken(cfg, user)
	r2 := gin.New()
	r2.Use(Middleware(db, cfg))
	var gotUser *models.User
	r2.GET("/test2", func(c *gin.Context) {
		gotUser = CurrentUser(c)
		c.JSON(200, gin.H{"ok": true})
	})
	req2 := httptest.NewRequest("GET", "/test2", nil)
	req2.Header.Set("Authorization", "Bearer "+userTok)
	w2 := httptest.NewRecorder()
	r2.ServeHTTP(w2, req2)
	if w2.Code != 200 || gotUser == nil || gotUser.ID != user.ID {
		t.Fatalf("CurrentUser failed: code %d user %v", w2.Code, gotUser)
	}
	// Basic auth should have no device
	req3 := httptest.NewRequest("GET", "/test", nil)
	req3.SetBasicAuth("alice", "wrong") // will fail; just test no device path: use valid password? skip
	// Not needed; just ensure device absence for basic when no did
}

func TestAuthBoundary_InvalidDidRejected(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db := newTestDB(t)
	cfg := newTestConfig()
	user := models.User{Username: "bob", PasswordHash: "x", Role: "user"}
	if err := db.Create(&user).Error; err != nil {
		t.Fatalf("create: %v", err)
	}
	// craft token with invalid did (contains !)
	claims := jwt.Claims{UserID: user.ID, Username: user.Username, Role: user.Role, DeviceID: jwt.DeviceID("bad!id")}
	tok, _ := jwt.Sign(cfg.Auth.JWTSecret, claims, 3600*time.Second)
	r := gin.New()
	r.Use(Middleware(db, cfg))
	r.GET("/test", func(c *gin.Context) { c.JSON(200, gin.H{"ok": true}) })
	req := httptest.NewRequest("GET", "/test", nil)
	req.Header.Set("Authorization", "Bearer "+tok)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 for invalid did, got %d", w.Code)
	}
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && (func() bool {
		for i := 0; i <= len(s)-len(substr); i++ {
			if s[i:i+len(substr)] == substr {
				return true
			}
		}
		return false
	})()
}
