//go:build !windows

package update

import (
	"os"
	"syscall"
)

func isProcessAlive(pid int) bool {
	if pid <= 0 {
		return false
	}
	if pid == os.Getpid() {
		return true
	}
	proc, err := os.FindProcess(pid)
	if err != nil {
		return false
	}
	err = proc.Signal(syscall.Signal(0))
	if err == nil {
		return true
	}
	// 进程不存在时返回假，无权限时同样按不存在处理
	if err.Error() == "os: process already finished" {
		return false
	}
	return false
}
