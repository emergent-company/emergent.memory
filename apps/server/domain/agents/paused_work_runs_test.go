package agents

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// TestAbandonedPausedWorkRunBlockReason asserts the block reason reports the
// condition that parked the run (issue #993): a step-limit park names the budget
// and the steps used, so an operator can see why the item stopped instead of
// finding it untouched in its pickup status.
func TestAbandonedPausedWorkRunBlockReason(t *testing.T) {
	limit := 120
	tests := []struct {
		name       string
		candidate  *pausedWorkRunCandidate
		wantSubstr []string
	}{
		{
			name: "step limit with max steps",
			candidate: &pausedWorkRunCandidate{
				RunID:       "run-1",
				StepCount:   120,
				MaxSteps:    &limit,
				PauseReason: string(SuspendReasonStepLimit),
			},
			wantSubstr: []string{"run-1", "step limit", "120/120", "not resumed"},
		},
		{
			name: "step limit without recorded max steps",
			candidate: &pausedWorkRunCandidate{
				RunID:       "run-2",
				StepCount:   30,
				PauseReason: string(SuspendReasonStepLimit),
			},
			wantSubstr: []string{"run-2", "step limit", "after 30 steps"},
		},
		{
			name: "unidentified pause still reports why",
			candidate: &pausedWorkRunCandidate{
				RunID:     "run-3",
				StepCount: 2,
			},
			wantSubstr: []string{"run-3", "nothing pending", "not resumed"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			reason := abandonedPausedWorkRunBlockReason(tt.candidate, 30*time.Minute)
			for _, want := range tt.wantSubstr {
				require.Contains(t, reason, want)
			}
		})
	}
}

// TestListAbandonedPausedWorkRunsRejectsNonPositiveThreshold guards the sweep's
// safety contract: an unconditional query would reap every paused work run,
// including one a human is about to answer.
func TestListAbandonedPausedWorkRunsRejectsNonPositiveThreshold(t *testing.T) {
	repo := NewRepository(nil)
	for _, olderThan := range []time.Duration{0, -time.Minute} {
		_, err := repo.ListAbandonedPausedWorkRuns(t.Context(), olderThan, 10)
		require.Error(t, err)
	}
}
