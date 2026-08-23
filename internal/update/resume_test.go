package update

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/oss/oss-server/internal/version"
)

func TestResumePendingHandoffs_CrashAfterMarkerBeforeHelperLaunch(t *testing.T) {
	origVer := version.Version
	version.Version = "1.0.0"
	t.Cleanup(func() { version.Version = origVer })
	exeDir := t.TempDir()
	exePath := filepath.Join(exeDir, "oss-server")
	_ = os.WriteFile(exePath, []byte("old-binary"), 0o755)
	mgrRoot := t.TempDir()
	mgr, _ := NewManager(mgrRoot)
	// create checked candidate
	assetName, _ := AssetName("9.9.9", "linux", "amd64")
	content := fakeExecBytes()
	serveContent := makeTarGz(t, map[string][]byte{"oss-server": content})
	digest := digestOfBytes(serveContent)
	cand, _ := NewCandidate("v9.9.9", "linux", "amd64", "https://example.com/"+assetName, "https://example.com/releases/tag/v9.9.9", int64(len(serveContent)), 1001, 2001, digest)
	cc, _ := mgr.IssueChecked(*cand, time.Minute)
	// Simulate crash after marker write but before helper launch:
	// Manually create marker via atomicWriteMarker with active operation
	op, _ := mgr.StartOperation(cc.ID, "9.9.9")
	// Drive to swap state as InitiateHelperHandoff would
	seq := []OperationState{StatePrepare, StateFetchRelease, StateSelectAsset, StateDownload, StateVerify, StateBackup, StateSwap}
	for _, nxt := range seq {
		cur, _ := mgr.GetOperation(op.ID)
		if isAllowedTransition(cur.State, nxt) {
			mgr.Transition(op.ID, nxt, "")
		}
	}
	// stage files
	staged := filepath.Join(exeDir, ".oss-update-pending", "staged-"+op.ID)
	backup := filepath.Join(exeDir, ".oss-update-pending", "backup-"+op.ID)
	helperCopy := filepath.Join(exeDir, ".oss-update-pending", "helper-"+op.ID)
	_ = os.MkdirAll(filepath.Join(exeDir, ".oss-update-pending"), 0o755)
	_ = os.WriteFile(staged, serveContent, 0o755)
	_ = os.WriteFile(backup, []byte("old-binary"), 0o644)
	_ = os.WriteFile(helperCopy, []byte("old-binary"), 0o755)
	marker := HandoffMarker{
		OpID:          op.ID,
		ManagerRoot:   mgrRoot,
		ExecPath:      exePath,
		StagedPath:    staged,
		BackupPath:    backup,
		HelperPath:    helperCopy,
		TargetVersion: "9.9.9",
		Digest:        digest,
		ParentPID:     99999, // dead PID
		ReadyURL:      "http://127.0.0.1:0/readyz",
		OrigArgs:      []string{exePath},
		WorkDir:       exeDir,
	}
	markerPath := helperMarkerPath(exePath, op.ID)
	if err := atomicWriteMarker(markerPath, marker); err != nil {
		t.Fatalf("atomicWriteMarker: %v", err)
	}
	// Ensure helper not yet launched – marker exists, staged exists
	if _, err := os.Stat(markerPath); err != nil {
		t.Fatalf("marker should exist before resume")
	}
	// Mock helper launch
	launched := 0
	origLaunch := launchHelperFn
	launchHelperFn = func(ep, mp string) error {
		launched++
		if mp != markerPath {
			t.Errorf("launch mp %q != %q", mp, markerPath)
		}
		return nil
	}
	t.Cleanup(func() { launchHelperFn = origLaunch })
	n, err := ResumePendingHandoffs(exePath)
	if err != nil {
		t.Fatalf("ResumePendingHandoffs: %v", err)
	}
	if n != 1 || launched != 1 {
		t.Fatalf("should resume 1 pending, got %d launched %d", n, launched)
	}
	// marker should still exist after resume (helper will clean up)
	if _, err := os.Stat(markerPath); err != nil {
		t.Errorf("marker should still exist after resume launch, helper cleans later")
	}
}

func TestResumePendingHandoffs_NeverActOnCorruptNonActive(t *testing.T) {
	exeDir := t.TempDir()
	exePath := filepath.Join(exeDir, "oss-server")
	_ = os.WriteFile(exePath, []byte("old"), 0o755)
	dir := helperMarkerDir(exePath)
	_ = os.MkdirAll(dir, 0o755)
	// corrupt marker
	corruptPath := filepath.Join(dir, "corrupt.handoff.json")
	_ = os.WriteFile(corruptPath, []byte("{ invalid"), 0o644)
	// non-active marker: create manager, issue, start, then transition to failed
	mgrRoot := t.TempDir()
	mgr, _ := NewManager(mgrRoot)
	assetName, _ := AssetName("1.2.3", "linux", "amd64")
	cand, _ := NewCandidate("v1.2.3", "linux", "amd64", "https://example.com/"+assetName, "https://example.com/releases/tag/v1.2.3", 1234, 1001, 2001, "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa")
	cc, _ := mgr.IssueChecked(*cand, time.Minute)
	op, _ := mgr.StartOperation(cc.ID, "1.2.3")
	mgr.Transition(op.ID, StateFailed, "done")
	nonActiveMarker := HandoffMarker{
		OpID:          op.ID,
		ManagerRoot:   mgrRoot,
		ExecPath:      exePath,
		StagedPath:    filepath.Join(dir, "staged-"+op.ID),
		BackupPath:    filepath.Join(dir, "backup-"+op.ID),
		HelperPath:    filepath.Join(dir, "helper-"+op.ID),
		TargetVersion: "1.2.3",
		Digest:        "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		ParentPID:     123,
		ReadyURL:      "http://127.0.0.1:8080/readyz",
	}
	nonActivePath := helperMarkerPath(exePath, op.ID)
	data, _ := json.Marshal(nonActiveMarker)
	_ = os.WriteFile(nonActivePath, data, 0o644)

	origLaunch := launchHelperFn
	launched := 0
	launchHelperFn = func(string, string) error { launched++; return nil }
	t.Cleanup(func() { launchHelperFn = origLaunch })
	n, err := ResumePendingHandoffs(exePath)
	if err != nil {
		t.Fatalf("ResumePendingHandoffs: %v", err)
	}
	if n != 0 || launched != 0 {
		t.Fatalf("should not resume corrupt/non-active, got %d launched %d", n, launched)
	}
	// ensure corrupt and non-active files still exist (no deletion, no rollback)
	if _, err := os.Stat(corruptPath); err != nil {
		t.Error("corrupt marker should not be deleted")
	}
	if _, err := os.Stat(nonActivePath); err != nil {
		t.Error("non-active marker should not be deleted")
	}
	// ensure exe not modified
}


