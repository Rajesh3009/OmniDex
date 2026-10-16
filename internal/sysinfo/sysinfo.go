package sysinfo

import (
	"runtime"
)

type MemoryStats struct {
	AllocBytes      uint64 `json:"alloc_bytes"`
	TotalAllocBytes uint64 `json:"total_alloc_bytes"`
	SysBytes        uint64 `json:"sys_bytes"`
	HeapInuseBytes  uint64 `json:"heap_inuse_bytes"`
	StackInuseBytes uint64 `json:"stack_inuse_bytes"`
	RSSBytes        uint64 `json:"rss_bytes"`
	NumGC           uint32 `json:"num_gc"`
	NumGoroutine    int    `json:"num_goroutine"`
}

func GetMemoryStats() MemoryStats {
	var m runtime.MemStats
	runtime.ReadMemStats(&m)

	stats := MemoryStats{
		AllocBytes:      m.Alloc,
		TotalAllocBytes: m.TotalAlloc,
		SysBytes:        m.Sys,
		HeapInuseBytes:  m.HeapInuse,
		StackInuseBytes: m.StackInuse,
		NumGC:           m.NumGC,
		NumGoroutine:    runtime.NumGoroutine(),
		RSSBytes:        getRSS(),
	}

	if stats.RSSBytes == 0 {
		stats.RSSBytes = stats.SysBytes
	}

	return stats
}
