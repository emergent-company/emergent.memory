package extraction

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/uptrace/bun"

	"github.com/emergent-company/emergent.memory/pkg/logger"
	"github.com/emergent-company/emergent.memory/pkg/runstatus"
)

const (
	// extractionRunAgentName is the name of the per-project runtime agent that
	// owns extraction runs. It follows the same synthetic-agent pattern chat
	// uses for remember/forget sessions, so runs hang off a real kb.agents row
	// (the run listing joins it for project scoping) without appearing as a
	// scheduled trigger — it is created disabled with a manual trigger.
	extractionRunAgentName = "Extraction"
	// extractionRunStrategyType marks the runtime agent as extraction-owned. A
	// future user-defined extraction agent plugs in by pointing this row's
	// agent_definition_id at that definition, which is also the hook the chat
	// transcript then shows (the run's agent).
	extractionRunStrategyType = "extraction"
	// runMessageMaxChars bounds one stored message. Extraction prompts carry
	// document batches (~4000 tokens) and the full fidelity lives in the job's
	// trace log + OTel spans, so the transcript keeps a readable prefix instead
	// of duplicating the document into kb.agent_run_messages.
	runMessageMaxChars = 12000
	// failureMessageStep numbers the failure message written when a job fails, so
	// it sorts after every stage message even if the timestamps tie.
	failureMessageStep = 1 << 30
	// runWriteTimeout bounds each transcript write so a stuck database cannot
	// stall extraction: recording is diagnostics, never a dependency.
	runWriteTimeout = 5 * time.Second
)

// RunRecorder persists an extraction job's pipeline transcript as an agent run
// (kb.agent_runs + kb.agent_run_messages), the same shape the chat workspace
// renders for scheduled and board runs. Extraction runs a bespoke staged
// pipeline rather than the agent executor, so it writes the transcript itself.
//
// Every method is best-effort by contract: callers log a failure and continue,
// because a missing transcript must never fail an extraction. The recorder is
// safe for concurrent use (megabytes of INSERTs are independent statements).
type RunRecorder struct {
	db  bun.IDB
	log *slog.Logger
}

// NewRunRecorder creates an extraction run recorder.
func NewRunRecorder(db bun.IDB, log *slog.Logger) *RunRecorder {
	return &RunRecorder{db: db, log: log.With(logger.Scope("extraction.run-recorder"))}
}

// RecordMessage persists one transcript message for a run. It satisfies the
// runMessageRecorder seam the trace-logger bridge uses, which keeps that bridge
// testable without a database.
func (r *RunRecorder) RecordMessage(ctx context.Context, runID, role string, step int, text string) error {
	if r == nil || runID == "" || strings.TrimSpace(text) == "" {
		return nil
	}
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), runWriteTimeout)
	defer cancel()

	content, err := json.Marshal(map[string]any{"text": truncateForRun(text, runMessageMaxChars)})
	if err != nil {
		return fmt.Errorf("marshal run message: %w", err)
	}
	if _, err := r.db.NewRaw(`
		INSERT INTO kb.agent_run_messages (run_id, role, content, step_number, created_at)
		VALUES (?, ?, ?::jsonb, ?, now())`, runID, role, string(content), step).Exec(ctx); err != nil {
		return fmt.Errorf("insert run message: %w", err)
	}
	// last_step_at is how the run listing (and the chat workspace) tells an
	// active run from a stalled one; a transcript write is the heartbeat.
	if _, err := r.db.NewRaw(`UPDATE kb.agent_runs SET last_step_at = now() WHERE id = ?`, runID).Exec(ctx); err != nil {
		return fmt.Errorf("touch run: %w", err)
	}
	return nil
}

// StartRun opens the run that records this job's transcript and links it back
// onto the job row. It returns the run id, or "" when the link or creation
// failed (callers then degrade to the extraction history page).
func (r *RunRecorder) StartRun(ctx context.Context, job *ObjectExtractionJob) (string, error) {
	if r == nil || job == nil || job.ProjectID == "" {
		return "", nil
	}
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), runWriteTimeout*2)
	defer cancel()

	agentID, err := r.ensureExtractionAgent(ctx, job.ProjectID)
	if err != nil {
		return "", err
	}

	trigger := fmt.Sprintf("Extraction job %s", job.ID)
	if job.DocumentID != nil && *job.DocumentID != "" {
		trigger = fmt.Sprintf("Extraction job %s · document %s", job.ID, *job.DocumentID)
	}
	summary := `{"trigger":"extraction","job_id":"` + job.ID + `"}`
	var runID string
	if err := r.db.NewRaw(`
		INSERT INTO kb.agent_runs (agent_id, status, started_at, trigger_source, trigger_message, trace_id, summary)
		VALUES (?, ?, now(), 'extraction', ?, ?, ?::jsonb)
		RETURNING id::text`,
		agentID, string(runstatus.Running), trigger, job.TraceID, summary).Scan(ctx, &runID); err != nil {
		return "", fmt.Errorf("create extraction run: %w", err)
	}

	if _, err := r.db.NewRaw(`
		UPDATE kb.object_extraction_jobs SET run_id = ?, updated_at = now() WHERE id = ?`,
		runID, job.ID).Exec(ctx); err != nil {
		// The run exists but the job does not point at it: mark the run failed so
		// it is not left "working" forever, and let the caller degrade.
		r.failOrphanRun(ctx, runID, "link run to extraction job: "+err.Error())
		return "", fmt.Errorf("link extraction run: %w", err)
	}
	job.RunID = &runID
	return runID, nil
}

// FinishRun closes the run with the terminal status the job reached.
func (r *RunRecorder) FinishRun(ctx context.Context, runID, status, errMessage string) error {
	if r == nil || runID == "" {
		return nil
	}
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), runWriteTimeout)
	defer cancel()
	var errMsg *string
	if strings.TrimSpace(errMessage) != "" {
		trimmed := truncateForRun(errMessage, 2000)
		errMsg = &trimmed
	}
	if _, err := r.db.NewRaw(`
		UPDATE kb.agent_runs
		SET status = ?, completed_at = now(), error_message = ?,
		    duration_ms = GREATEST(0, (EXTRACT(EPOCH FROM (now() - started_at)) * 1000)::int)
		WHERE id = ?`, status, errMsg, runID).Exec(ctx); err != nil {
		return fmt.Errorf("finish extraction run: %w", err)
	}
	return nil
}

// ensureExtractionAgent returns the project's extraction runtime agent,
// creating it on first use. Two workers racing on a cold project can both
// insert; the extra row is harmless (both own runs and are project-scoped), and
// the lookup is by name so subsequent runs converge on the oldest row.
func (r *RunRecorder) ensureExtractionAgent(ctx context.Context, projectID string) (string, error) {
	var agentID string
	err := r.db.NewSelect().
		TableExpr("kb.agents").
		ColumnExpr("id::text").
		Where("project_id = ? AND name = ?", projectID, extractionRunAgentName).
		Order("created_at ASC").
		Limit(1).
		Scan(ctx, &agentID)
	if err == nil && agentID != "" {
		return agentID, nil
	}
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return "", fmt.Errorf("find extraction agent: %w", err)
	}
	if err := r.db.NewRaw(`
		INSERT INTO kb.agents (project_id, name, strategy_type, cron_schedule, enabled, trigger_type, config, description)
		VALUES (?, ?, ?, '0 0 * * *', false, 'manual', '{}'::jsonb,
		        'Records document-extraction pipeline runs. Managed automatically; runs are shown in chat.')
		RETURNING id::text`,
		projectID, extractionRunAgentName, extractionRunStrategyType).Scan(ctx, &agentID); err != nil {
		return "", fmt.Errorf("create extraction agent: %w", err)
	}
	return agentID, nil
}

// failOrphanRun closes a run that could not be linked to its job.
func (r *RunRecorder) failOrphanRun(ctx context.Context, runID, reason string) {
	if err := r.FinishRun(ctx, runID, string(runstatus.Error), reason); err != nil {
		r.log.Warn("failed to close unlinked extraction run",
			slog.String("run_id", runID),
			logger.Error(err))
	}
}

// truncateForRun bounds stored text, marking the cut so a reader knows the full
// text lives in the job's trace log. The bound counts runes, not bytes: a byte
// slice can split a multi-byte character (documents are full of them) and leave
// invalid UTF-8 in the transcript.
func truncateForRun(text string, max int) string {
	if max <= 0 {
		return text
	}
	runes := []rune(text)
	if len(runes) <= max {
		return text
	}
	const marker = "\n\n…[truncated — the full text is in the extraction trace log]"
	budget := max - len([]rune(marker))
	if budget <= 0 {
		return string(runes[:max])
	}
	return string(runes[:budget]) + marker
}
