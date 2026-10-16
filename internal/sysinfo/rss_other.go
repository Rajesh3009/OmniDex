//go:build !windows

package sysinfo

func getRSS() uint64 {
	return 0
}
