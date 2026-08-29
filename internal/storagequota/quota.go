// Package storagequota applies the deployment-wide application storage limit.
package storagequota

import (
	"errors"
	"fmt"
	"io/fs"
	"math"
	"os"
	"path/filepath"
	"sync"
)

var (
	ErrExceeded = errors.New("project storage quota exceeded")
	writeMu     sync.Mutex
)

// Usage returns the bytes occupied by regular files below root.
func Usage(root string) (int64, error) {
	var used int64
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.Type().IsRegular() {
			info, err := entry.Info()
			if err != nil {
				return err
			}
			if info.Size() > math.MaxInt64-used {
				return fmt.Errorf("measure project storage: size overflow")
			}
			used += info.Size()
		}
		return nil
	})
	if errors.Is(err, os.ErrNotExist) {
		return 0, nil
	}
	return used, err
}

// WithinLimit serializes capacity checks and data-expanding commits.
// reserved covers files that the commit can create after the initial scan.
func WithinLimit(root string, limit, reserved int64, commit func() error) error {
	if limit <= 0 {
		return commit()
	}
	writeMu.Lock()
	defer writeMu.Unlock()
	used, err := Usage(root)
	if err != nil {
		return fmt.Errorf("measure project storage: %w", err)
	}
	if reserved < 0 || used > limit || reserved > limit-used {
		return fmt.Errorf("%w: used=%d reserved=%d limit=%d", ErrExceeded, used, reserved, limit)
	}
	return commit()
}
