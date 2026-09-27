package update

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

// 网页触发的 systemd 托管更新契约：网页（非特权）把目标版本写入请求文件，
// 宿主机 oss-sync-update.path 触发 root oneshot（oss.sh apply-web-update）执行下载、校验、
// 换装与重启，完成后把结果写回 result.json。网页只写请求、只读结果，绝不把二进制交给 root
const (
	managedDirName    = ".update"
	managedReqName    = "request"
	managedResultName = "result.json"
)

// ManagedRequest 是网页写给宿主机 updater 的请求，仅含目标版本
type ManagedRequest struct {
	OpID          string `json:"op_id"`
	TargetVersion string `json:"target_version"`
}

// ManagedResult 是宿主机 updater 写回的结果
type ManagedResult struct {
	OpID       string `json:"op_id"`
	Version    string `json:"version"`
	Status     string `json:"status"` // success / failed
	Error      string `json:"error,omitempty"`
	FinishedAt string `json:"finished_at,omitempty"`
}

func managedDir(dataDir string) string     { return filepath.Join(dataDir, managedDirName) }
func managedReqPath(dataDir string) string { return filepath.Join(managedDir(dataDir), managedReqName) }
func managedResPath(dataDir string) string {
	return filepath.Join(managedDir(dataDir), managedResultName)
}

// ManagedRequestPending 判断是否有尚未被宿主机处理的更新请求
func ManagedRequestPending(dataDir string) bool {
	_, err := os.Stat(managedReqPath(dataDir))
	return err == nil
}

// ReadManagedResult 读取宿主机写回的最近一次更新结果，不存在或损坏时返回 false
func ReadManagedResult(dataDir string) (*ManagedResult, bool) {
	data, err := os.ReadFile(managedResPath(dataDir))
	if err != nil {
		return nil, false
	}
	var r ManagedResult
	if err := json.Unmarshal(data, &r); err != nil {
		return nil, false
	}
	return &r, true
}

// WriteManagedRequest 原子写入更新请求，供 oss-sync-update.path 触发宿主机 updater
func WriteManagedRequest(dataDir, opID, targetVersion string) error {
	dir := managedDir(dataDir)
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return fmt.Errorf("create managed update dir: %w", err)
	}
	payload, err := json.Marshal(ManagedRequest{OpID: opID, TargetVersion: targetVersion})
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, ".request-*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	if _, err := tmp.Write(payload); err != nil {
		_ = tmp.Close()
		_ = os.Remove(tmpName)
		return err
	}
	if err := tmp.Close(); err != nil {
		_ = os.Remove(tmpName)
		return err
	}
	_ = os.Chmod(tmpName, 0o640)
	// 原子改名到 path unit 监听的固定路径，避免触发到半截文件
	if err := os.Rename(tmpName, managedReqPath(dataDir)); err != nil {
		_ = os.Remove(tmpName)
		return err
	}
	return nil
}

// StartManagedUpdate 声明持久化操作并写入更新请求，交由宿主机 path unit 执行
// 返回操作 ID 与目标版本；不下载、不替换、不关停自身，实际更新与重启由 root oneshot 完成
func (s *Service) StartManagedUpdate(checkID string) (opID string, targetVersion string, err error) {
	if s.mgr == nil {
		return "", "", errors.New("manager is nil")
	}
	if !ManagedUpdateEnabled() {
		return "", "", ErrExternalUpdate
	}
	if s.cfg == nil || s.cfg.Storage.DataDir == "" {
		return "", "", errors.New("data dir not configured")
	}
	if checkID == "" {
		return "", "", newUpdateError(CodeCheckNotFound, "check_id is empty", ErrCheckNotFound)
	}
	// 声明持久化操作：进入 History、作为唯一活跃操作，并校验候选有效性
	op, err := s.mgr.StartOperation(checkID, "")
	if err != nil {
		return "", "", err
	}
	if op.Candidate != nil {
		targetVersion = op.Candidate.Version
	}
	dataDir := s.cfg.Storage.DataDir
	// 清理上一次结果，避免旧结果被误当作本次结果
	_ = os.Remove(managedResPath(dataDir))
	if err := WriteManagedRequest(dataDir, op.ID, targetVersion); err != nil {
		// 写请求失败则把操作置为失败，避免留下永久活跃操作
		_, _ = s.mgr.Transition(op.ID, StateFailed, err.Error())
		return "", "", err
	}
	return op.ID, targetVersion, nil
}

// ReconcileManagedUpdate 依据宿主机写回的 result.json 收敛活跃的托管更新操作
// 幂等：结果缺失、操作已终态或未找到时直接返回，可在启动与每次状态查询时调用
func (s *Service) ReconcileManagedUpdate() {
	if s.mgr == nil || s.cfg == nil || s.cfg.Storage.DataDir == "" {
		return
	}
	res, ok := ReadManagedResult(s.cfg.Storage.DataDir)
	if !ok || res.OpID == "" {
		return
	}
	op, err := s.mgr.GetOperation(res.OpID)
	if err != nil || op == nil || op.IsTerminal() {
		return
	}
	if res.Status == "success" {
		s.driveManagedToDone(res.OpID)
		return
	}
	msg := res.Error
	if msg == "" {
		msg = "managed update failed"
	}
	_, _ = s.mgr.Transition(res.OpID, StateFailed, msg)
}

// driveManagedToDone 沿线性状态图把操作从 in_progress 推进到 done
func (s *Service) driveManagedToDone(opID string) {
	for _, next := range []OperationState{StatePrepare, StateFetchRelease, StateSelectAsset, StateDownload, StateVerify, StateBackup, StateSwap, StateDone} {
		cur, err := s.mgr.GetOperation(opID)
		if err != nil || cur == nil || cur.IsTerminal() {
			return
		}
		if isAllowedTransition(cur.State, next) {
			if _, err := s.mgr.Transition(opID, next, ""); err != nil {
				return
			}
		}
	}
}
