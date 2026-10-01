package server

import (
	"html"
	"net/http/httptest"
	"net/url"
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

func TestBlogAssetURLQueryAndFragment(t *testing.T) {
	srv, _, _ := newTestServer(t)
	r := srv.Router()
	token := registerAndLogin(t, r, "urlassets", "password123")
	uploadFile(t, r, token, "Notes/Post.md", "![fragment](./chart.svg#view)\n![query](./chart.svg?v=1)\n![both](./chart.svg?v=2#view)\n![[literal#name.png]]\n![encoded](./literal%23name.png)", 1700000000000)
	uploadFile(t, r, token, "Notes/chart.svg", "<svg></svg>", 1700000000000)
	uploadFile(t, r, token, "Notes/literal#name.png", "literal-hash", 1700000000000)
	status, body := doJSON(t, r, "POST", "/api/shares", token, map[string]any{"target_path": "Notes/Post.md"})
	if status != 200 {
		t.Fatal(status, body)
	}
	page := httptest.NewRecorder()
	r.ServeHTTP(page, httptest.NewRequest("GET", "/p/"+body["share_id"].(string), nil))
	images := regexp.MustCompile(`<img src="([^"]+)"`).FindAllStringSubmatch(page.Body.String(), -1)
	if len(images) != 5 {
		t.Fatalf("images=%v page=%s", images, page.Body)
	}
	for i, want := range []string{"<svg></svg>", "<svg></svg>", "<svg></svg>", "literal-hash", "literal-hash"} {
		target, err := url.Parse(html.UnescapeString(images[i][1]))
		if err != nil {
			t.Fatal(err)
		}
		if (i == 0 || i == 2) && target.Fragment != "view" {
			t.Fatalf("fragment lost: %s", target)
		}
		if i >= 3 && target.Fragment != "" {
			t.Fatalf("literal hash became fragment: %s", target)
		}
		target.Fragment = ""
		w := httptest.NewRecorder()
		r.ServeHTTP(w, httptest.NewRequest("GET", target.String(), nil))
		if w.Code != 200 || w.Body.String() != want {
			t.Fatalf("image %d: status=%d body=%q", i, w.Code, w.Body.String())
		}
	}
}
