package agents

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/emergent-company/emergent.memory/domain/graph"
)

// TestIsReworkRun verifies rework-run detection keys on the work_feedback
// trigger metadata set by the request-changes enqueue.
func TestIsReworkRun(t *testing.T) {
	require.False(t, isReworkRun(nil))
	require.False(t, isReworkRun(&AgentRun{}))
	require.False(t, isReworkRun(&AgentRun{TriggerMetadata: map[string]any{}}))
	require.True(t, isReworkRun(&AgentRun{TriggerMetadata: map[string]any{"work_feedback": []any{}}}))
}

// claimRecordingStore captures which claim transition the worker performed.
type claimRecordingStore struct {
	fakeWorkObjectStore
	claimedReady string
	transitions  []graph.WorkObjectTransition
}

func (f *claimRecordingStore) ClaimWorkObject(ctx context.Context, projectID, canonicalID, readyStatus, inProgressStatus string) (bool, error) {
	f.claimedReady = readyStatus
	return true, nil
}

func (f *claimRecordingStore) TransitionWorkObject(ctx context.Context, projectID, canonicalID string, t graph.WorkObjectTransition) (bool, error) {
	f.transitions = append(f.transitions, t)
	return true, nil
}

// TestClaimWorkObject_ReworkClaimsFromRevision verifies a rework run claims from
// the revision status (not ready), so a request-changes rework run is not
// skipped and stranded in revision forever.
func TestClaimWorkObject_ReworkClaimsFromRevision(t *testing.T) {
	pool := NewWorkerPool(nil, nil, testAgentLogger(), 0, time.Second)
	store := &claimRecordingStore{}
	pool.SetWorkObjectStore(store)

	subjectID := uuid.NewString()
	agent := &Agent{ProjectID: "p1"}
	def := &AgentDefinition{WorkConfig: AgentWorkConfig{Status: AgentWorkStatusConfig{
		Ready: "ready", Revision: "revision", InProgress: "in_progress",
	}}}

	reworkRun := &AgentRun{
		SubjectObjectID: &subjectID,
		TriggerMetadata: map[string]any{"work_feedback": []any{map[string]any{"round": 1, "text": "fix"}}},
	}
	claimed, err := pool.claimWorkObject(context.Background(), reworkRun, agent, def)
	require.NoError(t, err)
	require.True(t, claimed)
	require.Len(t, store.transitions, 1)
	require.Equal(t, "revision", store.transitions[0].FromStatus)
	require.Equal(t, "in_progress", store.transitions[0].ToStatus)

	normalRun := &AgentRun{SubjectObjectID: &subjectID}
	claimed, err = pool.claimWorkObject(context.Background(), normalRun, agent, def)
	require.NoError(t, err)
	require.True(t, claimed)
	require.Equal(t, "ready", store.claimedReady)
}

// TestIncrementWorkItemFailure_Concurrent verifies the failure count is
// incremented atomically (no double-increment across concurrent replicas).
func TestIncrementWorkItemFailure_Concurrent(t *testing.T) {
	ctx, _, repo, projectID := setupWorkObjectDispatch(t)
	canonicalID := uuid.NewString()

	const n = 25
	var wg sync.WaitGroup
	errCh := make(chan error, n)
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := repo.IncrementWorkItemFailure(ctx, projectID, canonicalID, string(FailureClassRetryable), nil); err != nil {
				errCh <- err
			}
		}()
	}
	wg.Wait()
	close(errCh)
	for err := range errCh {
		t.Fatalf("increment failed: %v", err)
	}

	st := mustState(t, ctx, repo, projectID, canonicalID)
	require.Equal(t, n, st.FailureCount)
}

// TestEnqueueWorkRunDeduped_Dedup verifies the atomic claim+enqueue creates a
// single run and a single processing-log row for a repeated dispatch.
func TestEnqueueWorkRunDeduped_Dedup(t *testing.T) {
	ctx, db, repo, projectID := setupWorkObjectDispatch(t)
	defID := insertWorkDefinition(t, db, ctx, projectID, "researcher",
		`{"status":{"ready":"ready","inProgress":"in_progress"}}`)
	agentID := insertWorkAgent(t, db, ctx, projectID, "researcher", defID)

	canonicalID := uuid.NewString()
	insertWorkObject(t, db, ctx, projectID, canonicalID, "dedup-1", "ready")

	objType := "ResearchRequest"
	opts := CreateRunQueuedOptions{SubjectObjectID: &canonicalID, SubjectObjectType: &objType, TrustedInternal: true}
	mkLog := func() *AgentProcessingLog {
		return &AgentProcessingLog{
			AgentID:       agentID,
			GraphObjectID: canonicalID,
			ObjectVersion: 1,
			EventType:     EventTypeCreated,
			Status:        ProcessingStatusPending,
		}
	}

	run, claimed, err := repo.EnqueueWorkRunDeduped(ctx, mkLog(), agentID, 1, opts)
	require.NoError(t, err)
	require.True(t, claimed)
	require.NotNil(t, run)

	run2, claimed2, err := repo.EnqueueWorkRunDeduped(ctx, mkLog(), agentID, 1, opts)
	require.NoError(t, err)
	require.False(t, claimed2)
	require.Nil(t, run2)

	require.Equal(t, 1, countQueuedRuns(t, db, ctx, agentID, canonicalID))
	var logCount int
	require.NoError(t, db.NewRaw(
		`SELECT COUNT(*) FROM kb.agent_processing_log WHERE agent_id = ? AND graph_object_id = ? AND object_version = 1 AND event_type = 'created'`,
		agentID, canonicalID,
	).Scan(ctx, &logCount))
	require.Equal(t, 1, logCount)
}

// recordingReaperStore returns heads only for the status it is asked for, and
// records the transitions/blocks the reaper performs.
type recordingReaperStore struct {
	fakeWorkObjectStore
	listStatuses  []string
	headsByStatus map[string][]*graph.WorkObjectHead
	transitions   []graph.WorkObjectTransition
	blocks        [][2]string // {inProgress, blocked}
}

func (f *recordingReaperStore) ListWorkObjectsByStatus(ctx context.Context, projectID, status string, olderThan time.Time, limit int) ([]*graph.WorkObjectHead, error) {
	f.listStatuses = append(f.listStatuses, status)
	return f.headsByStatus[status], nil
}

func (f *recordingReaperStore) TransitionWorkObject(ctx context.Context, projectID, canonicalID string, t graph.WorkObjectTransition) (bool, error) {
	f.transitions = append(f.transitions, t)
	return true, nil
}

func (f *recordingReaperStore) BlockWorkObject(ctx context.Context, projectID, canonicalID, inProgressStatus, blockedStatus string) (bool, error) {
	f.blocks = append(f.blocks, [2]string{inProgressStatus, blockedStatus})
	return true, nil
}

// TestWorkStatusReaper_CustomStatuses verifies the reaper scans and transitions
// using the owning agent's custom status mappings (and effective failure limit),
// not the literal "in_progress"/"ready"/"blocked" defaults.
func TestWorkStatusReaper_CustomStatuses(t *testing.T) {
	ctx, db, repo, projectID := setupWorkObjectDispatch(t)
	defID := insertWorkDefinition(t, db, ctx, projectID, "researcher",
		`{"status":{"ready":"todo","inProgress":"doing","blocked":"stuck"},"failureLimit":5}`)
	agentID := insertWorkAgent(t, db, ctx, projectID, "researcher", defID)
	_, err := db.ExecContext(ctx,
		`UPDATE kb.agents SET trigger_type = 'reaction', reaction_config = '{"objectTypes":["ResearchRequest"],"events":["created"]}'::jsonb WHERE id = ?`,
		agentID)
	require.NoError(t, err)

	canonicalID := uuid.NewString()
	objType := "ResearchRequest"
	run, err := repo.CreateRunQueued(ctx, agentID, 1, CreateRunQueuedOptions{
		SubjectObjectID:   &canonicalID,
		SubjectObjectType: &objType,
	})
	require.NoError(t, err)
	// Terminal run: not "live", but still the latest subject run so the reaper
	// can resolve the owning agent (and its custom statuses).
	_, err = db.ExecContext(ctx, `UPDATE kb.agent_runs SET status = 'error' WHERE id = ?`, run.ID)
	require.NoError(t, err)

	store := &recordingReaperStore{
		headsByStatus: map[string][]*graph.WorkObjectHead{
			"doing": {{ProjectID: projectID, CanonicalID: canonicalID, Status: "doing", Type: "ResearchRequest"}},
		},
	}
	reaper := NewWorkStatusReaper(repo, testAgentLogger(), time.Minute, time.Minute)
	reaper.SetWorkObjectStore(store)
	reaper.reap(ctx)

	require.Contains(t, store.listStatuses, "doing")
	require.Len(t, store.transitions, 1)
	require.Equal(t, "doing", store.transitions[0].FromStatus)
	require.Equal(t, "todo", store.transitions[0].ToStatus)
	require.Equal(t, 1, mustState(t, ctx, repo, projectID, canonicalID).FailureCount)
}
