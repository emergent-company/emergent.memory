package main

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
)

// --- schema discovery jobs ---
//
// Discovery is the one API area that answers with snake_case keys, because its
// DTOs predate the camelCase convention used by the rest of the public API. The
// client mirrors those tags rather than normalising them.

// DiscoveryJob mirrors memory's discovery-jobs JobListItem: one schema
// discovery run, including the terminal failure's reason.
type DiscoveryJob struct {
	ID           string  `json:"id"`
	Status       string  `json:"status"`
	CreatedAt    string  `json:"created_at"`
	CompletedAt  *string `json:"completed_at,omitempty"`
	SchemaID     *string `json:"schema_id,omitempty"`
	ErrorMessage *string `json:"error_message,omitempty"`
	// DiscoveredTypes is the proposed type list. The page only counts it; the
	// full payload is deliberately not rendered.
	DiscoveredTypes []any `json:"discovered_types"`
}

// FailureText returns the job's terminal error, or "" when it has none. It is
// deliberately not named Error: a type with an Error method looks like an error
// value to readers and linters.
func (j DiscoveryJob) FailureText() string {
	if j.ErrorMessage == nil {
		return ""
	}
	return *j.ErrorMessage
}

// CanRetry reports whether the run stopped at a terminal failure the user can
// retry (a cancelled run keeps its configuration too).
func (j DiscoveryJob) CanRetry() bool {
	return j.Status == "failed" || j.Status == "cancelled"
}

// ListDiscoveryJobs fetches the project's discovery runs, newest first
// (GET /api/discovery-jobs/projects/{projectId}).
func (m *MemoryClient) ListDiscoveryJobs(ctx context.Context) ([]DiscoveryJob, error) {
	projectID := m.projectIDFor(ctx)
	if projectID == "" {
		return nil, nil
	}
	var out []DiscoveryJob
	if err := m.do(ctx, http.MethodGet, "/api/discovery-jobs/projects/"+url.PathEscape(projectID), nil, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// RetryDiscoveryJob starts a new discovery run from a failed job's recorded
// configuration (POST /api/discovery-jobs/{jobId}/retry). The failed attempt is
// kept as history and the new job id is returned.
func (m *MemoryClient) RetryDiscoveryJob(ctx context.Context, jobID string) (string, error) {
	if jobID == "" {
		return "", fmt.Errorf("retry discovery: a job id is required")
	}
	var out struct {
		JobID string `json:"job_id"`
	}
	path := "/api/discovery-jobs/" + url.PathEscape(jobID) + "/retry"
	if err := m.do(ctx, http.MethodPost, path, nil, &out); err != nil {
		return "", err
	}
	return out.JobID, nil
}
