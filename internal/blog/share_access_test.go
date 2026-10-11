package blog

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/helantianshen/oss-sync/internal/config"
	"github.com/helantianshen/oss-sync/internal/jwt"
	"github.com/helantianshen/oss-sync/internal/models"
)

func TestShareAccessPasswordBindingAndTokenIsolation(t *testing.T) {
	cfg := &config.Config{}
	cfg.Auth.JWTSecret = "test-account-secret"
	h := &Handler{Cfg: cfg}
	share := models.Share{ShareID: "test-share", UserID: 1, SharePassword: "old-hash", UpdatedAt: time.Now()}
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest("GET", "/p/test-share", nil)
	h.grantShareAccess(c, share)
	cookies := recorder.Result().Cookies()
	if len(cookies) != 1 {
		t.Fatalf("cookies = %v", cookies)
	}
	check, _ := gin.CreateTestContext(httptest.NewRecorder())
	check.Request = httptest.NewRequest("GET", "/p/test-share", nil)
	check.Request.AddCookie(cookies[0])
	if !h.hasShareAccess(check, share) {
		t.Fatal("valid grant rejected")
	}
	if _, err := jwt.Parse(cfg.Auth.JWTSecret, cookies[0].Value); err == nil {
		t.Fatal("share grant accepted as account token")
	}
	changed := share
	changed.SharePassword = "new-hash"
	if h.hasShareAccess(check, changed) {
		t.Fatal("password change with unchanged timestamp preserves old grant")
	}
	changed.UpdatedAt = share.UpdatedAt.Add(time.Duration(1 << 32))
	if h.hasShareAccess(check, changed) {
		t.Fatal("timestamp wrap preserves old grant")
	}
	expired, err := jwt.Sign(h.shareAccessSecret(share), jwt.Claims{UserID: 1, Username: share.ShareID}, -time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	check.Request.Header.Del("Cookie")
	check.Request.AddCookie(&http.Cookie{Name: shareAccessCookieName, Value: expired})
	if h.hasShareAccess(check, share) {
		t.Fatal("expired grant accepted")
	}
}
