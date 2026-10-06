package blueprints

import (
	"context"
	"database/sql"
	"errors"
	"log/slog"
	"time"

	"github.com/uptrace/bun"

	"github.com/emergent-company/emergent.memory/pkg/apperror"
	"github.com/emergent-company/emergent.memory/pkg/logger"
)

// Repository handles database operations for blueprints
type Repository struct {
	db  bun.IDB
	log *slog.Logger
}

// NewRepository creates a new blueprints repository
func NewRepository(db bun.IDB, log *slog.Logger) *Repository {
	return &Repository{
		db:  db,
		log: log.With(logger.Scope("blueprints.repo")),
	}
}

// scopeWhere returns the read-scope WHERE condition and its bind args for the
// caller's project: global blueprints plus the caller's own private ones when
// projectID is non-empty; global-only when projectID is empty.
func scopeWhere(projectID string) (string, []any) {
	if projectID == "" {
		return "project_id IS NULL", nil
	}
	return "(project_id IS NULL OR project_id = ?)", []any{projectID}
}

// Create inserts a new blueprint row.
// Duplicate (name, version) within the blueprint's scope is enforced atomically
// by the database via ON CONFLICT DO NOTHING (backed by the partial unique
// indexes on project_id); RowsAffected == 0 means the row already exists. The
// service's ExistsByNameVersion pre-check is only a friendly fast path, not the
// enforcement.
func (r *Repository) Create(ctx context.Context, bp *Blueprint) error {
	result, err := r.db.NewInsert().
		Model(bp).
		On("CONFLICT DO NOTHING").
		Returning("id").
		Exec(ctx)
	if err != nil {
		r.log.Error("failed to create blueprint", logger.Error(err))
		return apperror.ErrDatabase.WithInternal(err)
	}

	rowsAffected, _ := result.RowsAffected()
	if rowsAffected == 0 {
		return apperror.ErrConflict.WithMessage("blueprint with name and version already exists")
	}

	return nil
}

// GetByID returns a blueprint by ID within the caller's read scope (global +
// own private). A private blueprint owned by another project is not visible.
func (r *Repository) GetByID(ctx context.Context, projectID, id string) (*Blueprint, error) {
	var bp Blueprint
	cond, args := scopeWhere(projectID)
	err := r.db.NewSelect().
		Model(&bp).
		Where("id = ?", id).
		Where(cond, args...).
		Scan(ctx)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, apperror.ErrNotFound.WithMessage("blueprint not found")
		}
		r.log.Error("failed to get blueprint", logger.Error(err))
		return nil, apperror.ErrDatabase.WithInternal(err)
	}
	return &bp, nil
}

// List returns blueprints within the caller's read scope (global + own
// private), optionally filtered by name, ordered by name then version.
func (r *Repository) List(ctx context.Context, projectID, nameFilter string) ([]Blueprint, error) {
	cond, args := scopeWhere(projectID)
	q := r.db.NewSelect().Model((*Blueprint)(nil)).Where(cond, args...)
	if nameFilter != "" {
		q = q.Where("name = ?", nameFilter)
	}

	var blueprints []Blueprint
	err := q.Order("name ASC", "version ASC").Scan(ctx, &blueprints)
	if err != nil {
		r.log.Error("failed to list blueprints", logger.Error(err))
		return nil, apperror.ErrDatabase.WithInternal(err)
	}
	if blueprints == nil {
		return []Blueprint{}, nil
	}
	return blueprints, nil
}

// Update persists the mutable content fields of a draft blueprint in place.
// Status is not touched here; use UpdateStatus for state transitions.
func (r *Repository) Update(ctx context.Context, bp *Blueprint) error {
	now := time.Now()
	result, err := r.db.NewUpdate().
		Model((*Blueprint)(nil)).
		Where("id = ?", bp.ID).
		Set("description = ?", bp.Description).
		Set("author = ?", bp.Author).
		Set("manifest = ?", bp.Manifest).
		Set("updated_at = ?", now).
		Exec(ctx)
	if err != nil {
		r.log.Error("failed to update blueprint", logger.Error(err))
		return apperror.ErrDatabase.WithInternal(err)
	}

	rowsAffected, _ := result.RowsAffected()
	if rowsAffected == 0 {
		return apperror.ErrNotFound.WithMessage("blueprint not found")
	}

	bp.UpdatedAt = now
	return nil
}

// Delete removes a blueprint row by ID.
func (r *Repository) Delete(ctx context.Context, id string) error {
	result, err := r.db.NewDelete().
		Model((*Blueprint)(nil)).
		Where("id = ?", id).
		Exec(ctx)
	if err != nil {
		r.log.Error("failed to delete blueprint", logger.Error(err))
		return apperror.ErrDatabase.WithInternal(err)
	}

	rowsAffected, _ := result.RowsAffected()
	if rowsAffected == 0 {
		return apperror.ErrNotFound.WithMessage("blueprint not found")
	}

	return nil
}

// ListVersionsByName returns all versions of a blueprint name within the
// caller's read scope (global + own private), ordered by version.
func (r *Repository) ListVersionsByName(ctx context.Context, projectID, name string) ([]Blueprint, error) {
	cond, args := scopeWhere(projectID)
	var blueprints []Blueprint
	err := r.db.NewSelect().
		Model(&blueprints).
		Where("name = ?", name).
		Where(cond, args...).
		Order("version ASC").
		Scan(ctx)
	if err != nil {
		r.log.Error("failed to list blueprint versions", logger.Error(err))
		return nil, apperror.ErrDatabase.WithInternal(err)
	}
	if blueprints == nil {
		return []Blueprint{}, nil
	}
	return blueprints, nil
}

// UpdateStatus transitions a blueprint's status and sets its checksum.
func (r *Repository) UpdateStatus(ctx context.Context, id, status, checksum string) error {
	result, err := r.db.NewUpdate().
		Model((*Blueprint)(nil)).
		Where("id = ?", id).
		Set("status = ?", status).
		Set("checksum = ?", checksum).
		Set("updated_at = ?", time.Now()).
		Exec(ctx)
	if err != nil {
		r.log.Error("failed to update blueprint status", logger.Error(err))
		return apperror.ErrDatabase.WithInternal(err)
	}

	rowsAffected, _ := result.RowsAffected()
	if rowsAffected == 0 {
		return apperror.ErrNotFound.WithMessage("blueprint not found")
	}

	return nil
}

// ExistsByNameVersion reports whether a blueprint with the given name and
// version exists in the given scope: nil projectID checks the global scope
// (project_id IS NULL); a projectID checks only that project's private scope.
// This is a friendly fast-path pre-check; the partial unique indexes backing
// ON CONFLICT in Create are the real TOCTOU-safe enforcement.
func (r *Repository) ExistsByNameVersion(ctx context.Context, projectID *string, name, version string) (bool, error) {
	q := r.db.NewSelect().
		Model((*Blueprint)(nil)).
		Where("name = ?", name).
		Where("version = ?", version)
	if projectID == nil {
		q = q.Where("project_id IS NULL")
	} else {
		q = q.Where("project_id = ?", *projectID)
	}
	exists, err := q.Exists(ctx)
	if err != nil {
		r.log.Error("failed to check blueprint existence", logger.Error(err))
		return false, apperror.ErrDatabase.WithInternal(err)
	}
	return exists, nil
}

// RecordApplication upserts a blueprint application keyed by
// (project_id, blueprint_id). A re-apply updates the existing row in place.
func (r *Repository) RecordApplication(ctx context.Context, app *BlueprintApplication) error {
	_, err := r.db.NewInsert().
		Model(app).
		On("CONFLICT (project_id, blueprint_id) DO UPDATE").
		Set("version = EXCLUDED.version").
		Set("checksum = EXCLUDED.checksum").
		Set("applied_by = EXCLUDED.applied_by").
		Set("applied_at = EXCLUDED.applied_at").
		Set("updated_at = EXCLUDED.updated_at").
		Set("status = EXCLUDED.status").
		Exec(ctx)
	if err != nil {
		r.log.Error("failed to record blueprint application", logger.Error(err))
		return apperror.ErrDatabase.WithInternal(err)
	}
	return nil
}

// SupersedeApplications marks any other application of the same blueprint name
// (a different blueprint_id) in the project as 'superseded'. Called on apply so
// an upgrade (a new version) replaces the previous application instead of
// leaving both listed as applied.
func (r *Repository) SupersedeApplications(ctx context.Context, projectID, blueprintID, name string) error {
	_, err := r.db.NewUpdate().
		Model((*BlueprintApplication)(nil)).
		Where("project_id = ?", projectID).
		Where("blueprint_id <> ?", blueprintID).
		Where("blueprint_id IN (SELECT id FROM kb.blueprints WHERE name = ?)", name).
		Set("status = ?", "superseded").
		Set("updated_at = ?", time.Now()).
		Exec(ctx)
	if err != nil {
		r.log.Error("failed to supersede blueprint applications", logger.Error(err))
		return apperror.ErrDatabase.WithInternal(err)
	}
	return nil
}

// ListApplied returns the blueprints applied to a project, joined with their
// blueprint metadata, ordered by name.
func (r *Repository) ListApplied(ctx context.Context, projectID string) ([]AppliedBlueprint, error) {
	var results []struct {
		BlueprintID string    `bun:"blueprint_id"`
		Name        string    `bun:"name"`
		Version     string    `bun:"version"`
		Description string    `bun:"description"`
		Author      string    `bun:"author"`
		Checksum    string    `bun:"checksum"`
		AppliedAt   time.Time `bun:"applied_at"`
	}
	err := r.db.NewRaw(`
		SELECT bpa.blueprint_id, bp.name, bpa.version, bp.description, bp.author,
		       bpa.checksum, bpa.applied_at
		FROM kb.blueprint_applications bpa
		JOIN kb.blueprints bp ON bp.id = bpa.blueprint_id
		WHERE bpa.project_id = ?
		  AND bpa.status = 'applied'
		ORDER BY bp.name ASC
	`, projectID).Scan(ctx, &results)
	if err != nil {
		r.log.Error("failed to list applied blueprints", logger.Error(err))
		return nil, apperror.ErrDatabase.WithInternal(err)
	}

	out := make([]AppliedBlueprint, len(results))
	for i, res := range results {
		out[i] = AppliedBlueprint{
			BlueprintID: res.BlueprintID,
			Name:        res.Name,
			Version:     res.Version,
			Description: res.Description,
			Author:      res.Author,
			Checksum:    res.Checksum,
			AppliedAt:   res.AppliedAt,
		}
	}
	return out, nil
}

// GetApplication returns the application record for a blueprint+project, or
// ErrNotFound when the blueprint has not been applied to the project.
func (r *Repository) GetApplication(ctx context.Context, projectID, blueprintID string) (*BlueprintApplication, error) {
	var app BlueprintApplication
	err := r.db.NewSelect().
		Model(&app).
		Where("project_id = ?", projectID).
		Where("blueprint_id = ?", blueprintID).
		Scan(ctx)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, apperror.ErrNotFound.WithMessage("blueprint not applied to this project")
		}
		r.log.Error("failed to get blueprint application", logger.Error(err))
		return nil, apperror.ErrDatabase.WithInternal(err)
	}
	return &app, nil
}

// MarkUnapplied flips an application record to status 'unapplied'. Called last
// in Unapply so a mid-unapply crash leaves the record 'applied' and retryable.
// The applied_at guard prevents a stale unapply from overwriting a concurrent
// re-apply (which bumps applied_at).
func (r *Repository) MarkUnapplied(ctx context.Context, projectID, blueprintID string, appliedAt time.Time) error {
	result, err := r.db.NewUpdate().
		Model((*BlueprintApplication)(nil)).
		Where("project_id = ?", projectID).
		Where("blueprint_id = ?", blueprintID).
		Where("status = ?", "applied").
		Where("applied_at = ?", appliedAt).
		Set("status = ?", "unapplied").
		Set("updated_at = ?", time.Now()).
		Exec(ctx)
	if err != nil {
		r.log.Error("failed to mark blueprint application unapplied", logger.Error(err))
		return apperror.ErrDatabase.WithInternal(err)
	}
	rowsAffected, _ := result.RowsAffected()
	if rowsAffected == 0 {
		return apperror.ErrConflict.WithMessage("blueprint application changed concurrently; retry unapply")
	}
	return nil
}

// AddPackClaim records that a blueprint's apply depends on a schema pack for a
// project. Idempotent (INSERT ... ON CONFLICT DO NOTHING) so re-applies and
// retries converge.
func (r *Repository) AddPackClaim(ctx context.Context, blueprintID, projectID, schemaID string) error {
	_, err := r.db.NewInsert().
		Model(&BlueprintPackClaim{BlueprintID: blueprintID, ProjectID: projectID, SchemaID: schemaID}).
		On("CONFLICT (project_id, blueprint_id, schema_id) DO NOTHING").
		Exec(ctx)
	if err != nil {
		r.log.Error("failed to add pack claim", logger.Error(err))
		return apperror.ErrDatabase.WithInternal(err)
	}
	return nil
}

// DeletePackClaim removes a blueprint's pack claim for a project. Idempotent.
func (r *Repository) DeletePackClaim(ctx context.Context, projectID, blueprintID, schemaID string) error {
	_, err := r.db.NewDelete().
		Model((*BlueprintPackClaim)(nil)).
		Where("project_id = ?", projectID).
		Where("blueprint_id = ?", blueprintID).
		Where("schema_id = ?", schemaID).
		Exec(ctx)
	if err != nil {
		r.log.Error("failed to delete pack claim", logger.Error(err))
		return apperror.ErrDatabase.WithInternal(err)
	}
	return nil
}

// DeletePackClaimsForBlueprint removes every pack claim owned by a blueprint,
// across all projects. It is used by the atomic-import rollback BEFORE
// hard-deleting the just-created blueprint row: kb.blueprint_pack_claims
// .blueprint_id has a non-cascading FK to kb.blueprints(id), so any claim
// materialized before an apply failure would make the row delete fail and the
// next import collide on (name, version) with a 409 — the exact symptom this
// rollback exists to prevent. Idempotent (deleting zero rows is not an error).
func (r *Repository) DeletePackClaimsForBlueprint(ctx context.Context, blueprintID string) error {
	if _, err := r.db.NewDelete().
		Model((*BlueprintPackClaim)(nil)).
		Where("blueprint_id = ?", blueprintID).
		Exec(ctx); err != nil {
		r.log.Error("failed to delete blueprint pack claims", logger.Error(err))
		return apperror.ErrDatabase.WithInternal(err)
	}
	return nil
}
