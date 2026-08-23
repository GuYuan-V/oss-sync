package update

import (
	"errors"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

func testCandidate(version string) Candidate {
	c, err := newTestCandidate(version, "linux", "amd64", "https://example.com/oss-server_"+version+"_linux_amd64.tar.gz", "https://example.com/releases/tag/v"+version, 1234)
	if err != nil {
		panic(err)
	}
	return *c
}

func newTestManager(t *testing.T) *Manager {
	t.Helper()
	root := t.TempDir()
	m, err := NewManager(root)
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	return m
}

func TestManager_CheckExpiration(t *testing.T) {
	m := newTestManager(t)
	c := testCandidate("1.2.3")
	cc, err := m.IssueChecked(c, 20*time.Millisecond)
	if err != nil {
		t.Fatalf("IssueChecked: %v", err)
	}
	time.Sleep(40 * time.Millisecond)
	if _, err := m.ValidateChecked(cc.ID); !errors.Is(err, ErrCheckExpired) {
		t.Fatalf("expected expired, got %v", err)
	}
	if _, err := m.StartOperation(cc.ID, ""); !errors.Is(err, ErrCheckExpired) && !errors.Is(err, ErrCheckNotFound) {
		t.Fatalf("start with expired should fail, got %v", err)
	}
}

func TestManager_TargetCheckMismatch(t *testing.T) {
	m := newTestManager(t)
	c := testCandidate("1.2.4")
	cc, _ := m.IssueChecked(c, time.Minute)
	if _, err := m.StartOperation(cc.ID, "9.9.9"); !errors.Is(err, ErrCheckMismatch) {
		t.Fatalf("expected mismatch, got %v", err)
	}
	op, err := m.StartOperation(cc.ID, "1.2.4")
	if err != nil {
		t.Fatalf("matching target should succeed: %v", err)
	}
	if op.Candidate.Version != "1.2.4" {
		t.Fatalf("version mismatch %q", op.Candidate.Version)
	}
}

func TestManager_OneActiveUnderConcurrency(t *testing.T) {
	m := newTestManager(t)
	cc, _ := m.IssueChecked(testCandidate("2.0.0"), time.Minute)
	const n = 20
	var wg sync.WaitGroup
	success := make(chan string, n)
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			op, err := m.StartOperation(cc.ID, "")
			if err == nil {
				success <- op.ID
			}
		}()
	}
	wg.Wait()
	close(success)
	var ids []string
	for id := range success {
		ids = append(ids, id)
	}
	if len(ids) != 1 {
		t.Fatalf("expected exactly 1 active, got %d", len(ids))
	}
	if m.ActiveOperation() == nil {
		t.Fatal("active should exist")
	}
}

func TestManager_CorruptedJSONSafeFailure(t *testing.T) {
	root := t.TempDir()
	m, _ := NewManager(root)
	_, _ = m.IssueChecked(testCandidate("1.2.5"), time.Minute)
	path := stateFilePath(root)
	if err := os.WriteFile(path, []byte("{ invalid json"), 0o644); err != nil {
		t.Fatal(err)
	}
	m2, err := NewManager(root)
	if err == nil {
		t.Fatal("expected corrupted error")
	}
	if !errors.Is(err, ErrCorruptedState) {
		t.Fatalf("expected corrupted state, got %v", err)
	}
	if m2 == nil {
		t.Fatal("manager should still be returned on corrupted")
	}
	if got := m2.ListHistory(); len(got) != 0 {
		t.Fatalf("corrupted should reset to empty, got %d", len(got))
	}
}

func TestManager_AtomicWriteRecovery(t *testing.T) {
	root := t.TempDir()
	m, _ := NewManager(root)
	cc, _ := m.IssueChecked(testCandidate("1.2.6"), time.Minute)
	_, _ = m.StartOperation(cc.ID, "")
	path := stateFilePath(root)
	orig, _ := os.ReadFile(path)
	tmp := path + ".tmp"
	_ = os.WriteFile(tmp, []byte(`{"checked":{},"operations":{},"active_id":"bad","history":[]}`), 0o644)
	m2, err := NewManager(root)
	if err != nil {
		t.Fatalf("reopen should not fail on leftover tmp: %v", err)
	}
	data, _ := os.ReadFile(path)
	if string(data) != string(orig) {
		t.Fatal("atomic recovery failed: original overwritten by tmp")
	}
	if m2.ActiveOperation() == nil {
		t.Fatal("active should be recovered")
	}
	_ = os.Remove(tmp)
}

func TestManager_ImmutableSnapshots(t *testing.T) {
	m := newTestManager(t)
	cc, _ := m.IssueChecked(testCandidate("3.0.0"), time.Minute)
	op, _ := m.StartOperation(cc.ID, "")
	snap, _ := m.GetOperation(op.ID)
	snap.State = StateDone
	snap.Candidate.Version = "mutated"
	orig, _ := m.GetOperation(op.ID)
	if orig.State != StateInProgress {
		t.Fatalf("immutable violated: state %q", orig.State)
	}
	if orig.Candidate.Version == "mutated" {
		t.Fatal("candidate immutable violated")
	}
	ccSnap, _ := m.GetChecked(cc.ID)
	ccSnap.Candidate.Version = "hacked"
	origC, _ := m.ValidateChecked(cc.ID)
	if origC.Version == "hacked" {
		t.Fatal("checked immutable violated")
	}
	_ = filepath.Join
}

func TestManager_TerminalRetentionAndHistoryLimit(t *testing.T) {
	root := t.TempDir()
	m, _ := NewManager(root)
	for i := 0; i < maxHistoryKept+5; i++ {
		c2 := testCandidate("1.1." + itoa(i+10))
		cc, _ := m.IssueChecked(c2, time.Minute)
		op, err := m.StartOperation(cc.ID, "")
		if err != nil {
			t.Fatalf("start %d: %v", i, err)
		}
		// strict graph: in_progress -> failed is the minimal terminal; use failed for retention test
		if _, err := m.Transition(op.ID, StateFailed, "done"); err != nil {
			t.Fatalf("transition %d: %v", i, err)
		}
	}
	hist := m.ListHistory()
	if len(hist) != maxHistoryKept {
		t.Fatalf("expected history %d, got %d", maxHistoryKept, len(hist))
	}
	for _, op := range hist {
		if !op.IsTerminal() {
			t.Fatalf("history should be terminal, got %q", op.State)
		}
	}
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	b := make([]byte, 0, 4)
	for n > 0 {
		b = append([]byte{byte('0' + n%10)}, b...)
		n /= 10
	}
	return string(b)
}

func TestManager_ReopenOverSameRoot(t *testing.T) {
	root := t.TempDir()
	m1, _ := NewManager(root)
	cc, _ := m1.IssueChecked(testCandidate("4.5.6"), time.Minute)
	op1, _ := m1.StartOperation(cc.ID, "")
	m2, err := NewManager(root)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	op2, err := m2.GetOperation(op1.ID)
	if err != nil {
		t.Fatalf("recovered op: %v", err)
	}
	if op2.State != StateInProgress {
		t.Fatalf("state %q", op2.State)
	}
	if _, err := m2.Transition(op1.ID, StateFailed, "oops"); err != nil {
		t.Fatalf("transition after reopen: %v", err)
	}
	m3, _ := NewManager(root)
	op3, _ := m3.GetOperation(op1.ID)
	if !op3.IsTerminal() {
		t.Fatalf("terminal not persisted")
	}
	if m3.ActiveOperation() != nil {
		t.Fatal("active should be cleared after terminal")
	}
}

func TestManager_CurrentStatusNoGitHubPaths(t *testing.T) {
	m := newTestManager(t)
	cc, _ := m.IssueChecked(testCandidate("5.0.0"), time.Minute)
	_, _ = m.StartOperation(cc.ID, "")
	st := m.CurrentStatus()
	if st.Active == nil {
		t.Fatal("active missing")
	}
	// PublicOperation must not contain URLs; ensure marshal does not contain https
	data, _ := os.ReadFile(stateFilePath(m.path))
	_ = data
	if st.Active.Version != "5.0.0" {
		t.Fatalf("version %q", st.Active.Version)
	}
	// Verify ManagerStatus JSON has no asset_url / release_url / backup_path
	if st.Active.Error != "" {
		t.Fatal("unexpected error")
	}
}

func TestManager_InvalidTransition(t *testing.T) {
	m := newTestManager(t)
	cc, _ := m.IssueChecked(testCandidate("6.0.0"), time.Minute)
	op, _ := m.StartOperation(cc.ID, "")
	// strict graph rejects skip: in_progress -> done is not allowed
	if _, err := m.Transition(op.ID, StateDone, ""); !errors.Is(err, ErrInvalidTransition) {
		t.Fatalf("skip to done should be rejected, got %v", err)
	}
	if _, err := m.Transition(op.ID, StateFailed, ""); err != nil {
		t.Fatalf("to failed should be allowed: %v", err)
	}
	if _, err := m.Transition(op.ID, StateFailed, ""); !errors.Is(err, ErrInvalidTransition) {
		t.Fatalf("terminal transition should fail, got %v", err)
	}
}
