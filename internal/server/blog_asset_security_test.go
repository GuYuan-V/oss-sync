package server

import (
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

func TestBlogAssetsAreSandboxed(t *testing.T) {
	srv, _, _ := newTestServer(t)
	r := srv.Router()
	token := registerAndLogin(t, r, "sandboxassets", "password123")
	uploadFile(t, r, token, "Post.md", "![[page.html]]\n![[image.svg]]\n![[image.png]]", 1700000000000)
	for name, content := range map[string]string{
		"page.html": "<!doctype html><script>document.title='marker'</script>",
		"image.svg": "<svg xmlns=\"http://www.w3.org/2000/svg\"><script>document.title='marker'</script></svg>",
		"image.png": "image-bytes",
	} {
		uploadFile(t, r, token, name, content, 1700000000000)
	}
	status, body := doJSON(t, r, "POST", "/api/shares", token, map[string]any{"target_path": "Post.md"})
	if status != 200 {
		t.Fatal(status, body)
	}
	for _, name := range []string{"page.html", "image.svg", "image.png"} {
		w := httptest.NewRecorder()
		r.ServeHTTP(w, httptest.NewRequest("GET", "/assets/"+body["share_id"].(string)+"?ref="+url.QueryEscape(name), nil))
		policy := w.Header().Get("Content-Security-Policy")
		if w.Code != 200 || !strings.HasPrefix(policy, "sandbox;") || strings.Contains(policy, "allow-scripts") || strings.Contains(policy, "allow-same-origin") || w.Header().Get("X-Content-Type-Options") != "nosniff" {
			t.Fatalf("%s: status=%d headers=%v", name, w.Code, w.Header())
		}
	}
}
