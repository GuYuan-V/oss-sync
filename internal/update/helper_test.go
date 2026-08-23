package update

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/oss/oss-server/internal/version"
)

func fakeDigestForFile(path string) string {
	data, _ := os.ReadFile(path)
	h := sha256.Sum256(data)
	return "sha256:" + hex.EncodeToString(h[:])
}

func writeOldExec(t *testing.T, dir, name, content string) string {
	t.Helper()
	p := filepath.Join(dir, name)
	if err := os.WriteFile(p, []byte(content), 0o755); err != nil {
		t.Fatalf("write exec: %v", err)
	}
	return p
}

func newCheckedForHelper(t *testing.T, m *Manager, ver string) string {
	t.Helper()
	assetName, err := AssetName(ver, runtime.GOOS, runtime.GOARCH)
	if err != nil {
		t.Fatalf("AssetName: %v", err)
	}
	assetURL := "https://example.com/" + assetName
	releaseURL := "https://example.com/releases/tag/v" + ver
	tmp := t.TempDir()
	candFile := filepath.Join(tmp, "cand-"+ver)
	content := fakeExecBytes()
	if err := os.WriteFile(candFile, content, 0o755); err != nil {
		t.Fatal(err)
	}
	dig := fakeDigestForFile(candFile)
	c2, err := NewCandidate("v"+ver, runtime.GOOS, runtime.GOARCH, assetURL, releaseURL, 1234, 1, 1, dig)
	if err != nil {
		t.Fatalf("NewCandidate: %v", err)
	}
	cc, err := m.IssueChecked(*c2, time.Minute)
	if err != nil {
		t.Fatalf("IssueChecked: %v", err)
	}
	helperCandFiles[cc.ID] = candFile
	return cc.ID
}

var helperCandFiles = map[string]string{}

func candidatePathFor(checkID string) string {
	if p, ok := helperCandFiles[checkID]; ok {
		return p
	}
	return ""
}

func withStableVersion(t *testing.T) func() {
	t.Helper()
	orig := version.Version
	version.Version = "1.0.0"
	return func() { version.Version = orig }
}

func TestHelper_LaunchFailure(t *testing.T) {
	defer withStableVersion(t)()
	origLaunch := launchHelperFn
	defer func() { launchHelperFn = origLaunch }()
	launchHelperFn = func(string, string) error { return os.ErrInvalid }
	origVerify := verifyStagedFileFn
	verifyStagedFileFn = func(string, string, string) error { return nil }
	defer func() { verifyStagedFileFn = origVerify }()

	exeDir := t.TempDir()
	exePath := writeOldExec(t, exeDir, "oss-server", "old-binary")
	mgrRoot := t.TempDir()
	mgr, err := NewManager(mgrRoot)
	if err != nil {
		t.Fatal(err)
	}
	id := newCheckedForHelper(t, mgr, "9.9.1")
	candPath := candidatePathFor(id)
	u, err := NewUpdater(testCfg(), Options{ExecPath: exePath, Verifier: func(string, string) error { return nil }})
	if err != nil {
		t.Fatal(err)
	}
	_, err = u.InitiateHelperHandoff(mgr, id, candPath, "http://127.0.0.1:0/readyz", []string{exePath}, exeDir)
	if err == nil {
		t.Fatal("expected launch failure")
	}
	if mgr.ActiveOperation() != nil {
		t.Errorf("active should be nil after launch failure, got %v", mgr.ActiveOperation().State)
	}
	markerGlob, _ := filepath.Glob(filepath.Join(exeDir, ".oss-update-pending", "*.handoff.json"))
	if len(markerGlob) != 0 {
		t.Errorf("marker should be removed on launch failure, found %v", markerGlob)
	}
}

func TestHelper_ParentWaitFailureTriggersRollback(t *testing.T) {
	defer withStableVersion(t)()
	origWait := waitForParentFn
	waitForParentFn = func(int, time.Duration) error { return os.ErrDeadlineExceeded }
	defer func() { waitForParentFn = origWait }()
	origVerify := verifyStagedFileFn
	verifyStagedFileFn = func(string, string, string) error { return nil }
	defer func() { verifyStagedFileFn = origVerify }()
	origProbe := probeReadyzWithVersionFn
	probeReadyzWithVersionFn = func(string, string, time.Duration, time.Duration) error { return nil }
	defer func() { probeReadyzWithVersionFn = origProbe }()
	origStart := startNewServerFn
	startNewServerFn = func(*HandoffMarker) (*exec.Cmd, error) { return &exec.Cmd{}, nil }
	defer func() { startNewServerFn = origStart }()

	exeDir := t.TempDir()
	exePath := writeOldExec(t, exeDir, "oss-server", "old-binary")
	mgrRoot := t.TempDir()
	mgr, _ := NewManager(mgrRoot)
	id := newCheckedForHelper(t, mgr, "9.9.2")
	candPath := candidatePathFor(id)
	u, _ := NewUpdater(testCfg(), Options{ExecPath: exePath, Verifier: func(string, string) error { return nil }})
	origLaunch := launchHelperFn
	launched := ""
	launchHelperFn = func(ep, mp string) error { launched = mp; return nil }
	defer func() { launchHelperFn = origLaunch }()

	op, err := u.InitiateHelperHandoff(mgr, id, candPath, "http://127.0.0.1:0/readyz", []string{exePath}, exeDir)
	if err != nil {
		t.Fatalf("init handoff: %v", err)
	}
	if launched == "" {
		t.Fatal("not launched")
	}
	code := RunHelper(launched)
	if code == 0 {
		t.Errorf("expected failure code for parent wait")
	}
	data, _ := os.ReadFile(exePath)
	if string(data) != "old-binary" {
		t.Errorf("after parent wait rollback, exe should be old-binary, got %q", string(data))
	}
	op2, _ := mgr.GetOperation(op.ID)
	if op2.State != StateFailed {
		t.Errorf("operation should be failed after parent wait, got %q", op2.State)
	}
	if _, err := os.Stat(launched); !os.IsNotExist(err) {
		t.Errorf("marker should be removed after rollback")
	}
}

func TestHelper_MissingStagedFileTriggersRollback(t *testing.T) {
	defer withStableVersion(t)()
	origVerify := verifyStagedFileFn
	verifyStagedFileFn = func(string, string, string) error { return nil }
	defer func() { verifyStagedFileFn = origVerify }()
	origWait := waitForParentFn
	waitForParentFn = func(int, time.Duration) error { return nil }
	defer func() { waitForParentFn = origWait }()

	exeDir := t.TempDir()
	exePath := writeOldExec(t, exeDir, "oss-server", "old-binary")
	mgrRoot := t.TempDir()
	mgr, _ := NewManager(mgrRoot)
	id := newCheckedForHelper(t, mgr, "9.9.3")
	candPath := candidatePathFor(id)
	u, _ := NewUpdater(testCfg(), Options{ExecPath: exePath, Verifier: func(string, string) error { return nil }})
	origLaunch := launchHelperFn
	var markerPath string
	launchHelperFn = func(ep, mp string) error { markerPath = mp; return nil }
	defer func() { launchHelperFn = origLaunch }()
	op, err := u.InitiateHelperHandoff(mgr, id, candPath, "http://127.0.0.1:0/readyz", []string{exePath}, exeDir)
	if err != nil {
		t.Fatalf("init: %v", err)
	}
	data, _ := os.ReadFile(markerPath)
	var m HandoffMarker
	_ = json.Unmarshal(data, &m)
	_ = os.Remove(m.StagedPath)
	verifyStagedFileFn = verifyStagedFile
	code := RunHelper(markerPath)
	if code == 0 {
		t.Error("expected failure for missing staged")
	}
	op2, _ := mgr.GetOperation(op.ID)
	if op2.State != StateFailed {
		t.Errorf("should be failed, got %q", op2.State)
	}
	got, _ := os.ReadFile(exePath)
	if string(got) != "old-binary" {
		t.Errorf("rollback should restore old-binary, got %q", string(got))
	}
}

func TestHelper_SwapFailureTriggersRollback(t *testing.T) {
	defer withStableVersion(t)()
	origVerify := verifyStagedFileFn
	verifyStagedFileFn = func(string, string, string) error { return nil }
	defer func() { verifyStagedFileFn = origVerify }()
	origAtomic := atomicReplaceFn
	atomicReplaceFn = func(string, string) error { return os.ErrPermission }
	defer func() { atomicReplaceFn = origAtomic }()
	origWait := waitForParentFn
	waitForParentFn = func(int, time.Duration) error { return nil }
	defer func() { waitForParentFn = origWait }()

	exeDir := t.TempDir()
	exePath := writeOldExec(t, exeDir, "oss-server", "old-binary")
	mgrRoot := t.TempDir()
	mgr, _ := NewManager(mgrRoot)
	id := newCheckedForHelper(t, mgr, "9.9.4")
	candPath := candidatePathFor(id)
	u, _ := NewUpdater(testCfg(), Options{ExecPath: exePath, Verifier: func(string, string) error { return nil }})
	var markerPath string
	origLaunch := launchHelperFn
	launchHelperFn = func(ep, mp string) error { markerPath = mp; return nil }
	defer func() { launchHelperFn = origLaunch }()
	op, err := u.InitiateHelperHandoff(mgr, id, candPath, "http://127.0.0.1:0/readyz", []string{exePath}, exeDir)
	if err != nil {
		t.Fatalf("init: %v", err)
	}
	code := RunHelper(markerPath)
	if code == 0 {
		t.Error("expected swap failure")
	}
	op2, _ := mgr.GetOperation(op.ID)
	if op2.State != StateFailed {
		t.Errorf("should be failed, got %q", op2.State)
	}
}

func TestHelper_WrongVersionTriggersRollback(t *testing.T) {
	defer withStableVersion(t)()
	origVerify := verifyStagedFileFn
	verifyStagedFileFn = func(string, string, string) error { return nil }
	defer func() { verifyStagedFileFn = origVerify }()
	origWait := waitForParentFn
	waitForParentFn = func(int, time.Duration) error { return nil }
	defer func() { waitForParentFn = origWait }()
	origAtomic := atomicReplaceFn
	atomicReplaceFn = func(staged, target string) error {
		data, _ := os.ReadFile(staged)
		_ = os.WriteFile(target, data, 0o755)
		_ = os.Remove(staged)
		return nil
	}
	defer func() { atomicReplaceFn = origAtomic }()
	origStart := startNewServerFn
	startNewServerFn = func(*HandoffMarker) (*exec.Cmd, error) { return &exec.Cmd{}, nil }
	defer func() { startNewServerFn = origStart }()
	origProbe := probeReadyzWithVersionFn
	probeReadyzWithVersionFn = func(url, want string, timeout, interval time.Duration) error { return os.ErrInvalid }
	defer func() { probeReadyzWithVersionFn = origProbe }()

	exeDir := t.TempDir()
	exePath := writeOldExec(t, exeDir, "oss-server", "old-binary")
	mgrRoot := t.TempDir()
	mgr, _ := NewManager(mgrRoot)
	id := newCheckedForHelper(t, mgr, "9.9.5")
	candPath := candidatePathFor(id)
	_ = os.WriteFile(candPath, []byte("new-binary"), 0o755)
	u, _ := NewUpdater(testCfg(), Options{ExecPath: exePath, Verifier: func(string, string) error { return nil }})
	var markerPath string
	origLaunch := launchHelperFn
	launchHelperFn = func(ep, mp string) error { markerPath = mp; return nil }
	defer func() { launchHelperFn = origLaunch }()
	op, err := u.InitiateHelperHandoff(mgr, id, candPath, "http://127.0.0.1:0/readyz", []string{exePath}, exeDir)
	if err != nil {
		t.Fatalf("init: %v", err)
	}
	code := RunHelper(markerPath)
	if code == 0 {
		t.Error("expected wrong version failure")
	}
	op2, _ := mgr.GetOperation(op.ID)
	if op2.State != StateFailed {
		t.Errorf("should be failed on wrong version, got %q", op2.State)
	}
	got, _ := os.ReadFile(exePath)
	if string(got) != "old-binary" {
		t.Errorf("rollback should restore old-binary, got %q", string(got))
	}
}

func TestHelper_ReadinessTimeoutTriggersRollback(t *testing.T) {
	defer withStableVersion(t)()
	origVerify := verifyStagedFileFn
	verifyStagedFileFn = func(string, string, string) error { return nil }
	defer func() { verifyStagedFileFn = origVerify }()
	origWait := waitForParentFn
	waitForParentFn = func(int, time.Duration) error { return nil }
	defer func() { waitForParentFn = origWait }()
	origAtomic := atomicReplaceFn
	atomicReplaceFn = func(staged, target string) error {
		data, _ := os.ReadFile(staged)
		_ = os.WriteFile(target, data, 0o755)
		_ = os.Remove(staged)
		return nil
	}
	defer func() { atomicReplaceFn = origAtomic }()
	origStart := startNewServerFn
	startNewServerFn = func(*HandoffMarker) (*exec.Cmd, error) { return &exec.Cmd{}, nil }
	defer func() { startNewServerFn = origStart }()
	origProbe := probeReadyzWithVersionFn
	probeReadyzWithVersionFn = func(string, string, time.Duration, time.Duration) error { return os.ErrDeadlineExceeded }
	defer func() { probeReadyzWithVersionFn = origProbe }()

	exeDir := t.TempDir()
	exePath := writeOldExec(t, exeDir, "oss-server", "old-binary")
	mgrRoot := t.TempDir()
	mgr, _ := NewManager(mgrRoot)
	id := newCheckedForHelper(t, mgr, "9.9.6")
	candPath := candidatePathFor(id)
	_ = os.WriteFile(candPath, []byte("new-binary"), 0o755)
	u, _ := NewUpdater(testCfg(), Options{ExecPath: exePath, Verifier: func(string, string) error { return nil }})
	var markerPath string
	origLaunch := launchHelperFn
	launchHelperFn = func(ep, mp string) error { markerPath = mp; return nil }
	defer func() { launchHelperFn = origLaunch }()
	op, err := u.InitiateHelperHandoff(mgr, id, candPath, "http://127.0.0.1:0/readyz", []string{exePath}, exeDir)
	if err != nil {
		t.Fatalf("init: %v", err)
	}
	code := RunHelper(markerPath)
	if code == 0 {
		t.Error("expected timeout failure")
	}
	op2, _ := mgr.GetOperation(op.ID)
	if op2.State != StateFailed {
		t.Errorf("should be failed on timeout, got %q", op2.State)
	}
}

func TestHelper_SuccessfulHandoff(t *testing.T) {
	defer withStableVersion(t)()
	origVerify := verifyStagedFileFn
	verifyStagedFileFn = func(string, string, string) error { return nil }
	defer func() { verifyStagedFileFn = origVerify }()
	origWait := waitForParentFn
	waitForParentFn = func(int, time.Duration) error { return nil }
	defer func() { waitForParentFn = origWait }()
	origAtomic := atomicReplaceFn
	atomicReplaceFn = func(staged, target string) error {
		data, _ := os.ReadFile(staged)
		_ = os.WriteFile(target, data, 0o755)
		_ = os.Remove(staged)
		return nil
	}
	defer func() { atomicReplaceFn = origAtomic }()
	origStart := startNewServerFn
	startNewServerFn = func(*HandoffMarker) (*exec.Cmd, error) { return &exec.Cmd{}, nil }
	defer func() { startNewServerFn = origStart }()
	origProbe := probeReadyzWithVersionFn
	probeReadyzWithVersionFn = func(string, string, time.Duration, time.Duration) error { return nil }
	defer func() { probeReadyzWithVersionFn = origProbe }()

	exeDir := t.TempDir()
	exePath := writeOldExec(t, exeDir, "oss-server", "old-binary")
	mgrRoot := t.TempDir()
	mgr, _ := NewManager(mgrRoot)
	id := newCheckedForHelper(t, mgr, "9.9.7")
	candPath := candidatePathFor(id)
	_ = os.WriteFile(candPath, []byte("new-binary-v9.9.7"), 0o755)
	u, _ := NewUpdater(testCfg(), Options{ExecPath: exePath, Verifier: func(string, string) error { return nil }})
	var markerPath string
	origLaunch := launchHelperFn
	launchHelperFn = func(ep, mp string) error { markerPath = mp; return nil }
	defer func() { launchHelperFn = origLaunch }()
	op, err := u.InitiateHelperHandoff(mgr, id, candPath, "http://127.0.0.1:0/readyz", []string{exePath, "--custom-flag"}, exeDir)
	if err != nil {
		t.Fatalf("init: %v", err)
	}
	code := RunHelper(markerPath)
	if code != 0 {
		t.Fatalf("successful handoff should return 0, got %d", code)
	}
	got, _ := os.ReadFile(exePath)
	if string(got) != "new-binary-v9.9.7" {
		t.Errorf("exe should be new binary, got %q", string(got))
	}
	if _, err := os.Stat(markerPath); !os.IsNotExist(err) {
		t.Error("marker should be removed after success")
	}
	op2, _ := mgr.GetOperation(op.ID)
	if op2.State != StateDone {
		t.Errorf("operation should be done, got %q", op2.State)
	}
	if mgr.ActiveOperation() != nil {
		t.Error("active should be nil after done")
	}
}

func TestHelper_RollbackDoesNotTriggerOnOrdinaryStartup(t *testing.T) {
	code := RunHelper(filepath.Join(t.TempDir(), "nonexistent.handoff.json"))
	if code != 0 {
		t.Errorf("ordinary startup without marker should return 0, got %d", code)
	}
	exeDir := t.TempDir()
	exePath := writeOldExec(t, exeDir, "oss-server", "old-binary")
	mgrRoot := t.TempDir()
	mgr, _ := NewManager(mgrRoot)
	id := newCheckedForHelper(t, mgr, "9.9.8")
	op, _ := mgr.StartOperation(id, "9.9.8")
	_, _ = mgr.Transition(op.ID, StateFailed, "already done")
	marker := HandoffMarker{
		OpID:          op.ID,
		ManagerRoot:   mgrRoot,
		ExecPath:      exePath,
		StagedPath:    filepath.Join(exeDir, "staged-missing"),
		BackupPath:    filepath.Join(exeDir, "backup-missing"),
		TargetVersion: "9.9.8",
		ParentPID:     999999,
		ReadyURL:      "http://127.0.0.1:0/readyz",
	}
	markerPath := filepath.Join(exeDir, "terminal.handoff.json")
	data, _ := json.Marshal(marker)
	_ = os.WriteFile(markerPath, data, 0o644)
	code = RunHelper(markerPath)
	if code != 0 {
		t.Logf("helper returned %d for terminal marker, expected 0", code)
	}
	got, _ := os.ReadFile(exePath)
	if string(got) != "old-binary" {
		t.Errorf("ordinary startup should not modify exe, got %q", string(got))
	}
}

func TestHelper_MarkerNeverContainsTokens(t *testing.T) {
	defer withStableVersion(t)()
	exeDir := t.TempDir()
	exePath := writeOldExec(t, exeDir, "oss-server", "old-binary")
	mgrRoot := t.TempDir()
	mgr, _ := NewManager(mgrRoot)
	id := newCheckedForHelper(t, mgr, "9.9.9")
	candPath := candidatePathFor(id)
	origVerify := verifyStagedFileFn
	verifyStagedFileFn = func(string, string, string) error { return nil }
	defer func() { verifyStagedFileFn = origVerify }()
	u, _ := NewUpdater(testCfg(), Options{ExecPath: exePath, Verifier: func(string, string) error { return nil }})
	t.Setenv("OSS_GITHUB_TOKEN", "super-secret-token")
	var markerPath string
	origLaunch := launchHelperFn
	launchHelperFn = func(ep, mp string) error { markerPath = mp; return nil }
	defer func() { launchHelperFn = origLaunch }()
	_, err := u.InitiateHelperHandoff(mgr, id, candPath, "http://127.0.0.1:0/readyz", []string{exePath}, exeDir)
	if err != nil {
		t.Fatalf("init: %v", err)
	}
	data, _ := os.ReadFile(markerPath)
	if strings.Contains(string(data), "super-secret-token") {
		t.Error("marker must not contain tokens")
	}
	if strings.Contains(string(data), "Authorization") {
		t.Error("marker must not contain Authorization")
	}
	var m HandoffMarker
	_ = json.Unmarshal(data, &m)
	if m.Digest == "" || m.TargetVersion == "" {
		t.Error("marker missing required fields")
	}
}
