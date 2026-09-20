//go:build windows

package update

import (
	"os"
	"syscall"
	"unsafe"
)

var (
	kernel32               = syscall.NewLazyDLL("kernel32.dll")
	procOpenProcess        = kernel32.NewProc("OpenProcess")
	procGetExitCodeProcess = kernel32.NewProc("GetExitCodeProcess")
	procCloseHandle        = kernel32.NewProc("CloseHandle")
)

const (
	processQueryLimitedInformation = 0x1000
	stillActive                    = 259
)

func isProcessAlive(pid int) bool {
	if pid <= 0 {
		return false
	}
	if pid == os.Getpid() {
		return true
	}
	h, _, err := procOpenProcess.Call(uintptr(processQueryLimitedInformation), uintptr(0), uintptr(uint32(pid)))
	if h == 0 {
		// 打开进程失败，视为进程不存在或无权访问。
		if err != nil && err.Error() != "The operation completed successfully." {
			return false
		}
		return false
	}
	defer procCloseHandle.Call(h)
	var exitCode uint32
	r, _, _ := procGetExitCodeProcess.Call(h, uintptr(unsafe.Pointer(&exitCode)))
	if r == 0 {
		return true // 查询失败时保守地视为存活。
	}
	return exitCode == stillActive
}
