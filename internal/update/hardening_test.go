package update

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"

	"github.com/oss/oss-server/internal/config"
	"github.com/oss/oss-server/internal/version"
)

func mustDigest(b []byte) string {
	h := sha256.Sum256(b)
	return "sha256:" + hex.EncodeToString(h[:])
}

// helpers for hardened tests
func hardeningAssetName(tag string) string {
	n, _ := AssetName(tag, runtime.GOOS, runtime.GOARCH)
	return n
}

// Test malformed semver tag is rejected
func TestHardening_MalformedSemverRejected(t *testing.T) {
	malformed := []string{"", "v", "1.2", "01.2.3", "1.02.3", "not-semver", "1.2.3-01", "v1.2.3.4.5"}
	for _, tag := range malformed {
		assets := []Asset{{ID: 1, Name: "oss-server_1.0.0_linux_amd64.tar.gz", Size: 100, Digest: "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", BrowserDownloadURL: "https://example.com/a.tar.gz"}}
		_, err := selectAsset(assets, tag, "linux", "amd64")
		if err == nil {
			t.Errorf("selectAsset should fail for malformed tag %q", tag)
		}
		// also version.Parse should fail
		if _, err := version.Parse(tag); err == nil && tag != "" {
			// some tags like "v" will fail inside selectAsset via AssetName
		}
		// validateRelease should reject
		rel := Release{ID: 1, TagName: tag, Draft: false, Prerelease: false, HTMLURL: "https://example.com/releases/tag/" + tag, Assets: assets}
		if err := validateRelease(&rel); err == nil && tag != "" {
			// empty tag is ErrNoRelease which is considered error; but we check malformed should be error
			t.Errorf("validateRelease should fail for malformed tag %q, got nil", tag)
		}
	}
}

// Test prerelease and draft rejected
func TestHardening_PrereleaseDraftRejected(t *testing.T) {
	// prerelease tag via validateRelease
	rel := Release{ID: 1, TagName: "v1.2.3-alpha.1", Draft: false, Prerelease: false}
	if err := validateRelease(&rel); err == nil {
		t.Error("validateRelease should reject prerelease tag")
	}
	rel2 := Release{ID: 2, TagName: "v1.2.3", Draft: true, Prerelease: false}
	if err := validateRelease(&rel2); !isErrNoRelease(err) {
		t.Errorf("draft should be ErrNoRelease, got %v", err)
	}
	rel3 := Release{ID: 3, TagName: "v1.2.3", Draft: false, Prerelease: true}
	if err := validateRelease(&rel3); !isErrNoRelease(err) {
		t.Errorf("prerelease flag should be ErrNoRelease, got %v", err)
	}
	// selectAsset should reject prerelease tag
	assets := []Asset{{ID: 1, Name: "oss-server_1.2.3-alpha.1_linux_amd64.tar.gz", Size: 100, Digest: "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", BrowserDownloadURL: "https://example.com/a.tar.gz"}}
	if _, err := selectAsset(assets, "v1.2.3-alpha.1", "linux", "amd64"); err == nil {
		t.Error("selectAsset should reject prerelease tag")
	}
}

func isErrNoRelease(err error) bool {
	return err != nil && strings.Contains(err.Error(), ErrNoRelease.Error())
}

// Test missing/duplicate asset
func TestHardening_MissingDuplicateAsset(t *testing.T) {
	tag := "v1.2.3"
	expected, _ := AssetName(tag, "linux", "amd64")
	// missing
	assets := []Asset{{ID: 1, Name: "other-asset.tar.gz", Size: 100, Digest: "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", BrowserDownloadURL: "https://example.com/other.tar.gz"}}
	if _, err := selectAsset(assets, tag, "linux", "amd64"); err == nil {
		t.Error("should fail for missing asset")
	}
	// duplicate
	dup := []Asset{
		{ID: 1, Name: expected, Size: 100, Digest: "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", BrowserDownloadURL: "https://example.com/a.tar.gz"},
		{ID: 2, Name: expected, Size: 100, Digest: "sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb", BrowserDownloadURL: "https://example.com/b.tar.gz"},
	}
	if _, err := selectAsset(dup, tag, "linux", "amd64"); err == nil {
		t.Error("should fail for duplicate asset")
	}
}

// Test malformed / missing digest
func TestHardening_DigestVariants(t *testing.T) {
	tag := "v1.2.3"
	expected, _ := AssetName(tag, "linux", "amd64")
	cases := []struct {
		name   string
		digest string
		ok     bool
	}{
		{"missing", "", false},
		{"no prefix", "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", false},
		{"short", "sha256:abc", false},
		{"invalid hex", "sha256:zzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzz", false},
		{"valid lower", "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", true},
		{"valid upper", "sha256:AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA", true},
	}
	for _, c := range cases {
		assets := []Asset{{ID: 1, Name: expected, Size: 100, Digest: c.digest, BrowserDownloadURL: "https://example.com/a.tar.gz"}}
		_, err := selectAsset(assets, tag, "linux", "amd64")
		if c.ok && err != nil {
			t.Errorf("digest %q should be ok, got %v", c.digest, err)
		}
		if !c.ok && err == nil {
			t.Errorf("digest %q should fail", c.digest)
		}
	}
}

// Test wrong digest/size via httptest download
func TestHardening_WrongDigestSize(t *testing.T) {
	content := fakeExecBytes()
	correctDigest := mustDigest(wrapContentIfArchiveForTest(content, hardeningAssetName("v9.9.9")))
	// wrong digest case
	t.Run("wrong digest", func(t *testing.T) {
		up := newMockUpstreamWithDigest(t, "v9.9.9", content, "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa")
		exePath := filepath.Join(t.TempDir(), "oss-server")
		if err := os.WriteFile(exePath, []byte("old"), 0o755); err != nil {
			t.Fatal(err)
		}
		u := newTestUpdater(t, up, exePath)
		res := u.Update(context.Background())
		if res.OK || res.Code == "ok" {
			t.Fatalf("wrong digest should fail, got %+v", res)
		}
		_ = correctDigest
	})
	// wrong size
	t.Run("wrong size", func(t *testing.T) {
		up := newMockUpstreamWithWrongSize(t, "v9.9.9", content)
		exePath := filepath.Join(t.TempDir(), "oss-server")
		if err := os.WriteFile(exePath, []byte("old"), 0o755); err != nil {
			t.Fatal(err)
		}
		u := newTestUpdater(t, up, exePath)
		res := u.Update(context.Background())
		if res.OK {
			t.Fatalf("wrong size should fail, got %+v", res)
		}
	})
}

func wrapContentIfArchiveForTest(content []byte, assetName string) []byte {
	l := strings.ToLower(assetName)
	if strings.HasSuffix(l, ".tar.gz") || strings.HasSuffix(l, ".tgz") {
		// need to mimic wrapContentIfArchive but without t
		return content // simplified: digest of serveContent vs raw not critical for this test's wrong case
	}
	return content
}

func newMockUpstreamWithDigest(t *testing.T, tag string, content []byte, digest string) *mockUpstream {
	t.Helper()
	assetName := hardeningAssetName(tag)
	serveContent := wrapContentIfArchive(t, assetName, content)
	var srv *httptest.Server
	downloadPath := "/downloads/" + assetName
	mux := http.NewServeMux()
	mux.HandleFunc(downloadPath, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Length", strconv.Itoa(len(serveContent)))
		w.Write(serveContent)
	})
	mux.HandleFunc("/repos/fake/oss-sync/releases/latest", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintf(w,
			`{"id":1001,"tag_name":%q,"html_url":%q,"draft":false,"prerelease":false,"assets":[{"id":2001,"name":%q,"browser_download_url":%q,"size":%d,"digest":%q}]}`,
			tag, srv.URL+"/releases/tag/"+tag, assetName, srv.URL+downloadPath, len(serveContent), digest)
	})
	srv = httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return &mockUpstream{srv: srv}
}

func newMockUpstreamWithWrongSize(t *testing.T, tag string, content []byte) *mockUpstream {
	t.Helper()
	assetName := hardeningAssetName(tag)
	serveContent := wrapContentIfArchive(t, assetName, content)
	digest := mustDigest(serveContent)
	var srv *httptest.Server
	downloadPath := "/downloads/" + assetName
	mux := http.NewServeMux()
	mux.HandleFunc(downloadPath, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Length", strconv.Itoa(len(serveContent)))
		w.Write(serveContent)
	})
	mux.HandleFunc("/repos/fake/oss-sync/releases/latest", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		// declare size larger than actual
		fmt.Fprintf(w,
			`{"id":1001,"tag_name":%q,"html_url":%q,"draft":false,"prerelease":false,"assets":[{"id":2001,"name":%q,"browser_download_url":%q,"size":%d,"digest":%q}]}`,
			tag, srv.URL+"/releases/tag/"+tag, assetName, srv.URL+downloadPath, len(serveContent)+100, digest)
	})
	srv = httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return &mockUpstream{srv: srv}
}

// Test unsafe redirect (downgrade) and token leakage
func TestHardening_UnsafeRedirectAndTokenLeakage(t *testing.T) {
	content := fakeExecBytes()
	assetName := hardeningAssetName("v9.9.9")
	serveContent := wrapContentIfArchive(t, assetName, content)
	digest := mustDigest(serveContent)

	// Setup second server that records Authorization header
	var leaked bool
	var secondSrv *httptest.Server
	secondMux := http.NewServeMux()
	secondMux.HandleFunc("/downloads/"+assetName, func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "" {
			leaked = true
		}
		w.Header().Set("Content-Length", strconv.Itoa(len(serveContent)))
		w.Write(serveContent)
	})
	secondSrv = httptest.NewServer(secondMux)
	t.Cleanup(secondSrv.Close)

	// First server redirects to second server (cross-host)
	var firstSrv *httptest.Server
	firstMux := http.NewServeMux()
	firstMux.HandleFunc("/downloads/"+assetName, func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, secondSrv.URL+"/downloads/"+assetName, http.StatusFound)
	})
	firstMux.HandleFunc("/repos/fake/oss-sync/releases/latest", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintf(w,
			`{"id":1001,"tag_name":%q,"html_url":%q,"draft":false,"prerelease":false,"assets":[{"id":2001,"name":%q,"browser_download_url":%q,"size":%d,"digest":%q}]}`,
			"v9.9.9", firstSrv.URL+"/releases/tag/v9.9.9", assetName, firstSrv.URL+"/downloads/"+assetName, len(serveContent), digest)
	})
	firstSrv = httptest.NewServer(firstMux)
	t.Cleanup(firstSrv.Close)

	exePath := filepath.Join(t.TempDir(), "oss-server")
	if err := os.WriteFile(exePath, []byte("old"), 0o755); err != nil {
		t.Fatal(err)
	}
	// Set token
	t.Setenv("OSS_GITHUB_TOKEN", "secret-token-123")
	cfg := testCfg()
	u, err := NewUpdater(cfg, Options{
		ExecPath:   exePath,
		APIBase:    firstSrv.URL,
		HTTPClient: firstSrv.Client(),
		Verifier:   func(string, string) error { return nil },
	})
	if err != nil {
		t.Fatal(err)
	}
	// ensure token is picked up (newGitHubClient reads env)
	u.gh.token = "secret-token-123"
	res := u.Update(context.Background())
	// Update may succeed via redirect, but token must not leak
	if leaked {
		t.Error("Authorization token leaked to cross-host redirect")
	}
	// The redirect itself should succeed if same scheme (http loopback) – we allow it, but token stripped
	// If it failed due to downgrade logic, res would be failed; both are acceptable as long as no leakage
	if res.OK && leaked {
		t.Error("leaked token on success")
	}

	// Downgrade test: https -> http should be rejected
	t.Run("downgrade rejected", func(t *testing.T) {
		// Simulate https initial URL redirecting to http
		// Since httptest is http, we simulate by having CheckRedirect see https->http
		// We can directly test clientWithSafeRedirect logic
		client := &http.Client{}
		safe := clientWithSafeRedirect(client, "https://api.github.com")
		req, _ := http.NewRequest(http.MethodGet, "http://example.com/a", nil)
		via := []*http.Request{{URL: mustParseURL("https://example.com/b")}}
		err := safe.CheckRedirect(req, via)
		if err == nil || !strings.Contains(err.Error(), "downgrade") {
			t.Errorf("expected downgrade rejection, got %v", err)
		}
	})
}

func mustParseURL(s string) *url.URL {
	u, _ := url.Parse(s)
	return u
}

// Test platform assets rejection – asset for different platform should not be selected
func TestHardening_PlatformAssetRejection(t *testing.T) {
	tag := "v1.2.3"
	// Release contains only windows asset but we request linux
	windowsAsset, _ := AssetName(tag, "windows", "amd64")
	assets := []Asset{{ID: 1, Name: windowsAsset, Size: 100, Digest: "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", BrowserDownloadURL: "https://example.com/a.zip"}}
	if _, err := selectAsset(assets, tag, "linux", "amd64"); err == nil {
		t.Error("should reject platform mismatch")
	}
}

// Test fetchLatest via httptest for draft/prerelease/malformed
func TestHardening_FetchLatestRejections(t *testing.T) {
	cases := []struct {
		name string
		body string
	}{
		{"draft", `{"id":1,"tag_name":"v1.2.3","draft":true,"prerelease":false,"html_url":"https://example.com/tag/v1.2.3","assets":[]}`},
		{"prerelease flag", `{"id":1,"tag_name":"v1.2.3","draft":false,"prerelease":true,"html_url":"https://example.com/tag/v1.2.3","assets":[]}`},
		{"prerelease tag", `{"id":1,"tag_name":"v1.2.3-beta.1","draft":false,"prerelease":false,"html_url":"https://example.com/tag/v1.2.3-beta.1","assets":[]}`},
		{"malformed tag", `{"id":1,"tag_name":"not-semver","draft":false,"prerelease":false,"html_url":"https://example.com/tag/not-semver","assets":[]}`},
		{"missing id", `{"tag_name":"v1.2.3","draft":false,"prerelease":false,"html_url":"https://example.com/tag/v1.2.3","assets":[]}`},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				w.Write([]byte(c.body))
			}))
			defer srv.Close()
			cfg := &config.Config{Update: config.UpdateConfig{GitHubRepo: "fake/oss-sync"}}
			gh := newGitHubClient(cfg, srv.Client())
			gh.apiBase = srv.URL
			_, err := gh.fetchLatest(context.Background())
			if err == nil {
				t.Errorf("fetchLatest should reject %s", c.name)
			}
		})
	}
}
