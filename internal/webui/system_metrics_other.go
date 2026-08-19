//go:build !windows

package webui

import (
	"runtime"
	"runtime/metrics"
	"time"
)

func readCPUSample() cpuSample {
	samples := []metrics.Sample{{Name: "/cpu/classes/total:cpu-seconds"}, {Name: "/cpu/classes/idle:cpu-seconds"}}
	metrics.Read(samples)
	return cpuSample{total: uint64(samples[0].Value.Float64() * 1e9), idle: uint64(samples[1].Value.Float64() * 1e9), at: time.Now()}
}

func memoryUsage() (int64, int64) {
	var memory runtime.MemStats
	runtime.ReadMemStats(&memory)
	return int64(memory.HeapAlloc), int64(memory.Sys)
}

func diskUsage(string) (int64, int64) {
	return 0, 0
}

func cpuModelName() string { return "CPU" }
