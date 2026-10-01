package server

import (
	"html"
	"net/http/httptest"
	"regexp"
	"testing"
)

func TestBlogWikiAssetLiteralPercent(t *testing.T) {
	srv, _, _ := newTestServer(t)
	r := srv.Router()
	token := registerAndLogin(t, r, "literalassets", "password123")
	uploadFile(t, r, token, "Post.md", "![[literal%20name.png]]\n![space](literal%20name.png)\n![[missing%20name.png]]", 1700000000000)
	for name, content := range map[string]string{
		"literal%20name.png": "literal",
		"literal name.png":   "space",
		"missing name.png":   "private",
	} {
		uploadFile(t, r, token, name, content, 1700000000000)
	}
	status, body := doJSON(t, r, "POST", "/api/shares", token, map[string]any{"target_path": "Post.md"})
	if status != 200 {
		t.Fatal(status, body)
	}
	page := httptest.NewRecorder()
	r.ServeHTTP(page, httptest.NewRequest("GET", "/p/"+body["share_id"].(string), nil))
	images := regexp.MustCompile(`<img src="([^"]+)"`).FindAllStringSubmatch(page.Body.String(), -1)
	if len(images) != 3 {
		t.Fatalf("images=%v page=%s", images, page.Body)
	}
	for i, want := range []struct {
		status int
		body   string
	}{{200, "literal"}, {200, "space"}, {404, ""}} {
		w := httptest.NewRecorder()
		r.ServeHTTP(w, httptest.NewRequest("GET", html.UnescapeString(images[i][1]), nil))
		if w.Code != want.status || w.Body.String() != want.body {
			t.Fatalf("image %d: status=%d body=%q", i, w.Code, w.Body.String())
		}
	}
}
