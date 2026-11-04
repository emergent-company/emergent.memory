package main

import (
	"testing"

	ui "github.com/emergent-company/go-daisy/components/ui"
)

// TestDsStatus_ParkedSourceIsLabelled pins the connections table's rendering of
// the parked lifecycle state: falling through to the default would print the raw
// `needs_reconnect` snake_case next to a neutral badge, which reads like an
// internal detail rather than "this source needs you to reconnect it".
func TestDsStatus_ParkedSourceIsLabelled(t *testing.T) {
	if got := dsStatusLabel("needs_reconnect"); got != "Needs reconnect" {
		t.Errorf("dsStatusLabel(needs_reconnect) = %q, want %q", got, "Needs reconnect")
	}
	if got := dsStatusIntent("needs_reconnect"); got != ui.BadgeError {
		t.Errorf("dsStatusIntent(needs_reconnect) = %v, want the error intent", got)
	}
	// The other states must not be disturbed by the new case.
	if got := dsStatusLabel("active"); got != "Active" {
		t.Errorf("dsStatusLabel(active) = %q, want %q", got, "Active")
	}
	if got := dsStatusIntent("active"); got != ui.BadgeSuccess {
		t.Errorf("dsStatusIntent(active) = %v, want the success intent", got)
	}
}
