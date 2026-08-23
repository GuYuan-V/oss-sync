// 更新存储
package update

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
)

// CheckedCandidate 是一次已校验的更新候选，携带过期时间与校验标识。
type CheckedCandidate struct {
	ID        string    `json:"id"`
	Candidate Candidate `json:"candidate"`
	CreatedAt int64     `json:"created_at_unix"`
	ExpiresAt int64     `json:"expires_at_unix"`
}

const (
	CodeCheckExpired      ErrorCode = "check_expired"
	CodeCheckNotFound     ErrorCode = "check_not_found"
	CodeCheckMismatch     ErrorCode = "check_mismatch"
	CodeAlreadyInProgress ErrorCode = "already_in_progress"
	CodeInvalidTransition ErrorCode = "invalid_transition"
	CodeCorruptedState    ErrorCode = "corrupted_state"
)

var (
	ErrCheckExpired      = &UpdateError{Code: CodeCheckExpired, Message: "checked candidate expired"}
	ErrCheckNotFound     = &UpdateError{Code: CodeCheckNotFound, Message: "checked candidate not found"}
	ErrCheckMismatch     = &UpdateError{Code: CodeCheckMismatch, Message: "target does not match checked candidate"}
	ErrAlreadyInProgress = &UpdateError{Code: CodeAlreadyInProgress, Message: "another operation is already in progress"}
	ErrInvalidTransition = &UpdateError{Code: CodeInvalidTransition, Message: "invalid state transition"}
	ErrCorruptedState    = &UpdateError{Code: CodeCorruptedState, Message: "corrupted persisted state"}
)

var (
	stateFileName  = "update_state.json"
	maxHistoryKept = 20
)

type persistedState struct {
	Checked  map[string]CheckedCandidate `json:"checked"`
	Ops      map[string]Operation        `json:"operations"`
	ActiveID string                      `json:"active_id"`
	History  []string                    `json:"history"`
}

func newEmptyState() persistedState {
	return persistedState{
		Checked: make(map[string]CheckedCandidate),
		Ops:     make(map[string]Operation),
		History: []string{},
	}
}

func stateFilePath(root string) string {
	return filepath.Join(root, stateFileName)
}

func loadState(path string) (persistedState, error) {
	st := newEmptyState()
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return st, nil
		}
		return st, newUpdateError(CodeCorruptedState, "read state: "+err.Error(), err)
	}
	if len(data) == 0 {
		return st, nil
	}
	if err := json.Unmarshal(data, &st); err != nil {
		return newEmptyState(), newUpdateError(CodeCorruptedState, "corrupted json: "+err.Error(), err)
	}
	if st.Checked == nil {
		st.Checked = make(map[string]CheckedCandidate)
	}
	if st.Ops == nil {
		st.Ops = make(map[string]Operation)
	}
	if st.History == nil {
		st.History = []string{}
	}
	return st, nil
}

var globalRootLocks sync.Map // map[string]*sync.Mutex

func rootMutex(root string) *sync.Mutex {
	if root == "" {
		root = "__empty__"
	}
	v, _ := globalRootLocks.LoadOrStore(root, &sync.Mutex{})
	return v.(*sync.Mutex)
}

type lockMeta struct {
	PID  int   `json:"pid"`
	Time int64 `json:"time"`
}

func acquireFileLock(root string) (func(), error) {
	if root == "" {
		return func() {}, nil
	}
	lockPath := filepath.Join(root, ".update_state.lock")
	if err := os.MkdirAll(root, 0o755); err != nil {
		return nil, err
	}
	selfPID := os.Getpid()
	now := time.Now().UnixNano()
	deadline := time.Now().Add(1200 * time.Millisecond)
	for time.Now().Before(deadline) {
		f, err := os.OpenFile(lockPath, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
		if err == nil {
			meta := lockMeta{PID: selfPID, Time: now}
			data, _ := json.Marshal(meta)
			_, _ = f.Write(data)
			_ = f.Sync()
			_ = f.Close()
			release := func() {
				// only remove if we still own it
				if data, err := os.ReadFile(lockPath); err == nil {
					var cur lockMeta
					if json.Unmarshal(data, &cur) == nil && cur.PID == selfPID {
						_ = os.Remove(lockPath)
					} else if strings.TrimSpace(string(data)) == fmt.Sprintf("%d", selfPID) {
						_ = os.Remove(lockPath)
					}
				}
			}
			return release, nil
		}
		if !errors.Is(err, os.ErrExist) {
			time.Sleep(10 * time.Millisecond)
			continue
		}
		// lock exists: inspect owner
		data, err := os.ReadFile(lockPath)
		if err != nil {
			time.Sleep(10 * time.Millisecond)
			continue
		}
		var meta lockMeta
		if err := json.Unmarshal(data, &meta); err != nil {
			// try plain pid fallback
			var pid int
			if _, err := fmt.Sscanf(strings.TrimSpace(string(data)), "%d", &pid); err == nil {
				meta.PID = pid
			} else {
				// corrupted: cannot determine owner – conservative: do not steal, wait
				time.Sleep(10 * time.Millisecond)
				continue
			}
		}
		if isProcessAlive(meta.PID) {
			time.Sleep(10 * time.Millisecond)
			continue
		}
		// owner dead – attempt to break stale lock, but verify still same dead owner
		curData, err := os.ReadFile(lockPath)
		if err != nil {
			continue
		}
		var cur lockMeta
		if json.Unmarshal(curData, &cur) != nil {
			var pid int
			if _, err := fmt.Sscanf(strings.TrimSpace(string(curData)), "%d", &pid); err != nil {
				time.Sleep(10 * time.Millisecond)
				continue
			}
			cur.PID = pid
		}
		if cur.PID != meta.PID {
			time.Sleep(10 * time.Millisecond)
			continue
		}
		_ = os.Remove(lockPath)
		// retry immediately without sleep
		continue
	}
	return nil, newUpdateError(CodeCorruptedState, "failed to acquire state lock", nil)
}

var atomicWriteJSONFn = atomicWriteJSON

func atomicWriteJSON(path string, v any) error {
	data, err := json.Marshal(v)
	if err != nil {
		return err
	}
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	tmp := path + ".tmp." + uuid.NewString()
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return err
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	return nil
}

// SetAtomicWriteJSONFn injects atomicWriteJSON for tests.
func SetAtomicWriteJSONFn(fn func(string, any) error) {
	if fn == nil {
		atomicWriteJSONFn = atomicWriteJSON
	} else {
		atomicWriteJSONFn = fn
	}
}

func cloneCandidate(c *Candidate) *Candidate {
	if c == nil {
		return nil
	}
	cp := *c
	return &cp
}

func cloneOperation(o Operation) Operation {
	cp := o
	cp.Candidate = cloneCandidate(o.Candidate)
	return cp
}

func cloneChecked(cc CheckedCandidate) CheckedCandidate {
	cp := cc
	cp.Candidate = *cloneCandidate(&cc.Candidate)
	return cp
}

func isAllowedTransition(from, to OperationState) bool {
	if from.IsTerminal() || from == to {
		return false
	}
	// failures allowed from any active phase
	if to == StateFailed {
		switch from {
		case StateInProgress, StatePrepare, StateFetchRelease, StateSelectAsset, StateDownload, StateVerify, StateBackup, StateSwap, StateChecking:
			return true
		default:
			return false
		}
	}
	// up_to_date only from release checking (fetch_release / checking)
	if to == StateUpToDate {
		return from == StateFetchRelease || from == StateChecking
	}
	// exact linear durable graph: in_progress -> prepare -> fetch_release -> select_asset -> download -> verify -> backup -> swap -> done
	linear := map[OperationState]OperationState{
		StateInProgress:   StatePrepare,
		StatePrepare:      StateFetchRelease,
		StateFetchRelease: StateSelectAsset,
		StateSelectAsset:  StateDownload,
		StateDownload:     StateVerify,
		StateVerify:       StateBackup,
		StateBackup:       StateSwap,
		StateSwap:         StateDone,
	}
	if nxt, ok := linear[from]; ok && to == nxt {
		return true
	}
	// allow idle -> in_progress and checking -> in_progress as entry points (legacy)
	if from == StateIdle && to == StateInProgress {
		return true
	}
	if from == StateChecking && to == StateInProgress {
		return true
	}
	return false
}

func (s *persistedState) retainHistory() {
	if len(s.History) <= maxHistoryKept {
		return
	}
	keep := make(map[string]struct{}, maxHistoryKept)
	start := len(s.History) - maxHistoryKept
	for _, id := range s.History[start:] {
		keep[id] = struct{}{}
	}
	if s.ActiveID != "" {
		keep[s.ActiveID] = struct{}{}
	}
	for id, op := range s.Ops {
		if _, ok := keep[id]; !ok && op.IsTerminal() {
			delete(s.Ops, id)
		}
	}
	filtered := s.History[:0]
	for _, id := range s.History {
		if _, ok := s.Ops[id]; ok {
			filtered = append(filtered, id)
		}
	}
	s.History = filtered
	if len(s.History) > maxHistoryKept {
		s.History = s.History[len(s.History)-maxHistoryKept:]
	}
}

// PublicOperation 是对外暴露的精简操作视图，不含下载 URL 与本地路径。
type PublicOperation struct {
	ID        string         `json:"id"`
	State     OperationState `json:"state"`
	Version   string         `json:"version"`
	StartedAt int64          `json:"started_at_unix"`
	UpdatedAt int64          `json:"updated_at_unix"`
	Error     string         `json:"error,omitempty"`
}

// ManagerStatus 是对外暴露的精简状态，不含 GitHub/可执行路径。
type ManagerStatus struct {
	Active  *PublicOperation  `json:"active,omitempty"`
	History []PublicOperation `json:"history"`
}

func toPublic(op Operation) PublicOperation {
	ver := ""
	if op.Candidate != nil {
		ver = op.Candidate.Version
	}
	return PublicOperation{
		ID:        op.ID,
		State:     op.State,
		Version:   ver,
		StartedAt: op.StartedAt.Unix(),
		UpdatedAt: op.UpdatedAt.Unix(),
		Error:     op.Error,
	}
}

