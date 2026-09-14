package linux

import (
	"context"
	"os"
	"runtime"
	"strconv"
	"strings"

	"github.com/emergent-company/memory.web-ui/connector/internal/toolreg"
)

// newHostInfoTool builds the linux-host-info tool. It reports host identity and
// resource usage only: it never reads or returns environment variables.
func newHostInfoTool() toolreg.Tool {
	return toolreg.Tool{
		Name:        ToolHostInfo,
		Description: "Report host identity and resource usage: hostname, OS, kernel version, uptime, disk usage, and memory usage. Does not expose environment variables or credentials.",
		InputSchema: map[string]any{
			"type":       "object",
			"properties": map[string]any{},
		},
		Handler: func(_ context.Context, _ map[string]any) (map[string]any, error) {
			hostname, err := os.Hostname()
			if err != nil || hostname == "" {
				hostname = "unknown"
			}

			kernel := ""
			if b, err := os.ReadFile("/proc/sys/kernel/osrelease"); err == nil {
				kernel = strings.TrimSpace(string(b))
			}

			uptime := 0.0
			if b, err := os.ReadFile("/proc/uptime"); err == nil {
				uptime = parseUptime(string(b))
			}

			var memTotal, memAvailable uint64
			if b, err := os.ReadFile("/proc/meminfo"); err == nil {
				memTotal, memAvailable = parseMeminfo(string(b))
			}
			memUsed := uint64(0)
			if memTotal >= memAvailable {
				memUsed = memTotal - memAvailable
			}

			diskTotal, diskFree, diskUsed := diskUsage("/")

			return map[string]any{
				"hostname":       hostname,
				"os":             runtime.GOOS,
				"kernel":         kernel,
				"uptime_seconds": uptime,
				"disk": map[string]any{
					"path":        "/",
					"total_bytes": diskTotal,
					"free_bytes":  diskFree,
					"used_bytes":  diskUsed,
				},
				"memory": map[string]any{
					"total_bytes":     memTotal,
					"available_bytes": memAvailable,
					"used_bytes":      memUsed,
				},
			}, nil
		},
	}
}

// parseUptime returns the first field of /proc/uptime in seconds, or 0 when it
// cannot be parsed.
func parseUptime(data string) float64 {
	fields := strings.Fields(data)
	if len(fields) == 0 {
		return 0
	}
	seconds, err := strconv.ParseFloat(fields[0], 64)
	if err != nil || seconds < 0 {
		return 0
	}
	return seconds
}

// parseMeminfo extracts MemTotal and MemAvailable (falling back to MemFree)
// from /proc/meminfo, in bytes. Missing or malformed values yield zero.
func parseMeminfo(data string) (total, available uint64) {
	var memFree uint64
	for _, line := range strings.Split(data, "\n") {
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}
		value, err := strconv.ParseUint(fields[1], 10, 64)
		if err != nil {
			continue
		}
		value *= 1024 // /proc/meminfo values are in kB
		switch strings.TrimSuffix(fields[0], ":") {
		case "MemTotal":
			total = value
		case "MemAvailable":
			available = value
		case "MemFree":
			memFree = value
		}
	}
	if available == 0 {
		available = memFree
	}
	return total, available
}
