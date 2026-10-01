package server

import (
	"fmt"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/helantianshen/oss-sync/internal/models"
)

func TestBlogHomeIncludesMoreThan100Shares(t *testing.T) {
	srv, db, _ := newTestServer(t)
	r := srv.Router()
	token := registerAndLogin(t, r, "auditlimit", "password123")
	vaultID := defaultVaultIDFromAPI(t, r, token)
	var v models.Vault
	if err := db.First(&v, "id = ?", vaultID).Error; err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 101; i++ {
		p := fmt.Sprintf("Post%03d.md", i)
		uploadFile(t, r, token, p, "Article body", 1700000000000+int64(i))
		s := models.Share{ShareID: fmt.Sprintf("audit%03d", i), UserID: v.OwnerID, VaultID: vaultID, TargetPath: p, CreatedAt: time.Unix(int64(i), 0)}
		if err := db.Create(&s).Error; err != nil {
			t.Fatal(err)
		}
	}
	if err := db.Model(&models.VaultSetting{}).Where("vault_id = ?", vaultID).Updates(map[string]any{"theme_name": "papertrail", "is_public_blog": true}).Error; err != nil {
		t.Fatal(err)
	}
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest("GET", "/b/"+vaultID, nil))
	if w.Code != 200 || strings.Count(w.Body.String(), `class="pt-post"`) != 101 || !strings.Contains(w.Body.String(), `href="/p/audit000"`) {
		t.Errorf("old published article missing: status=%d, listed=%d", w.Code, strings.Count(w.Body.String(), `class="pt-post"`))
	}
}

func TestBlogRelativeImage(t *testing.T) {
	srv, _, _ := newTestServer(t)
	r := srv.Router()
	token := registerAndLogin(t, r, "auditimage", "password123")
	uploadFile(t, r, token, "Notes/Post.md", "![diagram](images/chart.png)", 1700000000000)
	uploadFile(t, r, token, "Notes/images/chart.png", "image-bytes", 1700000000001)
	status, body := doJSON(t, r, "POST", "/api/shares", token, map[string]any{"target_path": "Notes/Post.md"})
	if status != 200 {
		t.Fatal(status, body)
	}
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest("GET", "/assets/"+body["share_id"].(string)+"?ref=images/chart.png", nil))
	if w.Code != 200 {
		t.Errorf("relative image returned %d; expected 200", w.Code)
	}
}

func TestBlogAssetPathCompatibilityAndIsolation(t *testing.T) {
	srv, _, _ := newTestServer(t)
	r := srv.Router()
	token := registerAndLogin(t, r, "assetpaths", "password123")
	for p, body := range map[string]string{
		"Notes/Post.md":   "![root](images/root.png)\n![local](./images/local.png)\n![parent](../shared/parent.png)\n![space](images/a%20b.png)\n![[literal_1.png]]\n![escape](../../outside.png)",
		"images/root.png": "root", "Notes/images/root.png": "shadow",
		"Notes/images/local.png": "local", "shared/parent.png": "parent",
		"Notes/images/a b.png": "space", "Private/literalX1.png": "private",
		"outside.png": "outside",
	} {
		uploadFile(t, r, token, p, body, 1700000000000)
	}
	status, body := doJSON(t, r, "POST", "/api/shares", token, map[string]any{"target_path": "Notes/Post.md"})
	if status != 200 {
		t.Fatal(status, body)
	}
	id := body["share_id"].(string)
	for _, tt := range []struct {
		ref, body string
		status    int
	}{
		{"images/root.png", "root", 200}, {"./images/local.png", "local", 200},
		{"../shared/parent.png", "parent", 200}, {"images/a%20b.png", "space", 200},
		{"literal_1.png", "", 404}, {"../../outside.png", "", 404}, {"Private/literalX1.png", "", 404},
	} {
		t.Run(tt.ref, func(t *testing.T) {
			w := httptest.NewRecorder()
			r.ServeHTTP(w, httptest.NewRequest("GET", "/assets/"+id+"?ref="+url.QueryEscape(tt.ref), nil))
			if w.Code != tt.status || tt.status == 200 && w.Body.String() != tt.body {
				t.Fatalf("status=%d body=%q", w.Code, w.Body.String())
			}
		})
	}
}

func TestFolderBlogImagesUseAuthorizedArticleContext(t *testing.T) {
	srv, db, _ := newTestServer(t)
	r := srv.Router()
	token := registerAndLogin(t, r, "folderimages", "password123")
	for p, body := range map[string]string{
		"Public/A/Post.md":          "![image](./images/chart.png)",
		"Public/B/Post.md":          "---\nimage: ./images/chart.png\n---\nBody",
		"Private/Post.md":           "![image](./images/chart.png)",
		"Public/A/images/chart.png": "A", "Public/B/images/chart.png": "B", "Private/images/chart.png": "secret",
	} {
		uploadFile(t, r, token, p, body, 1700000000000)
	}
	status, body := doJSON(t, r, "POST", "/api/shares", token, map[string]any{"target_path": "Public", "is_folder": true})
	if status != 200 {
		t.Fatal(status, body)
	}
	id := body["share_id"].(string)
	page := httptest.NewRecorder()
	r.ServeHTTP(page, httptest.NewRequest("GET", "/p/"+id+"/A/Post.md", nil))
	if page.Code != 200 || !strings.Contains(page.Body.String(), "source=Public%2FA%2FPost.md") {
		t.Fatalf("article context missing: %s", page.Body)
	}
	for _, tt := range []struct {
		source, body string
		status       int
	}{
		{"Public/A/Post.md", "A", 200}, {"Public/B/Post.md", "B", 200}, {"Private/Post.md", "", 404}, {"Public/A/missing.md", "", 404},
	} {
		w := httptest.NewRecorder()
		r.ServeHTTP(w, httptest.NewRequest("GET", "/assets/"+id+"?ref=./images/chart.png&source="+url.QueryEscape(tt.source), nil))
		if w.Code != tt.status || tt.status == 200 && w.Body.String() != tt.body {
			t.Fatalf("source=%s status=%d body=%q", tt.source, w.Code, w.Body.String())
		}
	}
	vaultID := defaultVaultIDFromAPI(t, r, token)
	if err := db.Model(&models.File{}).Where("vault_id = ? AND path = ?", vaultID, "Public/A/Post.md").Update("is_deleted", true).Error; err != nil {
		t.Fatal(err)
	}
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest("GET", "/assets/"+id+"?ref=./images/chart.png&source=Public%2FA%2FPost.md", nil))
	if w.Code != 404 {
		t.Fatalf("deleted article asset status=%d", w.Code)
	}
}
