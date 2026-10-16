//go:build windows

package sysinfo

import (
	"unsafe"

	"golang.org/x/sys/windows"
)

type processMemoryCounters struct {
	CB                         uint32
	PageFaultCount             uint32
	PeakWorkingSetSize         uintptr
	WorkingSetSize             uintptr
	QuotaPeakPagedPoolUsage    uintptr
	QuotaPagedPoolUsage        uintptr
	QuotaPeakNonPagedPoolUsage uintptr
	QuotaNonPagedPoolUsage     uintptr
	PagefileUsage              uintptr
	PeakPagefileUsage          uintptr
}

func getRSSAndPeak() (uint64, uint64) {
	var mem processMemoryCounters
	mem.CB = uint32(unsafe.Sizeof(mem))
	handle := windows.CurrentProcess()
	modpsapi := windows.NewLazySystemDLL("psapi.dll")
	procGetProcessMemoryInfo := modpsapi.NewProc("GetProcessMemoryInfo")
	r1, _, _ := procGetProcessMemoryInfo.Call(
		uintptr(handle),
		uintptr(unsafe.Pointer(&mem)),
		uintptr(mem.CB),
	)
	if r1 != 0 {
		return uint64(mem.WorkingSetSize), uint64(mem.PeakWorkingSetSize)
	}
	return 0, 0
}
