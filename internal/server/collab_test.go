package server

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	"github.com/oss/oss-server/internal/auth"
	"github.com/oss/oss-server/internal/collaboration"
	"github.com/oss/oss-server/internal/deviceauth"
	"github.com/oss/oss-server/internal/models"
)

func registerUser(db *gorm.DB, username, password string) (*models.User, error) {
	return auth.CreateAccount(db, username, password, "user")
}

func newDeviceLoginRequest(username, password, clientID, deviceName string) *http.Request {
	body := fmt.Sprintf(`{"username":%q,"password":%q}`, username, password)
	req := httptest.NewRequest(http.MethodPost, "/api/auth/login", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set(deviceauth.ClientIDHeader, clientID)
	req.Header.Set(deviceauth.DeviceNameHeader, deviceName)
	return req
}

func performRequest(router *gin.Engine, req *http.Request) *httptest.ResponseRecorder {
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	return w
}

func readFileContent(dataDir, vaultID, path string) ([]byte, error) {
	return os.ReadFile(filepath.Join(dataDir, "vaults", vaultID, "files", filepath.FromSlash(path)))
}

// approveDeviceForVault 注册并批准设备，授权仓库，返回 client_id。
func approveDeviceForVault(t *testing.T, router *gin.Engine, token, clientID, vaultID string) {
	t.Helper()
	// 通过带设备头的登录请求登记设备。
	req := newDeviceLoginRequest("strat-owner", "password123", clientID, "测试设备")
	w := performRequest(router, req)
	if w.Code != http.StatusOK {
		t.Fatalf("device login: %d %s", w.Code, w.Body.String())
	}
	code, authzBody := doJSON(t, router, http.MethodPut,
		"/api/devices/"+clientID+"/authorization", token,
		map[string]any{"status": "approved", "vault_ids": []string{vaultID}})
	if code != http.StatusNoContent && code != http.StatusOK {
		t.Fatalf("approve device: %d %v", code, authzBody)
	}
}

func TestVaultSyncStrategy(t *testing.T) {
	srv, db, _ := newTestServer(t)
	router := srv.Router()
	devToken := registerAndLogin(t, router, "strat-owner", "password123")
	vaultID := defaultVaultIDFromAPI(t, router, devToken)
	userToken := userOnlyToken(t, router, "strat-owner", "password123")
	stratToken := deviceTokenFor(t, router, "strat-owner", "password123", "strat-device", userToken, []string{vaultID})

	// 默认 user_choice：客户端选择生效。
	code, body := doJSONAsDevice(t, router, http.MethodGet,
		"/api/vaults/"+vaultID+"/sync/strategy?client_id=strat-device&mode=long_poll", stratToken, "strat-device", "strat-device", nil)
	if code != http.StatusOK {
		t.Fatalf("strategy: %d %v", code, body)
	}
	if body["policy"] != "user_choice" || body["effective_mode"] != "long_poll" {
		t.Fatalf("strategy user_choice: %v", body)
	}
	if body["min_debounce_sec"].(float64) < 3 || body["long_poll_wait_sec"].(float64) != 30 {
		t.Fatalf("strategy limits: %v", body)
	}

	// 管理员强制 short_poll。
	if err := db.Model(&models.SystemSetting{}).Where("id = 1").Update("sync_mode", "short_poll").Error; err != nil {
		t.Fatal(err)
	}
	code, body = doJSONAsDevice(t, router, http.MethodGet,
		"/api/vaults/"+vaultID+"/sync/strategy?client_id=strat-device&mode=long_poll", stratToken, "strat-device", "strat-device", nil)
	if code != http.StatusOK {
		t.Fatalf("strategy forced: %d", code)
	}
	if body["policy"] != "short_poll" || body["effective_mode"] != "short_poll" {
		t.Fatalf("forced short_poll: %v", body)
	}
}

func TestCollaborationInviteUploadAndEvents(t *testing.T) {
	srv, db, dataDir := newTestServer(t)
	router := srv.Router()
	// collab-user 需先注册（owner 由 registerAndLogin 注册）。
	if _, err := registerUser(db, "collab-user", "password123"); err != nil {
		t.Fatal(err)
	}
	token := registerAndLogin(t, router, "owner", "password123")
	vaultID := defaultVaultIDFromAPI(t, router, token)
	uploadViaV1(t, router, token, "Shared.md", "# 原始内容")
	var file models.File
	if err := db.Where("vault_id = ? AND path = ?", vaultID, "Shared.md").First(&file).Error; err != nil {
		t.Fatal(err)
	}

	// owner 邀请 collab-user。
	invite, _ := doJSON(t, router, http.MethodPost,
		"/api/vaults/"+vaultID+"/collaborations", token,
		map[string]any{"file_path": "Shared.md", "username": "collab-user"})
	if invite != http.StatusOK {
		t.Fatalf("invite: %d", invite)
	}
	var collab models.Collaboration
	if err := db.Where("vault_id = ? AND file_id = ? AND collaborator_id = ?",
		vaultID, file.ID, 1).First(&collab).Error; err != nil {
		t.Fatal(err)
	}
	if collab.Status != collaboration.StatusPending {
		t.Fatalf("status = %q", collab.Status)
	}

	// 重复邀请应 409。
	dup, _ := doJSON(t, router, http.MethodPost,
		"/api/vaults/"+vaultID+"/collaborations", token,
		map[string]any{"file_path": "Shared.md", "username": "collab-user"})
	if dup != http.StatusConflict {
		t.Fatalf("duplicate invite: %d, want 409", dup)
	}

	// collab-user 登录并接受。
	code, devLogin := loginAsDevice(t, router, "collab-user", "password123", "collab-device", "Collab Device")
	if code != http.StatusOK {
		t.Fatalf("collab device login: %d %v", code, devLogin)
	}
	collabToken := devLogin["token"].(string)
	code, _ = doJSON(t, router, http.MethodPut, "/api/devices/collab-device/authorization", collabToken, map[string]any{"status": "approved", "vault_ids": []string{}})
	respond, _ := doJSON(t, router, http.MethodPost,
		"/api/vaults/"+vaultID+"/collaborations/"+strconv.FormatUint(uint64(collab.ID), 10)+"/respond", collabToken,
		map[string]any{"accept": true})
	if respond != http.StatusOK {
		t.Fatalf("respond: %d", respond)
	}
	if err := db.Where("id = ?", collab.ID).First(&collab).Error; err != nil || collab.Status != collaboration.StatusAccepted {
		t.Fatalf("accepted: %#v err=%v", collab, err)
	}

	// collab-user 上传正文。
	up, _ := doJSON(t, router, http.MethodPost,
		"/api/vaults/"+vaultID+"/collaborations/files/"+strconv.FormatUint(uint64(file.ID), 10)+"/upload", collabToken,
		map[string]any{
			"content":       "# 协作者更新",
			"base_revision": file.Revision,
			"operation_id":  "collab-test-upload",
		})
	if up != http.StatusOK {
		t.Fatalf("collab upload: %d", up)
	}
	// 原文件内容已更新，历史记录含协作者。
	updated, _ := readFileContent(dataDir, vaultID, "Shared.md")
	if string(updated) != "# 协作者更新" {
		t.Fatalf("collab file content: %q", updated)
	}
	var hist models.FileHistory
	if err := db.Where("vault_id = ? AND file_path = ? AND username = ?",
		vaultID, "Shared.md", "collab-user").Order("id desc").First(&hist).Error; err != nil {
		t.Fatal(err)
	}

	// 长轮询协作事件。
	poll, _ := doJSON(t, router, http.MethodGet,
		"/api/vaults/"+vaultID+"/collaborations/poll?after=0&wait=1", collabToken, nil)
	if poll != http.StatusOK {
		t.Fatalf("poll: %d", poll)
	}

	// 非协作者不能上传。
	if _, err := registerUser(db, "intruder", "password123"); err != nil {
		t.Fatal(err)
	}
	code, intruderLogin := loginAsDevice(t, router, "intruder", "password123", "intruder-dev", "Intruder Device")
	if code != http.StatusOK {
		t.Fatalf("intruder device login: %d %v", code, intruderLogin)
	}
	intruderToken := intruderLogin["token"].(string)
	// approve intruder device so it reaches collaboration check
	code, _ = doJSON(t, router, http.MethodPut, "/api/devices/intruder-dev/authorization", intruderToken, map[string]any{"status": "approved", "vault_ids": []string{}})
	if code != http.StatusOK {
		t.Fatalf("approve intruder: %d", code)
	}
	forbidden, _ := doJSON(t, router, http.MethodPost,
		"/api/vaults/"+vaultID+"/collaborations/files/"+strconv.FormatUint(uint64(file.ID), 10)+"/upload", intruderToken,
		map[string]any{"content": "x"})
	if forbidden != http.StatusForbidden {
		t.Fatalf("intruder upload: %d, want 403", forbidden)
	}

	// owner 撤回协作。
	revoke, _ := doJSON(t, router, http.MethodPost,
		"/api/vaults/"+vaultID+"/collaborations/"+strconv.FormatUint(uint64(collab.ID), 10)+"/revoke", token, nil)
	if revoke != http.StatusOK {
		t.Fatalf("revoke: %d", revoke)
	}
	if err := db.Where("id = ?", collab.ID).First(&collab).Error; err != nil || collab.Status != collaboration.StatusRevoked {
		t.Fatalf("revoked: %#v err=%v", collab, err)
	}

	// SSE 端点可用（EventSource 场景用 token 查询参数；HTTP 下应拒绝）。
	sse := doForm(t, router, http.MethodGet,
		"/api/vaults/"+vaultID+"/collaborations/stream?token=abc&client_id=x", nil, nil)
	if sse.Code != http.StatusForbidden {
		t.Fatalf("sse http token: %d, want 403 (non-HTTPS)", sse.Code)
	}
}
