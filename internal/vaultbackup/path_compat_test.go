package vaultbackup

import (
	"os"
	"path/filepath"
	"testing"
)

func TestExistingPathSupportsLegacyAndCurrentArchives(t *testing.T) {
	t.Chdir(t.TempDir())
	dataDir := "data"
	oldPath := filepath.Join("backups", "vaults", "legacy.zip")
	newPath, err := Path(dataDir, "current.zip")
	if err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{oldPath, newPath} {
		if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(path), 0600); err != nil {
			t.Fatal(err)
		}
		resolved, err := ExistingPath(dataDir, filepath.Base(path))
		if err != nil || resolved != path {
			t.Fatalf("resolve %s: %s %v", path, resolved, err)
		}
	}
	if _, err := ExistingPath(dataDir, "absent.zip"); !os.IsNotExist(err) {
		t.Fatalf("missing archive: %v", err)
	}
	for _, invalid := range []string{"../legacy.zip", "/tmp/legacy.zip", "bad.txt", ""} {
		if _, err := ExistingPath(dataDir, invalid); err == nil {
			t.Fatalf("unsafe name accepted: %q", invalid)
		}
	}
	// 新备份固定写入数据目录，同名旧归档不改变写入位置。
	writePath, err := Path(dataDir, "legacy.zip")
	if err != nil {
		t.Fatal(err)
	}
	if writePath == oldPath {
		t.Fatal("new writes fell back to the legacy directory")
	}
}
