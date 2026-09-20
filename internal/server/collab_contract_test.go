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

	"github.com/helantianshen/oss-sync/internal/collaboration"
	"github.com/helantianshen/oss-sync/internal/models"
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
	// 协作者以设备身份登录，用于创建所属仓库。
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
	// 另一用户 Vault 内已完成协作邀请与接受。
	fixture := newAcceptedCollaborationFixture(t)

	// 协作者轮询账号级事件版本。
	code, body := doJSON(t, fixture.router, http.MethodGet,
		"/api/collaborations/poll?after=0&wait=0", fixture.collaboratorToken, nil)

	// 跨 Vault 事件立即可见。
	if code != http.StatusOK || body["changed"] != true {
		t.Fatalf("account collaboration poll: %d %#v", code, body)
	}
}

func TestCollaborationAccountPollWakesWhenOwnerUpdatesSharedFile(t *testing.T) {
	// 协作者已消费当前账号事件版本。
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

	// 协作者紧随已消费版本立即轮询。
	code, body := doJSON(t, fixture.router, http.MethodGet,
		"/api/collaborations/poll?after="+strconv.FormatInt(int64(version), 10)+"&wait=0",
		fixture.collaboratorToken, nil)

	// 归属者编辑直接唤醒协作通道，不等待收件箱定时器。
	if code != http.StatusOK || body["changed"] != true {
		t.Fatalf("owner update account poll: %d %#v", code, body)
	}
}

func TestCollaborationLegacyBoundVaultPollWakesForCrossVaultEvents(t *testing.T) {
	// 旧客户端仅轮询协作者自绑 Vault。
	fixture := newAcceptedCollaborationFixture(t)

	// 跨 Vault 邀请与接受发生后发起轮询。
	code, body := doJSON(t, fixture.router, http.MethodGet,
		"/api/vaults/"+fixture.collaboratorVaultID+"/collaborations/poll?after=0&wait=0",
		fixture.collaboratorToken, nil)

	// 兼容主题立即唤醒旧客户端。
	if code != http.StatusOK || body["changed"] != true {
		t.Fatalf("legacy collaboration poll: %d %#v", code, body)
	}
}

func TestCollaborationLegacyVaultListIncludesIncomingAcrossVaults(t *testing.T) {
	// 旧客户端绑定协作者自有 Vault。
	fixture := newAcceptedCollaborationFixture(t)

	// 经历史 Vault 作用域路由加载协作列表。
	code, body := doJSON(t, fixture.router, http.MethodGet,
		"/api/vaults/"+fixture.collaboratorVaultID+"/collaborations", fixture.collaboratorToken, nil)

	// 归属者另一 Vault 的协作仍被返回。
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
	// 已存在一条接受态文件协作。
	fixture := newAcceptedCollaborationFixture(t)

	// 协作者加载协作列表。
	code, body := doJSON(t, fixture.router, http.MethodGet,
		"/api/vaults/"+fixture.vaultID+"/collaborations", fixture.collaboratorToken, nil)

	// 响应给出下载与上传接口所用的文件标识。
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
	// 已有接受态协作者，另备一个无关账号。
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

	// 协作者下载共享文件正文。
	req := httptest.NewRequest(http.MethodGet, path, nil)
	req.Header.Set("Authorization", "Bearer "+fixture.collaboratorToken)
	response := performRequest(fixture.router, req)

	// 接受态协作者可读，无关与已撤回用户不可读。
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
	// 旧客户端已知文件 ID，但仍绑定自有 Vault。
	fixture := newAcceptedCollaborationFixture(t)
	path := "/api/vaults/" + fixture.collaboratorVaultID + "/collaborations/files/" +
		strconv.FormatUint(uint64(fixture.file.ID), 10) + "/content"
	req := httptest.NewRequest(http.MethodGet, path, nil)
	req.Header.Set("Authorization", "Bearer "+fixture.collaboratorToken)

	// 经历史绑定 Vault 地址下载。
	response := performRequest(fixture.router, req)

	// 已接受协作解析到文件实际归属 Vault。
	if response.Code != http.StatusOK || response.Body.String() != "# original content" {
		t.Fatalf("legacy collaboration content: %d %q", response.Code, response.Body.String())
	}
}

func TestCollaborationLegacyBoundVaultCanUploadAcceptedContent(t *testing.T) {
	// 旧客户端在绑定自有 Vault 下编辑已接受协作。
	fixture := newAcceptedCollaborationFixture(t)
	path := "/api/vaults/" + fixture.collaboratorVaultID + "/collaborations/files/" +
		strconv.FormatUint(uint64(fixture.file.ID), 10) + "/upload"

	// 经历史绑定 Vault 地址上传。
	code, body := doJSON(t, fixture.router, http.MethodPost, path, fixture.collaboratorToken,
		map[string]any{
			"content":       "# collaborator edit",
			"base_revision": fixture.file.Revision,
			"operation_id":  "legacy-collab-upload",
		})

	// 更新落到原始协作文件。
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
	// 用户在另一归属者 Vault 内接受协作。
	fixture := newAcceptedCollaborationFixture(t)

	// 协作者加载账号级协作收件箱。
	code, body := doJSON(t, fixture.router, http.MethodGet,
		"/api/collaborations", fixture.collaboratorToken, nil)

	// 无需知道归属者 Vault ID 即可发现待处理协作。
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
	// 协作者已接受文件协作。
	fixture := newAcceptedCollaborationFixture(t)
	path := "/api/vaults/" + fixture.vaultID + "/collaborations/" +
		strconv.FormatUint(uint64(fixture.row.ID), 10) + "/leave"

	// 协作者主动离开协作。
	code, body := doJSON(t, fixture.router, http.MethodPost, path, fixture.collaboratorToken, nil)

	// 协作关系撤销，不再授予正文访问。
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
	// 已认证 Vault 归属者使用本地 HTTP 服务。
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

	// EventSource 以查询 token 建流。
	response, err := server.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()

	// 本地回环 HTTP 放行并首发 ready 事件，远端 HTTP 仍拒绝。
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
	// 已认证协作者从 Obsidian 打开账号级推送流。
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

	// 建流。
	response, err := server.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()

	// 账号流允许 CORS 读取并立即发出 ready 事件。
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
