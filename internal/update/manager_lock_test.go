package update

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func writeLockFile(t *testing.T, root string, pid int) {
	t.Helper()
	lockPath := filepath.Join(root, ".update_state.lock")
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	meta := lockMeta{PID: pid, Time: time.Now().UnixNano()}
	data, _ := json.Marshal(meta)
	if err := os.WriteFile(lockPath, data, 0o600); err != nil {
		t.Fatalf("write lock: %v", err)
	}
}

func TestNewManager_LockAcquisitionFailure(t *testing.T) {
	root := t.TempDir()
	// hold file lock with live PID (current process)
	release, err := acquireFileLock(root)
	if err != nil {
		t.Fatalf("initial acquire: %v", err)
	}
	// Note: keep lock held; do not defer release until after test
	// Attempt NewManager should fail to acquire within deadline
	start := time.Now()
	_, err = NewManager(root)
	elapsed := time.Since(start)
	if err == nil {
		t.Fatalf("expected NewManager to fail when lock held by live owner, got nil")
	}
	if elapsed < 400*time.Millisecond {
		t.Logf("warning: NewManager failed fast (elapsed %v), may not have waited for lock timeout", elapsed)
	}
	// Ensure second IssueChecked also fails (via Manager that already exists? Create m via bypassing lock for setup)
	// Use existing manager without lock: create manager manually bypassing NewManager's lock by direct struct
	// Instead test acquireFileLock directly fails
	_, err = acquireFileLock(root)
	if err == nil {
		t.Error("second acquireFileLock should fail while live lock held")
	}
	release()
	// after release, NewManager should succeed
	m, err := NewManager(root)
	if err != nil {
		t.Fatalf("NewManager after release: %v", err)
	}
	if m == nil {
		t.Fatal("expected manager after lock release")
	}
}

func TestNewManager_StaleLockRecovery(t *testing.T) {
	root := t.TempDir()
	// write stale lock with dead PID (very unlikely to exist)
	const deadPID = 999999
	writeLockFile(t, root, deadPID)
	// verify isProcessAlive correctly reports dead
	if isProcessAlive(deadPID) {
		t.Skip("dead PID unexpectedly considered alive on this platform, skipping stale recovery test")
	}
	m, err := NewManager(root)
	if err != nil {
		t.Fatalf("NewManager should recover stale lock, got err: %v", err)
	}
	if m == nil {
		t.Fatal("expected manager")
	}
	// lock file should have been replaced with live PID, not stale dead PID
	data, err := os.ReadFile(filepath.Join(root, ".update_state.lock"))
	if err == nil {
		// If file still exists, it should be held by NewManager's critical section then released – after NewManager returns, lock should be gone
		// acquireFileLock in NewManager releases after return, so file should not exist after success
		// But if still exists, ensure it's not dead PID
		var cur lockMeta
		if json.Unmarshal(data, &cur) == nil && cur.PID == deadPID {
			t.Errorf("stale lock with dead PID still present after recovery")
		}
	}
	// after recovery, normal operation should work
	c := testCandidate("1.2.3")
	if _, err := m.IssueChecked(c, time.Minute); err != nil {
		t.Fatalf("IssueChecked after stale recovery: %v", err)
	}
}

func TestNewManager_NonStealingActiveLock(t *testing.T) {
	root := t.TempDir()
	// Create a lock held by live PID (current)
	writeLockFile(t, root, os.Getpid())
	// Attempt to acquire should not steal; should fail after timeout
	start := time.Now()
	_, err := acquireFileLock(root)
	elapsed := time.Since(start)
	if err == nil {
		t.Fatal("acquire should not steal live lock")
	}
	if elapsed < 400*time.Millisecond {
		t.Logf("acquire failed fast, elapsed %v", elapsed)
	}
	// Ensure lock file still exists and still belongs to live owner (not deleted)
	data, err := os.ReadFile(filepath.Join(root, ".update_state.lock"))
	if err != nil {
		t.Fatalf("live lock file should still exist after failed steal attempt: %v", err)
	}
	var cur lockMeta
	if err := json.Unmarshal(data, &cur); err != nil {
		t.Fatalf("unmarshal lock: %v", err)
	}
	if cur.PID != os.Getpid() {
		t.Errorf("live lock PID changed after failed steal: got %d want %d", cur.PID, os.Getpid())
	}
	// Cleanup for other tests
	_ = os.Remove(filepath.Join(root, ".update_state.lock"))
	// Now NewManager should succeed after lock removed
	m, err := NewManager(root)
	if err != nil {
		t.Fatalf("NewManager after active lock removed: %v", err)
	}
	_ = m
}
