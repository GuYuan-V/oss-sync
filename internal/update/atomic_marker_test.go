package update

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestAtomicWriteMarker_PropagatesFileSyncError(t *testing.T) {
	exeDir := t.TempDir()
	exePath := filepath.Join(exeDir, "oss-server")
	_ = os.WriteFile(exePath, []byte("old"), 0o755)
	marker := HandoffMarker{
		OpID:          "op-sync-fail",
		ManagerRoot:   t.TempDir(),
		ExecPath:      exePath,
		StagedPath:    filepath.Join(exeDir, ".oss-update-pending", "staged-op-sync-fail"),
		BackupPath:    filepath.Join(exeDir, ".oss-update-pending", "backup-op-sync-fail"),
		HelperPath:    filepath.Join(exeDir, ".oss-update-pending", "helper-op-sync-fail"),
		TargetVersion: "1.2.3",
		Digest:        "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		ParentPID:     123,
		ReadyURL:      "http://127.0.0.1:8080/readyz",
	}
	markerPath := filepath.Join(exeDir, ".oss-update-pending", "op-sync-fail.handoff.json")
	origSyncFile := syncFileFn
	syncFileFn = func(f *os.File) error { return os.ErrInvalid }
	t.Cleanup(func() { syncFileFn = origSyncFile })
	err := atomicWriteMarker(markerPath, marker)
	if err == nil {
		t.Fatal("expected fsync file error propagation")
	}
	if _, err := os.Stat(markerPath); !os.IsNotExist(err) {
		t.Error("marker should not exist after temp sync failure")
	}
	tmpGlob, _ := filepath.Glob(markerPath + ".tmp.*")
	if len(tmpGlob) != 0 {
		t.Errorf("temp file should be cleaned up, found %v", tmpGlob)
	}
}

func TestAtomicWriteMarker_PropagatesOpenDirError(t *testing.T) {
	exeDir := t.TempDir()
	exePath := filepath.Join(exeDir, "oss-server")
	_ = os.WriteFile(exePath, []byte("old"), 0o755)
	marker := HandoffMarker{
		OpID:          "op-dir-open-fail",
		ManagerRoot:   t.TempDir(),
		ExecPath:      exePath,
		StagedPath:    filepath.Join(exeDir, ".oss-update-pending", "staged-op-dir-open-fail"),
		BackupPath:    filepath.Join(exeDir, ".oss-update-pending", "backup-op-dir-open-fail"),
		HelperPath:    filepath.Join(exeDir, ".oss-update-pending", "helper-op-dir-open-fail"),
		TargetVersion: "1.2.3",
		Digest:        "sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb",
		ParentPID:     123,
		ReadyURL:      "http://127.0.0.1:8080/readyz",
	}
	markerPath := filepath.Join(exeDir, ".oss-update-pending", "op-dir-open-fail.handoff.json")
	origOpenDir := openDirFn
	openDirFn = func(name string) (*os.File, error) { return nil, os.ErrPermission }
	t.Cleanup(func() { openDirFn = origOpenDir })
	err := atomicWriteMarker(markerPath, marker)
	if err == nil {
		t.Fatal("expected open dir error propagation")
	}
}

func TestAtomicWriteMarker_PropagatesDirSyncError(t *testing.T) {
	exeDir := t.TempDir()
	exePath := filepath.Join(exeDir, "oss-server")
	_ = os.WriteFile(exePath, []byte("old"), 0o755)
	marker := HandoffMarker{
		OpID:          "op-dir-sync-fail",
		ManagerRoot:   t.TempDir(),
		ExecPath:      exePath,
		StagedPath:    filepath.Join(exeDir, ".oss-update-pending", "staged-op-dir-sync-fail"),
		BackupPath:    filepath.Join(exeDir, ".oss-update-pending", "backup-op-dir-sync-fail"),
		HelperPath:    filepath.Join(exeDir, ".oss-update-pending", "helper-op-dir-sync-fail"),
		TargetVersion: "1.2.3",
		Digest:        "sha256:cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc",
		ParentPID:     123,
		ReadyURL:      "http://127.0.0.1:8080/readyz",
	}
	markerPath := filepath.Join(exeDir, ".oss-update-pending", "op-dir-sync-fail.handoff.json")
	origSyncDir := syncDirFn
	syncDirFn = func(f *os.File) error { return os.ErrInvalid }
	t.Cleanup(func() { syncDirFn = origSyncDir })
	err := atomicWriteMarker(markerPath, marker)
	if runtime.GOOS == "windows" && isWindowsDirSyncUnsupported(os.ErrInvalid) {
		// narrow Windows exception: should be ignored and return nil
		if err != nil {
			t.Errorf("Windows dir sync unsupported should be ignored, got %v", err)
		}
	} else {
		if err == nil {
			t.Fatal("expected fsync dir error propagation")
		}
	}
}

func TestAtomicWriteMarker_WindowsDirSyncExceptionNarrow(t *testing.T) {
	got := isWindowsDirSyncUnsupported(os.ErrInvalid)
	if runtime.GOOS == "windows" && !got {
		t.Error("Windows should detect ErrInvalid as unsupported directory sync")
	}
	if runtime.GOOS != "windows" && got {
		t.Error("non-Windows platforms must not ignore ErrInvalid")
	}
}
