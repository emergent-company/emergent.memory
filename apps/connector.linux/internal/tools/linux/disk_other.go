//go:build !linux

package linux

// diskUsage is the portable fallback for non-Linux hosts: it reports zeros so
// the host-info tool degrades gracefully instead of failing to build.
func diskUsage(_ string) (total, free, used uint64) {
	return 0, 0, 0
}
