package agents

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestLostResumeClaimError pins the verdict a lost resume claim produces for
// every state the pause can be left in (issue #912). `ErrRunAlreadyResumed` is a
// statement that another caller owns the turn; only states that can still mean
// that (a live pause, or a checkpoint the claim already finalized as success)
// may produce it. A pause that went terminal for another reason must not.
func TestLostResumeClaimError(t *testing.T) {
	benign := []AgentRunStatus{
		RunStatusPaused, // still claimable / claim in flight
		RunStatusQueued,
		RunStatusRunning,
		RunStatusSuccess, // the claim's own input-required → completed transition
	}
	for _, status := range benign {
		t.Run(string(status)+"_is_already_resumed", func(t *testing.T) {
			err := lostResumeClaimError(&AgentRun{ID: "run-1", Status: status}, "run-1")
			require.ErrorIs(t, err, ErrRunAlreadyResumed)
			require.NotErrorIs(t, err, ErrRunPauseNotResumable)
			assert.Contains(t, err.Error(), "run-1")
		})
	}

	terminal := []AgentRunStatus{
		RunStatusCancelled,
		RunStatusCancelling,
		RunStatusError,
		RunStatusSkipped,
	}
	for _, status := range terminal {
		t.Run(string(status)+"_is_not_resumable", func(t *testing.T) {
			err := lostResumeClaimError(&AgentRun{ID: "run-1", Status: status}, "run-1")
			require.ErrorIs(t, err, ErrRunPauseNotResumable)
			require.NotErrorIs(t, err, ErrRunAlreadyResumed,
				"a terminal pause must never be reported as another caller running the turn")
			assert.Contains(t, err.Error(), string(status),
				"the rejection must name the run's real status")
			assert.NotContains(t, err.Error(), "already resumed")
		})
	}

	t.Run("missing_run_is_not_resumable", func(t *testing.T) {
		err := lostResumeClaimError(nil, "run-gone")
		require.ErrorIs(t, err, ErrRunPauseNotResumable)
		require.NotErrorIs(t, err, ErrRunAlreadyResumed)
		assert.Contains(t, err.Error(), "run-gone")
	})
}

// TestSpawnResumeFailure pins how spawn_agents projects a resume_run_id spawn
// whose Resume lost the claim (issue #912). The two lost-claim reasons must not
// share a card state: a genuine concurrent claim leaves the sub-agent `running`
// under that claim, while a pause that went terminal can never execute a turn
// and must be projected as failed.
func TestSpawnResumeFailure(t *testing.T) {
	req := SpawnRequest{AgentName: "researcher", Task: "dig"}

	t.Run("genuine_concurrent_claim_stays_running", func(t *testing.T) {
		var got []SubagentUpdate
		deps := &CoordinationToolDeps{
			ParentRunID:  "parent-1",
			EmitSubagent: func(u SubagentUpdate) { got = append(got, u) },
		}

		res := spawnResumeFailure(deps, req, "run-1", "spawn-synthetic",
			lostResumeClaimError(&AgentRun{ID: "run-1", Status: RunStatusSuccess}, "run-1"))

		assert.Equal(t, RunStatusRunning, res.Status)
		assert.Equal(t, "run-1", res.RunID)
		assert.Contains(t, res.Summary["note"], "already claimed by another caller")
		require.Len(t, got, 1)
		assert.Equal(t, "running", got[0].Status)
		assert.Empty(t, got[0].Error)
	})

	t.Run("terminal_pause_is_failed", func(t *testing.T) {
		var got []SubagentUpdate
		deps := &CoordinationToolDeps{
			ParentRunID:  "parent-1",
			EmitSubagent: func(u SubagentUpdate) { got = append(got, u) },
		}

		claimErr := lostResumeClaimError(&AgentRun{ID: "run-1", Status: RunStatusCancelled}, "run-1")
		res := spawnResumeFailure(deps, req, "run-1", "spawn-synthetic", claimErr)

		assert.Equal(t, RunStatusError, res.Status)
		assert.Equal(t, "run-1", res.RunID)
		assert.Contains(t, res.Error, "cancelled")
		require.Len(t, got, 1, "the parent stream must be told the sub-agent is terminal")
		assert.Equal(t, "failed", got[0].Status,
			"a card left `running` for a cancelled pause never leaves that state")
		assert.Equal(t, "run-1", got[0].RunID, "the card names the run whose state it reports")
		assert.Equal(t, "run-1", got[0].ID)
		assert.Contains(t, got[0].Error, "cancelled")
	})

	t.Run("nil_emitter_is_safe", func(t *testing.T) {
		deps := &CoordinationToolDeps{ParentRunID: "parent-1"}
		res := spawnResumeFailure(deps, req, "run-1", "spawn-synthetic",
			lostResumeClaimError(&AgentRun{ID: "run-1", Status: RunStatusCancelled}, "run-1"))
		assert.Equal(t, RunStatusError, res.Status)
	})
}

// TestA2AResumeClaimLostError pins the A2A envelope each lost-claim reason maps
// to (issue #912): a concurrent resume keeps its own reason, while a terminal
// pause is INVALID_STATE carrying the run's real status instead of "already
// being resumed".
func TestA2AResumeClaimLostError(t *testing.T) {
	t.Run("concurrent_claim", func(t *testing.T) {
		a2aErr := a2aResumeClaimLostError(
			lostResumeClaimError(&AgentRun{ID: "run-1", Status: RunStatusSuccess}, "run-1"))
		require.NotNil(t, a2aErr)
		assert.Equal(t, a2aReasonResumeInProgress, a2aErr.Reason)
	})

	t.Run("terminal_pause", func(t *testing.T) {
		a2aErr := a2aResumeClaimLostError(
			lostResumeClaimError(&AgentRun{ID: "run-1", Status: RunStatusCancelled}, "run-1"))
		require.NotNil(t, a2aErr)
		assert.Equal(t, a2aReasonInvalidState, a2aErr.Reason)
		assert.NotEqual(t, a2aReasonResumeInProgress, a2aErr.Reason)
		assert.Contains(t, a2aErr.Message, "cancelled")
		assert.NotContains(t, a2aErr.Message, "already being resumed")
	})

	t.Run("unclassified_failure", func(t *testing.T) {
		a2aErr := a2aResumeClaimLostError(context.DeadlineExceeded)
		require.NotNil(t, a2aErr)
		assert.Equal(t, A2AReasonInvalidAgentResponse, a2aErr.Reason)
	})
}
