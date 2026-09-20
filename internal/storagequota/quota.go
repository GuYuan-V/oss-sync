// Package storagequota 执行部署级应用存储容量上限。
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

// Usage 返回 root 下常规文件占用的字节数。
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

// WithinLimit 串行化容量检查与扩容提交，避免并发超限。
// 其中 reserved 为提交后新增文件预留的字节数。
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
