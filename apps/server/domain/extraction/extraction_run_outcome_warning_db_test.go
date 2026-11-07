package extraction

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// TestRunRecorderRecordOutcomeWarning_PersistsSummaryAndTranscript is the
// persistence half of the #233 signal: the skip/zero-object warning lands on
// the run's summary jsonb (the field the run API and chat workspace read) and on
// the transcript as a warning-labelled message naming the skipped type.
func TestRunRecorderRecordOutcomeWarning_PersistsSummaryAndTranscript(t *testing.T) {
	ctx, db := openEmbeddingWorkerTestDB(t)
	projectID := seedEmbeddingProject(t, ctx, db)

	var jobID string
	require.NoError(t, db.NewRaw(`INSERT INTO kb.object_extraction_jobs
		(id, project_id, status, created_at, updated_at)
		VALUES (gen_random_uuid(), ?, 'pending', now(), now())
		RETURNING id::text`, projectID).Scan(ctx, &jobID))

	rec := NewRunRecorder(db, quietLogger())
	job := &ObjectExtractionJob{ID: jobID, ProjectID: projectID}
	runID, err := rec.StartRun(ctx, job)
	require.NoError(t, err)

	warning := planRunOutcomeWarning([]string{"PodcastEpisode"}, 0)
	require.NotNil(t, warning)
	require.NoError(t, rec.RecordOutcomeWarning(ctx, runID, warning))

	var kind, skipped, objects, severity string
	require.NoError(t, db.NewRaw(`SELECT
		summary->'outcome_warning'->>'kind',
		summary->'outcome_warning'->>'skipped_types',
		summary->'outcome_warning'->>'objects',
		summary->'outcome_warning'->>'severity'
		FROM kb.agent_runs WHERE id = ?`, runID).Scan(ctx, &kind, &skipped, &objects, &severity))
	require.Equal(t, "skipped_types+zero_objects", kind)
	require.Equal(t, `["PodcastEpisode"]`, skipped)
	require.Equal(t, "0", objects)
	require.Equal(t, "warning", severity)

	// The original summary keys survive the merge, like the variance writer.
	var trigger string
	require.NoError(t, db.NewRaw(`SELECT summary->>'trigger' FROM kb.agent_runs WHERE id = ?`, runID).Scan(ctx, &trigger))
	require.Equal(t, "extraction", trigger)

	var stageKind, text string
	require.NoError(t, db.NewRaw(`SELECT content->>'stage_kind', content->>'text'
		FROM kb.agent_run_messages
		WHERE run_id = ? AND content->>'stage_kind' = ?`, runID, stageKindWarning).Scan(ctx, &stageKind, &text))
	require.Equal(t, "warning", stageKind)
	require.Contains(t, text, "PodcastEpisode")
	require.Contains(t, text, "0 objects")
}
