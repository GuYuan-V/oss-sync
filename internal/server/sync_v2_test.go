package server

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/gin-gonic/gin"

<<<<<<< HEAD
	"github.com/oss/oss-server/internal/models"
	"github.com/oss/oss-server/internal/recycle"
=======
	"github.com/oss/oss-server/internal/filestore"
	"github.com/oss/oss-server/internal/models"
>>>>>>> 3b7aaacb143eff9df5a728b914a633fc58e70a6b
)

func uploadV2(
	t *testing.T,
	router *gin.Engine,
	token, vaultID, path, content string,
	baseRevision int64,
	clientID, operationID string,
) (int, map[string]any) {
	t.Helper()
	query := url.Values{
		"path":          {path},
		"base_revision": {strconv.FormatInt(baseRevision, 10)},
		"mtime":         {"1700000000000"},
		"client_id":     {clientID},
		"operation_id":  {operationID},
	}
	req := httptest.NewRequest(
		http.MethodPost,
		"/api/vaults/"+url.PathEscape(vaultID)+"/sync/upload?"+query.Encode(),
		bytes.NewBufferString(content),
	)
	req.Header.Set("Content-Type", "application/octet-stream")
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("X-OSS-Client-ID", clientID)
<<<<<<< HEAD
	req.Header.Set("X-OSS-Device-Name", url.QueryEscape("device-"+clientID))
=======
>>>>>>> 3b7aaacb143eff9df5a728b914a633fc58e70a6b
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	return w.Code, decodeMap(w.Body.Bytes())
}

func downloadV2(
	t *testing.T,
	router *gin.Engine,
	token, vaultID, path string,
	revision int64,
) *httptest.ResponseRecorder {
	t.Helper()
	query := url.Values{
		"path":      {path},
		"revision":  {strconv.FormatInt(revision, 10)},
		"client_id": {"device-test"},
	}
	req := httptest.NewRequest(
		http.MethodGet,
		"/api/vaults/"+url.PathEscape(vaultID)+"/sync/download?"+query.Encode(),
		nil,
	)
	req.Header.Set("Authorization", "Bearer "+token)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	return w
}

func decodeMap(raw []byte) map[string]any {
	var out map[string]any
	_ = json.Unmarshal(raw, &out)
	return out
}

func doJSONAsDevice(
	t *testing.T,
	router *gin.Engine,
	method, path, token, clientID, deviceName string,
	body any,
) (int, map[string]any) {
	t.Helper()
	var reader io.Reader
	if body != nil {
		raw, _ := json.Marshal(body)
		reader = bytes.NewReader(raw)
	}
	req := httptest.NewRequest(method, path, reader)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("X-OSS-Client-ID", clientID)
	req.Header.Set("X-OSS-Device-Name", url.QueryEscape(deviceName))
	response := httptest.NewRecorder()
	router.ServeHTTP(response, req)
	return response.Code, decodeMap(response.Body.Bytes())
}

func revisionOf(t *testing.T, body map[string]any) int64 {
	t.Helper()
	value, ok := body["revision"].(float64)
	if !ok {
		t.Fatalf("missing revision in %v", body)
	}
	return int64(value)
}

func defaultVaultIDFromAPI(t *testing.T, router *gin.Engine, token string) string {
	t.Helper()
	code, body := doJSON(t, router, http.MethodGet, "/api/vaults", token, nil)
	if code != http.StatusOK {
		t.Fatalf("list vaults: %d %v", code, body)
	}
	rows, ok := body["vaults"].([]any)
	if !ok || len(rows) != 1 {
		t.Fatalf("default vault response: %v", body)
	}
	vault := rows[0].(map[string]any)
	if vault["is_default"] != true {
		t.Fatalf("vault is not default: %v", vault)
	}
	return vault["id"].(string)
}

func createVaultViaAPI(t *testing.T, router *gin.Engine, token, name string) string {
	t.Helper()
	code, body := doJSON(t, router, http.MethodPost, "/api/vaults", token, map[string]any{"name": name})
	if code != http.StatusCreated {
		t.Fatalf("create vault: %d %v", code, body)
	}
	return body["id"].(string)
}

<<<<<<< HEAD
func approveDevice(t *testing.T, router *gin.Engine, token, clientID string, vaultIDs ...string) {
	t.Helper()
	code, body := doJSON(t, router, http.MethodPut,
		"/api/devices/"+url.PathEscape(clientID)+"/authorization",
		token,
		map[string]any{"status": "approved", "name": clientID, "vault_ids": vaultIDs},
	)
	if code != http.StatusOK {
		t.Fatalf("approve device %s: %d %v", clientID, code, body)
	}
}

func TestSyncStrategy_whenUserPreferencesAreWithinAdministratorCeilings_returnsEffectiveTiming(t *testing.T) {
	// Given
	srv, db, _ := newTestServer(t)
	router := srv.Router()
	token := registerAndLogin(t, router, "strategy-user", "password123")
	vaultID := defaultVaultIDFromAPI(t, router, token)
	approveDevice(t, router, token, "device-policy", vaultID)
	if err := db.Model(&models.SystemSetting{}).Where("id = 1").Updates(map[string]any{
		"max_long_poll_wait_sec": 20,
		"max_sync_debounce_sec":  60,
	}).Error; err != nil {
		t.Fatalf("set strategy ceilings: %v", err)
	}
	if err := db.Model(&models.UserSetting{}).Where("user_id = 1").Updates(map[string]any{
		"long_poll_wait_sec": 18,
		"sync_debounce_sec":  12,
	}).Error; err != nil {
		t.Fatalf("set strategy preferences: %v", err)
	}

	// When
	code, body := doJSONAsDevice(
		t,
		router,
		http.MethodGet,
		"/api/vaults/"+url.PathEscape(vaultID)+"/sync/strategy?client_id=device-policy&mode=long_poll",
		token,
		"device-policy",
		"Policy device",
		nil,
	)

	// Then
	if code != http.StatusOK {
		t.Fatalf("strategy response: %d %v", code, body)
	}
	if body["long_poll_wait_sec"] != float64(18) || body["min_debounce_sec"] != float64(12) {
		t.Errorf("strategy timing = %v/%v, want 18/12", body["long_poll_wait_sec"], body["min_debounce_sec"])
	}
}

func TestSyncStrategy_whenGlobalModeIsForced_ignoresClientPreference(t *testing.T) {
	// Given
	srv, db, _ := newTestServer(t)
	router := srv.Router()
	token := registerAndLogin(t, router, "global-strategy-user", "password123")
	vaultID := defaultVaultIDFromAPI(t, router, token)
	approveDevice(t, router, token, "global-policy-device", vaultID)
	if err := db.Model(&models.SystemSetting{}).Where("id = 1").Update("sync_mode", "long_poll").Error; err != nil {
		t.Fatalf("set global sync mode: %v", err)
	}
	// When
	code, body := doJSONAsDevice(
		t,
		router,
		http.MethodGet,
		"/api/vaults/"+url.PathEscape(vaultID)+"/sync/strategy?client_id=global-policy-device&mode=short_poll",
		token,
		"global-policy-device",
		"Global policy device",
		nil,
	)

	// Then
	if code != http.StatusOK {
		t.Fatalf("strategy response: %d %v", code, body)
	}
	if body["policy"] != "long_poll" || body["effective_mode"] != "long_poll" {
		t.Fatalf("strategy policy/effective = %v/%v, want long_poll/long_poll", body["policy"], body["effective_mode"])
	}
}

func TestV2Upload_whenUserUploadPreferenceTightensAdminCeiling_rejectsOversizedContent(t *testing.T) {
	// Given
	srv, db, _ := newTestServer(t)
	router := srv.Router()
	token := registerAndLogin(t, router, "v2-upload-limit", "password123")
	vaultID := defaultVaultIDFromAPI(t, router, token)
	approveDevice(t, router, token, "device-upload-limit", vaultID)
	if err := db.Model(&models.SystemSetting{}).Where("id = 1").Update("max_upload_size_bytes", 10).Error; err != nil {
		t.Fatalf("set upload ceiling: %v", err)
	}
	if err := db.Model(&models.UserSetting{}).Where("user_id = 1").Update("upload_size_bytes", 5).Error; err != nil {
		t.Fatalf("set upload preference: %v", err)
	}

	// When
	code, body := uploadV2(
		t, router, token, vaultID, "Notes/Large.md", "123456",
		0, "device-upload-limit", "upload-over-limit",
	)

	// Then
	if code != http.StatusRequestEntityTooLarge {
		t.Errorf("oversized v2 upload: status=%d body=%v, want 413", code, body)
	}
}

func TestV2Upload_whenUserVaultCapacityIsExceeded_rejectsWrite(t *testing.T) {
	// Given
	srv, db, _ := newTestServer(t)
	router := srv.Router()
	token := registerAndLogin(t, router, "v2-vault-limit", "password123")
	vaultID := defaultVaultIDFromAPI(t, router, token)
	approveDevice(t, router, token, "device-vault-limit", vaultID)
	if err := db.Model(&models.SystemSetting{}).Where("id = 1").Update("max_vault_storage_bytes", 10).Error; err != nil {
		t.Fatalf("set vault ceiling: %v", err)
	}
	if err := db.Model(&models.UserSetting{}).Where("user_id = 1").Update("vault_storage_bytes", 5).Error; err != nil {
		t.Fatalf("set vault preference: %v", err)
	}

	// When
	code, body := uploadV2(
		t, router, token, vaultID, "Notes/Capacity.md", "123456",
		0, "device-vault-limit", "upload-over-capacity",
	)

	// Then
	if code != http.StatusInternalServerError || body["error"] != "vault storage quota exceeded" {
		t.Errorf("capacity overflow: status=%d body=%v, want quota error", code, body)
	}
}

func TestVaultCreate_whenUserCapacityIsWithinAdminCeiling_seedsEffectiveQuota(t *testing.T) {
	// Given
	srv, db, _ := newTestServer(t)
	router := srv.Router()
	token := registerAndLogin(t, router, "vault-default-limit", "password123")
	if err := db.Model(&models.SystemSetting{}).Where("id = 1").Update("max_vault_storage_bytes", 10).Error; err != nil {
		t.Fatalf("set vault ceiling: %v", err)
	}
	if err := db.Model(&models.UserSetting{}).Where("user_id = 1").Update("vault_storage_bytes", 8).Error; err != nil {
		t.Fatalf("set vault preference: %v", err)
	}

	// When
	code, body := doJSON(t, router, http.MethodPost, "/api/vaults", token, map[string]any{"name": "Policy Vault"})

	// Then
	if code != http.StatusCreated {
		t.Fatalf("create policy vault: %d %v", code, body)
	}
	if body["storage_quota"] != float64(8) {
		t.Errorf("storage quota = %v, want 8", body["storage_quota"])
	}
}

=======
>>>>>>> 3b7aaacb143eff9df5a728b914a633fc58e70a6b
func TestSyncV2MultiVaultIsolationCASAndSharing(t *testing.T) {
	srv, _, _ := newTestServer(t)
	router := srv.Router()
	token := registerAndLogin(t, router, "v2-isolation", "password123")
	defaultVault := defaultVaultIDFromAPI(t, router, token)
	secondVault := createVaultViaAPI(t, router, token, "Second")

<<<<<<< HEAD
	approveDevice(t, router, token, "device-a", defaultVault, secondVault)
	approveDevice(t, router, token, "device-b", defaultVault, secondVault)
	approveDevice(t, router, token, "device-test", defaultVault, secondVault)

=======
>>>>>>> 3b7aaacb143eff9df5a728b914a633fc58e70a6b
	code, first := uploadV2(
		t, router, token, defaultVault, "Notes/Same.md", "# Default Vault",
		0, "device-a", "default-create",
	)
	if code != http.StatusOK {
		t.Fatalf("upload default: %d %v", code, first)
	}
	code, second := uploadV2(
		t, router, token, secondVault, "Notes/Same.md", "# Second Vault",
		0, "device-a", "second-create",
	)
	if code != http.StatusOK {
		t.Fatalf("upload second: %d %v", code, second)
	}

	for vaultID, wantHash := range map[string]any{
		defaultVault: first["hash"],
		secondVault:  second["hash"],
	} {
		code, manifest := doJSON(
			t,
			router,
			http.MethodGet,
			"/api/vaults/"+url.PathEscape(vaultID)+"/sync/manifest?after=0&client_id=device-b",
			token,
			nil,
		)
		if code != http.StatusOK {
			t.Fatalf("manifest %s: %d %v", vaultID, code, manifest)
		}
		files := manifest["files"].([]any)
		if len(files) != 1 || files[0].(map[string]any)["hash"] != wantHash {
			t.Fatalf("vault %s leaked or missed files: %v", vaultID, files)
		}
	}

	if got := downloadV2(t, router, token, defaultVault, "Notes/Same.md", revisionOf(t, first)); got.Body.String() != "# Default Vault" {
		t.Fatalf("default download: status=%d body=%q", got.Code, got.Body.String())
	}
	if got := downloadV2(t, router, token, secondVault, "Notes/Same.md", revisionOf(t, second)); got.Body.String() != "# Second Vault" {
		t.Fatalf("second download: status=%d body=%q", got.Code, got.Body.String())
	}

	code, stale := uploadV2(
		t, router, token, defaultVault, "Notes/Same.md", "# Stale",
		0, "device-b", "stale-update",
	)
	if code != http.StatusConflict {
		t.Fatalf("stale CAS: %d %v", code, stale)
	}
	current := stale["current"].(map[string]any)
	if int64(current["revision"].(float64)) != revisionOf(t, first) {
		t.Fatalf("stale conflict current=%v", current)
	}

	code, retry := uploadV2(
		t, router, token, defaultVault, "Notes/Same.md", "# Default Vault",
		0, "device-a", "default-create",
	)
	if code != http.StatusOK || revisionOf(t, retry) != revisionOf(t, first) {
		t.Fatalf("idempotent retry created another revision: %d %v", code, retry)
	}

	code, share := doJSON(t, router, http.MethodPost, "/api/shares", token, map[string]any{
		"vault_id":    secondVault,
		"target_path": "Notes/Same.md",
	})
	if code != http.StatusOK {
		t.Fatalf("create share: %d %v", code, share)
	}
	req := httptest.NewRequest(http.MethodGet, share["url"].(string), nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), "Second Vault") ||
		strings.Contains(w.Body.String(), "Default Vault") {
		t.Fatalf("share crossed vault boundary: status=%d body=%q", w.Code, w.Body.String())
	}
}

func TestVaultManagementCRUD(t *testing.T) {
	t.Chdir(t.TempDir())
	srv, db, _ := newTestServer(t)
	router := srv.Router()
	token := registerAndLogin(t, router, "vault-crud", "password123")
	defaultVault := defaultVaultIDFromAPI(t, router, token)
	secondVault := createVaultViaAPI(t, router, token, "Second")

	code, updated := doJSON(
		t,
		router,
		http.MethodPatch,
		"/api/vaults/"+url.PathEscape(secondVault),
		token,
		map[string]any{"name": "Renamed", "description": "secondary notes"},
	)
	if code != http.StatusOK || updated["name"] != "Renamed" ||
		updated["description"] != "secondary notes" {
		t.Fatalf("update vault: %d %v", code, updated)
	}

	code, _ = doJSON(
		t,
		router,
		http.MethodDelete,
		"/api/vaults/"+url.PathEscape(defaultVault),
		token,
		nil,
	)
	if code != http.StatusConflict {
		t.Fatalf("default vault should not be archived: %d", code)
	}

	code, _ = doJSON(
		t,
		router,
		http.MethodDelete,
		"/api/vaults/"+url.PathEscape(secondVault),
		token,
		nil,
	)
	if code != http.StatusOK {
		t.Fatalf("delete vault: %d", code)
	}
	var backup models.VaultBackup
	if err := db.Where("vault_id = ?", secondVault).First(&backup).Error; err != nil {
		t.Fatalf("vault backup record: %v", err)
	}
	if _, err := os.Stat(filepath.Join("backups", "vaults", backup.FileName)); err != nil {
		t.Fatalf("vault backup archive: %v", err)
	}
	code, _ = doJSON(
		t,
		router,
		http.MethodGet,
		"/api/vaults/"+url.PathEscape(secondVault),
		token,
		nil,
	)
	if code != http.StatusNotFound {
		t.Fatalf("archived vault should be hidden: %d", code)
	}
}

func TestSyncV2IncrementalDeleteAndRename(t *testing.T) {
	srv, db, dataDir := newTestServer(t)
	router := srv.Router()
	token := registerAndLogin(t, router, "v2-mutations", "password123")
	vaultID := defaultVaultIDFromAPI(t, router, token)
<<<<<<< HEAD
	approveDevice(t, router, token, "device-a", vaultID)
	approveDevice(t, router, token, "device-b", vaultID)
	approveDevice(t, router, token, "device-test", vaultID)
=======
>>>>>>> 3b7aaacb143eff9df5a728b914a633fc58e70a6b

	code, created := uploadV2(
		t, router, token, vaultID, "Notes/Changed.md", "one",
		0, "device-a", "create-changed",
	)
	if code != http.StatusOK {
		t.Fatalf("create: %d %v", code, created)
	}
	createdRevision := revisionOf(t, created)
	code, modified := uploadV2(
		t, router, token, vaultID, "Notes/Changed.md", "two",
		createdRevision, "device-a", "modify-changed",
	)
	if code != http.StatusOK {
		t.Fatalf("modify: %d %v", code, modified)
	}
	modifiedRevision := revisionOf(t, modified)

	code, changes := doJSON(
		t,
		router,
		http.MethodGet,
		"/api/vaults/"+url.PathEscape(vaultID)+"/sync/changes?after="+strconv.FormatInt(createdRevision, 10)+"&client_id=device-b",
		token,
		nil,
	)
	if code != http.StatusOK {
		t.Fatalf("changes: %d %v", code, changes)
	}
	files := changes["files"].([]any)
	if len(files) != 1 || int64(files[0].(map[string]any)["revision"].(float64)) != modifiedRevision {
		t.Fatalf("incremental response: %v", changes)
	}

	code, deleted := doJSON(t, router, http.MethodPost,
		"/api/vaults/"+url.PathEscape(vaultID)+"/sync/delete",
		token,
		map[string]any{
			"path":          "Notes/Changed.md",
			"base_revision": modifiedRevision,
			"client_id":     "device-a",
			"operation_id":  "delete-changed",
			"client_mtime":  1700000001000,
		},
	)
	if code != http.StatusOK || deleted["deleted"] != true {
		t.Fatalf("delete: %d %v", code, deleted)
	}
	var deletedFile models.File
	if err := db.Where(
		"vault_id = ? AND path = ?",
		vaultID,
		"Notes/Changed.md",
	).First(&deletedFile).Error; err != nil {
		t.Fatal(err)
	}
	if !deletedFile.IsDeleted {
		t.Fatal("delete did not create a synchronization tombstone")
	}
<<<<<<< HEAD
	// 删除后正文进入回收站，原始 files 路径不再存在。
	if _, err := os.Stat(recycle.DiskPath(dataDir, deletedFile)); os.IsNotExist(err) {
		t.Fatalf("deleted content missing from recycle bin: %v", err)
	}
	if !strings.HasPrefix(deletedFile.StorageKey, "vaults/"+vaultID+"/recycle/") {
		t.Fatalf("deleted file storage key should point to recycle bin: %q", deletedFile.StorageKey)
=======
	if _, err := os.Stat(filestore.DiskPath(dataDir, deletedFile)); !os.IsNotExist(err) {
		t.Fatalf("deleted content remains on disk: %v", err)
>>>>>>> 3b7aaacb143eff9df5a728b914a633fc58e70a6b
	}
	deletedRevision := revisionOf(t, deleted)
	code, retryDelete := doJSON(t, router, http.MethodPost,
		"/api/vaults/"+url.PathEscape(vaultID)+"/sync/delete",
		token,
		map[string]any{
			"path":          "Notes/Changed.md",
			"base_revision": modifiedRevision,
			"client_id":     "device-a",
			"operation_id":  "delete-changed",
			"client_mtime":  1700000001000,
		},
	)
	if code != http.StatusOK || revisionOf(t, retryDelete) != deletedRevision {
		t.Fatalf("delete retry: %d %v", code, retryDelete)
	}

	code, changes = doJSON(
		t,
		router,
		http.MethodGet,
		"/api/vaults/"+url.PathEscape(vaultID)+"/sync/changes?after="+strconv.FormatInt(modifiedRevision, 10)+"&client_id=device-b",
		token,
		nil,
	)
	files = changes["files"].([]any)
	if code != http.StatusOK || len(files) != 1 || files[0].(map[string]any)["deleted"] != true {
		t.Fatalf("tombstone was not propagated: %d %v", code, changes)
	}

	code, source := uploadV2(
		t, router, token, vaultID, "Notes/Source.md", "source",
		0, "device-a", "create-source",
	)
	if code != http.StatusOK {
		t.Fatalf("source upload: %d %v", code, source)
	}
	code, target := uploadV2(
		t, router, token, vaultID, "Notes/Target.md", "target",
		0, "device-a", "create-target",
	)
	if code != http.StatusOK {
		t.Fatalf("target upload: %d %v", code, target)
	}

	renamePath := "/api/vaults/" + url.PathEscape(vaultID) + "/sync/rename"
	code, conflict := doJSON(t, router, http.MethodPost, renamePath, token, map[string]any{
		"old_path":        "Notes/Source.md",
		"new_path":        "Notes/Target.md",
		"base_revision":   revisionOf(t, source),
		"target_revision": revisionOf(t, target),
		"client_id":       "device-a",
		"operation_id":    "rename-overwrite",
		"client_mtime":    1700000002000,
	})
	if code != http.StatusConflict || conflict["path"] != "Notes/Target.md" {
		t.Fatalf("live target overwrite was not rejected: %d %v", code, conflict)
	}

	code, targetDeleted := doJSON(t, router, http.MethodPost,
		"/api/vaults/"+url.PathEscape(vaultID)+"/sync/delete",
		token,
		map[string]any{
			"path":          "Notes/Target.md",
			"base_revision": revisionOf(t, target),
			"client_id":     "device-a",
			"operation_id":  "delete-target",
			"client_mtime":  1700000003000,
		},
	)
	if code != http.StatusOK {
		t.Fatalf("delete target: %d %v", code, targetDeleted)
	}

	code, renamed := doJSON(t, router, http.MethodPost, renamePath, token, map[string]any{
		"old_path":        "Notes/Source.md",
		"new_path":        "Notes/Target.md",
		"base_revision":   revisionOf(t, source),
		"target_revision": revisionOf(t, targetDeleted),
		"client_id":       "device-a",
		"operation_id":    "rename-source",
		"client_mtime":    1700000004000,
	})
	if code != http.StatusOK {
		t.Fatalf("rename: %d %v", code, renamed)
	}
	oldMeta := renamed["old"].(map[string]any)
	newMeta := renamed["new"].(map[string]any)
	if oldMeta["deleted"] != true || newMeta["deleted"] != false {
		t.Fatalf("rename metadata: %v", renamed)
	}
	newRevision := int64(newMeta["revision"].(float64))

	code, retryRename := doJSON(t, router, http.MethodPost, renamePath, token, map[string]any{
		"old_path":        "Notes/Source.md",
		"new_path":        "Notes/Target.md",
		"base_revision":   revisionOf(t, source),
		"target_revision": revisionOf(t, targetDeleted),
		"client_id":       "device-a",
		"operation_id":    "rename-source",
		"client_mtime":    1700000004000,
	})
	if code != http.StatusOK ||
		int64(retryRename["new"].(map[string]any)["revision"].(float64)) != newRevision {
		t.Fatalf("rename retry: %d %v", code, retryRename)
	}

	if got := downloadV2(t, router, token, vaultID, "Notes/Source.md", 0); got.Code != http.StatusGone {
		t.Fatalf("renamed source should be gone: %d %q", got.Code, got.Body.String())
	}
	if got := downloadV2(t, router, token, vaultID, "Notes/Target.md", newRevision); got.Code != http.StatusOK || got.Body.String() != "source" {
		t.Fatalf("renamed target: %d %q", got.Code, got.Body.String())
	}
}

func TestSyncV2ConcurrentUploadsHaveUniqueRevisions(t *testing.T) {
	srv, _, _ := newTestServer(t)
	router := srv.Router()
	token := registerAndLogin(t, router, "v2-concurrent", "password123")
	vaultID := defaultVaultIDFromAPI(t, router, token)
<<<<<<< HEAD
	approveDevice(t, router, token, "device-a", vaultID)
	approveDevice(t, router, token, "device-b", vaultID)
=======
>>>>>>> 3b7aaacb143eff9df5a728b914a633fc58e70a6b

	const uploads = 12
	type result struct {
		code     int
		revision int64
		body     map[string]any
	}
	results := make(chan result, uploads)
	var wg sync.WaitGroup
	for i := 0; i < uploads; i++ {
		wg.Add(1)
		go func(index int) {
			defer wg.Done()
			code, body := uploadV2(
				t,
				router,
				token,
				vaultID,
				"Concurrent/"+strconv.Itoa(index)+".md",
				"content-"+strconv.Itoa(index),
				0,
				"device-a",
				"concurrent-"+strconv.Itoa(index),
			)
			revision := int64(0)
			if value, ok := body["revision"].(float64); ok {
				revision = int64(value)
			}
			results <- result{code: code, revision: revision, body: body}
		}(i)
	}
	wg.Wait()
	close(results)

	revisions := map[int64]struct{}{}
	for result := range results {
		if result.code != http.StatusOK || result.revision <= 0 {
			t.Fatalf("concurrent upload failed: status=%d body=%v", result.code, result.body)
		}
		if _, duplicate := revisions[result.revision]; duplicate {
			t.Fatalf("duplicate revision %d", result.revision)
		}
		revisions[result.revision] = struct{}{}
	}
	if len(revisions) != uploads {
		t.Fatalf("got %d revisions, want %d", len(revisions), uploads)
	}

	code, manifest := doJSON(
		t,
		router,
		http.MethodGet,
		"/api/vaults/"+url.PathEscape(vaultID)+"/sync/manifest?after=0&limit=100&client_id=device-b",
		token,
		nil,
	)
	if code != http.StatusOK || int64(manifest["snapshot_revision"].(float64)) != uploads {
		t.Fatalf("manifest head after concurrent uploads: %d %v", code, manifest)
	}
}

func TestDeviceManagementAndExplicitCursorAcknowledgement(t *testing.T) {
	srv, db, _ := newTestServer(t)
	router := srv.Router()
	token := registerAndLogin(t, router, "devices", "password123")
	vaultID := defaultVaultIDFromAPI(t, router, token)
<<<<<<< HEAD
	approveDevice(t, router, token, "device-a", vaultID)
	approveDevice(t, router, token, "device-b", vaultID)
=======
>>>>>>> 3b7aaacb143eff9df5a728b914a633fc58e70a6b

	code, created := uploadV2(
		t, router, token, vaultID, "Device.md", "content",
		0, "device-a", "create-device-file",
	)
	if code != http.StatusOK {
		t.Fatalf("upload: %d %v", code, created)
	}
	revision := revisionOf(t, created)
	code, _ = doJSON(
		t,
		router,
		http.MethodGet,
		"/api/vaults/"+url.PathEscape(vaultID)+"/sync/manifest?after=0&client_id=device-b",
		token,
		nil,
	)
	if code != http.StatusOK {
		t.Fatalf("device B manifest: %d", code)
	}

	for _, clientID := range []string{"device-a", "device-b"} {
		var binding models.DeviceVault
		if err := db.Where("vault_id = ? AND client_id = ?", vaultID, clientID).
			First(&binding).Error; err != nil {
			t.Fatal(err)
		}
		if binding.LastCursor != 0 {
			t.Fatalf("%s cursor advanced before ACK: %d", clientID, binding.LastCursor)
		}
		code, body := doJSONAsDevice(
			t,
			router,
			http.MethodPost,
			"/api/vaults/"+url.PathEscape(vaultID)+"/sync/ack",
			token,
			clientID,
			"Device "+clientID,
			map[string]any{"client_id": clientID, "cursor": revision},
		)
		if code != http.StatusOK {
			t.Fatalf("%s ACK: %d %v", clientID, code, body)
		}
	}

	code, devicesBody := doJSONAsDevice(
		t, router, http.MethodGet, "/api/devices", token, "device-a", "Laptop A", nil,
	)
	if code != http.StatusOK {
		t.Fatalf("list devices: %d %v", code, devicesBody)
	}
	rows := devicesBody["devices"].([]any)
	if len(rows) != 2 {
		t.Fatalf("devices=%v", rows)
	}

	code, body := doJSONAsDevice(
		t,
		router,
		http.MethodPatch,
		"/api/devices/device-b",
		token,
		"device-a",
		"Laptop A",
		map[string]any{"name": "Laptop B"},
	)
<<<<<<< HEAD
	if code != http.StatusConflict {
		t.Fatalf("approved device rename: %d %v, want 409", code, body)
	}
	var lockedDevice models.ClientDevice
	if err := db.Where("client_id = ?", "device-b").First(&lockedDevice).Error; err != nil {
		t.Fatal(err)
	}
	if lockedDevice.Name != "device-b" {
		t.Fatalf("approved device name changed: %q", lockedDevice.Name)
=======
	if code != http.StatusOK {
		t.Fatalf("rename device: %d %v", code, body)
>>>>>>> 3b7aaacb143eff9df5a728b914a633fc58e70a6b
	}
	code, body = doJSONAsDevice(
		t,
		router,
		http.MethodDelete,
		"/api/devices/device-a",
		token,
		"device-a",
		"Laptop A",
		nil,
	)
	if code != http.StatusConflict {
		t.Fatalf("current device revoked itself: %d %v", code, body)
	}
	code, body = doJSONAsDevice(
		t,
		router,
		http.MethodDelete,
		"/api/devices/device-b",
		token,
		"device-a",
		"Laptop A",
		nil,
	)
	if code != http.StatusNoContent {
		t.Fatalf("revoke device B: %d %v", code, body)
	}

	code, body = doJSONAsDevice(
		t,
		router,
		http.MethodGet,
		"/api/vaults/"+url.PathEscape(vaultID)+"/sync/changes?after=0&client_id=device-b",
		token,
		"device-b",
		"Laptop B",
		nil,
	)
	if code != http.StatusForbidden || body["code"] != "device_revoked" {
		t.Fatalf("revoked device still synchronized: %d %v", code, body)
	}

	spoofedUpload := httptest.NewRequest(
		http.MethodPost,
		"/api/vaults/"+url.PathEscape(vaultID)+
			"/sync/upload?path=spoofed.md&base_revision=0&client_id=device-a&operation_id=spoof-upload",
		bytes.NewBufferString("spoofed"),
	)
	spoofedUpload.Header.Set("Content-Type", "application/octet-stream")
	spoofedUpload.Header.Set("Authorization", "Bearer "+token)
	spoofedUpload.Header.Set("X-OSS-Client-ID", "device-b")
	spoofedUploadResponse := httptest.NewRecorder()
	router.ServeHTTP(spoofedUploadResponse, spoofedUpload)
	if responseBody := decodeMap(spoofedUploadResponse.Body.Bytes()); spoofedUploadResponse.Code != http.StatusForbidden ||
		responseBody["code"] != "device_revoked" {
		t.Fatalf("revoked upload identity was bypassed: %d %v", spoofedUploadResponse.Code, responseBody)
	}

	for _, request := range []struct {
		name string
		path string
		body map[string]any
	}{
		{
			name: "delete",
			path: "/api/vaults/" + url.PathEscape(vaultID) + "/sync/delete",
			body: map[string]any{
				"path":          "spoofed.md",
				"base_revision": 0,
				"client_id":     "device-a",
				"operation_id":  "spoof-delete",
			},
		},
		{
			name: "rename",
			path: "/api/vaults/" + url.PathEscape(vaultID) + "/sync/rename",
			body: map[string]any{
				"old_path":        "old.md",
				"new_path":        "new.md",
				"base_revision":   1,
				"target_revision": 0,
				"client_id":       "device-a",
				"operation_id":    "spoof-rename",
			},
		},
	} {
		code, responseBody := doJSONAsDevice(
			t,
			router,
			http.MethodPost,
			request.path,
			token,
			"device-b",
			"Laptop B",
			request.body,
		)
		if code != http.StatusForbidden || responseBody["code"] != "device_revoked" {
			t.Fatalf("revoked %s identity was bypassed: %d %v", request.name, code, responseBody)
		}
	}
}

func TestSyncV2CompactedHistoryRequiresRecoverySnapshot(t *testing.T) {
	srv, db, _ := newTestServer(t)
	router := srv.Router()
	token := registerAndLogin(t, router, "compacted", "password123")
	vaultID := defaultVaultIDFromAPI(t, router, token)
<<<<<<< HEAD
	approveDevice(t, router, token, "device-a", vaultID)
	approveDevice(t, router, token, "device-b", vaultID)
=======
>>>>>>> 3b7aaacb143eff9df5a728b914a633fc58e70a6b

	code, created := uploadV2(
		t, router, token, vaultID, "Deleted.md", "content",
		0, "device-a", "create-deleted",
	)
	if code != http.StatusOK {
		t.Fatalf("upload: %d %v", code, created)
	}
	code, deleted := doJSON(
		t,
		router,
		http.MethodPost,
		"/api/vaults/"+url.PathEscape(vaultID)+"/sync/delete",
		token,
		map[string]any{
			"path":          "Deleted.md",
			"base_revision": revisionOf(t, created),
			"client_id":     "device-a",
			"operation_id":  "delete-compacted",
		},
	)
	if code != http.StatusOK {
		t.Fatalf("delete: %d %v", code, deleted)
	}
	deletedRevision := revisionOf(t, deleted)
	if err := db.Unscoped().Where("vault_id = ? AND path = ?", vaultID, "Deleted.md").
		Delete(&models.File{}).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&models.VaultSyncState{}).Where("vault_id = ?", vaultID).
		Update("compacted_revision", deletedRevision).Error; err != nil {
		t.Fatal(err)
	}

	code, body := doJSON(
		t,
		router,
		http.MethodGet,
		"/api/vaults/"+url.PathEscape(vaultID)+"/sync/changes?after=1&client_id=device-b",
		token,
		nil,
	)
	if code != http.StatusGone || body["code"] != "history_compacted" {
		t.Fatalf("incremental history gap was not rejected: %d %v", code, body)
	}
	code, body = doJSON(
		t,
		router,
		http.MethodGet,
		"/api/vaults/"+url.PathEscape(vaultID)+"/sync/manifest?after=0&client_id=device-b",
		token,
		nil,
	)
	if code != http.StatusOK || body["recovery_snapshot"] != true {
		t.Fatalf("recovery manifest: %d %v", code, body)
	}
	if files := body["files"].([]any); len(files) != 0 {
		t.Fatalf("compacted tombstone leaked into snapshot: %v", files)
	}

	code, recreated := uploadV2(
		t, router, token, vaultID, "Deleted.md", "recreated",
		deletedRevision, "device-b", "force-recreate",
	)
	if code != http.StatusOK || revisionOf(t, recreated) <= deletedRevision {
		t.Fatalf("explicit recreation after compaction failed: %d %v", code, recreated)
	}
}
