package linux

import (
	"context"
	"encoding/json"
	"runtime"
	"strings"
	"testing"
)

func TestHostInfoFieldsPopulated(t *testing.T) {
	tool := newHostInfoTool()
	res, err := tool.Handler(context.Background(), nil)
	if err != nil {
		t.Fatalf("Handler: %v", err)
	}
	if hostname, _ := res["hostname"].(string); hostname == "" {
		t.Errorf("hostname = %v, want non-empty", res["hostname"])
	}
	if res["os"] != runtime.GOOS {
		t.Errorf("os = %v, want %q", res["os"], runtime.GOOS)
	}
	if _, ok := res["uptime_seconds"].(float64); !ok {
		t.Errorf("uptime_seconds = %v (%T), want float64", res["uptime_seconds"], res["uptime_seconds"])
	}
	disk, ok := res["disk"].(map[string]any)
	if !ok {
		t.Fatalf("disk = %T, want map", res["disk"])
	}
	for _, key := range []string{"path", "total_bytes", "free_bytes", "used_bytes"} {
		if _, ok := disk[key]; !ok {
			t.Errorf("disk missing %q", key)
		}
	}
	memory, ok := res["memory"].(map[string]any)
	if !ok {
		t.Fatalf("memory = %T, want map", res["memory"])
	}
	for _, key := range []string{"total_bytes", "available_bytes", "used_bytes"} {
		if _, ok := memory[key]; !ok {
			t.Errorf("memory missing %q", key)
		}
	}
}

func TestHostInfoNeverLeaksEnvironment(t *testing.T) {
	const secret = "connector-test-secret-value"
	t.Setenv("CONNECTOR_TEST_SECRET", secret)

	res, err := newHostInfoTool().Handler(context.Background(), nil)
	if err != nil {
		t.Fatalf("Handler: %v", err)
	}
	encoded, err := json.Marshal(res)
	if err != nil {
		t.Fatalf("marshal result: %v", err)
	}
	if strings.Contains(string(encoded), secret) {
		t.Errorf("host info output leaked an environment variable:\n%s", encoded)
	}
	if _, ok := res["env"]; ok {
		t.Errorf("host info result must not include an env field")
	}
}

func TestParseUptime(t *testing.T) {
	if got := parseUptime("123.45 67.89\n"); got != 123.45 {
		t.Errorf("parseUptime = %v, want 123.45", got)
	}
	if got := parseUptime("garbage"); got != 0 {
		t.Errorf("parseUptime(garbage) = %v, want 0", got)
	}
	if got := parseUptime(""); got != 0 {
		t.Errorf("parseUptime(empty) = %v, want 0", got)
	}
}

func TestParseMeminfo(t *testing.T) {
	data := "MemTotal:       16384 kB\n" +
		"MemFree:         1024 kB\n" +
		"MemAvailable:    8192 kB\n" +
		"Buffers:          100 kB\n"
	total, available := parseMeminfo(data)
	if total != 16384*1024 {
		t.Errorf("total = %d, want %d", total, 16384*1024)
	}
	if available != 8192*1024 {
		t.Errorf("available = %d, want %d", available, 8192*1024)
	}
}

func TestParseMeminfoFallsBackToMemFree(t *testing.T) {
	data := "MemTotal:       1000 kB\n" +
		"MemFree:         400 kB\n"
	total, available := parseMeminfo(data)
	if total != 1000*1024 {
		t.Errorf("total = %d, want %d", total, 1000*1024)
	}
	if available != 400*1024 {
		t.Errorf("available = %d, want %d (MemFree fallback)", available, 400*1024)
	}
}

func TestParseMeminfoMalformed(t *testing.T) {
	total, available := parseMeminfo("MemTotal: not-a-number kB\nMemFree:\n")
	if total != 0 || available != 0 {
		t.Errorf("parseMeminfo(malformed) = (%d, %d), want (0, 0)", total, available)
	}
}
