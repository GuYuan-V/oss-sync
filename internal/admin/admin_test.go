package admin

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"

	"github.com/helantianshen/oss-sync/internal/auth"
	"github.com/helantianshen/oss-sync/internal/config"
	"github.com/helantianshen/oss-sync/internal/models"
)

func TestUpdateUser_whenRoleAndQuotaChange_returnsPersistedValues(t *testing.T) {
	// Given
	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "admin.db")), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatalf("open database: %v", err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatalf("get database handle: %v", err)
	}
	t.Cleanup(func() {
		if err := sqlDB.Close(); err != nil {
			t.Errorf("close database: %v", err)
		}
	})
	if err := db.AutoMigrate(&models.User{}, &models.UserSetting{}); err != nil {
		t.Fatalf("migrate users: %v", err)
	}
	admin, err := auth.CreateAccount(db, "admin", "AdminPass123!", "admin")
	if err != nil {
		t.Fatalf("create admin: %v", err)
	}
	target, err := auth.CreateAccount(db, "target", "TargetPass123!", "user")
	if err != nil {
		t.Fatalf("create target: %v", err)
	}
	cfg := &config.Config{Auth: config.AuthConfig{JWTSecret: "admin-test-secret", JWTTTLHours: 1}}
	token, _, err := auth.IssueToken(cfg, *admin)
	if err != nil {
		t.Fatalf("issue admin token: %v", err)
	}
	gin.SetMode(gin.TestMode)
	router := gin.New()
	New(db, cfg).Register(router)
	body, err := json.Marshal(map[string]any{"role": "admin", "storage_quota": 4096})
	if err != nil {
		t.Fatalf("encode request: %v", err)
	}
	request := httptest.NewRequest(http.MethodPatch, fmt.Sprintf("/api/admin/users/%d", target.ID), bytes.NewReader(body))
	request.Header.Set("Authorization", "Bearer "+token)
	request.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()

	// When
	router.ServeHTTP(recorder, request)

	// Then
	if recorder.Code != http.StatusOK {
		t.Fatalf("update user status = %d body=%s", recorder.Code, recorder.Body.String())
	}
	var response struct {
		Role         string `json:"role"`
		StorageQuota int64  `json:"storage_quota"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if response.Role != "admin" || response.StorageQuota != 4096 {
		t.Fatalf("response = %+v, want persisted role and quota", response)
	}
}
