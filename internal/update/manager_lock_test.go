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
	// 以当前进程持有文件锁。
	release, err := acquireFileLock(root)
	if err != nil {
		t.Fatalf("initial acquire: %v", err)
	}
	// 测试期间保持持有，结束前不释放；NewManager 应在超时内获取失败。
	start := time.Now()
	_, err = NewManager(root)
	elapsed := time.Since(start)
	if err == nil {
		t.Fatalf("expected NewManager to fail when lock held by live owner, got nil")
	}
	if elapsed < 400*time.Millisecond {
		t.Logf("warning: NewManager failed fast (elapsed %v), may not have waited for lock timeout", elapsed)
	}
	// 活跃锁持有期间直接再取锁同样应失败。
	_, err = acquireFileLock(root)
	if err == nil {
		t.Error("second acquireFileLock should fail while live lock held")
	}
	release()
	// 释放后 NewManager 应成功。
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
	// 写入极不可能存在的死亡 PID 构造陈旧锁。
	const deadPID = 999999
	writeLockFile(t, root, deadPID)
	// 先确认该 PID 在本平台确实判死。
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
	// 陈旧锁应被替换，残留文件不得仍归属死亡 PID。
	data, err := os.ReadFile(filepath.Join(root, ".update_state.lock"))
	if err == nil {
		// NewManager 在临界区持有锁并在返回时释放；若文件仍存在，必须不是死亡 PID。
		var cur lockMeta
		if json.Unmarshal(data, &cur) == nil && cur.PID == deadPID {
			t.Errorf("stale lock with dead PID still present after recovery")
		}
	}
	// 恢复后正常签发应可用。
	c := testCandidate("1.2.3")
	if _, err := m.IssueChecked(c, time.Minute); err != nil {
		t.Fatalf("IssueChecked after stale recovery: %v", err)
	}
}

func TestNewManager_NonStealingActiveLock(t *testing.T) {
	root := t.TempDir()
	// 以当前进程 PID 构造活跃锁。
	writeLockFile(t, root, os.Getpid())
	// 不得窃取活跃锁，应超时失败。
	start := time.Now()
	_, err := acquireFileLock(root)
	elapsed := time.Since(start)
	if err == nil {
		t.Fatal("acquire should not steal live lock")
	}
	if elapsed < 400*time.Millisecond {
		t.Logf("acquire failed fast, elapsed %v", elapsed)
	}
	// 失败后锁文件仍存在且仍归属原持有者。
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
	// 清理锁文件。
	_ = os.Remove(filepath.Join(root, ".update_state.lock"))
	// 锁移除后 NewManager 应成功。
	m, err := NewManager(root)
	if err != nil {
		t.Fatalf("NewManager after active lock removed: %v", err)
	}
	_ = m
}
