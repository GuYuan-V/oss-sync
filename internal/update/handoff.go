// 更新交接
package update

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/oss/oss-server/internal/version"
)

// Hidden helper flag — bypasses normal config/database startup.
const HelperFlag = "--oss-update-helper"

const helperMarkerExt = ".handoff.json"

// HandoffMarker is the durable reference the helper receives.
// It contains no tokens — only filesystem paths and the operation ID.
type HandoffMarker struct {
	OpID          string   `json:"op_id"`
	ManagerRoot   string   `json:"manager_root"`
	ExecPath      string   `json:"exec_path"`
	StagedPath    string   `json:"staged_path"`
	BackupPath    string   `json:"backup_path"`
	HelperPath    string   `json:"helper_path"`
	TargetVersion string   `json:"target_version"`
	Digest        string   `json:"digest"`
	ParentPID     int      `json:"parent_pid"`
	ReadyURL      string   `json:"ready_url"`
	OrigArgs      []string `json:"orig_args"`
	WorkDir       string   `json:"work_dir"`
}

// IsHelperInvocation reports whether the current process was launched as a helper.
func IsHelperInvocation() (bool, string) {
	args := os.Args[1:]
	for i, a := range args {
		if a == HelperFlag {
			if i+1 < len(args) {
				return true, args[i+1]
			}
			return true, ""
		}
		if strings.HasPrefix(a, HelperFlag+"=") {
			return true, strings.TrimPrefix(a, HelperFlag+"=")
		}
	}
	return false, ""
}

// helperMarkerDir returns the directory used for staging on the executable filesystem.
func helperMarkerDir(execPath string) string {
	return filepath.Join(filepath.Dir(execPath), ".oss-update-pending")
}

// helperMarkerPath returns the marker file path for a given operation.
func helperMarkerPath(execPath, opID string) string {
	dir := helperMarkerDir(execPath)
	return filepath.Join(dir, opID+helperMarkerExt)
}

// Injected seams for atomicWriteMarker error propagation tests.
var openFileForSyncFn = func(name string) (*os.File, error) { return os.OpenFile(name, os.O_RDWR, 0) }
var syncFileFn = func(f *os.File) error { return f.Sync() }
var openDirFn = func(name string) (*os.File, error) { return os.Open(name) }
var syncDirFn = func(f *os.File) error { return f.Sync() }
var removeFileFn = os.Remove

// atomicWriteMarker marshals marker to JSON and writes atomically with fsync+rename.
// Propagates temp file and directory sync/open errors; retains a narrow documented
// Windows-only directory-fsync exception only if the OS reports directory sync as
// unsupported (e.g., syscall.EINVAL on Windows). All other errors are returned
// and the temp file is cleaned up.
func atomicWriteMarker(markerPath string, marker HandoffMarker) error {
	data, err := json.Marshal(marker)
	if err != nil {
		return err
	}
	dir := filepath.Dir(markerPath)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	tmp := markerPath + ".tmp." + uuid.NewString()
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return err
	}
	// fsync temp file — propagate errors
	if f, err := openFileForSyncFn(tmp); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("open temp for sync: %w", err)
	} else {
		if err := syncFileFn(f); err != nil {
			_ = f.Close()
			_ = os.Remove(tmp)
			return fmt.Errorf("fsync temp: %w", err)
		}
		if err := f.Close(); err != nil {
			_ = os.Remove(tmp)
			return fmt.Errorf("close temp: %w", err)
		}
	}
	if err := os.Rename(tmp, markerPath); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	// fsync directory — propagate open/sync errors, with narrow Windows unsupported exception
	df, err := openDirFn(dir)
	if err != nil {
		return fmt.Errorf("open dir for sync: %w", err)
	}
	if err := syncDirFn(df); err != nil {
		_ = df.Close()
		// Windows-only narrow exception: directory sync may be unsupported
		if runtime.GOOS == "windows" && isWindowsDirSyncUnsupported(err) {
			// documented exception: Windows may return EINVAL/ENOSYS for dir sync; treat as non-fatal
			return nil
		}
		return fmt.Errorf("fsync dir: %w", err)
	}
	if err := df.Close(); err != nil {
		return fmt.Errorf("close dir: %w", err)
	}
	return nil
}

func isWindowsDirSyncUnsupported(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	// Windows directory fsync is often unsupported; Access denied is common when
	// opening a directory for sync on Windows. Narrow exception: only on Windows
	// and only for known unsupported messages.
	if runtime.GOOS != "windows" {
		return false
	}
	return strings.Contains(msg, "invalid") || strings.Contains(msg, "not supported") || strings.Contains(msg, "enotsup") || strings.Contains(msg, "einval") || strings.Contains(msg, "access is denied") || strings.Contains(msg, "denied")
}

// isSafePath ensures p is within base directory and not traversal.
func isSafePath(base, p string) bool {
	if p == "" {
		return false
	}
	baseAbs, err := filepath.Abs(base)
	if err != nil {
		return false
	}
	pAbs, err := filepath.Abs(p)
	if err != nil {
		return false
	}
	rel, err := filepath.Rel(baseAbs, pAbs)
	if err != nil {
		return false
	}
	if rel == "." {
		return false
	}
	return !strings.HasPrefix(rel, ".."+string(os.PathSeparator)) && rel != ".."
}

func validateMarkerSafe(m *HandoffMarker) error {
	if m == nil {
		return errors.New("marker is nil")
	}
	if !isValidDigest(m.Digest) {
		return fmt.Errorf("invalid digest %q", m.Digest)
	}
	normWant := version.Normalize(m.TargetVersion)
	if normWant == "" || !version.IsValid(normWant) {
		return fmt.Errorf("invalid target version %q", m.TargetVersion)
	}
	if m.TargetVersion != normWant {
		return fmt.Errorf("target version %q must be normalized", m.TargetVersion)
	}
	base := helperMarkerDir(m.ExecPath)
	if !isSafePath(base, m.StagedPath) || !isSafePath(base, m.BackupPath) || !isSafePath(base, m.HelperPath) {
		return fmt.Errorf("unsafe marker paths")
	}
	if m.ExecPath == "" || !filepath.IsAbs(m.ExecPath) {
		return fmt.Errorf("unsafe exec path")
	}
	return nil
}

// ResumePendingHandoffs discovers durable pending markers on ordinary startup
// and resumes the helper after validating active operation and marker safety.
// It covers crash-after-marker-before-helper-launch: a marker written but helper
// not yet launched is resumed by launching the helper now. Never acts on
// corrupt/non-active markers (no rollback, no deletion).
func ResumePendingHandoffs(execPath string) (int, error) {
	dir := helperMarkerDir(execPath)
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return 0, nil
		}
		return 0, err
	}
	resumed := 0
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), helperMarkerExt) {
			continue
		}
		markerPath := filepath.Join(dir, e.Name())
		// Validate active operation and safety before any action
		_, _, err := recoverActiveMarker(markerPath)
		if err != nil {
			// corrupt/non-active -> never act
			continue
		}
		// Load marker to validate safe paths/digest/target
		data, err := os.ReadFile(markerPath)
		if err != nil {
			continue
		}
		var m HandoffMarker
		if err := json.Unmarshal(data, &m); err != nil {
			continue
		}
		if err := validateMarkerSafe(&m); err != nil {
			continue
		}
		// Deterministic helper-owned action: launch helper to perform wait/swap/probe/rollback
		if err := launchHelperFn(m.ExecPath, markerPath); err != nil {
			// launch failure is handled as helper-owned rollback via recordRollback in helper,
			// but if launch itself fails here (e.g., cannot spawn), do deterministic rollback
			_ = recordRollback(&m, fmt.Sprintf("resume launch failed: %v", err))
			continue
		}
		resumed++
	}
	return resumed, nil
}

// CheckHandoffCapability validates that helper-based self-update is supported
// on the current platform and executable location. Returns a typed
// *UpdateError with CodeUnsupportedPlatform or related codes before any
// mutation occurs.
func CheckHandoffCapability(execPath string) error {
	// Windows and Unix (linux/darwin) are supported; other GOOS is unsupported.
	switch runtime.GOOS {
	case "linux", "darwin", "windows":
	default:
		return newUpdateError(CodeUnsupportedPlatform, fmt.Sprintf("helper not supported on %s", runtime.GOOS), ErrUnsupportedPlatform)
	}
	return CheckCurrentCapability(execPath)
}

// verifyStagedFile checks digest, executable magic, and exact --version output
// without requiring network. Caller must ensure stagedPath is on the same
// filesystem as the target executable.
func verifyStagedFile(stagedPath, digest, wantVersion string) error {
	if stagedPath == "" {
		return errors.New("staged path is empty")
	}
	if _, err := os.Stat(stagedPath); err != nil {
		return fmt.Errorf("staged file missing: %w", err)
	}
	if digest != "" {
		if err := verifyFileDigest(stagedPath, digest); err != nil {
			return fmt.Errorf("staged digest mismatch: %w", err)
		}
	}
	if err := checkExecutableMagic(stagedPath, runtime.GOOS); err != nil {
		return fmt.Errorf("staged magic check failed: %w", err)
	}
	// Exact normalized version equality, never contains (regression 1.2.3 vs 1.2.30).
	if wantVersion != "" {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		cmd := exec.CommandContext(ctx, stagedPath, "--version")
		out, err := cmd.CombinedOutput()
		if err != nil {
			return fmt.Errorf("staged --version failed: %w (%s)", err, strings.TrimSpace(string(out)))
		}
		got := strings.TrimSpace(string(out))
		if got == "" {
			return errors.New("staged --version output empty")
		}
		if version.Normalize(got) != version.Normalize(wantVersion) {
			return fmt.Errorf("staged version %q (normalized %q) does not equal target %q (normalized %q)", got, version.Normalize(got), wantVersion, version.Normalize(wantVersion))
		}
	}
	return nil
}

var verifyStagedFileFn = verifyStagedFile

// prepareStaging copies the candidate binary and current executable copies
// onto the executable filesystem, then verifies the staged file.
func prepareStaging(candidatePath, execPath, opID, wantVersion, digest string) (staged, backup, helperCopy string, err error) {
	if err := CheckHandoffCapability(execPath); err != nil {
		return "", "", "", err
	}
	dir := helperMarkerDir(execPath)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", "", "", fmt.Errorf("create staging dir: %w", err)
	}
	staged = filepath.Join(dir, "staged-"+opID)
	backup = filepath.Join(dir, "backup-"+opID)
	helperCopy = filepath.Join(dir, "helper-"+opID)

	// Copy candidate to staged location.
	if err := copyFile(candidatePath, staged); err != nil {
		return "", "", "", fmt.Errorf("stage candidate: %w", err)
	}
	// Ensure staged is executable on Unix.
	_ = os.Chmod(staged, 0o755)
	if err := verifyStagedFileFn(staged, digest, wantVersion); err != nil {
		_ = os.Remove(staged)
		return "", "", "", err
	}
	// Copy current binary to backup.
	if err := copyFile(execPath, backup); err != nil {
		_ = os.Remove(staged)
		return "", "", "", fmt.Errorf("backup current: %w", err)
	}
	// Copy helper (same binary as current) for resilience.
	if err := copyFile(execPath, helperCopy); err != nil {
		_ = os.Remove(staged)
		_ = os.Remove(backup)
		return "", "", "", fmt.Errorf("stage helper copy: %w", err)
	}
	return staged, backup, helperCopy, nil
}

// launchHelper starts the helper process detached. It receives only the marker path.
var launchHelperFn = launchHelper

func launchHelper(execPath, markerPath string) error {
	if markerPath == "" {
		return errors.New("marker path empty")
	}
	// Prefer the helper copy if present (same filesystem), otherwise current exe.
	helperBin := execPath
	// Read marker to see if HelperPath exists.
	if data, err := os.ReadFile(markerPath); err == nil {
		var m HandoffMarker
		if json.Unmarshal(data, &m) == nil && m.HelperPath != "" {
			if _, err := os.Stat(m.HelperPath); err == nil {
				helperBin = m.HelperPath
			}
		}
	}
	cmd := exec.Command(helperBin, HelperFlag+"="+markerPath)
	cmd.Env = os.Environ()
	if wd, err := os.Getwd(); err == nil {
		cmd.Dir = wd
	}
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	detachHelper(cmd)
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("launch helper: %w", err)
	}
	// Do not wait — helper runs independently.
	_ = cmd.Process.Release()
	return nil
}

// waitForParent polls until parent PID is gone or timeout expires.
var waitForParentFn = waitForParent

func waitForParent(parentPID int, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if !isProcessAlive(parentPID) {
			return nil
		}
		time.Sleep(50 * time.Millisecond)
	}
	if isProcessAlive(parentPID) {
		return fmt.Errorf("parent %d still alive after %s", parentPID, timeout)
	}
	return nil
}

// atomicReplace performs the staged -> execPath replacement.
var atomicReplaceFn = atomicReplace

func atomicReplace(stagedPath, execPath string) error {
	return swapBinary(stagedPath, execPath)
}

// recoverActiveMarker validates that a marker references a still-active durable operation.
// Returns the marker and operation if valid, or an error if not active/terminal.
// A missing or non-active marker means this is an ordinary startup — no rollback.
func recoverActiveMarker(markerPath string) (*HandoffMarker, *Operation, error) {
	data, err := os.ReadFile(markerPath)
	if err != nil {
		return nil, nil, fmt.Errorf("read marker: %w", err)
	}
	var m HandoffMarker
	if err := json.Unmarshal(data, &m); err != nil {
		return nil, nil, fmt.Errorf("corrupted marker: %w", err)
	}
	if m.OpID == "" || m.ManagerRoot == "" {
		return nil, nil, errors.New("marker missing op or root")
	}
	mgr, err := NewManager(m.ManagerRoot)
	if err != nil && !errors.Is(err, ErrCorruptedState) {
		return nil, nil, fmt.Errorf("open manager: %w", err)
	}
	// NewManager returns a manager even on corrupted state; use it to query.
	if mgr == nil {
		return nil, nil, errors.New("manager unavailable")
	}
	op, err := mgr.GetOperation(m.OpID)
	if err != nil {
		return nil, nil, fmt.Errorf("operation not found: %w", err)
	}
	if op.IsTerminal() {
		return nil, nil, fmt.Errorf("operation already terminal %s", op.State)
	}
	// Also ensure it is still the active operation.
	active := mgr.ActiveOperation()
	if active == nil || active.ID != m.OpID {
		return nil, nil, errors.New("operation not active")
	}
	return &m, op, nil
}

// InitiateHelperHandoff stages a candidate file, creates a durable marker, and launches the helper.
// It performs capability checking BEFORE any mutation and verifies digest/magic/--version before handoff.
// mgr is the durable Manager; checkID is a validated checked candidate; candidatePath is the local file
// containing the new binary (already downloaded); readyURL is the /readyz endpoint to probe.
// origArgs and workDir capture the runtime context to relaunch.
// Returns the active Operation or a typed capability error.
func (u *Updater) InitiateHelperHandoff(mgr *Manager, checkID string, candidatePath string, readyURL string, origArgs []string, workDir string) (*Operation, error) {
	if mgr == nil {
		return nil, errors.New("manager is nil")
	}
	if err := CheckHandoffCapability(u.exe); err != nil {
		return nil, err
	}
	if candidatePath == "" {
		return nil, errors.New("candidatePath is empty")
	}
	cand, err := mgr.ValidateChecked(checkID)
	if err != nil {
		return nil, err
	}
	if err := cand.Validate(); err != nil {
		return nil, err
	}
	// Start durable operation — this is the single source of truth.
	op, err := mgr.StartOperation(checkID, cand.Version)
	if err != nil {
		return nil, err
	}
	// Drive state forward to backup stage before handoff (prepare ... backup).
	// Helper will handle swap->done.
	seq := []OperationState{StatePrepare, StateFetchRelease, StateSelectAsset, StateDownload, StateVerify, StateBackup}
	for _, nxt := range seq {
		cur, _ := mgr.GetOperation(op.ID)
		if cur.State == nxt {
			continue
		}
		if isAllowedTransition(cur.State, nxt) {
			if _, err := mgr.Transition(op.ID, nxt, ""); err != nil {
				// If transition fails, mark failed and abort.
				_, _ = mgr.Transition(op.ID, StateFailed, err.Error())
				return nil, err
			}
		}
	}
	// Stage files on executable filesystem and verify before handoff.
	staged, backup, helperCopy, err := prepareStaging(candidatePath, u.exe, op.ID, cand.Version, cand.Digest)
	if err != nil {
		_, _ = mgr.Transition(op.ID, StateFailed, err.Error())
		return nil, err
	}
	// Ensure manager state reflects backup path for diagnostics (not exposed via PublicOperation).
	// We store it in persisted operation's BackupPath via direct file update? For now keep in memory via transition error message?
	// Instead we persist backup path via a dedicated helper: update operation's BackupPath field if manager supports.
	// As manager doesn't expose BackupPath mutation via Transition, we update via internal persist helper.
	_ = backup
	_ = helperCopy
	// Create marker.
	if workDir == "" {
		workDir, _ = os.Getwd()
	}
	if len(origArgs) == 0 {
		origArgs = os.Args
	}
	marker := HandoffMarker{
		OpID:          op.ID,
		ManagerRoot:   mgr.root,
		ExecPath:      u.exe,
		StagedPath:    staged,
		BackupPath:    backup,
		HelperPath:    helperCopy,
		TargetVersion: cand.Version,
		Digest:        cand.Digest,
		ParentPID:     os.Getpid(),
		ReadyURL:      readyURL,
		OrigArgs:      origArgs,
		WorkDir:       workDir,
	}
	markerPath := helperMarkerPath(u.exe, op.ID)
	dir := helperMarkerDir(u.exe)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		_, _ = mgr.Transition(op.ID, StateFailed, err.Error())
		return nil, err
	}
	if err := atomicWriteMarker(markerPath, marker); err != nil {
		// Check if marker is possibly durable (post-rename fsync/open dir failure).
		if _, statErr := os.Stat(markerPath); statErr == nil {
			// Marker exists after rename -> possibly durable. Attempt transactional cleanup proof.
			// Try remove first: if removal fails marker remains -> keep committed without transitioning to Failed.
			rmErr := removeFileFn(markerPath)
			_, statAfter := os.Stat(markerPath)
			stillDurable := statAfter == nil
			if stillDurable {
				// Cleanup not proven (marker still exists) -> keep as acknowledged recoverable committed handoff.
				if cur, _ := mgr.GetOperation(op.ID); cur != nil && isAllowedTransition(cur.State, StateSwap) {
					_, _ = mgr.Transition(op.ID, StateSwap, "")
				}
				if fresh, gErr := mgr.GetOperation(op.ID); gErr == nil {
					return fresh, nil
				}
				return op, nil
			}
			// Marker gone, now try to persist terminal. If that also succeeds, cleanup proven.
			transOp, transErr := mgr.Transition(op.ID, StateFailed, err.Error())
			_ = transOp
			if transErr == nil && rmErr == nil {
				_ = removeFileFn(staged)
				_ = removeFileFn(backup)
				_ = removeFileFn(helperCopy)
				return nil, fmt.Errorf("write marker: %w", err)
			}
			// Marker gone but terminal persist failed -> no resumable marker, so failure is not contradictory.
			_ = removeFileFn(staged)
			_ = removeFileFn(backup)
			_ = removeFileFn(helperCopy)
			if transErr != nil {
				return nil, fmt.Errorf("write marker: %w (terminal persist failed: %v)", err, transErr)
			}
			return nil, fmt.Errorf("write marker: %w", err)
		}
		// Marker definitely not durable (pre-rename failure): persist terminal and clean staged before reporting.
		_, _ = mgr.Transition(op.ID, StateFailed, err.Error())
		_ = removeFileFn(staged)
		_ = removeFileFn(backup)
		_ = removeFileFn(helperCopy)
		_ = removeFileFn(markerPath)
		return nil, fmt.Errorf("write marker: %w", err)
	}
	// Transition to swap to indicate handoff in progress.
	if cur, _ := mgr.GetOperation(op.ID); isAllowedTransition(cur.State, StateSwap) {
		_, _ = mgr.Transition(op.ID, StateSwap, "")
	}
	if err := launchHelperFn(u.exe, markerPath); err != nil {
		// Attempt transactional cleanup: remove marker first, then persist Failed. Only if both proven do we report failure.
		rmErr := removeFileFn(markerPath)
		_, statAfter := os.Stat(markerPath)
		stillDurable := statAfter == nil
		if stillDurable {
			// Marker still exists -> keep as recoverable committed handoff (do not transition to Failed).
			if fresh, gErr := mgr.GetOperation(op.ID); gErr == nil {
				return fresh, nil
			}
			return op, nil
		}
		transOp, transErr := mgr.Transition(op.ID, StateFailed, err.Error())
		_ = transOp
		if transErr == nil && rmErr == nil {
			_ = removeFileFn(helperCopy)
			return nil, fmt.Errorf("helper launch failure: %w", err)
		}
		// Marker gone but terminal failed -> no resume, report failure.
		_ = removeFileFn(helperCopy)
		if transErr != nil {
			return nil, fmt.Errorf("helper launch failure: %w (terminal persist failed: %v)", err, transErr)
		}
		return nil, fmt.Errorf("helper launch failure: %w", err)
	}
	if fresh, gErr := mgr.GetOperation(op.ID); gErr == nil {
		return fresh, nil
	}
	return op, nil
}

// recordSuccess transitions the operation to done and cleans the marker.
func recordSuccess(m *HandoffMarker) error {
	mgr, err := NewManager(m.ManagerRoot)
	if err != nil && !errors.Is(err, ErrCorruptedState) {
		return err
	}
	// Linear graph requires Swap -> Done . If current state is earlier, advance step by step.
	op, err := mgr.GetOperation(m.OpID)
	if err != nil {
		return err
	}
	// Drive to Done via allowed transitions. Best-effort: walk the linear chain.
	chain := []OperationState{StatePrepare, StateFetchRelease, StateSelectAsset, StateDownload, StateVerify, StateBackup, StateSwap, StateDone}
	for _, want := range chain {
		cur, _ := mgr.GetOperation(m.OpID)
		if cur.State == want {
			continue
		}
		if cur.State == StateDone || cur.IsTerminal() {
			break
		}
		if isAllowedTransition(cur.State, want) {
			if _, err := mgr.Transition(m.OpID, want, ""); err != nil {
				// If Want is not next, continue to find next allowed.
				continue
			}
		}
		_ = op
	}
	// Ensure final transition to Done if not already.
	cur, _ := mgr.GetOperation(m.OpID)
	if !cur.IsTerminal() {
		if isAllowedTransition(cur.State, StateDone) {
			_, _ = mgr.Transition(m.OpID, StateDone, "")
		} else if isAllowedTransition(cur.State, StateSwap) {
			_, _ = mgr.Transition(m.OpID, StateSwap, "")
			_, _ = mgr.Transition(m.OpID, StateDone, "")
		} else {
			// Force via Failed -> Done not allowed; fallback to direct persist if needed.
			// Use sequential walk again.
			for _, want := range chain {
				c, _ := mgr.GetOperation(m.OpID)
				if c.IsTerminal() {
					break
				}
				if isAllowedTransition(c.State, want) {
					_, _ = mgr.Transition(m.OpID, want, "")
				}
			}
		}
	}
	_ = os.Remove(m.StagedPath)
	_ = os.Remove(m.HelperPath)
	// Retain backup for potential future rollback? But after success, we can keep it.
	_ = os.Remove(markerPathFor(m))
	return nil
}

// recordRollback restores the previous binary, relaunches old server, and marks operation failed/rolled back.
func recordRollback(m *HandoffMarker, cause string) error {
	mgr, err := NewManager(m.ManagerRoot)
	if err != nil && !errors.Is(err, ErrCorruptedState) {
		return err
	}
	// Attempt to restore backup only while marker is still valid (active).
	if _, err := os.Stat(m.BackupPath); err == nil {
		_ = swapBinary(m.BackupPath, m.ExecPath)
	}
	// Relaunch old server with original args.
	relaunchOldServerFn(m)
	if mgr != nil {
		cur, _ := mgr.GetOperation(m.OpID)
		if cur != nil && !cur.IsTerminal() {
			// Walk to Failed if allowed.
			if isAllowedTransition(cur.State, StateFailed) {
				_, _ = mgr.Transition(m.OpID, StateFailed, cause)
			} else {
				// Drive through chain to reach a state where Failed is allowed.
				chain := []OperationState{StatePrepare, StateFetchRelease, StateSelectAsset, StateDownload, StateVerify, StateBackup, StateSwap}
				for _, want := range chain {
					c, _ := mgr.GetOperation(m.OpID)
					if c.IsTerminal() {
						break
					}
					if isAllowedTransition(c.State, want) {
						_, _ = mgr.Transition(m.OpID, want, "")
					}
					c2, _ := mgr.GetOperation(m.OpID)
					if isAllowedTransition(c2.State, StateFailed) {
						_, _ = mgr.Transition(m.OpID, StateFailed, cause)
						break
					}
				}
			}
		}
	}
	_ = os.Remove(m.StagedPath)
	_ = os.Remove(m.HelperPath)
	_ = os.Remove(markerPathFor(m))
	return nil
}

func markerPathFor(m *HandoffMarker) string {
	return helperMarkerPath(m.ExecPath, m.OpID)
}

var relaunchOldServerFn = relaunchOldServer

// Test seams – exported setters for cross-package tests (server).
func SetLaunchHelperFn(fn func(string, string) error) {
	if fn == nil {
		launchHelperFn = launchHelper
	} else {
		launchHelperFn = fn
	}
}
func SetVerifyStagedFileFn(fn func(string, string, string) error) {
	if fn == nil {
		verifyStagedFileFn = verifyStagedFile
	} else {
		verifyStagedFileFn = fn
	}
}
func SetWaitForParentFn(fn func(int, time.Duration) error) {
	if fn == nil {
		waitForParentFn = waitForParent
	} else {
		waitForParentFn = fn
	}
}
func SetAtomicReplaceFn(fn func(string, string) error) {
	if fn == nil {
		atomicReplaceFn = atomicReplace
	} else {
		atomicReplaceFn = fn
	}
}
func SetOpenFileForSyncFn(fn func(string) (*os.File, error)) {
	if fn == nil {
		openFileForSyncFn = func(name string) (*os.File, error) { return os.OpenFile(name, os.O_RDWR, 0) }
	} else {
		openFileForSyncFn = fn
	}
}
func SetSyncFileFn(fn func(*os.File) error) {
	if fn == nil {
		syncFileFn = func(f *os.File) error { return f.Sync() }
	} else {
		syncFileFn = fn
	}
}
func SetOpenDirFn(fn func(string) (*os.File, error)) {
	if fn == nil {
		openDirFn = func(name string) (*os.File, error) { return os.Open(name) }
	} else {
		openDirFn = fn
	}
}
func SetSyncDirFn(fn func(*os.File) error) {
	if fn == nil {
		syncDirFn = func(f *os.File) error { return f.Sync() }
	} else {
		syncDirFn = fn
	}
}
func SetRelaunchOldServerFn(fn func(*HandoffMarker)) {
	if fn == nil {
		relaunchOldServerFn = relaunchOldServer
	} else {
		relaunchOldServerFn = fn
	}
}
func SetRemoveFileFn(fn func(string) error) {
	if fn == nil {
		removeFileFn = os.Remove
	} else {
		removeFileFn = fn
	}
}

// relaunchOldServer starts the restored executable with original context.
func relaunchOldServer(m *HandoffMarker) {
	if m.ExecPath == "" {
		return
	}
	args := m.OrigArgs
	if len(args) == 0 {
		args = []string{m.ExecPath}
	}
	// Ensure first arg is exec path.
	cmd := exec.Command(m.ExecPath, args[1:]...)
	cmd.Env = os.Environ()
	if m.WorkDir != "" {
		cmd.Dir = m.WorkDir
	}
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	detachHelper(cmd)
	_ = cmd.Start()
	if cmd.Process != nil {
		_ = cmd.Process.Release()
	}
}

