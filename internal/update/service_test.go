package update

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/helantianshen/oss-sync/internal/config"
	"github.com/helantianshen/oss-sync/internal/version"
)

func newServiceTestManager(t *testing.T) (*Manager, *Updater, *config.Config, string) {
	t.Helper()
	orig := version.Version
	t.Cleanup(func() { version.Version = orig })
	version.Version = "1.0.0"
	dataDir := t.TempDir()
	exePath := filepath.Join(t.TempDir(), "oss-server")
	if err := os.WriteFile(exePath, []byte("old-binary"), 0o755); err != nil {
		t.Fatal(err)
	}
	cfg := &config.Config{
		Server:  config.ServerConfig{Host: "127.0.0.1", Port: 8080},
		Storage: config.StorageConfig{DataDir: dataDir},
		Update:  config.UpdateConfig{GitHubRepo: "fake/oss-sync"},
	}
	mgr, err := NewManager(dataDir)
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	up, err := NewUpdater(cfg, Options{ExecPath: exePath, Verifier: func(string, string) error { return nil }})
	if err != nil {
		t.Fatalf("NewUpdater: %v", err)
	}
	return mgr, up, cfg, exePath
}

// service 测试辅助函数，与 update_test 中同名辅助区分
func makeTarGzForService(t *testing.T, entries map[string][]byte) []byte {
	t.Helper()
	// 调用 update_test 中的 makeTarGz
	return makeTarGz(t, entries)
}
func makeZipForService(t *testing.T, entries map[string][]byte) []byte {
	t.Helper()
	return makeZip(t, entries)
}
func digestOfService(t *testing.T, b []byte) string {
	t.Helper()
	return digestOfBytes(b)
}

func TestService_StartHelperUpdate_SuccessSignalsShutdown(t *testing.T) {
	mgr, up, cfg, exePath := newServiceTestManager(t)
	// 构造已校验候选
	cand := newCheckedForHelperService(t, mgr)
	svc := NewService(mgr, up, cfg)
	called := make(chan struct{}, 1)
	svc.SetOnShutdown(func() { called <- struct{}{} })

	// 桩 helper 启动成功，不拉起真实进程
	origLaunch := launchHelperFn
	launchHelperFn = func(string, string) error { return nil }
	defer func() { launchHelperFn = origLaunch }()
	origVerify := verifyStagedFileFn
	verifyStagedFileFn = func(string, string, string) error { return nil }
	defer func() { verifyStagedFileFn = origVerify }()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	op, err := svc.StartHelperUpdate(ctx, cand, "", "")
	if err != nil {
		t.Fatalf("StartHelperUpdate: %v", err)
	}
	if op == nil || op.Candidate.Version != "9.9.9" {
		t.Fatalf("op version %v", op)
	}
	// helper 启动成功后应异步触发关闭回调
	select {
	case <-called:
	case <-time.After(1 * time.Second):
		t.Fatal("shutdown not signaled after helper launch success")
	}
	// helper 尚未执行替换，可执行文件保持旧版本，仅暂存完成
	if _, err := os.Stat(exePath); err != nil {
		t.Fatalf("exe missing: %v", err)
	}
}

func TestService_StartHelperUpdate_HelperLaunchFailureNoShutdown(t *testing.T) {
	mgr, up, cfg, _ := newServiceTestManager(t)
	cand := newCheckedForHelperService(t, mgr)
	svc := NewService(mgr, up, cfg)
	called := false
	svc.SetOnShutdown(func() { called = true })
	origLaunch := launchHelperFn
	launchHelperFn = func(string, string) error { return os.ErrInvalid }
	defer func() { launchHelperFn = origLaunch }()
	origVerify := verifyStagedFileFn
	verifyStagedFileFn = func(string, string, string) error { return nil }
	defer func() { verifyStagedFileFn = origVerify }()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_, err := svc.StartHelperUpdate(ctx, cand, "", "")
	if err == nil {
		t.Fatal("expected helper launch failure")
	}
	time.Sleep(300 * time.Millisecond)
	if called {
		t.Error("shutdown should not be signaled on helper launch failure")
	}
}

func TestService_StartHelperUpdate_UsesManagerCheckedCandidate(t *testing.T) {
	mgr, up, cfg, _ := newServiceTestManager(t)
	svc := NewService(mgr, up, cfg)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	_, err := svc.StartHelperUpdate(ctx, "nonexistent", "", "")
	if err == nil {
		t.Fatal("expected check_not_found")
	}
	// 覆盖过期候选分支
	cand := newCheckedForHelperService(t, mgr)
	mgr2, _ := NewManager(mgr.root)
	// 以 1 毫秒有效期签发并等待过期，再触发即得过期错误
	cc, _ := mgr2.IssueChecked(*mustNewCandidateForService(t, "9.9.10"), 1*time.Millisecond)
	time.Sleep(5 * time.Millisecond)
	_, err = svc.StartHelperUpdate(ctx, cc.ID, "", "")
	if err == nil {
		t.Fatal("expected expired")
	}
	_ = cand
}

func newCheckedForHelperService(t *testing.T, mgr *Manager) string {
	t.Helper()
	assetName, _ := AssetName("9.9.9", runtime.GOOS, runtime.GOARCH)
	content := fakeExecBytes()
	l := strings.ToLower(assetName)
	var serveContent []byte
	if strings.HasSuffix(l, ".tar.gz") {
		serveContent = makeTarGzForService(t, map[string][]byte{"oss-server": content})
	} else if strings.HasSuffix(l, ".zip") {
		serveContent = makeZipForService(t, map[string][]byte{"oss-server": content})
	} else {
		serveContent = content
	}
	digest := digestOfService(t, serveContent)
	// 经 loopback httptest 提供下载，不依赖外部网络
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Length", strconv.Itoa(len(serveContent)))
		_, _ = w.Write(serveContent)
	}))
	t.Cleanup(srv.Close)
	assetURL := srv.URL + "/" + assetName
	releaseURL := "https://example.com/releases/tag/v9.9.9"
	c, err := NewCandidate("v9.9.9", runtime.GOOS, runtime.GOARCH, assetURL, releaseURL, int64(len(serveContent)), 1001, 2001, digest)
	if err != nil {
		t.Fatalf("NewCandidate: %v", err)
	}
	cc, err := mgr.IssueChecked(*c, time.Minute)
	if err != nil {
		t.Fatalf("IssueChecked: %v", err)
	}
	return cc.ID
}

func mustNewCandidateForService(t *testing.T, ver string) *Candidate {
	t.Helper()
	assetName, _ := AssetName(ver, runtime.GOOS, runtime.GOARCH)
	content := fakeExecBytes()
	l := strings.ToLower(assetName)
	var serveContent []byte
	if strings.HasSuffix(l, ".tar.gz") {
		serveContent = makeTarGzForService(t, map[string][]byte{"oss-server": content})
	} else if strings.HasSuffix(l, ".zip") {
		serveContent = makeZipForService(t, map[string][]byte{"oss-server": content})
	} else {
		serveContent = content
	}
	digest := digestOfService(t, serveContent)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Length", strconv.Itoa(len(serveContent)))
		_, _ = w.Write(serveContent)
	}))
	t.Cleanup(srv.Close)
	assetURL := srv.URL + "/" + assetName
	releaseURL := "https://example.com/releases/tag/v" + ver
	c, err := NewCandidate("v"+ver, runtime.GOOS, runtime.GOARCH, assetURL, releaseURL, int64(len(serveContent)), 1002, 2002, digest)
	if err != nil {
		t.Fatalf("NewCandidate %s: %v", ver, err)
	}
	return c
}

func TestService_SystemdRejectsHelperBeforeShutdown(t *testing.T) {
	mgr, up, cfg, exePath := newServiceTestManager(t)
	checkID := newCheckedForHelperService(t, mgr)
	t.Setenv("OSS_UPDATE_MANAGER", "systemd")
	svc := NewService(mgr, up, cfg)
	svc.SetOnShutdown(func() { t.Error("systemd update must not request shutdown") })
	if _, err := svc.StartHelperUpdate(context.Background(), checkID, "", ""); !IsExternalUpdateError(err) {
		t.Fatalf("expected external update requirement, got %v", err)
	}
	content, err := os.ReadFile(exePath)
	if err != nil || string(content) != "old-binary" {
		t.Fatalf("managed executable changed: %q, %v", content, err)
	}
	if status := mgr.CurrentStatus(); status.Active != nil {
		t.Fatalf("managed update must not start an operation: %+v", status.Active)
	}
}
