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
		// OpenProcess failed – process does not exist or access denied
		if err != nil && err.Error() != "The operation completed successfully." {
			return false
		}
		return false
	}
	defer procCloseHandle.Call(h)
	var exitCode uint32
	r, _, _ := procGetExitCodeProcess.Call(h, uintptr(unsafe.Pointer(&exitCode)))
	if r == 0 {
		return true // conservative: assume alive if can't query
	}
	return exitCode == stillActive
}
