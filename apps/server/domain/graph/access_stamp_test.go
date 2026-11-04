package graph

import (
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestAccessStampDebouncerFilter(t *testing.T) {
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	d := newAccessStampDebouncerWithClock(func() time.Time { return now }, time.Minute)

	a, b, c := uuid.New(), uuid.New(), uuid.New()

	// First pass: all three are new, so all three are written.
	if got := d.filter([]uuid.UUID{a, b, c}); len(got) != 3 {
		t.Fatalf("first filter wrote %d ids, want 3", len(got))
	}

	// Within the same window, already-seen ids are suppressed; only the new id
	// passes through.
	dd := uuid.New()
	got := d.filter([]uuid.UUID{a, b, dd})
	if len(got) != 1 || got[0] != dd {
		t.Fatalf("second filter = %v, want only %s", got, dd)
	}

	// After the window elapses, a previously-seen id is eligible again.
	now = now.Add(time.Minute + time.Second)
	got = d.filter([]uuid.UUID{a})
	if len(got) != 1 || got[0] != a {
		t.Fatalf("expired filter = %v, want %s", got, a)
	}

	// Empty input never writes and never panics.
	if got := d.filter(nil); len(got) != 0 {
		t.Fatalf("nil filter = %v, want empty", got)
	}
}
