package main

import (
	"context"
	"encoding/json"
	"net/http"
	"time"
)

// --- schema discovery (kb.discovery_jobs) ---

// discoveryProgress mirrors the progress map memory stores on a job.
type discoveryProgress struct {
	CurrentStep int    `json:"current_step"`
	TotalSteps  int    `json:"total_steps"`
	Message     string `json:"message"`
}

// discoveryJob mirrors memory's JobListItem (GET
// /api/discovery-jobs/projects/:projectId). Discovered types and relationships
// stay raw: the card only counts them, and the full candidate payload belongs
// to the schema page, not to this summary.
type discoveryJob struct {
	ID                      string            `json:"id"`
	Status                  string            `json:"status"`
	Progress                discoveryProgress `json:"progress"`
	CreatedAt               time.Time         `json:"created_at"`
	CompletedAt             *time.Time        `json:"completed_at,omitempty"`
	ErrorMessage            string            `json:"error_message,omitempty"`
	DiscoveredTypes         []json.RawMessage `json:"discovered_types"`
	DiscoveredRelationships []json.RawMessage `json:"discovered_relationships"`
	SchemaID                string            `json:"schema_id,omitempty"`
	Trigger                 string            `json:"trigger,omitempty"`
	DocumentCount           int               `json:"document_count,omitempty"`
	TotalDocuments          int               `json:"total_documents,omitempty"`
	// CancellableFlag is the server's "may this job be cancelled now?" answer.
	// It is a pointer so an older server (which does not send it) falls back to
	// the status list below instead of reading every job as uncancellable.
	CancellableFlag *bool `json:"cancellable,omitempty"`
}

// isRunning reports whether the job is still advancing. The server's terminal
// statuses are completed, failed and cancelled; anything else is in flight.
func (j *discoveryJob) isRunning() bool {
	if j == nil {
		return false
	}
	switch j.Status {
	case "completed", "failed", "cancelled":
		return false
	}
	return true
}

// cancellable reports whether the card may offer Cancel. The server answers it
// (`cancellable` on the job), so the card renders the API's decision instead of
// re-deriving it; a run can be stopped while it is still analysing, but not once
// it is creating its pack, which is the run's last step and writes the pack. The
// status fallback exists only for a server old enough not to send the field.
func (j *discoveryJob) cancellable() bool {
	if j == nil {
		return false
	}
	if j.CancellableFlag != nil {
		return *j.CancellableFlag
	}
	switch j.Status {
	case "pending", "analyzing_documents", "extracting_types", "refining_schemas":
		return true
	}
	return false
}

// capped reports whether the run covered fewer documents than the project has,
// which is how the card tells the user a project-wide run was limited.
func (j *discoveryJob) capped() bool {
	return j != nil && j.TotalDocuments > j.DocumentCount && j.DocumentCount > 0
}

// DiscoveryStartResult is the response for starting or retrying a run
// (memory's StartDiscoveryResponse).
type DiscoveryStartResult struct {
	JobID          string `json:"job_id"`
	DocumentCount  int    `json:"document_count"`
	TotalDocuments int    `json:"total_documents"`
}

// ListDiscoveryJobs lists the project's recent discovery jobs, newest first.
func (m *MemoryClient) ListDiscoveryJobs(ctx context.Context) ([]discoveryJob, error) {
	var out []discoveryJob
	if err := m.doH(ctx, http.MethodGet, "/api/discovery-jobs/projects/"+m.projectIDFor(ctx), nil, sessionHeaders(ctx), &out); err != nil {
		return nil, err
	}
	return out, nil
}

// StartProjectDiscovery starts a project-wide run. scope=project makes the
// server resolve the document set itself (newest first, up to its per-run cap);
// the caller never enumerates ids.
func (m *MemoryClient) StartProjectDiscovery(ctx context.Context) (*DiscoveryStartResult, error) {
	body := map[string]any{"scope": "project"}
	var out DiscoveryStartResult
	if err := m.doH(ctx, http.MethodPost, "/api/discovery-jobs/projects/"+m.projectIDFor(ctx)+"/start", body, sessionHeaders(ctx), &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// StartDocumentDiscovery starts a run over one document.
func (m *MemoryClient) StartDocumentDiscovery(ctx context.Context, documentID string) (*DiscoveryStartResult, error) {
	body := map[string]any{
		"scope":        "documents",
		"document_ids": []string{documentID},
	}
	var out DiscoveryStartResult
	if err := m.doH(ctx, http.MethodPost, "/api/discovery-jobs/projects/"+m.projectIDFor(ctx)+"/start", body, sessionHeaders(ctx), &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// RetryDiscoveryJob re-runs a terminal job over the same documents. The server
// copies the stored config, so the retry cannot change the original scope.
func (m *MemoryClient) RetryDiscoveryJob(ctx context.Context, jobID string) (*DiscoveryStartResult, error) {
	var out DiscoveryStartResult
	if err := m.doH(ctx, http.MethodPost, "/api/discovery-jobs/"+jobID+"/retry", nil, sessionHeaders(ctx), &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// CancelDiscoveryJob stops a run that is in flight. The server treats a
// terminal job as a state conflict (409), which the caller surfaces as a
// message rather than retrying.
func (m *MemoryClient) CancelDiscoveryJob(ctx context.Context, jobID string) error {
	return m.doH(ctx, http.MethodDelete, "/api/discovery-jobs/"+jobID, nil, sessionHeaders(ctx), nil)
}
