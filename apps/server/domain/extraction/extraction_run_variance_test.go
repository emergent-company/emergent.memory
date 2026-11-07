package extraction

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

// fakePreviousRunFinder returns a fixed snapshot (or error) so the variance
// path is exercised without a database or the jobs service.
type fakePreviousRunFinder struct {
	snapshot *RunCountSnapshot
	err      error
}

func (f fakePreviousRunFinder) PreviousCompletedRunForDocument(context.Context, *ObjectExtractionJob) (*RunCountSnapshot, error) {
	return f.snapshot, f.err
}

// TestPlanRunVariance_WarnsWhenNewCountIsThreeTimesSmaller is the issue's own
// case: 213 objects, then 71. Three times smaller is over the band, so the run
// must carry a warning naming both counts.
func TestPlanRunVariance_WarnsWhenNewCountIsThreeTimesSmaller(t *testing.T) {
	warning := planRunVariance(&RunCountSnapshot{JobID: "job-old", ObjectsCreated: 213}, 71)

	require.NotNil(t, warning, "3× smaller must warn")
	require.Equal(t, "job-old", warning.PreviousJobID)
	require.Equal(t, 213, warning.PreviousObjects)
	require.Equal(t, 71, warning.Objects)
	require.Equal(t, "smaller", warning.Direction)
	require.Equal(t, 3.0, warning.Ratio)
	require.Contains(t, warning.Message, "71 objects")
	require.Contains(t, warning.Message, "213 objects")
}

// TestPlanRunVariance_NoWarningWithinBand holds the noise budget: a run may
// differ from its predecessor by up to 2× without being flagged, because model
// output legitimately varies.
func TestPlanRunVariance_NoWarningWithinBand(t *testing.T) {
	for _, tc := range []struct {
		name     string
		previous int
		objects  int
	}{
		{name: "same", previous: 120, objects: 120},
		{name: "slightly fewer", previous: 120, objects: 95},
		{name: "slightly more", previous: 120, objects: 150},
		{name: "almost double", previous: 120, objects: 239},
	} {
		t.Run(tc.name, func(t *testing.T) {
			require.Nil(t, planRunVariance(&RunCountSnapshot{JobID: "job-old", ObjectsCreated: tc.previous}, tc.objects))
		})
	}
}

// TestPlanRunVariance_NoWarningWithoutPreviousRun: the first run of a document
// has nothing to compare against and must never warn.
func TestPlanRunVariance_NoWarningWithoutPreviousRun(t *testing.T) {
	require.Nil(t, planRunVariance(nil, 235))
}

// TestPlanRunVariance_NoWarningBelowFloor keeps tiny runs quiet: 3 objects
// becoming 1 is a 3× drop but not the signal this check exists for.
func TestPlanRunVariance_NoWarningBelowFloor(t *testing.T) {
	require.Nil(t, planRunVariance(&RunCountSnapshot{JobID: "job-old", ObjectsCreated: 8}, 2))
	require.Nil(t, planRunVariance(&RunCountSnapshot{JobID: "job-old", ObjectsCreated: 3}, 1))
	require.Nil(t, planRunVariance(&RunCountSnapshot{JobID: "job-old", ObjectsCreated: 0}, 9))
}

// TestPlanRunVariance_WarnsOnLargeIncrease covers the other direction: a run
// that produces far more than the previous one is as worth flagging as one
// that produces far less.
func TestPlanRunVariance_WarnsOnLargeIncrease(t *testing.T) {
	warning := planRunVariance(&RunCountSnapshot{JobID: "job-old", ObjectsCreated: 71}, 235)

	require.NotNil(t, warning)
	require.Equal(t, "larger", warning.Direction)
	require.Equal(t, 71, warning.PreviousObjects)
	require.Equal(t, 235, warning.Objects)
}

// TestPlanRunVariance_WarnsAtTheBandEdge documents the threshold boundary: the
// band is "more than 2×", so exactly 2× already warns.
func TestPlanRunVariance_WarnsAtTheBandEdge(t *testing.T) {
	require.NotNil(t, planRunVariance(&RunCountSnapshot{JobID: "job-old", ObjectsCreated: 50}, 100))
	require.NotNil(t, planRunVariance(&RunCountSnapshot{JobID: "job-old", ObjectsCreated: 100}, 50))
}

// TestApplyRunVariance_RecordsWarningOnResultAndNotification proves the warning
// is actually attached where the operator can see it: the job result
// (debug_info), the run notification message and its severity.
func TestApplyRunVariance_RecordsWarningOnResultAndNotification(t *testing.T) {
	notifier := &fakeProjectNotifier{}
	w := testWorker(notifier)
	w.previousRuns = fakePreviousRunFinder{snapshot: &RunCountSnapshot{JobID: "prev-job", ObjectsCreated: 213}}

	job := &ObjectExtractionJob{ID: "job-new", ProjectID: "prj"}
	result := &ObjectExtractionResults{ObjectsCreated: 71, RelationshipsCreated: 40}

	warning := w.applyRunVariance(context.Background(), job, "", result)
	require.NotNil(t, warning)
	require.Same(t, warning, result.VarianceWarning)
	require.Contains(t, result.DebugInfo, "variance_warning")

	// The review-ready path is the one a user sees before merging.
	branch := uuid.New()
	w.emitExtractionOutcome(context.Background(), job, &branch, result)

	require.Len(t, notifier.calls, 1)
	call := notifier.calls[0]
	require.Equal(t, "warning", call.input.Severity, "a deviating run must not present as an ordinary info row")
	require.Contains(t, call.input.Message, "71 objects")
	require.Contains(t, call.input.Message, "213 objects")
	require.Contains(t, call.input.Title, "213")

	details := decodeDetails(t, call.input.Details)
	require.Equal(t, float64(213), details["previous_objects"])
	require.Equal(t, "prev-job", details["previous_job_id"])
	require.Equal(t, "smaller", details["variance_direction"])
}

// TestApplyRunVariance_NoWarningWithinBand: a comparable run records nothing
// and leaves the notification at info severity.
func TestApplyRunVariance_NoWarningWithinBand(t *testing.T) {
	notifier := &fakeProjectNotifier{}
	w := testWorker(notifier)
	w.previousRuns = fakePreviousRunFinder{snapshot: &RunCountSnapshot{JobID: "prev-job", ObjectsCreated: 100}}

	job := &ObjectExtractionJob{ID: "job-new", ProjectID: "prj"}
	result := &ObjectExtractionResults{ObjectsCreated: 120}

	require.Nil(t, w.applyRunVariance(context.Background(), job, "", result))
	require.Nil(t, result.VarianceWarning)
	require.NotContains(t, result.DebugInfo, "variance_warning")

	branch := uuid.New()
	w.emitExtractionOutcome(context.Background(), job, &branch, result)
	require.Len(t, notifier.calls, 1)
	require.Equal(t, "info", notifier.calls[0].input.Severity)
}

// TestApplyRunVariance_NoPreviousRun: nothing to compare, nothing to warn.
func TestApplyRunVariance_NoPreviousRun(t *testing.T) {
	w := testWorker(&fakeProjectNotifier{})
	w.previousRuns = fakePreviousRunFinder{snapshot: nil}

	result := &ObjectExtractionResults{ObjectsCreated: 235}
	require.Nil(t, w.applyRunVariance(context.Background(), &ObjectExtractionJob{ID: "job-new", ProjectID: "prj"}, "", result))
	require.Nil(t, result.VarianceWarning)
	require.NotContains(t, result.DebugInfo, "variance_warning")
}

// TestApplyRunVariance_LookupErrorDoesNotFailTheRun: a comparison failure is
// diagnostics, not an extraction failure.
func TestApplyRunVariance_LookupErrorDoesNotFailTheRun(t *testing.T) {
	w := testWorker(&fakeProjectNotifier{})
	w.previousRuns = fakePreviousRunFinder{err: errors.New("db down")}

	result := &ObjectExtractionResults{ObjectsCreated: 235}
	require.NotPanics(t, func() {
		require.Nil(t, w.applyRunVariance(context.Background(), &ObjectExtractionJob{ID: "job-new", ProjectID: "prj"}, "", result))
	})
}

// TestWithVarianceWarning_AppendsBothCounts pins the helper both outcome
// producers use: warning severity, no warning → untouched.
func TestWithVarianceWarning_AppendsBothCounts(t *testing.T) {
	message, severity := withVarianceWarning("file.docx · 71 objects", "info", map[string]any{},
		&RunVarianceWarning{PreviousObjects: 213, Objects: 71, Direction: "smaller", Ratio: 3, Message: "71 vs 213"})

	require.Equal(t, "warning", severity)
	require.Contains(t, message, "file.docx")
	require.Contains(t, message, "71 vs 213")

	message, severity = withVarianceWarning("unchanged", "info", map[string]any{}, nil)
	require.Equal(t, "info", severity)
	require.Equal(t, "unchanged", message)
}
