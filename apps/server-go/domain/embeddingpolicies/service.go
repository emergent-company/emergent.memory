package embeddingpolicies

import (
	"context"
	"log/slog"
	"time"

	"github.com/google/uuid"
	"github.com/lib/pq"

	"github.com/emergent-company/emergent/pkg/apperror"
	"github.com/emergent-company/emergent/pkg/logger"
)

// Service handles embedding policy business logic
type Service struct {
	store *Store
	log   *slog.Logger
}

// NewService creates a new embedding policies service
func NewService(store *Store, log *slog.Logger) *Service {
	return &Service{
		store: store,
		log:   log.With(logger.Scope("embeddingpolicies.svc")),
	}
}

// List retrieves all embedding policies for a project
func (s *Service) List(ctx context.Context, projectID string, objectType *string) ([]EmbeddingPolicy, error) {
	return s.store.List(ctx, projectID, objectType)
}

// GetByID retrieves a single embedding policy by ID
func (s *Service) GetByID(ctx context.Context, projectID, policyID string) (*EmbeddingPolicy, error) {
	// Validate UUID format
	if _, err := uuid.Parse(policyID); err != nil {
		return nil, apperror.ErrBadRequest.WithMessage("Invalid policy ID format")
	}

	policy, err := s.store.GetByID(ctx, projectID, policyID)
	if err != nil {
		return nil, err
	}
	if policy == nil {
		return nil, apperror.ErrNotFound.WithMessage("Embedding policy not found")
	}
	return policy, nil
}

// Create creates a new embedding policy
func (s *Service) Create(ctx context.Context, projectID string, req *CreateRequest) (*EmbeddingPolicy, error) {
	// Validate UUID format for projectId
	if _, err := uuid.Parse(req.ProjectID); err != nil {
		return nil, apperror.ErrBadRequest.WithMessage("Invalid projectId format")
	}

	// Validate maxPropertySize if provided
	if req.MaxPropertySize != nil && *req.MaxPropertySize < 1 {
		return nil, apperror.ErrBadRequest.WithMessage("maxPropertySize must be at least 1")
	}

	// Default enabled to true if not provided
	enabled := true
	if req.Enabled != nil {
		enabled = *req.Enabled
	}

	now := time.Now().UTC()
	policy := &EmbeddingPolicy{
		ID:               uuid.New().String(),
		ProjectID:        req.ProjectID, // Use projectId from body
		ObjectType:       req.ObjectType,
		Enabled:          enabled,
		MaxPropertySize:  req.MaxPropertySize,
		RequiredLabels:   toPqArray(req.RequiredLabels),
		ExcludedLabels:   toPqArray(req.ExcludedLabels),
		RelevantPaths:    toPqArray(req.RelevantPaths),
		ExcludedStatuses: toPqArray(req.ExcludedStatuses),
		CreatedAt:        now,
		UpdatedAt:        now,
	}

	if err := s.store.Create(ctx, policy); err != nil {
		return nil, err
	}

	s.log.Info("embedding policy created",
		slog.String("id", policy.ID),
		slog.String("projectId", policy.ProjectID),
		slog.String("objectType", policy.ObjectType))

	return policy, nil
}

// Update updates an existing embedding policy
func (s *Service) Update(ctx context.Context, projectID, policyID string, req *UpdateRequest) (*EmbeddingPolicy, error) {
	// Validate UUID format
	if _, err := uuid.Parse(policyID); err != nil {
		return nil, apperror.ErrBadRequest.WithMessage("Invalid policy ID format")
	}

	// Validate maxPropertySize if provided
	if req.MaxPropertySize != nil && *req.MaxPropertySize < 1 {
		return nil, apperror.ErrBadRequest.WithMessage("maxPropertySize must be at least 1")
	}

	// Get existing policy
	policy, err := s.store.GetByID(ctx, projectID, policyID)
	if err != nil {
		return nil, err
	}
	if policy == nil {
		return nil, apperror.ErrNotFound.WithMessage("Embedding policy not found")
	}

	// Apply updates
	if req.Enabled != nil {
		policy.Enabled = *req.Enabled
	}
	if req.MaxPropertySize != nil {
		policy.MaxPropertySize = req.MaxPropertySize
	}
	if req.RequiredLabels != nil {
		policy.RequiredLabels = toPqArray(req.RequiredLabels)
	}
	if req.ExcludedLabels != nil {
		policy.ExcludedLabels = toPqArray(req.ExcludedLabels)
	}
	if req.RelevantPaths != nil {
		policy.RelevantPaths = toPqArray(req.RelevantPaths)
	}
	if req.ExcludedStatuses != nil {
		policy.ExcludedStatuses = toPqArray(req.ExcludedStatuses)
	}
	policy.UpdatedAt = time.Now().UTC()

	if err := s.store.Update(ctx, policy); err != nil {
		return nil, err
	}

	s.log.Info("embedding policy updated",
		slog.String("id", policy.ID),
		slog.String("projectId", projectID))

	return policy, nil
}

// Delete deletes an embedding policy
func (s *Service) Delete(ctx context.Context, projectID, policyID string) error {
	// Validate UUID format
	if _, err := uuid.Parse(policyID); err != nil {
		return apperror.ErrBadRequest.WithMessage("Invalid policy ID format")
	}

	deleted, err := s.store.Delete(ctx, projectID, policyID)
	if err != nil {
		return err
	}
	if !deleted {
		return apperror.ErrNotFound.WithMessage("Embedding policy not found")
	}

	s.log.Info("embedding policy deleted",
		slog.String("id", policyID),
		slog.String("projectId", projectID))

	return nil
}

// toPqArray converts a string slice to pq.StringArray
func toPqArray(arr []string) pq.StringArray {
	if arr == nil {
		return pq.StringArray{}
	}
	return pq.StringArray(arr)
}
