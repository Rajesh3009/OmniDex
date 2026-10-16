//go:build !windows

package sysinfo

func getRSSAndPeak() (uint64, uint64) {
	return 0, 0
}
