package agents

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// TestClassifyQueuedRun pins the sweep's decision table without a database
// (issue #907). The candidate carries everything the sweep knows about one stale
// queued run: whether a parent/session owns it, whether a live job is claimable,
// whether the only live job sits on a disabled queue, and whether a worker ever
// claimed a job.
func TestClassifyQueuedRun(t *testing.T) {
	cases := []struct {
		name string
		c    queuedRunCandidate
		want queuedRunVerdict
	}{
		{
			name: "top-level run with a claimable job waits in line",
			c:    queuedRunCandidate{Claimable: true},
			want: queuedRunInLine,
		},
		{
			name: "owned run with a claimable job waits in line",
			c:    queuedRunCandidate{Owned: true, Claimable: true},
			want: queuedRunInLine,
		},
		{
			// A disabled queue is reversible operator intent and outranks both
			// the ownership rule and the "already executed" rule: re-enabling
			// must bring the backlog back.
			name: "disabled queue is spared",
			c:    queuedRunCandidate{Disabled: true},
			want: queuedRunSparedDisabled,
		},
		{
			name: "owned run parked on a disabled queue is spared",
			c:    queuedRunCandidate{Owned: true, Disabled: true, Claimed: true},
			want: queuedRunSparedDisabled,
		},
		{
			// Issue #907: a fan-out child or session turn with no live job that
			// a worker already claimed must not be re-enqueued.
			name: "owned run claimed by a worker is spared",
			c:    queuedRunCandidate{Owned: true, Claimed: true},
			want: queuedRunSparedExecuted,
		},
		{
			// The same shape without a claim never got as far as a worker, so
			// it is treated like a top-level run: heal or fail.
			name: "owned run never claimed is doomed",
			c:    queuedRunCandidate{Owned: true},
			want: queuedRunDoomed,
		},
		{
			name: "top-level run never claimed is doomed",
			c:    queuedRunCandidate{},
			want: queuedRunDoomed,
		},
		{
			// The top-level rule is unchanged by #907: nothing owns the run's
			// lifecycle, so the claim ledger does not make the sweep spare it.
			name: "top-level run past a claim keeps the original rule",
			c:    queuedRunCandidate{Claimed: true},
			want: queuedRunDoomed,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			require.Equal(t, tc.want, classifyQueuedRun(tc.c))
		})
	}
}
