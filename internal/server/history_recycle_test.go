package server

import (
	"net/http"
	"net/url"
	"strconv"
	"testing"

	"github.com/oss/oss-server/internal/collaboration"
	"github.com/oss/oss-server/internal/models"
)

func TestHistoryAndRecycleFlow(t *testing.T) {
	srv, _, _ := newTestServer(t)
	router := srv.Router()
	devToken := registerAndLogin(t, router, "hist-user", "password123")
	vaultID := defaultVaultIDFromAPI(t, router, devToken)
	userToken := userOnlyToken(t, router, "hist-user", "password123")
	histToken := deviceTokenFor(t, router, "hist-user", "password123", "hist-dev", userToken, []string{vaultID})
	_ = deviceTokenFor(t, router, "hist-user", "password123", "device-test", userToken, []string{vaultID})

	// 创建文件。
	code, created := uploadV2(t, router, histToken, vaultID, "Notes/Hist.md", "# v1", 0, "hist-dev", "create-hist")
	if code != http.StatusOK {
		t.Fatalf("create: %d %v", code, created)
	}
	// 修改文件。
	code, modified := uploadV2(t, router, histToken, vaultID, "Notes/Hist.md", "# v2", revisionOf(t, created), "hist-dev", "modify-hist")
	if code != http.StatusOK {
		t.Fatalf("modify: %d %v", code, modified)
	}

	// 历史列表应有 create + modify 两条。
	histPath := "/api/vaults/" + url.PathEscape(vaultID) + "/sync/history?path=" + url.QueryEscape("Notes/Hist.md") + "&client_id=hist-dev"
	code, list := doJSONAsDevice(t, router, http.MethodGet, histPath, histToken, "hist-dev", "hist-pc", nil)
	if code != http.StatusOK {
		t.Fatalf("history list: %d %v", code, list)
	}
	rows := list["history"].([]any)
	if len(rows) != 2 {
		t.Fatalf("history rows=%d want 2: %v", len(rows), list)
	}
	// 版本倒序：第一条是 modify，第二条是 create。
	modifyRow := rows[0].(map[string]any)
	createRow := rows[1].(map[string]any)
	if modifyRow["action"] != "modify" || createRow["action"] != "create" {
		t.Fatalf("history order/action: %v", list)
	}
	if modifyRow["username"] != "hist-user" || modifyRow["device_name"] != "device-hist-dev" {
		t.Fatalf("history actor info: %v", modifyRow)
	}

	// 历史详情应返回快照内容与 diff（对比当前 # v2）。
	modifyID := uint64(modifyRow["id"].(float64))
	detailPath := "/api/vaults/" + url.PathEscape(vaultID) + "/sync/history/" + strconv.FormatUint(modifyID, 10) +
		"?mode=current&path=" + url.QueryEscape("Notes/Hist.md") + "&client_id=hist-dev"
	code, detail := doJSONAsDevice(t, router, http.MethodGet, detailPath, histToken, "hist-dev", "hist-pc", nil)
	if code != http.StatusOK || detail["content"] != "# v1" {
		t.Fatalf("history detail: %d %v", code, detail)
	}
	if diff, ok := detail["diff"].([]any); !ok || len(diff) == 0 {
		t.Fatalf("history diff missing: %v", detail)
	}

	// 从 modify 历史（快照为 # v1）恢复 -> 内容回到 # v1。
	restorePath := "/api/vaults/" + url.PathEscape(vaultID) + "/sync/history/" + strconv.FormatUint(modifyID, 10) +
		"/restore?path=" + url.QueryEscape("Notes/Hist.md") + "&client_id=hist-dev"
	code, _ = doJSONAsDevice(t, router, http.MethodPost, restorePath, histToken, "hist-dev", "hist-pc", nil)
	if code != http.StatusOK {
		t.Fatalf("history restore: %d", code)
	}
	if got := downloadV2(t, router, histToken, vaultID, "Notes/Hist.md", 0); got.Body.String() != "# v1" {
		t.Fatalf("restored content: %q", got.Body.String())
	}

	// 删除 -> 回收站（恢复操作已推进 revision，以当前 revision 作为 base）。
	restoredResp := downloadV2(t, router, histToken, vaultID, "Notes/Hist.md", 0)
	currentRev, _ := strconv.ParseInt(restoredResp.Header().Get("X-OSS-Revision"), 10, 64)
	code, deleted := doJSONAsDevice(t, router, http.MethodPost,
		"/api/vaults/"+url.PathEscape(vaultID)+"/sync/delete",
		histToken, "hist-dev", "hist-pc",
		map[string]any{
			"path": "Notes/Hist.md", "base_revision": currentRev,
			"client_id": "hist-dev", "operation_id": "delete-hist",
		})
	if code != http.StatusOK {
		t.Fatalf("delete: %d %v", code, deleted)
	}
	rbPath := "/api/vaults/" + url.PathEscape(vaultID) + "/recycle-bin?client_id=hist-dev"
	code, rb := doJSONAsDevice(t, router, http.MethodGet, rbPath, histToken, "hist-dev", "hist-pc", nil)
	if code != http.StatusOK {
		t.Fatalf("recycle list: %d %v", code, rb)
	}
	items := rb["files"].([]any)
	if len(items) != 1 {
		t.Fatalf("recycle items=%d want 1: %v", len(items), rb)
	}
	fileID := uint64(items[0].(map[string]any)["id"].(float64))

	// 从回收站恢复。
	code, _ = doJSONAsDevice(t, router, http.MethodPost,
		"/api/vaults/"+url.PathEscape(vaultID)+"/recycle-bin/"+strconv.FormatUint(fileID, 10)+"/restore?client_id=hist-dev",
		histToken, "hist-dev", "hist-pc", nil)
	if code != http.StatusOK {
		t.Fatalf("recycle restore: %d", code)
	}
	if got := downloadV2(t, router, histToken, vaultID, "Notes/Hist.md", 0); got.Code != http.StatusOK || got.Body.String() != "# v1" {
		t.Fatalf("recycle restored content: %d %q", got.Code, got.Body.String())
	}

	// 再删除并永久删除（回收站恢复再次推进 revision）。
	restoredAgain := downloadV2(t, router, histToken, vaultID, "Notes/Hist.md", 0)
	currentRev2, _ := strconv.ParseInt(restoredAgain.Header().Get("X-OSS-Revision"), 10, 64)
	code, _ = doJSONAsDevice(t, router, http.MethodPost,
		"/api/vaults/"+url.PathEscape(vaultID)+"/sync/delete",
		histToken, "hist-dev", "hist-pc",
		map[string]any{
			"path": "Notes/Hist.md", "base_revision": currentRev2,
			"client_id": "hist-dev", "operation_id": "delete-hist-2",
		})
	if code != http.StatusOK {
		t.Fatalf("delete 2: %d", code)
	}
	code, rb = doJSONAsDevice(t, router, http.MethodGet, rbPath, histToken, "hist-dev", "hist-pc", nil)
	items = rb["files"].([]any)
	if len(items) != 1 {
		t.Fatalf("recycle items after re-delete=%d: %v", len(items), rb)
	}
	fileID = uint64(items[0].(map[string]any)["id"].(float64))
	code, _ = doJSONAsDevice(t, router, http.MethodPost,
		"/api/vaults/"+url.PathEscape(vaultID)+"/recycle-bin/"+strconv.FormatUint(fileID, 10)+"/delete?client_id=hist-dev",
		histToken, "hist-dev", "hist-pc", nil)
	if code != http.StatusOK {
		t.Fatalf("recycle permanent delete: %d", code)
	}
	code, rb = doJSONAsDevice(t, router, http.MethodGet, rbPath, histToken, "hist-dev", "hist-pc", nil)
	if items := rb["files"].([]any); len(items) != 0 {
		t.Fatalf("recycle not empty after purge: %v", rb)
	}
}

func TestParticipantCannotRestoreHistoryOrRecycle(t *testing.T) {
	srv, _, _ := newTestServer(t)
	router := srv.Router()
	ownerDevToken := registerAndLogin(t, router, "hist-owner", "password123")
	vaultID := defaultVaultIDFromAPI(t, router, ownerDevToken)
	ownerToken := userOnlyToken(t, router, "hist-owner", "password123")
	ownerDev := deviceTokenFor(t, router, "hist-owner", "password123", "owner-dev", ownerToken, []string{vaultID})

	code, memberLogin := doJSON(t, router, http.MethodPost, "/api/auth/register", "", map[string]string{"username": "hist-member", "password": "password123"})
	if code != http.StatusOK {
		t.Fatalf("register member: %d", code)
	}
	memberUserToken := memberLogin["token"].(string)
	// member needs vault membership first, then device approve
	code, _ = doJSON(t, router, http.MethodPost, "/api/vaults/"+url.PathEscape(vaultID)+"/members", ownerDev, map[string]string{"username": "hist-member", "role": "participant"})
	if code != http.StatusNoContent {
		t.Fatalf("add participant: %d", code)
	}
	// create member device after membership
	memberDev := deviceTokenFor(t, router, "hist-member", "password123", "member-dev", memberUserToken, []string{vaultID})

	code, created := uploadV2(t, router, ownerDev, vaultID, "Restrict.md", "x", 0, "owner-dev", "create-restrict")
	if code != http.StatusOK {
		t.Fatalf("owner create: %d", code)
	}
	// participant 只读历史，不能恢复。
	histPath := "/api/vaults/" + url.PathEscape(vaultID) + "/sync/history?path=" + url.QueryEscape("Restrict.md") + "&client_id=member-dev"
	code, list := doJSONAsDevice(t, router, http.MethodGet, histPath, memberDev, "member-dev", "member-pc", nil)
	if code != http.StatusOK {
		t.Fatalf("participant history list: %d", code)
	}
	restoreID := uint64(list["history"].([]any)[0].(map[string]any)["id"].(float64))
	restorePath := "/api/vaults/" + url.PathEscape(vaultID) + "/sync/history/" + strconv.FormatUint(restoreID, 10) +
		"/restore?path=" + url.QueryEscape("Restrict.md") + "&client_id=member-dev"
	code, _ = doJSONAsDevice(t, router, http.MethodPost, restorePath, memberDev, "member-dev", "member-pc", nil)
	if code != http.StatusForbidden {
		t.Fatalf("participant history restore: %d", code)
	}

	// 删除后 participant 不能恢复回收站。
	code, _ = doJSONAsDevice(t, router, http.MethodPost,
		"/api/vaults/"+url.PathEscape(vaultID)+"/sync/delete",
		ownerDev, "owner-dev", "owner-pc",
		map[string]any{"path": "Restrict.md", "base_revision": revisionOf(t, created), "client_id": "owner-dev", "operation_id": "del-restrict"})
	if code != http.StatusOK {
		t.Fatalf("owner delete: %d", code)
	}
	rbPath := "/api/vaults/" + url.PathEscape(vaultID) + "/recycle-bin?client_id=member-dev"
	code, rb := doJSONAsDevice(t, router, http.MethodGet, rbPath, memberDev, "member-dev", "member-pc", nil)
	fileID := uint64(rb["files"].([]any)[0].(map[string]any)["id"].(float64))
	code, _ = doJSONAsDevice(t, router, http.MethodPost,
		"/api/vaults/"+url.PathEscape(vaultID)+"/recycle-bin/"+strconv.FormatUint(fileID, 10)+"/restore?client_id=member-dev",
		memberDev, "member-dev", "member-pc", nil)
	if code != http.StatusForbidden {
		t.Fatalf("participant recycle restore: %d", code)
	}
}

func TestAcceptedCollaboratorCanReadOnlyCollaboratedFileHistory(t *testing.T) {
	srv, db, _ := newTestServer(t)
	router := srv.Router()
	ownerDevToken := registerAndLogin(t, router, "history-collab-owner", "password123")
	vaultID := defaultVaultIDFromAPI(t, router, ownerDevToken)
	ownerUserToken := userOnlyToken(t, router, "history-collab-owner", "password123")
	ownerDev := deviceTokenFor(t, router, "history-collab-owner", "password123", "history-owner-device", ownerUserToken, []string{vaultID})
	code, created := uploadV2(t, router, ownerDev, vaultID, "Shared.md", "first", 0, "history-owner-device", "create-shared")
	if code != http.StatusOK {
		t.Fatalf("create shared file: %d %v", code, created)
	}
	code, collaboratorLogin := doJSON(t, router, http.MethodPost, "/api/auth/register", "", map[string]string{
		"username": "history-collaborator", "password": "password123",
	})
	if code != http.StatusOK {
		t.Fatalf("register collaborator: %d", code)
	}
	collabUserToken := collaboratorLogin["token"].(string)
	// create collaborator device (even though not vault member, still need device for history read via collaboration)
	collabDev := deviceTokenFor(t, router, "history-collaborator", "password123", "collab-device", collabUserToken, []string{})
	var owner, collaborator models.User
	var file models.File
	if err := db.Where("username = ?", "history-collab-owner").First(&owner).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Where("username = ?", "history-collaborator").First(&collaborator).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Where("vault_id = ? AND path = ?", vaultID, "Shared.md").First(&file).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&models.Collaboration{
		VaultID: vaultID, FileID: file.ID, OwnerID: owner.ID,
		CollaboratorID: collaborator.ID, Status: collaboration.StatusAccepted,
	}).Error; err != nil {
		t.Fatal(err)
	}

	// Given: the user is an accepted collaborator for one file but not a Vault member.
	listPath := "/api/vaults/" + url.PathEscape(vaultID) + "/sync/history?path=Shared.md&client_id=collab-device"
	// When: the collaborator opens that file's history.
	code, list := doJSONAsDevice(t, router, http.MethodGet, listPath, collabDev, "collab-device", "collab-pc", nil)
	// Then: list and detail are readable, but restoration remains forbidden.
	if code != http.StatusOK {
		t.Fatalf("collaborator history list: %d %v", code, list)
	}
	rows := list["history"].([]any)
	if len(rows) != 1 {
		t.Fatalf("collaborator history rows = %d, want 1", len(rows))
	}
	historyID := uint64(rows[0].(map[string]any)["id"].(float64))
	detailPath := "/api/vaults/" + url.PathEscape(vaultID) + "/sync/history/" + strconv.FormatUint(historyID, 10) + "?mode=last&client_id=collab-device"
	code, detail := doJSONAsDevice(t, router, http.MethodGet, detailPath, collabDev, "collab-device", "collab-pc", nil)
	if code != http.StatusOK {
		t.Fatalf("collaborator history detail: %d %v", code, detail)
	}
	restorePath := "/api/vaults/" + url.PathEscape(vaultID) + "/sync/history/" + strconv.FormatUint(historyID, 10) + "/restore?client_id=collab-device"
	code, _ = doJSONAsDevice(t, router, http.MethodPost, restorePath, collabDev, "collab-device", "collab-pc", nil)
	if code == http.StatusOK {
		t.Fatal("collaborator must not restore owner history")
	}
}
