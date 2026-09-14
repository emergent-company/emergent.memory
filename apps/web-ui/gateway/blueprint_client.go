package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log"
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
	CreatedAt   string          `json:"created_at"`
	UpdatedAt   string          `json:"updated_at"`
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
}

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

// --- install primitive ---

// ensureBlueprintApplied builds a manifest from a bundled pack and installs it
// through the blueprint API.
func (s *Server) ensureBlueprintApplied(ctx context.Context, bp *BundledBlueprint) (*BlueprintApplyResult, error) {
	manifest, err := buildBlueprintManifest(bp)
	if err != nil {
		return nil, err
	}
	return s.ensureManifestApplied(ctx, bp.Name, bp.Version, bp.Description, bp.Author, manifest)
}

// ensureManifestApplied is the idempotent install primitive: it resolves or
// creates the blueprint definition at name/version, publishes it if still
// draft, then applies it to the project. Re-applying an already-applied version
// is safe (memory's apply is create-or-update by name/key), so this converges
// on retry after a mid-sequence failure.
func (s *Server) ensureManifestApplied(ctx context.Context, name, version, description, author string, manifest json.RawMessage) (*BlueprintApplyResult, error) {
	id, err := s.resolveOrCreateBlueprint(ctx, name, version, description, author, manifest)
	if err != nil {
		return nil, err
	}

	rec, err := s.memory.GetBlueprint(ctx, id)
	if err != nil {
		return nil, err
	}
	if rec.Status == "draft" {
		if err := s.memory.PublishBlueprint(ctx, id); err != nil {
			return nil, err
		}
	}

	return s.memory.ApplyBlueprint(ctx, id)
}

// resolveOrCreateBlueprint returns the blueprint id for name/version, creating
// it (draft) if it does not already exist. A 409 conflict from a concurrent
// create is resolved by re-listing and adopting the winner, so the create is
// never assumed fresh.
func (s *Server) resolveOrCreateBlueprint(ctx context.Context, name, version, description, author string, manifest json.RawMessage) (string, error) {
	versions, err := s.memory.ListBlueprintVersions(ctx, name)
	if err != nil {
		return "", err
	}
	for _, v := range versions {
		if v.Version == version {
			// Detect manifest drift: the bundled yaml changed without a version
			// bump, so the stored (published, immutable) blueprint is stale.
			// Surface loudly; the fix is to bump the bundle version. Global
			// published blueprints are immutable in memory, so we cannot
			// silently refresh the manifest here.
			if v.Checksum != "" && manifestChecksum(manifest) != v.Checksum {
				log.Printf("blueprint %s@%s: bundled manifest differs from stored definition (checksum mismatch); bump the version to apply changes (blueprint %s)", name, version, v.ID)
			}
			return v.ID, nil
		}
	}

	rec, err := s.memory.CreateBlueprint(ctx, &createBlueprintRequest{
		Name:        name,
		Version:     version,
		Description: description,
		Author:      author,
		Manifest:    manifest,
	})
	if err == nil {
		return rec.ID, nil
	}
	if !isMemoryStatus(err, http.StatusConflict) {
		return "", err
	}

	// Concurrent create won the race — adopt the existing row.
	versions, err = s.memory.ListBlueprintVersions(ctx, name)
	if err != nil {
		return "", err
	}
	for _, v := range versions {
		if v.Version == version {
			// Detect manifest drift: the bundled yaml changed without a version
			// bump, so the stored (published, immutable) blueprint is stale.
			// Surface loudly; the fix is to bump the bundle version. Global
			// published blueprints are immutable in memory, so we cannot
			// silently refresh the manifest here.
			if v.Checksum != "" && manifestChecksum(manifest) != v.Checksum {
				log.Printf("blueprint %s@%s: bundled manifest differs from stored definition (checksum mismatch); bump the version to apply changes (blueprint %s)", name, version, v.ID)
			}
			return v.ID, nil
		}
	}
	return "", fmt.Errorf("blueprint %s@%s exists but was not found on re-list", name, version)
}

// manifestChecksum computes the sha256 hex checksum of a manifest, matching
// memory's blueprint publish checksum (sha256 over the raw manifest JSON bytes).
func manifestChecksum(manifest json.RawMessage) string {
	sum := sha256.Sum256(manifest)
	return hex.EncodeToString(sum[:])
}

// installRegistryBlueprint fetches a registry schema, builds a blueprint
// manifest from it, and installs it through the blueprint API (create →
// publish → apply). This migrates registry packs off the legacy
// schema-assignment path.
func (s *Server) installRegistryBlueprint(ctx context.Context, schemaID string) (*BlueprintApplyResult, error) {
	sch, err := s.memory.GetSchema(ctx, schemaID)
	if err != nil {
		return nil, err
	}
	manifest, err := buildRegistryManifest(sch)
	if err != nil {
		return nil, err
	}
	return s.ensureManifestApplied(ctx, sch.Name, sch.Version, sch.Description, sch.Author, manifest)
}
