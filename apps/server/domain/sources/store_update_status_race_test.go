package sources

import (
	"context"
	"testing"
)

// TestUpdate_DoesNotResurrectAConcurrentlyParkedSource pins the invariant #2437
// asks for: a settings edit that does not mention `enabled` never moves a source
// out of `needs_reconnect`, regardless of the order the two writes land in.
//
// The edit reorders itself exactly as the race does: Service.Update reads the row
// while it is still active, a failing sync parks it, and only then does the
// edit's UPDATE land with the snapshot in hand. On the old code that UPDATE
// rewrote `status` from the snapshot (`active`) and its error-clearing CASE also
// dropped the parking failure, resurrecting a source the scheduler must keep out
// of the sweep — the every-cadence failed-sync loop #2406/#2415 removed.
func TestUpdate_DoesNotResurrectAConcurrentlyParkedSource(t *testing.T) {
	ctx := context.Background()
	fx := newParkedSourceFixture(t)

	// The snapshot a settings save took before the sync failed.
	snapshot := mustGetSource(t, ctx, fx.repo, fx.projectID, fx.sourceID)
	snapshot.DisplayName = "Renamed while a sync was failing"

	// The sync fails between the read and the write and parks the source.
	if _, err := fx.repo.MarkSourceNeedsReconnect(ctx, fx.projectID, fx.sourceID, driveReconnectRequired, nil); err != nil {
		t.Fatalf("MarkSourceNeedsReconnect() error = %v", err)
	}

	// The edit lands with the stale, still-active snapshot.
	if _, err := fx.repo.Update(ctx, snapshot); err != nil {
		t.Fatalf("Update(stale snapshot) error = %v", err)
	}

	got := mustGetSource(t, ctx, fx.repo, fx.projectID, fx.sourceID)
	if got.DisplayName != snapshot.DisplayName {
		t.Errorf("DisplayName = %q, want the edit applied (%q)", got.DisplayName, snapshot.DisplayName)
	}
	if got.Status != SourceStatusNeedsReconnect {
		t.Errorf("Status = %q, want %q: a settings edit must not un-park a source", got.Status, SourceStatusNeedsReconnect)
	}
	if got.LastError == nil || *got.LastError != driveReconnectRequired {
		t.Errorf("LastError = %v, want the parking failure kept", got.LastError)
	}
	if got.ErrorCount != 1 {
		t.Errorf("ErrorCount = %d, want 1: the edit must not reset the failure count", got.ErrorCount)
	}
}
