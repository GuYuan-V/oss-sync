//go:build windows

package webui

import (
	"path/filepath"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/registry"
)

var getSystemTimes = windows.NewLazySystemDLL("kernel32.dll").NewProc("GetSystemTimes")
var globalMemoryStatusEx = windows.NewLazySystemDLL("kernel32.dll").NewProc("GlobalMemoryStatusEx")

type memoryStatusEx struct {
	Length               uint32
	MemoryLoad           uint32
	TotalPhys            uint64
	AvailPhys            uint64
	TotalPageFile        uint64
	AvailPageFile        uint64
	TotalVirtual         uint64
	AvailVirtual         uint64
	AvailExtendedVirtual uint64
}

func cpuModelName() string {
	key, err := registry.OpenKey(registry.LOCAL_MACHINE, `HARDWARE\DESCRIPTION\System\CentralProcessor\0`, registry.QUERY_VALUE)
	if err != nil {
		return "CPU"
	}
	defer key.Close()
	name, _, err := key.GetStringValue("ProcessorNameString")
	if err != nil || name == "" {
		return "CPU"
	}
	return name
}

func readCPUSample() cpuSample {
	var idle, kernel, user windows.Filetime
	result, _, _ := getSystemTimes.Call(
		uintptr(unsafe.Pointer(&idle)),
		uintptr(unsafe.Pointer(&kernel)),
		uintptr(unsafe.Pointer(&user)),
	)
	if result == 0 {
		return cpuSample{}
	}
	return cpuSample{
		total: filetimeTicks(kernel) + filetimeTicks(user),
		idle:  filetimeTicks(idle),
		at:    time.Now(),
	}
}

func filetimeTicks(value windows.Filetime) uint64 {
	return uint64(value.HighDateTime)<<32 | uint64(value.LowDateTime)
}

func memoryUsage() (int64, int64) {
	status := memoryStatusEx{Length: uint32(unsafe.Sizeof(memoryStatusEx{}))}
	result, _, _ := globalMemoryStatusEx.Call(uintptr(unsafe.Pointer(&status)))
	if result == 0 || status.TotalPhys < status.AvailPhys {
		return 0, 0
	}
	return int64(status.TotalPhys - status.AvailPhys), int64(status.TotalPhys)
}

func diskUsage(dataDir string) (int64, int64) {
	root := filepath.VolumeName(dataDir) + `\`
	path, err := windows.UTF16PtrFromString(root)
	if err != nil {
		return 0, 0
	}
	var available, total, free uint64
	if err := windows.GetDiskFreeSpaceEx(path, &available, &total, &free); err != nil || total < free {
		return 0, 0
	}
	return int64(total - free), int64(total)
}
