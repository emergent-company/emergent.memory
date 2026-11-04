package extraction

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/emergent-company/emergent.memory/pkg/runstatus"
)

// TestRunRecorderPersistsTranscriptAndLinksJob is the persistence half of the
// extraction-as-session feature: a started job gets a run owned by a per-project
// "Extraction" agent, the job row points at that run, transcribed messages are
// readable through the same columns the chat workspace renders, and finishing
// writes a terminal status.
func TestRunRecorderPersistsTranscriptAndLinksJob(t *testing.T) {
	ctx, db := openEmbeddingWorkerTestDB(t)
	projectID := seedEmbeddingProject(t, ctx, db)

	var jobID string
	err := db.NewRaw(`INSERT INTO kb.object_extraction_jobs
		(id, project_id, status, created_at, updated_at)
		VALUES (gen_random_uuid(), ?, 'pending', now(), now())
		RETURNING id::text`, projectID).Scan(ctx, &jobID)
	require.NoError(t, err)

	job := &ObjectExtractionJob{ID: jobID, ProjectID: projectID}
	rec := NewRunRecorder(db, quietLogger())

	runID, err := rec.StartRun(ctx, job)
	require.NoError(t, err)
	require.NotEmpty(t, runID)
	require.NotNil(t, job.RunID, "StartRun must stamp the run id onto the in-memory job")
	require.Equal(t, runID, *job.RunID)

	// The job row must point at the run: that link is what the chat rail reads
	// to decide between opening a transcript and linking the history page.
	var linkedRunID string
	require.NoError(t, db.NewRaw(`SELECT run_id::text FROM kb.object_extraction_jobs WHERE id = ?`, jobID).Scan(ctx, &linkedRunID))
	require.Equal(t, runID, linkedRunID)

	// The run hangs off a project-scoped extraction agent, created disabled with
	// a manual trigger so the scheduler never treats it as a cron agent.
	var agentName, triggerType, strategyType string
	var enabled bool
	require.NoError(t, db.NewRaw(`SELECT a.name, a.trigger_type, a.strategy_type, a.enabled
		FROM kb.agent_runs r JOIN kb.agents a ON a.id = r.agent_id
		WHERE r.id = ?`, runID).Scan(ctx, &agentName, &triggerType, &strategyType, &enabled))
	require.Equal(t, extractionRunAgentName, agentName)
	require.Equal(t, "manual", triggerType)
	require.Equal(t, extractionRunStrategyType, strategyType)
	require.False(t, enabled, "the extraction agent must never be schedulable")

	// Transcript writes land where the run timeline reads from, with the content
	// shape runMessageText extracts ("text").
	require.NoError(t, rec.RecordMessage(ctx, runID, "user", 1, "extract entities"))
	require.NoError(t, rec.RecordMessage(ctx, runID, "assistant", 2, `{"entities":[]}`))

	var msgs []struct {
		Role string `bun:"role"`
		Text string `bun:"text"`
	}
	require.NoError(t, db.NewRaw(`SELECT role, content->>'text' AS text FROM kb.agent_run_messages
		WHERE run_id = ? ORDER BY step_number ASC`, runID).Scan(ctx, &msgs))
	require.Len(t, msgs, 2)
	require.Equal(t, "user", msgs[0].Role)
	require.Equal(t, "extract entities", msgs[0].Text)
	require.Equal(t, "assistant", msgs[1].Role)
	require.Equal(t, `{"entities":[]}`, msgs[1].Text)

	// last_step_at is the liveness heartbeat the run listing uses.
	var lastStepSet bool
	require.NoError(t, db.NewRaw(`SELECT last_step_at IS NOT NULL FROM kb.agent_runs WHERE id = ?`, runID).Scan(ctx, &lastStepSet))
	require.True(t, lastStepSet, "recording a message must touch last_step_at")

	// Finishing records the terminal status, the error and a duration.
	require.NoError(t, rec.FinishRun(ctx, runID, string(runstatus.Error), "model overloaded"))

	var status, errorMessage string
	var completed bool
	var duration int
	require.NoError(t, db.NewRaw(`SELECT status, error_message, completed_at IS NOT NULL AS completed, duration_ms
		FROM kb.agent_runs WHERE id = ?`, runID).Scan(ctx, &status, &errorMessage, &completed, &duration))
	require.Equal(t, string(runstatus.Error), status)
	require.Equal(t, "model overloaded", errorMessage)
	require.True(t, completed)
	require.GreaterOrEqual(t, duration, 0)
}

// TestRunRecorderReusesOneAgentPerProject keeps the agents list from filling with
// one "Extraction" row per job: a second job in the same project reuses the
// agent, and a second project gets its own.
func TestRunRecorderReusesOneAgentPerProject(t *testing.T) {
	ctx, db := openEmbeddingWorkerTestDB(t)
	projectID := seedEmbeddingProject(t, ctx, db)
	otherProjectID := seedEmbeddingProject(t, ctx, db)

	rec := NewRunRecorder(db, quietLogger())
	seedJob := func(project string) *ObjectExtractionJob {
		var jobID string
		require.NoError(t, db.NewRaw(`INSERT INTO kb.object_extraction_jobs
			(id, project_id, status, created_at, updated_at)
			VALUES (gen_random_uuid(), ?, 'pending', now(), now())
			RETURNING id::text`, project).Scan(ctx, &jobID))
		return &ObjectExtractionJob{ID: jobID, ProjectID: project}
	}

	for i := 0; i < 2; i++ {
		_, err := rec.StartRun(ctx, seedJob(projectID))
		require.NoError(t, err)
	}
	_, err := rec.StartRun(ctx, seedJob(otherProjectID))
	require.NoError(t, err)

	var perProject int
	require.NoError(t, db.NewRaw(`SELECT count(*) FROM kb.agents WHERE project_id = ? AND name = ?`,
		projectID, extractionRunAgentName).Scan(ctx, &perProject))
	require.Equal(t, 1, perProject, "two jobs in one project must share one extraction agent")

	var total int
	require.NoError(t, db.NewRaw(`SELECT count(*) FROM kb.agents WHERE name = ?`, extractionRunAgentName).Scan(ctx, &total))
	require.Equal(t, 2, total, "a second project gets its own extraction agent")
}
