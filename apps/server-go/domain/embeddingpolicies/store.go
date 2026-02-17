package embeddingpolicies

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"

	"github.com/lib/pq"
	"github.com/uptrace/bun"

	"github.com/emergent-company/emergent/pkg/apperror"
	"github.com/emergent-company/emergent/pkg/logger"
	"github.com/emergent-company/emergent/pkg/pgutils"
)

// Store handles embedding policy database operations
type Store struct {
	db  bun.IDB
	log *slog.Logger
}

// NewStore creates a new embedding policies store
func NewStore(db bun.IDB, log *slog.Logger) *Store {
	return &Store{
		db:  db,
		log: log.With(logger.Scope("embeddingpolicies.store")),
	}
}

// List retrieves all embedding policies for a project
func (s *Store) List(ctx context.Context, projectID string, objectType *string) ([]EmbeddingPolicy, error) {
	policies := []EmbeddingPolicy{}

	query := s.db.NewSelect().
		Model(&policies).
		Where("project_id = ?", projectID).
		Order("object_type ASC")

	if objectType != nil {
		query = query.Where("object_type = ?", *objectType)
	}

	if err := query.Scan(ctx); err != nil {
		s.log.Error("failed to list embedding policies", logger.Error(err))
		return nil, apperror.ErrDatabase.WithInternal(err)
	}

	return policies, nil
}

// GetByID retrieves a single embedding policy by ID
func (s *Store) GetByID(ctx context.Context, projectID, policyID string) (*EmbeddingPolicy, error) {
	var policy EmbeddingPolicy
	err := s.db.NewSelect().
		Model(&policy).
		Where("id = ?", policyID).
		Where("project_id = ?", projectID).
		Scan(ctx)

	if err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		s.log.Error("failed to get embedding policy", logger.Error(err), slog.String("id", policyID))
		return nil, apperror.ErrDatabase.WithInternal(err)
	}

	return &policy, nil
}

// GetByObjectType retrieves an embedding policy by project ID and object type
func (s *Store) GetByObjectType(ctx context.Context, projectID, objectType string) (*EmbeddingPolicy, error) {
	var policy EmbeddingPolicy
	err := s.db.NewSelect().
		Model(&policy).
		Where("project_id = ?", projectID).
		Where("object_type = ?", objectType).
		Scan(ctx)

	if err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		s.log.Error("failed to get embedding policy by object type", logger.Error(err),
			slog.String("projectId", projectID), slog.String("objectType", objectType))
		return nil, apperror.ErrDatabase.WithInternal(err)
	}

	return &policy, nil
}

// Create creates a new embedding policy
func (s *Store) Create(ctx context.Context, policy *EmbeddingPolicy) error {
	// Initialize empty arrays if nil to avoid NULL in DB
	if policy.RequiredLabels == nil {
		policy.RequiredLabels = pq.StringArray{}
	}
	if policy.ExcludedLabels == nil {
		policy.ExcludedLabels = pq.StringArray{}
	}
	if policy.RelevantPaths == nil {
		policy.RelevantPaths = pq.StringArray{}
	}
	if policy.ExcludedStatuses == nil {
		policy.ExcludedStatuses = pq.StringArray{}
	}

	_, err := s.db.NewInsert().
		Model(policy).
		Returning("*").
		Exec(ctx)

	if err != nil {
		if pgutils.IsUniqueViolation(err) {
			return apperror.New(409, "duplicate", fmt.Sprintf("Embedding policy for object type '%s' already exists in this project", policy.ObjectType))
		}
		if pgutils.IsForeignKeyViolation(err) {
			return apperror.New(400, "invalid-project", "Project not found")
		}
		s.log.Error("failed to create embedding policy", logger.Error(err))
		return apperror.ErrDatabase.WithInternal(err)
	}

	return nil
}

// Update updates an existing embedding policy
func (s *Store) Update(ctx context.Context, policy *EmbeddingPolicy) error {
	result, err := s.db.NewUpdate().
		Model(policy).
		WherePK().
		Where("project_id = ?", policy.ProjectID).
		Returning("*").
		Exec(ctx)

	if err != nil {
		s.log.Error("failed to update embedding policy", logger.Error(err), slog.String("id", policy.ID))
		return apperror.ErrDatabase.WithInternal(err)
	}

	rowsAffected, _ := result.RowsAffected()
	if rowsAffected == 0 {
		return apperror.ErrNotFound.WithMessage("Embedding policy not found")
	}

	return nil
}

// Delete deletes an embedding policy by ID
func (s *Store) Delete(ctx context.Context, projectID, policyID string) (bool, error) {
	result, err := s.db.NewDelete().
		Model((*EmbeddingPolicy)(nil)).
		Where("id = ?", policyID).
		Where("project_id = ?", projectID).
		Exec(ctx)

	if err != nil {
		s.log.Error("failed to delete embedding policy", logger.Error(err), slog.String("id", policyID))
		return false, apperror.ErrDatabase.WithInternal(err)
	}

	rowsAffected, _ := result.RowsAffected()
	return rowsAffected > 0, nil
}
