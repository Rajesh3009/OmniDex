package sysinfo

import (
	"runtime"
	"runtime/debug"
	"sync"
)

type MemoryStats struct {
	AllocBytes      uint64 `json:"alloc_bytes"`
	TotalAllocBytes uint64 `json:"total_alloc_bytes"`
	SysBytes        uint64 `json:"sys_bytes"`
	HeapInuseBytes  uint64 `json:"heap_inuse_bytes"`
	StackInuseBytes uint64 `json:"stack_inuse_bytes"`
	RSSBytes        uint64 `json:"rss_bytes"`
	MinRSSBytes     uint64 `json:"min_rss_bytes"`
	MaxRSSBytes     uint64 `json:"max_rss_bytes"`
	PeakRSSBytes    uint64 `json:"peak_rss_bytes"`
	NumGC           uint32 `json:"num_gc"`
	NumGoroutine    int    `json:"num_goroutine"`
}

var (
	memMu  sync.Mutex
	minRSS uint64
	maxRSS uint64
)

func GetMemoryStats() MemoryStats {
	var m runtime.MemStats
	runtime.ReadMemStats(&m)

	rss, peak := getRSSAndPeak()
	if rss == 0 {
		rss = m.Sys
	}

	memMu.Lock()
	if minRSS == 0 || (rss > 0 && rss < minRSS) {
		minRSS = rss
	}
	if rss > maxRSS {
		maxRSS = rss
	}
	if peak > maxRSS {
		maxRSS = peak
	}
	currMin := minRSS
	currMax := maxRSS
	memMu.Unlock()

	return MemoryStats{
		AllocBytes:      m.Alloc,
		TotalAllocBytes: m.TotalAlloc,
		SysBytes:        m.Sys,
		HeapInuseBytes:  m.HeapInuse,
		StackInuseBytes: m.StackInuse,
		RSSBytes:        rss,
		MinRSSBytes:     currMin,
		MaxRSSBytes:     currMax,
		PeakRSSBytes:    peak,
		NumGC:           m.NumGC,
		NumGoroutine:    runtime.NumGoroutine(),
	}
}

// FreeMemory manually triggers garbage collection and instructs the Go runtime
// to release idle virtual memory pages back to the operating system.
func FreeMemory() {
	runtime.GC()
	debug.FreeOSMemory()
}
