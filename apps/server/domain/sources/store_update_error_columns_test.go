package sources

import (
	"context"
	"testing"
)

// TestUpdate_PreservesRecordedErrorWhenNotReactivating pins the other half of the
// enable-clears-the-error rule (#2421): the CASE in Repository.Update fires only
// when the row is inactive and the write makes it active, so a plain edit of an
// active source cannot wipe the failure it just recorded. Writing the error columns
// from the snapshot instead would let a rename race a failing sync and erase the
// live error.
func TestUpdate_PreservesRecordedErrorWhenNotReactivating(t *testing.T) {
	ctx := context.Background()
	fx := newParkedSourceFixture(t)

	t.Run("active source keeps its live error", func(t *testing.T) {
		interval := 60
		id := createParkedTestSource(t, ctx, fx.repo, fx.projectID, &interval)
		if err := fx.repo.RecordSourceError(ctx, fx.projectID, id, "google drive: 503 Service Unavailable"); err != nil {
			t.Fatalf("RecordSourceError() error = %v", err)
		}

		src := mustGetSource(t, ctx, fx.repo, fx.projectID, id)
		src.DisplayName = "Renamed while failing"
		if _, err := fx.repo.Update(ctx, src); err != nil {
			t.Fatalf("Update() error = %v", err)
		}

		got := mustGetSource(t, ctx, fx.repo, fx.projectID, id)
		if got.LastError == nil || *got.LastError != "google drive: 503 Service Unavailable" {
			t.Errorf("LastError = %v, want the live error preserved by a plain edit", got.LastError)
		}
		if got.ErrorCount != 1 {
			t.Errorf("ErrorCount = %d, want 1", got.ErrorCount)
		}
	})

	t.Run("parked source keeps the failure that parked it", func(t *testing.T) {
		if _, err := fx.repo.MarkSourceNeedsReconnect(ctx, fx.projectID, fx.sourceID, driveReconnectRequired, nil); err != nil {
			t.Fatalf("MarkSourceNeedsReconnect(, nil) error = %v", err)
		}

		parked := mustGetSource(t, ctx, fx.repo, fx.projectID, fx.sourceID)
		parked.DisplayName = "Renamed while parked"
		if _, err := fx.repo.Update(ctx, parked); err != nil {
			t.Fatalf("Update() error = %v", err)
		}

		got := mustGetSource(t, ctx, fx.repo, fx.projectID, fx.sourceID)
		if got.Status != SourceStatusNeedsReconnect {
			t.Errorf("Status = %q, want it still parked", got.Status)
		}
		if got.LastError == nil || *got.LastError != driveReconnectRequired {
			t.Errorf("LastError = %v, want the parking failure preserved", got.LastError)
		}
	})

	t.Run("pause keeps the error for when it is unparked", func(t *testing.T) {
		if err := fx.repo.UpdateStatus(ctx, fx.sourceID, fx.projectID, SourceStatusDisabled); err != nil {
			t.Fatalf("UpdateStatus(disabled) error = %v", err)
		}

		got := mustGetSource(t, ctx, fx.repo, fx.projectID, fx.sourceID)
		if got.Status != SourceStatusDisabled {
			t.Errorf("Status = %q, want it paused", got.Status)
		}
		if got.LastError == nil {
			t.Error("LastError = nil, want the failure kept while paused")
		}

		// The pause is lifted by an enable, which is the parked → active path: the
		// error that was kept is the one the retry is clearing.
		if err := fx.repo.UpdateStatus(ctx, fx.sourceID, fx.projectID, SourceStatusActive); err != nil {
			t.Fatalf("UpdateStatus(active) error = %v", err)
		}
		reactivated := mustGetSource(t, ctx, fx.repo, fx.projectID, fx.sourceID)
		if reactivated.LastError != nil {
			t.Errorf("LastError = %q, want it cleared once active again", *reactivated.LastError)
		}
	})
}
