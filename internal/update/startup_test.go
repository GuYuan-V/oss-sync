package update

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"
)

// readyzServer 模拟 /readyz：ready=true 时返回 200，否则返回 503。
func readyzServer(ready bool) *httptest.Server {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if ready {
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"ready":true,"open_storage_issues":0}`))
			return
		}
		w.WriteHeader(http.StatusServiceUnavailable)
		_, _ = w.Write([]byte(`{"ready":false}`))
	}))
	return srv
}

// healthUpdater 构造仅用于自检/回滚测试的 Updater（不访问 GitHub）。
func healthUpdater(t *testing.T, exePath string) *Updater {
	t.Helper()
	u, err := NewUpdater(testCfg(), Options{
		ExecPath: exePath,
		Verifier: func(string, string) error { return nil },
	})
	if err != nil {
		t.Fatalf("NewUpdater: %v", err)
	}
	return u
}

func TestStartupHealthCheck_ReadyKeepsNewBinary(t *testing.T) {
	srv := readyzServer(true)
	defer srv.Close()

	dir := t.TempDir()
	exePath := filepath.Join(dir, "oss-server")
	if err := os.WriteFile(exePath, []byte("new-binary"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(exePath+".bak", []byte("old-binary"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(exePath+".updated", []byte("1.2.3"), 0o644); err != nil {
		t.Fatal(err)
	}
	u := healthUpdater(t, exePath)

	if err := u.StartupHealthCheck(context.Background(), srv.URL, 10*time.Millisecond, time.Second); err != nil {
		t.Fatalf("就绪时不应返回错误: %v", err)
	}
	got, _ := os.ReadFile(exePath)
	if string(got) != "new-binary" {
		t.Errorf("就绪时不应回滚，当前二进制 = %q", got)
	}
	if _, err := os.Stat(exePath + ".updated"); !os.IsNotExist(err) {
		t.Error("待验证标记应在自检后被消费")
	}
	if _, err := os.Stat(exePath + ".bak"); err != nil {
		t.Errorf("就绪时不应消费备份二进制: %v", err)
	}
}

func TestStartupHealthCheck_RollbackWhenNotReady(t *testing.T) {
	srv := readyzServer(false)
	defer srv.Close()

	dir := t.TempDir()
	exePath := filepath.Join(dir, "oss-server")
	if err := os.WriteFile(exePath, []byte("new-binary"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(exePath+".bak", []byte("old-binary"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(exePath+".updated", []byte("1.2.3"), 0o644); err != nil {
		t.Fatal(err)
	}
	u := healthUpdater(t, exePath)

	err := u.StartupHealthCheck(context.Background(), srv.URL, 10*time.Millisecond, 200*time.Millisecond)
	if !errors.Is(err, ErrRollbackRestart) {
		t.Fatalf("未就绪时应回滚并返回 ErrRollbackRestart，得到 %v", err)
	}
	got, _ := os.ReadFile(exePath)
	if string(got) != "old-binary" {
		t.Errorf("回滚后当前二进制应为备份内容，得到 %q", got)
	}
	if _, err := os.Stat(exePath + ".bak"); !os.IsNotExist(err) {
		t.Errorf("回滚后备份二进制应被消费: %v", err)
	}
}

func TestStartupHealthCheck_SkipsWithoutMarker(t *testing.T) {
	srv := readyzServer(false)
	defer srv.Close()

	dir := t.TempDir()
	exePath := filepath.Join(dir, "oss-server")
	if err := os.WriteFile(exePath, []byte("current-binary"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(exePath+".bak", []byte("old-binary"), 0o644); err != nil {
		t.Fatal(err)
	}
	u := healthUpdater(t, exePath)

	if err := u.StartupHealthCheck(context.Background(), srv.URL, 10*time.Millisecond, 100*time.Millisecond); err != nil {
		t.Fatalf("无待验证标记时不应运行自检，得到 %v", err)
	}
	got, _ := os.ReadFile(exePath)
	if string(got) != "current-binary" {
		t.Errorf("无标记时不应改动二进制，得到 %q", got)
	}
}

func TestStartupHealthCheck_NoRollbackWhenCanceled(t *testing.T) {
	srv := readyzServer(false)
	defer srv.Close()

	dir := t.TempDir()
	exePath := filepath.Join(dir, "oss-server")
	if err := os.WriteFile(exePath, []byte("new-binary"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(exePath+".bak", []byte("old-binary"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(exePath+".updated", []byte("1.2.3"), 0o644); err != nil {
		t.Fatal(err)
	}
	u := healthUpdater(t, exePath)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	err := u.StartupHealthCheck(ctx, srv.URL, 10*time.Millisecond, time.Second)
	if err == nil {
		t.Fatal("取消的上下文应返回错误")
	}
	if errors.Is(err, ErrRollbackRestart) {
		t.Fatalf("上下文取消时不应回滚，得到 %v", err)
	}
	got, _ := os.ReadFile(exePath)
	if string(got) != "new-binary" {
		t.Errorf("上下文取消时不应改动二进制，得到 %q", got)
	}
}

func TestCheckReady_SucceedsAfterRetries(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if calls.Add(1) < 3 {
			w.WriteHeader(http.StatusServiceUnavailable)
			_, _ = w.Write([]byte(`{"ready":false}`))
			return
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"ready":true}`))
	}))
	defer srv.Close()

	if err := CheckReady(context.Background(), srv.URL, 10*time.Millisecond, time.Second); err != nil {
		t.Fatalf("重试后应当就绪: %v", err)
	}
	if calls.Load() < 3 {
		t.Fatalf("应至少轮询 3 次，实际 %d 次", calls.Load())
	}
}

func TestRollback_ReplacesCurrentWithBackup(t *testing.T) {
	dir := t.TempDir()
	exePath := filepath.Join(dir, "oss-server")
	if err := os.WriteFile(exePath, []byte("new-binary"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(exePath+".bak", []byte("old-binary"), 0o644); err != nil {
		t.Fatal(err)
	}
	u := healthUpdater(t, exePath)

	if err := u.Rollback(); err != nil {
		t.Fatalf("Rollback: %v", err)
	}
	got, _ := os.ReadFile(exePath)
	if string(got) != "old-binary" {
		t.Errorf("回滚后当前二进制 = %q, 期望 old-binary", got)
	}
}

func TestRollback_MissingBackup(t *testing.T) {
	dir := t.TempDir()
	exePath := filepath.Join(dir, "oss-server")
	if err := os.WriteFile(exePath, []byte("new-binary"), 0o755); err != nil {
		t.Fatal(err)
	}
	u := healthUpdater(t, exePath)

	if err := u.Rollback(); err == nil {
		t.Fatal("无备份时应返回错误")
	}
	got, _ := os.ReadFile(exePath)
	if string(got) != "new-binary" {
		t.Errorf("无备份时不应改动二进制，得到 %q", got)
	}
}
