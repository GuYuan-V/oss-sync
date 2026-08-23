package update

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/oss/oss-server/internal/config"
	"github.com/oss/oss-server/internal/version"
)

func TestRegression_VersionExactEquality_1_2_3_vs_1_2_30(t *testing.T) {
	if !strings.Contains("1.2.30", "1.2.3") {
		t.Fatal("sanity: Contains should be true")
	}
	if version.Normalize("1.2.30") == version.Normalize("1.2.3") {
		t.Fatal("exact normalized equality should fail for 1.2.30 vs 1.2.3")
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"ready":true,"version":"1.2.30"}`))
	}))
	defer srv.Close()
	err := probeReadyzWithVersion(srv.URL, "1.2.3", 300*time.Millisecond, 50*time.Millisecond)
	if err == nil {
		t.Fatal("probe should fail for version mismatch 1.2.30 vs 1.2.3")
	}
	if !strings.Contains(err.Error(), "1.2.30") {
		t.Errorf("error should mention got version, got %v", err)
	}
	srv2 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"ready":true,"version":"1.2.3"}`))
	}))
	defer srv2.Close()
	if err := probeReadyzWithVersion(srv2.URL, "1.2.3", 300*time.Millisecond, 50*time.Millisecond); err != nil {
		t.Fatalf("probe exact match should succeed: %v", err)
	}
	if version.Normalize("v1.2.3") != version.Normalize("1.2.3") {
		t.Error("Normalize should handle v prefix exactly")
	}
}

func TestRegression_CorruptMarkerNoRollback(t *testing.T) {
	exeDir := t.TempDir()
	exePath := filepath.Join(exeDir, "oss-server")
	backupPath := filepath.Join(exeDir, "oss-server.bak")
	_ = os.WriteFile(exePath, []byte("old-binary"), 0o755)
	_ = os.WriteFile(backupPath, []byte("old-binary"), 0o644)
	markerPath := filepath.Join(exeDir, "corrupt.handoff.json")
	_ = os.WriteFile(markerPath, []byte("{ invalid json"), 0o644)
	origExe, _ := os.ReadFile(exePath)
	code := RunHelper(markerPath)
	if code != 2 {
		t.Errorf("corrupt marker should return 2, got %d", code)
	}
	got, _ := os.ReadFile(exePath)
	if string(got) != string(origExe) {
		t.Errorf("corrupt marker should not modify exe, got %q", string(got))
	}
	if _, err := os.Stat(backupPath); err != nil {
		t.Errorf("backup should still exist after corrupt marker")
	}
}

func TestRegression_OrdinaryFailedStartupNoBackupReplacement(t *testing.T) {
	exeDir := t.TempDir()
	exePath := filepath.Join(exeDir, "oss-server")
	backupPath := filepath.Join(exeDir, ".oss-update-pending", "backup-ordinary")
	_ = os.MkdirAll(filepath.Dir(backupPath), 0o755)
	_ = os.WriteFile(exePath, []byte("current-binary"), 0o755)
	_ = os.WriteFile(backupPath, []byte("backup-binary"), 0o644)
	code := RunHelper(filepath.Join(exeDir, "nonexistent.handoff.json"))
	if code != 0 {
		t.Errorf("ordinary startup without marker should return 0, got %d", code)
	}
	got, _ := os.ReadFile(exePath)
	if string(got) != "current-binary" {
		t.Errorf("ordinary failed startup should not replace exe with backup, got %q", string(got))
	}
	if _, err := os.Stat(backupPath); err != nil {
		t.Errorf("backup should still exist after ordinary failure")
	}
}

func TestRegression_HelperOwnedRollbackRelaunch(t *testing.T) {
	origVer := version.Version
	version.Version = "1.0.0"
	t.Cleanup(func() { version.Version = origVer })
	exeDir := t.TempDir()
	exePath := filepath.Join(exeDir, "oss-server")
	_ = os.WriteFile(exePath, []byte("old-binary"), 0o755)
	mgrRoot := t.TempDir()
	mgr, _ := NewManager(mgrRoot)
	assetName, _ := AssetName("9.9.9", "linux", "amd64")
	content := fakeExecBytes()
	serveContent := makeTarGz(t, map[string][]byte{"oss-server": content})
	digest := digestOfBytes(serveContent)
	// candidate with loopback URL so download would succeed if needed, but we use direct file path for handoff
	cand, _ := NewCandidate("v9.9.9", "linux", "amd64", "https://example.com/"+assetName, "https://example.com/releases/tag/v9.9.9", int64(len(serveContent)), 1001, 2001, digest)
	// For this regression we need candidate file path with correct digest and magic
	candPath := filepath.Join(t.TempDir(), "cand")
	_ = os.WriteFile(candPath, serveContent, 0o755)
	// recompute digest for file to match candidate – use file's actual digest
	// Overwrite candidate digest to match file
	cand2, _ := NewCandidate("v9.9.9", "linux", "amd64", "https://example.com/"+assetName, "https://example.com/releases/tag/v9.9.9", int64(len(serveContent)), 1001, 2001, digestOfBytes(serveContent))
	cc, _ := mgr.IssueChecked(*cand2, time.Minute)
	_ = cand // avoid unused
	origVerify := verifyStagedFileFn
	verifyStagedFileFn = func(string, string, string) error { return nil }
	t.Cleanup(func() { verifyStagedFileFn = origVerify })
	origWait := waitForParentFn
	waitForParentFn = func(int, time.Duration) error { return nil }
	t.Cleanup(func() { waitForParentFn = origWait })
	origLaunch := launchHelperFn
	var markerPath string
	launchHelperFn = func(ep, mp string) error { markerPath = mp; return nil }
	t.Cleanup(func() { launchHelperFn = origLaunch })
	origAtomic := atomicReplaceFn
	atomicReplaceFn = func(staged, target string) error {
		data, _ := os.ReadFile(staged)
		_ = os.WriteFile(target, data, 0o755)
		_ = os.Remove(staged)
		return nil
	}
	t.Cleanup(func() { atomicReplaceFn = origAtomic })
	origProbe := probeReadyzWithVersionFn
	probeReadyzWithVersionFn = func(string, string, time.Duration, time.Duration) error { return os.ErrInvalid }
	t.Cleanup(func() { probeReadyzWithVersionFn = origProbe })
	origStart := startNewServerFn
	startNewServerFn = func(*HandoffMarker) (*exec.Cmd, error) { return &exec.Cmd{}, nil }
	t.Cleanup(func() { startNewServerFn = origStart })
	relaunchCalled := false
	origRelaunch := relaunchOldServerFn
	relaunchOldServerFn = func(m *HandoffMarker) { relaunchCalled = true }
	t.Cleanup(func() { relaunchOldServerFn = origRelaunch })
	cfg := &config.Config{Server: config.ServerConfig{Host: "127.0.0.1", Port: 8080}, Storage: config.StorageConfig{DataDir: mgrRoot}}
	up, _ := NewUpdater(cfg, Options{ExecPath: exePath, Verifier: func(string, string) error { return nil }})
	op, err := up.InitiateHelperHandoff(mgr, cc.ID, candPath, "http://127.0.0.1:0/readyz", []string{exePath}, exeDir)
	if err != nil {
		t.Fatalf("InitiateHelperHandoff: %v", err)
	}
	code := RunHelper(markerPath)
	if code == 0 {
		t.Error("expected helper to fail and rollback")
	}
	if !relaunchCalled {
		t.Error("helper-owned rollback should relaunch old server")
	}
	got, _ := os.ReadFile(exePath)
	if string(got) != "old-binary" {
		t.Errorf("after rollback exe should be old-binary, got %q", string(got))
	}
	op2, _ := mgr.GetOperation(op.ID)
	if op2.State != StateFailed {
		t.Errorf("operation should be failed after rollback, got %v", op2.State)
	}
}

func TestRegression_MarkerAtomicWrite(t *testing.T) {
	exeDir := t.TempDir()
	exePath := filepath.Join(exeDir, "oss-server")
	_ = os.WriteFile(exePath, []byte("old"), 0o755)
	marker := HandoffMarker{
		OpID:          "test-op",
		ManagerRoot:   t.TempDir(),
		ExecPath:      exePath,
		StagedPath:    filepath.Join(exeDir, ".oss-update-pending", "staged-test-op"),
		BackupPath:    filepath.Join(exeDir, ".oss-update-pending", "backup-test-op"),
		HelperPath:    filepath.Join(exeDir, ".oss-update-pending", "helper-test-op"),
		TargetVersion: "1.2.3",
		Digest:        "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		ParentPID:     1234,
		ReadyURL:      "http://127.0.0.1:8080/readyz",
	}
	markerPath := filepath.Join(exeDir, ".oss-update-pending", "test-op.handoff.json")
	if err := atomicWriteMarker(markerPath, marker); err != nil {
		t.Fatalf("atomicWriteMarker: %v", err)
	}
	data, err := os.ReadFile(markerPath)
	if err != nil {
		t.Fatalf("read marker: %v", err)
	}
	var got HandoffMarker
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if got.OpID != "test-op" {
		t.Errorf("opID %q", got.OpID)
	}
	// no temp file should remain
	matches, _ := filepath.Glob(markerPath + ".tmp.*")
	if len(matches) != 0 {
		t.Errorf("temp files remain %v", matches)
	}
}
