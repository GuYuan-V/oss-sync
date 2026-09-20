package update

import (
	"runtime"
	"testing"
)

func validCandidate() *Candidate {
	c, err := newTestCandidate("v1.2.3", "linux", "amd64", "https://example.com/oss-server_1.2.3_linux_amd64.tar.gz", "https://example.com/releases/tag/v1.2.3", 1234)
	if err != nil {
		panic(err)
	}
	return c
}

func TestCandidate_Valid(t *testing.T) {
	c := validCandidate()
	if err := c.Validate(); err != nil {
		t.Fatalf("valid candidate should pass, got %v", err)
	}
}

func TestNewCandidate_MalformedVersion(t *testing.T) {
	malformed := []string{"", "dev", "v", "1.2", "01.2.3", "1.02.3", "not-semver", "1.2.3-01"}
	for _, v := range malformed {
		_, err := newTestCandidate(v, "linux", "amd64", "https://example.com/a.tar.gz", "https://example.com/r", 100)
		if err == nil {
			t.Errorf("NewCandidate should fail for malformed version %q", v)
		}
	}
	// 以下手工构造非法版本，覆盖 Validate 分支。
	c := validCandidate()
	c.Version = "bad"
	if err := c.Validate(); err == nil {
		t.Error("Validate should fail for malformed version")
	}
	c = validCandidate()
	c.Version = "1.2.3.4"
	if err := c.Validate(); err == nil {
		t.Error("Validate should fail for version with 4 parts")
	}
	c = validCandidate()
	c.Version = "v1.2.3" // 含 v 前缀，未规范化。
	if err := c.Validate(); err == nil {
		t.Error("Validate should fail for version with v prefix")
	}
}

func TestCandidate_AssetMismatch(t *testing.T) {
	// 版本正确但资产名错误。
	c := validCandidate()
	c.AssetName = "oss-server_1.2.3_linux_amd64.zip" // linux 要求 tar.gz，后缀错误。
	if err := c.Validate(); err == nil {
		t.Error("Validate should fail for asset name mismatch (ext)")
	}
	c = validCandidate()
	c.AssetName = "oss-server_9.9.9_linux_amd64.tar.gz" // 资产内版本与候选版本不一致。
	if err := c.Validate(); err == nil {
		t.Error("Validate should fail for asset name version mismatch")
	}
	c = validCandidate()
	c.AssetName = "oss-server-linux-amd64-v1.2.3.tar.gz" // 旧宽松命名必须拒绝。
	if err := c.Validate(); err == nil {
		t.Error("Validate should fail for legacy permissive name")
	}
	// NewCandidate 内部生成精确资产名，此处仅覆盖 Validate；另覆盖不支持平台。
	if _, err := newTestCandidate("v1.2.3", "freebsd", "amd64", "https://example.com/a", "https://example.com/r", 100); err == nil {
		t.Error("NewCandidate should fail for unsupported platform")
	}
}

func TestCandidate_NonPositiveSize(t *testing.T) {
	_, err := newTestCandidate("v1.2.3", "linux", "amd64", "https://example.com/a.tar.gz", "https://example.com/r", 0)
	if err == nil {
		t.Error("NewCandidate should fail for size 0")
	}
	_, err = newTestCandidate("v1.2.3", "linux", "amd64", "https://example.com/a.tar.gz", "https://example.com/r", -10)
	if err == nil {
		t.Error("NewCandidate should fail for negative size")
	}
	c := validCandidate()
	c.Size = 0
	if err := c.Validate(); err == nil {
		t.Error("Validate should fail for size 0")
	}
	c.Size = -1
	if err := c.Validate(); err == nil {
		t.Error("Validate should fail for negative size")
	}
}

func TestCandidate_NonHTTPSURL(t *testing.T) {
	cases := []struct {
		assetURL   string
		releaseURL string
	}{
		{"http://example.com/a.tar.gz", "https://example.com/r"},
		{"", "https://example.com/r"},
		{"ftp://example.com/a.tar.gz", "https://example.com/r"},
		{"/relative/path.tar.gz", "https://example.com/r"},
		{"https://example.com/a.tar.gz", "http://example.com/r"},
		{"https://example.com/a.tar.gz", ""},
		{"https://example.com/a.tar.gz", "ftp://example.com/r"},
		{"https://example.com/a.tar.gz", "/relative"},
	}
	for _, tc := range cases {
		_, err := newTestCandidate("v1.2.3", "linux", "amd64", tc.assetURL, tc.releaseURL, 100)
		if err == nil {
			t.Errorf("NewCandidate should fail for urls %q %q", tc.assetURL, tc.releaseURL)
		}
	}
	// 同一组输入覆盖 Validate。
	for _, tc := range cases {
		c := validCandidate()
		c.AssetURL = tc.assetURL
		c.ReleaseURL = tc.releaseURL
		if err := c.Validate(); err == nil {
			t.Errorf("Validate should fail for urls %q %q", tc.assetURL, tc.releaseURL)
		}
	}
	// 不限定 GitHub 域名，任意 https 主机均可接受。
	c := validCandidate()
	c.AssetURL = "https://my-custom-host.example.net/files/oss-server_1.2.3_linux_amd64.tar.gz"
	c.ReleaseURL = "https://my-custom-host.example.net/releases/v1.2.3"
	if err := c.Validate(); err != nil {
		t.Errorf("Validate should accept any https host, got %v", err)
	}
}

func TestCandidate_PlatformIdentity(t *testing.T) {
	c, err := newTestCandidate("v1.2.3", "darwin", "arm64", "https://example.com/a.tar.gz", "https://example.com/r", 100)
	if err != nil {
		t.Fatalf("NewCandidate: %v", err)
	}
	if c.GOOS != "darwin" || c.GOARCH != "arm64" {
		t.Errorf("platform identity not stored: %s/%s", c.GOOS, c.GOARCH)
	}
	// 仅改平台会导致资产名失配，Validate 应失败。
	c.GOOS = "linux"
	if err := c.Validate(); err == nil {
		t.Error("changing platform without updating asset name should fail")
	}
	exp, _ := AssetName("v1.2.3", "linux", "arm64")
	c.GOOS = "linux"
	c.GOARCH = "arm64"
	c.AssetName = exp
	if err := c.Validate(); err != nil {
		t.Errorf("after fixing asset name for new platform, Validate should pass, got %v", err)
	}
	// 候选必须自带平台信息，不得隐式使用当前运行平台。
	_ = runtime.GOOS
}

func TestSelectAsset_ExactOnly(t *testing.T) {
	expected, _ := AssetName("1.0.0", "linux", "amd64")
	assets := []Asset{
		{ID: 1, Name: expected, Size: 1024, Digest: "sha256:e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855", BrowserDownloadURL: "https://example.com/" + expected},
		{ID: 2, Name: "oss-server-linux-amd64-1.0.0.tar.gz", Size: 1024, Digest: "sha256:e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855", BrowserDownloadURL: "https://example.com/legacy"},
		{ID: 3, Name: "linux-amd64", Size: 1024, Digest: "sha256:e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855", BrowserDownloadURL: "https://example.com/other"},
	}
	got, err := selectAsset(assets, "v1.0.0", "linux", "amd64")
	if err != nil {
		t.Fatalf("selectAsset: %v", err)
	}
	if got.Name != expected {
		t.Errorf("selectAsset returned %q, want exact %q", got.Name, expected)
	}
	legacyAssets := []Asset{{ID: 4, Name: "my-oss-server-linux-amd64.tar.gz", Size: 1024, Digest: "sha256:e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855", BrowserDownloadURL: "https://example.com/legacy2"}}
	if _, err := selectAsset(legacyAssets, "v1.0.0", "linux", "amd64"); err == nil {
		t.Error("permissive substring should not be accepted")
	}
}
