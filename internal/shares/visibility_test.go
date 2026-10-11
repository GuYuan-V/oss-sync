package shares

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	"github.com/glebarez/sqlite"
	"github.com/helantianshen/oss-sync/internal/auth"
	"github.com/helantianshen/oss-sync/internal/config"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"

	"github.com/helantianshen/oss-sync/internal/models"
)

func newVisibilityTestHandler(t *testing.T) (*Handler, *gorm.DB) {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(t.TempDir()+"/visibility.db"), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatal(err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })
	if err := db.AutoMigrate(&models.Vault{}, &models.VaultSetting{}, &models.File{}, &models.Share{}); err != nil {
		t.Fatal(err)
	}
	return New(db, &config.Config{}), db
}

func TestNormalizeShareAccess(t *testing.T) {
	cases := []struct {
		name     string
		access   ShareAccess
		wantVis  string
		wantHash bool
		wantErr  bool
	}{
		{"empty defaults to public", ShareAccess{}, "public", false, false},
		{"explicit public", ShareAccess{Visibility: "public"}, "public", false, false},
		{"login", ShareAccess{Visibility: "login"}, "login", false, false},
		{"password hashes secret", ShareAccess{Visibility: "password", Password: "secret"}, "password", true, false},
		{"password without secret fails", ShareAccess{Visibility: "password"}, "", false, true},
		{"unknown value fails", ShareAccess{Visibility: "team"}, "", false, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			visibility, hash, err := normalizeShareAccess(tc.access)
			if tc.wantErr {
				if err == nil {
					t.Fatal("normalizeShareAccess() error = nil, want error")
				}
				return
			}
			if err != nil {
				t.Fatalf("normalizeShareAccess() error = %v", err)
			}
			if visibility != tc.wantVis {
				t.Errorf("visibility = %q, want %q", visibility, tc.wantVis)
			}
			if tc.wantHash {
				if hash == "" || hash == tc.access.Password {
					t.Errorf("password hash = %q, want a bcrypt digest", hash)
				}
				if !strings.HasPrefix(hash, "$2a$") {
					t.Errorf("password hash = %q, want bcrypt prefix", hash)
				}
			} else if hash != "" {
				t.Errorf("password hash = %q, want empty", hash)
			}
		})
	}
}

func TestShareUpdateVisibility(t *testing.T) {
	gin.SetMode(gin.TestMode)
	handler, db := newVisibilityTestHandler(t)

	vault := models.Vault{ID: "vault-vis", OwnerID: 7, Name: "Vis"}
	if err := db.Create(&vault).Error; err != nil {
		t.Fatal(err)
	}
	db.Create(&models.File{UserID: 7, VaultID: vault.ID, Path: "Note.md", Type: "markdown"})

	shareID, err := handler.CreateWeb(7, vault.ID, "Note.md", false, false, ShareAccess{Visibility: "password", Password: "pw12345"})
	if err != nil {
		t.Fatalf("CreateWeb() error = %v", err)
	}

	var stored models.Share
	if err := db.Where("share_id = ?", shareID).First(&stored).Error; err != nil {
		t.Fatal(err)
	}
	if stored.Visibility != "password" {
		t.Fatalf("stored visibility = %q, want password", stored.Visibility)
	}
	if stored.SharePassword == "" || stored.SharePassword == "pw12345" {
		t.Fatalf("stored password = %q, want hashed", stored.SharePassword)
	}

	// PATCH 切到 login 后必须清掉密码哈希
	body := `{"visibility":"login"}`
	req := httptest.NewRequest(http.MethodPatch, "/api/shares/"+shareID, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = req
	c.Params = gin.Params{{Key: "id", Value: shareID}}
	// auth.RequireUser 从 gin context 取已注入的用户
	c.Set(auth.ContextKeyCurrentUser, &models.User{ID: 7, Username: "owner", Role: "admin"})
	handler.Update(c)

	if rec.Code != http.StatusOK {
		t.Fatalf("Update() status = %d, want 200: %s", rec.Code, rec.Body.String())
	}
	db.Where("share_id = ?", shareID).First(&stored)
	if stored.Visibility != "login" || stored.SharePassword != "" {
		t.Fatalf("after update: visibility=%q password=%q, want login and empty", stored.Visibility, stored.SharePassword)
	}

	var out map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	if out["visibility"] != "login" {
		t.Fatalf("response visibility = %v, want login", out["visibility"])
	}
}
