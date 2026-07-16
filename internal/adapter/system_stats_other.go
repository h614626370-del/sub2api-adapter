//go:build !linux

package adapter

func processRSSBytes() int64 {
	return 0
}

func filesystemUsage(string) (uint64, uint64) {
	return 0, 0
}
