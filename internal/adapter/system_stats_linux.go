//go:build linux

package adapter

import (
	"os"
	"strconv"
	"strings"
	"syscall"
)

func processRSSBytes() int64 {
	raw, err := os.ReadFile("/proc/self/statm")
	if err != nil {
		return 0
	}
	fields := strings.Fields(string(raw))
	if len(fields) < 2 {
		return 0
	}
	pages, err := strconv.ParseInt(fields[1], 10, 64)
	if err != nil {
		return 0
	}
	return pages * int64(os.Getpagesize())
}

func filesystemUsage(path string) (uint64, uint64) {
	var stats syscall.Statfs_t
	if err := syscall.Statfs(path, &stats); err != nil {
		return 0, 0
	}
	return stats.Blocks * uint64(stats.Bsize), stats.Bavail * uint64(stats.Bsize)
}
