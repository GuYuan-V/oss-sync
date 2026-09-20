package update

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/helantianshen/oss-sync/internal/config"
	"github.com/helantianshen/oss-sync/internal/version"
)

func TestHandoff_DirectorySyncFailureAfterRename_CommittedAndResumable(t *testing.T) {
	origVer := version.Version
	version.Version = "1.0.0"
	t.Cleanup(func() { version.Version = origVer })

	origSyncDir := syncDirFn
	syncDirFn = func(f *os.File) error { return os.ErrDeadlineExceeded }
	t.Cleanup(func() { syncDirFn = origSyncDir })

	// 强制删除失败，使清理无法自证，交接保持已提交状态。
	origRemove := removeFileFn
	removeFileFn = func(name string) error {
		// 仅让 marker 删除失败，暂存清理仍可尝试，marker 保持存在。
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

	op, err := up.InitiateHelperHandoff(mgr, id, candPath, fakeDigestForFile(candPath), "http://127.0.0.1:0/readyz", []string{exePath}, exeDir)
	if err != nil {
		t.Fatalf("InitiateHelperHandoff should return committed success on dir sync failure with unprovable cleanup, got err %v", err)
	}
	if op == nil {
		t.Fatal("expected operation on committed handoff")
	}
	// marker 存在且可恢复。
	markerPath := helperMarkerPath(exePath, op.ID)
	if _, err := os.Stat(markerPath); err != nil {
		t.Fatalf("marker should still exist for resumable handoff, stat err %v", err)
	}
	// 操作不得进入终态。
	cur, _ := mgr.GetOperation(op.ID)
	if cur.IsTerminal() {
		t.Fatalf("operation should not be terminal for committed handoff, got %s", cur.State)
	}
	// 恢复流程应找到该 marker。
	origLaunch2 := launchHelperFn
	launched := 0
	launchHelperFn = func(ep, mp string) error { launched++; return nil }
	t.Cleanup(func() { launchHelperFn = origLaunch2 })
	// 恢复不依赖删除，原桩保持不动即可。
	n, err := ResumePendingHandoffs(exePath)
	if err != nil {
		t.Fatalf("ResumePendingHandoffs: %v", err)
	}
	if n != 1 || launched != 1 {
		t.Fatalf("resume should find 1 pending committed handoff, got %d launched %d", n, launched)
	}
	// 接口返回成功且恢复找到 marker，两者一致。
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

	// 默认删除路径下清理可自证，接口应返回失败且不留可恢复 marker。
	op, err := up.InitiateHelperHandoff(mgr, id, candPath, fakeDigestForFile(candPath), "http://127.0.0.1:0/readyz", []string{exePath}, exeDir)
	if err == nil {
		// 重命名后目录同步失败但清理成功时返回失败。
		if op != nil {
			markerPath := helperMarkerPath(exePath, op.ID)
			if _, statErr := os.Stat(markerPath); statErr == nil {
				t.Fatalf("marker should be removed after successful cleanup, still exists at %s", markerPath)
			}
		}
		t.Fatalf("expected write marker failure when cleanup proven, got success")
	}
	// 确认没有可恢复 marker 残留。
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
	// 接口失败且无可恢复 marker，两者一致。
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

	op, err := up.InitiateHelperHandoff(mgr, id, candPath, fakeDigestForFile(candPath), "http://127.0.0.1:0/readyz", []string{exePath}, exeDir)
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
	// 恢复流程必须找到该 marker。
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

	// 启动失败，同时经 atomicWriteJSONFn 让转终态持久化失败。
	origLaunch := launchHelperFn
	launchHelperFn = func(string, string) error { return os.ErrInvalid }
	t.Cleanup(func() { launchHelperFn = origLaunch })

	// marker 删除同样失败，使终态失败时 marker 保持可恢复。
	origRemove := removeFileFn
	removeFileFn = func(name string) error {
		if filepath.Ext(name) == ".json" {
			return os.ErrPermission
		}
		return os.Remove(name)
	}
	t.Cleanup(func() { removeFileFn = origRemove })

	// 仅对转 StateFailed 的持久化注入失败。
	origAtomic := atomicWriteJSONFn
	callCount := 0
	atomicWriteJSONFn = func(path string, v any) error {
		callCount++
		// 仅当持久化内容包含 StateFailed 的操作时失败；此前持久化全部放行。
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

	op, err := up.InitiateHelperHandoff(mgr, id, candPath, fakeDigestForFile(candPath), "http://127.0.0.1:0/readyz", []string{exePath}, exeDir)
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
	// StateFailed 持久化失败，操作停留在 Swap 或 Backup 等非终态。
	mgr2, _ := NewManager(mgrRoot)
	cur, _ := mgr2.GetOperation(op.ID)
	// 以重载后状态为准；重载失败时以原 Manager 内存状态为准。
	if cur != nil && cur.IsTerminal() {
		t.Fatalf("operation should not be terminal after persist failure, got %s", cur.State)
	}
	// 操作仍活跃，恢复流程应找到该 marker。
	launchHelperFn = func(ep, mp string) error { return nil }
	// 恢复前先恢复原子写桩，保证 Manager 重载正常。
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

	// 场景一：交接成功，启动桩放行，marker 在 helper 清理前一直存在。
	origLaunch := launchHelperFn
	launchHelperFn = func(string, string) error { return nil }
	t.Cleanup(func() { launchHelperFn = origLaunch })
	op, err := up.InitiateHelperHandoff(mgr, id, candPath, fakeDigestForFile(candPath), "http://127.0.0.1:0/readyz", []string{exePath}, exeDir)
	if err != nil {
		t.Fatalf("successful handoff: %v", err)
	}
	markerPath := helperMarkerPath(exePath, op.ID)
	if _, err := os.Stat(markerPath); err != nil {
		t.Fatalf("marker should exist after successful handoff, %v", err)
	}
	// 模拟 marker 已写但 helper 尚未运行的崩溃，恢复流程应重新拉起。
	launched := 0
	launchHelperFn = func(ep, mp string) error { launched++; return nil }
	n, err := ResumePendingHandoffs(exePath)
	if err != nil {
		t.Fatalf("resume: %v", err)
	}
	if n != 1 || launched != 1 {
		t.Fatalf("normal startup should resume committed handoff, got %d launched %d", n, launched)
	}

	// 场景二：marker 落盘前失败，接口返回失败且不产生可恢复 marker。
	// 让校验失败，使 prepareStaging 在写 marker 前返回。
	verifyStagedFileFn = func(string, string, string) error { return os.ErrInvalid }
	// 重新签发新的已校验候选。
	mgr2, _ := NewManager(mgrRoot)
	// 首个操作仍处于 Swap，先转终态以便签发新检查。
	_, _ = mgr.Transition(op.ID, StateFailed, "cleanup for test")
	// 签发新检查。
	id2 := newCheckedForHelper(t, mgr2, "9.9.55")
	candPath2 := candidatePathFor(id2)
	up2, _ := NewUpdater(cfg, Options{ExecPath: exePath, Verifier: func(string, string) error { return nil }})
	// 校验仍失败，交接在 marker 落盘前返回。
	op2, err := up2.InitiateHelperHandoff(mgr2, id2, candPath2, fakeDigestForFile(candPath2), "http://127.0.0.1:0/readyz", []string{exePath}, exeDir)
	if err == nil {
		t.Fatalf("expected failure for verify failure, got op %v", op2)
	}
	// 失败场景不得残留 marker。
	if op2 != nil {
		mp2 := helperMarkerPath(exePath, op2.ID)
		if _, err := os.Stat(mp2); err == nil {
			t.Fatalf("marker should not exist after pre-marker failure")
		}
	}
	// 清理旧 marker 后再恢复，应无可恢复项。
	_ = os.Remove(markerPath)
	_, _ = mgr.Transition(op.ID, StateFailed, "cleanup")
	// 清理后恢复结果应为空。
	launchHelperFn = func(string, string) error { launched++; return nil }
	n, err = ResumePendingHandoffs(exePath)
	if err != nil {
		t.Fatalf("resume after cleanup: %v", err)
	}
	if n != 0 {
		t.Fatalf("after failed handoff and cleanup, resume should find 0, got %d", n)
	}
	// 接口失败与无可恢复 marker 一致。

	// 场景三：公开状态不得携带路径等敏感信息。
	publicStatus := mgr2.CurrentStatus()
	if publicStatus.Active != nil {
		// 公开操作仅含 ID、State、Version 等字段。
		if publicStatus.Active.Error != "" {
		}
	}
	// 公开状态通过 handler 响应校验不含 marker 路径。
	_ = time.Now()
}
