package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
)

// --- blueprint API client types (mirror Emergent Memory's /api/blueprints) ---

// BlueprintRecord mirrors memory's Blueprint entity returned by the
// /api/blueprints endpoints. CreatedAt/UpdatedAt are kept as strings because
// the gateway never reads them.
type BlueprintRecord struct {
	ID          string          `json:"id"`
	Name        string          `json:"name"`
	Version     string          `json:"version"`
	Description string          `json:"description"`
	Author      string          `json:"author"`
	Status      string          `json:"status"`
	Manifest    json.RawMessage `json:"manifest"`
	Checksum    string          `json:"checksum"`
	ProjectID   *string         `json:"projectId,omitempty"`
	Origin      string          `json:"origin"`
	CreatedAt   string          `json:"created_at"`
	UpdatedAt   string          `json:"updated_at"`
}

// BlueprintPreview is the side-effect-free preview returned by POST
// /api/blueprints/import/preview: the blueprint's metadata plus its raw
// manifest, without creating or publishing a row. The gateway renders the
// model-mapping page from this before committing to an import.
type BlueprintPreview struct {
	Name        string          `json:"name"`
	Version     string          `json:"version"`
	Description string          `json:"description"`
	Author      string          `json:"author"`
	Manifest    json.RawMessage `json:"manifest"`
}

// BlueprintApplyResult mirrors memory's ApplyResult: per-entity-type
// materialization counts for one blueprint apply.
type BlueprintApplyResult struct {
	BlueprintID   string               `json:"blueprintId"`
	BlueprintName string               `json:"blueprintName"`
	Version       string               `json:"version"`
	Checksum      string               `json:"checksum"`
	Packs         BlueprintApplyCounts `json:"packs"`
	Agents        BlueprintApplyCounts `json:"agents"`
	Skills        BlueprintApplyCounts `json:"skills"`
	Seed          BlueprintApplyCounts `json:"seed"`
}

// BlueprintApplyCounts reports created/updated/skipped for one entity type.
type BlueprintApplyCounts struct {
	Created int `json:"created"`
	Updated int `json:"updated"`
	Skipped int `json:"skipped"`
}

// AppliedBlueprint is the project-scoped view of an applied blueprint, returned
// by GET /api/blueprints/applied (memory PR #330). Mirrors memory's
// AppliedBlueprint response shape. AppliedAt is kept as a string because the
// gateway never parses it.
type AppliedBlueprint struct {
	BlueprintID string `json:"blueprintId"`
	Name        string `json:"name"`
	Version     string `json:"version"`
	Description string `json:"description,omitempty"`
	Author      string `json:"author,omitempty"`
	Checksum    string `json:"checksum"`
	AppliedAt   string `json:"appliedAt"`
	// SourceURL/SourceRef carry the GitHub provenance recorded at import time;
	// both empty for bundled/registry packs. UpdateAvailable, LatestVersion and
	// LastCheckedAt are the latest update-check outcome for a GitHub-imported
	// blueprint (see BlueprintUpdateCheck). LastCheckedAt is kept as a string
	// because the gateway never parses it.
	SourceURL       string `json:"sourceUrl,omitempty"`
	SourceRef       string `json:"sourceRef,omitempty"`
	UpdateAvailable bool   `json:"updateAvailable"`
	LatestVersion   string `json:"latestVersion,omitempty"`
	LastCheckedAt   string `json:"lastCheckedAt,omitempty"`
	// LatestJob is not part of the /applied wire response: the page handler
	// fills it from GET /api/blueprints/:name/updates so the Update control can
	// reflect queued/running/succeeded/failed state across reloads.
	LatestJob *BlueprintUpdateJob `json:"-"`
}

// BlueprintUpdateCheck mirrors memory's update-check record returned by
// GET /api/blueprints/:name/updates: the latest check for one applied,
// GitHub-imported blueprint in a project, plus the latest update/check job.
// LastCheckedAt is kept as a string because the gateway never parses it.
type BlueprintUpdateCheck struct {
	BlueprintName   string `json:"blueprintName"`
	SourceURL       string `json:"sourceUrl,omitempty"`
	SourceRef       string `json:"sourceRef,omitempty"`
	LatestVersion   string `json:"latestVersion,omitempty"`
	LatestChecksum  string `json:"latestChecksum,omitempty"`
	UpdateAvailable bool   `json:"updateAvailable"`
	LastCheckedAt   string `json:"lastCheckedAt,omitempty"`
	LastCheckError  string `json:"lastCheckError,omitempty"`
	// LatestJob is the most recent update or check job for this blueprint, when
	// one has been enqueued (nil before the first job).
	LatestJob *BlueprintUpdateJob `json:"latestJob,omitempty"`
}

// BlueprintUpdateJob mirrors memory's durable update job returned by the
// check/apply endpoints and embedded as latestJob in the update-status
// response. Timestamps and Result are kept raw because the gateway only reads
// status/lastError and the result's noop flag.
type BlueprintUpdateJob struct {
	ID            string          `json:"id"`
	ProjectID     string          `json:"projectId,omitempty"`
	BlueprintName string          `json:"blueprintName,omitempty"`
	Action        string          `json:"action,omitempty"`
	Status        string          `json:"status"`
	Attempts      int             `json:"attempts"`
	LastError     string          `json:"lastError,omitempty"`
	Result        json.RawMessage `json:"result,omitempty"`
	CreatedAt     string          `json:"createdAt,omitempty"`
	StartedAt     string          `json:"startedAt,omitempty"`
	FinishedAt    string          `json:"finishedAt,omitempty"`
}

// Update job action and status values, mirroring memory's blueprints domain.
const (
	blueprintUpdateJobActionCheck  = "check"
	blueprintUpdateJobActionUpdate = "update"

	blueprintUpdateJobStatusPending   = "pending"
	blueprintUpdateJobStatusRunning   = "running"
	blueprintUpdateJobStatusSucceeded = "succeeded"
	blueprintUpdateJobStatusFailed    = "failed"
)

// BlueprintUnapplyCounts reports per-entity-type reversal counts for one
// blueprint unapply.
type BlueprintUnapplyCounts struct {
	Removed int `json:"removed"`
	Missing int `json:"missing"`
	Skipped int `json:"skipped"`
}

// BlueprintUnapplyResult mirrors memory's UnapplyResult (POST
// /api/blueprints/:id/unapply).
type BlueprintUnapplyResult struct {
	BlueprintID      string                 `json:"blueprintId"`
	BlueprintName    string                 `json:"blueprintName"`
	Version          string                 `json:"version"`
	Checksum         string                 `json:"checksum"`
	Drift            bool                   `json:"drift"`
	AlreadyUnapplied bool                   `json:"alreadyUnapplied"`
	DriftedAgents    []string               `json:"driftedAgents,omitempty"`
	Packs            BlueprintUnapplyCounts `json:"packs"`
	Agents           BlueprintUnapplyCounts `json:"agents"`
	Skills           BlueprintUnapplyCounts `json:"skills"`
	SkippedSkills    []string               `json:"skippedSkills,omitempty"`
	Seed             BlueprintUnapplyCounts `json:"seed"`
	Status           string                 `json:"status"`
}

// createBlueprintRequest is the body for POST /api/blueprints.
type createBlueprintRequest struct {
	Name        string          `json:"name"`
	Version     string          `json:"version"`
	Description string          `json:"description,omitempty"`
	Author      string          `json:"author,omitempty"`
	Manifest    json.RawMessage `json:"manifest"`
}

// importBlueprintRequest is the body for POST /api/blueprints/import. Token is
// an optional private-repo credential: it is sent only to the memory backend
// and is never logged.
type importBlueprintRequest struct {
	URL string `json:"url"`
	Ref string `json:"ref,omitempty"`
	// Token is an optional private-repo credential: it is sent only to the
	// memory backend and is never logged.
	Token string `json:"token,omitempty"`
	// ModelOverrides maps an agent name to a "provider/model" string, resolving
	// models the project can't map on its own. An empty value clears the model
	// so the agent falls back to the project default.
	ModelOverrides map[string]string `json:"modelOverrides,omitempty"`
	// Apply makes the import create+publish AND apply in one step; on an apply
	// failure memory deletes the created row so a retry is clean.
	Apply bool `json:"apply,omitempty"`
}

// setGithubCredentialRequest is the body for
// PUT /api/blueprints/github-credentials. Token is a private-repo credential:
// it is sent only to the memory backend and is never logged or echoed.
type setGithubCredentialRequest struct {
	Token string `json:"token"`
}

// --- MemoryClient blueprint methods ---

// ListBlueprintVersions returns all versions of a blueprint name
// (GET /api/blueprints/name/:name/versions). Not project-scoped.
func (m *MemoryClient) ListBlueprintVersions(ctx context.Context, name string) ([]BlueprintRecord, error) {
	var out []BlueprintRecord
	if err := m.do(ctx, http.MethodGet, "/api/blueprints/name/"+url.PathEscape(name)+"/versions", nil, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// ListBlueprints returns all blueprints visible to the project: global/shared
// blueprints plus the project's own (GET /api/blueprints). Project-scoped via
// X-Project-ID.
func (m *MemoryClient) ListBlueprints(ctx context.Context) ([]BlueprintRecord, error) {
	var out []BlueprintRecord
	if err := m.doH(ctx, http.MethodGet, "/api/blueprints", nil, m.documentHeaders(ctx), &out); err != nil {
		return nil, err
	}
	return out, nil
}

// CreateBlueprint creates a draft blueprint (POST /api/blueprints) and returns
// the created record. 409 on name+version collision.
func (m *MemoryClient) CreateBlueprint(ctx context.Context, req *createBlueprintRequest) (*BlueprintRecord, error) {
	var out BlueprintRecord
	if err := m.do(ctx, http.MethodPost, "/api/blueprints", req, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// ImportBlueprint imports a blueprint from a GitHub repository URL
// (POST /api/blueprints/import) and returns the created (published) record.
// The optional token is a private-repo credential; it is never logged.
func (m *MemoryClient) ImportBlueprint(ctx context.Context, url, ref, token string) (*BlueprintRecord, error) {
	req := importBlueprintRequest{URL: url, Ref: ref, Token: token}
	var out BlueprintRecord
	if err := m.doH(ctx, http.MethodPost, "/api/blueprints/import", req, m.documentHeaders(ctx), &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// PreviewImportBlueprint fetches a blueprint's metadata and manifest without
// creating or publishing anything (POST /api/blueprints/import/preview). It is
// project-scoped (same headers as import) so the gateway can render the
// model-mapping step before committing to an import. The optional token is a
// private-repo credential; it is never logged.
func (m *MemoryClient) PreviewImportBlueprint(ctx context.Context, url, ref, token string) (*BlueprintPreview, error) {
	req := importBlueprintRequest{URL: url, Ref: ref, Token: token}
	var out BlueprintPreview
	if err := m.doH(ctx, http.MethodPost, "/api/blueprints/import/preview", req, m.documentHeaders(ctx), &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// ImportAndApplyBlueprint imports a blueprint and applies it in one step
// (POST /api/blueprints/import with apply:true), passing the resolved model
// overrides. The optional token is a private-repo credential; it is never
// logged.
func (m *MemoryClient) ImportAndApplyBlueprint(ctx context.Context, url, ref, token string, overrides map[string]string) (*BlueprintRecord, error) {
	req := importBlueprintRequest{URL: url, Ref: ref, Token: token, Apply: true, ModelOverrides: overrides}
	var out BlueprintRecord
	if err := m.doH(ctx, http.MethodPost, "/api/blueprints/import", req, m.documentHeaders(ctx), &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// GetBlueprint fetches one blueprint by id (GET /api/blueprints/:id).
func (m *MemoryClient) GetBlueprint(ctx context.Context, id string) (*BlueprintRecord, error) {
	var out BlueprintRecord
	if err := m.do(ctx, http.MethodGet, "/api/blueprints/"+url.PathEscape(id), nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// PublishBlueprint transitions a draft blueprint to published
// (POST /api/blueprints/:id/publish).
func (m *MemoryClient) PublishBlueprint(ctx context.Context, id string) error {
	return m.do(ctx, http.MethodPost, "/api/blueprints/"+url.PathEscape(id)+"/publish", nil, nil)
}

// ApplyBlueprint materializes a blueprint into the project
// (POST /api/blueprints/:id/apply). Project-scoped via X-Project-ID.
func (m *MemoryClient) ApplyBlueprint(ctx context.Context, id string) (*BlueprintApplyResult, error) {
	var out BlueprintApplyResult
	// Empty object body — apply has no required fields; an empty body would
	// fail echo's JSON binding.
	if err := m.doH(ctx, http.MethodPost, "/api/blueprints/"+url.PathEscape(id)+"/apply", map[string]any{}, m.documentHeaders(ctx), &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// ApplyBlueprintWithOverrides materializes a blueprint into the project with
// explicit model overrides (POST /api/blueprints/:id/apply). Project-scoped via
// X-Project-ID. The overrides map an agent name to a "provider/model" reference
// (empty value = project default); the server persists them per-project after a
// successful apply.
func (m *MemoryClient) ApplyBlueprintWithOverrides(ctx context.Context, id string, overrides map[string]string) (*BlueprintApplyResult, error) {
	var out BlueprintApplyResult
	body := map[string]any{"modelOverrides": overrides}
	if err := m.doH(ctx, http.MethodPost, "/api/blueprints/"+url.PathEscape(id)+"/apply", body, m.documentHeaders(ctx), &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// ModelOverridesResponse is the body for GET /api/blueprints/model-overrides:
// the project-scoped agent-name -> "provider/model" map for one blueprint name.
type ModelOverridesResponse struct {
	ModelOverrides map[string]string `json:"modelOverrides"`
}

// GetModelOverrides returns the project-scoped model overrides for one
// blueprint name (GET /api/blueprints/model-overrides?name=<name>). Project-
// scoped via X-Project-ID.
func (m *MemoryClient) GetModelOverrides(ctx context.Context, name string) (map[string]string, error) {
	var out ModelOverridesResponse
	if err := m.doH(ctx, http.MethodGet, "/api/blueprints/model-overrides?name="+url.QueryEscape(name), nil, m.documentHeaders(ctx), &out); err != nil {
		return nil, err
	}
	return out.ModelOverrides, nil
}

// ListAppliedBlueprints returns the blueprints applied to the project
// (GET /api/blueprints/applied). Project-scoped via X-Project-ID. Requires
// memory's /applied endpoint (PR #330); returns an error until that ships.
func (m *MemoryClient) ListAppliedBlueprints(ctx context.Context) ([]AppliedBlueprint, error) {
	var out []AppliedBlueprint
	if err := m.doH(ctx, http.MethodGet, "/api/blueprints/applied", nil, m.documentHeaders(ctx), &out); err != nil {
		return nil, err
	}
	return out, nil
}

// UnapplyBlueprint reverses an apply for the project
// (POST /api/blueprints/:id/unapply). Project-scoped via X-Project-ID.
func (m *MemoryClient) UnapplyBlueprint(ctx context.Context, id string) (*BlueprintUnapplyResult, error) {
	var out BlueprintUnapplyResult
	if err := m.doH(ctx, http.MethodPost, "/api/blueprints/"+url.PathEscape(id)+"/unapply", map[string]any{}, m.documentHeaders(ctx), &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// GetBlueprintUpdates returns the latest update-check result for an applied
// blueprint by name (GET /api/blueprints/:name/updates). Project-scoped via
// X-Project-ID.
func (m *MemoryClient) GetBlueprintUpdates(ctx context.Context, name string) (*BlueprintUpdateCheck, error) {
	var out BlueprintUpdateCheck
	if err := m.doH(ctx, http.MethodGet, "/api/blueprints/"+url.PathEscape(name)+"/updates", nil, m.documentHeaders(ctx), &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// CheckBlueprintUpdates enqueues an on-demand update check for an applied
// blueprint by name (POST /api/blueprints/:name/updates/check) and returns the
// queued job. The check runs asynchronously in memory. Project-scoped via
// X-Project-ID.
func (m *MemoryClient) CheckBlueprintUpdates(ctx context.Context, name string) (*BlueprintUpdateJob, error) {
	var out BlueprintUpdateJob
	// Empty object body — the check has no required fields; an empty body would
	// fail echo's JSON binding.
	if err := m.doH(ctx, http.MethodPost, "/api/blueprints/"+url.PathEscape(name)+"/updates/check", map[string]any{}, m.documentHeaders(ctx), &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// ApplyBlueprintUpdate enqueues an asynchronous apply of a detected upstream
// update for an applied blueprint by name (POST /api/blueprints/:name/updates/apply)
// and returns the queued job. The apply runs in memory; a duplicate enqueue
// while one is pending/running is rejected with 409. Project-scoped via
// X-Project-ID.
func (m *MemoryClient) ApplyBlueprintUpdate(ctx context.Context, name string) (*BlueprintUpdateJob, error) {
	var out BlueprintUpdateJob
	// Empty object body — apply has no required fields; an empty body would
	// fail echo's JSON binding.
	if err := m.doH(ctx, http.MethodPost, "/api/blueprints/"+url.PathEscape(name)+"/updates/apply", map[string]any{}, m.documentHeaders(ctx), &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// SetGithubCredential stores (or replaces) the project's encrypted GitHub
// personal access token (PUT /api/blueprints/github-credentials). The token is
// forwarded to memory only; it is never logged and memory never echoes it.
// Project-scoped via X-Project-ID.
func (m *MemoryClient) SetGithubCredential(ctx context.Context, token string) error {
	req := setGithubCredentialRequest{Token: token}
	return m.doH(ctx, http.MethodPut, "/api/blueprints/github-credentials", req, m.documentHeaders(ctx), nil)
}

// DeleteBlueprint deletes a private, unapplied blueprint
// (DELETE /api/blueprints/:id). Memory rejects the deletion with 409 when the
// blueprint is applied or is a global built-in.
func (m *MemoryClient) DeleteBlueprint(ctx context.Context, id string) error {
	return m.do(ctx, http.MethodDelete, "/api/blueprints/"+url.PathEscape(id), nil, nil)
}
