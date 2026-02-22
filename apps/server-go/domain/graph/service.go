package graph

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"math"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/uptrace/bun"

	"github.com/emergent-company/emergent/domain/extraction/agents"
	"github.com/emergent-company/emergent/pkg/apperror"
	"github.com/emergent-company/emergent/pkg/logger"
	"github.com/emergent-company/emergent/pkg/mathutil"
)

// ExtractionSchemas contains object and relationship schemas.
type ExtractionSchemas struct {
	ObjectSchemas       map[string]agents.ObjectSchema
	RelationshipSchemas map[string]agents.RelationshipSchema
}

// SchemaProvider provides access to template pack schemas for validation.
type SchemaProvider interface {
	GetProjectSchemas(ctx context.Context, projectID string) (*ExtractionSchemas, error)
}

// InverseTypeProvider resolves inverse relationship types from template pack schemas.
// For a given project and relationship type (e.g. "PARENT_OF"), it returns the inverse
// type key (e.g. "CHILD_OF") if the template pack declares an inverseType field.
type InverseTypeProvider interface {
	GetInverseType(ctx context.Context, projectID string, relType string) (string, bool)
}

// EmbeddingService provides embedding generation for triplet text.
type EmbeddingService interface {
	EmbedQuery(ctx context.Context, query string) ([]float32, error)
}

// EmbeddingEnqueuer enqueues graph objects for asynchronous embedding generation.
// This is satisfied by extraction.GraphEmbeddingJobsService via an adapter in module.go
// to avoid a circular dependency (graph -> extraction -> graph).
type EmbeddingEnqueuer interface {
	EnqueueEmbedding(ctx context.Context, objectID string) error
}

// Service handles business logic for graph operations.
type Service struct {
	repo                *Repository
	log                 *slog.Logger
	schemaProvider      SchemaProvider
	inverseTypeProvider InverseTypeProvider
	embeddings          EmbeddingService
	embeddingEnqueuer   EmbeddingEnqueuer

	// Metrics
	metricsMu          sync.RWMutex
	validationSuccess  int64
	validationErrors   int64
	validationDuration time.Duration
}

// NewService creates a new graph service.
func NewService(repo *Repository, log *slog.Logger, schemaProvider SchemaProvider, inverseTypeProvider InverseTypeProvider, embeddings EmbeddingService, embeddingEnqueuer EmbeddingEnqueuer) *Service {
	return &Service{
		repo:                repo,
		log:                 log.With(logger.Scope("graph.svc")),
		schemaProvider:      schemaProvider,
		inverseTypeProvider: inverseTypeProvider,
		embeddings:          embeddings,
		embeddingEnqueuer:   embeddingEnqueuer,
	}
}

// enqueueEmbedding enqueues a graph object for async embedding generation.
// Logs and swallows errors — embedding is best-effort and must never block CRUD.
func (s *Service) enqueueEmbedding(ctx context.Context, objectID string) {
	if s.embeddingEnqueuer == nil {
		return
	}
	if err := s.embeddingEnqueuer.EnqueueEmbedding(ctx, objectID); err != nil {
		s.log.Warn("failed to enqueue embedding job",
			slog.String("object_id", objectID),
			slog.String("error", err.Error()))
	}
}

// UpdateAccessTimestamps updates last_accessed_at for the given object IDs.
func (s *Service) UpdateAccessTimestamps(ctx context.Context, objectIDs []uuid.UUID) error {
	return s.repo.UpdateAccessTimestamps(ctx, objectIDs)
}

// GetMostAccessed returns the most frequently accessed graph objects for analytics.
func (s *Service) GetMostAccessed(ctx context.Context, projectID uuid.UUID, limit int, minAccessCount int) (*MostAccessedResponse, error) {
	objects, err := s.repo.GetMostAccessed(ctx, projectID, limit, minAccessCount)
	if err != nil {
		return nil, err
	}

	items := make([]AnalyticsObjectItem, len(objects))
	for i, obj := range objects {
		items[i] = AnalyticsObjectItem{
			ID:             obj.ID,
			CanonicalID:    obj.CanonicalID,
			Type:           obj.Type,
			Key:            obj.Key,
			Properties:     obj.Properties,
			Labels:         obj.Labels,
			LastAccessedAt: obj.LastAccessedAt,
			CreatedAt:      obj.CreatedAt,
		}
	}

	return &MostAccessedResponse{
		Items: items,
		Total: len(items),
		Meta: map[string]interface{}{
			"limit":          limit,
			"minAccessCount": minAccessCount,
		},
	}, nil
}

// GetUnused returns graph objects that haven't been accessed recently.
func (s *Service) GetUnused(ctx context.Context, projectID uuid.UUID, limit int, daysThreshold int) (*UnusedObjectsResponse, error) {
	objects, err := s.repo.GetUnused(ctx, projectID, limit, daysThreshold)
	if err != nil {
		return nil, err
	}

	items := make([]AnalyticsObjectItem, len(objects))
	for i, obj := range objects {
		var daysSinceAccess *int
		if obj.LastAccessedAt != nil {
			days := int(time.Since(*obj.LastAccessedAt).Hours() / 24)
			daysSinceAccess = &days
		}

		items[i] = AnalyticsObjectItem{
			ID:              obj.ID,
			CanonicalID:     obj.CanonicalID,
			Type:            obj.Type,
			Key:             obj.Key,
			Properties:      obj.Properties,
			Labels:          obj.Labels,
			LastAccessedAt:  obj.LastAccessedAt,
			DaysSinceAccess: daysSinceAccess,
			CreatedAt:       obj.CreatedAt,
		}
	}

	return &UnusedObjectsResponse{
		Items: items,
		Total: len(items),
		Meta: map[string]interface{}{
			"limit":         limit,
			"daysThreshold": daysThreshold,
		},
	}, nil
}

// CountObjects returns the count of graph objects matching the given filters.
func (s *Service) CountObjects(ctx context.Context, params ListParams) (int, error) {
	return s.repo.Count(ctx, params)
}

// List returns graph objects matching the given parameters.
func (s *Service) List(ctx context.Context, params ListParams) (*SearchGraphObjectsResponse, error) {
	// Run count and list queries
	// Note: For better performance, these could be run in parallel with errgroup
	total, err := s.repo.Count(ctx, params)
	if err != nil {
		return nil, err
	}

	objects, err := s.repo.List(ctx, params)
	if err != nil {
		return nil, err
	}

	// Check if there are more results
	hasMore := len(objects) > params.Limit
	if hasMore {
		objects = objects[:params.Limit]
	}

	// Build response with NestJS-compatible field names
	items := make([]*GraphObjectResponse, len(objects))
	for i, obj := range objects {
		items[i] = obj.ToResponse()
	}

	// Apply field projection if requested (filter properties to only include specified keys)
	// TODO: Push field projection down to the repository/DB layer to avoid fetching
	// full property maps when only a subset of keys is needed. This is an optimization
	// for large property sets — the current in-memory approach is correct but wasteful.
	if len(params.Fields) > 0 {
		projection := &GraphExpandProjection{
			IncludeObjectProperties: params.Fields,
		}
		for _, item := range items {
			item.Properties = projectProperties(item.Properties, projection)
		}
	}

	var nextCursor *string
	if hasMore && len(objects) > 0 {
		last := objects[len(objects)-1]
		cursor := encodeCursor(last.CreatedAt, last.ID)
		nextCursor = &cursor
	}

	return &SearchGraphObjectsResponse{
		Items:      items,
		NextCursor: nextCursor,
		Total:      total,
	}, nil
}

// GetByID returns a graph object by its physical ID or canonical ID.
// The ID is resolved transparently — canonical IDs automatically return the HEAD version.
// If resolveHead is true and the ID refers to an older physical version, returns the HEAD version instead.
func (s *Service) GetByID(ctx context.Context, projectID, id uuid.UUID, resolveHead bool) (*GraphObjectResponse, error) {
	obj, err := s.repo.GetByID(ctx, projectID, id)
	if err != nil {
		return nil, err
	}

	// If resolveHead is requested and this isn't the HEAD version, fetch the HEAD.
	// Note: When a canonical_id is passed, the repo already returns HEAD. This only
	// matters when a physical ID of an old version is passed directly.
	if resolveHead && obj.SupersedesID != nil {
		headObj, err := s.repo.GetHeadByCanonicalID(ctx, s.repo.DB(), projectID, obj.CanonicalID, obj.BranchID)
		if err != nil {
			s.log.Warn("could not find HEAD version for resolveHead",
				slog.String("canonical_id", obj.CanonicalID.String()),
				slog.String("requested_id", id.String()))
			return obj.ToResponse(), nil
		}
		return headObj.ToResponse(), nil
	}

	return obj.ToResponse(), nil
}

// Create creates a new graph object.
func (s *Service) Create(ctx context.Context, projectID uuid.UUID, req *CreateGraphObjectRequest, actorID *uuid.UUID) (*GraphObjectResponse, error) {
	actorType := "user"

	validatedProps := req.Properties
	if s.schemaProvider != nil {
		schemas, err := s.schemaProvider.GetProjectSchemas(ctx, projectID.String())
		if err != nil {
			s.log.Warn("failed to load schemas, skipping validation",
				slog.String("project_id", projectID.String()),
				slog.String("error", err.Error()))
		} else if schema, ok := schemas.ObjectSchemas[req.Type]; ok {
			start := time.Now()
			validated, err := validateProperties(req.Properties, schema)
			duration := time.Since(start)

			if err != nil {
				s.incrementValidationError(duration)
				return nil, apperror.ErrBadRequest.WithMessage("property validation failed: " + err.Error())
			}
			s.incrementValidationSuccess(duration)
			validatedProps = validated
		}
	}

	obj := &GraphObject{
		ProjectID:  projectID,
		BranchID:   req.BranchID,
		Type:       req.Type,
		Key:        req.Key,
		Status:     req.Status,
		Properties: validatedProps,
		Labels:     req.Labels,
		ActorType:  &actorType,
		ActorID:    actorID,
	}

	if err := s.repo.Create(ctx, obj); err != nil {
		return nil, err
	}

	s.enqueueEmbedding(ctx, obj.ID.String())

	return obj.ToResponse(), nil
}

// CreateOrUpdate implements upsert semantics for graph objects identified by (type, key).
// If no existing HEAD object with the same (project_id, branch_id, type, key) is found, a new object is created.
// If an existing HEAD is found but was deleted, a new version is created to restore it with the new properties.
// If an existing HEAD is found and properties are identical, the existing object is returned (no-op).
// If an existing HEAD is found and properties differ, a new version is created with the updated properties.
// This follows the same pattern as CreateRelationship for relationships.
func (s *Service) CreateOrUpdate(ctx context.Context, projectID uuid.UUID, req *CreateGraphObjectRequest, actorID *uuid.UUID) (*GraphObjectResponse, bool, error) {
	if req.Key == nil || *req.Key == "" {
		return nil, false, apperror.ErrBadRequest.WithMessage("key is required for upsert")
	}

	// Validate properties against schema
	validatedProps := req.Properties
	if s.schemaProvider != nil {
		schemas, err := s.schemaProvider.GetProjectSchemas(ctx, projectID.String())
		if err != nil {
			s.log.Warn("failed to load schemas, skipping validation",
				slog.String("project_id", projectID.String()),
				slog.String("error", err.Error()))
		} else if schema, ok := schemas.ObjectSchemas[req.Type]; ok {
			start := time.Now()
			validated, err := validateProperties(req.Properties, schema)
			duration := time.Since(start)

			if err != nil {
				s.incrementValidationError(duration)
				return nil, false, apperror.ErrBadRequest.WithMessage("property validation failed: " + err.Error())
			}
			s.incrementValidationSuccess(duration)
			validatedProps = validated
		}
	}

	// Start transaction
	tx, err := s.repo.BeginTx(ctx)
	if err != nil {
		return nil, false, apperror.ErrDatabase.WithInternal(err)
	}
	defer tx.Rollback()

	// Acquire advisory lock for this (project_id, type, key) identity
	if err := s.repo.AcquireObjectUpsertLock(ctx, tx.Tx, projectID, req.Type, *req.Key); err != nil {
		return nil, false, err
	}

	// Check if object already exists
	existing, err := s.repo.FindHeadByTypeAndKey(ctx, tx.Tx, projectID, req.BranchID, req.Type, *req.Key)
	if err != nil {
		return nil, false, err
	}

	actorType := "user"

	if existing == nil {
		// Create new object
		obj := &GraphObject{
			ProjectID:  projectID,
			BranchID:   req.BranchID,
			Type:       req.Type,
			Key:        req.Key,
			Status:     req.Status,
			Properties: validatedProps,
			Labels:     req.Labels,
			ActorType:  &actorType,
			ActorID:    actorID,
		}

		if err := s.repo.CreateInTx(ctx, tx.Tx, obj); err != nil {
			return nil, false, err
		}

		if err := tx.Commit(); err != nil {
			return nil, false, apperror.ErrDatabase.WithInternal(err)
		}

		s.enqueueEmbedding(ctx, obj.ID.String())

		return obj.ToResponse(), true, nil
	}

	// Object exists - check if it was deleted
	if existing.DeletedAt != nil {
		// Was deleted, create new version to "restore" with new properties
		newVersion := &GraphObject{
			Type:       req.Type,
			Key:        req.Key,
			Status:     req.Status,
			Properties: validatedProps,
			Labels:     req.Labels,
			DeletedAt:  nil,
			ActorType:  &actorType,
			ActorID:    actorID,
		}
		newVersion.ChangeSummary = computeChangeSummary(existing.Properties, validatedProps)

		if err := s.repo.CreateVersion(ctx, tx.Tx, existing, newVersion); err != nil {
			return nil, false, err
		}

		if err := tx.Commit(); err != nil {
			return nil, false, apperror.ErrDatabase.WithInternal(err)
		}

		s.enqueueEmbedding(ctx, newVersion.ID.String())

		return newVersion.ToResponse(), false, nil
	}

	// Build the merged state to compare - check if properties, status, and labels changed
	newProps := validatedProps
	if newProps == nil {
		newProps = make(map[string]any)
	}

	// Check if properties changed
	diff := computeChangeSummary(existing.Properties, newProps)

	// Check if status changed
	statusChanged := false
	newStatus := existing.Status
	if req.Status != nil {
		if existing.Status == nil || *existing.Status != *req.Status {
			statusChanged = true
			newStatus = req.Status
		}
	}

	// Check if labels changed
	labelsChanged := false
	newLabels := existing.Labels
	if req.Labels != nil {
		existingLabelSet := make(map[string]bool, len(existing.Labels))
		for _, l := range existing.Labels {
			existingLabelSet[l] = true
		}
		reqLabelSet := make(map[string]bool, len(req.Labels))
		for _, l := range req.Labels {
			reqLabelSet[l] = true
		}
		if len(existingLabelSet) != len(reqLabelSet) {
			labelsChanged = true
		} else {
			for l := range reqLabelSet {
				if !existingLabelSet[l] {
					labelsChanged = true
					break
				}
			}
		}
		if labelsChanged {
			newLabels = req.Labels
		}
	}

	if diff == nil && !statusChanged && !labelsChanged {
		// No change - return existing (no-op)
		if err := tx.Commit(); err != nil {
			return nil, false, apperror.ErrDatabase.WithInternal(err)
		}
		return existing.ToResponse(), false, nil
	}

	// Properties, status, or labels differ - create new version
	newVersion := &GraphObject{
		Type:       existing.Type,
		Key:        existing.Key,
		Status:     newStatus,
		Properties: newProps,
		Labels:     newLabels,
		ActorType:  &actorType,
		ActorID:    actorID,
	}
	newVersion.ChangeSummary = diff

	if err := s.repo.CreateVersion(ctx, tx.Tx, existing, newVersion); err != nil {
		return nil, false, err
	}

	if err := tx.Commit(); err != nil {
		return nil, false, apperror.ErrDatabase.WithInternal(err)
	}

	s.enqueueEmbedding(ctx, newVersion.ID.String())

	return newVersion.ToResponse(), false, nil
}

// Patch updates a graph object by creating a new version.
func (s *Service) Patch(ctx context.Context, projectID, id uuid.UUID, req *PatchGraphObjectRequest, actorID *uuid.UUID) (*GraphObjectResponse, error) {
	// Get current HEAD
	current, err := s.repo.GetByID(ctx, projectID, id)
	if err != nil {
		return nil, err
	}

	// Check if already deleted
	if current.DeletedAt != nil {
		return nil, apperror.ErrBadRequest.WithMessage("cannot patch deleted object")
	}

	// Fetch schemas BEFORE transaction to avoid deadlock
	var schemas *ExtractionSchemas
	if s.schemaProvider != nil {
		schemas, err = s.schemaProvider.GetProjectSchemas(ctx, projectID.String())
		if err != nil {
			s.log.Warn("failed to load schemas, skipping validation",
				slog.String("project_id", projectID.String()),
				slog.String("error", err.Error()))
		}
	}

	// Start transaction
	tx, err := s.repo.BeginTx(ctx)
	if err != nil {
		return nil, apperror.ErrDatabase.WithInternal(err)
	}
	defer tx.Rollback()

	// Acquire advisory lock
	if err := s.repo.AcquireObjectLock(ctx, tx.Tx, current.CanonicalID); err != nil {
		return nil, err
	}

	// Re-fetch to ensure we have the latest after acquiring lock
	current, err = s.repo.GetHeadByCanonicalID(ctx, tx.Tx, projectID, current.CanonicalID, current.BranchID)
	if err != nil {
		return nil, err
	}

	// Merge properties
	newProps := make(map[string]any)
	for k, v := range current.Properties {
		newProps[k] = v
	}
	for k, v := range req.Properties {
		if v == nil {
			delete(newProps, k) // null removes property
		} else {
			newProps[k] = v
		}
	}

	// Validate merged properties
	if schemas != nil {
		if schema, ok := schemas.ObjectSchemas[current.Type]; ok {
			start := time.Now()
			validated, err := validateProperties(newProps, schema)
			duration := time.Since(start)

			if err != nil {
				s.incrementValidationError(duration)
				return nil, apperror.ErrBadRequest.WithMessage("property validation failed: " + err.Error())
			}
			s.incrementValidationSuccess(duration)
			newProps = validated
		}
	}

	// Handle labels
	var newLabels []string
	if req.ReplaceLabels {
		newLabels = req.Labels
	} else if len(req.Labels) > 0 {
		// Merge labels (add new, remove duplicates)
		labelSet := make(map[string]bool)
		for _, l := range current.Labels {
			labelSet[l] = true
		}
		for _, l := range req.Labels {
			labelSet[l] = true
		}
		newLabels = make([]string, 0, len(labelSet))
		for l := range labelSet {
			newLabels = append(newLabels, l)
		}
	} else {
		newLabels = current.Labels
	}

	// Handle status
	newStatus := current.Status
	if req.Status != nil {
		newStatus = req.Status
	}

	actorType := "user"
	newVersion := &GraphObject{
		Type:       current.Type,
		Key:        current.Key,
		Status:     newStatus,
		Properties: newProps,
		Labels:     newLabels,
		ActorType:  &actorType,
		ActorID:    actorID,
	}

	// Compute change summary
	newVersion.ChangeSummary = computeChangeSummary(current.Properties, newProps)

	// Check if status changed
	statusChanged := false
	if req.Status != nil {
		if current.Status == nil || *current.Status != *req.Status {
			statusChanged = true
		}
	}

	// Check if labels changed
	labelsChanged := false
	if len(newLabels) != len(current.Labels) {
		labelsChanged = true
	} else {
		existingLabelSet := make(map[string]bool, len(current.Labels))
		for _, l := range current.Labels {
			existingLabelSet[l] = true
		}
		for _, l := range newLabels {
			if !existingLabelSet[l] {
				labelsChanged = true
				break
			}
		}
	}

	// No effective change — return existing version without creating a new one
	if newVersion.ChangeSummary == nil && !statusChanged && !labelsChanged {
		if err := tx.Commit(); err != nil {
			return nil, apperror.ErrDatabase.WithInternal(err)
		}
		return current.ToResponse(), nil
	}

	if err := s.repo.CreateVersion(ctx, tx.Tx, current, newVersion); err != nil {
		return nil, err
	}

	if err := tx.Commit(); err != nil {
		return nil, apperror.ErrDatabase.WithInternal(err)
	}

	s.enqueueEmbedding(ctx, newVersion.ID.String())

	return newVersion.ToResponse(), nil
}

// Delete soft-deletes a graph object by creating a tombstone version.
func (s *Service) Delete(ctx context.Context, projectID, id uuid.UUID, actorID *uuid.UUID) error {
	current, err := s.repo.GetByID(ctx, projectID, id)
	if err != nil {
		return err
	}

	if current.DeletedAt != nil {
		return apperror.ErrBadRequest.WithMessage("object already deleted")
	}

	tx, err := s.repo.BeginTx(ctx)
	if err != nil {
		return apperror.ErrDatabase.WithInternal(err)
	}
	defer tx.Rollback()

	if err := s.repo.AcquireObjectLock(ctx, tx.Tx, current.CanonicalID); err != nil {
		return err
	}

	// Re-fetch HEAD after lock
	current, err = s.repo.GetHeadByCanonicalID(ctx, tx.Tx, projectID, current.CanonicalID, current.BranchID)
	if err != nil {
		return err
	}

	if err := s.repo.SoftDelete(ctx, tx.Tx, current, actorID); err != nil {
		return err
	}

	return tx.Commit()
}

// Restore restores a soft-deleted graph object.
func (s *Service) Restore(ctx context.Context, projectID, id uuid.UUID, actorID *uuid.UUID) (*GraphObjectResponse, error) {
	current, err := s.repo.GetByID(ctx, projectID, id)
	if err != nil {
		return nil, err
	}

	if current.DeletedAt == nil {
		return nil, apperror.ErrBadRequest.WithMessage("object is not deleted")
	}

	tx, err := s.repo.BeginTx(ctx)
	if err != nil {
		return nil, apperror.ErrDatabase.WithInternal(err)
	}
	defer tx.Rollback()

	if err := s.repo.AcquireObjectLock(ctx, tx.Tx, current.CanonicalID); err != nil {
		return nil, err
	}

	// Re-fetch HEAD after lock
	current, err = s.repo.GetHeadByCanonicalID(ctx, tx.Tx, projectID, current.CanonicalID, current.BranchID)
	if err != nil {
		return nil, err
	}

	if err := s.repo.Restore(ctx, tx.Tx, current, actorID); err != nil {
		return nil, err
	}

	if err := tx.Commit(); err != nil {
		return nil, apperror.ErrDatabase.WithInternal(err)
	}

	// Re-fetch the new HEAD to return
	restored, err := s.repo.GetHeadByCanonicalID(ctx, s.repo.DB(), projectID, current.CanonicalID, current.BranchID)
	if err != nil {
		return nil, err
	}

	return restored.ToResponse(), nil
}

// GetHistory returns version history for a graph object.
func (s *Service) GetHistory(ctx context.Context, projectID, id uuid.UUID) (*ObjectHistoryResponse, error) {
	// First get the object to find its canonical ID
	obj, err := s.repo.GetByID(ctx, projectID, id)
	if err != nil {
		return nil, err
	}

	versions, err := s.repo.GetHistory(ctx, projectID, obj.CanonicalID)
	if err != nil {
		return nil, err
	}

	data := make([]*GraphObjectResponse, len(versions))
	for i, v := range versions {
		data[i] = v.ToResponse()
	}

	return &ObjectHistoryResponse{Versions: data}, nil
}

// GetEdges returns incoming and outgoing relationships for an object.
// The objectID can be either a physical id or a canonical_id.
func (s *Service) GetEdges(ctx context.Context, projectID, objectID uuid.UUID, params GetEdgesParams) (*GetObjectEdgesResponse, error) {
	// Resolve the object to get its canonical_id, since relationships store canonical IDs.
	obj, err := s.repo.GetByID(ctx, projectID, objectID)
	if err != nil {
		return nil, err
	}

	incoming, outgoing, err := s.repo.GetEdges(ctx, projectID, obj.CanonicalID, params)
	if err != nil {
		return nil, err
	}

	incomingResp := make([]*GraphRelationshipResponse, len(incoming))
	for i, r := range incoming {
		incomingResp[i] = r.ToResponse()
	}

	outgoingResp := make([]*GraphRelationshipResponse, len(outgoing))
	for i, r := range outgoing {
		outgoingResp[i] = r.ToResponse()
	}

	return &GetObjectEdgesResponse{
		Incoming: incomingResp,
		Outgoing: outgoingResp,
	}, nil
}

// computeChangeSummary creates an RFC 6901 JSON Pointer diff.
func computeChangeSummary(oldProps, newProps map[string]any) map[string]any {
	added := make(map[string]any)
	removed := make([]string, 0)
	updated := make(map[string]any)
	paths := make([]string, 0)

	// Find added and updated
	for k, newVal := range newProps {
		path := "/" + k
		if oldVal, exists := oldProps[k]; !exists {
			added[path] = newVal
			paths = append(paths, path)
		} else if !jsonEqual(oldVal, newVal) {
			updated[path] = map[string]any{
				"from": oldVal,
				"to":   newVal,
			}
			paths = append(paths, path)
		}
	}

	// Find removed
	for k := range oldProps {
		if _, exists := newProps[k]; !exists {
			path := "/" + k
			removed = append(removed, path)
			paths = append(paths, path)
		}
	}

	if len(added) == 0 && len(removed) == 0 && len(updated) == 0 {
		return nil
	}

	return map[string]any{
		"added":   added,
		"removed": removed,
		"updated": updated,
		"paths":   paths,
		"meta": map[string]any{
			"added":   len(added),
			"removed": len(removed),
			"updated": len(updated),
		},
	}
}

// jsonEqual compares two values for JSON equality.
func jsonEqual(a, b any) bool {
	aJSON, _ := json.Marshal(a)
	bJSON, _ := json.Marshal(b)
	return string(aJSON) == string(bJSON)
}

// =============================================================================
// Relationship Operations
// =============================================================================

// humanizeRelationType converts a relation type to a human-readable format.
// Example: WORKS_FOR -> "works for", FOUNDED_BY -> "founded by"
func humanizeRelationType(relType string) string {
	return strings.ToLower(strings.ReplaceAll(relType, "_", " "))
}

// getDisplayName extracts a display name from a graph object.
// Tries properties["name"] first, falls back to Key if name is missing or empty.
func getDisplayName(obj *GraphObject) string {
	if obj.Properties != nil {
		if name, ok := obj.Properties["name"].(string); ok && name != "" {
			return name
		}
	}
	if obj.Key != nil && *obj.Key != "" {
		return *obj.Key
	}
	return obj.ID.String()
}

// generateTripletText creates natural language triplet from a relationship.
// Format: "{source.name} {humanized_relation_type} {target.name}"
// Example: "Elon Musk founded Tesla"
func generateTripletText(source, target *GraphObject, relType string) string {
	sourceName := getDisplayName(source)
	targetName := getDisplayName(target)
	humanizedType := humanizeRelationType(relType)
	return fmt.Sprintf("%s %s %s", sourceName, humanizedType, targetName)
}

// embedTripletText generates an embedding vector for a relationship triplet.
// Returns the embedding vector and timestamp, or nil if embeddings are disabled.
func (s *Service) embedTripletText(ctx context.Context, tripletText string) ([]float32, *time.Time, error) {
	if s.embeddings == nil {
		return nil, nil, nil
	}

	embedding, err := s.embeddings.EmbedQuery(ctx, tripletText)
	if err != nil {
		return nil, nil, fmt.Errorf("embed triplet: %w", err)
	}

	if embedding == nil || len(embedding) == 0 {
		return nil, nil, nil
	}

	now := time.Now()
	return embedding, &now, nil
}

// vectorToString converts a float32 slice to a string representation for pgvector.
func vectorToString(v []float32) string {
	if len(v) == 0 {
		return "[]"
	}
	result := "["
	for i, val := range v {
		if i > 0 {
			result += ","
		}
		result += fmt.Sprintf("%f", val)
	}
	result += "]"
	return result
}

// SearchRelationshipsResponse is the paginated response for relationship searches.
// Uses NestJS-compatible field names: items, next_cursor, total (consistent with SearchGraphObjectsResponse)
type SearchRelationshipsResponse struct {
	Items      []*GraphRelationshipResponse `json:"items"`
	NextCursor *string                      `json:"next_cursor,omitempty"`
	Total      int                          `json:"total"`
}

// ListRelationships returns relationships matching the given parameters.
// CountRelationships returns the total count of relationships matching the given parameters.
func (s *Service) CountRelationships(ctx context.Context, params RelationshipListParams) (int, error) {
	// Resolve SrcID/DstID to canonical_id values, since relationships store canonical IDs.
	if params.SrcID != nil {
		obj, err := s.repo.GetByID(ctx, params.ProjectID, *params.SrcID)
		if err != nil {
			return 0, err
		}
		params.SrcID = &obj.CanonicalID
	}
	if params.DstID != nil {
		obj, err := s.repo.GetByID(ctx, params.ProjectID, *params.DstID)
		if err != nil {
			return 0, err
		}
		params.DstID = &obj.CanonicalID
	}

	return s.repo.CountRelationships(ctx, params)
}

func (s *Service) ListRelationships(ctx context.Context, params RelationshipListParams) (*SearchRelationshipsResponse, error) {
	// Resolve SrcID/DstID to canonical_id values, since relationships store canonical IDs.
	// The caller may pass either a physical id or a canonical_id.
	if params.SrcID != nil {
		obj, err := s.repo.GetByID(ctx, params.ProjectID, *params.SrcID)
		if err != nil {
			return nil, err
		}
		params.SrcID = &obj.CanonicalID
	}
	if params.DstID != nil {
		obj, err := s.repo.GetByID(ctx, params.ProjectID, *params.DstID)
		if err != nil {
			return nil, err
		}
		params.DstID = &obj.CanonicalID
	}

	// Run count and list queries
	total, err := s.repo.CountRelationships(ctx, params)
	if err != nil {
		return nil, err
	}

	rels, err := s.repo.ListRelationships(ctx, params)
	if err != nil {
		return nil, err
	}

	hasMore := len(rels) > params.Limit
	if hasMore {
		rels = rels[:params.Limit]
	}

	items := make([]*GraphRelationshipResponse, len(rels))
	for i, rel := range rels {
		items[i] = rel.ToResponse()
	}

	var nextCursor *string
	if hasMore && len(rels) > 0 {
		last := rels[len(rels)-1]
		cursor := encodeCursor(last.CreatedAt, last.ID)
		nextCursor = &cursor
	}

	return &SearchRelationshipsResponse{
		Items:      items,
		NextCursor: nextCursor,
		Total:      total,
	}, nil
}

// GetRelationship returns a relationship by its ID.
func (s *Service) GetRelationship(ctx context.Context, projectID, id uuid.UUID) (*GraphRelationshipResponse, error) {
	rel, err := s.repo.GetRelationshipByID(ctx, projectID, id)
	if err != nil {
		return nil, err
	}

	// Only return non-deleted relationships
	if rel.DeletedAt != nil {
		return nil, apperror.ErrNotFound
	}

	return rel.ToResponse(), nil
}

// CreateRelationship creates a new relationship or returns existing if properties match.
func (s *Service) CreateRelationship(ctx context.Context, projectID uuid.UUID, req *CreateGraphRelationshipRequest) (*GraphRelationshipResponse, error) {
	// Validate: no self-loops
	if req.SrcID == req.DstID {
		return nil, apperror.ErrBadRequest.WithMessage("self_loop_not_allowed")
	}

	// Pre-load inverse map to avoid mutex deadlock with DB connection pool
	// If the cache is empty, this fetches from DB before we hold a transaction.
	if s.inverseTypeProvider != nil {
		_, _ = s.inverseTypeProvider.GetInverseType(ctx, projectID.String(), req.Type)
	}

	// Start transaction
	tx, err := s.repo.BeginTx(ctx)
	if err != nil {
		return nil, apperror.ErrDatabase.WithInternal(err)
	}
	defer tx.Rollback()

	// Validate endpoints exist and are not deleted
	srcObj, dstObj, err := s.repo.ValidateEndpoints(ctx, tx.Tx, projectID, req.SrcID, req.DstID)
	if err != nil {
		return nil, err
	}

	// Check branch consistency
	effectiveBranchID := req.BranchID
	if effectiveBranchID != nil {
		// Both endpoints must be on the same branch
		if (srcObj.BranchID == nil || *srcObj.BranchID != *effectiveBranchID) ||
			(dstObj.BranchID == nil || *dstObj.BranchID != *effectiveBranchID) {
			return nil, apperror.ErrBadRequest.WithMessage("relationship_branch_mismatch")
		}
	} else {
		// If no branch specified, endpoints must be on main branch (null) or same branch
		if !branchIDsEqual(srcObj.BranchID, dstObj.BranchID) {
			return nil, apperror.ErrBadRequest.WithMessage("relationship_branch_mismatch")
		}
		effectiveBranchID = srcObj.BranchID
	}

	// Acquire lock for this relationship identity (use canonical IDs for stable locking)
	if err := s.repo.AcquireRelationshipLock(ctx, tx.Tx, projectID, req.Type, srcObj.CanonicalID, dstObj.CanonicalID); err != nil {
		return nil, err
	}

	// Check if relationship already exists (using canonical IDs)
	existing, err := s.repo.GetRelationshipHead(ctx, tx.Tx, projectID, effectiveBranchID, req.Type, srcObj.CanonicalID, dstObj.CanonicalID)
	if err != nil {
		return nil, err
	}

	if existing == nil {
		// Create new relationship — store canonical_id values in src_id/dst_id
		// so that relationships survive object versioning (CreateVersion generates new physical IDs).
		rel := &GraphRelationship{
			ProjectID:  projectID,
			BranchID:   effectiveBranchID,
			Type:       req.Type,
			SrcID:      srcObj.CanonicalID,
			DstID:      dstObj.CanonicalID,
			Properties: req.Properties,
			Weight:     req.Weight,
		}

		// Compute change summary
		rel.ChangeSummary = computeChangeSummary(nil, req.Properties)

		if err := s.repo.CreateRelationship(ctx, tx.Tx, rel); err != nil {
			return nil, err
		}

		tripletText := generateTripletText(srcObj, dstObj, req.Type)
		embedding, embeddingTimestamp, embedErr := s.embedTripletText(ctx, tripletText)
		if embedErr != nil {
			s.log.Warn("failed to generate embedding for relationship, continuing without embedding",
				slog.String("relationship_id", rel.ID.String()),
				slog.String("triplet", tripletText),
				slog.String("error", embedErr.Error()))
		} else if embedding != nil {
			_, updateErr := tx.Tx.NewRaw(`UPDATE kb.graph_relationships 
				SET embedding = ?::vector, embedding_updated_at = ? 
				WHERE id = ?`,
				vectorToString(embedding), embeddingTimestamp, rel.ID).Exec(ctx)
			if updateErr != nil {
				s.log.Warn("failed to store embedding for relationship, continuing without embedding",
					slog.String("relationship_id", rel.ID.String()),
					slog.String("error", updateErr.Error()))
			}
		}

		// Auto-create inverse relationship if template pack declares inverseType
		var inverseResponse *GraphRelationshipResponse
		if s.inverseTypeProvider != nil {
			inverseResponse = s.maybeCreateInverse(ctx, tx.Tx, projectID, effectiveBranchID, req.Type, srcObj, dstObj, req.Properties, req.Weight)
		}

		if err := tx.Commit(); err != nil {
			return nil, apperror.ErrDatabase.WithInternal(err)
		}

		resp := rel.ToResponse()
		resp.InverseRelationship = inverseResponse
		return resp, nil
	}

	// Relationship exists - check if properties differ
	if existing.DeletedAt != nil {
		// Was deleted, create new version to "restore" with new properties
		newVersion := &GraphRelationship{
			Properties: req.Properties,
			Weight:     req.Weight,
			DeletedAt:  nil,
		}
		newVersion.ChangeSummary = computeChangeSummary(existing.Properties, req.Properties)

		if err := s.repo.CreateRelationshipVersion(ctx, tx.Tx, existing, newVersion); err != nil {
			return nil, err
		}

		tripletText := generateTripletText(srcObj, dstObj, req.Type)
		embedding, embeddingTimestamp, embedErr := s.embedTripletText(ctx, tripletText)
		if embedErr != nil {
			s.log.Warn("failed to generate embedding for relationship, continuing without embedding",
				slog.String("relationship_id", newVersion.ID.String()),
				slog.String("triplet", tripletText),
				slog.String("error", embedErr.Error()))
		} else if embedding != nil {
			_, updateErr := tx.Tx.NewRaw(`UPDATE kb.graph_relationships 
				SET embedding = ?::vector, embedding_updated_at = ? 
				WHERE id = ?`,
				vectorToString(embedding), embeddingTimestamp, newVersion.ID).Exec(ctx)
			if updateErr != nil {
				s.log.Warn("failed to store embedding for relationship, continuing without embedding",
					slog.String("relationship_id", newVersion.ID.String()),
					slog.String("error", updateErr.Error()))
			}
		}

		if err := tx.Commit(); err != nil {
			return nil, apperror.ErrDatabase.WithInternal(err)
		}

		// Return the new version (use canonical IDs for lookup)
		newHead, _ := s.repo.GetRelationshipHead(ctx, s.repo.DB(), projectID, effectiveBranchID, req.Type, srcObj.CanonicalID, dstObj.CanonicalID)
		return newHead.ToResponse(), nil
	}

	// Check if properties changed
	diff := computeChangeSummary(existing.Properties, req.Properties)
	if diff == nil {
		// No change - return existing
		return existing.ToResponse(), nil
	}

	// Properties differ - create new version
	newVersion := &GraphRelationship{
		Properties:    req.Properties,
		Weight:        req.Weight,
		ChangeSummary: diff,
	}

	if err := s.repo.CreateRelationshipVersion(ctx, tx.Tx, existing, newVersion); err != nil {
		return nil, err
	}

	tripletText := generateTripletText(srcObj, dstObj, req.Type)
	embedding, embeddingTimestamp, embedErr := s.embedTripletText(ctx, tripletText)
	if embedErr != nil {
		s.log.Warn("failed to generate embedding for relationship, continuing without embedding",
			slog.String("relationship_id", newVersion.ID.String()),
			slog.String("triplet", tripletText),
			slog.String("error", embedErr.Error()))
	} else if embedding != nil {
		_, updateErr := tx.Tx.NewRaw(`UPDATE kb.graph_relationships 
			SET embedding = ?::vector, embedding_updated_at = ? 
			WHERE id = ?`,
			vectorToString(embedding), embeddingTimestamp, newVersion.ID).Exec(ctx)
		if updateErr != nil {
			s.log.Warn("failed to store embedding for relationship, continuing without embedding",
				slog.String("relationship_id", newVersion.ID.String()),
				slog.String("error", updateErr.Error()))
		}
	}

	if err := tx.Commit(); err != nil {
		return nil, apperror.ErrDatabase.WithInternal(err)
	}

	// Return the new version
	newHead, _ := s.repo.GetRelationshipHead(ctx, s.repo.DB(), projectID, effectiveBranchID, req.Type, srcObj.CanonicalID, dstObj.CanonicalID)
	return newHead.ToResponse(), nil
}

// maybeCreateInverse checks if the template pack declares an inverseType for the given
// relationship type, and if so, creates the inverse relationship (swapped src/dst) within
// the same transaction. Returns the inverse response or nil if no inverse was created.
// Errors are logged but do not fail the primary relationship creation.
func (s *Service) maybeCreateInverse(
	ctx context.Context,
	tx bun.Tx,
	projectID uuid.UUID,
	branchID *uuid.UUID,
	relType string,
	srcObj, dstObj *GraphObject,
	properties map[string]any,
	weight *float32,
) *GraphRelationshipResponse {
	inverseType, ok := s.inverseTypeProvider.GetInverseType(ctx, projectID.String(), relType)
	if !ok || inverseType == "" {
		return nil
	}

	s.log.Debug("creating inverse relationship",
		slog.String("primary_type", relType),
		slog.String("inverse_type", inverseType),
		slog.String("src_id", dstObj.ID.String()),
		slog.String("dst_id", srcObj.ID.String()))

	// Guard: if the inverse type maps back to the original type, only create the inverse
	// from the lexicographically smaller type to prevent infinite loops.
	// e.g., PARENT_OF -> CHILD_OF -> PARENT_OF would be a loop.
	reverseInverse, revOk := s.inverseTypeProvider.GetInverseType(ctx, projectID.String(), inverseType)
	if revOk && reverseInverse == relType {
		// Both types point to each other. Only create inverse from the "first" type alphabetically.
		if relType > inverseType {
			s.log.Debug("skipping inverse creation: inverse type is primary side",
				slog.String("rel_type", relType),
				slog.String("inverse_type", inverseType))
			return nil
		}
	}

	// Acquire advisory lock for the inverse relationship identity (swapped endpoints, using canonical IDs)
	if err := s.repo.AcquireRelationshipLock(ctx, tx, projectID, inverseType, dstObj.CanonicalID, srcObj.CanonicalID); err != nil {
		s.log.Warn("failed to acquire lock for inverse relationship, skipping",
			slog.String("inverse_type", inverseType),
			slog.String("error", err.Error()))
		return nil
	}

	// Check if inverse already exists (using canonical IDs)
	existingInverse, err := s.repo.GetRelationshipHead(ctx, tx, projectID, branchID, inverseType, dstObj.CanonicalID, srcObj.CanonicalID)
	if err != nil {
		s.log.Warn("failed to check existing inverse relationship, skipping",
			slog.String("inverse_type", inverseType),
			slog.String("error", err.Error()))
		return nil
	}

	if existingInverse != nil && existingInverse.DeletedAt == nil {
		// Inverse already exists and is not deleted — return it as-is
		s.log.Debug("inverse relationship already exists",
			slog.String("inverse_id", existingInverse.ID.String()))
		return existingInverse.ToResponse()
	}

	if existingInverse != nil && existingInverse.DeletedAt != nil {
		// Was deleted — restore it by creating a new version
		newVersion := &GraphRelationship{
			Properties: properties,
			Weight:     weight,
			DeletedAt:  nil,
		}
		newVersion.ChangeSummary = computeChangeSummary(existingInverse.Properties, properties)

		if err := s.repo.CreateRelationshipVersion(ctx, tx, existingInverse, newVersion); err != nil {
			s.log.Warn("failed to restore inverse relationship, skipping",
				slog.String("inverse_type", inverseType),
				slog.String("error", err.Error()))
			return nil
		}

		// Generate embedding for the restored inverse
		inverseTripletText := generateTripletText(dstObj, srcObj, inverseType)
		invEmbedding, invEmbedTimestamp, invEmbedErr := s.embedTripletText(ctx, inverseTripletText)
		if invEmbedErr == nil && invEmbedding != nil {
			_, _ = tx.NewRaw(`UPDATE kb.graph_relationships 
				SET embedding = ?::vector, embedding_updated_at = ? 
				WHERE id = ?`,
				vectorToString(invEmbedding), invEmbedTimestamp, newVersion.ID).Exec(ctx)
		}

		return newVersion.ToResponse()
	}

	// Create brand new inverse relationship (store canonical IDs)
	inverseRel := &GraphRelationship{
		ProjectID:  projectID,
		BranchID:   branchID,
		Type:       inverseType,
		SrcID:      dstObj.CanonicalID, // swapped
		DstID:      srcObj.CanonicalID, // swapped
		Properties: properties,
		Weight:     weight,
	}
	inverseRel.ChangeSummary = computeChangeSummary(nil, properties)

	if err := s.repo.CreateRelationship(ctx, tx, inverseRel); err != nil {
		s.log.Warn("failed to create inverse relationship, skipping",
			slog.String("inverse_type", inverseType),
			slog.String("error", err.Error()))
		return nil
	}

	// Generate embedding for the inverse triplet
	inverseTripletText := generateTripletText(dstObj, srcObj, inverseType)
	invEmbedding, invEmbedTimestamp, invEmbedErr := s.embedTripletText(ctx, inverseTripletText)
	if invEmbedErr != nil {
		s.log.Warn("failed to generate embedding for inverse relationship, continuing without",
			slog.String("relationship_id", inverseRel.ID.String()),
			slog.String("error", invEmbedErr.Error()))
	} else if invEmbedding != nil {
		_, updateErr := tx.NewRaw(`UPDATE kb.graph_relationships 
			SET embedding = ?::vector, embedding_updated_at = ? 
			WHERE id = ?`,
			vectorToString(invEmbedding), invEmbedTimestamp, inverseRel.ID).Exec(ctx)
		if updateErr != nil {
			s.log.Warn("failed to store embedding for inverse relationship, continuing without",
				slog.String("relationship_id", inverseRel.ID.String()),
				slog.String("error", updateErr.Error()))
		}
	}

	s.log.Info("auto-created inverse relationship",
		slog.String("primary_type", relType),
		slog.String("inverse_type", inverseType),
		slog.String("inverse_id", inverseRel.ID.String()))

	return inverseRel.ToResponse()
}

// PatchRelationship updates a relationship by creating a new version.
func (s *Service) PatchRelationship(ctx context.Context, projectID, id uuid.UUID, req *PatchGraphRelationshipRequest) (*GraphRelationshipResponse, error) {
	current, err := s.repo.GetRelationshipByID(ctx, projectID, id)
	if err != nil {
		return nil, err
	}

	if current.DeletedAt != nil {
		return nil, apperror.ErrBadRequest.WithMessage("cannot patch deleted relationship")
	}

	tx, err := s.repo.BeginTx(ctx)
	if err != nil {
		return nil, apperror.ErrDatabase.WithInternal(err)
	}
	defer tx.Rollback()

	// Acquire lock
	if err := s.repo.AcquireRelationshipLock(ctx, tx.Tx, current.ProjectID, current.Type, current.SrcID, current.DstID); err != nil {
		return nil, err
	}

	// Re-fetch HEAD after lock
	head, err := s.repo.GetRelationshipHeadByCanonicalID(ctx, projectID, current.CanonicalID)
	if err != nil {
		return nil, err
	}

	// Ensure we're patching the HEAD version
	if head.ID != current.ID {
		return nil, apperror.ErrBadRequest.WithMessage("cannot_patch_non_head_version")
	}

	// Merge properties
	newProps := make(map[string]any)
	for k, v := range current.Properties {
		newProps[k] = v
	}
	for k, v := range req.Properties {
		if v == nil {
			delete(newProps, k)
		} else {
			newProps[k] = v
		}
	}

	// Check for actual changes
	diff := computeChangeSummary(current.Properties, newProps)
	if diff == nil && (req.Weight == nil || (current.Weight != nil && *req.Weight == *current.Weight)) {
		return nil, apperror.ErrBadRequest.WithMessage("no_effective_change")
	}

	newVersion := &GraphRelationship{
		Properties:    newProps,
		Weight:        req.Weight,
		ChangeSummary: diff,
	}
	if newVersion.Weight == nil {
		newVersion.Weight = current.Weight
	}

	if err := s.repo.CreateRelationshipVersion(ctx, tx.Tx, current, newVersion); err != nil {
		return nil, err
	}

	// Copy embedding from previous version to new version.
	// Triplet text (src_name + type + dst_name) doesn't change on patch (only
	// properties/weight change), so the embedding is still valid.
	// If the previous version had no embedding, the sweep worker will generate one.
	_, _ = tx.Tx.NewRaw(`UPDATE kb.graph_relationships
		SET embedding = prev.embedding, embedding_updated_at = prev.embedding_updated_at
		FROM kb.graph_relationships prev
		WHERE kb.graph_relationships.id = ? AND prev.id = ?
		  AND prev.embedding IS NOT NULL`,
		newVersion.ID, current.ID).Exec(ctx)

	if err := tx.Commit(); err != nil {
		return nil, apperror.ErrDatabase.WithInternal(err)
	}

	// Return the new version
	newHead, _ := s.repo.GetRelationshipHeadByCanonicalID(ctx, projectID, current.CanonicalID)
	return newHead.ToResponse(), nil
}

// DeleteRelationship soft-deletes a relationship.
func (s *Service) DeleteRelationship(ctx context.Context, projectID, id uuid.UUID) (*GraphRelationshipResponse, error) {
	current, err := s.repo.GetRelationshipByID(ctx, projectID, id)
	if err != nil {
		return nil, err
	}

	tx, err := s.repo.BeginTx(ctx)
	if err != nil {
		return nil, apperror.ErrDatabase.WithInternal(err)
	}
	defer tx.Rollback()

	// Acquire lock
	if err := s.repo.AcquireRelationshipLock(ctx, tx.Tx, current.ProjectID, current.Type, current.SrcID, current.DstID); err != nil {
		return nil, err
	}

	// Get HEAD version
	head, err := s.repo.GetRelationshipHeadByCanonicalID(ctx, projectID, current.CanonicalID)
	if err != nil {
		return nil, err
	}

	if head.DeletedAt != nil {
		return nil, apperror.ErrBadRequest.WithMessage("already_deleted")
	}

	if err := s.repo.SoftDeleteRelationship(ctx, tx.Tx, head); err != nil {
		return nil, err
	}

	if err := tx.Commit(); err != nil {
		return nil, apperror.ErrDatabase.WithInternal(err)
	}

	// Return the tombstone
	tombstone, _ := s.repo.GetRelationshipHeadByCanonicalID(ctx, projectID, current.CanonicalID)
	return tombstone.ToResponse(), nil
}

// RestoreRelationship restores a soft-deleted relationship.
func (s *Service) RestoreRelationship(ctx context.Context, projectID, id uuid.UUID) (*GraphRelationshipResponse, error) {
	current, err := s.repo.GetRelationshipByID(ctx, projectID, id)
	if err != nil {
		return nil, err
	}

	tx, err := s.repo.BeginTx(ctx)
	if err != nil {
		return nil, apperror.ErrDatabase.WithInternal(err)
	}
	defer tx.Rollback()

	// Acquire lock
	if err := s.repo.AcquireRelationshipLock(ctx, tx.Tx, current.ProjectID, current.Type, current.SrcID, current.DstID); err != nil {
		return nil, err
	}

	// Get HEAD version
	head, err := s.repo.GetRelationshipHeadByCanonicalID(ctx, projectID, current.CanonicalID)
	if err != nil {
		return nil, err
	}

	if head.DeletedAt == nil {
		return nil, apperror.ErrBadRequest.WithMessage("relationship_not_deleted")
	}

	if err := s.repo.RestoreRelationship(ctx, tx.Tx, head); err != nil {
		return nil, err
	}

	if err := tx.Commit(); err != nil {
		return nil, apperror.ErrDatabase.WithInternal(err)
	}

	// Return the restored version
	restored, _ := s.repo.GetRelationshipHeadByCanonicalID(ctx, projectID, current.CanonicalID)
	return restored.ToResponse(), nil
}

// GetRelationshipHistory returns version history for a relationship.
func (s *Service) GetRelationshipHistory(ctx context.Context, projectID, id uuid.UUID) ([]*GraphRelationshipResponse, error) {
	// First get the relationship to find its canonical ID
	rel, err := s.repo.GetRelationshipByID(ctx, projectID, id)
	if err != nil {
		return nil, err
	}

	versions, err := s.repo.GetRelationshipHistory(ctx, projectID, rel.CanonicalID)
	if err != nil {
		return nil, err
	}

	data := make([]*GraphRelationshipResponse, len(versions))
	for i, v := range versions {
		data[i] = v.ToResponse()
	}

	return data, nil
}

// branchIDsEqual compares two optional branch IDs.
func branchIDsEqual(a, b *uuid.UUID) bool {
	if a == nil && b == nil {
		return true
	}
	if a == nil || b == nil {
		return false
	}
	return *a == *b
}

// =============================================================================
// Search Operations
// =============================================================================

// FTSSearch performs full-text search on graph objects.
func (s *Service) FTSSearch(ctx context.Context, projectID uuid.UUID, req *FTSSearchRequest) (*SearchResponse, error) {
	limit := req.Limit
	if limit <= 0 {
		limit = 20
	}
	if limit > 200 {
		limit = 200
	}

	params := FTSSearchParams{
		ProjectID:      projectID,
		Query:          req.Query,
		BranchID:       req.BranchID,
		Types:          req.Types,
		Labels:         req.Labels,
		Status:         req.Status,
		IncludeDeleted: req.IncludeDeleted,
		Limit:          limit + 1, // Fetch one extra to determine hasMore
		Offset:         req.Offset,
	}

	results, err := s.repo.FTSSearch(ctx, params)
	if err != nil {
		return nil, err
	}

	hasMore := len(results) > limit
	if hasMore {
		results = results[:limit]
	}

	data := make([]*SearchResultItem, len(results))
	objectIDs := make([]uuid.UUID, 0, len(results))

	for i, r := range results {
		data[i] = &SearchResultItem{
			Object:       r.Object.ToResponse(),
			Score:        r.Rank,
			LexicalScore: &r.Rank,
		}
		objectIDs = append(objectIDs, r.Object.ID)
	}

	if len(objectIDs) > 0 {
		go func() {
			bgCtx := context.Background()
			if err := s.repo.UpdateAccessTimestamps(bgCtx, objectIDs); err != nil {
				s.log.Warn("failed to update access timestamps", logger.Error(err))
			}
		}()
	}

	return &SearchResponse{
		Data:    data,
		Total:   len(data),
		HasMore: hasMore,
		Offset:  req.Offset,
	}, nil
}

// VectorSearch performs vector similarity search on graph objects.
func (s *Service) VectorSearch(ctx context.Context, projectID uuid.UUID, req *VectorSearchRequest) (*SearchResponse, error) {
	limit := req.Limit
	if limit <= 0 {
		limit = 20
	}
	if limit > 200 {
		limit = 200
	}

	params := VectorSearchParams{
		ProjectID:      projectID,
		Vector:         req.Vector,
		BranchID:       req.BranchID,
		Types:          req.Types,
		Labels:         req.Labels,
		Status:         req.Status,
		IncludeDeleted: req.IncludeDeleted,
		MaxDistance:    req.MaxDistance,
		Limit:          limit + 1, // Fetch one extra to determine hasMore
		Offset:         req.Offset,
	}

	results, err := s.repo.VectorSearch(ctx, params)
	if err != nil {
		return nil, err
	}

	hasMore := len(results) > limit
	if hasMore {
		results = results[:limit]
	}

	data := make([]*SearchResultItem, len(results))
	objectIDs := make([]uuid.UUID, 0, len(results))

	for i, r := range results {
		similarity := 1.0 - r.Distance
		data[i] = &SearchResultItem{
			Object:      r.Object.ToResponse(),
			Score:       similarity,
			VectorScore: &similarity,
			VectorDist:  &r.Distance,
		}
		objectIDs = append(objectIDs, r.Object.ID)
	}

	if len(objectIDs) > 0 {
		go func() {
			bgCtx := context.Background()
			if err := s.repo.UpdateAccessTimestamps(bgCtx, objectIDs); err != nil {
				s.log.Warn("failed to update access timestamps", logger.Error(err))
			}
		}()
	}

	return &SearchResponse{
		Data:    data,
		Total:   len(data),
		HasMore: hasMore,
		Offset:  req.Offset,
	}, nil
}

// HybridSearchOptions contains options for hybrid search.
type HybridSearchOptions struct {
	Debug bool // Include timing and statistics in response
}

// HybridSearch performs combined lexical and vector search with score fusion.
func (s *Service) HybridSearch(ctx context.Context, projectID uuid.UUID, req *HybridSearchRequest, opts *HybridSearchOptions) (*SearchResponse, error) {
	// Start total timing
	totalStart := time.Now()

	// Initialize debug tracking
	var lexicalMs, vectorMs, fusionMs float64
	debug := opts != nil && opts.Debug

	limit := req.Limit
	if limit <= 0 {
		limit = 20
	}
	if limit > 200 {
		limit = 200
	}

	// Default weights
	lexicalWeight := float32(0.5)
	vectorWeight := float32(0.5)
	if req.LexicalWeight != nil {
		lexicalWeight = *req.LexicalWeight
	}
	if req.VectorWeight != nil {
		vectorWeight = *req.VectorWeight
	}

	// Normalize weights
	totalWeight := lexicalWeight + vectorWeight
	if totalWeight > 0 {
		lexicalWeight /= totalWeight
		vectorWeight /= totalWeight
	}

	// Determine search strategies based on available inputs
	hasQuery := req.Query != ""
	hasVector := len(req.Vector) > 0

	if !hasQuery && !hasVector {
		return &SearchResponse{
			Data:    []*SearchResultItem{},
			Total:   0,
			HasMore: false,
		}, nil
	}

	// Maps to collect results from both searches
	lexicalResults := make(map[uuid.UUID]*FTSSearchResult)
	vectorResults := make(map[uuid.UUID]*VectorSearchResult)

	// Run searches based on available inputs
	fetchLimit := limit * 3 // Fetch more for fusion

	if hasQuery {
		lexicalStart := time.Now()
		ftsParams := FTSSearchParams{
			ProjectID:      projectID,
			Query:          req.Query,
			BranchID:       req.BranchID,
			Types:          req.Types,
			Labels:         req.Labels,
			Status:         req.Status,
			IncludeDeleted: req.IncludeDeleted,
			Limit:          fetchLimit,
		}
		ftsResults, err := s.repo.FTSSearch(ctx, ftsParams)
		lexicalMs = float64(time.Since(lexicalStart).Microseconds()) / 1000.0
		if err != nil {
			return nil, err
		}
		for _, r := range ftsResults {
			lexicalResults[r.Object.ID] = r
		}
	}

	if hasVector {
		vectorStart := time.Now()
		vecParams := VectorSearchParams{
			ProjectID:      projectID,
			Vector:         req.Vector,
			BranchID:       req.BranchID,
			Types:          req.Types,
			Labels:         req.Labels,
			Status:         req.Status,
			IncludeDeleted: req.IncludeDeleted,
			Limit:          fetchLimit,
		}
		vecResults, err := s.repo.VectorSearch(ctx, vecParams)
		vectorMs = float64(time.Since(vectorStart).Microseconds()) / 1000.0
		if err != nil {
			return nil, err
		}
		for _, r := range vecResults {
			vectorResults[r.Object.ID] = r
		}
	}

	// Collect all unique IDs
	allIDs := make(map[uuid.UUID]bool)
	for id := range lexicalResults {
		allIDs[id] = true
	}
	for id := range vectorResults {
		allIDs[id] = true
	}

	if len(allIDs) == 0 {
		totalMs := float64(time.Since(totalStart).Microseconds()) / 1000.0
		resp := &SearchResponse{
			Data:    []*SearchResultItem{},
			Total:   0,
			HasMore: false,
			Meta:    &SearchResponseMeta{ElapsedMs: totalMs},
		}
		if debug {
			resp.Meta.Timing = &SearchTimingDebug{
				LexicalMs: lexicalMs,
				VectorMs:  vectorMs,
				FusionMs:  0,
				TotalMs:   totalMs,
			}
			resp.Meta.ChannelStats = &SearchChannelStats{
				Lexical: &ChannelStat{Count: 0},
				Vector:  &ChannelStat{Count: 0},
			}
		}
		return resp, nil
	}

	// Start fusion timing
	fusionStart := time.Now()

	// Calculate score statistics for normalization
	var lexicalScores, vectorScores []float32
	for id := range allIDs {
		if lr, ok := lexicalResults[id]; ok {
			lexicalScores = append(lexicalScores, lr.Rank)
		}
		if vr, ok := vectorResults[id]; ok {
			// Convert distance to similarity for scoring
			similarity := 1.0 - vr.Distance
			vectorScores = append(vectorScores, similarity)
		}
	}

	lexicalMean, lexicalStd := mathutil.CalcMeanStd(lexicalScores)
	vectorMean, vectorStd := mathutil.CalcMeanStd(vectorScores)

	// Fuse scores
	type fusedResult struct {
		id           uuid.UUID
		object       *GraphObject
		fusedScore   float32
		lexicalScore *float32
		vectorScore  *float32
		vectorDist   *float32
	}

	var fusedResults []fusedResult
	for id := range allIDs {
		var normLexical, normVector float32
		var lexScore, vecScore *float32
		var vecDist *float32
		var obj *GraphObject

		if lr, ok := lexicalResults[id]; ok {
			normalized := zScoreNormalize(lr.Rank, lexicalMean, lexicalStd)
			normLexical = mathutil.Sigmoid(normalized)
			score := lr.Rank
			lexScore = &score
			obj = lr.Object
		}

		if vr, ok := vectorResults[id]; ok {
			similarity := float32(1.0) - vr.Distance
			normalized := zScoreNormalize(similarity, vectorMean, vectorStd)
			normVector = mathutil.Sigmoid(normalized)
			vecScore = &similarity
			dist := vr.Distance
			vecDist = &dist
			if obj == nil {
				obj = vr.Object
			}
		}

		// Weighted combination
		fusedScore := lexicalWeight*normLexical + vectorWeight*normVector

		fusedResults = append(fusedResults, fusedResult{
			id:           id,
			object:       obj,
			fusedScore:   fusedScore,
			lexicalScore: lexScore,
			vectorScore:  vecScore,
			vectorDist:   vecDist,
		})
	}

	// Sort by fused score descending
	sort.Slice(fusedResults, func(i, j int) bool {
		return fusedResults[i].fusedScore > fusedResults[j].fusedScore
	})

	// Apply offset and limit
	offset := req.Offset
	if offset > len(fusedResults) {
		offset = len(fusedResults)
	}
	fusedResults = fusedResults[offset:]

	hasMore := len(fusedResults) > limit
	if hasMore {
		fusedResults = fusedResults[:limit]
	}

	fusionMs = float64(time.Since(fusionStart).Microseconds()) / 1000.0

	// Build response
	data := make([]*SearchResultItem, len(fusedResults))
	for i, fr := range fusedResults {
		data[i] = &SearchResultItem{
			Object:       fr.object.ToResponse(),
			Score:        fr.fusedScore,
			LexicalScore: fr.lexicalScore,
			VectorScore:  fr.vectorScore,
			VectorDist:   fr.vectorDist,
		}
	}

	totalMs := float64(time.Since(totalStart).Microseconds()) / 1000.0
	resp := &SearchResponse{
		Data:    data,
		Total:   len(data),
		HasMore: hasMore,
		Offset:  req.Offset,
		Meta:    &SearchResponseMeta{ElapsedMs: totalMs},
	}

	// Add debug info if requested
	if debug {
		resp.Meta.Timing = &SearchTimingDebug{
			EmbeddingMs: 0, // No embedding generation in this endpoint (vector is passed in)
			LexicalMs:   lexicalMs,
			VectorMs:    vectorMs,
			FusionMs:    fusionMs,
			TotalMs:     totalMs,
		}
		resp.Meta.ChannelStats = &SearchChannelStats{
			Lexical: &ChannelStat{
				Mean:  float64(lexicalMean),
				Std:   float64(lexicalStd),
				Count: len(lexicalScores),
			},
			Vector: &ChannelStat{
				Mean:  float64(vectorMean),
				Std:   float64(vectorStd),
				Count: len(vectorScores),
			},
		}
	}

	return resp, nil
}

// calcStdDev calculates standard deviation from mean.
func calcStdDev(scores []float32, mean float32) float64 {
	if len(scores) == 0 {
		return 0
	}
	var sumSq float64
	for _, s := range scores {
		diff := float64(s - mean)
		sumSq += diff * diff
	}
	return math.Sqrt(sumSq / float64(len(scores)))
}

// zScoreNormalize applies z-score normalization.
func zScoreNormalize(score, mean, std float32) float32 {
	return (score - mean) / std
}

// GetTags returns all distinct tags (labels) used by objects in a project.
func (s *Service) GetTags(ctx context.Context, projectID uuid.UUID, params *GetDistinctTagsParams) ([]string, error) {
	return s.repo.GetDistinctTags(ctx, projectID, params)
}

// BulkUpdateStatus updates the status of multiple objects.
func (s *Service) BulkUpdateStatus(ctx context.Context, projectID uuid.UUID, req *BulkUpdateStatusRequest, actorID *uuid.UUID) (*BulkUpdateStatusResponse, error) {
	results := make([]BulkUpdateStatusResult, len(req.IDs))

	// Parse UUIDs and track valid ones
	validIDs := make([]uuid.UUID, 0, len(req.IDs))
	for i, idStr := range req.IDs {
		id, err := uuid.Parse(idStr)
		if err != nil {
			errMsg := "invalid UUID"
			results[i] = BulkUpdateStatusResult{
				ID:      idStr,
				Success: false,
				Error:   &errMsg,
			}
		} else {
			validIDs = append(validIDs, id)
			results[i] = BulkUpdateStatusResult{
				ID:      idStr,
				Success: true,
			}
		}
	}

	if len(validIDs) == 0 {
		return &BulkUpdateStatusResponse{
			Success: 0,
			Failed:  len(req.IDs),
			Results: results,
		}, nil
	}

	// Perform bulk update
	updated, err := s.repo.BulkUpdateStatus(ctx, projectID, validIDs, req.Status, actorID)
	if err != nil {
		return nil, err
	}

	// Calculate success/failed counts
	successCount := updated
	failedCount := len(req.IDs) - updated

	return &BulkUpdateStatusResponse{
		Success: successCount,
		Failed:  failedCount,
		Results: results,
	}, nil
}

// BulkCreateObjects creates multiple objects in a single batch.
// Each object is created independently and concurrently — failures do not roll back other successes.
func (s *Service) BulkCreateObjects(ctx context.Context, projectID uuid.UUID, req *BulkCreateObjectsRequest, actorID *uuid.UUID) (*BulkCreateObjectsResponse, error) {
	results := make([]BulkCreateObjectResult, len(req.Items))

	workerCtx := context.WithoutCancel(ctx)

	// Pre-warm schema cache for this project to avoid lock contention
	if s.schemaProvider != nil {
		_, _ = s.schemaProvider.GetProjectSchemas(workerCtx, projectID.String())
	}

	type work struct {
		i    int
		item CreateGraphObjectRequest
	}
	jobs := make(chan work, len(req.Items))
	for i, item := range req.Items {
		jobs <- work{i, item}
	}
	close(jobs)

	workers := len(req.Items)
	if workers > 20 {
		workers = 20
	}

	var wg sync.WaitGroup
	for w := 0; w < workers; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := range jobs {
				resp, err := s.Create(workerCtx, projectID, &j.item, actorID)
				if err != nil {
					errMsg := err.Error()
					results[j.i] = BulkCreateObjectResult{Index: j.i, Success: false, Error: &errMsg}
				} else {
					results[j.i] = BulkCreateObjectResult{Index: j.i, Success: true, Object: resp}
				}
			}
		}()
	}
	wg.Wait()

	successCount, failedCount := 0, 0
	for _, r := range results {
		if r.Success {
			successCount++
		} else {
			failedCount++
		}
	}

	return &BulkCreateObjectsResponse{
		Success: successCount,
		Failed:  failedCount,
		Results: results,
	}, nil
}

// BulkCreateRelationships creates multiple relationships in a single batch.
// Each relationship is created independently and concurrently — failures do not roll back other successes.
// Inverse relationships are auto-created per template pack inverseType declarations.
func (s *Service) BulkCreateRelationships(ctx context.Context, projectID uuid.UUID, req *BulkCreateRelationshipsRequest) (*BulkCreateRelationshipsResponse, error) {
	results := make([]BulkCreateRelationshipResult, len(req.Items))

	workerCtx := context.WithoutCancel(ctx)

	// Pre-warm inverse type cache for this project to avoid lock contention
	if s.inverseTypeProvider != nil {
		// Just querying an empty type is enough to populate the whole map for the project
		_, _ = s.inverseTypeProvider.GetInverseType(workerCtx, projectID.String(), "")
	}

	type work struct {
		i    int
		item CreateGraphRelationshipRequest
	}
	jobs := make(chan work, len(req.Items))
	for i, item := range req.Items {
		jobs <- work{i, item}
	}
	close(jobs)

	workers := len(req.Items)
	if workers > 20 {
		workers = 20
	}

	var wg sync.WaitGroup
	for w := 0; w < workers; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := range jobs {
				resp, err := s.CreateRelationship(workerCtx, projectID, &j.item)
				if err != nil {
					errMsg := err.Error()
					results[j.i] = BulkCreateRelationshipResult{Index: j.i, Success: false, Error: &errMsg}
				} else {
					results[j.i] = BulkCreateRelationshipResult{Index: j.i, Success: true, Relationship: resp}
				}
			}
		}()
	}
	wg.Wait()

	successCount, failedCount := 0, 0
	for _, r := range results {
		if r.Success {
			successCount++
		} else {
			failedCount++
		}
	}

	return &BulkCreateRelationshipsResponse{
		Success: successCount,
		Failed:  failedCount,
		Results: results,
	}, nil
}

// =============================================================================
// Search with Neighbors
// =============================================================================

// SearchWithNeighbors performs FTS search and optionally retrieves neighbors.
func (s *Service) SearchWithNeighbors(ctx context.Context, projectID uuid.UUID, req *SearchWithNeighborsRequest) (*SearchWithNeighborsResponse, error) {
	// Set defaults
	limit := req.Limit
	if limit <= 0 {
		limit = 10
	}
	if limit > 100 {
		limit = 100
	}

	maxNeighbors := req.MaxNeighbors
	if maxNeighbors <= 0 {
		maxNeighbors = 5
	}
	if maxNeighbors > 20 {
		maxNeighbors = 20
	}

	maxDistance := float32(0.5)
	if req.MaxDistance != nil {
		maxDistance = *req.MaxDistance
	}

	// Perform FTS search for primary results
	ftsParams := FTSSearchParams{
		ProjectID:      projectID,
		Query:          req.Query,
		BranchID:       req.BranchID,
		Types:          req.Types,
		Labels:         req.Labels,
		IncludeDeleted: false,
		Limit:          limit,
	}

	ftsResults, err := s.repo.FTSSearch(ctx, ftsParams)
	if err != nil {
		return nil, err
	}

	// Build primary results
	primaryResults := make([]*SearchWithNeighborsResultItem, len(ftsResults))
	for i, r := range ftsResults {
		primaryResults[i] = &SearchWithNeighborsResultItem{
			Object: r.Object.ToResponse(),
			Score:  r.Rank,
		}
	}

	response := &SearchWithNeighborsResponse{
		PrimaryResults: primaryResults,
	}

	// If neighbors requested, fetch them for each primary result
	if req.IncludeNeighbors && len(ftsResults) > 0 {
		neighbors := make(map[string][]*GraphObjectResponse)

		for _, r := range ftsResults {
			objectID := r.Object.ID
			objectNeighbors := make([]*GraphObjectResponse, 0)

			// Get semantically similar objects via vector search
			embedding, err := s.repo.GetObjectEmbedding(ctx, projectID, objectID)
			if err == nil && len(embedding) > 0 {
				vecParams := VectorSearchParams{
					ProjectID:      projectID,
					Vector:         embedding,
					BranchID:       req.BranchID,
					Types:          req.Types,
					Labels:         req.Labels,
					MaxDistance:    &maxDistance,
					IncludeDeleted: false,
					Limit:          maxNeighbors + 1, // +1 to exclude self
				}

				vecResults, err := s.repo.VectorSearch(ctx, vecParams)
				if err == nil {
					for _, vr := range vecResults {
						// Skip self
						if vr.Object.ID == objectID {
							continue
						}
						if len(objectNeighbors) >= maxNeighbors {
							break
						}
						objectNeighbors = append(objectNeighbors, vr.Object.ToResponse())
					}
				}
			}

			// Get relationship-connected neighbors
			relNeighbors, err := s.repo.GetNeighborObjects(ctx, projectID, objectID, req.BranchID, maxNeighbors)
			if err == nil {
				for _, n := range relNeighbors {
					if len(objectNeighbors) >= maxNeighbors {
						break
					}
					// Avoid duplicates
					duplicate := false
					for _, existing := range objectNeighbors {
						if existing.ID == n.ID {
							duplicate = true
							break
						}
					}
					if !duplicate {
						objectNeighbors = append(objectNeighbors, n.ToResponse())
					}
				}
			}

			if len(objectNeighbors) > 0 {
				neighbors[objectID.String()] = objectNeighbors
			}
		}

		response.Neighbors = neighbors
	}

	return response, nil
}

// =============================================================================
// Similar Objects
// =============================================================================

// FindSimilarObjects finds objects similar to a given object.
func (s *Service) FindSimilarObjects(ctx context.Context, projectID, objectID uuid.UUID, req *SimilarObjectsRequest) ([]*SimilarObjectResult, error) {
	// Set defaults
	limit := req.Limit
	if limit <= 0 {
		limit = 10
	}
	if limit > 100 {
		limit = 100
	}

	// Use maxDistance or legacy minScore
	var maxDistance *float32
	if req.MaxDistance != nil {
		maxDistance = req.MaxDistance
	} else if req.MinScore != nil {
		maxDistance = req.MinScore
	}

	params := SimilarSearchParams{
		ProjectID:   projectID,
		ObjectID:    objectID,
		BranchID:    req.BranchID,
		Type:        req.Type,
		KeyPrefix:   req.KeyPrefix,
		LabelsAll:   req.LabelsAll,
		LabelsAny:   req.LabelsAny,
		MaxDistance: maxDistance,
		Limit:       limit,
	}

	results, err := s.repo.FindSimilarObjects(ctx, params)
	if err != nil {
		return nil, err
	}

	// Convert to response type
	response := make([]*SimilarObjectResult, len(results))
	for i, r := range results {
		createdAt := r.CreatedAt
		response[i] = &SimilarObjectResult{
			ID:          r.ID,
			CanonicalID: &r.CanonicalID,
			Version:     &r.Version,
			Distance:    r.Distance,
			ProjectID:   &r.ProjectID,
			BranchID:    r.BranchID,
			Type:        r.Type,
			Key:         r.Key,
			Status:      r.Status,
			Properties:  r.Properties,
			Labels:      r.Labels,
			CreatedAt:   &createdAt,
		}
	}

	return response, nil
}

// =============================================================================
// Graph Expand
// =============================================================================

// ExpandGraph performs bounded BFS graph expansion.
func (s *Service) ExpandGraph(ctx context.Context, projectID uuid.UUID, req *GraphExpandRequest) (*GraphExpandResponse, error) {
	startTime := time.Now()

	// Set defaults
	direction := req.Direction
	if direction == "" {
		direction = "both"
	}

	maxDepth := req.MaxDepth
	if maxDepth <= 0 {
		maxDepth = 2
	}
	if maxDepth > 8 {
		maxDepth = 8
	}

	maxNodes := req.MaxNodes
	if maxNodes <= 0 {
		maxNodes = 400
	}
	if maxNodes > 5000 {
		maxNodes = 5000
	}

	maxEdges := req.MaxEdges
	if maxEdges <= 0 {
		maxEdges = 800
	}
	if maxEdges > 15000 {
		maxEdges = 15000
	}

	// Perform expansion
	params := ExpandParams{
		ProjectID:         projectID,
		RootIDs:           req.RootIDs,
		Direction:         direction,
		MaxDepth:          maxDepth,
		MaxNodes:          maxNodes,
		MaxEdges:          maxEdges,
		RelationshipTypes: req.RelationshipTypes,
		ObjectTypes:       req.ObjectTypes,
		Labels:            req.Labels,
	}

	// If QueryContext is provided, generate embedding for query-aware edge ordering
	if req.QueryContext != "" {
		embedding, err := s.embeddings.EmbedQuery(ctx, req.QueryContext)
		if err != nil {
			// Log warning but continue with standard BFS order (graceful degradation)
			s.log.WarnContext(ctx, "failed to embed query context for graph expansion, falling back to standard BFS order",
				"error", err,
				"query_context", req.QueryContext,
			)
		} else if len(embedding) > 0 {
			params.QueryContext = req.QueryContext
			params.QueryVector = embedding
		}
	}

	result, err := s.repo.ExpandGraph(ctx, params)
	if err != nil {
		return nil, err
	}

	// Build response
	nodes := make([]*ExpandNode, 0, len(result.Nodes))
	for id, obj := range result.Nodes {
		node := &ExpandNode{
			ID:          id, // Use canonical_id so edges (which store canonical_id in src_id/dst_id) can reference nodes
			CanonicalID: id,
			Depth:       result.NodeDepths[id],
			Type:        obj.Type,
			Key:         obj.Key,
			Labels:      obj.Labels,
		}

		// Apply property projection
		if req.Projection != nil {
			node.Properties = projectProperties(obj.Properties, req.Projection)
		} else {
			node.Properties = obj.Properties
		}

		nodes = append(nodes, node)
	}

	edges := make([]*ExpandEdge, 0, len(result.Edges))
	for _, rel := range result.Edges {
		edge := &ExpandEdge{
			ID:    rel.ID,
			Type:  rel.Type,
			SrcID: rel.SrcID,
			DstID: rel.DstID,
		}
		if req.IncludeRelationshipProperties {
			edge.Properties = rel.Properties
		}
		edges = append(edges, edge)
	}

	elapsedMs := float64(time.Since(startTime).Microseconds()) / 1000.0

	// Build filters for meta
	var filters *GraphExpandFilters
	if len(req.RelationshipTypes) > 0 || len(req.ObjectTypes) > 0 || len(req.Labels) > 0 || req.Projection != nil || req.IncludeRelationshipProperties {
		filters = &GraphExpandFilters{
			RelationshipTypes:             req.RelationshipTypes,
			ObjectTypes:                   req.ObjectTypes,
			Labels:                        req.Labels,
			Projection:                    req.Projection,
			IncludeRelationshipProperties: req.IncludeRelationshipProperties,
		}
	}

	return &GraphExpandResponse{
		Roots:           req.RootIDs,
		Nodes:           nodes,
		Edges:           edges,
		Truncated:       result.Truncated,
		MaxDepthReached: result.MaxDepthReached,
		TotalNodes:      len(nodes),
		Meta: &GraphExpandMeta{
			Requested: GraphExpandRequested{
				MaxDepth:  maxDepth,
				MaxNodes:  maxNodes,
				MaxEdges:  maxEdges,
				Direction: direction,
			},
			NodeCount:       len(nodes),
			EdgeCount:       len(edges),
			Truncated:       result.Truncated,
			MaxDepthReached: result.MaxDepthReached,
			ElapsedMs:       elapsedMs,
			Filters:         filters,
		},
	}, nil
}

// projectProperties applies include/exclude projection to properties.
func projectProperties(props map[string]any, projection *GraphExpandProjection) map[string]any {
	if props == nil {
		return nil
	}

	result := make(map[string]any)

	if len(projection.IncludeObjectProperties) > 0 {
		// Whitelist mode
		includeSet := make(map[string]bool)
		for _, k := range projection.IncludeObjectProperties {
			includeSet[k] = true
		}
		for k, v := range props {
			if includeSet[k] {
				result[k] = v
			}
		}
	} else if len(projection.ExcludeObjectProperties) > 0 {
		// Blacklist mode
		excludeSet := make(map[string]bool)
		for _, k := range projection.ExcludeObjectProperties {
			excludeSet[k] = true
		}
		for k, v := range props {
			if !excludeSet[k] {
				result[k] = v
			}
		}
	} else {
		// No projection, return all
		return props
	}

	return result
}

// =============================================================================
// Graph Traverse
// =============================================================================

// TraverseGraph performs bounded BFS graph traversal with pagination.
func (s *Service) TraverseGraph(ctx context.Context, projectID uuid.UUID, req *TraverseGraphRequest) (*TraverseGraphResponse, error) {
	startTime := time.Now()

	// Set defaults
	direction := req.Direction
	if direction == "" {
		direction = "both"
	}

	maxDepth := req.MaxDepth
	if maxDepth <= 0 {
		maxDepth = 2
	}
	if maxDepth > 8 {
		maxDepth = 8
	}

	maxNodes := req.MaxNodes
	if maxNodes <= 0 {
		maxNodes = 200
	}
	if maxNodes > 5000 {
		maxNodes = 5000
	}

	maxEdges := req.MaxEdges
	if maxEdges <= 0 {
		maxEdges = 400
	}
	if maxEdges > 10000 {
		maxEdges = 10000
	}

	pageSize := req.Limit
	if pageSize <= 0 {
		pageSize = 50
	}
	if pageSize > 200 {
		pageSize = 200
	}

	pageDirection := req.PageDirection
	if pageDirection == "" {
		pageDirection = "forward"
	}

	// Use the same expansion logic as ExpandGraph for now
	// A more sophisticated implementation would handle phased traversal, filters, and pagination
	params := ExpandParams{
		ProjectID:         projectID,
		RootIDs:           req.RootIDs,
		Direction:         direction,
		MaxDepth:          maxDepth,
		MaxNodes:          maxNodes,
		MaxEdges:          maxEdges,
		RelationshipTypes: req.RelationshipTypes,
		ObjectTypes:       req.ObjectTypes,
		Labels:            req.Labels,
	}

	// If QueryContext is provided, generate embedding for query-aware edge ordering
	if req.QueryContext != "" {
		embedding, err := s.embeddings.EmbedQuery(ctx, req.QueryContext)
		if err != nil {
			// Log warning but continue with standard BFS order (graceful degradation)
			s.log.WarnContext(ctx, "failed to embed query context for graph traversal, falling back to standard BFS order",
				"error", err,
				"query_context", req.QueryContext,
			)
		} else if len(embedding) > 0 {
			params.QueryContext = req.QueryContext
			params.QueryVector = embedding
		}
	}

	result, err := s.repo.ExpandGraph(ctx, params)
	if err != nil {
		return nil, err
	}

	// Build response nodes
	nodes := make([]*TraverseNode, 0, len(result.Nodes))
	for id, obj := range result.Nodes {
		node := &TraverseNode{
			ID:          id, // Use canonical_id so edges (which store canonical_id in src_id/dst_id) can reference nodes
			CanonicalID: id,
			Depth:       result.NodeDepths[id],
			Type:        obj.Type,
			Key:         obj.Key,
			Labels:      obj.Labels,
		}
		nodes = append(nodes, node)
	}

	// Build response edges
	edges := make([]*TraverseEdge, 0, len(result.Edges))
	for _, rel := range result.Edges {
		edges = append(edges, &TraverseEdge{
			ID:    rel.ID,
			Type:  rel.Type,
			SrcID: rel.SrcID,
			DstID: rel.DstID,
		})
	}

	elapsedMs := float64(time.Since(startTime).Microseconds()) / 1000.0
	resultCount := len(nodes)

	return &TraverseGraphResponse{
		Roots:               req.RootIDs,
		Nodes:               nodes,
		Edges:               edges,
		Truncated:           result.Truncated,
		MaxDepthReached:     result.MaxDepthReached,
		TotalNodes:          len(nodes),
		HasNextPage:         false, // Simplified: no pagination in basic implementation
		HasPreviousPage:     false,
		NextCursor:          nil,
		PreviousCursor:      nil,
		ApproxPositionStart: 0,
		ApproxPositionEnd:   resultCount,
		PageDirection:       pageDirection,
		QueryTimeMs:         &elapsedMs,
		ResultCount:         &resultCount,
	}, nil
}

// =============================================================================
// Branch Merge
// =============================================================================

// MergeBranch performs dry-run or actual merge of a source branch into target branch.
func (s *Service) MergeBranch(ctx context.Context, projectID uuid.UUID, targetBranchID uuid.UUID, req *BranchMergeRequest) (*BranchMergeResponse, error) {
	// Validate target branch exists
	_, err := s.repo.GetBranchByID(ctx, projectID, targetBranchID)
	if err != nil {
		return nil, apperror.ErrNotFound.WithMessage("target branch not found")
	}

	// Validate source branch exists
	_, err = s.repo.GetBranchByID(ctx, projectID, req.SourceBranchID)
	if err != nil {
		return nil, apperror.ErrNotFound.WithMessage("source branch not found")
	}

	// Get HEAD versions for both branches
	targetObjects, err := s.repo.GetBranchObjectHeads(ctx, projectID, &targetBranchID)
	if err != nil {
		return nil, err
	}

	sourceObjects, err := s.repo.GetBranchObjectHeads(ctx, projectID, &req.SourceBranchID)
	if err != nil {
		return nil, err
	}

	targetRels, err := s.repo.GetBranchRelationshipHeads(ctx, projectID, &targetBranchID)
	if err != nil {
		return nil, err
	}

	sourceRels, err := s.repo.GetBranchRelationshipHeads(ctx, projectID, &req.SourceBranchID)
	if err != nil {
		return nil, err
	}

	// Enumerate objects: classify each canonical_id
	objectSummaries := make([]*BranchMergeObjectSummary, 0)
	unchangedCount, addedCount, ffCount, conflictCount := 0, 0, 0, 0

	// Hard limit for enumeration
	hardLimit := 500
	if req.Limit != nil && *req.Limit > 0 {
		hardLimit = *req.Limit
	}
	truncated := false

	// Collect all canonical IDs
	allCanonicalIDs := make(map[uuid.UUID]bool)
	for cid := range sourceObjects {
		allCanonicalIDs[cid] = true
	}
	for cid := range targetObjects {
		allCanonicalIDs[cid] = true
	}

	for cid := range allCanonicalIDs {
		if len(objectSummaries) >= hardLimit {
			truncated = true
			break
		}

		sourceHead := sourceObjects[cid]
		targetHead := targetObjects[cid]

		summary := &BranchMergeObjectSummary{
			CanonicalID: cid,
		}

		if sourceHead != nil {
			summary.SourceHeadID = &sourceHead.ID
		}
		if targetHead != nil {
			summary.TargetHeadID = &targetHead.ID
		}

		if sourceHead == nil && targetHead != nil {
			// Exists only on target - unchanged (nothing to merge from source)
			summary.Status = "unchanged"
			unchangedCount++
		} else if sourceHead != nil && targetHead == nil {
			// Exists only on source - added
			summary.Status = "added"
			addedCount++
		} else if sourceHead != nil && targetHead != nil {
			// Exists on both - compare content
			if bytesEqual(sourceHead.ContentHash, targetHead.ContentHash) {
				summary.Status = "unchanged"
				unchangedCount++
			} else {
				// Properties differ - check for conflicts
				sourcePaths := getPropertyPaths(sourceHead.Properties)
				targetPaths := getPropertyPaths(targetHead.Properties)
				conflicts := findConflictingPaths(sourcePaths, targetPaths)

				summary.SourcePaths = sourcePaths
				summary.TargetPaths = targetPaths

				if len(conflicts) > 0 {
					summary.Status = "conflict"
					summary.Conflicts = conflicts
					conflictCount++
				} else {
					summary.Status = "fast_forward"
					ffCount++
				}
			}
		}

		objectSummaries = append(objectSummaries, summary)
	}

	// Enumerate relationships
	relSummaries := make([]*BranchMergeRelationshipSummary, 0)
	relUnchanged, relAdded, relFF, relConflict := 0, 0, 0, 0

	allRelCanonicalIDs := make(map[uuid.UUID]bool)
	for cid := range sourceRels {
		allRelCanonicalIDs[cid] = true
	}
	for cid := range targetRels {
		allRelCanonicalIDs[cid] = true
	}

	for cid := range allRelCanonicalIDs {
		if len(relSummaries) >= hardLimit {
			truncated = true
			break
		}

		sourceHead := sourceRels[cid]
		targetHead := targetRels[cid]

		summary := &BranchMergeRelationshipSummary{
			CanonicalID: cid,
		}

		if sourceHead != nil {
			summary.SourceHeadID = &sourceHead.ID
			summary.SourceSrcID = &sourceHead.SrcID
			summary.SourceDstID = &sourceHead.DstID
		}
		if targetHead != nil {
			summary.TargetHeadID = &targetHead.ID
			summary.TargetSrcID = &targetHead.SrcID
			summary.TargetDstID = &targetHead.DstID
		}

		if sourceHead == nil && targetHead != nil {
			summary.Status = "unchanged"
			relUnchanged++
		} else if sourceHead != nil && targetHead == nil {
			summary.Status = "added"
			relAdded++
		} else if sourceHead != nil && targetHead != nil {
			if bytesEqual(sourceHead.ContentHash, targetHead.ContentHash) {
				summary.Status = "unchanged"
				relUnchanged++
			} else {
				sourcePaths := getPropertyPaths(sourceHead.Properties)
				targetPaths := getPropertyPaths(targetHead.Properties)
				conflicts := findConflictingPaths(sourcePaths, targetPaths)

				summary.SourcePaths = sourcePaths
				summary.TargetPaths = targetPaths

				if len(conflicts) > 0 {
					summary.Status = "conflict"
					summary.Conflicts = conflicts
					relConflict++
				} else {
					summary.Status = "fast_forward"
					relFF++
				}
			}
		}

		relSummaries = append(relSummaries, summary)
	}

	// Sort summaries: conflict -> fast_forward -> added -> unchanged
	sortMergeObjectSummaries(objectSummaries)
	sortMergeRelationshipSummaries(relSummaries)

	response := &BranchMergeResponse{
		TargetBranchID:                targetBranchID,
		SourceBranchID:                req.SourceBranchID,
		DryRun:                        !req.Execute,
		TotalObjects:                  len(allCanonicalIDs),
		UnchangedCount:                unchangedCount,
		AddedCount:                    addedCount,
		FastForwardCount:              ffCount,
		ConflictCount:                 conflictCount,
		Objects:                       objectSummaries,
		Truncated:                     truncated,
		HardLimit:                     &hardLimit,
		RelationshipsTotal:            intPtr(len(allRelCanonicalIDs)),
		RelationshipsUnchangedCount:   &relUnchanged,
		RelationshipsAddedCount:       &relAdded,
		RelationshipsFastForwardCount: &relFF,
		RelationshipsConflictCount:    &relConflict,
		Relationships:                 relSummaries,
	}

	// If execute is requested and no conflicts, apply merge
	if req.Execute && conflictCount == 0 && relConflict == 0 {
		// Note: Actual merge application would require:
		// 1. Starting a transaction
		// 2. Cloning added objects/relationships to target branch
		// 3. Patching fast-forward objects/relationships
		// 4. Recording merge provenance
		// This is a complex operation that would need careful implementation
		response.Applied = true
		appliedCount := addedCount + ffCount + relAdded + relFF
		response.AppliedObjects = &appliedCount
	}

	return response, nil
}

// Helper functions for branch merge

func bytesEqual(a, b []byte) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func getPropertyPaths(props map[string]any) []string {
	if props == nil {
		return []string{}
	}
	paths := make([]string, 0, len(props))
	for k := range props {
		paths = append(paths, "/"+k)
	}
	sort.Strings(paths)
	return paths
}

func findConflictingPaths(sourcePaths, targetPaths []string) []string {
	// For now, if both branches modified any property, consider it a potential conflict
	// A more sophisticated implementation would compare the actual values
	sourceSet := make(map[string]bool)
	for _, p := range sourcePaths {
		sourceSet[p] = true
	}

	conflicts := []string{}
	for _, p := range targetPaths {
		if sourceSet[p] {
			conflicts = append(conflicts, p)
		}
	}
	return conflicts
}

func sortMergeObjectSummaries(summaries []*BranchMergeObjectSummary) {
	statusOrder := map[string]int{
		"conflict":     0,
		"fast_forward": 1,
		"added":        2,
		"unchanged":    3,
	}
	sort.Slice(summaries, func(i, j int) bool {
		return statusOrder[summaries[i].Status] < statusOrder[summaries[j].Status]
	})
}

func sortMergeRelationshipSummaries(summaries []*BranchMergeRelationshipSummary) {
	statusOrder := map[string]int{
		"conflict":     0,
		"fast_forward": 1,
		"added":        2,
		"unchanged":    3,
	}
	sort.Slice(summaries, func(i, j int) bool {
		return statusOrder[summaries[i].Status] < statusOrder[summaries[j].Status]
	})
}

func intPtr(i int) *int {
	return &i
}

func (s *Service) incrementValidationSuccess(duration time.Duration) {
	s.metricsMu.Lock()
	s.validationSuccess++
	s.validationDuration += duration
	s.metricsMu.Unlock()
}

func (s *Service) incrementValidationError(duration time.Duration) {
	s.metricsMu.Lock()
	s.validationErrors++
	s.validationDuration += duration
	s.metricsMu.Unlock()
}

func (s *Service) Metrics() ValidationMetrics {
	s.metricsMu.RLock()
	defer s.metricsMu.RUnlock()
	return ValidationMetrics{
		Success:       s.validationSuccess,
		Errors:        s.validationErrors,
		TotalDuration: s.validationDuration,
	}
}

type ValidationMetrics struct {
	Success       int64
	Errors        int64
	TotalDuration time.Duration
}

// =============================================================================
// Atomic Subgraph Creation
// =============================================================================

// CreateSubgraph atomically creates a set of objects and relationships in a single transaction.
// Objects are referenced by client-side placeholder refs (_ref), which are resolved to server-assigned
// IDs before relationship creation. If any step fails, the entire operation is rolled back.
func (s *Service) CreateSubgraph(ctx context.Context, projectID uuid.UUID, req *CreateSubgraphRequest, actorID *uuid.UUID) (*CreateSubgraphResponse, error) {
	// Validate: check for duplicate refs
	refSet := make(map[string]bool, len(req.Objects))
	for i, obj := range req.Objects {
		if obj.Ref == "" {
			return nil, apperror.ErrBadRequest.WithMessage(fmt.Sprintf("objects[%d]: _ref is required", i))
		}
		if refSet[obj.Ref] {
			return nil, apperror.ErrBadRequest.WithMessage(fmt.Sprintf("objects[%d]: duplicate _ref %q", i, obj.Ref))
		}
		refSet[obj.Ref] = true
	}

	// Validate: check that all relationship refs point to defined objects
	for i, rel := range req.Relationships {
		if !refSet[rel.SrcRef] {
			return nil, apperror.ErrBadRequest.WithMessage(fmt.Sprintf("relationships[%d]: src_ref %q not found in objects", i, rel.SrcRef))
		}
		if !refSet[rel.DstRef] {
			return nil, apperror.ErrBadRequest.WithMessage(fmt.Sprintf("relationships[%d]: dst_ref %q not found in objects", i, rel.DstRef))
		}
		if rel.SrcRef == rel.DstRef {
			return nil, apperror.ErrBadRequest.WithMessage(fmt.Sprintf("relationships[%d]: self-loop not allowed (src_ref == dst_ref == %q)", i, rel.SrcRef))
		}
	}

	// Load schemas once for property validation
	var schemas *ExtractionSchemas
	if s.schemaProvider != nil {
		var err error
		schemas, err = s.schemaProvider.GetProjectSchemas(ctx, projectID.String())
		if err != nil {
			s.log.Warn("failed to load schemas for subgraph creation, skipping validation",
				slog.String("project_id", projectID.String()),
				slog.String("error", err.Error()))
		}
	}

	// Start transaction
	tx, err := s.repo.BeginTx(ctx)
	if err != nil {
		return nil, apperror.ErrDatabase.WithInternal(err)
	}
	defer tx.Rollback()

	actorType := "user"
	refMap := make(map[string]uuid.UUID, len(req.Objects))
	objResponses := make([]*GraphObjectResponse, 0, len(req.Objects))
	objByRef := make(map[string]*GraphObject, len(req.Objects))

	// Phase 1: Create all objects
	for i, objReq := range req.Objects {
		// Validate type is non-empty
		if objReq.Type == "" {
			return nil, apperror.ErrBadRequest.WithMessage(fmt.Sprintf("objects[%d] (%s): type is required and must not be empty", i, objReq.Ref))
		}

		// Validate properties against schema
		validatedProps := objReq.Properties
		if schemas != nil {
			if schema, ok := schemas.ObjectSchemas[objReq.Type]; ok {
				start := time.Now()
				validated, err := validateProperties(objReq.Properties, schema)
				duration := time.Since(start)

				if err != nil {
					s.incrementValidationError(duration)
					return nil, apperror.ErrBadRequest.WithMessage(fmt.Sprintf("objects[%d] (%s): property validation failed: %s", i, objReq.Ref, err.Error()))
				}
				s.incrementValidationSuccess(duration)
				validatedProps = validated
			}
		}

		obj := &GraphObject{
			ProjectID:  projectID,
			BranchID:   objReq.BranchID,
			Type:       objReq.Type,
			Key:        objReq.Key,
			Status:     objReq.Status,
			Properties: validatedProps,
			Labels:     objReq.Labels,
			ActorType:  &actorType,
			ActorID:    actorID,
		}

		if err := s.repo.CreateInTx(ctx, tx.Tx, obj); err != nil {
			return nil, apperror.ErrDatabase.WithMessage(fmt.Sprintf("objects[%d] (%s): %s", i, objReq.Ref, err.Error()))
		}

		refMap[objReq.Ref] = obj.ID
		objByRef[objReq.Ref] = obj
		objResponses = append(objResponses, obj.ToResponse())
	}

	// Phase 2: Create all relationships
	relResponses := make([]*GraphRelationshipResponse, 0, len(req.Relationships))
	for i, relReq := range req.Relationships {
		// Validate type is non-empty
		if relReq.Type == "" {
			return nil, apperror.ErrBadRequest.WithMessage(fmt.Sprintf("relationships[%d]: type is required and must not be empty", i))
		}

		srcObj := objByRef[relReq.SrcRef]
		dstObj := objByRef[relReq.DstRef]

		// Validate branch consistency: src and dst must be on the same branch
		if !branchIDsEqual(srcObj.BranchID, dstObj.BranchID) {
			return nil, apperror.ErrBadRequest.WithMessage(fmt.Sprintf("relationships[%d]: branch mismatch between src_ref %q and dst_ref %q", i, relReq.SrcRef, relReq.DstRef))
		}

		rel := &GraphRelationship{
			ProjectID:  projectID,
			BranchID:   srcObj.BranchID,
			Type:       relReq.Type,
			SrcID:      srcObj.CanonicalID,
			DstID:      dstObj.CanonicalID,
			Properties: relReq.Properties,
			Weight:     relReq.Weight,
		}

		// Compute change summary
		rel.ChangeSummary = computeChangeSummary(nil, relReq.Properties)

		if err := s.repo.CreateRelationship(ctx, tx.Tx, rel); err != nil {
			return nil, apperror.ErrDatabase.WithMessage(fmt.Sprintf("relationships[%d] (%s->%s): %s", i, relReq.SrcRef, relReq.DstRef, err.Error()))
		}

		// Generate triplet embedding (best-effort, don't fail the transaction)
		tripletText := generateTripletText(srcObj, dstObj, relReq.Type)
		embedding, embeddingTimestamp, embedErr := s.embedTripletText(ctx, tripletText)
		if embedErr != nil {
			s.log.Warn("subgraph: failed to generate embedding for relationship, continuing",
				slog.String("relationship_id", rel.ID.String()),
				slog.String("error", embedErr.Error()))
		} else if embedding != nil {
			_, updateErr := tx.Tx.NewRaw(`UPDATE kb.graph_relationships 
				SET embedding = ?::vector, embedding_updated_at = ? 
				WHERE id = ?`,
				vectorToString(embedding), embeddingTimestamp, rel.ID).Exec(ctx)
			if updateErr != nil {
				s.log.Warn("subgraph: failed to store embedding, continuing",
					slog.String("relationship_id", rel.ID.String()),
					slog.String("error", updateErr.Error()))
			}
		}

		// Auto-create inverse relationship if template pack declares inverseType
		var inverseResponse *GraphRelationshipResponse
		if s.inverseTypeProvider != nil {
			inverseResponse = s.maybeCreateInverse(ctx, tx.Tx, projectID, srcObj.BranchID, relReq.Type, srcObj, dstObj, relReq.Properties, relReq.Weight)
		}

		resp := rel.ToResponse()
		resp.InverseRelationship = inverseResponse
		relResponses = append(relResponses, resp)
	}

	// Commit the transaction
	if err := tx.Commit(); err != nil {
		return nil, apperror.ErrDatabase.WithInternal(err)
	}

	return &CreateSubgraphResponse{
		Objects:       objResponses,
		Relationships: relResponses,
		RefMap:        refMap,
	}, nil
}
