package storagequota

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestWithinLimitCountsWholeDataDirectory(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "oss.db"), make([]byte, 8), 0o600); err != nil {
		t.Fatal(err)
	}
	called := false
	err := WithinLimit(root, 10, 3, func() error { called = true; return nil })
	if !errors.Is(err, ErrExceeded) || called {
		t.Fatalf("want quota rejection before commit, err=%v called=%v", err, called)
	}
	if err := WithinLimit(root, 12, 3, func() error { called = true; return nil }); err != nil || !called {
		t.Fatalf("want commit within quota, err=%v called=%v", err, called)
	}
}
