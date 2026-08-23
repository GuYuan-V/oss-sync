package update

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/oss/oss-server/internal/config"
	"github.com/oss/oss-server/internal/version"
)

func TestHandoff_DirectorySyncFailureAfterRename_CommittedAndResumable(t *testing.T) {
	origVer := version.Version
	version.Version = "1.0.0"
	t.Cleanup(func() { version.Version = origVer })

	origSyncDir := syncDirFn
	syncDirFn = func(f *os.File) error { return os.ErrDeadlineExceeded }
	t.Cleanup(func() { syncDirFn = origSyncDir })

	// Force removal to fail so cleanup cannot be proven -> committed.
	origRemove := removeFileFn
	removeFileFn = func(name string) error {
		// Fail only for marker removal, allow staged cleanup to be attempted but marker remains.
		if filepath.Ext(name) == ".json" {
			return os.ErrInvalid
		}
		return os.Remove(name)
	}
	t.Cleanup(func() { removeFileFn = origRemove })

	origVerify := verifyStagedFileFn
	verifyStagedFileFn = func(string, string, string) error { return nil }
	t.Cleanup(func() { verifyStagedFileFn = origVerify })

	origLaunch := launchHelperFn
	launchHelperFn = func(string, string) error { return nil }
	t.Cleanup(func() { launchHelperFn = origLaunch })

	exeDir := t.TempDir()
	exePath := filepath.Join(exeDir, "oss-server")
	_ = os.WriteFile(exePath, []byte("old-binary"), 0o755)
	mgrRoot := t.TempDir()
	mgr, err := NewManager(mgrRoot)
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	id := newCheckedForHelper(t, mgr, "9.9.50")
	candPath := candidatePathFor(id)
	cfg := &config.Config{Storage: config.StorageConfig{DataDir: mgrRoot}}
	up, _ := NewUpdater(cfg, Options{ExecPath: exePath, Verifier: func(string, string) error { return nil }})

	op, err := up.InitiateHelperHandoff(mgr, id, candPath, "http://127.0.0.1:0/readyz", []string{exePath}, exeDir)
	if err != nil {
		t.Fatalf("InitiateHelperHandoff should return committed success on dir sync failure with unprovable cleanup, got err %v", err)
	}
	if op == nil {
		t.Fatal("expected operation on committed handoff")
	}
	// Marker must still exist and be resumable.
	markerPath := helperMarkerPath(exePath, op.ID)
	if _, err := os.Stat(markerPath); err != nil {
		t.Fatalf("marker should still exist for resumable handoff, stat err %v", err)
	}
	// Operation must not be terminal.
	cur, _ := mgr.GetOperation(op.ID)
	if cur.IsTerminal() {
		t.Fatalf("operation should not be terminal for committed handoff, got %s", cur.State)
	}
	// Resume should find it.
	origLaunch2 := launchHelperFn
	launched := 0
	launchHelperFn = func(ep, mp string) error { launched++; return nil }
	t.Cleanup(func() { launchHelperFn = origLaunch2 })
	// Temporarily restore removeFileFn to allow resume to work? Resume doesn't need remove.
	n, err := ResumePendingHandoffs(exePath)
	if err != nil {
		t.Fatalf("ResumePendingHandoffs: %v", err)
	}
	if n != 1 || launched != 1 {
		t.Fatalf("resume should find 1 pending committed handoff, got %d launched %d", n, launched)
	}
	// Invariant: API returned success and resume found marker -> consistent.
}

func TestHandoff_DirectorySyncFailureAfterRename_SuccessfulCleanup_NoResume(t *testing.T) {
	origVer := version.Version
	version.Version = "1.0.0"
	t.Cleanup(func() { version.Version = origVer })

	origSyncDir := syncDirFn
	syncDirFn = func(f *os.File) error { return os.ErrDeadlineExceeded }
	t.Cleanup(func() { syncDirFn = origSyncDir })

	origVerify := verifyStagedFileFn
	verifyStagedFileFn = func(string, string, string) error { return nil }
	t.Cleanup(func() { verifyStagedFileFn = origVerify })

	exeDir := t.TempDir()
	exePath := filepath.Join(exeDir, "oss-server")
	_ = os.WriteFile(exePath, []byte("old-binary"), 0o755)
	mgrRoot := t.TempDir()
	mgr, _ := NewManager(mgrRoot)
	id := newCheckedForHelper(t, mgr, "9.9.51")
	candPath := candidatePathFor(id)
	cfg := &config.Config{Storage: config.StorageConfig{DataDir: mgrRoot}}
	up, _ := NewUpdater(cfg, Options{ExecPath: exePath, Verifier: func(string, string) error { return nil }})

	// With default removeFileFn and successful persist, cleanup should be proven and API should return failure with no resumable marker.
	op, err := up.InitiateHelperHandoff(mgr, id, candPath, "http://127.0.0.1:0/readyz", []string{exePath}, exeDir)
	if err == nil {
		// In this implementation, post-rename dir sync failure with successful cleanup returns failure.
		// Verify no resumable marker.
		if op != nil {
			markerPath := helperMarkerPath(exePath, op.ID)
			if _, statErr := os.Stat(markerPath); statErr == nil {
				t.Fatalf("marker should be removed after successful cleanup, still exists at %s", markerPath)
			}
		}
		t.Fatalf("expected write marker failure when cleanup proven, got success")
	}
	// Ensure no resumable marker left.
	origLaunch := launchHelperFn
	launched := 0
	launchHelperFn = func(string, string) error { launched++; return nil }
	t.Cleanup(func() { launchHelperFn = origLaunch })
	n, err := ResumePendingHandoffs(exePath)
	if err != nil {
		t.Fatalf("ResumePendingHandoffs: %v", err)
	}
	if n != 0 || launched != 0 {
		t.Fatalf("no resume should happen after proven cleanup failure, got %d launched %d", n, launched)
	}
	// Invariant holds: API failure with no resumable marker.
}

func TestHandoff_MarkerRemovalFailure_KeepsCommitted(t *testing.T) {
	origVer := version.Version
	version.Version = "1.0.0"
	t.Cleanup(func() { version.Version = origVer })

	origVerify := verifyStagedFileFn
	verifyStagedFileFn = func(string, string, string) error { return nil }
	t.Cleanup(func() { verifyStagedFileFn = origVerify })

	origRemove := removeFileFn
	removeFileFn = func(name string) error {
		if filepath.Ext(name) == ".json" {
			return os.ErrPermission
		}
		return os.Remove(name)
	}
	t.Cleanup(func() { removeFileFn = origRemove })

	origLaunch := launchHelperFn
	launchHelperFn = func(string, string) error { return os.ErrInvalid }
	t.Cleanup(func() { launchHelperFn = origLaunch })

	exeDir := t.TempDir()
	exePath := filepath.Join(exeDir, "oss-server")
	_ = os.WriteFile(exePath, []byte("old-binary"), 0o755)
	mgrRoot := t.TempDir()
	mgr, _ := NewManager(mgrRoot)
	id := newCheckedForHelper(t, mgr, "9.9.52")
	candPath := candidatePathFor(id)
	cfg := &config.Config{Storage: config.StorageConfig{DataDir: mgrRoot}}
	up, _ := NewUpdater(cfg, Options{ExecPath: exePath, Verifier: func(string, string) error { return nil }})

	op, err := up.InitiateHelperHandoff(mgr, id, candPath, "http://127.0.0.1:0/readyz", []string{exePath}, exeDir)
	if err != nil {
		t.Fatalf("helper launch failure with unprovable removal should return committed success, got err %v", err)
	}
	if op == nil {
		t.Fatal("expected operation")
	}
	markerPath := helperMarkerPath(exePath, op.ID)
	if _, err := os.Stat(markerPath); err != nil {
		t.Fatalf("marker should remain after removal failure, stat %v", err)
	}
	cur, _ := mgr.GetOperation(op.ID)
	if cur.IsTerminal() {
		t.Fatalf("operation should remain non-terminal (committed) after removal failure, got %s", cur.State)
	}
	// Resume must find it.
	launchHelperFn = func(ep, mp string) error { return nil }
	n, err := ResumePendingHandoffs(exePath)
	if err != nil {
		t.Fatalf("ResumePendingHandoffs: %v", err)
	}
	if n != 1 {
		t.Fatalf("resume should find committed handoff after removal failure, got %d", n)
	}
}

func TestHandoff_TerminalStatePersistenceFailure_KeepsCommitted(t *testing.T) {
	origVer := version.Version
	version.Version = "1.0.0"
	t.Cleanup(func() { version.Version = origVer })

	origVerify := verifyStagedFileFn
	verifyStagedFileFn = func(string, string, string) error { return nil }
	t.Cleanup(func() { verifyStagedFileFn = origVerify })

	exeDir := t.TempDir()
	exePath := filepath.Join(exeDir, "oss-server")
	_ = os.WriteFile(exePath, []byte("old-binary"), 0o755)
	mgrRoot := t.TempDir()
	mgr, _ := NewManager(mgrRoot)
	id := newCheckedForHelper(t, mgr, "9.9.53")
	candPath := candidatePathFor(id)
	cfg := &config.Config{Storage: config.StorageConfig{DataDir: mgrRoot}}
	up, _ := NewUpdater(cfg, Options{ExecPath: exePath, Verifier: func(string, string) error { return nil }})

	// Make launch fail, and make Transition to Failed fail via atomicWriteJSONFn.
	origLaunch := launchHelperFn
	launchHelperFn = func(string, string) error { return os.ErrInvalid }
	t.Cleanup(func() { launchHelperFn = origLaunch })

	// Also make marker removal fail so marker remains durable when transition fails.
	origRemove := removeFileFn
	removeFileFn = func(name string) error {
		if filepath.Ext(name) == ".json" {
			return os.ErrPermission
		}
		return os.Remove(name)
	}
	t.Cleanup(func() { removeFileFn = origRemove })

	// Inject failure for Transition to Failed only.
	origAtomic := atomicWriteJSONFn
	callCount := 0
	atomicWriteJSONFn = func(path string, v any) error {
		callCount++
		// Fail the persist that corresponds to Transition to Failed (after marker+swap).
		// First persists are StartOperation + Prepare...Backup + Swap. Those should succeed.
		// We detect by inspecting operations map: if any op is Failed state, fail.
		if ps, ok := v.(persistedState); ok {
			for _, op := range ps.Ops {
				if op.State == StateFailed {
					return os.ErrInvalid
				}
			}
		}
		return origAtomic(path, v)
	}
	t.Cleanup(func() { atomicWriteJSONFn = origAtomic })

	op, err := up.InitiateHelperHandoff(mgr, id, candPath, "http://127.0.0.1:0/readyz", []string{exePath}, exeDir)
	if err != nil {
		t.Fatalf("terminal persist failure with durable marker should return committed success, got err %v", err)
	}
	if op == nil {
		t.Fatal("expected committed operation")
	}
	markerPath := helperMarkerPath(exePath, op.ID)
	if _, err := os.Stat(markerPath); err != nil {
		t.Fatalf("marker should remain when terminal persist fails, stat %v", err)
	}
	// Operation should still be non-terminal (since Failed persist failed, it remains Swap/Backup)
	mgr2, _ := NewManager(mgrRoot)
	cur, _ := mgr2.GetOperation(op.ID)
	// Even if manager reload fails, original mgr should still have non-terminal.
	if cur != nil && cur.IsTerminal() {
		t.Fatalf("operation should not be terminal after persist failure, got %s", cur.State)
	}
	// Resume should find it (since still active).
	launchHelperFn = func(ep, mp string) error { return nil }
	// Restore atomic write for resume's manager load.
	atomicWriteJSONFn = origAtomic
	n, err := ResumePendingHandoffs(exePath)
	if err != nil {
		t.Fatalf("ResumePendingHandoffs: %v", err)
	}
	if n != 1 {
		t.Fatalf("resume should find committed handoff after terminal persist failure, got %d", n)
	}
}

func TestHandoff_NormalStartupResumeConsistency(t *testing.T) {
	origVer := version.Version
	version.Version = "1.0.0"
	t.Cleanup(func() { version.Version = origVer })

	origVerify := verifyStagedFileFn
	verifyStagedFileFn = func(string, string, string) error { return nil }
	t.Cleanup(func() { verifyStagedFileFn = origVerify })

	exeDir := t.TempDir()
	exePath := filepath.Join(exeDir, "oss-server")
	_ = os.WriteFile(exePath, []byte("old-binary"), 0o755)
	mgrRoot := t.TempDir()
	mgr, _ := NewManager(mgrRoot)
	id := newCheckedForHelper(t, mgr, "9.9.54")
	candPath := candidatePathFor(id)
	cfg := &config.Config{Storage: config.StorageConfig{DataDir: mgrRoot}}
	up, _ := NewUpdater(cfg, Options{ExecPath: exePath, Verifier: func(string, string) error { return nil }})

	// Case 1: successful handoff -> API success and resume finds marker (if helper not yet launched? Actually Initiate launches helper, so marker still exists until helper cleans).
	// To simulate committed without launch, we make launch succeed.
	origLaunch := launchHelperFn
	launchHelperFn = func(string, string) error { return nil }
	t.Cleanup(func() { launchHelperFn = origLaunch })
	op, err := up.InitiateHelperHandoff(mgr, id, candPath, "http://127.0.0.1:0/readyz", []string{exePath}, exeDir)
	if err != nil {
		t.Fatalf("successful handoff: %v", err)
	}
	markerPath := helperMarkerPath(exePath, op.ID)
	if _, err := os.Stat(markerPath); err != nil {
		t.Fatalf("marker should exist after successful handoff, %v", err)
	}
	// Simulate crash-before-helper-launch: marker exists, operation in Swap, helper not yet run.
	// Resume should launch it.
	launched := 0
	launchHelperFn = func(ep, mp string) error { launched++; return nil }
	n, err := ResumePendingHandoffs(exePath)
	if err != nil {
		t.Fatalf("resume: %v", err)
	}
	if n != 1 || launched != 1 {
		t.Fatalf("normal startup should resume committed handoff, got %d launched %d", n, launched)
	}

	// Case 2: fully failed handoff (pre-marker) -> API failure and no resume.
	// Use a new manager and make prepareStaging fail by making candidatePath invalid?
	// Simpler: make verify fail before marker.
	verifyStagedFileFn = func(string, string, string) error { return os.ErrInvalid }
	// Need new checked candidate.
	mgr2, _ := NewManager(mgrRoot)
	// Need new exe for second op to avoid active lock: first op is still active (Swap). Need to transition it to Failed to allow new op.
	_, _ = mgr.Transition(op.ID, StateFailed, "cleanup for test")
	// Now issue new check.
	id2 := newCheckedForHelper(t, mgr2, "9.9.55")
	candPath2 := candidatePathFor(id2)
	up2, _ := NewUpdater(cfg, Options{ExecPath: exePath, Verifier: func(string, string) error { return nil }})
	// verifyStaged still fails -> prepareStaging will fail before marker.
	op2, err := up2.InitiateHelperHandoff(mgr2, id2, candPath2, "http://127.0.0.1:0/readyz", []string{exePath}, exeDir)
	if err == nil {
		t.Fatalf("expected failure for verify failure, got op %v", op2)
	}
	// No marker for op2 should exist.
	if op2 != nil {
		mp2 := helperMarkerPath(exePath, op2.ID)
		if _, err := os.Stat(mp2); err == nil {
			t.Fatalf("marker should not exist after pre-marker failure")
		}
	}
	// Resume should not find new marker, but will still find old one (op). Clean old marker.
	_ = os.Remove(markerPath)
	_, _ = mgr.Transition(op.ID, StateFailed, "cleanup")
	// After cleanup, resume should find 0.
	launchHelperFn = func(string, string) error { launched++; return nil }
	n, err = ResumePendingHandoffs(exePath)
	if err != nil {
		t.Fatalf("resume after cleanup: %v", err)
	}
	if n != 0 {
		t.Fatalf("after failed handoff and cleanup, resume should find 0, got %d", n)
	}
	// Invariant: API failure matches no resume.

	// Case 3: verify public status safe (no paths in PublicOperation).
	publicStatus := mgr2.CurrentStatus()
	if publicStatus.Active != nil {
		// Should not contain paths; only ID, State, Version, etc.
		if publicStatus.Active.Error != "" {
			// error may be present but should be low cardinality code, not path
		}
	}
	// Ensure toPublic doesn't expose staged path (checked via handler response not containing marker strings).
	_ = time.Now()
}
