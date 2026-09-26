package webui

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
)

func TestSetPageHeaders_whenRendered_setsCSPImagePolicyWithoutRelaxingOtherDirectives(t *testing.T) {
	w := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(w)
	req, err := http.NewRequest(http.MethodGet, "/dashboard", nil)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	ctx.Request = req

	setPageHeaders(ctx, "test-nonce")
	policy := w.Header().Get("Content-Security-Policy")
	if policy == "" {
		t.Fatal("missing CSP header")
	}

	if got := policy; !strings.Contains(got, "img-src 'self' data: https:") {
		t.Fatalf("unexpected img-src directive in CSP: %q", got)
	}
	if strings.Contains(policy, "img-src 'self' data: http:") {
		t.Fatalf("img-src allows insecure http: scheme: %q", policy)
	}

	for _, want := range []string{
		"default-src 'none'",
		"connect-src 'self'",
		"script-src 'self'",
		"style-src 'self'",
		"form-action 'self'",
		"frame-ancestors 'none'",
		"base-uri 'none'",
	} {
		if !strings.Contains(policy, want) {
			t.Fatalf("CSP missing %q directive in %q", want, policy)
		}
	}
}
