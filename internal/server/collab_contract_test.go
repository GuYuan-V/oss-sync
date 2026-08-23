package server

import (
	"bufio"
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"testing"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	"github.com/oss/oss-server/internal/collaboration"
	"github.com/oss/oss-server/internal/models"
)

type acceptedCollaborationFixture struct {
	router              *gin.Engine
	db                  *gorm.DB
	vaultID             string
	ownerToken          string
	collaboratorToken   string
	collaboratorVaultID string
	file                models.File
	row                 models.Collaboration
}

func newAcceptedCollaborationFixture(t *testing.T) acceptedCollaborationFixture {
	t.Helper()
	srv, db, _ := newTestServer(t)
	router := srv.Router()
	collaborator, err := registerUser(db, "collab-contract-user", "password123")
	if err != nil {
		t.Fatal(err)
	}
	// collaborator device login for vault creation
	code, loginBody := doJSON(t, router, http.MethodPost, "/api/auth/login", "",
		map[string]string{"username": collaborator.Username, "password": "password123"})
	if code != http.StatusOK {
		t.Fatalf("collaborator login: %d %v", code, loginBody)
	}
	collabUserToken := loginBody["token"].(string)
	code, devLogin := loginAsDevice(t, router, collaborator.Username, "password123", "collab-dev", "Collab Device")
	if code != http.StatusOK {
		t.Fatalf("collab device login: %d %v", code, devLogin)
	}
	collabDevToken := devLogin["token"].(string)
	code, _ = doJSON(t, router, http.MethodPut, "/api/devices/collab-dev/authorization", collabUserToken, map[string]any{"status": "approved", "vault_ids": []string{}})
	if code != http.StatusOK {
		t.Fatalf("approve collab dev: %d", code)
	}
	code, vaultBody := doJSON(t, router, http.MethodPost, "/api/vaults", collabDevToken,
		map[string]any{"name": "Collaborator Vault"})
	if code != http.StatusCreated {
		t.Fatalf("create collaborator vault: %d %v", code, vaultBody)
	}
	collaboratorToken := collabDevToken
	collaboratorVaultID, ok := vaultBody["id"].(string)
	if !ok || collaboratorVaultID == "" {
		t.Fatalf("collaborator vault id: %#v", vaultBody)
	}
	ownerToken := registerAndLogin(t, router, "collab-contract-owner", "password123")
	vaultID := defaultVaultIDFromAPI(t, router, ownerToken)
	uploadViaV1(t, router, ownerToken, "Shared.md", "# original content")

	var file models.File
	if err := db.Where("vault_id = ? AND path = ?", vaultID, "Shared.md").First(&file).Error; err != nil {
		t.Fatal(err)
	}
	code, body := doJSON(t, router, http.MethodPost,
		"/api/vaults/"+vaultID+"/collaborations", ownerToken,
		map[string]any{"file_path": file.Path, "username": collaborator.Username})
	if code != http.StatusOK {
		t.Fatalf("invite: %d %v", code, body)
	}

	var row models.Collaboration
	if err := db.Where("vault_id = ? AND file_id = ? AND collaborator_id = ?",
		vaultID, file.ID, collaborator.ID).First(&row).Error; err != nil {
		t.Fatal(err)
	}
	code, body = doJSON(t, router, http.MethodPost,
		"/api/vaults/"+vaultID+"/collaborations/"+strconv.FormatUint(uint64(row.ID), 10)+"/respond",
		collaboratorToken, map[string]any{"accept": true})
	if code != http.StatusOK {
		t.Fatalf("accept: %d %v", code, body)
	}
	row.Status = collaboration.StatusAccepted

	return acceptedCollaborationFixture{
		router:              router,
		db:                  db,
		vaultID:             vaultID,
		ownerToken:          ownerToken,
		collaboratorToken:   collaboratorToken,
		collaboratorVaultID: collaboratorVaultID,
		file:                file,
		row:                 row,
	}
}

func TestCollaborationAccountPollReportsCrossVaultEvents(t *testing.T) {
	// Given: a collaboration invitation and acceptance occurred in another user's Vault.
	fixture := newAcceptedCollaborationFixture(t)

	// When: the collaborator polls their account-wide event version.
	code, body := doJSON(t, fixture.router, http.MethodGet,
		"/api/collaborations/poll?after=0&wait=0", fixture.collaboratorToken, nil)

	// Then: the cross-Vault event is immediately visible.
	if code != http.StatusOK || body["changed"] != true {
		t.Fatalf("account collaboration poll: %d %#v", code, body)
	}
}

func TestCollaborationAccountPollWakesWhenOwnerUpdatesSharedFile(t *testing.T) {
	// Given: the collaborator has consumed the current account event version.
	fixture := newAcceptedCollaborationFixture(t)
	code, initial := doJSON(t, fixture.router, http.MethodGet,
		"/api/collaborations/poll?after=0&wait=0", fixture.collaboratorToken, nil)
	if code != http.StatusOK {
		t.Fatalf("initial account poll: %d %#v", code, initial)
	}
	version, ok := initial["version"].(float64)
	if !ok || version < 1 {
		t.Fatalf("initial account version: %#v", initial)
	}
	uploadViaV1(t, fixture.router, fixture.ownerToken, "Shared.md", "# owner edit")

	// When: the collaborator immediately polls after the consumed version.
	code, body := doJSON(t, fixture.router, http.MethodGet,
		"/api/collaborations/poll?after="+strconv.FormatInt(int64(version), 10)+"&wait=0",
		fixture.collaboratorToken, nil)

	// Then: the owner edit wakes the collaboration channel without waiting for the inbox timer.
	if code != http.StatusOK || body["changed"] != true {
		t.Fatalf("owner update account poll: %d %#v", code, body)
	}
}

func TestCollaborationLegacyBoundVaultPollWakesForCrossVaultEvents(t *testing.T) {
	// Given: an older client polls only the collaborator's own bound Vault.
	fixture := newAcceptedCollaborationFixture(t)

	// When: it polls after a cross-Vault invitation and acceptance.
	code, body := doJSON(t, fixture.router, http.MethodGet,
		"/api/vaults/"+fixture.collaboratorVaultID+"/collaborations/poll?after=0&wait=0",
		fixture.collaboratorToken, nil)

	// Then: the compatibility topic wakes it immediately.
	if code != http.StatusOK || body["changed"] != true {
		t.Fatalf("legacy collaboration poll: %d %#v", code, body)
	}
}

func TestCollaborationLegacyVaultListIncludesIncomingAcrossVaults(t *testing.T) {
	// Given: an older client is bound to the collaborator's own Vault.
	fixture := newAcceptedCollaborationFixture(t)

	// When: it loads collaborations through the legacy Vault-scoped route.
	code, body := doJSON(t, fixture.router, http.MethodGet,
		"/api/vaults/"+fixture.collaboratorVaultID+"/collaborations", fixture.collaboratorToken, nil)

	// Then: the collaboration from the owner's different Vault is still returned.
	if code != http.StatusOK {
		t.Fatalf("legacy collaboration list: %d %v", code, body)
	}
	rows, ok := body["collaborations"].([]any)
	if !ok || len(rows) != 1 {
		t.Fatalf("legacy collaboration list: %#v", body)
	}
	entry := rows[0].(map[string]any)
	if entry["vault_id"] != fixture.vaultID {
		t.Fatalf("legacy vault_id = %#v, want %q", entry["vault_id"], fixture.vaultID)
	}
}

func TestCollaborationListIncludesFileID(t *testing.T) {
	// Given: an accepted file collaboration exists.
	fixture := newAcceptedCollaborationFixture(t)

	// When: the collaborator loads the collaboration list.
	code, body := doJSON(t, fixture.router, http.MethodGet,
		"/api/vaults/"+fixture.vaultID+"/collaborations", fixture.collaboratorToken, nil)

	// Then: the response identifies the file used by download and upload endpoints.
	if code != http.StatusOK {
		t.Fatalf("collaboration list: %d %v", code, body)
	}
	rows, ok := body["collaborations"].([]any)
	if !ok || len(rows) != 1 {
		t.Fatalf("collaborations: %#v", body)
	}
	entry := rows[0].(map[string]any)
	if entry["file_id"] != float64(fixture.file.ID) {
		t.Fatalf("file_id = %#v, want %d", entry["file_id"], fixture.file.ID)
	}
}

func TestCollaborationContentAccessForAcceptedCollaborator(t *testing.T) {
	// Given: an accepted collaborator and an unrelated account exist.
	fixture := newAcceptedCollaborationFixture(t)
	if _, err := registerUser(fixture.db, "collab-contract-intruder", "password123"); err != nil {
		t.Fatal(err)
	}
	code, intruderLogin := loginAsDevice(t, fixture.router, "collab-contract-intruder", "password123", "intruder-dev", "Intruder Device")
	if code != http.StatusOK {
		t.Fatalf("intruder login: %d %v", code, intruderLogin)
	}
	intruderToken := intruderLogin["token"].(string)
	path := "/api/vaults/" + fixture.vaultID + "/collaborations/files/" +
		strconv.FormatUint(uint64(fixture.file.ID), 10) + "/content"

	// When: the collaborator downloads the shared file content.
	req := httptest.NewRequest(http.MethodGet, path, nil)
	req.Header.Set("Authorization", "Bearer "+fixture.collaboratorToken)
	response := performRequest(fixture.router, req)

	// Then: the accepted collaborator can read it, while unrelated and revoked users cannot.
	if response.Code != http.StatusOK || response.Body.String() != "# original content" {
		t.Fatalf("collaborator content: %d %q", response.Code, response.Body.String())
	}
	if response.Header().Get("X-OSS-Hash") == "" || response.Header().Get("X-OSS-Revision") == "" {
		t.Fatalf("missing collaboration content metadata: %#v", response.Header())
	}

	intruderReq := httptest.NewRequest(http.MethodGet, path, nil)
	intruderReq.Header.Set("Authorization", "Bearer "+intruderToken)
	if got := performRequest(fixture.router, intruderReq).Code; got != http.StatusForbidden {
		t.Fatalf("intruder content: %d, want 403", got)
	}

	code, body := doJSON(t, fixture.router, http.MethodPost,
		"/api/vaults/"+fixture.vaultID+"/collaborations/"+
			strconv.FormatUint(uint64(fixture.row.ID), 10)+"/revoke", fixture.ownerToken, nil)
	if code != http.StatusOK {
		t.Fatalf("revoke: %d %v", code, body)
	}
	revokedReq := httptest.NewRequest(http.MethodGet, path, nil)
	revokedReq.Header.Set("Authorization", "Bearer "+fixture.collaboratorToken)
	if got := performRequest(fixture.router, revokedReq).Code; got != http.StatusForbidden {
		t.Fatalf("revoked collaborator content: %d, want 403", got)
	}
}

func TestCollaborationLegacyBoundVaultCanDownloadAcceptedContent(t *testing.T) {
	// Given: an older client knows the file ID but remains bound to its own Vault.
	fixture := newAcceptedCollaborationFixture(t)
	path := "/api/vaults/" + fixture.collaboratorVaultID + "/collaborations/files/" +
		strconv.FormatUint(uint64(fixture.file.ID), 10) + "/content"
	req := httptest.NewRequest(http.MethodGet, path, nil)
	req.Header.Set("Authorization", "Bearer "+fixture.collaboratorToken)

	// When: it downloads through the legacy bound-Vault URL.
	response := performRequest(fixture.router, req)

	// Then: the accepted collaboration resolves to the file's actual owner Vault.
	if response.Code != http.StatusOK || response.Body.String() != "# original content" {
		t.Fatalf("legacy collaboration content: %d %q", response.Code, response.Body.String())
	}
}

func TestCollaborationLegacyBoundVaultCanUploadAcceptedContent(t *testing.T) {
	// Given: an older client edits an accepted collaboration while bound to its own Vault.
	fixture := newAcceptedCollaborationFixture(t)
	path := "/api/vaults/" + fixture.collaboratorVaultID + "/collaborations/files/" +
		strconv.FormatUint(uint64(fixture.file.ID), 10) + "/upload"

	// When: it uploads through the legacy bound-Vault URL.
	code, body := doJSON(t, fixture.router, http.MethodPost, path, fixture.collaboratorToken,
		map[string]any{
			"content":       "# collaborator edit",
			"base_revision": fixture.file.Revision,
			"operation_id":  "legacy-collab-upload",
		})

	// Then: the update is applied to the original collaboration file.
	if code != http.StatusOK {
		t.Fatalf("legacy collaboration upload: %d %v", code, body)
	}
	contentPath := "/api/vaults/" + fixture.vaultID + "/collaborations/files/" +
		strconv.FormatUint(uint64(fixture.file.ID), 10) + "/content"
	req := httptest.NewRequest(http.MethodGet, contentPath, nil)
	req.Header.Set("Authorization", "Bearer "+fixture.collaboratorToken)
	response := performRequest(fixture.router, req)
	if response.Code != http.StatusOK || response.Body.String() != "# collaborator edit" {
		t.Fatalf("updated collaboration content: %d %q", response.Code, response.Body.String())
	}
}

func TestCollaborationInboxListsIncomingAcrossVaults(t *testing.T) {
	// Given: a user accepted a collaboration in another owner's Vault.
	fixture := newAcceptedCollaborationFixture(t)

	// When: the collaborator loads their account-wide collaboration inbox.
	code, body := doJSON(t, fixture.router, http.MethodGet,
		"/api/collaborations", fixture.collaboratorToken, nil)

	// Then: the incoming collaboration is discoverable without knowing the owner's Vault ID.
	if code != http.StatusOK {
		t.Fatalf("collaboration inbox: %d %v", code, body)
	}
	rows, ok := body["collaborations"].([]any)
	if !ok || len(rows) != 1 {
		t.Fatalf("collaboration inbox: %#v", body)
	}
	entry := rows[0].(map[string]any)
	if entry["vault_id"] != fixture.vaultID {
		t.Fatalf("inbox vault_id = %#v, want %q", entry["vault_id"], fixture.vaultID)
	}
}

func TestAcceptedCollaboratorCanLeaveCollaboration(t *testing.T) {
	// Given: a collaborator has accepted a file collaboration.
	fixture := newAcceptedCollaborationFixture(t)
	path := "/api/vaults/" + fixture.vaultID + "/collaborations/" +
		strconv.FormatUint(uint64(fixture.row.ID), 10) + "/leave"

	// When: the collaborator actively leaves the collaboration.
	code, body := doJSON(t, fixture.router, http.MethodPost, path, fixture.collaboratorToken, nil)

	// Then: the relation is revoked and no longer grants content access.
	if code != http.StatusOK || body["status"] != "ok" {
		t.Fatalf("leave collaboration: %d %#v", code, body)
	}
	contentPath := "/api/vaults/" + fixture.vaultID + "/collaborations/files/" +
		strconv.FormatUint(uint64(fixture.file.ID), 10) + "/content"
	req := httptest.NewRequest(http.MethodGet, contentPath, nil)
	req.Header.Set("Authorization", "Bearer "+fixture.collaboratorToken)
	if got := performRequest(fixture.router, req).Code; got != http.StatusForbidden {
		t.Fatalf("left collaborator content: %d, want 403", got)
	}
}

func TestCollaborationSSEAllowsLoopbackQueryToken(t *testing.T) {
	// Given: an authenticated vault owner uses a local HTTP server.
	fixture := newAcceptedCollaborationFixture(t)
	server := httptest.NewServer(fixture.router)
	defer server.Close()
	path := "/api/vaults/" + fixture.vaultID + "/collaborations/stream?token=" +
		url.QueryEscape(fixture.ownerToken)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, server.URL+path, nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Host = "localhost:9090"
	req.Header.Set("Origin", "app://obsidian.md")

	// When: EventSource opens the stream with its query token.
	response, err := server.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()

	// Then: loopback HTTP is accepted and emits the ready event, but remote HTTP remains forbidden.
	if response.StatusCode != http.StatusOK {
		t.Fatalf("loopback SSE: %d, want 200", response.StatusCode)
	}
	if got := response.Header.Get("Access-Control-Allow-Origin"); got != "app://obsidian.md" {
		t.Fatalf("loopback SSE allow origin = %q, want app://obsidian.md", got)
	}
	line, err := bufio.NewReader(response.Body).ReadString('\n')
	if err != nil || line != "event: ready\n" {
		t.Fatalf("ready event: %q err=%v", line, err)
	}
	cancel()

	disallowedOriginReq := httptest.NewRequest(http.MethodGet,
		"/api/vaults/"+fixture.vaultID+"/collaborations/stream?token=invalid", nil)
	disallowedOriginReq.Host = "localhost:9090"
	disallowedOriginReq.RemoteAddr = "127.0.0.1:1234"
	disallowedOriginReq.Header.Set("Origin", "https://example.invalid")
	disallowedOriginResponse := performRequest(fixture.router, disallowedOriginReq)
	if got := disallowedOriginResponse.Header().Get("Access-Control-Allow-Origin"); got != "" {
		t.Fatalf("disallowed SSE allow origin = %q, want empty", got)
	}

	remoteReq := httptest.NewRequest(http.MethodGet, path, nil)
	remoteReq.Host = "sync.example.com"
	if got := performRequest(fixture.router, remoteReq).Code; got != http.StatusForbidden {
		t.Fatalf("remote HTTP SSE: %d, want 403", got)
	}
	spoofedReq := httptest.NewRequest(http.MethodGet, path, nil)
	spoofedReq.Host = "localhost:9090"
	spoofedReq.RemoteAddr = "198.51.100.2:1234"
	if got := performRequest(fixture.router, spoofedReq).Code; got != http.StatusForbidden {
		t.Fatalf("spoofed loopback host: %d, want 403", got)
	}
}

func TestCollaborationAccountSSEAllowsObsidianOrigin(t *testing.T) {
	// Given: an authenticated collaborator opens the account-wide stream from Obsidian.
	fixture := newAcceptedCollaborationFixture(t)
	server := httptest.NewServer(fixture.router)
	defer server.Close()
	path := "/api/collaborations/stream?token=" + url.QueryEscape(fixture.collaboratorToken)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, server.URL+path, nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Host = "localhost:9090"
	req.Header.Set("Origin", "app://obsidian.md")

	// When: the stream is opened.
	response, err := server.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()

	// Then: the account stream is CORS-readable and immediately emits ready.
	if response.StatusCode != http.StatusOK {
		t.Fatalf("account SSE: %d, want 200", response.StatusCode)
	}
	if got := response.Header.Get("Access-Control-Allow-Origin"); got != "app://obsidian.md" {
		t.Fatalf("account SSE allow origin = %q, want app://obsidian.md", got)
	}
	line, err := bufio.NewReader(response.Body).ReadString('\n')
	if err != nil || line != "event: ready\n" {
		t.Fatalf("account ready event: %q err=%v", line, err)
	}
	cancel()
}
