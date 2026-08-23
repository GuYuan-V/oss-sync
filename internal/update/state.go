// 更新状态
package update

import (
	"time"

	"github.com/oss/oss-server/internal/version"
)

// OperationState 是更新操作的有穷状态，字符串值稳定，作为 API 与持久化契约。
type OperationState string

const (
	StateIdle         OperationState = "idle"
	StatePrepare      OperationState = "prepare"
	StateFetchRelease OperationState = "fetch_release"
	StateSelectAsset  OperationState = "select_asset"
	StateDownload     OperationState = "download"
	StateVerify       OperationState = "verify"
	StateBackup       OperationState = "backup"
	StateSwap         OperationState = "swap"
	StateDone         OperationState = "done"
	StateFailed       OperationState = "failed"
	StateUpToDate     OperationState = "up_to_date"
	StateChecking     OperationState = "checking"
	StateInProgress   OperationState = "in_progress"
)

// IsTerminal 判断是否为终态。
func (s OperationState) IsTerminal() bool {
	switch s {
	case StateDone, StateFailed, StateUpToDate:
		return true
	default:
		return false
	}
}

// IsValid 判断是否为已知状态。
func (s OperationState) IsValid() bool {
	switch s {
	case StateIdle, StatePrepare, StateFetchRelease, StateSelectAsset,
		StateDownload, StateVerify, StateBackup, StateSwap,
		StateDone, StateFailed, StateUpToDate, StateChecking, StateInProgress:
		return true
	default:
		return false
	}
}

// String 返回状态的字符串表示。
func (s OperationState) String() string { return string(s) }

// CheckResult 是一次“检查更新”的结果，同时写入状态供 /update/status 查询。
type CheckResult struct {
	CheckedAt       time.Time `json:"checked_at"`
	CurrentVersion  string    `json:"current_version"`
	LatestVersion   string    `json:"latest_version,omitempty"`
	UpdateAvailable bool      `json:"update_available"`
	ReleaseURL      string    `json:"release_url,omitempty"`
	Note            string    `json:"note,omitempty"`
}

// UpdateResult 是一次“执行更新”的结果。
type UpdateResult struct {
	At         time.Time      `json:"at"`
	OK         bool           `json:"ok"`
	Code       string         `json:"code"`  // ok / in_progress / up_to_date / github_error / failed
	Phase      OperationState `json:"phase"` // 稳定的操作阶段，用于状态机与排障
	State      OperationState `json:"state"` // Phase 的别名，保持兼容
	Version    string         `json:"version,omitempty"`
	Error      string         `json:"error,omitempty"`
	BackupPath string         `json:"backup_path,omitempty"`
}

// Status 是 /update/status 返回的运行时状态。
type Status struct {
	Version          string         `json:"version"`
	ExecPath         string         `json:"exec_path"`
	BackupPath       string         `json:"backup_path"`
	UpdateInProgress bool           `json:"update_in_progress"`
	State            OperationState `json:"state"`
	LastCheck        *CheckResult   `json:"last_check,omitempty"`
	LastUpdate       *UpdateResult  `json:"last_update,omitempty"`
}

// Status 返回更新器的运行时状态。
func (u *Updater) Status() Status {
	u.stateMu.Lock()
	defer u.stateMu.Unlock()
	state := OperationState(u.updatePhase)
	if state == "" {
		state = StateIdle
	}
	var lc *CheckResult
	if u.lastCheck != nil {
		cp := *u.lastCheck
		lc = &cp
	}
	var lu *UpdateResult
	if u.lastUpdate != nil {
		cp := *u.lastUpdate
		lu = &cp
	}
	return Status{
		Version:          version.Version,
		ExecPath:         u.exe,
		BackupPath:       u.backup,
		UpdateInProgress: u.running.Load(),
		State:            state,
		LastCheck:        lc,
		LastUpdate:       lu,
	}
}

