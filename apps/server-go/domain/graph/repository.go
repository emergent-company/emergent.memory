package graph

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/json"
	"fmt"
	"log/slog"
	"math"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/uptrace/bun"

	"github.com/emergent/emergent-core/internal/database"
	"github.com/emergent/emergent-core/pkg/apperror"
	"github.com/emergent/emergent-core/pkg/logger"
	"github.com/emergent/emergent-core/pkg/pgutils"
)

// Repository handles database operations for graph objects and relationships.
type Repository struct {
	db  bun.IDB
	log *slog.Logger
}

// NewRepository creates a new graph repository.
func NewRepository(db bun.IDB, log *slog.Logger) *Repository {
	return &Repository{
		db:  db,
		log: log.With(logger.Scope("graph.repo")),
	}
}

// UpdateAccessTimestamps updates last_accessed_at for the given object IDs.
// This is used to track when graph objects are accessed via search queries.
func (r *Repository) UpdateAccessTimestamps(ctx context.Context, objectIDs []uuid.UUID) error {
	if len(objectIDs) == 0 {
		return nil
	}

	_, err := r.db.NewUpdate().
		Model((*GraphObject)(nil)).
		Set("last_accessed_at = ?", time.Now()).
		Where("id IN (?)", bun.In(objectIDs)).
		Exec(ctx)

	if err != nil {
		return apperror.ErrDatabase.WithInternal(err)
	}
	return nil
}

// GetMostAccessed returns the most frequently accessed graph objects.
// minAccessCount filters objects that have been accessed at least N times (not implemented yet - always 1).
// Only returns HEAD versions (supersedes_id IS NULL) that have been accessed.
func (r *Repository) GetMostAccessed(ctx context.Context, projectID uuid.UUID, limit int, minAccessCount int) ([]*GraphObject, error) {
	if limit <= 0 {
		limit = 50
	}
	if limit > 200 {
		limit = 200
	}

	var objects []*GraphObject
	err := r.db.NewSelect().
		Model(&objects).
		Where("project_id = ?", projectID).
		Where("supersedes_id IS NULL").
		Where("last_accessed_at IS NOT NULL").
		Where("deleted_at IS NULL").
		Order("last_accessed_at DESC").
		Limit(limit).
		Scan(ctx)

	if err != nil {
		return nil, apperror.ErrDatabase.WithInternal(err)
	}

	return objects, nil
}

// GetUnused returns graph objects that haven't been accessed in the specified number of days.
// Only returns HEAD versions (supersedes_id IS NULL).
// Objects with NULL last_accessed_at are considered never accessed (included if threshold allows).
func (r *Repository) GetUnused(ctx context.Context, projectID uuid.UUID, limit int, daysThreshold int) ([]*GraphObject, error) {
	if limit <= 0 {
		limit = 50
	}
	if limit > 200 {
		limit = 200
	}
	if daysThreshold <= 0 {
		daysThreshold = 30
	}

	cutoffTime := time.Now().AddDate(0, 0, -daysThreshold)

	var objects []*GraphObject
	err := r.db.NewSelect().
		Model(&objects).
		Where("project_id = ?", projectID).
		Where("supersedes_id IS NULL").
		Where("deleted_at IS NULL").
		WhereGroup(" AND ", func(q *bun.SelectQuery) *bun.SelectQuery {
			return q.
				WhereOr("last_accessed_at IS NULL").
				WhereOr("last_accessed_at < ?", cutoffTime)
		}).
		Order("last_accessed_at ASC NULLS FIRST").
		Limit(limit).
		Scan(ctx)

	if err != nil {
		return nil, apperror.ErrDatabase.WithInternal(err)
	}

	return objects, nil
}

// ListParams contains parameters for listing graph objects.
type ListParams struct {
	ProjectID       uuid.UUID
	BranchID        *uuid.UUID
	Type            *string  // Single type (NestJS compat)
	Types           []string // Multiple types
	Label           *string  // Single label (NestJS compat)
	Labels          []string // Multiple labels
	Status          *string
	Key             *string
	IncludeDeleted  bool
	Limit           int
	Cursor          *string
	Order           string           // "asc" or "desc" (default: "desc")
	RelatedToID     *uuid.UUID       // Filter by related object
	IDs             []uuid.UUID      // Filter by specific IDs
	ExtractionJobID *uuid.UUID       // Filter by extraction job
	PropertyFilters []PropertyFilter // JSONB property filters
}

// applyPropertyFilters applies JSONB property filters to a Bun select query.
// Each PropertyFilter generates a WHERE clause against the properties JSONB column.
// Supported operators: eq, neq, gt, gte, lt, lte, contains, exists, in
func applyPropertyFilters(q *bun.SelectQuery, filters []PropertyFilter) *bun.SelectQuery {
	for _, f := range filters {
		// Convert dot-notation path to PostgreSQL JSONB accessor.
		// "name"         → properties->>'name'
		// "address.city" → properties->'address'->>'city'
		segments := strings.Split(f.Path, ".")
		if len(segments) == 0 {
			continue
		}

		// Build the accessor: intermediate segments use -> (returns JSON), last uses ->> (returns text)
		var textAccessor string // returns text via ->>
		var jsonAccessor string // returns jsonb via ->
		if len(segments) == 1 {
			textAccessor = "properties->>'" + segments[0] + "'"
			jsonAccessor = "properties->'" + segments[0] + "'"
		} else {
			// Build intermediate path with -> and final with ->>
			var builder strings.Builder
			builder.WriteString("properties")
			for _, seg := range segments[:len(segments)-1] {
				builder.WriteString("->'" + seg + "'")
			}
			jsonAccessor = builder.String() + "->'" + segments[len(segments)-1] + "'"
			textAccessor = builder.String() + "->>'" + segments[len(segments)-1] + "'"
		}

		switch f.Op {
		case "eq":
			q = q.Where(textAccessor+" = ?", fmt.Sprintf("%v", f.Value))
		case "neq":
			q = q.Where("("+textAccessor+" IS NULL OR "+textAccessor+" != ?)", fmt.Sprintf("%v", f.Value))
		case "gt":
			q = q.Where("("+textAccessor+")::numeric > ?::numeric", f.Value)
		case "gte":
			q = q.Where("("+textAccessor+")::numeric >= ?::numeric", f.Value)
		case "lt":
			q = q.Where("("+textAccessor+")::numeric < ?::numeric", f.Value)
		case "lte":
			q = q.Where("("+textAccessor+")::numeric <= ?::numeric", f.Value)
		case "contains":
			// Text contains (ILIKE) for string values
			q = q.Where(textAccessor+" ILIKE ?", "%"+fmt.Sprintf("%v", f.Value)+"%")
		case "exists":
			// Check if the key exists in the JSONB object
			q = q.Where(jsonAccessor + " IS NOT NULL")
		case "in":
			// Value should be an array
			if arr, ok := f.Value.([]interface{}); ok {
				strVals := make([]string, 0, len(arr))
				for _, v := range arr {
					strVals = append(strVals, fmt.Sprintf("%v", v))
				}
				q = q.Where(textAccessor+" IN (?)", bun.In(strVals))
			}
		}
	}
	return q
}

// List returns graph objects matching the given parameters.
// Returns only HEAD versions (latest version per canonical_id).
func (r *Repository) List(ctx context.Context, params ListParams) ([]*GraphObject, error) {
	if params.Limit <= 0 {
		params.Limit = 50
	}
	if params.Limit > 200 {
		params.Limit = 200
	}
	if params.Order == "" {
		params.Order = "desc"
	}

	// Build subquery to get HEAD versions
	subq := r.db.NewSelect().
		Model((*GraphObject)(nil)).
		Column("id", "project_id", "branch_id", "canonical_id", "supersedes_id", "version",
			"type", "key", "status", "properties", "labels", "change_summary",
			"created_at", "updated_at", "deleted_at", "actor_type", "actor_id", "schema_version",
			"extraction_job_id", "extraction_confidence", "needs_review", "reviewed_by", "reviewed_at",
			"content_hash").
		Where("project_id = ?", params.ProjectID).
		Where("supersedes_id IS NULL") // HEAD versions have no successor

	if params.BranchID != nil {
		subq = subq.Where("branch_id = ?", *params.BranchID)
	} else {
		subq = subq.Where("branch_id IS NULL")
	}

	// Filter by specific IDs if provided (accepts both physical id and canonical_id)
	if len(params.IDs) > 0 {
		subq = subq.Where("(id IN (?) OR canonical_id IN (?))", bun.In(params.IDs), bun.In(params.IDs))
	}

	// Support both single type (NestJS compat) and multiple types
	if params.Type != nil {
		subq = subq.Where("type = ?", *params.Type)
	} else if len(params.Types) > 0 {
		subq = subq.Where("type IN (?)", bun.In(params.Types))
	}

	// Support both single label (NestJS compat) and multiple labels
	if params.Label != nil {
		subq = subq.Where("? = ANY(labels)", *params.Label)
	} else if len(params.Labels) > 0 {
		subq = subq.Where("labels && ?::text[]", formatTextArray(params.Labels))
	}

	if params.Status != nil {
		subq = subq.Where("status = ?", *params.Status)
	}

	if params.Key != nil {
		subq = subq.Where("key = ?", *params.Key)
	}

	if params.ExtractionJobID != nil {
		subq = subq.Where("extraction_job_id = ?", *params.ExtractionJobID)
	}

	if !params.IncludeDeleted {
		subq = subq.Where("deleted_at IS NULL")
	}

	// Apply JSONB property filters
	if len(params.PropertyFilters) > 0 {
		subq = applyPropertyFilters(subq, params.PropertyFilters)
	}

	// Pagination via cursor (created_at, id)
	if params.Cursor != nil {
		// Decode cursor: base64(json({"created_at": "...", "id": "..."}))
		cursorData, err := decodeCursor(*params.Cursor)
		if err != nil {
			return nil, apperror.ErrBadRequest.WithMessage("invalid cursor")
		}
		if params.Order == "asc" {
			subq = subq.Where("(created_at, id) > (?, ?)", cursorData.CreatedAt, cursorData.ID)
		} else {
			subq = subq.Where("(created_at, id) < (?, ?)", cursorData.CreatedAt, cursorData.ID)
		}
	}

	if params.Order == "asc" {
		subq = subq.Order("created_at ASC", "id ASC")
	} else {
		subq = subq.Order("created_at DESC", "id DESC")
	}
	subq = subq.Limit(params.Limit + 1)

	var objects []*GraphObject
	err := subq.Scan(ctx, &objects)
	if err != nil {
		r.log.Error("failed to list graph objects", logger.Error(err))
		return nil, apperror.ErrDatabase.WithInternal(err)
	}

	return objects, nil
}

// Count returns the total count of graph objects matching the given parameters.
// This is used for pagination to return the true total, not just the page count.
func (r *Repository) Count(ctx context.Context, params ListParams) (int, error) {
	q := r.db.NewSelect().
		Model((*GraphObject)(nil)).
		Where("project_id = ?", params.ProjectID).
		Where("supersedes_id IS NULL") // HEAD versions only

	if params.BranchID != nil {
		q = q.Where("branch_id = ?", *params.BranchID)
	} else {
		q = q.Where("branch_id IS NULL")
	}

	// Filter by specific IDs if provided (accepts both physical id and canonical_id)
	if len(params.IDs) > 0 {
		q = q.Where("(id IN (?) OR canonical_id IN (?))", bun.In(params.IDs), bun.In(params.IDs))
	}

	// Support both single type (NestJS compat) and multiple types
	if params.Type != nil {
		q = q.Where("type = ?", *params.Type)
	} else if len(params.Types) > 0 {
		q = q.Where("type IN (?)", bun.In(params.Types))
	}

	// Support both single label (NestJS compat) and multiple labels
	if params.Label != nil {
		q = q.Where("? = ANY(labels)", *params.Label)
	} else if len(params.Labels) > 0 {
		q = q.Where("labels && ?::text[]", formatTextArray(params.Labels))
	}

	if params.Status != nil {
		q = q.Where("status = ?", *params.Status)
	}

	if params.Key != nil {
		q = q.Where("key = ?", *params.Key)
	}

	if params.ExtractionJobID != nil {
		q = q.Where("extraction_job_id = ?", *params.ExtractionJobID)
	}

	if !params.IncludeDeleted {
		q = q.Where("deleted_at IS NULL")
	}

	// Apply JSONB property filters
	if len(params.PropertyFilters) > 0 {
		q = applyPropertyFilters(q, params.PropertyFilters)
	}

	count, err := q.Count(ctx)
	if err != nil {
		r.log.Error("failed to count graph objects", logger.Error(err))
		return 0, apperror.ErrDatabase.WithInternal(err)
	}

	return count, nil
}

// GetByID returns a graph object by its physical ID or canonical ID.
// It accepts either type of ID transparently:
//   - If the ID matches a physical id, returns that object
//   - If the ID matches a canonical_id, returns the HEAD version
//
// HEAD versions (supersedes_id IS NULL) are preferred when multiple rows match.
func (r *Repository) GetByID(ctx context.Context, projectID, id uuid.UUID) (*GraphObject, error) {
	var objects []GraphObject
	err := r.db.NewSelect().
		Model(&objects).
		Where("(id = ? OR canonical_id = ?)", id, id).
		Where("project_id = ?", projectID).
		Where("deleted_at IS NULL").
		Scan(ctx)

	if err != nil {
		r.log.Error("failed to get graph object", logger.Error(err), slog.String("id", id.String()))
		return nil, apperror.ErrDatabase.WithInternal(err)
	}

	if len(objects) == 0 {
		return nil, apperror.ErrNotFound
	}

	// Prefer the HEAD version (supersedes_id IS NULL)
	for i := range objects {
		if objects[i].SupersedesID == nil {
			return &objects[i], nil
		}
	}

	// Fallback: return the first match (exact physical id hit on a non-HEAD version)
	return &objects[0], nil
}

// GetHeadByCanonicalID returns the HEAD version of a graph object by canonical ID.
func (r *Repository) GetHeadByCanonicalID(ctx context.Context, projectID, canonicalID uuid.UUID, branchID *uuid.UUID) (*GraphObject, error) {
	var obj GraphObject
	q := r.db.NewSelect().
		Model(&obj).
		Where("canonical_id = ?", canonicalID).
		Where("project_id = ?", projectID).
		Where("supersedes_id IS NULL") // HEAD version

	if branchID != nil {
		q = q.Where("branch_id = ?", *branchID)
	} else {
		q = q.Where("branch_id IS NULL")
	}

	err := q.Scan(ctx)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, apperror.ErrNotFound
		}
		r.log.Error("failed to get graph object by canonical ID", logger.Error(err))
		return nil, apperror.ErrDatabase.WithInternal(err)
	}

	return &obj, nil
}

// Create inserts a new graph object (version 1).
func (r *Repository) Create(ctx context.Context, obj *GraphObject) error {
	// Set defaults
	if obj.ID == uuid.Nil {
		obj.ID = uuid.New()
	}
	if obj.CanonicalID == uuid.Nil {
		obj.CanonicalID = obj.ID // First version: canonical_id == id
	}
	obj.Version = 1
	obj.ContentHash = computeContentHash(obj.Properties)
	now := time.Now()
	obj.CreatedAt = now
	obj.UpdatedAt = now

	if obj.Properties == nil {
		obj.Properties = make(map[string]any)
	}
	if obj.Labels == nil {
		obj.Labels = []string{}
	}

	_, err := r.db.NewInsert().
		Model(obj).
		Exec(ctx)

	if err != nil {
		r.log.Error("failed to create graph object", logger.Error(err))
		return apperror.ErrDatabase.WithInternal(err)
	}

	return nil
}

// CreateVersion creates a new version of an existing graph object.
// The previous HEAD's supersedes_id is set to the new version's ID.
func (r *Repository) CreateVersion(ctx context.Context, tx bun.Tx, prevHead *GraphObject, newVersion *GraphObject) error {
	// New version setup
	newVersion.ID = uuid.New()
	newVersion.CanonicalID = prevHead.CanonicalID
	newVersion.SupersedesID = nil // New version is HEAD
	newVersion.Version = prevHead.Version + 1
	newVersion.ProjectID = prevHead.ProjectID
	newVersion.BranchID = prevHead.BranchID
	newVersion.ContentHash = computeContentHash(newVersion.Properties)
	now := time.Now()
	newVersion.CreatedAt = now
	newVersion.UpdatedAt = now

	// Update previous HEAD first to clear the unique index slot.
	// The unique index IDX_graph_objects_upsert_main enforces one HEAD
	// (supersedes_id IS NULL) per (project_id, type, key), so the old HEAD
	// must be marked as superseded before the new HEAD can be inserted.
	_, err := tx.NewUpdate().
		Model((*GraphObject)(nil)).
		Set("supersedes_id = ?", newVersion.ID).
		Set("updated_at = ?", now).
		Where("id = ?", prevHead.ID).
		Exec(ctx)
	if err != nil {
		return apperror.ErrDatabase.WithInternal(err)
	}

	// Insert new version as HEAD
	_, err = tx.NewInsert().
		Model(newVersion).
		Exec(ctx)
	if err != nil {
		return apperror.ErrDatabase.WithInternal(err)
	}

	return nil
}

// SoftDelete marks a graph object as deleted by creating a tombstone version.
func (r *Repository) SoftDelete(ctx context.Context, tx bun.Tx, obj *GraphObject, actorID *uuid.UUID) error {
	now := time.Now()
	tombstone := &GraphObject{
		Type:       obj.Type,
		Key:        obj.Key,
		Status:     obj.Status,
		Properties: obj.Properties,
		Labels:     obj.Labels,
		DeletedAt:  &now,
		ActorID:    actorID,
	}
	actorType := "user"
	tombstone.ActorType = &actorType

	return r.CreateVersion(ctx, tx, obj, tombstone)
}

// Restore removes the deleted_at flag by creating a new non-deleted version.
func (r *Repository) Restore(ctx context.Context, tx bun.Tx, obj *GraphObject, actorID *uuid.UUID) error {
	restored := &GraphObject{
		Type:       obj.Type,
		Key:        obj.Key,
		Status:     obj.Status,
		Properties: obj.Properties,
		Labels:     obj.Labels,
		DeletedAt:  nil,
		ActorID:    actorID,
	}
	actorType := "user"
	restored.ActorType = &actorType

	return r.CreateVersion(ctx, tx, obj, restored)
}

// GetHistory returns all versions of a graph object by canonical ID.
func (r *Repository) GetHistory(ctx context.Context, projectID, canonicalID uuid.UUID) ([]*GraphObject, error) {
	var versions []*GraphObject
	err := r.db.NewSelect().
		Model(&versions).
		Where("canonical_id = ?", canonicalID).
		Where("project_id = ?", projectID).
		Order("version DESC").
		Scan(ctx)

	if err != nil {
		r.log.Error("failed to get object history", logger.Error(err))
		return nil, apperror.ErrDatabase.WithInternal(err)
	}

	return versions, nil
}

// GetEdges returns incoming and outgoing relationships for an object by its canonical_id.
// Relationships store canonical_id values in src_id/dst_id, so this matches directly.
func (r *Repository) GetEdges(ctx context.Context, projectID, canonicalID uuid.UUID, params GetEdgesParams) ([]*GraphRelationship, []*GraphRelationship, error) {
	var incoming []*GraphRelationship
	var outgoing []*GraphRelationship

	// Collect relationship types to filter by
	var typeFilter []string
	if params.Type != "" {
		typeFilter = append(typeFilter, params.Type)
	}
	if len(params.Types) > 0 {
		typeFilter = append(typeFilter, params.Types...)
	}

	// Get incoming edges (object is destination) unless direction is "outgoing"
	if params.Direction != "outgoing" {
		q := r.db.NewSelect().
			Model(&incoming).
			Where("dst_id = ?", canonicalID).
			Where("project_id = ?", projectID).
			Where("supersedes_id IS NULL").
			Where("deleted_at IS NULL")
		if len(typeFilter) > 0 {
			q = q.Where("type IN (?)", bun.In(typeFilter))
		}
		err := q.Scan(ctx)
		if err != nil && err != sql.ErrNoRows {
			return nil, nil, apperror.ErrDatabase.WithInternal(err)
		}
	}

	// Get outgoing edges (object is source) unless direction is "incoming"
	if params.Direction != "incoming" {
		q := r.db.NewSelect().
			Model(&outgoing).
			Where("src_id = ?", canonicalID).
			Where("project_id = ?", projectID).
			Where("supersedes_id IS NULL").
			Where("deleted_at IS NULL")
		if len(typeFilter) > 0 {
			q = q.Where("type IN (?)", bun.In(typeFilter))
		}
		err := q.Scan(ctx)
		if err != nil && err != sql.ErrNoRows {
			return nil, nil, apperror.ErrDatabase.WithInternal(err)
		}
	}

	return incoming, outgoing, nil
}

// AcquireObjectLock acquires an advisory lock for a graph object.
// The lock is released when the transaction commits or rolls back.
func (r *Repository) AcquireObjectLock(ctx context.Context, tx bun.Tx, canonicalID uuid.UUID) error {
	lockKey := "obj|" + canonicalID.String()
	_, err := tx.ExecContext(ctx, "SELECT pg_advisory_xact_lock(hashtext(?)::bigint)", lockKey)
	if err != nil {
		return apperror.ErrDatabase.WithInternal(err)
	}
	return nil
}

// FindHeadByTypeAndKey returns the HEAD version of an object identified by (project_id, type, key).
// Returns nil, nil if not found (not an error).
func (r *Repository) FindHeadByTypeAndKey(ctx context.Context, projectID uuid.UUID, branchID *uuid.UUID, objType string, key string) (*GraphObject, error) {
	var obj GraphObject
	q := r.db.NewSelect().
		Model(&obj).
		Where("project_id = ?", projectID).
		Where("type = ?", objType).
		Where("key = ?", key).
		Where("supersedes_id IS NULL")

	if branchID != nil {
		q = q.Where("branch_id = ?", *branchID)
	} else {
		q = q.Where("branch_id IS NULL")
	}

	err := q.Scan(ctx)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, nil // Not found is not an error for this method
		}
		return nil, apperror.ErrDatabase.WithInternal(err)
	}

	return &obj, nil
}

// AcquireObjectUpsertLock acquires an advisory lock for an object upsert by (project_id, type, key).
// The lock is released when the transaction commits or rolls back.
func (r *Repository) AcquireObjectUpsertLock(ctx context.Context, tx bun.Tx, projectID uuid.UUID, objType string, key string) error {
	lockKey := "obj-upsert|" + projectID.String() + "|" + objType + "|" + key
	_, err := tx.ExecContext(ctx, "SELECT pg_advisory_xact_lock(hashtext(?)::bigint)", lockKey)
	if err != nil {
		return apperror.ErrDatabase.WithInternal(err)
	}
	return nil
}

// CreateInTx inserts a new graph object (version 1) within an existing transaction.
func (r *Repository) CreateInTx(ctx context.Context, tx bun.Tx, obj *GraphObject) error {
	// Set defaults
	if obj.ID == uuid.Nil {
		obj.ID = uuid.New()
	}
	if obj.CanonicalID == uuid.Nil {
		obj.CanonicalID = obj.ID // First version: canonical_id == id
	}
	obj.Version = 1
	obj.ContentHash = computeContentHash(obj.Properties)
	now := time.Now()
	obj.CreatedAt = now
	obj.UpdatedAt = now

	if obj.Properties == nil {
		obj.Properties = make(map[string]any)
	}
	if obj.Labels == nil {
		obj.Labels = []string{}
	}

	_, err := tx.NewInsert().
		Model(obj).
		Exec(ctx)

	if err != nil {
		r.log.Error("failed to create graph object in tx", logger.Error(err))
		return apperror.ErrDatabase.WithInternal(err)
	}

	return nil
}

// BeginTx starts a new database transaction.
// Returns a SafeTx that's safe to call Rollback after Commit (important for savepoints).
func (r *Repository) BeginTx(ctx context.Context) (*database.SafeTx, error) {
	return database.BeginSafeTx(ctx, r.db)
}

// Cursor represents pagination state.
type Cursor struct {
	CreatedAt time.Time `json:"created_at"`
	ID        uuid.UUID `json:"id"`
}

func decodeCursor(encoded string) (*Cursor, error) {
	// For now, simple JSON - could use base64 encoding
	var c Cursor
	if err := json.Unmarshal([]byte(encoded), &c); err != nil {
		return nil, err
	}
	return &c, nil
}

func encodeCursor(createdAt time.Time, id uuid.UUID) string {
	c := Cursor{CreatedAt: createdAt, ID: id}
	data, _ := json.Marshal(c)
	return string(data)
}

// computeContentHash creates a SHA-256 hash of the properties for deduplication.
func computeContentHash(properties map[string]any) []byte {
	if properties == nil {
		properties = make(map[string]any)
	}

	// Sort keys for deterministic ordering
	keys := make([]string, 0, len(properties))
	for k := range properties {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	sorted := make(map[string]any)
	for _, k := range keys {
		sorted[k] = properties[k]
	}

	data, _ := json.Marshal(sorted)
	hash := sha256.Sum256(data)
	return hash[:]
}

// =============================================================================
// Relationship Operations
// =============================================================================

// RelationshipListParams contains parameters for listing relationships.
type RelationshipListParams struct {
	ProjectID      uuid.UUID
	BranchID       *uuid.UUID
	Type           *string
	SrcID          *uuid.UUID
	DstID          *uuid.UUID
	IncludeDeleted bool
	Limit          int
	Cursor         *string
	Order          string // "asc" or "desc"
}

// ListRelationships returns relationships matching the given parameters.
// Returns only HEAD versions (supersedes_id IS NULL).
func (r *Repository) ListRelationships(ctx context.Context, params RelationshipListParams) ([]*GraphRelationship, error) {
	if params.Limit <= 0 {
		params.Limit = 20
	}
	if params.Limit > 200 {
		params.Limit = 200
	}
	if params.Order == "" {
		params.Order = "asc"
	}

	q := r.db.NewSelect().
		Model((*GraphRelationship)(nil)).
		Where("project_id = ?", params.ProjectID).
		Where("supersedes_id IS NULL") // HEAD versions only

	if params.BranchID != nil {
		q = q.Where("branch_id = ?", *params.BranchID)
	} else {
		q = q.Where("branch_id IS NULL")
	}

	if params.Type != nil {
		q = q.Where("type = ?", *params.Type)
	}

	if params.SrcID != nil {
		q = q.Where("src_id = ?", *params.SrcID)
	}

	if params.DstID != nil {
		q = q.Where("dst_id = ?", *params.DstID)
	}

	if !params.IncludeDeleted {
		q = q.Where("deleted_at IS NULL")
	}

	// Pagination via cursor (created_at)
	if params.Cursor != nil {
		cursorData, err := decodeCursor(*params.Cursor)
		if err != nil {
			return nil, apperror.ErrBadRequest.WithMessage("invalid cursor")
		}
		if params.Order == "desc" {
			q = q.Where("(created_at, id) < (?, ?)", cursorData.CreatedAt, cursorData.ID)
		} else {
			q = q.Where("(created_at, id) > (?, ?)", cursorData.CreatedAt, cursorData.ID)
		}
	}

	if params.Order == "desc" {
		q = q.Order("created_at DESC", "id DESC")
	} else {
		q = q.Order("created_at ASC", "id ASC")
	}

	q = q.Limit(params.Limit + 1)

	var rels []*GraphRelationship
	err := q.Scan(ctx, &rels)
	if err != nil {
		r.log.Error("failed to list relationships", logger.Error(err))
		return nil, apperror.ErrDatabase.WithInternal(err)
	}

	return rels, nil
}

// GetRelationshipByID returns a relationship by its physical ID or canonical ID.
// Accepts either type of ID transparently, preferring the HEAD version.
func (r *Repository) GetRelationshipByID(ctx context.Context, projectID, id uuid.UUID) (*GraphRelationship, error) {
	var rels []GraphRelationship
	err := r.db.NewSelect().
		Model(&rels).
		Where("(id = ? OR canonical_id = ?)", id, id).
		Where("project_id = ?", projectID).
		Scan(ctx)

	if err != nil {
		r.log.Error("failed to get relationship", logger.Error(err), slog.String("id", id.String()))
		return nil, apperror.ErrDatabase.WithInternal(err)
	}

	if len(rels) == 0 {
		return nil, apperror.ErrNotFound
	}

	// Prefer the HEAD version (supersedes_id IS NULL)
	for i := range rels {
		if rels[i].SupersedesID == nil {
			return &rels[i], nil
		}
	}

	// Fallback: return the first match
	return &rels[0], nil
}

// GetRelationshipHead returns the HEAD version of a relationship by type, src, dst.
func (r *Repository) GetRelationshipHead(ctx context.Context, projectID uuid.UUID, branchID *uuid.UUID, relType string, srcID, dstID uuid.UUID) (*GraphRelationship, error) {
	var rel GraphRelationship
	q := r.db.NewSelect().
		Model(&rel).
		Where("project_id = ?", projectID).
		Where("type = ?", relType).
		Where("src_id = ?", srcID).
		Where("dst_id = ?", dstID).
		Where("supersedes_id IS NULL")

	if branchID != nil {
		q = q.Where("branch_id = ?", *branchID)
	} else {
		q = q.Where("branch_id IS NULL")
	}

	err := q.Scan(ctx)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, nil // Not found is not an error for this method
		}
		return nil, apperror.ErrDatabase.WithInternal(err)
	}

	return &rel, nil
}

// CreateRelationship inserts a new relationship (version 1).
func (r *Repository) CreateRelationship(ctx context.Context, tx bun.Tx, rel *GraphRelationship) error {
	// Set defaults
	if rel.ID == uuid.Nil {
		rel.ID = uuid.New()
	}
	if rel.CanonicalID == uuid.Nil {
		rel.CanonicalID = rel.ID // First version: canonical_id == id
	}
	rel.Version = 1
	rel.ContentHash = computeContentHash(rel.Properties)
	rel.CreatedAt = time.Now()

	if rel.Properties == nil {
		rel.Properties = make(map[string]any)
	}

	_, err := tx.NewInsert().Model(rel).Exec(ctx)
	if err != nil {
		r.log.Error("failed to create relationship", logger.Error(err))
		return apperror.ErrDatabase.WithInternal(err)
	}

	return nil
}

// CreateRelationshipVersion creates a new version of an existing relationship.
func (r *Repository) CreateRelationshipVersion(ctx context.Context, tx bun.Tx, prevHead *GraphRelationship, newVersion *GraphRelationship) error {
	// New version setup
	newVersion.ID = uuid.New()
	newVersion.CanonicalID = prevHead.CanonicalID
	newVersion.SupersedesID = nil // New version is HEAD
	newVersion.Version = prevHead.Version + 1
	newVersion.ProjectID = prevHead.ProjectID
	newVersion.BranchID = prevHead.BranchID
	newVersion.Type = prevHead.Type
	newVersion.SrcID = prevHead.SrcID
	newVersion.DstID = prevHead.DstID
	newVersion.ContentHash = computeContentHash(newVersion.Properties)
	newVersion.CreatedAt = time.Now()

	// Insert new version
	_, err := tx.NewInsert().
		Model(newVersion).
		Exec(ctx)
	if err != nil {
		return apperror.ErrDatabase.WithInternal(err)
	}

	// Update previous HEAD to point to new version
	_, err = tx.NewUpdate().
		Model((*GraphRelationship)(nil)).
		Set("supersedes_id = ?", newVersion.ID).
		Where("id = ?", prevHead.ID).
		Exec(ctx)
	if err != nil {
		return apperror.ErrDatabase.WithInternal(err)
	}

	return nil
}

// SoftDeleteRelationship marks a relationship as deleted by creating a tombstone version.
func (r *Repository) SoftDeleteRelationship(ctx context.Context, tx bun.Tx, rel *GraphRelationship) error {
	now := time.Now()
	tombstone := &GraphRelationship{
		Properties: rel.Properties,
		Weight:     rel.Weight,
		DeletedAt:  &now,
	}

	return r.CreateRelationshipVersion(ctx, tx, rel, tombstone)
}

// RestoreRelationship removes the deleted_at flag by creating a new non-deleted version.
func (r *Repository) RestoreRelationship(ctx context.Context, tx bun.Tx, rel *GraphRelationship) error {
	restored := &GraphRelationship{
		Properties: rel.Properties,
		Weight:     rel.Weight,
		DeletedAt:  nil,
	}

	return r.CreateRelationshipVersion(ctx, tx, rel, restored)
}

// GetRelationshipHistory returns all versions of a relationship by canonical ID.
func (r *Repository) GetRelationshipHistory(ctx context.Context, projectID, canonicalID uuid.UUID) ([]*GraphRelationship, error) {
	var versions []*GraphRelationship
	err := r.db.NewSelect().
		Model(&versions).
		Where("canonical_id = ?", canonicalID).
		Where("project_id = ?", projectID).
		Order("version DESC").
		Scan(ctx)

	if err != nil {
		r.log.Error("failed to get relationship history", logger.Error(err))
		return nil, apperror.ErrDatabase.WithInternal(err)
	}

	return versions, nil
}

// GetRelationshipHeadByCanonicalID returns the HEAD version by canonical ID.
func (r *Repository) GetRelationshipHeadByCanonicalID(ctx context.Context, projectID, canonicalID uuid.UUID) (*GraphRelationship, error) {
	var rel GraphRelationship
	err := r.db.NewSelect().
		Model(&rel).
		Where("canonical_id = ?", canonicalID).
		Where("project_id = ?", projectID).
		Where("supersedes_id IS NULL").
		Scan(ctx)

	if err != nil {
		if err == sql.ErrNoRows {
			return nil, apperror.ErrNotFound
		}
		return nil, apperror.ErrDatabase.WithInternal(err)
	}

	return &rel, nil
}

// AcquireRelationshipLock acquires an advisory lock for a relationship.
func (r *Repository) AcquireRelationshipLock(ctx context.Context, tx bun.Tx, projectID uuid.UUID, relType string, srcID, dstID uuid.UUID) error {
	lockKey := "rel|" + projectID.String() + "|" + relType + "|" + srcID.String() + "|" + dstID.String()
	_, err := tx.ExecContext(ctx, "SELECT pg_advisory_xact_lock(hashtext(?)::bigint)", lockKey)
	if err != nil {
		return apperror.ErrDatabase.WithInternal(err)
	}
	return nil
}

// ValidateEndpoints checks that src and dst objects exist and are not deleted.
func (r *Repository) ValidateEndpoints(ctx context.Context, tx bun.Tx, projectID, srcID, dstID uuid.UUID) (*GraphObject, *GraphObject, error) {
	var objects []*GraphObject

	// Look up objects by physical ID or canonical_id (supports both).
	// The caller may pass either the physical id (from a specific version) or
	// the canonical_id (stable logical identity). We resolve to the HEAD version
	// so that relationships are always anchored to canonical identities.
	err := tx.NewSelect().
		Model(&objects).
		Where("(id IN (?) OR canonical_id IN (?))", bun.In([]uuid.UUID{srcID, dstID}), bun.In([]uuid.UUID{srcID, dstID})).
		Where("supersedes_id IS NULL"). // HEAD versions only
		Where("project_id = ?", projectID).
		Scan(ctx)

	if err != nil {
		return nil, nil, apperror.ErrDatabase.WithInternal(err)
	}

	var srcObj, dstObj *GraphObject
	for _, obj := range objects {
		// Match by physical id or canonical_id
		if obj.ID == srcID || obj.CanonicalID == srcID {
			srcObj = obj
		}
		if obj.ID == dstID || obj.CanonicalID == dstID {
			dstObj = obj
		}
	}

	if srcObj == nil {
		return nil, nil, apperror.ErrNotFound.WithMessage("src_object_not_found")
	}
	if dstObj == nil {
		return nil, nil, apperror.ErrNotFound.WithMessage("dst_object_not_found")
	}

	if srcObj.DeletedAt != nil {
		return nil, nil, apperror.ErrBadRequest.WithMessage("src_object_deleted")
	}
	if dstObj.DeletedAt != nil {
		return nil, nil, apperror.ErrBadRequest.WithMessage("dst_object_deleted")
	}

	// Prevent self-referencing relationships (src and dst resolve to the same logical object)
	if srcObj.CanonicalID == dstObj.CanonicalID {
		return nil, nil, apperror.ErrBadRequest.WithMessage("self_referencing_relationship_not_allowed")
	}

	// Verify same project
	if srcObj.ProjectID != projectID || dstObj.ProjectID != projectID {
		return nil, nil, apperror.ErrBadRequest.WithMessage("relationship_project_mismatch")
	}

	return srcObj, dstObj, nil
}

// =============================================================================
// Search Operations
// =============================================================================

// searchFilters contains common filter parameters for search queries.
type searchFilters struct {
	ProjectID      uuid.UUID
	BranchID       *uuid.UUID
	Types          []string
	Labels         []string
	Status         *string
	IncludeDeleted bool
}

// buildSearchFilters builds WHERE conditions and args for common search filters.
// Returns conditions and args to be appended to existing slices.
func buildSearchFilters(filters searchFilters) (conditions []string, args []any) {
	if filters.BranchID != nil {
		conditions = append(conditions, "branch_id = ?")
		args = append(args, *filters.BranchID)
	} else {
		conditions = append(conditions, "branch_id IS NULL")
	}

	if len(filters.Types) > 0 {
		conditions = append(conditions, "type = ANY(?::text[])")
		args = append(args, formatTextArray(filters.Types))
	}

	if len(filters.Labels) > 0 {
		conditions = append(conditions, "labels && ?::text[]")
		args = append(args, formatTextArray(filters.Labels))
	}

	if filters.Status != nil {
		conditions = append(conditions, "status = ?")
		args = append(args, *filters.Status)
	}

	if !filters.IncludeDeleted {
		conditions = append(conditions, "deleted_at IS NULL")
	}

	return conditions, args
}

// buildWhereClause joins conditions into a WHERE clause.
func buildWhereClause(conditions []string) string {
	if len(conditions) == 0 {
		return ""
	}
	return "WHERE " + strings.Join(conditions, " AND ")
}

// graphObjectColumns is the list of columns to select for GraphObject.
const graphObjectColumns = `id, project_id, branch_id, canonical_id, supersedes_id, version,
	type, key, status, properties, labels, change_summary, content_hash,
	created_at, updated_at, deleted_at, fts, embedding_updated_at,
	extraction_job_id, extraction_confidence, needs_review, reviewed_by, reviewed_at,
	actor_type, actor_id, schema_version`

// scanGraphObject scans a row into a GraphObject.
func scanGraphObject(rows *sql.Rows, obj *GraphObject) error {
	return rows.Scan(
		&obj.ID, &obj.ProjectID, &obj.BranchID, &obj.CanonicalID, &obj.SupersedesID, &obj.Version,
		&obj.Type, &obj.Key, &obj.Status, &obj.Properties, &obj.Labels, &obj.ChangeSummary, &obj.ContentHash,
		&obj.CreatedAt, &obj.UpdatedAt, &obj.DeletedAt, &obj.FTS, &obj.EmbeddingUpdatedAt,
		&obj.ExtractionJobID, &obj.ExtractionConfidence, &obj.NeedsReview, &obj.ReviewedBy, &obj.ReviewedAt,
		&obj.ActorType, &obj.ActorID, &obj.SchemaVersion,
	)
}

// FTSSearchParams contains parameters for full-text search.
type FTSSearchParams struct {
	ProjectID      uuid.UUID
	Query          string
	BranchID       *uuid.UUID
	Types          []string
	Labels         []string
	Status         *string
	IncludeDeleted bool
	Limit          int
	Offset         int
}

// FTSSearchResult represents a single FTS search result with score.
type FTSSearchResult struct {
	Object *GraphObject
	Rank   float32
}

// FTSSearch performs full-text search using PostgreSQL's websearch_to_tsquery.
// Returns objects sorted by relevance (ts_rank).
func (r *Repository) FTSSearch(ctx context.Context, params FTSSearchParams) ([]*FTSSearchResult, error) {
	if params.Limit <= 0 {
		params.Limit = 20
	}
	if params.Limit > 200 {
		params.Limit = 200
	}

	// Build WHERE conditions
	conditions := []string{
		"project_id = ?",
		"supersedes_id IS NULL", // HEAD versions only
		"fts @@ websearch_to_tsquery('simple', ?)",
	}
	args := []any{params.ProjectID, params.Query}

	// Add common filters
	filterConds, filterArgs := buildSearchFilters(searchFilters{
		ProjectID:      params.ProjectID,
		BranchID:       params.BranchID,
		Types:          params.Types,
		Labels:         params.Labels,
		Status:         params.Status,
		IncludeDeleted: params.IncludeDeleted,
	})
	conditions = append(conditions, filterConds...)
	args = append(args, filterArgs...)

	whereClause := buildWhereClause(conditions)

	// Build the query with ts_rank
	query := `
		SELECT ` + graphObjectColumns + `,
			ts_rank(fts, websearch_to_tsquery('simple', ?)) AS rank
		FROM kb.graph_objects
		` + whereClause + `
		ORDER BY rank DESC
		LIMIT ?
		OFFSET ?
	`

	// Prepend query param for ts_rank, append limit and offset
	finalArgs := append([]any{params.Query}, args...)
	finalArgs = append(finalArgs, params.Limit, params.Offset)

	rows, err := r.db.QueryContext(ctx, query, finalArgs...)
	if err != nil {
		r.log.Error("FTS search failed", logger.Error(err))
		return nil, apperror.ErrDatabase.WithInternal(err)
	}
	defer rows.Close()

	var results []*FTSSearchResult
	for rows.Next() {
		obj := &GraphObject{}
		var rank float32
		err := rows.Scan(
			&obj.ID, &obj.ProjectID, &obj.BranchID, &obj.CanonicalID, &obj.SupersedesID, &obj.Version,
			&obj.Type, &obj.Key, &obj.Status, &obj.Properties, &obj.Labels, &obj.ChangeSummary, &obj.ContentHash,
			&obj.CreatedAt, &obj.UpdatedAt, &obj.DeletedAt, &obj.FTS, &obj.EmbeddingUpdatedAt,
			&obj.ExtractionJobID, &obj.ExtractionConfidence, &obj.NeedsReview, &obj.ReviewedBy, &obj.ReviewedAt,
			&obj.ActorType, &obj.ActorID, &obj.SchemaVersion,
			&rank,
		)
		if err != nil {
			r.log.Error("FTS search row scan failed", logger.Error(err))
			return nil, apperror.ErrDatabase.WithInternal(err)
		}
		results = append(results, &FTSSearchResult{Object: obj, Rank: rank})
	}

	if err := rows.Err(); err != nil {
		return nil, apperror.ErrDatabase.WithInternal(err)
	}

	return results, nil
}

// VectorSearchParams contains parameters for vector similarity search.
type VectorSearchParams struct {
	ProjectID      uuid.UUID
	Vector         []float32
	BranchID       *uuid.UUID
	Types          []string
	Labels         []string
	Status         *string
	IncludeDeleted bool
	MaxDistance    *float32
	Limit          int
	Offset         int
}

// VectorSearchResult represents a single vector search result with distance.
type VectorSearchResult struct {
	Object   *GraphObject
	Distance float32
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

// VectorSearch performs vector similarity search using pgvector's cosine distance.
// Returns objects sorted by similarity (ascending distance).
func (r *Repository) VectorSearch(ctx context.Context, params VectorSearchParams) ([]*VectorSearchResult, error) {
	if params.Limit <= 0 {
		params.Limit = 20
	}
	if params.Limit > 200 {
		params.Limit = 200
	}

	// Format vector as PostgreSQL array string: '[0.1,0.2,...]'
	vectorStr := pgutils.FormatVector(params.Vector)

	// Build WHERE conditions
	conditions := []string{
		"project_id = ?",
		"supersedes_id IS NULL",    // HEAD versions only
		"embedding_v2 IS NOT NULL", // Must have embedding
	}
	args := []any{params.ProjectID}

	// Add common filters
	filterConds, filterArgs := buildSearchFilters(searchFilters{
		ProjectID:      params.ProjectID,
		BranchID:       params.BranchID,
		Types:          params.Types,
		Labels:         params.Labels,
		Status:         params.Status,
		IncludeDeleted: params.IncludeDeleted,
	})
	conditions = append(conditions, filterConds...)
	args = append(args, filterArgs...)

	// Max distance filter (specific to vector search)
	if params.MaxDistance != nil {
		conditions = append(conditions, "(embedding_v2 <=> ?::vector) <= ?")
		args = append(args, vectorStr, *params.MaxDistance)
	}

	whereClause := buildWhereClause(conditions)

	// Build the query with cosine distance
	query := `
		SELECT ` + graphObjectColumns + `,
			(embedding_v2 <=> ?::vector) AS distance
		FROM kb.graph_objects
		` + whereClause + `
		ORDER BY distance ASC
		LIMIT ?
		OFFSET ?
	`

	// Prepend vector param for distance calculation, append limit and offset
	finalArgs := append([]any{vectorStr}, args...)
	finalArgs = append(finalArgs, params.Limit, params.Offset)

	// Begin transaction with increased IVFFlat probes for better recall
	tx, err := r.beginTxWithIVFFlatProbes(ctx, 10)
	if err != nil {
		r.log.Error("vector search: failed to set ivfflat probes", logger.Error(err))
		return nil, err
	}
	defer func() { _ = tx.Rollback() }()

	rows, err := tx.QueryContext(ctx, query, finalArgs...)
	if err != nil {
		r.log.Error("Vector search failed", logger.Error(err))
		return nil, apperror.ErrDatabase.WithInternal(err)
	}
	defer rows.Close()

	var results []*VectorSearchResult
	for rows.Next() {
		obj := &GraphObject{}
		var distance float32
		err := rows.Scan(
			&obj.ID, &obj.ProjectID, &obj.BranchID, &obj.CanonicalID, &obj.SupersedesID, &obj.Version,
			&obj.Type, &obj.Key, &obj.Status, &obj.Properties, &obj.Labels, &obj.ChangeSummary, &obj.ContentHash,
			&obj.CreatedAt, &obj.UpdatedAt, &obj.DeletedAt, &obj.FTS, &obj.EmbeddingUpdatedAt,
			&obj.ExtractionJobID, &obj.ExtractionConfidence, &obj.NeedsReview, &obj.ReviewedBy, &obj.ReviewedAt,
			&obj.ActorType, &obj.ActorID, &obj.SchemaVersion,
			&distance,
		)
		if err != nil {
			r.log.Error("Vector search row scan failed", logger.Error(err))
			return nil, apperror.ErrDatabase.WithInternal(err)
		}
		results = append(results, &VectorSearchResult{Object: obj, Distance: distance})
	}

	if err := rows.Err(); err != nil {
		return nil, apperror.ErrDatabase.WithInternal(err)
	}

	// Commit the read-only transaction
	if err := tx.Commit(); err != nil {
		return nil, apperror.ErrDatabase.WithInternal(err)
	}

	return results, nil
}

// formatTextArray converts a string slice to PostgreSQL text array literal format.
// Example: ["foo", "bar baz"] -> {foo,"bar baz"}
func formatTextArray(arr []string) string {
	if len(arr) == 0 {
		return "{}"
	}

	var buf strings.Builder
	buf.WriteByte('{')

	for i, s := range arr {
		if i > 0 {
			buf.WriteByte(',')
		}
		// Quote strings that contain special characters
		needsQuote := strings.ContainsAny(s, `{},"\`)
		if needsQuote {
			buf.WriteByte('"')
			// Escape backslashes and quotes
			for _, c := range s {
				if c == '\\' || c == '"' {
					buf.WriteByte('\\')
				}
				buf.WriteRune(c)
			}
			buf.WriteByte('"')
		} else {
			buf.WriteString(s)
		}
	}

	buf.WriteByte('}')
	return buf.String()
}

// GetDistinctTagsParams holds optional filtering parameters for tag retrieval.
type GetDistinctTagsParams struct {
	ObjectType string // Filter tags to objects of this type
	Prefix     string // Filter tags that start with this prefix
	Limit      int    // Maximum number of tags to return (0 = no limit)
}

// GetDistinctTags returns all distinct tags (labels) used by objects in a project.
func (r *Repository) GetDistinctTags(ctx context.Context, projectID uuid.UUID, params *GetDistinctTagsParams) ([]string, error) {
	var tags []string

	args := []interface{}{projectID}

	query := `
		SELECT DISTINCT unnest(labels) as tag
		FROM kb.graph_objects
		WHERE project_id = ?
		  AND supersedes_id IS NULL
		  AND deleted_at IS NULL`

	if params != nil && params.ObjectType != "" {
		query += `
		  AND type = ?`
		args = append(args, params.ObjectType)
	}

	// Wrap with a subquery to apply prefix filter and ordering
	outerQuery := `SELECT tag FROM (` + query + `) AS t`

	if params != nil && params.Prefix != "" {
		outerQuery += ` WHERE tag ILIKE ?`
		args = append(args, params.Prefix+"%")
	}

	outerQuery += ` ORDER BY tag`

	if params != nil && params.Limit > 0 {
		outerQuery += ` LIMIT ?`
		args = append(args, params.Limit)
	}

	err := r.db.NewRaw(outerQuery, args...).Scan(ctx, &tags)
	if err != nil {
		r.log.Error("failed to get distinct tags", logger.Error(err))
		return nil, apperror.ErrDatabase.WithInternal(err)
	}
	if tags == nil {
		return []string{}, nil
	}
	return tags, nil
}

// BulkUpdateStatus updates the status of multiple objects.
// Accepts either physical ids or canonical_ids.
func (r *Repository) BulkUpdateStatus(ctx context.Context, projectID uuid.UUID, ids []uuid.UUID, status string, actorID *uuid.UUID) (int, error) {
	if len(ids) == 0 {
		return 0, nil
	}

	now := time.Now()
	result, err := r.db.NewUpdate().
		Model((*GraphObject)(nil)).
		Set("status = ?", status).
		Set("updated_at = ?", now).
		Set("actor_id = ?", actorID).
		Where("(id IN (?) OR canonical_id IN (?))", bun.In(ids), bun.In(ids)).
		Where("project_id = ?", projectID).
		Where("supersedes_id IS NULL"). // Only update HEAD versions
		Where("deleted_at IS NULL").
		Exec(ctx)
	if err != nil {
		r.log.Error("failed to bulk update status", logger.Error(err))
		return 0, apperror.ErrDatabase.WithInternal(err)
	}

	rowsAffected, _ := result.RowsAffected()
	return int(rowsAffected), nil
}

// =============================================================================
// Similar Objects Search
// =============================================================================

// SimilarSearchParams contains parameters for finding similar objects.
type SimilarSearchParams struct {
	ProjectID   uuid.UUID
	ObjectID    uuid.UUID
	BranchID    *uuid.UUID
	Type        *string
	KeyPrefix   *string
	LabelsAll   []string
	LabelsAny   []string
	MaxDistance *float32
	Limit       int
}

// SimilarSearchResult represents a single similar object result.
type SimilarSearchResult struct {
	ID          uuid.UUID
	CanonicalID uuid.UUID
	Version     int
	Distance    float32
	ProjectID   uuid.UUID
	BranchID    *uuid.UUID
	Type        string
	Key         *string
	Status      string
	Properties  map[string]any
	Labels      []string
	CreatedAt   time.Time
}

// FindSimilarObjects finds objects similar to a given object using stored embeddings.
func (r *Repository) FindSimilarObjects(ctx context.Context, params SimilarSearchParams) ([]*SimilarSearchResult, error) {
	if params.Limit <= 0 {
		params.Limit = 10
	}
	if params.Limit > 100 {
		params.Limit = 100
	}

	// First get the embedding for the source object (accepts physical id or canonical_id)
	var embedding []float32
	err := r.db.NewRaw(`
		SELECT embedding_v2
		FROM kb.graph_objects
		WHERE (id = ? OR canonical_id = ?) AND project_id = ?
		AND supersedes_id IS NULL
		LIMIT 1
	`, params.ObjectID, params.ObjectID, params.ProjectID).Scan(ctx, &embedding)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, apperror.ErrNotFound
		}
		return nil, apperror.ErrDatabase.WithInternal(err)
	}

	if len(embedding) == 0 {
		// No embedding available for source object
		return []*SimilarSearchResult{}, nil
	}

	// Build query for similar objects
	vectorStr := pgutils.FormatVector(embedding)

	conditions := []string{
		"project_id = ?",
		"(id != ? AND canonical_id != ?)", // Exclude source object by either ID type
		"supersedes_id IS NULL",
		"deleted_at IS NULL",
		"embedding_v2 IS NOT NULL",
	}
	args := []any{params.ProjectID, params.ObjectID, params.ObjectID}

	if params.BranchID != nil {
		conditions = append(conditions, "branch_id = ?")
		args = append(args, *params.BranchID)
	} else {
		conditions = append(conditions, "branch_id IS NULL")
	}

	if params.Type != nil {
		conditions = append(conditions, "type = ?")
		args = append(args, *params.Type)
	}

	if params.KeyPrefix != nil {
		conditions = append(conditions, "key ILIKE ?")
		args = append(args, *params.KeyPrefix+"%")
	}

	if len(params.LabelsAll) > 0 {
		conditions = append(conditions, "labels @> ?::text[]")
		args = append(args, formatTextArray(params.LabelsAll))
	}

	if len(params.LabelsAny) > 0 {
		conditions = append(conditions, "labels && ?::text[]")
		args = append(args, formatTextArray(params.LabelsAny))
	}

	if params.MaxDistance != nil {
		conditions = append(conditions, "(embedding_v2 <=> ?::vector) <= ?")
		args = append(args, vectorStr, *params.MaxDistance)
	}

	whereClause := buildWhereClause(conditions)

	query := `
		SELECT id, canonical_id, version, project_id, branch_id,
			type, key, status, properties, labels, created_at,
			(embedding_v2 <=> ?::vector) AS distance
		FROM kb.graph_objects
		` + whereClause + `
		ORDER BY distance ASC
		LIMIT ?
	`

	finalArgs := append([]any{vectorStr}, args...)
	finalArgs = append(finalArgs, params.Limit)

	// Begin transaction with increased IVFFlat probes for better recall
	tx, err := r.beginTxWithIVFFlatProbes(ctx, 10)
	if err != nil {
		r.log.Error("similar objects search: failed to set ivfflat probes", logger.Error(err))
		return nil, err
	}
	defer func() { _ = tx.Rollback() }()

	rows, err := tx.QueryContext(ctx, query, finalArgs...)
	if err != nil {
		r.log.Error("similar objects search failed", logger.Error(err))
		return nil, apperror.ErrDatabase.WithInternal(err)
	}
	defer rows.Close()

	var results []*SimilarSearchResult
	for rows.Next() {
		result := &SimilarSearchResult{}
		err := rows.Scan(
			&result.ID, &result.CanonicalID, &result.Version, &result.ProjectID, &result.BranchID,
			&result.Type, &result.Key, &result.Status, &result.Properties, &result.Labels, &result.CreatedAt,
			&result.Distance,
		)
		if err != nil {
			return nil, apperror.ErrDatabase.WithInternal(err)
		}
		results = append(results, result)
	}

	if err := rows.Err(); err != nil {
		return nil, apperror.ErrDatabase.WithInternal(err)
	}

	// Commit the read-only transaction
	if err := tx.Commit(); err != nil {
		return nil, apperror.ErrDatabase.WithInternal(err)
	}

	return results, nil
}

// =============================================================================
// Graph Expand/Traverse Operations
// =============================================================================

// cosineSimilarity computes cosine similarity between two vectors.
// Returns the dot product divided by the product of magnitudes.
// For unit-normalized vectors (as produced by Vertex AI), this simplifies to dot(a, b).
// Returns 0.0 if either vector is empty or has zero magnitude.
func cosineSimilarity(a, b []float32) float32 {
	if len(a) == 0 || len(b) == 0 || len(a) != len(b) {
		return 0.0
	}

	var dot, magA, magB float64
	for i := range a {
		ai, bi := float64(a[i]), float64(b[i])
		dot += ai * bi
		magA += ai * ai
		magB += bi * bi
	}

	mag := math.Sqrt(magA * magB)
	if mag == 0 {
		return 0.0
	}

	return float32(dot / mag)
}

// ExpandParams contains parameters for graph expansion.
type ExpandParams struct {
	ProjectID         uuid.UUID
	RootIDs           []uuid.UUID
	Direction         string // "out", "in", "both"
	MaxDepth          int
	MaxNodes          int
	MaxEdges          int
	RelationshipTypes []string
	ObjectTypes       []string
	Labels            []string
	BranchID          *uuid.UUID
	QueryContext      string    // Optional query context for relevance-based edge ordering
	QueryVector       []float32 // Pre-computed embedding of QueryContext; if nil and QueryContext is set, service layer embeds it
}

// ExpandResult contains the raw results of graph expansion.
type ExpandResult struct {
	Nodes           map[uuid.UUID]*GraphObject
	Edges           []*GraphRelationship
	NodeDepths      map[uuid.UUID]int
	Truncated       bool
	MaxDepthReached int
}

// ExpandGraph performs BFS graph expansion from root nodes.
func (r *Repository) ExpandGraph(ctx context.Context, params ExpandParams) (*ExpandResult, error) {
	result := &ExpandResult{
		Nodes:      make(map[uuid.UUID]*GraphObject),
		Edges:      []*GraphRelationship{},
		NodeDepths: make(map[uuid.UUID]int),
	}

	// Initialize with root nodes at depth 0
	currentLevel := make([]uuid.UUID, 0, len(params.RootIDs))
	visited := make(map[uuid.UUID]bool)

	// Fetch root objects — accept physical id or canonical_id
	var rootObjects []*GraphObject
	err := r.db.NewSelect().
		Model(&rootObjects).
		Where("(id IN (?) OR canonical_id IN (?))", bun.In(params.RootIDs), bun.In(params.RootIDs)).
		Where("project_id = ?", params.ProjectID).
		Where("supersedes_id IS NULL").
		Where("deleted_at IS NULL").
		Scan(ctx)
	if err != nil && err != sql.ErrNoRows {
		return nil, apperror.ErrDatabase.WithInternal(err)
	}

	// Use canonical_id for traversal since relationships store canonical IDs in src_id/dst_id
	for _, obj := range rootObjects {
		result.Nodes[obj.CanonicalID] = obj
		result.NodeDepths[obj.CanonicalID] = 0
		visited[obj.CanonicalID] = true
		currentLevel = append(currentLevel, obj.CanonicalID)
	}

	// BFS traversal
	for depth := 0; depth < params.MaxDepth && len(currentLevel) > 0; depth++ {
		// Check limits
		if len(result.Nodes) >= params.MaxNodes {
			result.Truncated = true
			break
		}
		if len(result.Edges) >= params.MaxEdges {
			result.Truncated = true
			break
		}

		result.MaxDepthReached = depth + 1
		nextLevel := []uuid.UUID{}

		// Fetch relationships for current level nodes
		var relationships []*GraphRelationship

		// Build direction condition
		var directionCond string
		switch params.Direction {
		case "out":
			directionCond = "src_id IN (?)"
		case "in":
			directionCond = "dst_id IN (?)"
		default: // "both"
			directionCond = "(src_id IN (?) OR dst_id IN (?))"
		}

		q := r.db.NewSelect().
			Model(&relationships).
			Where("project_id = ?", params.ProjectID).
			Where("supersedes_id IS NULL").
			Where("deleted_at IS NULL")

		if params.BranchID != nil {
			q = q.Where("branch_id = ?", *params.BranchID)
		} else {
			q = q.Where("branch_id IS NULL")
		}

		switch params.Direction {
		case "out":
			q = q.Where(directionCond, bun.In(currentLevel))
		case "in":
			q = q.Where(directionCond, bun.In(currentLevel))
		default:
			q = q.Where(directionCond, bun.In(currentLevel), bun.In(currentLevel))
		}

		if len(params.RelationshipTypes) > 0 {
			q = q.Where("type IN (?)", bun.In(params.RelationshipTypes))
		}

		err := q.Scan(ctx)
		if err != nil && err != sql.ErrNoRows {
			return nil, apperror.ErrDatabase.WithInternal(err)
		}

		// Sort edges by cosine similarity to query vector when provided.
		// This ensures that when MaxEdges/MaxNodes limits cause truncation,
		// the most query-relevant edges survive.
		if len(params.QueryVector) > 0 && len(relationships) > 1 {
			// Fetch similarity scores from the database for relationships that have embeddings
			relIDs := make([]uuid.UUID, len(relationships))
			for i, rel := range relationships {
				relIDs[i] = rel.ID
			}

			type similarityResult struct {
				ID         uuid.UUID `bun:"id"`
				Similarity float64   `bun:"similarity"`
			}

			var similarities []similarityResult
			vecStr := vectorToString(params.QueryVector)
			simErr := r.db.NewRaw(
				"SELECT id, (1 - (embedding <=> ?::vector)) AS similarity FROM kb.graph_relationships WHERE id IN (?) AND embedding IS NOT NULL",
				vecStr, bun.In(relIDs),
			).Scan(ctx, &similarities)

			if simErr == nil && len(similarities) > 0 {
				// Build similarity map (relationships without embeddings get 0.0)
				simMap := make(map[uuid.UUID]float64, len(similarities))
				for _, s := range similarities {
					simMap[s.ID] = s.Similarity
				}

				sort.SliceStable(relationships, func(i, j int) bool {
					return simMap[relationships[i].ID] > simMap[relationships[j].ID]
				})
			}
			// If similarity query fails, fall through to standard BFS order (graceful degradation)
		}

		// Collect neighbor IDs
		neighborIDs := make(map[uuid.UUID]bool)
		for _, rel := range relationships {
			// Check edge limit
			if len(result.Edges) >= params.MaxEdges {
				result.Truncated = true
				break
			}

			result.Edges = append(result.Edges, rel)

			// Determine the neighbor ID based on direction
			var neighborID uuid.UUID
			if visited[rel.SrcID] && !visited[rel.DstID] {
				neighborID = rel.DstID
			} else if visited[rel.DstID] && !visited[rel.SrcID] {
				neighborID = rel.SrcID
			} else {
				continue // Both already visited or edge within current frontier
			}

			if !neighborIDs[neighborID] {
				neighborIDs[neighborID] = true
			}
		}

		if result.Truncated {
			break
		}

		// Fetch neighbor objects — neighborIDs are canonical_id values
		if len(neighborIDs) > 0 {
			neighborIDList := make([]uuid.UUID, 0, len(neighborIDs))
			for id := range neighborIDs {
				neighborIDList = append(neighborIDList, id)
			}

			var neighbors []*GraphObject
			nq := r.db.NewSelect().
				Model(&neighbors).
				Where("canonical_id IN (?)", bun.In(neighborIDList)).
				Where("project_id = ?", params.ProjectID).
				Where("supersedes_id IS NULL").
				Where("deleted_at IS NULL")

			if len(params.ObjectTypes) > 0 {
				nq = nq.Where("type IN (?)", bun.In(params.ObjectTypes))
			}

			if len(params.Labels) > 0 {
				nq = nq.Where("labels && ?::text[]", formatTextArray(params.Labels))
			}

			err := nq.Scan(ctx)
			if err != nil && err != sql.ErrNoRows {
				return nil, apperror.ErrDatabase.WithInternal(err)
			}

			for _, obj := range neighbors {
				if !visited[obj.CanonicalID] {
					// Check node limit
					if len(result.Nodes) >= params.MaxNodes {
						result.Truncated = true
						break
					}

					result.Nodes[obj.CanonicalID] = obj
					result.NodeDepths[obj.CanonicalID] = depth + 1
					visited[obj.CanonicalID] = true
					nextLevel = append(nextLevel, obj.CanonicalID)
				}
			}
		}

		currentLevel = nextLevel
	}

	return result, nil
}

// GetNeighborObjects returns objects connected to the given object via relationships.
// This is used by search-with-neighbors to find relationship-connected neighbors.
// objectID can be a physical id or canonical_id; it is used to match against
// src_id/dst_id in relationships, which store canonical_id values.
func (r *Repository) GetNeighborObjects(ctx context.Context, projectID uuid.UUID, objectID uuid.UUID, branchID *uuid.UUID, maxNeighbors int) ([]*GraphObject, error) {
	// Resolve objectID to canonical_id by looking up the object
	var resolvedObj GraphObject
	err := r.db.NewSelect().
		Model(&resolvedObj).
		Column("canonical_id").
		Where("(id = ? OR canonical_id = ?)", objectID, objectID).
		Where("project_id = ?", projectID).
		Where("supersedes_id IS NULL").
		Limit(1).
		Scan(ctx)
	if err != nil {
		if err == sql.ErrNoRows {
			return []*GraphObject{}, nil
		}
		return nil, apperror.ErrDatabase.WithInternal(err)
	}
	canonicalID := resolvedObj.CanonicalID

	// Get all connected object IDs via relationships (src_id/dst_id store canonical IDs)
	var neighborIDs []uuid.UUID

	// Get outgoing relationships
	var outgoing []uuid.UUID
	outQ := r.db.NewSelect().
		Column("dst_id").
		Model((*GraphRelationship)(nil)).
		Where("src_id = ?", canonicalID).
		Where("project_id = ?", projectID).
		Where("supersedes_id IS NULL").
		Where("deleted_at IS NULL")

	if branchID != nil {
		outQ = outQ.Where("branch_id = ?", *branchID)
	} else {
		outQ = outQ.Where("branch_id IS NULL")
	}

	err = outQ.Scan(ctx, &outgoing)
	if err != nil && err != sql.ErrNoRows {
		return nil, apperror.ErrDatabase.WithInternal(err)
	}
	neighborIDs = append(neighborIDs, outgoing...)

	// Get incoming relationships
	var incoming []uuid.UUID
	inQ := r.db.NewSelect().
		Column("src_id").
		Model((*GraphRelationship)(nil)).
		Where("dst_id = ?", canonicalID).
		Where("project_id = ?", projectID).
		Where("supersedes_id IS NULL").
		Where("deleted_at IS NULL")

	if branchID != nil {
		inQ = inQ.Where("branch_id = ?", *branchID)
	} else {
		inQ = inQ.Where("branch_id IS NULL")
	}

	err = inQ.Scan(ctx, &incoming)
	if err != nil && err != sql.ErrNoRows {
		return nil, apperror.ErrDatabase.WithInternal(err)
	}
	neighborIDs = append(neighborIDs, incoming...)

	if len(neighborIDs) == 0 {
		return []*GraphObject{}, nil
	}

	// Dedupe (neighborIDs are canonical_id values)
	seen := make(map[uuid.UUID]bool)
	unique := make([]uuid.UUID, 0)
	for _, id := range neighborIDs {
		if !seen[id] && id != canonicalID {
			seen[id] = true
			unique = append(unique, id)
		}
	}

	if len(unique) == 0 {
		return []*GraphObject{}, nil
	}

	// Limit
	if maxNeighbors > 0 && len(unique) > maxNeighbors {
		unique = unique[:maxNeighbors]
	}

	// Fetch HEAD objects by canonical_id
	var objects []*GraphObject
	err = r.db.NewSelect().
		Model(&objects).
		Where("canonical_id IN (?)", bun.In(unique)).
		Where("project_id = ?", projectID).
		Where("supersedes_id IS NULL").
		Where("deleted_at IS NULL").
		Scan(ctx)
	if err != nil && err != sql.ErrNoRows {
		return nil, apperror.ErrDatabase.WithInternal(err)
	}

	return objects, nil
}

// GetObjectEmbedding returns the embedding vector for an object.
// Accepts either physical id or canonical_id, returns the HEAD version's embedding.
func (r *Repository) GetObjectEmbedding(ctx context.Context, projectID, objectID uuid.UUID) ([]float32, error) {
	var embedding []float32
	err := r.db.NewRaw(`
		SELECT embedding_v2
		FROM kb.graph_objects
		WHERE (id = ? OR canonical_id = ?) AND project_id = ?
		AND supersedes_id IS NULL
		LIMIT 1
	`, objectID, objectID, projectID).Scan(ctx, &embedding)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, apperror.ErrNotFound
		}
		return nil, apperror.ErrDatabase.WithInternal(err)
	}
	return embedding, nil
}

// =============================================================================
// Branch Operations
// =============================================================================

// GetBranchByID returns a branch by its ID.
func (r *Repository) GetBranchByID(ctx context.Context, projectID, branchID uuid.UUID) (*Branch, error) {
	var branch Branch
	err := r.db.NewSelect().
		Model(&branch).
		Where("id = ?", branchID).
		Where("project_id = ?", projectID).
		Scan(ctx)

	if err != nil {
		if err == sql.ErrNoRows {
			return nil, apperror.ErrNotFound
		}
		return nil, apperror.ErrDatabase.WithInternal(err)
	}

	return &branch, nil
}

// IsAncestorBranch checks if ancestorBranchID is an ancestor of branchID.
func (r *Repository) IsAncestorBranch(ctx context.Context, branchID, ancestorBranchID uuid.UUID) (bool, error) {
	count, err := r.db.NewSelect().
		Model((*BranchLineage)(nil)).
		Where("branch_id = ?", branchID).
		Where("ancestor_branch_id = ?", ancestorBranchID).
		Count(ctx)

	if err != nil {
		return false, apperror.ErrDatabase.WithInternal(err)
	}

	return count > 0, nil
}

// BranchObjectHead represents HEAD object version per canonical_id on a branch.
type BranchObjectHead struct {
	CanonicalID uuid.UUID
	ID          uuid.UUID
	ContentHash []byte
	Properties  map[string]any
}

// GetBranchObjectHeads returns HEAD versions of all objects on a branch.
func (r *Repository) GetBranchObjectHeads(ctx context.Context, projectID uuid.UUID, branchID *uuid.UUID) (map[uuid.UUID]*BranchObjectHead, error) {
	var objects []*GraphObject

	q := r.db.NewSelect().
		Model(&objects).
		Column("id", "canonical_id", "content_hash", "properties").
		Where("project_id = ?", projectID).
		Where("supersedes_id IS NULL").
		Where("deleted_at IS NULL")

	if branchID != nil {
		q = q.Where("branch_id = ?", *branchID)
	} else {
		q = q.Where("branch_id IS NULL")
	}

	err := q.Scan(ctx)
	if err != nil && err != sql.ErrNoRows {
		return nil, apperror.ErrDatabase.WithInternal(err)
	}

	result := make(map[uuid.UUID]*BranchObjectHead)
	for _, obj := range objects {
		result[obj.CanonicalID] = &BranchObjectHead{
			CanonicalID: obj.CanonicalID,
			ID:          obj.ID,
			ContentHash: obj.ContentHash,
			Properties:  obj.Properties,
		}
	}

	return result, nil
}

// BranchRelationshipHead represents HEAD relationship version per canonical_id on a branch.
type BranchRelationshipHead struct {
	CanonicalID uuid.UUID
	ID          uuid.UUID
	ContentHash []byte
	Properties  map[string]any
	SrcID       uuid.UUID
	DstID       uuid.UUID
}

// GetBranchRelationshipHeads returns HEAD versions of all relationships on a branch.
func (r *Repository) GetBranchRelationshipHeads(ctx context.Context, projectID uuid.UUID, branchID *uuid.UUID) (map[uuid.UUID]*BranchRelationshipHead, error) {
	var rels []*GraphRelationship

	q := r.db.NewSelect().
		Model(&rels).
		Column("id", "canonical_id", "content_hash", "properties", "src_id", "dst_id").
		Where("project_id = ?", projectID).
		Where("supersedes_id IS NULL").
		Where("deleted_at IS NULL")

	if branchID != nil {
		q = q.Where("branch_id = ?", *branchID)
	} else {
		q = q.Where("branch_id IS NULL")
	}

	err := q.Scan(ctx)
	if err != nil && err != sql.ErrNoRows {
		return nil, apperror.ErrDatabase.WithInternal(err)
	}

	result := make(map[uuid.UUID]*BranchRelationshipHead)
	for _, rel := range rels {
		result[rel.CanonicalID] = &BranchRelationshipHead{
			CanonicalID: rel.CanonicalID,
			ID:          rel.ID,
			ContentHash: rel.ContentHash,
			Properties:  rel.Properties,
			SrcID:       rel.SrcID,
			DstID:       rel.DstID,
		}
	}

	return result, nil
}
