package agents

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"

	"github.com/emergent-company/emergent.memory/domain/mcp"
	"github.com/emergent-company/emergent.memory/internal/jobs"
	"github.com/emergent-company/emergent.memory/pkg/logger"
)

// ============================================================================
// remember-status aggregation
// ============================================================================

// graphMutatingToolNames is the allowlist of MCP tools that mutate the
// knowledge graph. remember-status aggregates ONLY these tool calls to compute
// a run's graph-mutation summary. Extend this slice when new mutating tools are
// added — calls to tools outside this list are ignored for counting purposes.
var graphMutatingToolNames = []string{
	"entity-create",
	"entity-update",
	"entity-relationship-create",
	"entity-delete",
	"relationship-delete",
}

// queueReextractionToolName is the tool the remember/forget agent actually calls
// in its primary flow: it classifies the message into a document and queues an
// async extraction job (args: document_id, schema_id; output: {"job_id": "..."})
// instead of calling entity-create directly. The graph mutations then happen in
// the extraction job AFTER the agent run reports completed — remember-status
// follows these jobs so it does not under-report for the real remember flow.
const queueReextractionToolName = "queue-reextraction"

// Job status values, mirroring extraction.JobStatus (shared by the extraction
// and embedding job queues). Defined here because agents cannot import
// extraction (agents → extraction → projects → agents import cycle); the
// extraction domain projects jobs through mcp.ExtractionJobInfo /
// mcp.EmbeddingJobInfo instead.
const (
	jobStatusPending    = "pending"
	jobStatusProcessing = "processing"
	jobStatusCompleted  = "completed"
	jobStatusFailed     = "failed"
	jobStatusDeadLetter = "dead_letter"
	jobStatusCancelled  = "cancelled"
)

var graphMutatingToolSet = func() map[string]bool {
	set := make(map[string]bool, len(graphMutatingToolNames))
	for _, n := range graphMutatingToolNames {
		set[n] = true
	}
	return set
}()

func isGraphMutatingTool(name string) bool {
	return graphMutatingToolSet[name]
}

// rememberStatusAggregation holds the derived graph-mutation summary for a run.
type rememberStatusAggregation struct {
	ObjectsCreated         int
	ObjectsUpdated         int
	RelationshipsCreated   int
	ObjectsDeleted         int
	RelationshipsDeleted   int
	CreatedObjectIDs       []string
	CreatedRelationshipIDs []string
	DeletedObjectIDs       []string
	DeletedRelationshipIDs []string
	DiscoveredTypes        []string
	// FailedToolCalls counts graph-mutating tool calls that did not complete.
	FailedToolCalls int
	// Summary is a short human-readable line describing the run's impact.
	Summary string
}

// aggregateRememberStatus derives objects_created/updated, relationships_created,
// created IDs, and discovered types from a run's recorded tool calls.
//
// Parsing is defensive: tool outputs are free-form JSON, so any call whose
// output is missing or malformed is skipped rather than failing the whole
// aggregation. Calls with status != "completed" are counted as failures and
// excluded from mutation counts.
func aggregateRememberStatus(toolCalls []*AgentRunToolCall) rememberStatusAggregation {
	agg := rememberStatusAggregation{
		CreatedObjectIDs:       []string{},
		CreatedRelationshipIDs: []string{},
		DeletedObjectIDs:       []string{},
		DeletedRelationshipIDs: []string{},
		DiscoveredTypes:        []string{},
	}

	for _, tc := range toolCalls {
		if tc == nil || !isGraphMutatingTool(tc.ToolName) {
			continue
		}
		if tc.Status != "completed" {
			agg.FailedToolCalls++
			continue
		}

		switch tc.ToolName {
		case "entity-create":
			agg.parseEntityCreate(tc.Output)
		case "entity-update":
			agg.parseEntityUpdate(tc.Output)
		case "entity-relationship-create":
			agg.parseRelationshipCreate(tc.Output)
		case "entity-delete":
			agg.parseEntityDelete(tc)
		case "relationship-delete":
			agg.parseRelationshipDelete(tc)
		}
	}

	agg.buildSummary()
	return agg
}

// reextractionJobIDs collects job_id values from completed queue-reextraction
// tool call outputs ({"job_id": "<uuid>"}). These identify the async extraction
// jobs that perform the actual graph mutations for the remember flow.
func reextractionJobIDs(toolCalls []*AgentRunToolCall) []string {
	var ids []string
	for _, tc := range toolCalls {
		if tc == nil || tc.ToolName != queueReextractionToolName || tc.Status != "completed" {
			continue
		}
		id := mapStr(tc.Output, "job_id")
		if id == "" {
			id = mapStr(tc.Output, "jobId") // defensive: camelCase variant
		}
		if id != "" {
			addID(&ids, id)
		}
	}
	return ids
}

// aggregateExtractionJobs merges the persisted results of completed extraction
// jobs into an aggregation and reports whether any job is still in flight
// (pending), genuinely failed/dead-lettered, or was terminal-failed by the
// stale-job sweep.
//
// ObjectsMerged is computed at job completion but NOT persisted on the job row,
// so update counts cannot be recovered from extraction jobs — only
// tool-call-derived aggregation provides objects_updated.
//
// Jobs reaped by the stale sweep are returned as staleReaped rather than added
// to failures: they are historical cleanup artifacts, not current failures the
// caller can act on (mirrors the sibling stale_failed split, #763/#772).
func aggregateExtractionJobs(jobInfos []*mcp.ExtractionJobInfo) (agg rememberStatusAggregation, pending bool, failures []string, staleReaped int) {
	agg = rememberStatusAggregation{
		CreatedObjectIDs:       []string{},
		CreatedRelationshipIDs: []string{},
		DeletedObjectIDs:       []string{},
		DeletedRelationshipIDs: []string{},
		DiscoveredTypes:        []string{},
	}
	for _, job := range jobInfos {
		if job == nil {
			continue
		}
		switch job.Status {
		case jobStatusPending, jobStatusProcessing:
			// Still in flight — remembering isn't done until extraction finishes.
			pending = true
		case jobStatusCancelled:
			// Cancelled jobs are terminal and never produce results; neither
			// pending nor a failure.
			continue
		case jobStatusCompleted:
			agg.ObjectsCreated += job.ObjectsCreated
			agg.RelationshipsCreated += job.RelationshipsCreated
			for _, id := range job.CreatedObjectIDs {
				addID(&agg.CreatedObjectIDs, id)
			}
			for _, t := range job.DiscoveredTypes {
				addType(&agg.DiscoveredTypes, t)
			}
		case jobStatusFailed:
			if isStaleReaped(job.ErrorMessage) {
				// Terminal-failed by the stale-job sweep, not by a worker:
				// report the reap separately instead of as a current failure.
				staleReaped++
				continue
			}
			failures = append(failures, fmt.Sprintf("job %s failed: %s", job.ID, extractionJobErr(job)))
		case jobStatusDeadLetter:
			if isStaleReaped(job.ErrorMessage) {
				staleReaped++
				continue
			}
			failures = append(failures, fmt.Sprintf("job %s dead-lettered: %s", job.ID, extractionJobErr(job)))
		}
	}
	return agg, pending, failures, staleReaped
}

// extractionJobErr returns the job's error message or a placeholder.
func extractionJobErr(job *mcp.ExtractionJobInfo) string {
	if job.ErrorMessage != nil && *job.ErrorMessage != "" {
		return *job.ErrorMessage
	}
	return "no error message"
}

// isStaleReaped reports whether a job row was terminal-failed by the stale-job
// sweep rather than by a genuine worker failure. The sweep
// (domain/scheduler.StaleJobCleanupTask) stamps status='failed' plus the
// canonical jobs.StaleJobMessage error text; sourcing the marker from
// internal/jobs keeps this consumer classifying rows identically to the sweep
// and to every other reporting surface (#763/#772).
func isStaleReaped(errMsg *string) bool {
	return errMsg != nil && *errMsg == jobs.StaleJobMessage
}

// embeddingStatusSummary summarizes embedding-generation readiness for the
// objects a remember run created (third async stage after the agent run and the
// extraction job). Objects with no embedding job row ("" status) or a terminal
// completed/cancelled status count as ready — nothing is blocking recall for
// them.
type embeddingStatusSummary struct {
	// Pending is the number of created objects whose embedding job is not yet
	// completed (pending or processing).
	Pending int
	// Failed is the number of created objects whose embedding job genuinely
	// failed or dead-lettered.
	Failed int
	// Stale is the number of created objects whose embedding job was
	// terminal-failed by the stale-job sweep (jobs.StaleJobMessage). Kept out of
	// Failed so historical cleanup is not reported as a current failure.
	Stale int
}

// aggregateEmbeddingStatus classifies per-object embedding-job statuses into
// pending/failed/stale counts. Embedding readiness is NOT part of the
// remember-status "completed" gate: graph mutation (what "remember succeeded"
// means) is done once the extraction jobs finish, and embeddings are a separate,
// independently retryable concern (searchability). Failing the whole status on
// embeddings would conflate "did remember persist the memory" with "is it
// recallable yet" — instead embeddings_ready is exposed as a separate signal
// callers can poll if they want full recall-readiness. Permanently failed
// embeddings are surfaced via embeddings_failed and a summary note, without
// failing the overall status.
//
// Rows carrying the stale-sweep marker are counted in Stale, not Failed, so a
// reaped job is not presented as a current embedding failure (same split as the
// sibling embedding-status surfaces, #763/#772).
func aggregateEmbeddingStatus(infos []mcp.EmbeddingJobInfo) embeddingStatusSummary {
	var s embeddingStatusSummary
	for _, info := range infos {
		switch info.Status {
		case jobStatusPending, jobStatusProcessing:
			s.Pending++
		case jobStatusFailed, jobStatusDeadLetter:
			if isStaleReaped(info.LastError) {
				s.Stale++
				continue
			}
			s.Failed++
		}
	}
	return s
}

// embeddingSummaryNote returns a human-readable note appended to the run
// summary when embeddings are not yet fully ready, or "" when all created
// objects are recall-ready. total is the number of created objects checked.
func embeddingSummaryNote(pending, failed, total int) string {
	if pending > 0 {
		return fmt.Sprintf(" Embeddings still processing (%d/%d pending) — recall may not find these yet.", pending, total)
	}
	if failed > 0 {
		return fmt.Sprintf(" %d object(s) failed embedding generation — recall may miss these.", failed)
	}
	return ""
}

// merge folds another aggregation's counts/ids/types into this one and rebuilds
// the combined summary. Used to merge tool-call-derived results with
// extraction-job-derived results.
func (a *rememberStatusAggregation) merge(other rememberStatusAggregation) {
	a.ObjectsCreated += other.ObjectsCreated
	a.ObjectsUpdated += other.ObjectsUpdated
	a.RelationshipsCreated += other.RelationshipsCreated
	a.ObjectsDeleted += other.ObjectsDeleted
	a.RelationshipsDeleted += other.RelationshipsDeleted
	a.FailedToolCalls += other.FailedToolCalls
	for _, id := range other.CreatedObjectIDs {
		addID(&a.CreatedObjectIDs, id)
	}
	for _, id := range other.CreatedRelationshipIDs {
		addID(&a.CreatedRelationshipIDs, id)
	}
	for _, id := range other.DeletedObjectIDs {
		addID(&a.DeletedObjectIDs, id)
	}
	for _, id := range other.DeletedRelationshipIDs {
		addID(&a.DeletedRelationshipIDs, id)
	}
	for _, t := range other.DiscoveredTypes {
		addType(&a.DiscoveredTypes, t)
	}
	a.buildSummary()
}

// overallRememberStatus merges the agent run lifecycle state with the state of
// any extraction jobs the run queued via queue-reextraction, producing the
// final remember-status status and (for failed) a human-readable error.
//
// Semantics:
//   - "running": the agent run is not yet terminal, OR any queued extraction
//     job is still pending/processing — remembering is not done until the
//     extraction that performs the actual graph mutations finishes.
//   - "failed": the agent run failed, OR (the agent run completed but an
//     extraction job failed/dead-lettered). A failed extraction job is treated
//     as a failed remember even when the agent itself succeeded, because the
//     graph mutations the caller asked for never happened; the job errors are
//     surfaced in the error field alongside whatever partial counts succeeded.
//     Stale-sweep reaps are excluded from jobFailures by aggregateExtractionJobs
//     and reported separately as stale_jobs, so they never force "failed".
//   - "completed": the agent run is terminal and all queued extraction jobs are
//     terminal (completed/failed/dead-lettered handled above; no jobs → done).
func overallRememberStatus(runStatus AgentRunStatus, agentErr string, jobsPending bool, jobFailures []string) (status, errMsg string) {
	agentStatus, terminal, agentTerminalErr := rememberStatusFromRunStatus(runStatus)

	if !terminal || jobsPending {
		return "running", ""
	}
	if agentStatus == "failed" {
		msg := agentErr
		if msg == "" {
			msg = agentTerminalErr
		}
		if msg == "" {
			msg = "run failed"
		}
		return "failed", msg
	}
	if len(jobFailures) > 0 {
		return "failed", "extraction job(s) failed: " + strings.Join(jobFailures, "; ")
	}
	return "completed", ""
}

// parseEntityCreate counts created entities (and any inline relationships
// created with them) from an entity-create tool call output.
//
// Entity-create emits the uniform envelope: the batch results[] live at
// data.results[] and each item carries its own ok flag and entity payload —
// {"ok": true, "data": {"results": [{"ok": true, "entity": {"id": "...",
// "type": "...", "key": "...", "relationships": [...]}, "index": n}, ...],
// "message": "..."}, "meta": {"created": n, "failed": m, "total": t, ...}}.
// Items with ok=false are skipped; only items with ok=true and an entity
// payload are counted.
func (a *rememberStatusAggregation) parseEntityCreate(output map[string]any) {
	data := mapMap(output, "data")
	if data == nil {
		return // not an enveloped result — no data layer, skip
	}
	for _, raw := range mapSlice(data, "results") {
		item, ok := raw.(map[string]any)
		if !ok || !mapBool(item, "ok") {
			continue
		}
		ent := mapMap(item, "entity")
		if ent == nil {
			continue // malformed result — no entity payload, skip
		}
		a.ObjectsCreated++
		a.collectEntity(ent)
		if rels := mapSlice(ent, "relationships"); rels != nil {
			for _, rRaw := range rels {
				r, ok := rRaw.(map[string]any)
				if !ok {
					continue
				}
				a.RelationshipsCreated++
				if rid := mapStr(r, "id"); rid != "" {
					addID(&a.CreatedRelationshipIDs, rid)
				}
				if rt := mapStr(r, "type"); rt != "" {
					addType(&a.DiscoveredTypes, rt)
				}
			}
		}
	}
}

// parseEntityUpdate counts an entity-update tool call output.
//
// Entity-update emits the uniform envelope with the entity payload nested under
// data.entity — {"ok": true, "data": {"entity": {"id": "...", "type": "..."}}}.
// Calls with ok=false or a missing/malformed entity payload are skipped
// (defensive best-effort parsing).
func (a *rememberStatusAggregation) parseEntityUpdate(output map[string]any) {
	if !mapBool(output, "ok") {
		return
	}
	ent := mapMap(mapMap(output, "data"), "entity")
	if ent == nil {
		return // malformed output — no entity payload, skip
	}
	a.ObjectsUpdated++
	if t := mapStr(ent, "type"); t != "" {
		addType(&a.DiscoveredTypes, t)
	}
}

// parseRelationshipCreate counts an entity-relationship-create tool call output.
//
// Relationship-create emits the uniform batch envelope: per-item results live
// at data.results[], each carrying its own ok flag and a relationship payload —
// {"ok": true, "data": {"results": [{"ok": true, "relationship": {"id": "...",
// "type": "...", "source_id": "...", "target_id": "..."}, "index": n}, ...]},
// "meta": {"created": n, "failed": m, "total": t}}. Items with ok=false or a
// missing relationship payload are skipped.
func (a *rememberStatusAggregation) parseRelationshipCreate(output map[string]any) {
	data := mapMap(output, "data")
	if data == nil {
		return // not an enveloped result — no data layer, skip
	}
	for _, raw := range mapSlice(data, "results") {
		item, ok := raw.(map[string]any)
		if !ok || !mapBool(item, "ok") {
			continue
		}
		rel := mapMap(item, "relationship")
		if rel == nil {
			continue // malformed result — no relationship payload, skip
		}
		a.RelationshipsCreated++
		if id := mapStr(rel, "id"); id != "" {
			addID(&a.CreatedRelationshipIDs, id)
		}
		if t := mapStr(rel, "type"); t != "" {
			addType(&a.DiscoveredTypes, t)
		}
	}
}

// parseEntityDelete counts an entity-delete tool call output.
//
// Entity-delete emits the uniform envelope with the deleted id nested under
// data.entity_id — {"ok": true, "data": {"entity_id": "..."}}. The id read
// from the output data is authoritative; when absent, fall back to the call's
// input args (entity_id, a UUID or key). Calls without an explicit ok flag are
// skipped (defensive best-effort parsing).
func (a *rememberStatusAggregation) parseEntityDelete(tc *AgentRunToolCall) {
	if !mapBool(tc.Output, "ok") {
		return
	}
	a.ObjectsDeleted++
	id := mapStr(mapMap(tc.Output, "data"), "entity_id")
	if id == "" {
		id = mapStr(tc.Input, "entity_id")
	}
	if id != "" {
		addID(&a.DeletedObjectIDs, id)
	}
}

// parseRelationshipDelete counts a relationship-delete tool call output.
//
// Relationship-delete emits the uniform envelope with the deleted id nested
// under data.relationship_id — {"ok": true, "data": {"relationship_id": "..."}}.
// The id read from the output data is authoritative; fall back to the input
// relationship_id when it is absent.
func (a *rememberStatusAggregation) parseRelationshipDelete(tc *AgentRunToolCall) {
	if !mapBool(tc.Output, "ok") {
		return
	}
	a.RelationshipsDeleted++
	id := mapStr(mapMap(tc.Output, "data"), "relationship_id")
	if id == "" {
		id = mapStr(tc.Input, "relationship_id")
	}
	if id != "" {
		addID(&a.DeletedRelationshipIDs, id)
	}
}

// collectEntity records a created entity's id/type from a slim entity payload.
func (a *rememberStatusAggregation) collectEntity(ent map[string]any) {
	if id := mapStr(ent, "id"); id != "" {
		addID(&a.CreatedObjectIDs, id)
	}
	if t := mapStr(ent, "type"); t != "" {
		addType(&a.DiscoveredTypes, t)
	}
}

// buildSummary produces a short human-readable summary line.
func (a *rememberStatusAggregation) buildSummary() {
	var parts []string
	if a.ObjectsCreated > 0 {
		parts = append(parts, fmt.Sprintf("created %d objects", a.ObjectsCreated))
	}
	if a.ObjectsUpdated > 0 {
		parts = append(parts, fmt.Sprintf("updated %d objects", a.ObjectsUpdated))
	}
	if a.RelationshipsCreated > 0 {
		parts = append(parts, fmt.Sprintf("created %d relationships", a.RelationshipsCreated))
	}
	if a.ObjectsDeleted > 0 {
		parts = append(parts, fmt.Sprintf("deleted %d objects", a.ObjectsDeleted))
	}
	if a.RelationshipsDeleted > 0 {
		parts = append(parts, fmt.Sprintf("deleted %d relationships", a.RelationshipsDeleted))
	}
	if a.FailedToolCalls > 0 {
		parts = append(parts, fmt.Sprintf("%d tool call(s) failed", a.FailedToolCalls))
	}
	if len(parts) == 0 {
		a.Summary = "No graph changes were made by this run."
		return
	}
	a.Summary = strings.Join(parts, "; ") + "."
}

// toMap renders the aggregation with the JSON field names exposed by
// remember-status. Slices are guaranteed non-nil so they marshal as [].
func (a rememberStatusAggregation) toMap() map[string]any {
	if a.CreatedObjectIDs == nil {
		a.CreatedObjectIDs = []string{}
	}
	if a.CreatedRelationshipIDs == nil {
		a.CreatedRelationshipIDs = []string{}
	}
	if a.DeletedObjectIDs == nil {
		a.DeletedObjectIDs = []string{}
	}
	if a.DeletedRelationshipIDs == nil {
		a.DeletedRelationshipIDs = []string{}
	}
	if a.DiscoveredTypes == nil {
		a.DiscoveredTypes = []string{}
	}
	return map[string]any{
		"objects_created":          a.ObjectsCreated,
		"objects_updated":          a.ObjectsUpdated,
		"relationships_created":    a.RelationshipsCreated,
		"objects_deleted":          a.ObjectsDeleted,
		"relationships_deleted":    a.RelationshipsDeleted,
		"created_object_ids":       a.CreatedObjectIDs,
		"created_relationship_ids": a.CreatedRelationshipIDs,
		"deleted_object_ids":       a.DeletedObjectIDs,
		"deleted_relationship_ids": a.DeletedRelationshipIDs,
		"discovered_types":         a.DiscoveredTypes,
		"summary":                  a.Summary,
	}
}

// Defensive map accessors — tolerate missing/wrong-typed fields.

func mapStr(m map[string]any, key string) string {
	if v, ok := m[key].(string); ok {
		return v
	}
	return ""
}

func mapBool(m map[string]any, key string) bool {
	if v, ok := m[key].(bool); ok {
		return v
	}
	return false
}

func mapMap(m map[string]any, key string) map[string]any {
	if v, ok := m[key].(map[string]any); ok {
		return v
	}
	return nil
}

func mapSlice(m map[string]any, key string) []any {
	if v, ok := m[key].([]any); ok {
		return v
	}
	return nil
}

// addID appends a non-empty id, deduplicating.
func addID(ids *[]string, id string) {
	id = strings.TrimSpace(id)
	if id == "" {
		return
	}
	for _, existing := range *ids {
		if existing == id {
			return
		}
	}
	*ids = append(*ids, id)
}

// addType appends a non-empty type, deduplicating.
func addType(types *[]string, t string) {
	t = strings.TrimSpace(t)
	if t == "" {
		return
	}
	for _, existing := range *types {
		if existing == t {
			return
		}
	}
	*types = append(*types, t)
}

// rememberStatusFromRunStatus maps a run's lifecycle status to the three-state
// remember-status vocabulary (running/completed/failed).
func rememberStatusFromRunStatus(s AgentRunStatus) (status string, terminal bool, errMsg string) {
	switch s {
	case RunStatusSuccess:
		return "completed", true, ""
	case RunStatusError:
		return "failed", true, ""
	case RunStatusSkipped:
		return "completed", true, ""
	case RunStatusCancelled:
		return "failed", true, "run was cancelled"
	default: // submitted, working, input-required, cancelling
		return "running", false, ""
	}
}

// ============================================================================
// MCP tool handler
// ============================================================================

// errRunNotFound is returned by buildRememberStatus when no agent run exists for
// the given run_id/project_id pair. It is wrapped with the run id for the
// message (e.g. "agent run not found: <id>"); callers detect the not-found case
// via errors.Is.
var errRunNotFound = errors.New("agent run not found")

// buildRememberStatus reports the completion state and graph-mutation summary
// of an async remember/forget run, derived from the run's recorded tool calls
// AND the async extraction jobs it queued via queue-reextraction (the actual
// mutation path for the primary remember flow). It returns the raw result map
// shared by the remember-status MCP tool and the REST route.
// Follows the same validate → lookup → respond pattern as ExecuteGetRunStatus.
// The run_id is validated first; errRunNotFound (wrapped with the id) is
// returned when no run matches.
func (h *MCPToolHandler) buildRememberStatus(ctx context.Context, projectID, runID string) (map[string]any, error) {
	if runID == "" {
		return nil, errors.New("run_id is required")
	}

	run, err := h.repo.FindRunByIDForProject(ctx, runID, projectID)
	if err != nil {
		return nil, fmt.Errorf("failed to get run status: %w", err)
	}
	if run == nil {
		return nil, fmt.Errorf("%w: %s", errRunNotFound, runID)
	}

	toolCalls, err := h.repo.FindToolCallsByRunID(ctx, runID)
	if err != nil {
		return nil, fmt.Errorf("failed to get run tool calls: %w", err)
	}

	// Aggregate direct graph-mutating tool calls (entity-create, ...).
	agg := aggregateRememberStatus(toolCalls)

	// Follow queue-reextraction calls to the async extraction jobs they queued.
	// The actual graph mutations for the primary remember flow happen there,
	// AFTER the agent run itself completes.
	jobIDs := reextractionJobIDs(toolCalls)
	var jobs []*mcp.ExtractionJobInfo
	if len(jobIDs) > 0 {
		if h.extractionJobs == nil {
			h.log.Warn("remember-status: extraction job finder not configured; cannot follow queue-reextraction jobs",
				slog.String("run_id", runID))
		} else {
			for _, jid := range jobIDs {
				job, ferr := h.extractionJobs.FindByID(ctx, jid)
				if ferr != nil {
					h.log.Warn("remember-status: failed to look up extraction job",
						slog.String("job_id", jid), slog.String("run_id", runID), logger.Error(ferr))
					continue
				}
				if job == nil {
					// Job row not visible yet — treat as in-flight so we never
					// claim completion before extraction has actually run.
					jobs = append(jobs, &mcp.ExtractionJobInfo{ID: jid, Status: jobStatusPending})
					continue
				}
				jobs = append(jobs, job)
			}
		}
	}
	jobAgg, jobsPending, jobFailures, jobsStaleReaped := aggregateExtractionJobs(jobs)
	agg.merge(jobAgg)

	// Track embedding generation for the created objects — a third async stage.
	// Embedding readiness does NOT gate the "completed" status (the memorize
	// operation itself succeeded once graph mutation is done); it is reported as
	// a separate signal callers can poll on for full recall-readiness.
	createdIDs := agg.CreatedObjectIDs
	var embSummary embeddingStatusSummary
	embTracked := false
	if len(createdIDs) > 0 {
		if h.embeddingJobs == nil {
			h.log.Warn("remember-status: embedding job finder not configured; cannot report embedding readiness",
				slog.String("run_id", runID))
		} else {
			infos, eerr := h.embeddingJobs.FindByObjectIDs(ctx, createdIDs)
			if eerr != nil {
				h.log.Warn("remember-status: failed to look up embedding jobs",
					slog.String("run_id", runID), logger.Error(eerr))
			} else {
				embSummary = aggregateEmbeddingStatus(infos)
				embTracked = true
			}
		}
	}

	agentErr := ""
	if run.ErrorMessage != nil {
		agentErr = *run.ErrorMessage
	}
	status, statusErr := overallRememberStatus(run.Status, agentErr, jobsPending, jobFailures)

	result := map[string]any{"run_id": run.ID}
	for k, v := range agg.toMap() {
		result[k] = v
	}
	result["status"] = status
	// stale_jobs counts extraction jobs reaped by the stale-job sweep: terminal
	// but not a current failure. Always present so callers get a stable shape;
	// mirrors the sibling stale_failed counter (#763/#772).
	result["stale_jobs"] = jobsStaleReaped

	summary := agg.Summary
	if embTracked {
		result["embeddings_pending"] = embSummary.Pending
		result["embeddings_failed"] = embSummary.Failed
		result["embeddings_stale"] = embSummary.Stale
		result["embeddings_ready"] = embSummary.Pending == 0
		summary += embeddingSummaryNote(embSummary.Pending, embSummary.Failed, len(createdIDs))
	}

	if status == "running" {
		result["partial"] = true
		prefix := "Run is still in progress — counts are partial."
		if jobsPending {
			prefix = "Extraction is still in progress — counts are partial."
		}
		result["summary"] = prefix + " " + summary
	} else if status == "failed" {
		result["error"] = statusErr
		result["summary"] = summary
	} else {
		result["summary"] = summary
	}

	return result, nil
}

// ExecuteRememberStatus is the MCP tool entrypoint for remember-status. It
// validates run_id presence and wraps the shared buildRememberStatus result
// (or error) into an MCP ToolResult.
func (h *MCPToolHandler) ExecuteRememberStatus(ctx context.Context, projectID string, args map[string]any) (*mcp.ToolResult, error) {
	runID, _ := args["run_id"].(string)
	result, err := h.buildRememberStatus(ctx, projectID, runID)
	if err != nil {
		return errResult(err.Error())
	}
	return wrapResult(result)
}
