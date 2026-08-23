package update

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestProductionCandidateHasNoPlaceholderPath(t *testing.T) {
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	dir := filepath.Dir(file)
	data, err := os.ReadFile(filepath.Join(dir, "candidate.go"))
	if err != nil {
		t.Fatalf("read candidate.go: %v", err)
	}
	src := string(data)
	forbidden := []string{
		"placeholderDigest",
		"newTestCandidate",
		"sha256:aaaaaaaa",
	}
	for _, s := range forbidden {
		if strings.Contains(strings.ToLower(src), strings.ToLower(s)) {
			t.Errorf("production candidate.go must not contain %q", s)
		}
	}
	// also ensure no 6-arg old constructor stub
	if strings.Contains(src, "func NewCandidate(tag, goos, goarch, assetURL, releaseURL string, size int64) (") {
		t.Error("old 6-arg NewCandidate must not exist in production")
	}
}

func TestNewCandidate_StrictURLContract(t *testing.T) {
	digest := "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	// arbitrary http must be rejected
	if _, err := NewCandidate("v1.2.3", "linux", "amd64", "http://example.com/oss-server_1.2.3_linux_amd64.tar.gz", "https://example.com/releases/tag/v1.2.3", 100, 1, 1, digest); err == nil {
		t.Error("arbitrary http asset_url must be rejected")
	}
	if _, err := NewCandidate("v1.2.3", "linux", "amd64", "https://example.com/oss-server_1.2.3_linux_amd64.tar.gz", "http://example.com/releases/tag/v1.2.3", 100, 1, 1, digest); err == nil {
		t.Error("arbitrary http release_url must be rejected")
	}
	// loopback http allowed for local tests
	loops := []string{
		"http://127.0.0.1/oss-server_1.2.3_linux_amd64.tar.gz",
		"http://localhost/oss-server_1.2.3_linux_amd64.tar.gz",
		"http://127.0.0.1:8080/releases/tag/v1.2.3",
		"http://localhost:8080/releases/tag/v1.2.3",
	}
	for _, u := range loops {
		assetURL := "https://example.com/oss-server_1.2.3_linux_amd64.tar.gz"
		releaseURL := "https://example.com/releases/tag/v1.2.3"
		if strings.Contains(u, "releases") {
			releaseURL = u
		} else {
			assetURL = u
		}
		if _, err := NewCandidate("v1.2.3", "linux", "amd64", assetURL, releaseURL, 100, 1, 1, digest); err != nil {
			t.Errorf("loopback %q should be allowed, got %v", u, err)
		}
	}
	// https always allowed
	if _, err := NewCandidate("v1.2.3", "linux", "amd64", "https://cdn.example.com/oss-server_1.2.3_linux_amd64.tar.gz", "https://example.com/releases/tag/v1.2.3", 100, 1, 1, digest); err != nil {
		t.Errorf("https URL should be allowed, got %v", err)
	}
	// ftp / relative must be rejected
	bad := []string{"ftp://example.com/a.tar.gz", "/relative/path", ""}
	for _, u := range bad {
		if _, err := NewCandidate("v1.2.3", "linux", "amd64", u, "https://example.com/releases/tag/v1.2.3", 100, 1, 1, digest); err == nil {
			t.Errorf("bad asset_url %q should be rejected", u)
		}
	}
}
