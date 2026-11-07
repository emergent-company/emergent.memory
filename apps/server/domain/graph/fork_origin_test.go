package graph

import "testing"

// TestForkOrigin pins the fork path's origin vocabulary to the same four values
// branches.Service.Create accepts. Fork cannot import the branches package
// (cycle), so the set is repeated in forkOrigin and must not drift.
func TestForkOrigin(t *testing.T) {
	for _, origin := range []string{"extraction", "agent", "manual", "system"} {
		if got := forkOrigin(origin); got != origin {
			t.Errorf("forkOrigin(%q) = %q, want it preserved", origin, got)
		}
	}
	for _, origin := range []string{"", "  ", "Extraction", "scheduled"} {
		if got := forkOrigin(origin); got != "manual" {
			t.Errorf("forkOrigin(%q) = %q, want manual", origin, got)
		}
	}
}
