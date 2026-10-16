package syncapi

import (
	"encoding/json"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/helantianshen/oss-sync/internal/auth"
	"github.com/helantianshen/oss-sync/internal/history"
	"github.com/helantianshen/oss-sync/internal/jwt"
	"github.com/helantianshen/oss-sync/internal/models"
	"gorm.io/gorm"
)

func TestLocalObsidianStatePaths(t *testing.T) {
	for _, p := range []string{".obsidian/workspace.json", ".obsidian/workspace-mobile.json", ".obsidian/cache", ".obsidian/cache/index", ".obsidian/plugins/oss-sync/data.json", `\.obsidian\workspace.json`, "./.obsidian/plugins/test/data.json", ".obsidian/plugins/test/../test/data.json"} {
		if _, ok := normalizeRelativePath(p); ok {
			t.Errorf("allowed device-local path %q", p)
		}
	}
	for _, p := range []string{"notes/a.md", ".obsidian/app.json", ".obsidian/appearance.json", ".obsidian/plugins/test/main.js", ".obsidian/plugins/test/dataXjson", "xobsidian/plugins/test/data.json", "notes/workspace.json"} {
		if _, ok := normalizeRelativePath(p); !ok {
			t.Errorf("blocked ordinary path %q", p)
		}
	}
}

func TestLocalStateCannotBeWrittenOrRestored(t *testing.T) {
	h := &Handler{}
	for _, p := range []string{".obsidian/workspace.json", ".obsidian/cache/index", ".obsidian/plugins/test/data.json"} {
		if _, err := h.WriteFileContent(1, "v", p, []byte("state"), 1); err == nil {
			t.Fatal("write allowed", p)
		}
		if _, err := h.writeFileFromBytes(models.Vault{ID: "v"}, p, []byte("state"), history.Actor{}, history.ActionRestore, ""); err == nil {
			t.Fatal("restore allowed", p)
		}
	}
}

func TestFilteredLocalStatePagesAdvanceCursorWithoutDeletingRows(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "test.db")), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	sqlDB, _ := db.DB()
	t.Cleanup(func() { _ = sqlDB.Close() })
	if err = db.AutoMigrate(&models.User{}, &models.Vault{}, &models.File{}, &models.VaultSyncState{}, &models.ClientDevice{}, &models.DeviceVaultAccess{}, &models.DeviceVault{}); err != nil {
		t.Fatal(err)
	}
	user := models.User{Username: "owner", PasswordHash: "unused"}
	if err = db.Create(&user).Error; err != nil {
		t.Fatal(err)
	}
	for _, row := range []any{
		&models.Vault{ID: "v", OwnerID: user.ID, Name: "v"},
		&models.VaultSyncState{VaultID: "v", HeadRevision: 3},
		&models.ClientDevice{UserID: user.ID, ClientID: "client", Status: "approved"},
		&models.DeviceVaultAccess{UserID: user.ID, ClientID: "client", VaultID: "v"},
	} {
		if err = db.Create(row).Error; err != nil {
			t.Fatal(err)
		}
	}
	for i, p := range []string{".obsidian/workspace.json", ".obsidian/cache/index", "notes/a.md"} {
		if err = db.Create(&models.File{UserID: user.ID, VaultID: "v", Path: p, Revision: int64(i + 1)}).Error; err != nil {
			t.Fatal(err)
		}
	}
	h := &Handler{DB: db}
	for _, manifest := range []bool{true, false} {
		for cursor := 0; cursor < 3; cursor++ {
			w := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(w)
			c.Request = httptest.NewRequest("GET", []string{"/?after=0&limit=1", "/?after=1&limit=1", "/?after=2&limit=1"}[cursor], nil)
			c.Params = gin.Params{{Key: "vault_id", Value: "v"}}
			c.Set(auth.ContextKeyCurrentUser, &user)
			c.Set(auth.ContextKeyIdentity, &auth.Identity{User: &user, DeviceID: jwt.DeviceID("client"), HasDID: true})
			h.v2ListChanges(c, manifest)
			if w.Code != 200 {
				t.Fatalf("HTTP %d: %s", w.Code, w.Body.String())
			}
			var page v2ManifestResponse
			if err = json.Unmarshal(w.Body.Bytes(), &page); err != nil {
				t.Fatal(err)
			}
			if page.NextCursor != int64(cursor+1) || page.HasMore != (cursor < 2) {
				t.Fatalf("cursor did not advance: %+v", page)
			}
			if cursor < 2 && len(page.Files) != 0 {
				t.Fatalf("device-local row advertised: %+v", page)
			}
			if cursor == 2 && (len(page.Files) != 1 || page.Files[0].Path != "notes/a.md") {
				t.Fatalf("ordinary file missing: %+v", page)
			}
		}
	}
	var count int64
	db.Model(&models.File{}).Count(&count)
	if count != 3 {
		t.Fatalf("stored data removed: %d", count)
	}
}
