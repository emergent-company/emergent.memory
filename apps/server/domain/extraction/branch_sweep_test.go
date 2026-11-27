package extraction

import (
	"testing"
	"time"
)

func TestEnvIntClamps(t *testing.T) {
	cases := []struct {
		name string
		env  string
		def  int
		min  int
		max  int
		want int
	}{
		{"unset", "", 30, 1, 100000, 30},
		{"valid", "15", 30, 1, 100000, 15},
		{"below min", "0", 30, 1, 100000, 30},
		{"negative", "-5", 30, 1, 100000, 30},
		{"above max", "999999999999", 30, 1, 100000, 30},
		{"not a number", "abc", 30, 1, 100000, 30},
		{"retention zero allowed", "0", 7, 0, 10000, 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if tc.env != "" {
				t.Setenv("TEST_ENV_INT", tc.env)
			} else {
				t.Setenv("TEST_ENV_INT", "")
			}
			// envInt reads a fixed name; test it via a tiny indirection.
			if got := envInt("TEST_ENV_INT", tc.def, tc.min, tc.max); got != tc.want {
				t.Fatalf("envInt(%q) = %d, want %d", tc.env, got, tc.want)
			}
		})
	}
}

// TestDefaultBranchSweepConfigClamps pins that a zero/negative/oversized interval
// falls back to the default (never a zero duration, which would panic
// time.NewTicker), while retention zero is a valid "disabled" value.
func TestDefaultBranchSweepConfigClamps(t *testing.T) {
	t.Setenv("BRANCH_SWEEP_INTERVAL_MINUTES", "0")
	t.Setenv("BRANCH_SWEEP_RETENTION_DAYS", "0")
	cfg := DefaultBranchSweepConfig()
	if cfg.Interval != 30*time.Minute {
		t.Fatalf("interval = %v, want default 30m (zero must not be allowed)", cfg.Interval)
	}
	if cfg.Retention != 0 {
		t.Fatalf("retention = %v, want 0 (disabled is a valid value)", cfg.Retention)
	}

	t.Setenv("BRANCH_SWEEP_INTERVAL_MINUTES", "999999999999")
	cfg = DefaultBranchSweepConfig()
	if cfg.Interval != 30*time.Minute {
		t.Fatalf("interval = %v, want default 30m (overflow must not be allowed)", cfg.Interval)
	}
}
