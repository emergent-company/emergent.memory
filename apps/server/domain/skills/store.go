package skills

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"time"

	"github.com/google/uuid"
	"github.com/uptrace/bun"

	"github.com/emergent-company/emergent.memory/pkg/apperror"
	"github.com/emergent-company/emergent.memory/pkg/logger"
	"github.com/emergent-company/emergent.memory/pkg/pgutils"
)

// Repository handles database operations for kb.skills.
type Repository struct {
	db  bun.IDB
	log *slog.Logger
}

// NewRepository creates a new skills repository.
func NewRepository(db bun.IDB, log *slog.Logger) *Repository {
	return &Repository{
		db:  db,
		log: log.With(logger.Scope("skills.repo")),
	}
}

// FindAll returns all skills for listing. If projectID is nil, returns only global skills.
// If projectID is set, returns only project-scoped skills for that project.
func (r *Repository) FindAll(ctx context.Context, projectID *string) ([]*Skill, error) {
	var skills []*Skill
	q := r.db.NewSelect().Model(&skills)
	if projectID == nil {
		q = q.Where("s.project_id IS NULL")
	} else {
		q = q.Where("s.project_id = ?", *projectID)
	}
	q = q.OrderExpr("s.name ASC")
	if err := q.Scan(ctx); err != nil {
		return nil, apperror.NewInternal("failed to list skills", err)
	}
	return skills, nil
}

// FindForAgent returns the merged set of global + project-scoped skills for a given project.
// Project-scoped skills win on name collision (project_id NOT NULL sorts first via NULLS LAST).
// Returns one skill per name: if both global and project exist, project version is returned.
func (r *Repository) FindForAgent(ctx context.Context, projectID string) ([]*Skill, error) {
	var all []*Skill

	// Fetch global skills first, then project skills so we can apply project-wins logic in Go
	// via a single query ordered by project_id NULLS LAST: project rows sort before nulls.
	err := r.db.NewSelect().
		Model(&all).
		Where("s.project_id = ? OR s.project_id IS NULL", projectID).
		OrderExpr("s.project_id NULLS LAST, s.name ASC").
		Scan(ctx)
	if err != nil {
		return nil, apperror.NewInternal("failed to list agent skills", err)
	}

	// De-duplicate: project-scoped wins on name collision.
	// Since project rows sort before NULL rows, the first occurrence of each name wins.
	seen := make(map[string]struct{}, len(all))
	result := make([]*Skill, 0, len(all))
	for _, s := range all {
		if _, ok := seen[s.Name]; ok {
			continue
		}
		seen[s.Name] = struct{}{}
		result = append(result, s)
	}
	return result, nil
}

// FindRelevant performs cosine similarity search against description embeddings.
// Returns the topK most relevant skills for the given project (global + project-scoped).
// Uses IVFFlat with probes=10 for high recall (~90%+) with low latency.
// Only considers skills with a non-NULL description_embedding.
func (r *Repository) FindRelevant(ctx context.Context, projectID string, vec []float32, topK int) ([]*Skill, error) {
	tx, err := r.beginTxWithIVFFlatProbes(ctx, 10)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback() }()

	vectorStr := pgutils.FormatVector(vec)

	var skills []*Skill
	err = tx.NewSelect().
		Model(&skills).
		Where("(s.project_id = ? OR s.project_id IS NULL)", projectID).
		Where("s.description_embedding IS NOT NULL").
		OrderExpr("s.description_embedding <=> ?::vector ASC", vectorStr).
		Limit(topK).
		Scan(ctx)
	if err != nil {
		return nil, apperror.NewInternal("failed to find relevant skills", err)
	}

	if err := tx.Commit(); err != nil {
		r.log.Warn("skills vector search: failed to commit tx (results still valid)",
			logger.Error(err),
		)
	}

	return skills, nil
}

// FindByID returns a single skill by its UUID.
func (r *Repository) FindByID(ctx context.Context, id uuid.UUID) (*Skill, error) {
	skill := new(Skill)
	err := r.db.NewSelect().
		Model(skill).
		Where("s.id = ?", id).
		Scan(ctx)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, apperror.NewNotFound("skill", id.String())
		}
		return nil, apperror.NewInternal("failed to get skill", err)
	}
	return skill, nil
}

// Create inserts a new skill. If embedding is provided, it is written via raw SQL to avoid
// Bun trying to bind a []float32 to a vector column. Embedding may be nil (stored as NULL).
func (r *Repository) Create(ctx context.Context, s *Skill, embedding []float32) error {
	if embedding != nil {
		vectorStr := pgutils.FormatVector(embedding)
		_, err := r.db.ExecContext(ctx,
			`INSERT INTO kb.skills (id, name, description, content, metadata, description_embedding, project_id, created_at, updated_at)
			 VALUES (gen_random_uuid(), ?, ?, ?, ?, ?::vector, ?, now(), now())
			 RETURNING id, created_at, updated_at`,
			s.Name, s.Description, s.Content, s.Metadata, vectorStr, s.ProjectID,
		)
		if err != nil {
			return r.wrapDBError("failed to create skill", err)
		}
		// Fetch to populate id/timestamps
		return r.db.NewSelect().Model(s).Where("s.name = ? AND (s.project_id = ? OR (s.project_id IS NULL AND ? IS NULL))",
			s.Name, s.ProjectID, s.ProjectID).OrderExpr("s.created_at DESC").Limit(1).Scan(ctx)
	}

	// No embedding: use Bun ORM insert
	_, err := r.db.NewInsert().Model(s).Exec(ctx)
	if err != nil {
		return r.wrapDBError("failed to create skill", err)
	}
	return nil
}

// Update applies partial updates to an existing skill. If descriptionChanged is true and
// embedding is non-nil, the embedding is updated via raw SQL.
func (r *Repository) Update(ctx context.Context, id uuid.UUID, dto *UpdateSkillDTO, embedding []float32, descriptionChanged bool) (*Skill, error) {
	// Load existing
	existing, err := r.FindByID(ctx, id)
	if err != nil {
		return nil, err
	}

	now := time.Now()

	if dto.Description != nil {
		existing.Description = *dto.Description
	}
	if dto.Content != nil {
		existing.Content = *dto.Content
	}
	if dto.Metadata != nil {
		existing.Metadata = dto.Metadata
	}
	existing.UpdatedAt = now

	if descriptionChanged && embedding != nil {
		vectorStr := pgutils.FormatVector(embedding)
		_, err = r.db.ExecContext(ctx,
			`UPDATE kb.skills SET description = ?, content = ?, metadata = ?, description_embedding = ?::vector, updated_at = ? WHERE id = ?`,
			existing.Description, existing.Content, existing.Metadata, vectorStr, now, id,
		)
		if err != nil {
			return nil, r.wrapDBError("failed to update skill", err)
		}
		return existing, nil
	}

	if descriptionChanged && embedding == nil {
		// Description changed but no embedding available: clear the old embedding
		_, err = r.db.ExecContext(ctx,
			`UPDATE kb.skills SET description = ?, content = ?, metadata = ?, description_embedding = NULL, updated_at = ? WHERE id = ?`,
			existing.Description, existing.Content, existing.Metadata, now, id,
		)
		if err != nil {
			return nil, r.wrapDBError("failed to update skill", err)
		}
		return existing, nil
	}

	// No description change: update other fields with Bun ORM
	_, err = r.db.NewUpdate().Model(existing).
		Column("description", "content", "metadata", "updated_at").
		Where("id = ?", id).
		Exec(ctx)
	if err != nil {
		return nil, r.wrapDBError("failed to update skill", err)
	}
	return existing, nil
}

// Delete removes a skill by ID. Returns NotFound if the skill does not exist.
func (r *Repository) Delete(ctx context.Context, id uuid.UUID) error {
	result, err := r.db.NewDelete().
		Model((*Skill)(nil)).
		Where("id = ?", id).
		Exec(ctx)
	if err != nil {
		return apperror.NewInternal("failed to delete skill", err)
	}
	n, _ := result.RowsAffected()
	if n == 0 {
		return apperror.NewNotFound("skill", id.String())
	}
	return nil
}

// Count returns total number of skills accessible to an agent for a project (global + project).
func (r *Repository) Count(ctx context.Context, projectID string) (int, error) {
	n, err := r.db.NewSelect().
		Model((*Skill)(nil)).
		Where("project_id = ? OR project_id IS NULL", projectID).
		Count(ctx)
	if err != nil {
		return 0, apperror.NewInternal("failed to count skills", err)
	}
	return n, nil
}

// beginTxWithIVFFlatProbes starts a transaction and sets ivfflat.probes for improved
// vector index recall. SET LOCAL scopes the setting to the current transaction only.
func (r *Repository) beginTxWithIVFFlatProbes(ctx context.Context, probes int) (bun.Tx, error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return tx, apperror.ErrDatabase.WithInternal(err)
	}
	if _, err := tx.ExecContext(ctx, fmt.Sprintf("SET LOCAL ivfflat.probes = %d", probes)); err != nil {
		_ = tx.Rollback()
		return tx, apperror.ErrDatabase.WithInternal(err)
	}
	return tx, nil
}

// wrapDBError wraps a DB error, detecting unique constraint violations.
func (r *Repository) wrapDBError(msg string, err error) error {
	if pgutils.IsUniqueViolation(err) {
		return apperror.ErrConflict.WithMessage("a skill with this name already exists")
	}
	return apperror.NewInternal(msg, err)
}
