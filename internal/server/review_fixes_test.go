package server

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/helantianshen/oss-sync/internal/models"
)

// createVisibilityShare 上传一篇 Markdown 后按指定可见性建分享，返回 shareID
// dev 由调用方复用同一设备身份，避免多次设备登录触发限流
func createVisibilityShare(t *testing.T, router *gin.Engine, devToken, clientID, vaultID, path, content, visibility string) string {
	t.Helper()
	if code, body := uploadV2(t, router, devToken, vaultID, path, content, 0, clientID, "op-"+path); code != http.StatusOK {
		t.Fatalf("upload %s: %d %v", path, code, body)
	}
	payload := map[string]any{
		"target_path": path, "is_folder": false, "allow_copy": true, "visibility": visibility,
	}
	if visibility == "password" {
		payload["share_password"] = "review-pass"
	}
	code, body := doJSON(t, router, "POST", "/api/shares", devToken, payload)
	if code != http.StatusOK {
		t.Fatalf("create share: %d %v", code, body)
	}
	id, _ := body["share_id"].(string)
	if id == "" {
		t.Fatalf("share id missing: %v", body)
	}
	return id
}

// getPublicHome 以匿名身份请求公开博客首页，返回响应体
func getPublicHome(t *testing.T, router *gin.Engine, vaultID string) (int, string) {
	t.Helper()
	req := httptest.NewRequest("GET", "/b/"+vaultID, nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	return w.Code, w.Body.String()
}

// 公开首页与文章页都不得泄露受保护分享的正文摘要
func TestReviewPR13ProtectedSummary(t *testing.T) {
	srv, db, _ := newTestServer(t)
	router := srv.Router()
	token := registerAndLogin(t, router, "review-owner", "password123")
	vaultID := defaultVaultIDFromAPI(t, router, token)

	var vault models.Vault
	if err := db.Where("id = ?", vaultID).First(&vault).Error; err != nil {
		t.Fatal(err)
	}
	var owner models.User
	if err := db.Where("username = ?", "review-owner").First(&owner).Error; err != nil {
		t.Fatal(err)
	}
	var vs models.VaultSetting
	if err := db.Where("vault_id = ?", vaultID).First(&vs).Error; err != nil {
		if err := db.Create(&models.VaultSetting{VaultID: vaultID, IsPublicBlog: true}).Error; err != nil {
			t.Fatal(err)
		}
	} else if err := db.Model(&models.VaultSetting{}).Where("vault_id = ?", vaultID).
		Update("is_public_blog", true).Error; err != nil {
		t.Fatal(err)
	}
	// papertrail 主题的首页模板渲染文章列表，是唯一能让摘要出现在 HTML 中的路径
	if err := db.Model(&models.VaultSetting{}).Where("vault_id = ?", vaultID).
		Update("theme_name", "papertrail").Error; err != nil {
		t.Fatal(err)
	}

	ownerUser := userOnlyToken(t, router, "review-owner", "password123")
	ownerDev := deviceTokenFor(t, router, "review-owner", "password123", "review-upload-device", ownerUser, []string{vaultID})

	const secret = "SECRET PRIVATE CONTENT"
	protected := map[string]string{}
	for _, visibility := range []string{"password", "login"} {
		protected[visibility] = createVisibilityShare(t, router, ownerDev, "review-upload-device", vaultID,
			"Secret-"+visibility+".md", "# "+visibility+"\n\n"+secret, visibility)
	}
	publicID := createVisibilityShare(t, router, ownerDev, "review-upload-device", vaultID,
		"Open.md", "# open\n\nPUBLIC BODY", "public")

	// 匿名访问公开博客首页，受保护分享的正文摘要不得出现
	code, body := getPublicHome(t, router, vaultID)
	if code != http.StatusOK {
		t.Fatalf("public home: %d %s", code, body)
	}
	if strings.Contains(body, secret) {
		t.Error("protected share summary leaked into public home")
	}

	// 匿名访问受保护分享的文章页：password 要求密码，login 要求登录，均不得返回正文
	for _, visibility := range []string{"password", "login"} {
		req := httptest.NewRequest("GET", "/p/"+protected[visibility], nil)
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)
		if w.Code == http.StatusOK && strings.Contains(w.Body.String(), secret) {
			t.Errorf("%s share body leaked to anonymous visitor", visibility)
		}
	}

	// 公开分享对匿名访客可读，作为对照确认链路本身可用
	req := httptest.NewRequest("GET", "/p/"+publicID, nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("public share page: %d", w.Code)
	}
	if !strings.Contains(w.Body.String(), "PUBLIC BODY") {
		t.Error("public share body should be readable by anonymous visitor")
	}
}

// 登录分享必须复用控制台认证链路，改密后的旧 TokenVersion 令牌不能继续访问
func TestReviewPR13RevokedLogin(t *testing.T) {
	srv, db, _ := newTestServer(t)
	router := srv.Router()
	token := registerAndLogin(t, router, "review-login-owner", "password123")
	vaultID := defaultVaultIDFromAPI(t, router, token)
	loginUser := userOnlyToken(t, router, "review-login-owner", "password123")
	loginDev := deviceTokenFor(t, router, "review-login-owner", "password123", "review-login-device", loginUser, []string{vaultID})
	shareID := createVisibilityShare(t, router, loginDev, "review-login-device", vaultID, "Login.md", "# login\n\nLOGIN BODY", "login")

	code, loginBody := doJSON(t, router, "POST", "/api/auth/login", "", map[string]any{
		"username": "review-login-owner", "password": "password123",
	})
	if code != http.StatusOK {
		t.Fatalf("login: %d %v", code, loginBody)
	}
	session := loginBody["token"].(string)

	var user models.User
	if err := db.Where("username = ?", "review-login-owner").First(&user).Error; err != nil {
		t.Fatal(err)
	}
	// 改密后 TokenVersion 递增，旧会话令牌随之失效
	if err := db.Model(&models.User{}).Where("id = ?", user.ID).
		Update("token_version", user.TokenVersion+1).Error; err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest("GET", "/p/"+shareID, nil)
	req.AddCookie(&http.Cookie{Name: "oss_web_session", Value: session})
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Code == http.StatusOK && strings.Contains(w.Body.String(), "LOGIN BODY") {
		t.Error("stale session token must not read a login share")
	}
}

// 密码分享通过校验后，同浏览器会话内的资源请求不再被拒绝
func TestReviewPR13PasswordAssets(t *testing.T) {
	srv, _, _ := newTestServer(t)
	router := srv.Router()
	token := registerAndLogin(t, router, "review-asset-owner", "password123")
	vaultID := defaultVaultIDFromAPI(t, router, token)
	assetUser := userOnlyToken(t, router, "review-asset-owner", "password123")
	assetDev := deviceTokenFor(t, router, "review-asset-owner", "password123", "review-asset-device", assetUser, []string{vaultID})
	shareID := createVisibilityShare(t, router, assetDev, "review-asset-device", vaultID, "WithImage.md",
		"# img\n\n![pic](pic.png)\n\nPROTECTED ARTICLE BODY", "password")

	// 首次带密码访问，校验通过并写入授权 cookie
	req := httptest.NewRequest("GET", "/p/"+shareID+"?password=review-pass", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("password page: %d %s", w.Code, w.Body.String())
	}

	// 复用同一会话访问文章内图片地址，不得再要求密码
	assetReq := httptest.NewRequest("GET", "/assets/"+shareID+"?ref=pic.png", nil)
	for _, c := range w.Result().Cookies() {
		if c.Name == "oss_share_access" {
			assetReq.AddCookie(c)
		}
	}
	assetW := httptest.NewRecorder()
	router.ServeHTTP(assetW, assetReq)
	if assetW.Code == http.StatusUnauthorized {
		t.Error("asset request should reuse the share access grant")
	}

	// 不带授权 cookie 时资源请求仍被拒绝
	bareReq := httptest.NewRequest("GET", "/assets/"+shareID+"?ref=pic.png", nil)
	bareW := httptest.NewRecorder()
	router.ServeHTTP(bareW, bareReq)
	if bareW.Code != http.StatusUnauthorized {
		t.Errorf("asset without grant should be rejected, got %d", bareW.Code)
	}
}

// 未授权协作者读不到历史，授权后才可读
func TestReviewPR13HistoryGrantBoundary(t *testing.T) {
	srv, db, _ := newTestServer(t)
	router := srv.Router()
	token := registerAndLogin(t, router, "review-history-owner", "password123")
	vaultID := defaultVaultIDFromAPI(t, router, token)

	code, collaborator := doJSON(t, router, "POST", "/api/auth/register", "", map[string]any{
		"username": "review-history-collaborator", "password": "password123",
	})
	if code != http.StatusOK {
		t.Fatalf("register collaborator: %d", code)
	}
	collabToken := collaborator["token"].(string)

	historyUser := userOnlyToken(t, router, "review-history-owner", "password123")
	historyDev := deviceTokenFor(t, router, "review-history-owner", "password123", "review-history-device", historyUser, []string{vaultID})
	code, _ = uploadV2(t, router, historyDev, vaultID, "Shared.md", "first", 0, "review-history-device", "create-shared")
	if code != http.StatusOK {
		t.Fatalf("upload shared file: %d", code)
	}

	var owner, guest models.User
	var file models.File
	if err := db.Where("username = ?", "review-history-owner").First(&owner).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Where("username = ?", "review-history-collaborator").First(&guest).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Where("vault_id = ? AND path = ?", vaultID, "Shared.md").First(&file).Error; err != nil {
		t.Fatal(err)
	}

	historyPath := "/api/vaults/" + url.PathEscape(vaultID) + "/sync/history?path=Shared.md&client_id=review-history-collab-device"
	collabDev := deviceTokenFor(t, router, "review-history-collaborator", "password123", "review-history-collab-device", collabToken, []string{})

	// 无协作关系时读不到历史
	code, _ = doJSON(t, router, "GET", historyPath, collabDev, nil)
	if code != http.StatusNotFound {
		t.Fatalf("history without collaboration = %d, want 404", code)
	}

	if err := db.Create(&models.Collaboration{
		VaultID: vaultID, FileID: file.ID, OwnerID: owner.ID,
		CollaboratorID: guest.ID, Status: "accepted",
	}).Error; err != nil {
		t.Fatal(err)
	}
	// 未授予历史权限时依然读不到
	code, _ = doJSON(t, router, "GET", historyPath, collabDev, nil)
	if code != http.StatusNotFound {
		t.Fatalf("history without grant = %d, want 404", code)
	}

	if err := db.Model(&models.Collaboration{}).
		Where("vault_id = ? AND file_id = ? AND collaborator_id = ?", vaultID, file.ID, guest.ID).
		Update("allow_history_view", true).Error; err != nil {
		t.Fatal(err)
	}
	code, body := doJSON(t, router, "GET", historyPath, collabDev, nil)
	if code != http.StatusOK {
		t.Fatalf("history with grant = %d %v", code, body)
	}
}
