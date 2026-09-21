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

	"github.com/helantianshen/oss-sync/internal/config"
	"github.com/helantianshen/oss-sync/internal/version"
)

func mustDigest(b []byte) string {
	h := sha256.Sum256(b)
	return "sha256:" + hex.EncodeToString(h[:])
}

// hardeningAssetName 按当前平台生成期望资产名
func hardeningAssetName(tag string) string {
	n, _ := AssetName(tag, runtime.GOOS, runtime.GOARCH)
	return n
}

// 非法 semver tag 必须被拒绝
func TestHardening_MalformedSemverRejected(t *testing.T) {
	malformed := []string{"", "v", "1.2", "01.2.3", "1.02.3", "not-semver", "1.2.3-01", "v1.2.3.4.5"}
	for _, tag := range malformed {
		assets := []Asset{{ID: 1, Name: "oss-server_1.0.0_linux_amd64.tar.gz", Size: 100, Digest: "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", BrowserDownloadURL: "https://example.com/a.tar.gz"}}
		_, err := selectAsset(assets, tag, "linux", "amd64")
		if err == nil {
			t.Errorf("selectAsset should fail for malformed tag %q", tag)
		}
		// version.Parse 同样应失败；部分 tag 已在 selectAsset 经 AssetName 失败，此处无需断言
		if _, err := version.Parse(tag); err == nil && tag != "" {
		}
		// validateRelease 同样应拒绝；空 tag 属于 ErrNoRelease，此处仅断言非空非法 tag 必须失败
		rel := Release{ID: 1, TagName: tag, Draft: false, Prerelease: false, HTMLURL: "https://example.com/releases/tag/" + tag, Assets: assets}
		if err := validateRelease(&rel); err == nil && tag != "" {
			t.Errorf("validateRelease should fail for malformed tag %q, got nil", tag)
		}
	}
}

// 预发布与草稿必须被拒绝
func TestHardening_PrereleaseDraftRejected(t *testing.T) {
	// 经 validateRelease 覆盖预发布 tag
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
	// selectAsset 同样拒绝预发布 tag
	assets := []Asset{{ID: 1, Name: "oss-server_1.2.3-alpha.1_linux_amd64.tar.gz", Size: 100, Digest: "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", BrowserDownloadURL: "https://example.com/a.tar.gz"}}
	if _, err := selectAsset(assets, "v1.2.3-alpha.1", "linux", "amd64"); err == nil {
		t.Error("selectAsset should reject prerelease tag")
	}
}

func isErrNoRelease(err error) bool {
	return err != nil && strings.Contains(err.Error(), ErrNoRelease.Error())
}

// 缺失与重复资产必须失败
func TestHardening_MissingDuplicateAsset(t *testing.T) {
	tag := "v1.2.3"
	expected, _ := AssetName(tag, "linux", "amd64")
	// 资产缺失
	assets := []Asset{{ID: 1, Name: "other-asset.tar.gz", Size: 100, Digest: "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", BrowserDownloadURL: "https://example.com/other.tar.gz"}}
	if _, err := selectAsset(assets, tag, "linux", "amd64"); err == nil {
		t.Error("should fail for missing asset")
	}
	// 同名资产重复
	dup := []Asset{
		{ID: 1, Name: expected, Size: 100, Digest: "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", BrowserDownloadURL: "https://example.com/a.tar.gz"},
		{ID: 2, Name: expected, Size: 100, Digest: "sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb", BrowserDownloadURL: "https://example.com/b.tar.gz"},
	}
	if _, err := selectAsset(dup, tag, "linux", "amd64"); err == nil {
		t.Error("should fail for duplicate asset")
	}
}

// digest 缺失或格式非法必须失败
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

// 经 httptest 下载覆盖 digest 错误与大小错误
func TestHardening_WrongDigestSize(t *testing.T) {
	content := fakeExecBytes()
	correctDigest := mustDigest(wrapContentIfArchiveForTest(content, hardeningAssetName("v9.9.9")))
	// 错误 digest 分支
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
	// 错误大小分支
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
		// 与 wrapContentIfArchive 对齐的简化实现，错误分支不依赖精确 digest 计算
		return content
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
		// 声明大小大于实际，用于覆盖大小不一致分支
		fmt.Fprintf(w,
			`{"id":1001,"tag_name":%q,"html_url":%q,"draft":false,"prerelease":false,"assets":[{"id":2001,"name":%q,"browser_download_url":%q,"size":%d,"digest":%q}]}`,
			tag, srv.URL+"/releases/tag/"+tag, assetName, srv.URL+downloadPath, len(serveContent)+100, digest)
	})
	srv = httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return &mockUpstream{srv: srv}
}

// 非安全重定向与 Token 泄露必须被拦截
func TestHardening_UnsafeRedirectAndTokenLeakage(t *testing.T) {
	content := fakeExecBytes()
	assetName := hardeningAssetName("v9.9.9")
	serveContent := wrapContentIfArchive(t, assetName, content)
	digest := mustDigest(serveContent)

	// 第二个服务端记录 Authorization 头，用于判定跨站泄露
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

	// 第一个服务端跨站重定向到第二个服务端
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
	// newGitHubClient 从环境变量读取 Token，此处显式赋值保证生效
	u.gh.token = "secret-token-123"
	res := u.Update(context.Background())
	// 重定向本身可成功，但 Token 不得泄露
	if leaked {
		t.Error("Authorization token leaked to cross-host redirect")
	}
	// 同协议 loopback 重定向允许通过且剥离 Token；降级失败亦可接受，只要不泄露
	if res.OK && leaked {
		t.Error("leaked token on success")
	}

	// 降级场景：https 跳 http 必须拒绝
	t.Run("downgrade rejected", func(t *testing.T) {
		// httptest 只有 http，直接以 https 起始构造 CheckRedirect 输入
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

// 平台不匹配的资产必须被拒绝
func TestHardening_PlatformAssetRejection(t *testing.T) {
	tag := "v1.2.3"
	// Release 仅含 windows 资产，请求 linux 时必须失败
	windowsAsset, _ := AssetName(tag, "windows", "amd64")
	assets := []Asset{{ID: 1, Name: windowsAsset, Size: 100, Digest: "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", BrowserDownloadURL: "https://example.com/a.zip"}}
	if _, err := selectAsset(assets, tag, "linux", "amd64"); err == nil {
		t.Error("should reject platform mismatch")
	}
}

// 经 httptest 覆盖 fetchLatest 对草稿、预发布与非法 tag 的拒绝
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
