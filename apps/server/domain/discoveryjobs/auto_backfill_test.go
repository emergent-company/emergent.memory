package discoveryjobs

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/emergent-company/emergent.memory/domain/extraction"
	"github.com/emergent-company/emergent.memory/domain/pipeline"
)

type fakeBackfillTrigger struct {
	calls  []extraction.BackfillFilter
	limits []int
}

func (f *fakeBackfillTrigger) Backfill(ctx context.Context, projectID string, filter extraction.BackfillFilter, limit int, dryRun bool) (*extraction.BackfillResult, error) {
	f.calls = append(f.calls, filter)
	f.limits = append(f.limits, limit)
	return &extraction.BackfillResult{Filter: filter, Selected: 1, Enqueued: 1}, nil
}

// TestMaybeAutoBackfillGating pins the re-extraction trigger: off (or empty)
// never calls backfill, incremental maps to stale_schema, full maps to all, and
// a nil trigger is a no-op.
func TestMaybeAutoBackfillGating(t *testing.T) {
	projectID := uuid.New()

	cases := []struct {
		name   string
		mode   string
		want   extraction.BackfillFilter
		called bool
	}{
		{"off", pipeline.ReExtractOff, "", false},
		{"empty", "", "", false},
		{"incremental", pipeline.ReExtractIncremental, extraction.BackfillStaleSchema, true},
		{"full", pipeline.ReExtractFull, extraction.BackfillAll, true},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			trig := &fakeBackfillTrigger{}
			svc := &Service{log: quietLogger(), backfill: trig}
			pol := pipeline.DefaultPolicy(pipeline.ModeAuto)
			pol.Extract.ReExtractOnSchemaChange = tc.mode

			svc.maybeAutoBackfill(context.Background(), projectID, pol)

			if !tc.called {
				require.Empty(t, trig.calls, "backfill must not run for mode %q", tc.mode)
				return
			}
			require.Len(t, trig.calls, 1)
			require.Equal(t, tc.want, trig.calls[0])
		})
	}
}

// TestMaybeAutoBackfillNilTrigger pins that a service with no backfill trigger
// (manual finalize-only discovery) never re-extracts, even under a full policy.
func TestMaybeAutoBackfillNilTrigger(t *testing.T) {
	svc := &Service{log: quietLogger()}
	pol := pipeline.DefaultPolicy(pipeline.ModeAuto)
	pol.Extract.ReExtractOnSchemaChange = pipeline.ReExtractFull

	svc.maybeAutoBackfill(context.Background(), uuid.New(), pol)
	// No panic, no call: nothing to assert beyond reaching here without a
	// trigger.
}

// TestMaybeAutoBackfillLimitFallback pins that a zero backfill limit falls back
// to the default 50 rather than passing 0 through.
func TestMaybeAutoBackfillLimitFallback(t *testing.T) {
	trig := &fakeBackfillTrigger{}
	svc := &Service{log: quietLogger(), backfill: trig}
	pol := pipeline.DefaultPolicy(pipeline.ModeAuto)
	pol.Extract.ReExtractOnSchemaChange = pipeline.ReExtractIncremental
	pol.Extract.BackfillLimitPerRun = 0

	svc.maybeAutoBackfill(context.Background(), uuid.New(), pol)

	require.Len(t, trig.limits, 1)
	require.Equal(t, 50, trig.limits[0])
}
