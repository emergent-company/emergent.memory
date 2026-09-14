//go:build linux

package linux

import "syscall"

// diskUsage returns total/free/used bytes for the filesystem containing path.
// On error it returns zeros (host info degrades gracefully).
func diskUsage(path string) (total, free, used uint64) {
	var stat syscall.Statfs_t
	if err := syscall.Statfs(path, &stat); err != nil {
		return 0, 0, 0
	}
	total = stat.Blocks * uint64(stat.Bsize)
	free = stat.Bavail * uint64(stat.Bsize)
	if total >= free {
		used = total - free
	}
	return total, free, used
}
