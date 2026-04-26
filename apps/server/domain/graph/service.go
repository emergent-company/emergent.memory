package graph

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"math"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/uptrace/bun"

	"github.com/emergent-company/emergent.memory/domain/events"
	"github.com/emergent-company/emergent.memory/domain/extraction/agents"
	"github.com/emergent-company/emergent.memory/domain/journal"
	"github.com/emergent-company/emergent.memory/pkg/apperror"
	"github.com/emergent-company/emergent.memory/pkg/logger"
	"github.com/emergent-company/emergent.memory/pkg/mathutil"
)

// ExtractionSchemas contains object and relationship schemas.
type ExtractionSchemas struct {
	ObjectSchemas       map[string]agents.ObjectSchema
	RelationshipSchemas map[string]agents.RelationshipSchema
}

// SchemaProvider provides access to template pack schemas for validation.
type SchemaProvider interface {
	GetProjectSchemas(ctx context.Context, projectID string) (*ExtractionSchemas, error)
	// InvalidateProjectCache evicts the cached schemas for a project so the next
	// call to GetProjectSchemas fetches fresh data from the database. Call this
	// after any schema install, update, or removal.
	InvalidateProjectCache(projectID string)
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

// RelationshipEmbeddingEnqueuer enqueues graph relationships for asynchronous embedding generation.
// This is satisfied by extraction.GraphRelationshipEmbeddingJobsService via an adapter in module.go.
type RelationshipEmbeddingEnqueuer interface {
	EnqueueRelationshipEmbedding(ctx context.Context, relationshipID string) error
}

// branchStoreIface is a minimal interface for branch store operations needed by the graph service.
type branchStoreIface interface {
	SetMergedAt(ctx context.Context, branchID string, mergedAt time.Time) error
}

// Service handles business logic for graph operations.
type Service struct {
	repo                 *Repository
	log                  *slog.Logger
	schemaProvider       SchemaProvider
	inverseTypeProvider  InverseTypeProvider
	embeddings           EmbeddingService
	embeddingEnqueuer    EmbeddingEnqueuer
	relEmbeddingEnqueuer RelationshipEmbeddingEnqueuer
	journal              *journal.Service
	branchStore          branchStoreIface
	events               *events.Service

	// Metrics
	metricsMu          sync.RWMutex
	validationSuccess  int64
	validationErrors   int64
	validationDuration time.Duration
}

// NewService creates a new graph service.
func NewService(repo *Repository, log *slog.Logger, schemaProvider SchemaProvider, inverseTypeProvider InverseTypeProvider, embeddings EmbeddingService, embeddingEnqueuer EmbeddingEnqueuer, relEmbeddingEnqueuer RelationshipEmbeddingEnqueuer, journalSvc *journal.Service, branchStore branchStoreIface, eventsSvc *events.Service) *Service {
	return &Service{
		repo:                 repo,
		log:                  log.With(logger.Scope("graph.svc")),
		schemaProvider:       schemaProvider,
		inverseTypeProvider:  inverseTypeProvider,
		embeddings:           embeddings,
		embeddingEnqueuer:    embeddingEnqueuer,
		relEmbeddingEnqueuer: relEmbeddingEnqueuer,
		journal:              journalSvc,
		branchStore:          branchStore,
		events:               eventsSvc,
	}
}

// emitObjectCreated emits a graph_object entity.created event (no-op if events not wired).
func (s *Service) emitObjectCreated(obj *GraphObjectResponse) {
	if s.events == nil {
		return
	}
	s.events.EmitCreated(events.EntityGraphObject, obj.CanonicalID.String(), obj.ProjectID.String(), &events.EmitOptions{
		Version:    &obj.Version,
		ObjectType: obj.Type,
	})
}

// emitObjectUpdated emits a graph_object entity.updated event (no-op if events not wired).
func (s *Service) emitObjectUpdated(obj *GraphObjectResponse) {
	if s.events == nil {
		return
	}
	s.events.EmitUpdated(events.EntityGraphObject, obj.CanonicalID.String(), obj.ProjectID.String(), &events.EmitOptions{
		Version:    &obj.Version,
		ObjectType: obj.Type,
	})
}

// emitObjectDeleted emits a graph_object entity.deleted event (no-op if events not wired).
func (s *Service) emitObjectDeleted(projectID, canonicalID, objType string) {
	if s.events == nil {
		return
	}
	s.events.EmitDeleted(events.EntityGraphObject, canonicalID, projectID, &events.EmitOptions{
		ObjectType: objType,
	})
}

// nameFromProps extracts the "name" property from a properties map, or returns an empty string.
func nameFromProps(props map[string]any) string {
	if props == nil {
		return ""
	}
	if v, ok := props["name"].(string); ok {
		return v
	}
	return ""
}

// entityTypeObject is the entity type constant for graph objects.
const entityTypeObject = journal.EntityObject

// entityTypeRelationship is the entity type constant for graph relationships.
const entityTypeRelationship = journal.EntityRelationship

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

// enqueueRelationshipEmbedding enqueues a graph relationship for async embedding generation.
// Logs and swallows errors — embedding is best-effort and must never block CRUD.
func (s *Service) enqueueRelationshipEmbedding(ctx context.Context, relationshipID string) {
	if s.relEmbeddingEnqueuer == nil {
		return
	}
	if err := s.relEmbeddingEnqueuer.EnqueueRelationshipEmbedding(ctx, relationshipID); err != nil {
		s.log.Warn("failed to enqueue relationship embedding job",
			slog.String("relationship_id", relationshipID),
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

// maxListLimit is the maximum number of items returned per page for list endpoints.
// It must match the cap applied in repository.go to ensure hasMore detection is consistent.
const maxListLimit = 5000

// List returns graph objects matching the given parameters.
func (s *Service) List(ctx context.Context, params ListParams) (*SearchGraphObjectsResponse, error) {
	// Cap the limit so the service and repository agree on the effective page size.
	// Without this cap, the repository silently clamps params.Limit in its own stack
	// frame, causing hasMore to evaluate against the unclamped value and drop next_cursor.
	if params.Limit <= 0 {
		params.Limit = 50
	}
	if params.Limit > maxListLimit {
		params.Limit = maxListLimit
	}

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

	// Build paginated response
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
// ValidateObjectRequest is the request body for the validate endpoint.
type ValidateObjectRequest struct {
	Type       string         `json:"type"`
	Properties map[string]any `json:"properties"`
}

// ValidateObjectResponse is the response from the validate endpoint.
type ValidateObjectResponse struct {
	Valid             bool           `json:"valid"`
	CoercedProperties map[string]any `json:"coercedProperties,omitempty"`
	Errors            []string       `json:"errors,omitempty"`
}

// ValidateObject checks whether the given type and properties would pass schema
// validation without creating any object. Returns coerced properties on success.
func (s *Service) ValidateObject(ctx context.Context, projectID uuid.UUID, req *ValidateObjectRequest) (*ValidateObjectResponse, error) {
	if s.schemaProvider == nil {
		return &ValidateObjectResponse{Valid: true, CoercedProperties: req.Properties}, nil
	}

	schemas, err := s.schemaProvider.GetProjectSchemas(ctx, projectID.String())
	if err != nil {
		return nil, apperror.ErrInternal.WithMessage("failed to load schemas")
	}

	// No schemas installed — everything is valid
	if schemas == nil || schemas.ObjectSchemas == nil {
		return &ValidateObjectResponse{Valid: true, CoercedProperties: req.Properties}, nil
	}

	schema, ok := schemas.ObjectSchemas[req.Type]
	if !ok {
		// Unknown object types are allowed — the schema defines constraints
		// for known types but does not act as an allowlist.
		return &ValidateObjectResponse{Valid: true, CoercedProperties: req.Properties}, nil
	}

	coerced, err := validateProperties(req.Properties, schema)
	if err != nil {
		return &ValidateObjectResponse{
			Valid:  false,
			Errors: []string{err.Error()},
		}, nil
	}

	return &ValidateObjectResponse{Valid: true, CoercedProperties: coerced}, nil
}

func (s *Service) Create(ctx context.Context, projectID uuid.UUID, req *CreateGraphObjectRequest, actorID *uuid.UUID) (*GraphObjectResponse, error) {
	actorType := "user"

	validatedProps := req.Properties
	if s.schemaProvider != nil {
		schemas, err := s.schemaProvider.GetProjectSchemas(ctx, projectID.String())
		if err != nil {
			s.log.Warn("failed to load schemas, skipping validation",
				slog.String("project_id", projectID.String()),
				slog.String("error", err.Error()))
		} else if schemas.ObjectSchemas != nil {
			if schema, ok := schemas.ObjectSchemas[req.Type]; ok {
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
			// Unknown object types are allowed — the schema defines constraints
			// for known types but does not act as an allowlist. Users may create
			// objects with any type name, including domain-specific ones like
			// ServiceMethod, Scenario, Context, etc.
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

	// Sync status column from properties["status"] — properties is the
	// authoritative store, so it always wins over the req.Status field.
	if st, ok := validatedProps["status"].(string); ok && st != "" {
		obj.Status = &st
	}

	if err := s.repo.Create(ctx, obj); err != nil {
		return nil, err
	}

	s.enqueueEmbedding(ctx, obj.ID.String())

	if s.journal != nil && obj.Key != nil {
		objType := obj.Type
		entityType := entityTypeObject
		s.journal.Log(ctx, journal.LogParams{
			ProjectID:  projectID,
			BranchID:   obj.BranchID,
			EventType:  journal.EventTypeCreated,
			EntityType: &entityType,
			ObjectType: &objType,
			ActorType:  journal.ActorUser,
			ActorID:    actorID,
			Metadata: map[string]any{
				"key":         *obj.Key,
				"name":        nameFromProps(obj.Properties),
				"object_type": obj.Type,
			},
		})
	}

	resp := obj.ToResponse()
	s.emitObjectCreated(resp)
	return resp, nil
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
		} else if schemas.ObjectSchemas != nil {
			if schema, ok := schemas.ObjectSchemas[req.Type]; ok {
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
			// Unknown object types are allowed — the schema defines constraints
			// for known types but does not act as an allowlist.
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

		resp := obj.ToResponse()
		s.emitObjectCreated(resp)
		return resp, true, nil
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

		resp := newVersion.ToResponse()
		s.emitObjectUpdated(resp)
		return resp, false, nil
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
		// Re-enqueue for embedding if not yet embedded (e.g. after an API key recovery).
		if existing.EmbeddingUpdatedAt == nil {
			s.enqueueEmbedding(ctx, existing.ID.String())
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

	s.emitObjectUpdated(newVersion.ToResponse())

	return newVersion.ToResponse(), false, nil
}

// Patch updates a graph object by creating a new version.
func (s *Service) Patch(ctx context.Context, projectID, id uuid.UUID, req *PatchGraphObjectRequest, actorID *uuid.UUID) (*GraphObjectResponse, error) {
	// Get current HEAD (initial lookup to resolve canonical ID)
	current, err := s.repo.GetByID(ctx, projectID, id)
	if err != nil {
		return nil, err
	}

	// If a branch is specified, re-fetch the HEAD for that branch using the canonical ID.
	// GetByID does not filter by branch, so it may return the wrong HEAD when an object
	// exists on multiple branches.
	if req.BranchID != nil {
		current, err = s.repo.GetHeadByCanonicalID(ctx, s.repo.DB(), projectID, current.CanonicalID, req.BranchID)
		if err != nil {
			return nil, err
		}
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

	// Validate patch delta properties against current schema.
	// We only check the keys being added/changed by this patch — not the full merged set —
	// so that objects created under an older schema version (with now-removed properties) can
	// still be patched on unrelated fields. Required-field enforcement is intentionally skipped
	// here; it applies at Create time, not on subsequent patches.
	if schemas != nil {
		if schema, ok := schemas.ObjectSchemas[current.Type]; ok {
			start := time.Now()
			// Build a delta of only the keys actually being set (non-nil patch entries).
			// Nil-valued entries mean "delete this property" and need no validation.
			patchDelta := make(map[string]any)
			for k, v := range req.Properties {
				if v != nil {
					patchDelta[k] = v
				}
			}
			validatedDelta, err := validatePatchProperties(patchDelta, schema)
			duration := time.Since(start)

			if err != nil {
				s.incrementValidationError(duration)
				return nil, apperror.ErrBadRequest.WithMessage("property validation failed: " + err.Error())
			}
			s.incrementValidationSuccess(duration)
			// Merge coerced delta values back into newProps.
			for k, v := range validatedDelta {
				newProps[k] = v
			}
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

	// Handle status: prefer properties["status"] (authoritative JSONB), then
	// req.Status (explicit field), then keep current.
	newStatus := current.Status
	if req.Status != nil {
		newStatus = req.Status
	}
	// Sync from merged properties["status"] — this is the authoritative source
	// so that update_entity(properties={"status":"complete"}) always wins.
	if st, ok := newProps["status"].(string); ok && st != "" {
		newStatus = &st
	}

	// Handle key: use req.Key if provided, otherwise preserve current
	newKey := current.Key
	if req.Key != nil {
		newKey = req.Key
	}

	actorType := "user"
	newVersion := &GraphObject{
		Type:       current.Type,
		Key:        newKey,
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

	// Check if key changed
	keyChanged := false
	if req.Key != nil {
		if current.Key == nil || *current.Key != *req.Key {
			keyChanged = true
		}
	}

	// No effective change — return existing version without creating a new one
	if newVersion.ChangeSummary == nil && !statusChanged && !labelsChanged && !keyChanged {
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

	if s.journal != nil && newVersion.Key != nil {
		objType := newVersion.Type
		entityType := entityTypeObject
		var fieldsChanged []string
		if newVersion.ChangeSummary != nil {
			for k := range newVersion.ChangeSummary {
				fieldsChanged = append(fieldsChanged, k)
			}
		}
		s.journal.Log(ctx, journal.LogParams{
			ProjectID:  projectID,
			BranchID:   newVersion.BranchID,
			EventType:  journal.EventTypeUpdated,
			EntityType: &entityType,
			ObjectType: &objType,
			ActorType:  journal.ActorUser,
			ActorID:    actorID,
			Metadata: map[string]any{
				"key":            *newVersion.Key,
				"name":           nameFromProps(newVersion.Properties),
				"object_type":    newVersion.Type,
				"fields_changed": fieldsChanged,
			},
		})
	}

	s.emitObjectUpdated(newVersion.ToResponse())

	return newVersion.ToResponse(), nil
}

// Delete soft-deletes a graph object by creating a tombstone version.
func (s *Service) Delete(ctx context.Context, projectID, id uuid.UUID, actorID *uuid.UUID, branchID *uuid.UUID) error {
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

	if branchID != nil {
		// Branch-scoped delete: try to get branch-local HEAD first.
		// If none exists, fall back to main HEAD and create a branch-local tombstone.
		branchHead, err := s.repo.GetHeadByCanonicalID(ctx, tx.Tx, projectID, current.CanonicalID, branchID)
		if err != nil {
			var appErr *apperror.Error
			if !errors.As(err, &appErr) || appErr.HTTPStatus != 404 {
				return err
			}
			// No branch-local HEAD — fetch main HEAD and create tombstone directly on branch.
			mainHead, merr := s.repo.GetHeadByCanonicalID(ctx, tx.Tx, projectID, current.CanonicalID, nil)
			if merr != nil {
				return merr
			}
			if err := s.repo.SoftDeleteOnBranch(ctx, tx.Tx, mainHead, branchID, actorID); err != nil {
				return err
			}
			current = mainHead
		} else {
			if err := s.repo.SoftDelete(ctx, tx.Tx, branchHead, actorID); err != nil {
				return err
			}
			current = branchHead
		}
	} else {
		// Main branch delete: re-fetch HEAD after lock.
		current, err = s.repo.GetHeadByCanonicalID(ctx, tx.Tx, projectID, current.CanonicalID, nil)
		if err != nil {
			return err
		}
		if err := s.repo.SoftDelete(ctx, tx.Tx, current, actorID); err != nil {
			return err
		}
	}

	if err := tx.Commit(); err != nil {
		return err
	}

	if s.journal != nil && current.Key != nil {
		objType := current.Type
		entityType := entityTypeObject
		s.journal.Log(ctx, journal.LogParams{
			ProjectID:  projectID,
			BranchID:   current.BranchID,
			EventType:  journal.EventTypeDeleted,
			EntityType: &entityType,
			ObjectType: &objType,
			ActorType:  journal.ActorUser,
			ActorID:    actorID,
			Metadata: map[string]any{
				"key":         *current.Key,
				"name":        nameFromProps(current.Properties),
				"object_type": current.Type,
			},
		})
	}

	s.emitObjectDeleted(current.ProjectID.String(), current.CanonicalID.String(), current.Type)

	return nil
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

	if s.journal != nil && restored.Key != nil {
		objType := restored.Type
		entityType := entityTypeObject
		s.journal.Log(ctx, journal.LogParams{
			ProjectID:  projectID,
			BranchID:   restored.BranchID,
			EventType:  journal.EventTypeRestored,
			EntityType: &entityType,
			ObjectType: &objType,
			ActorType:  journal.ActorUser,
			ActorID:    actorID,
			Metadata: map[string]any{
				"key":         *restored.Key,
				"name":        nameFromProps(restored.Properties),
				"object_type": restored.Type,
			},
		})
	}

	s.emitObjectUpdated(restored.ToResponse())

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
// Standard field names: items, next_cursor, total (consistent with SearchGraphObjectsResponse)
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
	// Cap the limit for the same reason as List — see maxListLimit.
	if params.Limit <= 0 {
		params.Limit = 50
	}
	if params.Limit > maxListLimit {
		params.Limit = maxListLimit
	}

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

	// Fetch schemas BEFORE transaction to avoid deadlock
	var schemas *ExtractionSchemas
	var schemaErr error
	if s.schemaProvider != nil {
		schemas, schemaErr = s.schemaProvider.GetProjectSchemas(ctx, projectID.String())
		if schemaErr != nil {
			s.log.Warn("failed to load schemas for relationship validation, skipping",
				slog.String("project_id", projectID.String()),
				slog.String("error", schemaErr.Error()))
		}
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

	// Attempt lock-free insert using ON CONFLICT DO NOTHING (protected by partial unique index).
	// No advisory lock needed for the common "create new" path.

	// Validate relationship type, endpoint types, and properties against schema.
	if schemas != nil {
		if err := validateRelationship(req.Type, srcObj.Type, dstObj.Type, req.Properties, schemas); err != nil {
			return nil, apperror.ErrBadRequest.WithMessage(err.Error())
		}
	}

	rel := &GraphRelationship{
		ProjectID:  projectID,
		BranchID:   effectiveBranchID,
		Type:       req.Type,
		SrcID:      srcObj.CanonicalID,
		DstID:      dstObj.CanonicalID,
		Properties: req.Properties,
		Weight:     req.Weight,
	}
	rel.ChangeSummary = computeChangeSummary(nil, req.Properties)

	created, err := s.repo.CreateRelationship(ctx, tx.Tx, rel)
	if err != nil {
		return nil, err
	}

	if created {
		// New relationship was inserted — auto-create inverse if configured.
		var inverseResponse *GraphRelationshipResponse
		var inverseRelID string
		if s.inverseTypeProvider != nil {
			inverseResponse, inverseRelID = s.maybeCreateInverse(ctx, tx.Tx, projectID, effectiveBranchID, req.Type, srcObj, dstObj, req.Properties, req.Weight)
		}

		if err := tx.Commit(); err != nil {
			return nil, apperror.ErrDatabase.WithInternal(err)
		}

		// Enqueue async embeddings after commit — never block the response.
		s.enqueueRelationshipEmbedding(ctx, rel.ID.String())
		if inverseRelID != "" {
			s.enqueueRelationshipEmbedding(ctx, inverseRelID)
		}

		if s.journal != nil {
			var srcKey, dstKey string
			if srcObj.Key != nil {
				srcKey = *srcObj.Key
			}
			if dstObj.Key != nil {
				dstKey = *dstObj.Key
			}
			entityType := entityTypeRelationship
			s.journal.Log(ctx, journal.LogParams{
				ProjectID:  projectID,
				BranchID:   effectiveBranchID,
				EventType:  journal.EventTypeRelated,
				EntityType: &entityType,
				ActorType:  journal.ActorUser,
				Metadata: map[string]any{
					"src_key":  srcKey,
					"rel_type": req.Type,
					"dst_key":  dstKey,
				},
			})
		}

		resp := rel.ToResponse()
		resp.InverseRelationship = inverseResponse
		return resp, nil
	}

	// Conflict: relationship HEAD already exists (another concurrent goroutine inserted it).
	// Roll back the current tx — we don't need it for a read-only check.
	tx.Rollback()

	// Read HEAD outside any transaction (no lock needed — the row is stable).
	existing, err := s.repo.GetRelationshipHead(ctx, s.repo.DB(), projectID, effectiveBranchID, req.Type, srcObj.CanonicalID, dstObj.CanonicalID)
	if err != nil {
		return nil, err
	}
	if existing == nil {
		// Extremely rare race: HEAD was deleted between our failed insert and the read.
		// Treat as "not found" — the caller can retry.
		return nil, apperror.ErrNotFound.WithMessage("relationship_head_not_found_after_conflict")
	}

	// Fast path: no change needed (common during bulk seeding).
	if existing.DeletedAt == nil {
		diff := computeChangeSummary(existing.Properties, req.Properties)
		if diff == nil {
			return existing.ToResponse(), nil
		}
	}

	// Slow path: we need to create a new version (deleted restore or property change).
	// Open a fresh transaction, acquire advisory lock to prevent concurrent version racing.
	tx2, err := s.repo.BeginTx(ctx)
	if err != nil {
		return nil, apperror.ErrDatabase.WithInternal(err)
	}
	defer tx2.Rollback()

	if err := s.repo.AcquireRelationshipLock(ctx, tx2.Tx, projectID, req.Type, srcObj.CanonicalID, dstObj.CanonicalID); err != nil {
		return nil, err
	}

	// Re-fetch inside the locked tx to get the definitive current HEAD.
	existing, err = s.repo.GetRelationshipHead(ctx, tx2.Tx, projectID, effectiveBranchID, req.Type, srcObj.CanonicalID, dstObj.CanonicalID)
	if err != nil {
		return nil, err
	}
	if existing == nil {
		return nil, apperror.ErrNotFound.WithMessage("relationship_head_not_found_after_lock")
	}

	if existing.DeletedAt != nil {
		// Was deleted, restore with new properties.
		newVersion := &GraphRelationship{
			Properties: req.Properties,
			Weight:     req.Weight,
			DeletedAt:  nil,
		}
		newVersion.ChangeSummary = computeChangeSummary(existing.Properties, req.Properties)

		if err := s.repo.CreateRelationshipVersion(ctx, tx2.Tx, existing, newVersion); err != nil {
			return nil, err
		}

		if err := tx2.Commit(); err != nil {
			return nil, apperror.ErrDatabase.WithInternal(err)
		}

		s.enqueueRelationshipEmbedding(ctx, newVersion.ID.String())

		newHead, _ := s.repo.GetRelationshipHead(ctx, s.repo.DB(), projectID, effectiveBranchID, req.Type, srcObj.CanonicalID, dstObj.CanonicalID)
		return newHead.ToResponse(), nil
	}

	// Check if properties changed (re-read under lock).
	diff := computeChangeSummary(existing.Properties, req.Properties)
	if diff == nil {
		// No change - return existing (no commit needed).
		return existing.ToResponse(), nil
	}

	// Properties differ - create new version.
	newVersion := &GraphRelationship{
		Properties:    req.Properties,
		Weight:        req.Weight,
		ChangeSummary: diff,
	}

	if err := s.repo.CreateRelationshipVersion(ctx, tx2.Tx, existing, newVersion); err != nil {
		return nil, err
	}

	if err := tx2.Commit(); err != nil {
		return nil, apperror.ErrDatabase.WithInternal(err)
	}

	s.enqueueRelationshipEmbedding(ctx, newVersion.ID.String())

	newHead, _ := s.repo.GetRelationshipHead(ctx, s.repo.DB(), projectID, effectiveBranchID, req.Type, srcObj.CanonicalID, dstObj.CanonicalID)
	return newHead.ToResponse(), nil
}

// UpsertRelationship provides idempotent create-or-skip semantics for relationships.
// Dedup key: (project_id, branch_id, type, src_id, dst_id).
// Returns (response, created=true) when a new relationship is inserted.
// Returns (response, created=false) when an existing relationship is returned or updated.
func (s *Service) UpsertRelationship(ctx context.Context, projectID uuid.UUID, req *CreateGraphRelationshipRequest) (*GraphRelationshipResponse, bool, error) {
	// Delegate to CreateRelationship which already implements full upsert logic.
	// We detect "created" by checking whether the returned relationship is version 1.
	resp, err := s.CreateRelationship(ctx, projectID, req)
	if err != nil {
		return nil, false, err
	}
	return resp, resp.Version == 1, nil
}

// maybeCreateInverse checks if the template pack declares an inverseType for the given
// relationship type, and if so, creates the inverse relationship (swapped src/dst) within
// the same transaction. Returns the inverse response (or nil) and the inverse relationship ID
// (empty string if none created). Errors are logged but do not fail the primary relationship creation.
func (s *Service) maybeCreateInverse(
	ctx context.Context,
	tx bun.Tx,
	projectID uuid.UUID,
	branchID *uuid.UUID,
	relType string,
	srcObj, dstObj *GraphObject,
	properties map[string]any,
	weight *float32,
) (*GraphRelationshipResponse, string) {
	inverseType, ok := s.inverseTypeProvider.GetInverseType(ctx, projectID.String(), relType)
	if !ok || inverseType == "" {
		return nil, ""
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
			return nil, ""
		}
	}

	// Acquire advisory lock for the inverse relationship identity (swapped endpoints, using canonical IDs)
	if err := s.repo.AcquireRelationshipLock(ctx, tx, projectID, inverseType, dstObj.CanonicalID, srcObj.CanonicalID); err != nil {
		s.log.Warn("failed to acquire lock for inverse relationship, skipping",
			slog.String("inverse_type", inverseType),
			slog.String("error", err.Error()))
		return nil, ""
	}

	// Check if inverse already exists (using canonical IDs)
	existingInverse, err := s.repo.GetRelationshipHead(ctx, tx, projectID, branchID, inverseType, dstObj.CanonicalID, srcObj.CanonicalID)
	if err != nil {
		s.log.Warn("failed to check existing inverse relationship, skipping",
			slog.String("inverse_type", inverseType),
			slog.String("error", err.Error()))
		return nil, ""
	}

	if existingInverse != nil && existingInverse.DeletedAt == nil {
		// Inverse already exists and is not deleted — return it as-is (no new embedding needed)
		s.log.Debug("inverse relationship already exists",
			slog.String("inverse_id", existingInverse.ID.String()))
		return existingInverse.ToResponse(), ""
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
			return nil, ""
		}

		// Embedding will be enqueued async by caller after commit.
		return newVersion.ToResponse(), newVersion.ID.String()
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

	if _, err := s.repo.CreateRelationship(ctx, tx, inverseRel); err != nil {
		s.log.Warn("failed to create inverse relationship, skipping",
			slog.String("inverse_type", inverseType),
			slog.String("error", err.Error()))
		return nil, ""
	}

	s.log.Info("auto-created inverse relationship",
		slog.String("primary_type", relType),
		slog.String("inverse_type", inverseType),
		slog.String("inverse_id", inverseRel.ID.String()))

	// Embedding will be enqueued async by caller after commit.
	return inverseRel.ToResponse(), inverseRel.ID.String()
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

	// Fetch schemas BEFORE transaction to avoid deadlock
	var schemas *ExtractionSchemas
	var schemaErr error
	if s.schemaProvider != nil {
		schemas, schemaErr = s.schemaProvider.GetProjectSchemas(ctx, projectID.String())
		if schemaErr != nil {
			s.log.Warn("failed to load schemas for relationship patch validation, skipping",
				slog.String("project_id", projectID.String()),
				slog.String("error", schemaErr.Error()))
		}
	}

	tx, txErr := s.repo.BeginTx(ctx)
	if txErr != nil {
		return nil, apperror.ErrDatabase.WithInternal(txErr)
	}
	defer tx.Rollback()

	// Acquire lock
	if err := s.repo.AcquireRelationshipLock(ctx, tx.Tx, current.ProjectID, current.Type, current.SrcID, current.DstID); err != nil {
		return nil, err
	}

	// Re-fetch HEAD after lock
	head, headErr := s.repo.GetRelationshipHeadByCanonicalID(ctx, projectID, current.CanonicalID)
	if headErr != nil {
		return nil, headErr
	}

	// Ensure we're patching the HEAD version
	if head.ID != current.ID {
		return nil, apperror.ErrBadRequest.WithMessage("cannot_patch_non_head_version")
	}

	// Validate patch delta properties against schema (soft-fail on schema load error).
	// Same delta-only approach as Patch: only validate properties being added/changed,
	// not those already stored on the relationship from an older schema version.
	if schemas != nil {
		if relSchema, ok := schemas.RelationshipSchemas[current.Type]; ok && (len(relSchema.Properties) > 0 || len(relSchema.Required) > 0) {
			objSchema := agents.ObjectSchema{
				Properties: relSchema.Properties,
				Required:   relSchema.Required,
			}
			patchDelta := make(map[string]any)
			for k, v := range req.Properties {
				if v != nil {
					patchDelta[k] = v
				}
			}
			if validatedDelta, err := validatePatchProperties(patchDelta, objSchema); err != nil {
				return nil, apperror.ErrBadRequest.WithMessage("property validation failed: " + err.Error())
			} else {
				for k, v := range validatedDelta {
					newProps[k] = v
				}
			}
		}
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
func (s *Service) DeleteRelationship(ctx context.Context, projectID, id uuid.UUID, branchID *uuid.UUID) (*GraphRelationshipResponse, error) {
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

	// Get HEAD version — scoped to the requested branch when provided.
	head, err := s.repo.GetRelationshipHead(ctx, tx.Tx, projectID, branchID, current.Type, current.SrcID, current.DstID)
	if err != nil {
		return nil, err
	}
	if head == nil {
		return nil, apperror.ErrNotFound
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
	tombstone, _ := s.repo.GetRelationshipHead(ctx, s.repo.DB(), projectID, branchID, head.Type, head.SrcID, head.DstID)
	if tombstone == nil {
		return head.ToResponse(), nil
	}
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

	// Boost parameters — default RecencyHalfLife to 168h (7 days) when RecencyBoost > 0
	var recencyBoost, accessBoost, recencyHalfLife float32
	if req.RecencyBoost != nil {
		recencyBoost = *req.RecencyBoost
	}
	if req.AccessBoost != nil {
		accessBoost = *req.AccessBoost
	}
	if recencyBoost > 0 {
		if req.RecencyHalfLife != nil {
			recencyHalfLife = *req.RecencyHalfLife
		} else {
			recencyHalfLife = 168.0
		}
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

		// Apply recency boost if requested
		if recencyBoost > 0 && obj != nil {
			hoursOld := float32(time.Since(obj.CreatedAt).Hours())
			recencyScore := float32(1.0 / (1.0 + math.Exp(float64((hoursOld-recencyHalfLife)/(recencyHalfLife/4.0)))))
			fusedScore += recencyBoost * recencyScore
		}

		// Apply access boost if requested
		if accessBoost > 0 && obj != nil && obj.LastAccessedAt != nil {
			daysSinceAccess := float32(time.Since(*obj.LastAccessedAt).Hours() / 24)
			accessScore := float32(math.Max(0, float64(1.0-daysSinceAccess/365.0)))
			fusedScore += accessBoost * accessScore
		}

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

	for i, item := range req.Items {
		resp, err := s.Create(workerCtx, projectID, &item, actorID)
		if err != nil {
			errMsg := err.Error()
			results[i] = BulkCreateObjectResult{Index: i, Success: false, Error: &errMsg}
		} else {
			results[i] = BulkCreateObjectResult{Index: i, Success: true, Object: resp}
		}
	}

	successCount, failedCount := 0, 0
	byType := make(map[string]int)
	for _, r := range results {
		if r.Success {
			successCount++
			if r.Object != nil {
				byType[r.Object.Type]++
			}
		} else {
			failedCount++
		}
	}

	if s.journal != nil && successCount > 0 {
		byTypeAny := make(map[string]any, len(byType))
		for k, v := range byType {
			byTypeAny[k] = v
		}
		s.journal.Log(ctx, journal.LogParams{
			ProjectID: projectID,
			EventType: journal.EventTypeBatch,
			ActorType: journal.ActorUser,
			ActorID:   actorID,
			Metadata: map[string]any{
				"created": successCount,
				"by_type": byTypeAny,
			},
		})
	}

	return &BulkCreateObjectsResponse{
		Success: successCount,
		Failed:  failedCount,
		Results: results,
	}, nil
}

// BulkUpdateObjects updates multiple objects in a single batch.
// Each object is updated independently and concurrently — failures do not roll back other successes.
func (s *Service) BulkUpdateObjects(ctx context.Context, projectID uuid.UUID, req *BulkUpdateObjectsRequest, actorID *uuid.UUID) (*BulkUpdateObjectsResponse, error) {
	results := make([]BulkUpdateObjectResult, len(req.Items))

	workerCtx := context.WithoutCancel(ctx)

	// Pre-warm schema cache for this project to avoid lock contention
	if s.schemaProvider != nil {
		_, _ = s.schemaProvider.GetProjectSchemas(workerCtx, projectID.String())
	}

	for i, item := range req.Items {
		id, parseErr := uuid.Parse(item.ID)
		if parseErr != nil {
			errMsg := "invalid id: " + parseErr.Error()
			results[i] = BulkUpdateObjectResult{Index: i, ID: item.ID, Success: false, Error: &errMsg}
			continue
		}

		patchReq := &PatchGraphObjectRequest{
			Key:           item.Key,
			Properties:    item.Properties,
			Labels:        item.Labels,
			ReplaceLabels: item.ReplaceLabels,
			Status:        item.Status,
			BranchID:      item.BranchID,
		}

		resp, err := s.Patch(workerCtx, projectID, id, patchReq, actorID)
		if err != nil {
			errMsg := err.Error()
			results[i] = BulkUpdateObjectResult{Index: i, ID: item.ID, Success: false, Error: &errMsg}
		} else {
			results[i] = BulkUpdateObjectResult{Index: i, ID: item.ID, Success: true, Object: resp}
		}
	}

	successCount, failedCount := 0, 0
	for _, r := range results {
		if r.Success {
			successCount++
		} else {
			failedCount++
		}
	}

	if s.journal != nil && successCount > 0 {
		s.journal.Log(ctx, journal.LogParams{
			ProjectID: projectID,
			EventType: journal.EventTypeBatch,
			ActorType: journal.ActorUser,
			ActorID:   actorID,
			Metadata: map[string]any{
				"updated": successCount,
			},
		})
	}

	return &BulkUpdateObjectsResponse{
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

	for i, item := range req.Items {
		resp, err := s.CreateRelationship(workerCtx, projectID, &item)
		if err != nil {
			errMsg := err.Error()
			s.log.Debug("bulk relationship creation failed",
				slog.String("type", item.Type),
				slog.String("src_id", item.SrcID.String()),
				slog.String("dst_id", item.DstID.String()),
				slog.String("error", errMsg))
			results[i] = BulkCreateRelationshipResult{Index: i, Success: false, Error: &errMsg}
		} else {
			results[i] = BulkCreateRelationshipResult{Index: i, Success: true, Relationship: resp}
		}
	}

	successCount, failedCount := 0, 0
	for _, r := range results {
		if r.Success {
			successCount++
		} else {
			failedCount++
		}
	}

	if s.journal != nil && successCount > 0 {
		s.journal.Log(ctx, journal.LogParams{
			ProjectID: projectID,
			EventType: journal.EventTypeBatch,
			ActorType: journal.ActorUser,
			Metadata: map[string]any{
				"created": successCount,
			},
		})
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
	if pageSize > 1000 {
		pageSize = 1000
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
			Properties:  obj.Properties,
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
func (s *Service) MergeBranch(ctx context.Context, projectID uuid.UUID, targetBranchID *uuid.UUID, req *BranchMergeRequest) (*BranchMergeResponse, error) {
	// Validate target branch exists (skip for main — it has no branch row)
	if targetBranchID != nil {
		_, err := s.repo.GetBranchByID(ctx, projectID, *targetBranchID)
		if err != nil {
			return nil, apperror.ErrNotFound.WithMessage("target branch not found")
		}
	}

	// Validate source branch exists
	_, err := s.repo.GetBranchByID(ctx, projectID, req.SourceBranchID)
	if err != nil {
		return nil, apperror.ErrNotFound.WithMessage("source branch not found")
	}

	// Get HEAD versions for both branches (nil targetBranchID = main graph)
	targetObjects, err := s.repo.GetBranchObjectHeads(ctx, projectID, targetBranchID)
	if err != nil {
		return nil, err
	}

	sourceObjects, err := s.repo.GetBranchObjectHeads(ctx, projectID, &req.SourceBranchID)
	if err != nil {
		return nil, err
	}

	targetRels, err := s.repo.GetBranchRelationshipHeads(ctx, projectID, targetBranchID)
	if err != nil {
		return nil, err
	}

	sourceRels, err := s.repo.GetBranchRelationshipHeads(ctx, projectID, &req.SourceBranchID)
	if err != nil {
		return nil, err
	}

	// Enumerate objects: classify each canonical_id
	objectSummaries := make([]*BranchMergeObjectSummary, 0)
	unchangedCount, addedCount, ffCount, conflictCount, deletedCount := 0, 0, 0, 0, 0

	// Hard limit for enumeration
	hardLimit := 2000
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
		} else if sourceHead != nil && sourceHead.DeletedAt != nil && targetHead != nil {
			// Deleted on source branch, exists on target — classify as "deleted"
			summary.Status = "deleted"
			// For now count as fast_forward (it's a change that needs to be applied)
			ffCount++
			deletedCount++
		} else if sourceHead != nil && sourceHead.DeletedAt != nil && targetHead == nil {
			// Deleted on source, doesn't exist on target — nothing to do
			summary.Status = "unchanged"
			unchangedCount++
		} else if sourceHead != nil && targetHead == nil {
			// Exists only on source - added
			summary.Status = "added"
			addedCount++
		} else if sourceHead != nil && targetHead != nil {
			// Exists on both - compare content hash (covers properties, status, key, labels)
			if bytesEqual(sourceHead.ContentHash, targetHead.ContentHash) {
				summary.Status = "unchanged"
				unchangedCount++
			} else {
				// Content differs — find which property keys have conflicting values
				conflicts := findConflictingPaths(sourceHead.Properties, targetHead.Properties)

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
				conflicts := findConflictingPaths(sourceHead.Properties, targetHead.Properties)

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

	// Sort summaries: conflict -> deleted -> fast_forward -> added -> unchanged
	// Sort BEFORE truncating so high-priority items always appear in the payload.
	sortMergeObjectSummaries(objectSummaries)
	sortMergeRelationshipSummaries(relSummaries)

	// Truncate payload to hardLimit (after sort, so non-unchanged items are first).
	responseObjectSummaries := objectSummaries
	responseRelSummaries := relSummaries
	if len(objectSummaries) > hardLimit {
		responseObjectSummaries = objectSummaries[:hardLimit]
		truncated = true
	}
	if len(relSummaries) > hardLimit {
		responseRelSummaries = relSummaries[:hardLimit]
		truncated = true
	}

	response := &BranchMergeResponse{
		TargetBranchID:                targetBranchID, // nil = main graph
		SourceBranchID:                req.SourceBranchID,
		DryRun:                        !req.Execute,
		TotalObjects:                  len(allCanonicalIDs),
		UnchangedCount:                unchangedCount,
		AddedCount:                    addedCount,
		FastForwardCount:              ffCount,
		DeletedCount:                  &deletedCount,
		ConflictCount:                 conflictCount,
		Objects:                       responseObjectSummaries,
		Truncated:                     truncated,
		HardLimit:                     &hardLimit,
		RelationshipsTotal:            intPtr(len(allRelCanonicalIDs)),
		RelationshipsUnchangedCount:   &relUnchanged,
		RelationshipsAddedCount:       &relAdded,
		RelationshipsFastForwardCount: &relFF,
		RelationshipsConflictCount:    &relConflict,
		Relationships:                 responseRelSummaries,
	}

	// If execute is requested and no conflicts, apply merge transactionally.
	if req.Execute && conflictCount == 0 && relConflict == 0 {
		appliedCount, err := s.applyMerge(ctx, projectID, targetBranchID, objectSummaries, relSummaries, sourceObjects, targetObjects, sourceRels, targetRels) //nolint:staticcheck
		if err != nil {
			return nil, fmt.Errorf("apply merge: %w", err)
		}
		response.Applied = true
		response.AppliedObjects = &appliedCount

		if s.journal != nil {
			relsMerged := 0
			if response.RelationshipsTotal != nil {
				relsMerged = *response.RelationshipsTotal - relUnchanged
			}
			s.journal.Log(ctx, journal.LogParams{
				ProjectID: projectID,
				BranchID:  targetBranchID,
				EventType: journal.EventTypeMerge,
				ActorType: journal.ActorUser,
				Metadata: map[string]any{
					"objects_merged":       appliedCount,
					"relationships_merged": relsMerged,
				},
			})
		}

		// Stamp the source branch as merged.
		if s.branchStore != nil {
			if err := s.branchStore.SetMergedAt(ctx, req.SourceBranchID.String(), time.Now().UTC()); err != nil {
				s.log.Warn("failed to stamp branch merged_at",
					slog.String("branch_id", req.SourceBranchID.String()),
					slog.String("error", err.Error()))
			}
		}
	}

	return response, nil
}

// applyMerge executes the merge inside a single database transaction.
// It clones "added" objects/relationships onto the target branch and creates
// new versions for "fast_forward" objects/relationships.
// Returns the total number of objects+relationships written.
func (s *Service) applyMerge(
	ctx context.Context,
	projectID uuid.UUID,
	targetBranchID *uuid.UUID,
	objectSummaries []*BranchMergeObjectSummary,
	relSummaries []*BranchMergeRelationshipSummary,
	sourceObjects map[uuid.UUID]*BranchObjectHead,
	targetObjects map[uuid.UUID]*BranchObjectHead,
	sourceRels map[uuid.UUID]*BranchRelationshipHead,
	targetRels map[uuid.UUID]*BranchRelationshipHead,
) (int, error) {
	tx, err := s.repo.BeginTx(ctx)
	if err != nil {
		return 0, fmt.Errorf("begin transaction: %w", err)
	}
	defer tx.Rollback() //nolint:errcheck

	// canonicalIDMap maps source canonical IDs → target canonical IDs for
	// objects that are "added" (new on source, don't exist on target yet).
	// Used to remap relationship endpoints when both endpoints are new.
	canonicalIDMap := make(map[uuid.UUID]uuid.UUID)

	appliedCount := 0

	// ── Objects ──────────────────────────────────────────────────────────────

	for _, summary := range objectSummaries {
		cid := summary.CanonicalID

		switch summary.Status {
		case "added":
			src := sourceObjects[cid]
			if src == nil {
				continue
			}
			labels := src.Labels
			if labels == nil {
				labels = []string{}
			}
			props := src.Properties
			if props == nil {
				props = map[string]any{}
			}
			newCanonicalID := uuid.New()
			clone := &GraphObject{
				ID:          uuid.New(),
				CanonicalID: newCanonicalID,
				ProjectID:   projectID,
				BranchID:    targetBranchID,
				Version:     1,
				Type:        src.Type,
				Key:         src.Key,
				Status:      src.Status,
				Labels:      labels,
				Properties:  props,
			}
			clone.ContentHash = computeContentHash(clone.Properties, clone.Status, clone.Key, clone.Labels)
			now := time.Now()
			clone.CreatedAt = now
			clone.UpdatedAt = now

			res, err := tx.NewInsert().Model(clone).On("CONFLICT DO NOTHING").Returning("id").Exec(ctx)
			if err != nil {
				return 0, fmt.Errorf("clone object %s: %w", cid, err)
			}
			// If the object already exists on the target (e.g. previously merged
			// under a different canonical ID), the insert is silently skipped.
			if n, _ := res.RowsAffected(); n > 0 {
				canonicalIDMap[cid] = newCanonicalID
				appliedCount++
				// Enqueue embedding best-effort after commit (use new canonical ID)
				defer s.enqueueEmbedding(ctx, newCanonicalID.String())
			}

		case "fast_forward":
			src := sourceObjects[cid]
			if src == nil {
				continue
			}
			// Fetch the current HEAD on the target branch to use as prevHead
			prevHead, err := s.repo.GetHeadByCanonicalID(ctx, tx, projectID, cid, targetBranchID)
			if err != nil {
				return 0, fmt.Errorf("get target head for fast-forward %s: %w", cid, err)
			}
			labels := src.Labels
			if labels == nil {
				labels = []string{}
			}
			props := src.Properties
			if props == nil {
				props = map[string]any{}
			}
			newVersion := &GraphObject{
				Type:       prevHead.Type,
				Key:        src.Key,
				Status:     src.Status,
				Labels:     labels,
				Properties: props,
				ProjectID:  projectID,
				BranchID:   targetBranchID,
			}
			if err := s.repo.CreateVersion(ctx, tx.Tx, prevHead, newVersion); err != nil {
				return 0, fmt.Errorf("fast-forward object %s: %w", cid, err)
			}
			appliedCount++
			defer s.enqueueEmbedding(ctx, cid.String())

		case "deleted":
			// Soft delete on target branch
			targetHead, err := s.repo.GetHeadByCanonicalID(ctx, tx, projectID, summary.CanonicalID, targetBranchID)
			if err != nil {
				return 0, fmt.Errorf("get target head for delete %s: %w", summary.CanonicalID, err)
			}
			if err := s.repo.SoftDelete(ctx, tx.Tx, targetHead, nil); err != nil {
				return 0, fmt.Errorf("delete object %s: %w", summary.CanonicalID, err)
			}
			appliedCount++
		}
	}

	// ── Relationships ─────────────────────────────────────────────────────────

	for _, summary := range relSummaries {
		cid := summary.CanonicalID

		switch summary.Status {
		case "added":
			src := sourceRels[cid]
			if src == nil {
				continue
			}
			props := src.Properties
			if props == nil {
				props = map[string]any{}
			}
			// Remap src/dst canonical IDs if they were added in this merge
			srcID := src.SrcID
			if mapped, ok := canonicalIDMap[srcID]; ok {
				srcID = mapped
			}
			dstID := src.DstID
			if mapped, ok := canonicalIDMap[dstID]; ok {
				dstID = mapped
			}
			rel := &GraphRelationship{
				ID:          uuid.New(),
				CanonicalID: uuid.New(),
				ProjectID:   projectID,
				BranchID:    targetBranchID,
				Version:     1,
				Type:        src.Type,
				SrcID:       srcID,
				DstID:       dstID,
				Properties:  props,
			}
			rel.ContentHash = computeContentHash(rel.Properties, nil, nil, nil)
			rel.CreatedAt = time.Now()

			if _, err := tx.NewInsert().Model(rel).On("CONFLICT DO NOTHING").Returning("").Exec(ctx); err != nil {
				return 0, fmt.Errorf("clone relationship %s: %w", cid, err)
			}
			appliedCount++

		case "fast_forward":
			src := sourceRels[cid]
			if src == nil {
				continue
			}
			prevHead, err := s.repo.GetRelationshipHeadByCanonicalID(ctx, projectID, cid)
			if err != nil {
				return 0, fmt.Errorf("get target rel head for fast-forward %s: %w", cid, err)
			}
			props := src.Properties
			if props == nil {
				props = map[string]any{}
			}
			newVersion := &GraphRelationship{
				Properties: props,
				BranchID:   targetBranchID,
				ProjectID:  projectID,
			}
			if err := s.repo.CreateRelationshipVersion(ctx, tx.Tx, prevHead, newVersion); err != nil {
				return 0, fmt.Errorf("fast-forward relationship %s: %w", cid, err)
			}
			appliedCount++
		}
	}

	if err := tx.Commit(); err != nil {
		return 0, fmt.Errorf("commit merge: %w", err)
	}

	return appliedCount, nil
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

// findConflictingPaths returns the property keys that differ between source and
// target. A key is a conflict only when both branches have it AND the values
// differ (JSON-serialised comparison). Keys present on only one side are not
// conflicts — they are additive changes that merge cleanly.
func findConflictingPaths(sourceProps, targetProps map[string]any) []string {
	conflicts := []string{}
	for k, sv := range sourceProps {
		tv, exists := targetProps[k]
		if !exists {
			continue // only on source — not a conflict
		}
		// Compare by JSON representation for deep equality
		sb, _ := json.Marshal(sv)
		tb, _ := json.Marshal(tv)
		if string(sb) != string(tb) {
			conflicts = append(conflicts, "/"+k)
		}
	}
	sort.Strings(conflicts)
	return conflicts
}

func sortMergeObjectSummaries(summaries []*BranchMergeObjectSummary) {
	statusOrder := map[string]int{
		"conflict":     0,
		"deleted":      1,
		"fast_forward": 2,
		"added":        3,
		"unchanged":    4,
	}
	sort.Slice(summaries, func(i, j int) bool {
		return statusOrder[summaries[i].Status] < statusOrder[summaries[j].Status]
	})
}

func sortMergeRelationshipSummaries(summaries []*BranchMergeRelationshipSummary) {
	statusOrder := map[string]int{
		"conflict":     0,
		"deleted":      1,
		"fast_forward": 2,
		"added":        3,
		"unchanged":    4,
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

// InvalidateSchemaCache evicts the cached schemas for a project so the next
// graph operation fetches fresh schema data from the database. Call this after
// any schema install, update, or removal to avoid stale validation.
func (s *Service) InvalidateSchemaCache(projectID string) {
	s.schemaProvider.InvalidateProjectCache(projectID)
}

// =============================================================================
// Atomic Subgraph Creation
// =============================================================================

// CreateSubgraph atomically creates a set of objects and relationships in a single transaction.
// Objects are referenced by client-side placeholder refs (_ref), which are resolved to server-assigned
// IDs before relationship creation. If any step fails, the entire operation is rolled back.
func (s *Service) CreateSubgraph(ctx context.Context, projectID uuid.UUID, req *CreateSubgraphRequest, actorID *uuid.UUID) (*CreateSubgraphResponse, error) {
	// Validate: check for duplicate refs (only for objects that have a _ref)
	refSet := make(map[string]bool, len(req.Objects))
	for i, obj := range req.Objects {
		if obj.Ref == "" {
			continue // _ref is optional; object will be created but not referenceable by relationships
		}
		if refSet[obj.Ref] {
			return nil, apperror.ErrBadRequest.WithMessage(fmt.Sprintf("objects[%d]: duplicate _ref %q", i, obj.Ref))
		}
		refSet[obj.Ref] = true
	}

	// Validate: check that all relationship endpoints are resolvable
	for i, rel := range req.Relationships {
		// src: must have either src_ref (pointing to an object in this call) or src_id (existing UUID)
		if rel.SrcRef != "" {
			if !refSet[rel.SrcRef] {
				return nil, apperror.ErrBadRequest.WithMessage(fmt.Sprintf("relationships[%d]: src_ref %q not found in objects", i, rel.SrcRef))
			}
		} else if rel.SrcID == nil {
			return nil, apperror.ErrBadRequest.WithMessage(fmt.Sprintf("relationships[%d]: must specify either src_ref or src_id", i))
		}
		// dst: must have either dst_ref (pointing to an object in this call) or dst_id (existing UUID)
		if rel.DstRef != "" {
			if !refSet[rel.DstRef] {
				return nil, apperror.ErrBadRequest.WithMessage(fmt.Sprintf("relationships[%d]: dst_ref %q not found in objects", i, rel.DstRef))
			}
		} else if rel.DstID == nil {
			return nil, apperror.ErrBadRequest.WithMessage(fmt.Sprintf("relationships[%d]: must specify either dst_ref or dst_id", i))
		}
		// self-loop check (only possible when both endpoints are refs)
		if rel.SrcRef != "" && rel.DstRef != "" && rel.SrcRef == rel.DstRef {
			return nil, apperror.ErrBadRequest.WithMessage(fmt.Sprintf("relationships[%d]: self-loop not allowed (src_ref == dst_ref == %q)", i, rel.SrcRef))
		}
		if rel.SrcID != nil && rel.DstID != nil && *rel.SrcID == *rel.DstID {
			return nil, apperror.ErrBadRequest.WithMessage(fmt.Sprintf("relationships[%d]: self-loop not allowed (src_id == dst_id == %q)", i, rel.SrcID))
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
	relEmbedIDs := make([]string, 0, len(req.Relationships)*2) // IDs to enqueue for embedding after commit
	for i, relReq := range req.Relationships {
		// Validate type is non-empty
		if relReq.Type == "" {
			return nil, apperror.ErrBadRequest.WithMessage(fmt.Sprintf("relationships[%d]: type is required and must not be empty", i))
		}

		// Resolve src: prefer src_ref (new object in this call), fall back to src_id (existing object)
		var srcObj *GraphObject
		if relReq.SrcRef != "" {
			srcObj = objByRef[relReq.SrcRef]
		} else {
			existing, err := s.repo.GetByID(ctx, projectID, *relReq.SrcID)
			if err != nil {
				return nil, apperror.ErrBadRequest.WithMessage(fmt.Sprintf("relationships[%d]: src_id %q not found: %s", i, relReq.SrcID, err.Error()))
			}
			srcObj = existing
		}

		// Resolve dst: prefer dst_ref (new object in this call), fall back to dst_id (existing object)
		var dstObj *GraphObject
		if relReq.DstRef != "" {
			dstObj = objByRef[relReq.DstRef]
		} else {
			existing, err := s.repo.GetByID(ctx, projectID, *relReq.DstID)
			if err != nil {
				return nil, apperror.ErrBadRequest.WithMessage(fmt.Sprintf("relationships[%d]: dst_id %q not found: %s", i, relReq.DstID, err.Error()))
			}
			dstObj = existing
		}

		// Validate branch consistency: src and dst must be on the same branch
		if !branchIDsEqual(srcObj.BranchID, dstObj.BranchID) {
			srcLabel := relReq.SrcRef
			if srcLabel == "" {
				srcLabel = relReq.SrcID.String()
			}
			dstLabel := relReq.DstRef
			if dstLabel == "" {
				dstLabel = relReq.DstID.String()
			}
			return nil, apperror.ErrBadRequest.WithMessage(fmt.Sprintf("relationships[%d]: branch mismatch between src %q and dst %q", i, srcLabel, dstLabel))
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

		if _, err := s.repo.CreateRelationship(ctx, tx.Tx, rel); err != nil {
			return nil, apperror.ErrDatabase.WithMessage(fmt.Sprintf("relationships[%d] (%s->%s): %s", i, relReq.SrcRef, relReq.DstRef, err.Error()))
		}

		// Auto-create inverse relationship if template pack declares inverseType
		var inverseResponse *GraphRelationshipResponse
		var inverseRelID string
		if s.inverseTypeProvider != nil {
			inverseResponse, inverseRelID = s.maybeCreateInverse(ctx, tx.Tx, projectID, srcObj.BranchID, relReq.Type, srcObj, dstObj, relReq.Properties, relReq.Weight)
		}

		resp := rel.ToResponse()
		resp.InverseRelationship = inverseResponse
		relResponses = append(relResponses, resp)

		// Collect IDs for async embedding enqueue after commit.
		relEmbedIDs = append(relEmbedIDs, rel.ID.String())
		if inverseRelID != "" {
			relEmbedIDs = append(relEmbedIDs, inverseRelID)
		}
	}

	// Commit the transaction
	if err := tx.Commit(); err != nil {
		return nil, apperror.ErrDatabase.WithInternal(err)
	}

	// Enqueue async embeddings for all created/updated relationships.
	for _, relID := range relEmbedIDs {
		s.enqueueRelationshipEmbedding(ctx, relID)
	}

	return &CreateSubgraphResponse{
		Objects:       objResponses,
		Relationships: relResponses,
		RefMap:        refMap,
	}, nil
}

// branchLabel returns a human-readable label for a branch ID pointer.
func branchLabel(id *uuid.UUID) string {
	if id == nil {
		return "main"
	}
	return id.String()
}

// MoveObject moves a graph object (and its same-branch relationships) from its
// current branch to a target branch. The operation:
//  1. Validates the object exists and is not deleted
//  2. Checks source != target
//  3. Checks for type+key conflicts on the target branch
//  4. Finds all relationships on the source branch touching this object
//  5. Fails if any relationship connects to an object NOT being moved
//  6. Moves the object version chain + relationship version chains in one transaction
//  7. Re-queues embeddings and logs journal entry
func (s *Service) MoveObject(ctx context.Context, projectID, objectID uuid.UUID, req *MoveObjectRequest, actorID *uuid.UUID) (*MoveObjectResponse, error) {
	// 1. Fetch the object (resolve HEAD by any ID — version_id or entity_id)
	current, err := s.repo.GetByID(ctx, projectID, objectID)
	if err != nil {
		return nil, err
	}

	if current.DeletedAt != nil {
		return nil, apperror.ErrBadRequest.WithMessage("cannot move a deleted object; restore it first")
	}

	sourceBranchID := current.BranchID
	targetBranchID := req.TargetBranchID

	// 2. Check source != target
	if branchIDsEqual(sourceBranchID, targetBranchID) {
		return nil, apperror.ErrBadRequest.WithMessage(fmt.Sprintf("object is already on branch %s", branchLabel(sourceBranchID)))
	}

	// 3. Begin transaction with advisory lock
	tx, err := s.repo.BeginTx(ctx)
	if err != nil {
		return nil, apperror.ErrDatabase.WithInternal(err)
	}
	defer tx.Rollback()

	if err := s.repo.AcquireObjectLock(ctx, tx.Tx, current.CanonicalID); err != nil {
		return nil, err
	}

	// Re-fetch HEAD after lock to prevent races
	current, err = s.repo.GetHeadByCanonicalID(ctx, tx.Tx, projectID, current.CanonicalID, sourceBranchID)
	if err != nil {
		return nil, err
	}

	// 4. Check for type+key conflict on target branch
	if current.Key != nil && *current.Key != "" {
		conflict, err := s.repo.CheckObjectExistsOnBranch(ctx, tx.Tx, projectID, targetBranchID, current.Type, *current.Key)
		if err != nil {
			return nil, err
		}
		if conflict != nil {
			return nil, apperror.ErrConflict.WithMessage(fmt.Sprintf(
				"object with type=%q key=%q already exists on branch %s (entity_id: %s)",
				current.Type, *current.Key, branchLabel(targetBranchID), conflict.CanonicalID.String(),
			))
		}
	}

	// 5. Find all same-branch relationships touching this object
	rels, err := s.repo.GetRelationshipsByEndpoint(ctx, tx.Tx, projectID, current.CanonicalID, sourceBranchID)
	if err != nil {
		return nil, err
	}

	// Check that all relationship endpoints are this object (both sides same canonical).
	// If a relationship connects to a different object, fail — user must move that object
	// first, or delete the relationship.
	for _, rel := range rels {
		otherID := rel.DstID
		if rel.DstID == current.CanonicalID {
			otherID = rel.SrcID
		}
		if otherID != current.CanonicalID {
			// The other endpoint is a different object still on the source branch.
			return nil, apperror.ErrBadRequest.WithMessage(fmt.Sprintf(
				"cannot move: relationship %s (type=%q) connects to object %s which is on branch %s; "+
					"move or delete that object/relationship first",
				rel.CanonicalID.String(), rel.Type, otherID.String(), branchLabel(sourceBranchID),
			))
		}
	}

	// 6. Move the object version chain
	_, err = s.repo.MoveObjectVersionChain(ctx, tx.Tx, projectID, current.CanonicalID, targetBranchID)
	if err != nil {
		return nil, err
	}

	// Move associated relationship version chains (self-referencing relationships)
	movedRels := 0
	for _, rel := range rels {
		n, err := s.repo.MoveRelationshipVersionChain(ctx, tx.Tx, projectID, rel.CanonicalID, targetBranchID)
		if err != nil {
			return nil, err
		}
		if n > 0 {
			movedRels++
		}
	}

	// 7. Commit
	if err := tx.Commit(); err != nil {
		return nil, apperror.ErrDatabase.WithInternal(err)
	}

	// Re-queue embedding for the moved object
	s.enqueueEmbedding(ctx, current.ID.String())
	for _, rel := range rels {
		s.enqueueRelationshipEmbedding(ctx, rel.ID.String())
	}

	// Journal entry
	if s.journal != nil && current.Key != nil {
		objType := current.Type
		entityType := entityTypeObject
		s.journal.Log(ctx, journal.LogParams{
			ProjectID:  projectID,
			BranchID:   targetBranchID,
			EventType:  journal.EventTypeMoved,
			EntityType: &entityType,
			ObjectType: &objType,
			ActorType:  journal.ActorUser,
			ActorID:    actorID,
			Metadata: map[string]any{
				"key":                 *current.Key,
				"name":                nameFromProps(current.Properties),
				"object_type":         current.Type,
				"source_branch":       branchLabel(sourceBranchID),
				"target_branch":       branchLabel(targetBranchID),
				"moved_relationships": movedRels,
			},
		})
	}

	// Build response with updated branch_id
	current.BranchID = targetBranchID
	return &MoveObjectResponse{
		Object:             current.ToResponse(),
		MovedRelationships: movedRels,
		SourceBranchID:     sourceBranchID,
		TargetBranchID:     targetBranchID,
	}, nil
}

// ForkBranch creates a new branch from a source branch and copies all HEAD objects
// and their relationships. This is the "copy-on-fork" semantics requested by
// `branches create --parent --copy-objects`.
func (s *Service) ForkBranch(ctx context.Context, projectID uuid.UUID, sourceBranchID *uuid.UUID, req *ForkBranchRequest) (*ForkBranchResponse, error) {
	// Validate source branch exists (nil = main, no row to check)
	if sourceBranchID != nil {
		if _, err := s.repo.GetBranchByID(ctx, projectID, *sourceBranchID); err != nil {
			return nil, apperror.ErrNotFound.WithMessage("source branch not found")
		}
	}

	// Check target name uniqueness
	existing, err := s.repo.GetBranchByNameAndProject(ctx, req.Name, projectID)
	if err != nil {
		return nil, err
	}
	if existing != nil {
		return nil, apperror.ErrConflict.WithMessage("a branch with that name already exists")
	}

	// Create the new branch
	newBranch := &Branch{
		ID:        uuid.New(),
		ProjectID: projectID,
		Name:      req.Name,
	}
	if sourceBranchID != nil {
		newBranch.ParentBranchID = sourceBranchID
	}

	if err := s.repo.CreateBranch(ctx, newBranch); err != nil {
		return nil, fmt.Errorf("create branch: %w", err)
	}

	// Populate lineage closure table
	s.repo.EnsureBranchLineage(ctx, newBranch.ID, sourceBranchID)

	// Copy HEAD objects
	targetBranchID := newBranch.ID
	copiedCanonicals, copiedObjects, err := s.repo.BulkCopyObjectsToBranch(ctx, projectID, sourceBranchID, &targetBranchID, req.FilterTypes)
	if err != nil {
		return nil, fmt.Errorf("copy objects: %w", err)
	}

	// Copy relationships where both endpoints were copied
	copiedRels, skippedRels, err := s.repo.BulkCopyRelationshipsToBranch(ctx, projectID, sourceBranchID, &targetBranchID, copiedCanonicals)
	if err != nil {
		return nil, fmt.Errorf("copy relationships: %w", err)
	}

	// Journal log
	if s.journal != nil {
		s.journal.Log(ctx, journal.LogParams{
			ProjectID: projectID,
			BranchID:  &targetBranchID,
			EventType: journal.EventTypeBatch,
			ActorType: journal.ActorUser,
			Metadata: map[string]any{
				"action":                "fork",
				"source_branch_id":      sourceBranchID,
				"copied_objects":        copiedObjects,
				"copied_relationships":  copiedRels,
				"skipped_relationships": skippedRels,
			},
		})
	}

	sourceID := "main"
	if sourceBranchID != nil {
		sourceID = sourceBranchID.String()
	}

	return &ForkBranchResponse{
		BranchID:             newBranch.ID.String(),
		BranchName:           newBranch.Name,
		SourceBranchID:       sourceID,
		CopiedObjects:        copiedObjects,
		CopiedRelationships:  copiedRels,
		SkippedRelationships: skippedRels,
	}, nil
}

// GetRepository exposes the underlying Repository for use by cross-domain orchestrators.
func (s *Service) GetRepository() *Repository {
	return s.repo
}

// =============================================================================
// Bulk Action by Filter
// =============================================================================

const (
	bulkActionDefaultLimit = 1000
	bulkActionMaxLimit     = 100_000
)

// BulkAction executes a filter-then-action bulk operation on graph objects.
// Validates limits, applies defaults, calls the repository, and logs a journal entry.
func (s *Service) BulkAction(ctx context.Context, projectID uuid.UUID, req *BulkActionRequest, actorID *uuid.UUID) (*BulkActionResponse, error) {
	// Validate action
	switch req.Action {
	case BulkActionUpdateStatus, BulkActionSoftDelete, BulkActionHardDelete,
		BulkActionMergeProperties, BulkActionReplaceProperties,
		BulkActionSetLabels, BulkActionAddLabels, BulkActionRemoveLabels:
		// valid
	default:
		return nil, apperror.ErrBadRequest.WithMessage("unknown action: " + req.Action)
	}

	// Apply default and max limit
	limit := req.Limit
	if limit <= 0 {
		limit = bulkActionDefaultLimit
	}
	if limit > bulkActionMaxLimit {
		return nil, apperror.ErrBadRequest.WithMessage(fmt.Sprintf("limit exceeds maximum of %d", bulkActionMaxLimit))
	}

	matched, affected, err := s.repo.BulkActionByFilter(ctx, BulkActionParams{
		ProjectID:  projectID,
		Filter:     req.Filter,
		Action:     req.Action,
		Value:      req.Value,
		Properties: req.Properties,
		Labels:     req.Labels,
		Limit:      limit,
		DryRun:     req.DryRun,
	})
	if err != nil {
		return nil, err
	}

	resp := &BulkActionResponse{
		Matched:  matched,
		Affected: affected,
		DryRun:   req.DryRun,
	}

	// Write journal entry for non-dry-run operations
	if !req.DryRun && s.journal != nil {
		actorType := journal.ActorSystem
		if actorID != nil {
			actorType = journal.ActorUser
		}
		entityType := entityTypeObject
		s.journal.Log(ctx, journal.LogParams{
			ProjectID:  projectID,
			EventType:  journal.EventTypeBatch,
			EntityType: &entityType,
			ActorType:  actorType,
			ActorID:    actorID,
			Metadata: map[string]any{
				"action":   req.Action,
				"filter":   req.Filter,
				"matched":  matched,
				"affected": affected,
				"limit":    limit,
			},
		})
	}

	return resp, nil
}

// PatchGraphObjectTitle merges {"title": title} into the graph object's Properties.
// Implements mcp.GraphObjectPatcher. Best-effort: returns an error only if the object
// exists and the patch fails; a missing object is silently ignored.
func (s *Service) PatchGraphObjectTitle(ctx context.Context, projectID, objectID, title string) error {
	pid, err := uuid.Parse(projectID)
	if err != nil {
		return fmt.Errorf("PatchGraphObjectTitle: invalid project_id %q: %w", projectID, err)
	}
	oid, err := uuid.Parse(objectID)
	if err != nil {
		return fmt.Errorf("PatchGraphObjectTitle: invalid object_id %q: %w", objectID, err)
	}

	_, err = s.Patch(ctx, pid, oid, &PatchGraphObjectRequest{
		Properties: map[string]any{"title": title},
	}, nil)
	if err != nil {
		// Object not found is non-fatal — session may not yet exist as a graph object.
		var appErr *apperror.Error
		if errors.As(err, &appErr) && appErr.HTTPStatus == 404 {
			return nil
		}
		return fmt.Errorf("PatchGraphObjectTitle: %w", err)
	}
	return nil
}
