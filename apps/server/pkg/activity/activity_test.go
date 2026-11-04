package activity

import (
	"testing"
	"time"
)

func TestTierFor(t *testing.T) {
	now := time.Date(2026, 7, 12, 12, 0, 0, 0, time.UTC)
	ago := func(d time.Duration) *time.Time {
		t := now.Add(-d)
		return &t
	}
	cases := []struct {
		name   string
		last   *time.Time
		online bool
		want   string
	}{
		{"online wins", ago(90 * 24 * time.Hour), true, TierOnline},
		{"active now", ago(0), false, TierActive},
		{"active inside week", ago(6 * 24 * time.Hour), false, TierActive},
		{"occasional just over week", ago(8 * 24 * time.Hour), false, TierOccasional},
		{"occasional inside month", ago(29 * 24 * time.Hour), false, TierOccasional},
		{"dormant past month", ago(31 * 24 * time.Hour), false, TierDormant},
		{"never", nil, false, TierNever},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := TierFor(tc.last, tc.online, now); got != tc.want {
				t.Fatalf("TierFor = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestScoreBounds(t *testing.T) {
	if got := Score(0, 0, 0); got != 0 {
		t.Fatalf("zero activity score = %d, want 0", got)
	}
	low := Score(10, 1, 1)
	high := Score(500, 200, 25)
	if low >= high {
		t.Fatalf("score not monotonic: low=%d high=%d", low, high)
	}
	if high > 100 {
		t.Fatalf("score = %d, want <= 100", high)
	}
	if got := Score(1<<30, 1<<30, 1<<30); got != 100 {
		t.Fatalf("saturated score = %d, want 100", got)
	}
	if got := Score(-5, -5, -5); got != 0 {
		t.Fatalf("negative inputs score = %d, want 0", got)
	}
}
