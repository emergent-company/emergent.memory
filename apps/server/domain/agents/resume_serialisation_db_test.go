package agents

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/emergent-company/emergent.memory/internal/config"
)

// TestBeginResumedRunSerialisesConcurrentResumes is the required regression test
// for issue #887: two concurrent followers of one paused run must produce
// exactly one executing child and one rejection.
//
// Before the fix Resume read the caller's in-memory `input-required` row, created
// the child, and only then flipped the checkpoint terminal — so both followers
// passed the check, both created a child, and both executed the turn. The claim
// now happens in the same transaction as the child insert
// (CreateResumedRunFromPause), and `input-required → completed` with the row
// count as the verdict is what serialises them.
//
// The test drives AgentExecutor.beginResumedRun, the exact seam Resume uses for
// every resume that has to create its own child, so it exercises the real SQL
// against the real schema. Driving Resume end to end would require a model
// factory and a workspace provisioner; the fake-driver seam cannot express this
// either, because a stubbed driver returns no row for the guarded UPDATE.
func TestBeginResumedRunSerialisesConcurrentResumes(t *testing.T) {
	tdb, projectID := seedAgentDeleteProject(t, "resume_serialisation")
	ctx := context.Background()
	repo := NewRepository(tdb.DB)
	ae := &AgentExecutor{repo: repo, log: testAgentLogger()}

	agentID := uuid.NewString()
	_, err := tdb.DB.ExecContext(ctx,
		`INSERT INTO kb.agents (id, name, strategy_type, cron_schedule, project_id)
		 VALUES (?, ?, 'graph', '', ?)`,
		agentID, "resume-887", projectID)
	require.NoError(t, err)

	paused, err := repo.CreateRunWithOptions(ctx, CreateRunOptions{AgentID: agentID, InitialStepCount: 2})
	require.NoError(t, err)
	require.NoError(t, repo.PauseRun(ctx, paused.ID, 2))
	priorRun, err := repo.FindRunByID(ctx, paused.ID)
	require.NoError(t, err)
	require.Equal(t, RunStatusPaused, priorRun.Status)

	resumedFrom := priorRun.ID
	opts := CreateRunOptions{
		AgentID:          agentID,
		ResumedFrom:      &resumedFrom,
		InitialStepCount: priorRun.StepCount,
	}

	const followers = 8
	var (
		wg      sync.WaitGroup
		mu      sync.Mutex
		winners []string
		losers  []error
	)
	start := make(chan struct{})
	for i := 0; i < followers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start // release every follower at once
			child, runErr := ae.beginResumedRun(ctx, priorRun, opts, false)
			mu.Lock()
			defer mu.Unlock()
			if runErr == nil {
				winners = append(winners, child.ID)
				return
			}
			losers = append(losers, runErr)
		}()
	}
	close(start)
	wg.Wait()

	require.Len(t, winners, 1, "exactly one follower may create the resumed child")
	require.Len(t, losers, followers-1)
	for _, runErr := range losers {
		require.True(t, errors.Is(runErr, ErrRunAlreadyResumed),
			"a losing follower must be rejected as already resumed, got %v", runErr)
	}

	// The database agrees: one child run, linked to the checkpoint.
	var children []*AgentRun
	require.NoError(t, tdb.DB.NewSelect().
		Model(&children).
		Where("resumed_from = ?", priorRun.ID).
		Scan(ctx))
	require.Len(t, children, 1, "exactly one resumed child row")
	require.Equal(t, winners[0], children[0].ID)
	require.Equal(t, RunStatusRunning, children[0].Status, "the child is the executing run")

	// The checkpoint is terminal, never left paused or running.
	checkpoint, err := repo.FindRunByID(ctx, priorRun.ID)
	require.NoError(t, err)
	require.Equal(t, RunStatusSuccess, checkpoint.Status)
	require.NotNil(t, checkpoint.CompletedAt)
}

// TestCreateResumedRunFromPauseRollsBackClaimOnInsertFailure pins the trade the
// claim must not make: consuming a pause it then cannot honour. The claim and the
// child insert share a transaction, so a failed insert leaves the run resumable
// instead of stranding an `input-required` run that no longer accepts a resume.
func TestCreateResumedRunFromPauseRollsBackClaimOnInsertFailure(t *testing.T) {
	tdb, projectID := seedAgentDeleteProject(t, "resume_claim_rollback")
	ctx := context.Background()
	repo := NewRepository(tdb.DB)

	agentID := uuid.NewString()
	_, err := tdb.DB.ExecContext(ctx,
		`INSERT INTO kb.agents (id, name, strategy_type, cron_schedule, project_id)
		 VALUES (?, ?, 'graph', '', ?)`,
		agentID, "resume-rollback-887", projectID)
	require.NoError(t, err)

	paused, err := repo.CreateRunWithOptions(ctx, CreateRunOptions{AgentID: agentID, InitialStepCount: 1})
	require.NoError(t, err)
	require.NoError(t, repo.PauseRun(ctx, paused.ID, 1))

	// A non-UUID agent id makes the child insert fail on the uuid column after
	// the claim UPDATE has already run inside the transaction.
	_, claimed, err := repo.CreateResumedRunFromPause(ctx, paused.ID, CreateRunOptions{AgentID: "not-a-uuid"})
	require.Error(t, err)
	require.False(t, claimed)

	after, err := repo.FindRunByID(ctx, paused.ID)
	require.NoError(t, err)
	require.Equal(t, RunStatusPaused, after.Status,
		"a claim whose child insert failed must roll back, leaving the pause resumable")

	// And a real resume still works afterwards.
	resumedFrom := paused.ID
	child, claimed, err := repo.CreateResumedRunFromPause(ctx, paused.ID, CreateRunOptions{
		AgentID:          agentID,
		ResumedFrom:      &resumedFrom,
		InitialStepCount: after.StepCount,
	})
	require.NoError(t, err)
	require.True(t, claimed)
	require.NotNil(t, child)
}

// TestClaimPausedRunForResumeIsSingleUse covers the pre-created-child variant:
// the caller creates the child itself (question-answer paths do), so the claim is
// what stops the second follower.
func TestClaimPausedRunForResumeIsSingleUse(t *testing.T) {
	tdb, projectID := seedAgentDeleteProject(t, "resume_pause_claim")
	ctx := context.Background()
	repo := NewRepository(tdb.DB)

	agentID := uuid.NewString()
	_, err := tdb.DB.ExecContext(ctx,
		`INSERT INTO kb.agents (id, name, strategy_type, cron_schedule, project_id)
		 VALUES (?, ?, 'graph', '', ?)`,
		agentID, fmt.Sprintf("resume-claim-887"), projectID)
	require.NoError(t, err)

	paused, err := repo.CreateRunWithOptions(ctx, CreateRunOptions{AgentID: agentID})
	require.NoError(t, err)
	require.NoError(t, repo.PauseRun(ctx, paused.ID, 0))

	claimed, err := repo.ClaimPausedRunForResume(ctx, paused.ID)
	require.NoError(t, err)
	require.True(t, claimed, "the first follower claims the pause")

	claimed, err = repo.ClaimPausedRunForResume(ctx, paused.ID)
	require.NoError(t, err)
	require.False(t, claimed, "the second follower must be rejected")
}

// TestResumeRetiresLosingFollowersPreCreatedChild covers Resume's own
// pre-created-child branch (issue #887). The private-seam test above drives
// beginResumedRun directly and never enters Resume, while the SQL test above only
// exercises the claim statement — so nothing pinned the branch that decides what
// happens to the child a losing follower already inserted. A refactor that
// dropped it (or that skipped the claim when PreCreatedRun is set) would leave
// both green while a second turn executed on a leaked `working` child.
//
// The test walks the real caller shape: pre-create the child, let another
// follower claim the pause first, then call Resume with that child.
func TestResumeRetiresLosingFollowersPreCreatedChild(t *testing.T) {
	tdb, projectID := seedAgentDeleteProject(t, "resume_precreated_loser")
	ctx := context.Background()
	repo := NewRepository(tdb.DB)
	ae := &AgentExecutor{
		repo:       repo,
		log:        testAgentLogger(),
		safeguards: config.AgentSafeguardsConfig{ExecutionEnabled: true},
	}

	agentID := uuid.NewString()
	_, err := tdb.DB.ExecContext(ctx,
		`INSERT INTO kb.agents (id, name, strategy_type, cron_schedule, project_id)
		 VALUES (?, ?, 'graph', '', ?)`,
		agentID, "resume-loser-887", projectID)
	require.NoError(t, err)

	checkpoint, err := repo.CreateRunWithOptions(ctx, CreateRunOptions{AgentID: agentID, InitialStepCount: 1})
	require.NoError(t, err)
	require.NoError(t, repo.PauseRun(ctx, checkpoint.ID, 1))
	priorRun, err := repo.FindRunByID(ctx, checkpoint.ID)
	require.NoError(t, err)
	require.Equal(t, RunStatusPaused, priorRun.Status)

	// The child a follower pre-creates before calling Resume.
	resumedFrom := priorRun.ID
	preCreated, err := repo.CreateRunWithOptions(ctx, CreateRunOptions{
		AgentID:          agentID,
		ResumedFrom:      &resumedFrom,
		InitialStepCount: priorRun.StepCount,
	})
	require.NoError(t, err)
	require.Equal(t, RunStatusRunning, preCreated.Status)

	// Another follower wins the pause.
	won, err := repo.ClaimPausedRunForResume(ctx, priorRun.ID)
	require.NoError(t, err)
	require.True(t, won)

	result, err := ae.Resume(ctx, priorRun, ExecuteRequest{
		ProjectID:     projectID,
		PreCreatedRun: preCreated,
	})
	require.Nil(t, result, "a losing follower must not execute")
	require.ErrorIs(t, err, ErrRunAlreadyResumed)

	after, err := repo.FindRunByID(ctx, preCreated.ID)
	require.NoError(t, err)
	require.Equal(t, RunStatusError, after.Status,
		"the losing follower's pre-created child must be retired, not left running")
	require.NotNil(t, after.CompletedAt)

	// And the winner's checkpoint is terminal, not left paused.
	spent, err := repo.FindRunByID(ctx, priorRun.ID)
	require.NoError(t, err)
	require.Equal(t, RunStatusSuccess, spent.Status)

	// A second resume of the same pause is still rejected: the child retirement
	// must not have made the pause resumable again.
	again, err := ae.Resume(ctx, priorRun, ExecuteRequest{
		ProjectID:     projectID,
		PreCreatedRun: preCreated,
	})
	require.Nil(t, again)
	require.ErrorIs(t, err, ErrRunAlreadyResumed)
}
