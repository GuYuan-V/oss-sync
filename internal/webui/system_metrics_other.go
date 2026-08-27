//go:build !windows

package webui

import (
	"bufio"
	"os"
	"runtime"
	"runtime/metrics"
	"strconv"
	"strings"
	"time"

	"golang.org/x/sys/unix"
)

func readCPUSample() cpuSample {
	if s, ok := readProcStatSample(); ok {
		return s
	}
	samples := []metrics.Sample{{Name: "/cpu/classes/total:cpu-seconds"}, {Name: "/cpu/classes/idle:cpu-seconds"}}
	metrics.Read(samples)
	return cpuSample{total: uint64(samples[0].Value.Float64() * 1e9), idle: uint64(samples[1].Value.Float64() * 1e9), at: time.Now()}
}

func readProcStatSample() (cpuSample, bool) {
	f, err := os.Open("/proc/stat")
	if err != nil {
		return cpuSample{}, false
	}
	defer f.Close()
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := scanner.Text()
		if !strings.HasPrefix(line, "cpu ") {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) < 2 {
			return cpuSample{}, false
		}
		var total uint64
		var idle uint64
		for i, field := range fields[1:] {
			if i >= 8 {
				break // guest/guest_nice 已计入 user/nice，避免重复
			}
			val, err := strconv.ParseUint(field, 10, 64)
			if err != nil {
				return cpuSample{}, false
			}
			total += val
			// idle is the 4th field (index 3), iowait is 5th (index 4)
			if i == 3 || i == 4 {
				idle += val
			}
		}
		return cpuSample{total: total, idle: idle, at: time.Now()}, true
	}
	return cpuSample{}, false
}

func memoryUsage() (int64, int64) {
	if total, avail, ok := readProcMemInfo(); ok {
		used := total - avail
		if used < 0 {
			used = 0
		}
		return used, total
	}
	var memory runtime.MemStats
	runtime.ReadMemStats(&memory)
	return int64(memory.HeapAlloc), int64(memory.Sys)
}

func readProcMemInfo() (int64, int64, bool) {
	f, err := os.Open("/proc/meminfo")
	if err != nil {
		return 0, 0, false
	}
	defer f.Close()
	var total, available, free, buffers, cached int64
	var hasTotal, hasAvail bool
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := scanner.Text()
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}
		key := strings.TrimSuffix(fields[0], ":")
		val, err := strconv.ParseInt(fields[1], 10, 64)
		if err != nil {
			continue
		}
		// values are in kB
		valBytes := val * 1024
		switch key {
		case "MemTotal":
			total = valBytes
			hasTotal = true
		case "MemAvailable":
			available = valBytes
			hasAvail = true
		case "MemFree":
			free = valBytes
		case "Buffers":
			buffers = valBytes
		case "Cached":
			cached = valBytes
		}
	}
	if !hasTotal {
		return 0, 0, false
	}
	if !hasAvail {
		// fallback for old kernels without MemAvailable
		available = free + buffers + cached
	}
	if available > total {
		available = total
	}
	return total, available, true
}

func diskUsage(dataDir string) (int64, int64) {
	path := dataDir
	if path == "" {
		path = "/"
	}
	if _, err := os.Stat(path); err != nil {
		path = "/"
	}
	var stat unix.Statfs_t
	if err := unix.Statfs(path, &stat); err != nil {
		return 0, 0
	}
	total := int64(stat.Blocks) * int64(stat.Bsize)
	free := int64(stat.Bfree) * int64(stat.Bsize)
	if total <= 0 || total < free {
		return 0, 0
	}
	return total - free, total
}

func cpuModelName() string {
	f, err := os.Open("/proc/cpuinfo")
	if err != nil {
		return "CPU"
	}
	defer f.Close()
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := scanner.Text()
		if strings.HasPrefix(line, "model name") {
			parts := strings.SplitN(line, ":", 2)
			if len(parts) == 2 {
				name := strings.TrimSpace(parts[1])
				if name != "" {
					return name
				}
			}
		}
	}
	// ARM 等无 model name 时，回退仅看 Hardware，避免 processor/cpu part 的数值噪音。
	if _, err := f.Seek(0, 0); err == nil {
		scanner = bufio.NewScanner(f)
		for scanner.Scan() {
			line := scanner.Text()
			if strings.HasPrefix(strings.ToLower(line), "hardware") {
				parts := strings.SplitN(line, ":", 2)
				if len(parts) == 2 {
					name := strings.TrimSpace(parts[1])
					if name != "" {
						return name
					}
				}
			}
		}
	}
	return "CPU"
}
