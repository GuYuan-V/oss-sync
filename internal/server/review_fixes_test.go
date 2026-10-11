package server

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/helantianshen/oss-sync/internal/jwt"
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

	// 上传真实图片，保证授权后能取到内容而非 404
	uploadFile(t, router, token, "pic.png", "PNGDATA", 1700000000000)
	assetUser := userOnlyToken(t, router, "review-asset-owner", "password123")
	assetDev := deviceTokenFor(t, router, "review-asset-owner", "password123", "review-asset-device", assetUser, []string{vaultID})
	code, _ := uploadV2(t, router, assetDev, vaultID, "WithImage.md",
		"# img\n\n![pic](pic.png)\n\nPROTECTED ARTICLE BODY", 0, "review-asset-device", "create-with-image")
	if code != http.StatusOK {
		t.Fatalf("upload article: %d", code)
	}
	code, shareBody := doJSON(t, router, "POST", "/api/shares", assetDev, map[string]any{
		"target_path": "WithImage.md", "is_folder": false, "visibility": "password", "share_password": "review-pass",
	})
	if code != http.StatusOK {
		t.Fatalf("create password share: %d %v", code, shareBody)
	}
	shareID := shareBody["share_id"].(string)

	// 首次带密码访问，校验通过并写入授权令牌
	req := httptest.NewRequest("GET", "/p/"+shareID+"?password=review-pass", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("password page: %d %s", w.Code, w.Body.String())
	}
	var grant *http.Cookie
	for _, c := range w.Result().Cookies() {
		if c.Name == "oss_share_access" {
			grant = c
		}
	}
	if grant == nil {
		t.Fatal("password verification did not issue a share access grant")
	}

	// 复用授权访问文章内图片，必须取到真实内容
	assetReq := httptest.NewRequest("GET", "/assets/"+shareID+"?ref=pic.png", nil)
	assetReq.AddCookie(grant)
	assetW := httptest.NewRecorder()
	router.ServeHTTP(assetW, assetReq)
	if assetW.Code != http.StatusOK {
		t.Fatalf("authorized asset = %d, want 200", assetW.Code)
	}
	if !strings.Contains(assetW.Body.String(), "PNGDATA") {
		t.Errorf("authorized asset body = %q, want PNGDATA", assetW.Body.String())
	}

	// 不带授权时资源请求仍被拒绝
	bareReq := httptest.NewRequest("GET", "/assets/"+shareID+"?ref=pic.png", nil)
	bareW := httptest.NewRecorder()
	router.ServeHTTP(bareW, bareReq)
	if bareW.Code != http.StatusUnauthorized {
		t.Errorf("asset without grant = %d, want 401", bareW.Code)
	}
}

// 授权令牌不可伪造：把公开分享 ID 直接当 Cookie 值必须被拒绝
func TestReviewPasswordCookieForgery(t *testing.T) {
	srv, _, _ := newTestServer(t)
	router := srv.Router()
	token := registerAndLogin(t, router, "review-forge-owner", "password123")
	vaultID := defaultVaultIDFromAPI(t, router, token)

	forgeUser := userOnlyToken(t, router, "review-forge-owner", "password123")
	forgeDev := deviceTokenFor(t, router, "review-forge-owner", "password123", "review-forge-device", forgeUser, []string{vaultID})
	code, _ := uploadV2(t, router, forgeDev, vaultID, "Forged.md",
		"# forged\n\nSECRET COOKIE BYPASS BODY", 0, "review-forge-device", "create-forged")
	if code != http.StatusOK {
		t.Fatalf("upload: %d", code)
	}
	code, shareBody := doJSON(t, router, "POST", "/api/shares", forgeDev, map[string]any{
		"target_path": "Forged.md", "is_folder": false, "visibility": "password", "share_password": "review-pass",
	})
	if code != http.StatusOK {
		t.Fatalf("create share: %d %v", code, shareBody)
	}
	shareID := shareBody["share_id"].(string)

	// 直接用公开分享 ID 构造 Cookie，未提交密码
	req := httptest.NewRequest("GET", "/p/"+shareID, nil)
	req.AddCookie(&http.Cookie{Name: "oss_share_access", Value: shareID})
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Code == http.StatusOK && strings.Contains(w.Body.String(), "SECRET COOKIE BYPASS BODY") {
		t.Error("forged share id cookie must not bypass the password check")
	}

	// 同一伪造值也不能用于附件接口
	assetReq := httptest.NewRequest("GET", "/assets/"+shareID+"?ref=pic.png", nil)
	assetReq.AddCookie(&http.Cookie{Name: "oss_share_access", Value: shareID})
	assetW := httptest.NewRecorder()
	router.ServeHTTP(assetW, assetReq)
	if assetW.Code != http.StatusUnauthorized {
		t.Errorf("forged cookie on asset = %d, want 401", assetW.Code)
	}
}

// 授权令牌不可篡改：改动载荷或签名都必须失效
func TestReviewPasswordGrantTampering(t *testing.T) {
	srv, _, secret := newTestServer(t)
	router := srv.Router()
	token := registerAndLogin(t, router, "review-tamper-owner", "password123")
	vaultID := defaultVaultIDFromAPI(t, router, token)

	tamperUser := userOnlyToken(t, router, "review-tamper-owner", "password123")
	tamperDev := deviceTokenFor(t, router, "review-tamper-owner", "password123", "review-tamper-device", tamperUser, []string{vaultID})
	code, _ := uploadV2(t, router, tamperDev, vaultID, "Tamper.md",
		"# tamper\n\nTAMPER BODY", 0, "review-tamper-device", "create-tamper")
	if code != http.StatusOK {
		t.Fatalf("upload: %d", code)
	}
	code, shareBody := doJSON(t, router, "POST", "/api/shares", tamperDev, map[string]any{
		"target_path": "Tamper.md", "is_folder": false, "visibility": "password", "share_password": "review-pass",
	})
	if code != http.StatusOK {
		t.Fatalf("create share: %d %v", code, shareBody)
	}
	shareID := shareBody["share_id"].(string)

	// 取一枚真实授权令牌
	req := httptest.NewRequest("GET", "/p/"+shareID+"?password=review-pass", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	var grant string
	for _, c := range w.Result().Cookies() {
		if c.Name == "oss_share_access" {
			grant = c.Value
		}
	}
	if grant == "" {
		t.Fatal("no grant issued")
	}

	// 篡改签名段
	parts := strings.Split(grant, ".")
	tampered := parts[0] + "." + parts[1] + "." + strings.Repeat("a", len(parts[2]))
	forged := httptest.NewRequest("GET", "/p/"+shareID, nil)
	forged.AddCookie(&http.Cookie{Name: "oss_share_access", Value: tampered})
	forgedW := httptest.NewRecorder()
	router.ServeHTTP(forgedW, forged)
	if forgedW.Code == http.StatusOK && strings.Contains(forgedW.Body.String(), "TAMPER BODY") {
		t.Error("tampered signature must be rejected")
	}

	// 用错误密钥签发的令牌同样无效
	wrongKey, err := jwt.Sign("not-the-server-secret", jwt.Claims{
		UserID: 1, Username: shareID, TokenVersion: 1,
		ExpAt: time.Now().Add(time.Hour).Unix(),
	}, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	foreign := httptest.NewRequest("GET", "/p/"+shareID, nil)
	foreign.AddCookie(&http.Cookie{Name: "oss_share_access", Value: wrongKey})
	foreignW := httptest.NewRecorder()
	router.ServeHTTP(foreignW, foreign)
	if foreignW.Code == http.StatusOK && strings.Contains(foreignW.Body.String(), "TAMPER BODY") {
		t.Error("token signed with a foreign key must be rejected")
	}

	// 过期令牌必须失效
	expired, err := jwt.Sign(secret, jwt.Claims{
		UserID: 1, Username: shareID, TokenVersion: 1,
		IssuedAt: time.Now().Add(-2 * time.Hour).Unix(),
		ExpAt:    time.Now().Add(-time.Hour).Unix(),
	}, -time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	expiredReq := httptest.NewRequest("GET", "/p/"+shareID, nil)
	expiredReq.AddCookie(&http.Cookie{Name: "oss_share_access", Value: expired})
	expiredW := httptest.NewRecorder()
	router.ServeHTTP(expiredW, expiredReq)
	if expiredW.Code == http.StatusOK && strings.Contains(expiredW.Body.String(), "TAMPER BODY") {
		t.Error("expired grant must be rejected")
	}
}

// 改密后此前签发的授权令牌必须失效
func TestReviewPasswordGrantRevokedByPasswordChange(t *testing.T) {
	srv, _, _ := newTestServer(t)
	router := srv.Router()
	token := registerAndLogin(t, router, "review-revoke-owner", "password123")
	vaultID := defaultVaultIDFromAPI(t, router, token)
	revokeUser := userOnlyToken(t, router, "review-revoke-owner", "password123")
	revokeDev := deviceTokenFor(t, router, "review-revoke-owner", "password123", "review-revoke-device", revokeUser, []string{vaultID})

	code, _ := uploadV2(t, router, revokeDev, vaultID, "Revoke.md",
		"# revoke\n\nREVOKE BODY", 0, "review-revoke-device", "create-revoke")
	if code != http.StatusOK {
		t.Fatalf("upload: %d", code)
	}
	code, shareBody := doJSON(t, router, "POST", "/api/shares", revokeDev, map[string]any{
		"target_path": "Revoke.md", "is_folder": false, "visibility": "password", "share_password": "old-pass",
	})
	if code != http.StatusOK {
		t.Fatalf("create share: %d %v", code, shareBody)
	}
	shareID := shareBody["share_id"].(string)

	// 用旧密码取得授权令牌
	req := httptest.NewRequest("GET", "/p/"+shareID+"?password=old-pass", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	var grant *http.Cookie
	for _, c := range w.Result().Cookies() {
		if c.Name == "oss_share_access" {
			grant = c
		}
	}
	if grant == nil {
		t.Fatal("no grant issued")
	}

	// 改密，旧令牌应随之失效
	code, _ = doJSON(t, router, "PATCH", "/api/shares/"+shareID, revokeDev, map[string]any{
		"visibility": "password", "share_password": "new-pass",
	})
	if code != http.StatusOK {
		t.Fatalf("change share password: %d", code)
	}

	stale := httptest.NewRequest("GET", "/p/"+shareID, nil)
	stale.AddCookie(grant)
	staleW := httptest.NewRecorder()
	router.ServeHTTP(staleW, stale)
	if staleW.Code == http.StatusOK && strings.Contains(staleW.Body.String(), "REVOKE BODY") {
		t.Error("grant issued before a password change must not survive it")
	}

	// 新密码可以重新取得授权
	fresh := httptest.NewRequest("GET", "/p/"+shareID+"?password=new-pass", nil)
	freshW := httptest.NewRecorder()
	router.ServeHTTP(freshW, fresh)
	if freshW.Code != http.StatusOK {
		t.Fatalf("new password should work: %d", freshW.Code)
	}
}

// 授权令牌绑定单一分享：A 分享的令牌不能用于 B 分享
func TestReviewPasswordGrantCrossShare(t *testing.T) {
	srv, _, _ := newTestServer(t)
	router := srv.Router()
	token := registerAndLogin(t, router, "review-cross-owner", "password123")
	vaultID := defaultVaultIDFromAPI(t, router, token)
	crossUser := userOnlyToken(t, router, "review-cross-owner", "password123")
	crossDev := deviceTokenFor(t, router, "review-cross-owner", "password123", "review-cross-device", crossUser, []string{vaultID})

	aShare, bShare := "", ""
	for _, path := range []string{"A.md", "B.md"} {
		code, _ := uploadV2(t, router, crossDev, vaultID, path,
			"# "+path+"\n\nBODY "+path, 0, "review-cross-device", "create-"+path)
		if code != http.StatusOK {
			t.Fatalf("upload %s: %d", path, code)
		}
		code, body := doJSON(t, router, "POST", "/api/shares", crossDev, map[string]any{
			"target_path": path, "is_folder": false, "visibility": "password", "share_password": "review-pass",
		})
		if code != http.StatusOK {
			t.Fatalf("create share %s: %d %v", path, code, body)
		}
		if path == "A.md" {
			aShare, _ = body["share_id"].(string)
		} else {
			bShare, _ = body["share_id"].(string)
		}
	}

	// 只对 A 通过密码校验，取得 A 的授权令牌
	req := httptest.NewRequest("GET", "/p/"+aShare+"?password=review-pass", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	var grant *http.Cookie
	for _, c := range w.Result().Cookies() {
		if c.Name == "oss_share_access" {
			grant = c
		}
	}
	if grant == nil {
		t.Fatal("no grant issued for A")
	}

	// 用 A 的令牌访问 B，必须被拒绝
	crossReq := httptest.NewRequest("GET", "/p/"+bShare, nil)
	crossReq.AddCookie(grant)
	crossW := httptest.NewRecorder()
	router.ServeHTTP(crossW, crossReq)
	if crossW.Code == http.StatusOK && strings.Contains(crossW.Body.String(), "BODY B.md") {
		t.Error("grant for one share must not unlock another share")
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
