package documents

import (
	"context"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	"github.com/uptrace/bun"

	"github.com/emergent-company/emergent.memory/pkg/apperror"
	"github.com/emergent-company/emergent.memory/pkg/logger"
	"github.com/emergent-company/emergent.memory/pkg/pgutils"
)

// Repository handles document database operations
type Repository struct {
	db  bun.IDB
	log *slog.Logger
}

// NewRepository creates a new documents repository
func NewRepository(db bun.IDB, log *slog.Logger) *Repository {
	return &Repository{
		db:  db,
		log: log.With(logger.Scope("documents-repo")),
	}
}

// List retrieves documents with pagination and filtering
func (r *Repository) List(ctx context.Context, params ListParams) (*ListResult, error) {
	// Default limit
	if params.Limit <= 0 {
		params.Limit = 100
	}
	if params.Limit > 500 {
		params.Limit = 500
	}

	// Build base query with RLS context via project_id
	// Use explicit column selection to include computed fields via subqueries
	query := r.db.NewSelect().
		TableExpr("kb.documents AS d").
		ColumnExpr("d.*").
		ColumnExpr("COALESCE(LENGTH(d.content), 0) AS total_chars").
		ColumnExpr("(SELECT COUNT(*)::int FROM kb.chunks c WHERE c.document_id = d.id) AS chunks").
		ColumnExpr("(SELECT COUNT(*)::int FROM kb.chunks c WHERE c.document_id = d.id AND c.embedding IS NOT NULL) AS embedded_chunks").
		ColumnExpr("(SELECT ej.status FROM kb.object_extraction_jobs ej WHERE ej.document_id = d.id ORDER BY ej.created_at DESC LIMIT 1) AS extraction_status").
		ColumnExpr(processingStatusSQL()).
		ColumnExpr(lastExtractionAtSQL()).
		ColumnExpr(extractionObjectsCreatedSQL()).
		ColumnExpr(extractionRelationshipsCreatedSQL()).
		Where("d.project_id = ?", params.ProjectID)

	// Apply filters
	if params.SourceType != nil {
		query = query.Where("d.source_type = ?", *params.SourceType)
	}
	if params.RootOnly {
		query = query.Where("d.parent_document_id IS NULL")
	}
	if params.ParentDocumentID != nil {
		query = query.Where("d.parent_document_id = ?", *params.ParentDocumentID)
	}

	// Apply cursor-based pagination
	if params.Cursor != nil {
		query = query.Where("(d.created_at, d.id) < (?, ?)", params.Cursor.CreatedAt, params.Cursor.ID)
	}

	// Get total count (without pagination)
	countQuery := r.db.NewSelect().
		Model((*Document)(nil)).
		Where("project_id = ?", params.ProjectID)

	if params.SourceType != nil {
		countQuery = countQuery.Where("source_type = ?", *params.SourceType)
	}
	if params.RootOnly {
		countQuery = countQuery.Where("parent_document_id IS NULL")
	}
	if params.ParentDocumentID != nil {
		countQuery = countQuery.Where("parent_document_id = ?", *params.ParentDocumentID)
	}

	total, err := countQuery.Count(ctx)
	if err != nil {
		return nil, fmt.Errorf("count documents: %w", err)
	}

	// Order by created_at DESC, id DESC for cursor pagination
	query = query.Order("d.created_at DESC", "d.id DESC").
		Limit(params.Limit + 1) // +1 to detect if there are more

	documents := []Document{}
	if err := query.Scan(ctx, &documents); err != nil {
		return nil, fmt.Errorf("list documents: %w", err)
	}

	// Check if there are more results
	var nextCursor *string
	if len(documents) > params.Limit {
		// There are more results - create cursor from last item we're returning
		documents = documents[:params.Limit]
		lastDoc := documents[len(documents)-1]
		cursor := Cursor{
			CreatedAt: lastDoc.CreatedAt,
			ID:        lastDoc.ID,
		}
		cursorJSON, _ := json.Marshal(cursor)
		encoded := base64.URLEncoding.EncodeToString(cursorJSON)
		nextCursor = &encoded
	}

	return &ListResult{
		Documents:  documents,
		Total:      total,
		NextCursor: nextCursor,
	}, nil
}

// GetByID retrieves a single document by ID
func (r *Repository) GetByID(ctx context.Context, projectID, documentID string) (*Document, error) {
	var doc Document
	err := r.db.NewSelect().
		TableExpr("kb.documents AS d").
		ColumnExpr("d.*").
		ColumnExpr("COALESCE(LENGTH(d.content), 0) AS total_chars").
		ColumnExpr("(SELECT COUNT(*)::int FROM kb.chunks c WHERE c.document_id = d.id) AS chunks").
		ColumnExpr("(SELECT COUNT(*)::int FROM kb.chunks c WHERE c.document_id = d.id AND c.embedding IS NOT NULL) AS embedded_chunks").
		ColumnExpr("(SELECT ej.status FROM kb.object_extraction_jobs ej WHERE ej.document_id = d.id ORDER BY ej.created_at DESC LIMIT 1) AS extraction_status").
		ColumnExpr(processingStatusSQL()).
		ColumnExpr(lastExtractionAtSQL()).
		ColumnExpr(extractionObjectsCreatedSQL()).
		ColumnExpr(extractionRelationshipsCreatedSQL()).
		Where("d.id = ?", documentID).
		Where("d.project_id = ?", projectID).
		Scan(ctx, &doc)

	if err != nil {
		if err == sql.ErrNoRows {
			return nil, nil // Return nil, nil for not found (let caller decide error)
		}
		return nil, fmt.Errorf("get document: %w", err)
	}

	// Populate job_ids: all extraction job IDs for this document
	var jobIDs []string
	err = r.db.NewSelect().
		TableExpr("kb.object_extraction_jobs").
		ColumnExpr("id").
		Where("document_id = ?", documentID).
		Where("project_id = ?", projectID).
		OrderExpr("created_at DESC").
		Scan(ctx, &jobIDs)
	if err != nil && err != sql.ErrNoRows {
		// Non-fatal: log and continue without job_ids
		jobIDs = nil
	}
	doc.JobIDs = jobIDs

	return &doc, nil
}

// GetContentByID retrieves just the document content (and basic fields) by ID.
// Uses Model() to avoid computed-column scan issues with Bun.
func (r *Repository) GetContentByID(ctx context.Context, projectID, documentID string) (*Document, error) {
	var doc Document
	err := r.db.NewSelect().
		Model(&doc).
		Where("id = ?", documentID).
		Where("project_id = ?", projectID).
		Scan(ctx)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, fmt.Errorf("get document: %w", err)
	}
	return &doc, nil
}

// GetByContentHash retrieves a document by content hash (for deduplication)
func (r *Repository) GetByContentHash(ctx context.Context, projectID, contentHash string) (*Document, error) {
	var doc Document
	err := r.db.NewSelect().
		Model(&doc).
		Where("project_id = ?", projectID).
		Where("content_hash = ?", contentHash).
		Scan(ctx)

	if err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, fmt.Errorf("get document by content hash: %w", err)
	}

	return &doc, nil
}

// GetDistinctSourceTypes returns all distinct source types with document counts for a project
func (r *Repository) GetDistinctSourceTypes(ctx context.Context, projectID string) ([]SourceTypeWithCount, error) {
	var results []SourceTypeWithCount
	err := r.db.NewSelect().
		TableExpr("kb.documents").
		ColumnExpr("source_type, COUNT(*)::int as count").
		Where("project_id = ?", projectID).
		Where("source_type IS NOT NULL").
		GroupExpr("source_type").
		OrderExpr("count DESC").
		Scan(ctx, &results)

	if err != nil {
		r.log.Error("failed to get distinct source types", logger.Error(err))
		return nil, apperror.ErrDatabase.WithInternal(err)
	}

	if results == nil {
		results = []SourceTypeWithCount{}
	}

	return results, nil
}

// Create creates a new document in the database
func (r *Repository) Create(ctx context.Context, doc *Document) error {
	_, err := r.db.NewInsert().
		Model(doc).
		Returning("*").
		Exec(ctx)

	if err != nil {
		if pgutils.IsUniqueViolation(err) {
			// Content hash duplicate - let service handle this
			return apperror.New(409, "duplicate", "Document with this content already exists")
		}
		if pgutils.IsForeignKeyViolation(err) {
			return apperror.New(400, "invalid-project", "Project not found")
		}
		r.log.Error("failed to create document", logger.Error(err))
		return apperror.ErrDatabase.WithInternal(err)
	}

	return nil
}

// Delete permanently deletes a document by ID
// Returns true if a document was deleted, false if not found
func (r *Repository) Delete(ctx context.Context, projectID, documentID string) (bool, error) {
	result, err := r.db.NewDelete().
		Model((*Document)(nil)).
		Where("id = ?", documentID).
		Where("project_id = ?", projectID).
		Exec(ctx)

	if err != nil {
		r.log.Error("failed to delete document", logger.Error(err), slog.String("id", documentID))
		return false, apperror.ErrDatabase.WithInternal(err)
	}

	rowsAffected, _ := result.RowsAffected()
	return rowsAffected > 0, nil
}

// graphVersionRow is a single physical version of a graph object. Graph objects
// are versioned: each physical `id` row shares a stable `canonical_id` with the
// other versions of the same entity.
type graphVersionRow struct {
	ID              string  `bun:"id"`
	CanonicalID     string  `bun:"canonical_id"`
	ExtractionJobID *string `bun:"extraction_job_id"`
}

// resolveGraphRemovals decides which graph object rows and canonical entities
// are removed when the given extraction jobs are deleted.
//
// Returns:
//
//	objectRowIDs  - physical kb.graph_objects.id rows to delete (all rows of
//	                wholly-removed canonicals, plus attributable rows of
//	                entities that survive)
//	canonicalIDs  - canonical ids of entities fully removed (used to delete
//	                relationships whose src_id/dst_id reference them)
func resolveGraphRemovals(rows []graphVersionRow, jobIDs []string) (objectRowIDs, canonicalIDs []string) {
	jobSet := make(map[string]bool, len(jobIDs))
	for _, id := range jobIDs {
		jobSet[id] = true
	}

	// Group rows by canonical entity.
	canonicalRows := make(map[string][]graphVersionRow)
	for _, row := range rows {
		canonicalRows[row.CanonicalID] = append(canonicalRows[row.CanonicalID], row)
	}

	objectSet := make(map[string]bool)
	canonicalSet := make(map[string]bool)

	for canonicalID, group := range canonicalRows {
		// A canonical is fully removed iff every one of its rows is
		// attributable to a deleted job (non-nil ExtractionJobID in jobSet).
		fullyRemoved := true
		for _, row := range group {
			if row.ExtractionJobID == nil || !jobSet[*row.ExtractionJobID] {
				fullyRemoved = false
			}
		}

		if fullyRemoved {
			canonicalSet[canonicalID] = true
		}

		for _, row := range group {
			attributable := row.ExtractionJobID != nil && jobSet[*row.ExtractionJobID]
			if fullyRemoved || attributable {
				objectSet[row.ID] = true
			}
		}
	}

	objectRowIDs = make([]string, 0, len(objectSet))
	for id := range objectSet {
		objectRowIDs = append(objectRowIDs, id)
	}
	canonicalIDs = make([]string, 0, len(canonicalSet))
	for id := range canonicalSet {
		canonicalIDs = append(canonicalIDs, id)
	}

	return objectRowIDs, canonicalIDs
}

// loadGraphVersionRows loads all version rows for the canonical entities that
// are attributable to the given extraction jobs. It bounds its work via
// attributable canonicals so that only the affected entities' version rows are
// loaded. Returns an empty slice (not an error) when jobIDs is empty or no
// canonicals are attributable.
func (r *Repository) loadGraphVersionRows(ctx context.Context, db bun.IDB, projectID string, jobIDs []string) ([]graphVersionRow, error) {
	if len(jobIDs) == 0 {
		return nil, nil
	}

	// 1. Attributable canonical ids, to bound the second query.
	var attrCanon []string
	err := db.NewSelect().
		TableExpr("kb.graph_objects").
		Column("canonical_id").
		Distinct().
		Where("extraction_job_id IN (?)", bun.In(jobIDs)).
		Where("project_id = ?", projectID).
		Scan(ctx, &attrCanon)
	if err != nil && err != sql.ErrNoRows {
		return nil, fmt.Errorf("resolve graph canonicals: %w", err)
	}

	if len(attrCanon) == 0 {
		return nil, nil
	}

	// 2. Load all version rows for the attributable canonicals.
	var rows []graphVersionRow
	err = db.NewSelect().
		TableExpr("kb.graph_objects").
		Column("id", "canonical_id", "extraction_job_id").
		Where("canonical_id IN (?)", bun.In(attrCanon)).
		Where("project_id = ?", projectID).
		OrderExpr("canonical_id").
		Scan(ctx, &rows)
	if err != nil && err != sql.ErrNoRows {
		return nil, fmt.Errorf("load graph versions: %w", err)
	}

	return rows, nil
}

// resolveGraphCascade resolves which graph object rows and canonical entities
// are removed by deleting the given extraction jobs, without touching the DB.
func (r *Repository) resolveGraphCascade(ctx context.Context, db bun.IDB, projectID string, jobIDs []string) (objectRowIDs, canonicalIDs []string, err error) {
	rows, err := r.loadGraphVersionRows(ctx, db, projectID, jobIDs)
	if err != nil {
		return nil, nil, err
	}
	if len(rows) == 0 {
		return nil, nil, nil
	}

	objectRowIDs, canonicalIDs = resolveGraphRemovals(rows, jobIDs)
	return objectRowIDs, canonicalIDs, nil
}

// relEndpoint is a single relationship row's endpoint canonical ids.
type relEndpoint struct {
	SrcID string `bun:"src_id"`
	DstID string `bun:"dst_id"`
}

// countRelationshipsTouching returns how many relationship rows have at least
// one endpoint in the given canonical set. A relationship is counted once even
// when both endpoints are in the set.
func countRelationshipsTouching(endpoints []relEndpoint, canonicalSet map[string]bool) int {
	count := 0
	for _, e := range endpoints {
		if canonicalSet[e.SrcID] || canonicalSet[e.DstID] {
			count++
		}
	}
	return count
}

// survivingVersionRow is a version row of a surviving canonical entity, used to
// repair its version chain after some of its versions were deleted.
type survivingVersionRow struct {
	ID           string  `bun:"id"`
	CanonicalID  string  `bun:"canonical_id"`
	SupersedesID *string `bun:"supersedes_id"`
	Version      int     `bun:"version"`
}

// survivingCanonicalsForDeletion returns the canonical ids of entities that
// survive deletion: the distinct canonical ids of the rows being deleted, minus
// those canonical ids that are fully removed. These are the entities whose
// version chains must be repaired after their attributable rows are gone.
func (r *Repository) survivingCanonicalsForDeletion(ctx context.Context, db bun.IDB, projectID string, objectRowIDs []string, fullyRemoved []string) ([]string, error) {
	if len(objectRowIDs) == 0 {
		return nil, nil
	}

	var deletedCanonicals []string
	err := db.NewSelect().
		TableExpr("kb.graph_objects").
		Column("canonical_id").
		Distinct().
		Where("id IN (?)", bun.In(objectRowIDs)).
		Where("project_id = ?", projectID).
		Scan(ctx, &deletedCanonicals)
	if err != nil && err != sql.ErrNoRows {
		return nil, fmt.Errorf("load deleted row canonicals: %w", err)
	}

	removedSet := make(map[string]bool, len(fullyRemoved))
	for _, c := range fullyRemoved {
		removedSet[c] = true
	}

	surviving := make([]string, 0, len(deletedCanonicals))
	for _, c := range deletedCanonicals {
		if removedSet[c] {
			continue
		}
		surviving = append(surviving, c)
	}
	return surviving, nil
}

// repairSurvivingVersionChains keeps surviving canonical entities queryable after
// some of their version rows were removed. Any surviving row left pointing at a
// deleted row's id has its supersedes_id cleared, and each surviving canonical is
// guaranteed exactly one HEAD row (supersedes_id IS NULL) — the highest-version
// survivor — so the entity does not vanish from HEAD queries.
func (r *Repository) repairSurvivingVersionChains(ctx context.Context, db bun.IDB, projectID string, survivingCanonicalIDs []string, deletedRowIDs []string) error {
	if len(survivingCanonicalIDs) == 0 {
		return nil
	}

	// 1. Clear supersedes_id on surviving rows that point at a deleted row.
	if len(deletedRowIDs) > 0 {
		_, err := db.NewUpdate().
			TableExpr("kb.graph_objects").
			Set("supersedes_id = NULL").
			Where("project_id = ?", projectID).
			Where("canonical_id IN (?)", bun.In(survivingCanonicalIDs)).
			Where("supersedes_id IN (?)", bun.In(deletedRowIDs)).
			Exec(ctx)
		if err != nil {
			return fmt.Errorf("clear dangling supersedes_id: %w", err)
		}
	}

	// 2. Load the remaining rows for the surviving canonicals.
	var rows []survivingVersionRow
	err := db.NewSelect().
		TableExpr("kb.graph_objects").
		Column("id", "canonical_id", "supersedes_id", "version").
		Where("project_id = ?", projectID).
		Where("canonical_id IN (?)", bun.In(survivingCanonicalIDs)).
		OrderExpr("canonical_id", "version DESC").
		Scan(ctx, &rows)
	if err != nil && err != sql.ErrNoRows {
		return fmt.Errorf("load surviving versions: %w", err)
	}

	// 3. For each surviving canonical with no HEAD, promote the highest-version
	//    row (first in version DESC order) to HEAD by clearing supersedes_id.
	type headState struct {
		hasHead bool
		headID  string
	}
	groups := make(map[string]*headState)
	for _, row := range rows {
		g := groups[row.CanonicalID]
		if g == nil {
			g = &headState{}
			groups[row.CanonicalID] = g
		}
		if row.SupersedesID == nil {
			g.hasHead = true
		}
		if g.headID == "" {
			g.headID = row.ID
		}
	}

	for canonicalID, g := range groups {
		if g.hasHead {
			continue
		}
		_, err := db.NewUpdate().
			TableExpr("kb.graph_objects").
			Set("supersedes_id = NULL").
			Where("id = ?", g.headID).
			Exec(ctx)
		if err != nil {
			return fmt.Errorf("promote head for canonical %s: %w", canonicalID, err)
		}
	}

	return nil
}

// DeleteWithCascade deletes a document and all related entities in a transaction
// Returns a summary of what was deleted
func (r *Repository) DeleteWithCascade(ctx context.Context, projectID, documentID string) (*DeleteSummary, error) {
	summary := &DeleteSummary{}

	err := r.db.RunInTx(ctx, nil, func(ctx context.Context, tx bun.Tx) error {
		// 1. Delete notifications related to this document
		result, err := tx.NewDelete().
			TableExpr("kb.notifications").
			Where("related_resource_type = ?", "document").
			Where("related_resource_id = ?", documentID).
			Exec(ctx)
		if err != nil {
			return fmt.Errorf("delete notifications: %w", err)
		}
		if n, _ := result.RowsAffected(); n > 0 {
			summary.Notifications = int(n)
		}

		// 2. Get extraction job IDs for this document
		var jobIDs []string
		err = tx.NewSelect().
			TableExpr("kb.object_extraction_jobs").
			Column("id").
			Where("document_id = ?", documentID).
			Scan(ctx, &jobIDs)
		if err != nil && err != sql.ErrNoRows {
			return fmt.Errorf("get extraction jobs: %w", err)
		}

		if len(jobIDs) > 0 {
			// 3-5. Resolve graph object rows and canonical entities to remove.
			objectRowIDs, canonicalIDs, err := r.resolveGraphCascade(ctx, tx, projectID, jobIDs)
			if err != nil {
				return err
			}

			// Determine which canonical entities survive (attributable rows are
			// deleted, but the entity itself is not fully removed). Their version
			// chains must be repaired after the rows are gone.
			survivingCanonicals, err := r.survivingCanonicalsForDeletion(ctx, tx, projectID, objectRowIDs, canonicalIDs)
			if err != nil {
				return err
			}

			// Delete relationships referencing fully-removed canonicals FIRST
			// (they must go before their endpoint objects).
			if len(canonicalIDs) > 0 {
				result, err = tx.NewDelete().
					TableExpr("kb.graph_relationships").
					Where("src_id IN (?) OR dst_id IN (?)", bun.In(canonicalIDs), bun.In(canonicalIDs)).
					Exec(ctx)
				if err != nil {
					return fmt.Errorf("delete relationships: %w", err)
				}
				if n, _ := result.RowsAffected(); n > 0 {
					summary.GraphRelationships = int(n)
				}
			}

			// Delete graph object rows.
			if len(objectRowIDs) > 0 {
				result, err = tx.NewDelete().
					TableExpr("kb.graph_objects").
					Where("id IN (?) AND project_id = ?", bun.In(objectRowIDs), projectID).
					Exec(ctx)
				if err != nil {
					return fmt.Errorf("delete graph objects: %w", err)
				}
				if n, _ := result.RowsAffected(); n > 0 {
					summary.GraphObjects = int(n)
				}
			}

			// Repair version chains for surviving canonicals so they keep a
			// single HEAD and never point at the deleted rows.
			if err := r.repairSurvivingVersionChains(ctx, tx, projectID, survivingCanonicals, objectRowIDs); err != nil {
				return err
			}

			// 6. Delete extraction jobs
			result, err = tx.NewDelete().
				TableExpr("kb.object_extraction_jobs").
				Where("id IN (?)", bun.In(jobIDs)).
				Exec(ctx)
			if err != nil {
				return fmt.Errorf("delete extraction jobs: %w", err)
			}
			if n, _ := result.RowsAffected(); n > 0 {
				summary.ExtractionJobs = int(n)
			}
		}

		// 7. Delete chunks (should cascade via FK, but explicit for count)
		result, err = tx.NewDelete().
			TableExpr("kb.chunks").
			Where("document_id = ?", documentID).
			Exec(ctx)
		if err != nil {
			return fmt.Errorf("delete chunks: %w", err)
		}
		if n, _ := result.RowsAffected(); n > 0 {
			summary.Chunks = int(n)
		}

		// 8. Delete the document itself
		result, err = tx.NewDelete().
			Model((*Document)(nil)).
			Where("id = ?", documentID).
			Where("project_id = ?", projectID).
			Exec(ctx)
		if err != nil {
			return fmt.Errorf("delete document: %w", err)
		}

		rowsAffected, _ := result.RowsAffected()
		if rowsAffected == 0 {
			return apperror.ErrNotFound.WithMessage("Document not found")
		}

		return nil
	})

	if err != nil {
		if appErr, ok := err.(*apperror.Error); ok {
			return nil, appErr
		}
		r.log.Error("failed to delete document with cascade", logger.Error(err), slog.String("id", documentID))
		return nil, apperror.ErrDatabase.WithInternal(err)
	}

	return summary, nil
}

// BulkDeleteWithCascade deletes multiple documents and their related entities
// Returns a summary and list of IDs that were not found
func (r *Repository) BulkDeleteWithCascade(ctx context.Context, projectID string, documentIDs []string) (*DeleteSummary, []string, error) {
	summary := &DeleteSummary{}
	var notFound []string

	// First, verify which documents exist
	var existingIDs []string
	err := r.db.NewSelect().
		Model((*Document)(nil)).
		Column("id").
		Where("id IN (?)", bun.In(documentIDs)).
		Where("project_id = ?", projectID).
		Scan(ctx, &existingIDs)
	if err != nil {
		return nil, nil, apperror.ErrDatabase.WithInternal(err)
	}

	// Find which IDs don't exist
	existingSet := make(map[string]bool)
	for _, id := range existingIDs {
		existingSet[id] = true
	}
	for _, id := range documentIDs {
		if !existingSet[id] {
			notFound = append(notFound, id)
		}
	}

	// If no documents exist, return early
	if len(existingIDs) == 0 {
		return summary, notFound, nil
	}

	err = r.db.RunInTx(ctx, nil, func(ctx context.Context, tx bun.Tx) error {
		// 1. Delete notifications
		result, err := tx.NewDelete().
			TableExpr("kb.notifications").
			Where("related_resource_type = ?", "document").
			Where("related_resource_id IN (?)", bun.In(existingIDs)).
			Exec(ctx)
		if err != nil {
			return fmt.Errorf("delete notifications: %w", err)
		}
		if n, _ := result.RowsAffected(); n > 0 {
			summary.Notifications = int(n)
		}

		// 2. Get extraction job IDs
		var jobIDs []string
		err = tx.NewSelect().
			TableExpr("kb.object_extraction_jobs").
			Column("id").
			Where("document_id IN (?)", bun.In(existingIDs)).
			Scan(ctx, &jobIDs)
		if err != nil && err != sql.ErrNoRows {
			return fmt.Errorf("get extraction jobs: %w", err)
		}

		if len(jobIDs) > 0 {
			// 3-5. Resolve graph object rows and canonical entities to remove.
			objectRowIDs, canonicalIDs, err := r.resolveGraphCascade(ctx, tx, projectID, jobIDs)
			if err != nil {
				return err
			}

			// Determine which canonical entities survive (attributable rows are
			// deleted, but the entity itself is not fully removed). Their version
			// chains must be repaired after the rows are gone.
			survivingCanonicals, err := r.survivingCanonicalsForDeletion(ctx, tx, projectID, objectRowIDs, canonicalIDs)
			if err != nil {
				return err
			}

			// Delete relationships referencing fully-removed canonicals FIRST.
			if len(canonicalIDs) > 0 {
				result, err = tx.NewDelete().
					TableExpr("kb.graph_relationships").
					Where("src_id IN (?) OR dst_id IN (?)", bun.In(canonicalIDs), bun.In(canonicalIDs)).
					Exec(ctx)
				if err != nil {
					return fmt.Errorf("delete relationships: %w", err)
				}
				if n, _ := result.RowsAffected(); n > 0 {
					summary.GraphRelationships = int(n)
				}
			}

			// Delete graph object rows.
			if len(objectRowIDs) > 0 {
				result, err = tx.NewDelete().
					TableExpr("kb.graph_objects").
					Where("id IN (?) AND project_id = ?", bun.In(objectRowIDs), projectID).
					Exec(ctx)
				if err != nil {
					return fmt.Errorf("delete graph objects: %w", err)
				}
				if n, _ := result.RowsAffected(); n > 0 {
					summary.GraphObjects = int(n)
				}
			}

			// Repair version chains for surviving canonicals so they keep a
			// single HEAD and never point at the deleted rows.
			if err := r.repairSurvivingVersionChains(ctx, tx, projectID, survivingCanonicals, objectRowIDs); err != nil {
				return err
			}

			// 6. Delete extraction jobs
			result, err = tx.NewDelete().
				TableExpr("kb.object_extraction_jobs").
				Where("id IN (?)", bun.In(jobIDs)).
				Exec(ctx)
			if err != nil {
				return fmt.Errorf("delete extraction jobs: %w", err)
			}
			if n, _ := result.RowsAffected(); n > 0 {
				summary.ExtractionJobs = int(n)
			}
		}

		// 7. Delete chunks
		result, err = tx.NewDelete().
			TableExpr("kb.chunks").
			Where("document_id IN (?)", bun.In(existingIDs)).
			Exec(ctx)
		if err != nil {
			return fmt.Errorf("delete chunks: %w", err)
		}
		if n, _ := result.RowsAffected(); n > 0 {
			summary.Chunks = int(n)
		}

		// 8. Delete documents
		_, err = tx.NewDelete().
			Model((*Document)(nil)).
			Where("id IN (?)", bun.In(existingIDs)).
			Where("project_id = ?", projectID).
			Exec(ctx)
		if err != nil {
			return fmt.Errorf("delete documents: %w", err)
		}

		return nil
	})

	if err != nil {
		r.log.Error("failed to bulk delete documents", logger.Error(err))
		return nil, nil, apperror.ErrDatabase.WithInternal(err)
	}

	return summary, notFound, nil
}

// ParseCursor decodes a base64-encoded cursor
func ParseCursor(encoded string) (*Cursor, error) {
	if encoded == "" {
		return nil, nil
	}

	data, err := base64.URLEncoding.DecodeString(encoded)
	if err != nil {
		return nil, fmt.Errorf("invalid cursor encoding: %w", err)
	}

	var cursor Cursor
	if err := json.Unmarshal(data, &cursor); err != nil {
		return nil, fmt.Errorf("invalid cursor format: %w", err)
	}

	return &cursor, nil
}

// GetContent retrieves just the content of a document
func (r *Repository) GetContent(ctx context.Context, projectID, documentID string) (*string, error) {
	var content sql.NullString
	err := r.db.NewSelect().
		TableExpr("kb.documents").
		Column("content").
		Where("id = ?", documentID).
		Where("project_id = ?", projectID).
		Scan(ctx, &content)

	if err != nil {
		if err == sql.ErrNoRows {
			return nil, nil // Return nil, nil for not found
		}
		return nil, fmt.Errorf("get document content: %w", err)
	}

	if content.Valid {
		return &content.String, nil
	}
	return nil, nil
}

// GetStorageInfo retrieves storage-related document info for downloads
func (r *Repository) GetStorageInfo(ctx context.Context, projectID, documentID string) (*StorageInfo, error) {
	var info StorageInfo
	err := r.db.NewSelect().
		TableExpr("kb.documents").
		Column("id", "filename", "storage_key", "mime_type", "file_size_bytes", "project_id", "conversion_status").
		Where("id = ?", documentID).
		Where("project_id = ?", projectID).
		Scan(ctx, &info)

	if err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, fmt.Errorf("get storage info: %w", err)
	}

	return &info, nil
}

// GetDeletionImpact calculates the impact of deleting a document
func (r *Repository) GetDeletionImpact(ctx context.Context, projectID, documentID string) (*DeletionImpact, error) {
	// Get document info
	var doc struct {
		ID        string    `bun:"id"`
		Filename  *string   `bun:"filename"`
		SourceURL *string   `bun:"source_url"`
		CreatedAt time.Time `bun:"created_at"`
	}
	err := r.db.NewSelect().
		TableExpr("kb.documents").
		Column("id", "filename", "source_url", "created_at").
		Where("id = ?", documentID).
		Where("project_id = ?", projectID).
		Scan(ctx, &doc)

	if err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, fmt.Errorf("get document: %w", err)
	}

	// Count chunks
	chunksCount, err := r.db.NewSelect().
		TableExpr("kb.chunks").
		Where("document_id = ?", documentID).
		Count(ctx)
	if err != nil {
		return nil, fmt.Errorf("count chunks: %w", err)
	}

	// Get extraction job IDs
	var jobIDs []string
	err = r.db.NewSelect().
		TableExpr("kb.object_extraction_jobs").
		Column("id").
		Where("document_id = ?", documentID).
		Scan(ctx, &jobIDs)
	if err != nil && err != sql.ErrNoRows {
		return nil, fmt.Errorf("get extraction jobs: %w", err)
	}

	// Resolve graph object rows and canonical entities to remove (canonical-aware).
	objectRowIDs, canonicalIDs, err := r.resolveGraphCascade(ctx, r.db, projectID, jobIDs)
	if err != nil {
		return nil, err
	}
	objectsCount := len(objectRowIDs)

	// Count relationships referencing fully-removed canonicals.
	var relationshipsCount int
	if len(canonicalIDs) > 0 {
		relationshipsCount, err = r.db.NewSelect().
			TableExpr("kb.graph_relationships").
			Where("src_id IN (?) OR dst_id IN (?)", bun.In(canonicalIDs), bun.In(canonicalIDs)).
			Count(ctx)
		if err != nil {
			return nil, fmt.Errorf("count relationships: %w", err)
		}
	}

	// Count notifications
	notificationsCount, err := r.db.NewSelect().
		TableExpr("kb.notifications").
		Where("related_resource_type = ?", "document").
		Where("related_resource_id = ?", documentID).
		Count(ctx)
	if err != nil {
		return nil, fmt.Errorf("count notifications: %w", err)
	}

	// Determine document name
	name := "unknown"
	if doc.Filename != nil && *doc.Filename != "" {
		name = *doc.Filename
	} else if doc.SourceURL != nil && *doc.SourceURL != "" {
		name = *doc.SourceURL
	}

	return &DeletionImpact{
		Document: DocumentInfo{
			ID:        doc.ID,
			Name:      name,
			CreatedAt: doc.CreatedAt.Format(time.RFC3339),
		},
		Impact: ImpactSummary{
			Chunks:             chunksCount,
			ExtractionJobs:     len(jobIDs),
			GraphObjects:       objectsCount,
			GraphRelationships: relationshipsCount,
			Notifications:      notificationsCount,
		},
	}, nil
}

// GetBulkDeletionImpact calculates the impact of deleting multiple documents
func (r *Repository) GetBulkDeletionImpact(ctx context.Context, projectID string, documentIDs []string) (*BulkDeletionImpact, error) {
	if len(documentIDs) == 0 {
		return &BulkDeletionImpact{
			TotalDocuments: 0,
			Impact:         ImpactSummary{},
			Documents:      []DeletionImpact{},
		}, nil
	}

	// Get all documents info
	var docs []struct {
		ID        string    `bun:"id"`
		Filename  *string   `bun:"filename"`
		SourceURL *string   `bun:"source_url"`
		CreatedAt time.Time `bun:"created_at"`
	}
	err := r.db.NewSelect().
		TableExpr("kb.documents").
		Column("id", "filename", "source_url", "created_at").
		Where("id IN (?)", bun.In(documentIDs)).
		Where("project_id = ?", projectID).
		Scan(ctx, &docs)

	if err != nil {
		return nil, fmt.Errorf("get documents: %w", err)
	}

	if len(docs) == 0 {
		return &BulkDeletionImpact{
			TotalDocuments: 0,
			Impact:         ImpactSummary{},
			Documents:      []DeletionImpact{},
		}, nil
	}

	existingIDs := make([]string, len(docs))
	for i, doc := range docs {
		existingIDs[i] = doc.ID
	}

	// Count chunks per document
	type countResult struct {
		DocumentID string `bun:"document_id"`
		Count      int    `bun:"count"`
	}
	var chunksCounts []countResult
	err = r.db.NewSelect().
		TableExpr("kb.chunks").
		ColumnExpr("document_id, COUNT(*)::int as count").
		Where("document_id IN (?)", bun.In(existingIDs)).
		Group("document_id").
		Scan(ctx, &chunksCounts)
	if err != nil {
		return nil, fmt.Errorf("count chunks: %w", err)
	}
	chunksMap := make(map[string]int)
	for _, c := range chunksCounts {
		chunksMap[c.DocumentID] = c.Count
	}

	// Get all extraction job IDs
	type jobResult struct {
		ID         string `bun:"id"`
		DocumentID string `bun:"document_id"`
	}
	var jobs []jobResult
	err = r.db.NewSelect().
		TableExpr("kb.object_extraction_jobs").
		Column("id", "document_id").
		Where("document_id IN (?)", bun.In(existingIDs)).
		Scan(ctx, &jobs)
	if err != nil && err != sql.ErrNoRows {
		return nil, fmt.Errorf("get extraction jobs: %w", err)
	}

	jobsMap := make(map[string]int)
	docJobIDs := make(map[string][]string)
	var allJobIDs []string
	for _, j := range jobs {
		jobsMap[j.DocumentID]++
		docJobIDs[j.DocumentID] = append(docJobIDs[j.DocumentID], j.ID)
		allJobIDs = append(allJobIDs, j.ID)
	}

	// Load all version rows for the union of all job IDs once. Per-document
	// resolutions reuse these rows below: the pure resolver treats rows from
	// other documents' jobs as survivors, so no extra queries are needed per
	// document.
	var totalObjects, totalRelationships int
	var unionRows []graphVersionRow
	var unionCanonicalIDs []string
	if len(allJobIDs) > 0 {
		unionRows, err = r.loadGraphVersionRows(ctx, r.db, projectID, allJobIDs)
		if err != nil {
			return nil, err
		}
		unionObjectRowIDs, canonicals := resolveGraphRemovals(unionRows, allJobIDs)
		unionCanonicalIDs = canonicals
		totalObjects = len(unionObjectRowIDs)
	}

	// Load relationship endpoint rows ONCE for the union of fully-removed
	// canonicals, then partition in Go per document.
	var relEndpoints []relEndpoint
	if len(unionCanonicalIDs) > 0 {
		err = r.db.NewSelect().
			TableExpr("kb.graph_relationships").
			Column("src_id", "dst_id").
			Where("src_id IN (?) OR dst_id IN (?)", bun.In(unionCanonicalIDs), bun.In(unionCanonicalIDs)).
			Scan(ctx, &relEndpoints)
		if err != nil && err != sql.ErrNoRows {
			return nil, fmt.Errorf("load relationship endpoints: %w", err)
		}
	}
	totalRelationships = len(relEndpoints)

	// Per-document graph impact. Each entry reflects deleting that document
	// alone, so entries for documents sharing entities may not sum to the
	// summary totals above.
	docObjects := make(map[string]int)
	docRelationships := make(map[string]int)
	for _, doc := range docs {
		jobIDs := docJobIDs[doc.ID]
		if len(jobIDs) == 0 {
			continue
		}
		objectRowIDs, canonicalIDs := resolveGraphRemovals(unionRows, jobIDs)
		docObjects[doc.ID] = len(objectRowIDs)

		canonicalSet := make(map[string]bool, len(canonicalIDs))
		for _, c := range canonicalIDs {
			canonicalSet[c] = true
		}
		docRelationships[doc.ID] = countRelationshipsTouching(relEndpoints, canonicalSet)
	}

	// Count notifications per document
	var notificationsCounts []countResult
	err = r.db.NewSelect().
		TableExpr("kb.notifications").
		ColumnExpr("related_resource_id as document_id, COUNT(*)::int as count").
		Where("related_resource_type = ?", "document").
		Where("related_resource_id IN (?)", bun.In(existingIDs)).
		Group("related_resource_id").
		Scan(ctx, &notificationsCounts)
	if err != nil {
		return nil, fmt.Errorf("count notifications: %w", err)
	}
	notificationsMap := make(map[string]int)
	for _, n := range notificationsCounts {
		notificationsMap[n.DocumentID] = n.Count
	}

	// Build per-document results
	documents := make([]DeletionImpact, len(docs))
	totalImpact := ImpactSummary{}
	for i, doc := range docs {
		name := "unknown"
		if doc.Filename != nil && *doc.Filename != "" {
			name = *doc.Filename
		} else if doc.SourceURL != nil && *doc.SourceURL != "" {
			name = *doc.SourceURL
		}

		impact := ImpactSummary{
			Chunks:             chunksMap[doc.ID],
			ExtractionJobs:     jobsMap[doc.ID],
			GraphObjects:       docObjects[doc.ID],
			GraphRelationships: docRelationships[doc.ID],
			Notifications:      notificationsMap[doc.ID],
		}

		documents[i] = DeletionImpact{
			Document: DocumentInfo{
				ID:        doc.ID,
				Name:      name,
				CreatedAt: doc.CreatedAt.Format(time.RFC3339),
			},
			Impact: impact,
		}

		totalImpact.Chunks += impact.Chunks
		totalImpact.ExtractionJobs += impact.ExtractionJobs
		totalImpact.Notifications += impact.Notifications
	}

	// Graph totals are exact (computed over the union of all jobs), not the sum
	// of per-document entries (which reflect each document deleted in isolation).
	totalImpact.GraphObjects = totalObjects
	totalImpact.GraphRelationships = totalRelationships

	return &BulkDeletionImpact{
		TotalDocuments: len(documents),
		Impact:         totalImpact,
		Documents:      documents,
	}, nil
}

// UpdateConversionStatus updates the conversion status of a document
func (r *Repository) UpdateConversionStatus(ctx context.Context, documentID, status string, errorMsg *string) error {
	query := r.db.NewUpdate().
		Model((*Document)(nil)).
		Set("conversion_status = ?", status).
		Set("updated_at = ?", time.Now().UTC()).
		Where("id = ?", documentID)

	if errorMsg != nil {
		query = query.Set("conversion_error = ?", *errorMsg)
	} else {
		query = query.Set("conversion_error = NULL")
	}

	if status == "failed" || status == "completed" {
		query = query.Set("conversion_completed_at = ?", time.Now().UTC())
	} else if status == "pending" {
		query = query.Set("conversion_completed_at = NULL")
	}

	_, err := query.Exec(ctx)
	if err != nil {
		return fmt.Errorf("update conversion status: %w", err)
	}

	return nil
}

// UpdateContentAndStatus updates the document content and conversion status after parsing completes
func (r *Repository) UpdateContentAndStatus(ctx context.Context, documentID, content, status string) error {
	now := time.Now().UTC()
	_, err := r.db.NewUpdate().
		Model((*Document)(nil)).
		Set("content = ?", content).
		Set("conversion_status = ?", status).
		Set("conversion_completed_at = ?", now).
		Set("conversion_error = NULL").
		Set("updated_at = ?", now).
		Where("id = ?", documentID).
		Exec(ctx)

	if err != nil {
		return fmt.Errorf("update content and status: %w", err)
	}

	return nil
}

// --- SQL helper functions for computed document fields ---

// processingStatusSQL returns a SQL CASE expression that derives the unified processing status
// from conversion_status, document_parsing_jobs, and object_extraction_jobs.
// Precedence: converting > conversion_failed > extracting > extraction_failed > completed > ready_for_extraction
func processingStatusSQL() string {
	return `CASE
		WHEN EXISTS (
			SELECT 1 FROM kb.document_parsing_jobs dpj
			WHERE dpj.document_id = d.id AND dpj.status IN ('pending', 'processing')
		) THEN 'converting'
		WHEN d.conversion_status = 'failed' THEN 'conversion_failed'
		WHEN EXISTS (
			SELECT 1 FROM kb.object_extraction_jobs oej
			WHERE oej.document_id = d.id AND oej.status IN ('pending', 'processing')
		) THEN 'extracting'
		WHEN (
			SELECT oej2.status FROM kb.object_extraction_jobs oej2
			WHERE oej2.document_id = d.id ORDER BY oej2.created_at DESC LIMIT 1
		) = 'failed' THEN 'extraction_failed'
		WHEN (
			SELECT oej3.status FROM kb.object_extraction_jobs oej3
			WHERE oej3.document_id = d.id ORDER BY oej3.created_at DESC LIMIT 1
		) = 'completed' THEN 'completed'
		ELSE 'ready_for_extraction'
	END AS processing_status`
}

// lastExtractionAtSQL returns the completion timestamp of the most recent completed extraction job
func lastExtractionAtSQL() string {
	return `(SELECT oej.completed_at FROM kb.object_extraction_jobs oej
		WHERE oej.document_id = d.id AND oej.status = 'completed'
		ORDER BY oej.completed_at DESC LIMIT 1) AS last_extraction_at`
}

// extractionObjectsCreatedSQL returns the objects_created count from the most recent completed extraction job
func extractionObjectsCreatedSQL() string {
	return `(SELECT oej.objects_created FROM kb.object_extraction_jobs oej
		WHERE oej.document_id = d.id AND oej.status = 'completed'
		ORDER BY oej.completed_at DESC LIMIT 1) AS extraction_objects_created`
}

// extractionRelationshipsCreatedSQL returns the relationships_created count from the most recent completed extraction job
func extractionRelationshipsCreatedSQL() string {
	return `(SELECT oej.relationships_created FROM kb.object_extraction_jobs oej
		WHERE oej.document_id = d.id AND oej.status = 'completed'
		ORDER BY oej.completed_at DESC LIMIT 1) AS extraction_relationships_created`
}

// GetExtractionSummary retrieves the extraction summary for the most recently completed extraction job
func (r *Repository) GetExtractionSummary(ctx context.Context, projectID, documentID string) (*ExtractionSummary, error) {
	// First, get the most recently completed extraction job for this document
	var job struct {
		ID                   string    `bun:"id"`
		CompletedAt          time.Time `bun:"completed_at"`
		ObjectsCreated       int       `bun:"objects_created"`
		RelationshipsCreated int       `bun:"relationships_created"`
		ProcessedItems       int       `bun:"processed_items"`
		TotalItems           int       `bun:"total_items"`
		CreatedObjectIDs     []string  `bun:"created_object_ids,array"`
	}

	err := r.db.NewSelect().
		TableExpr("kb.object_extraction_jobs").
		Column("id", "completed_at", "objects_created", "relationships_created", "processed_items", "total_items", "created_object_ids").
		Where("document_id = ?", documentID).
		Where("project_id = ?", projectID).
		Where("status = 'completed'").
		OrderExpr("completed_at DESC").
		Limit(1).
		Scan(ctx, &job)

	if err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, fmt.Errorf("get extraction summary job: %w", err)
	}

	// Get objects by type from graph_objects created by this job
	type typeCount struct {
		TypeLabel string `bun:"type"`
		Count     int    `bun:"count"`
	}
	var typeCounts []typeCount
	err = r.db.NewSelect().
		TableExpr("kb.graph_objects").
		ColumnExpr("type, COUNT(*)::int AS count").
		Where("properties->>'_extraction_job_id' = ?", job.ID).
		GroupExpr("type").
		OrderExpr("count DESC").
		Scan(ctx, &typeCounts)
	if err != nil && err != sql.ErrNoRows {
		return nil, fmt.Errorf("get objects by type: %w", err)
	}

	objectsByType := make(map[string]int)
	for _, tc := range typeCounts {
		objectsByType[tc.TypeLabel] = tc.Count
	}

	// Check for errors in extraction logs
	hasErrors := false
	var errorSummary *string

	errorCount, err := r.db.NewSelect().
		TableExpr("kb.object_extraction_logs").
		Where("extraction_job_id = ?", job.ID).
		Where("status = 'failed'").
		Count(ctx)
	if err != nil && err != sql.ErrNoRows {
		return nil, fmt.Errorf("count extraction errors: %w", err)
	}

	if errorCount > 0 {
		hasErrors = true
		msg := fmt.Sprintf("%d step(s) failed during extraction", errorCount)
		errorSummary = &msg
	}

	return &ExtractionSummary{
		JobID:                job.ID,
		CompletedAt:          job.CompletedAt,
		ObjectsCreated:       job.ObjectsCreated,
		RelationshipsCreated: job.RelationshipsCreated,
		ObjectsByType:        objectsByType,
		ObjectIDs:            job.CreatedObjectIDs,
		ChunksProcessed:      job.ProcessedItems,
		TotalChunks:          job.TotalItems,
		HasErrors:            hasErrors,
		ErrorSummary:         errorSummary,
	}, nil
}

// UpdateDomainClassification writes domain classification results to a document.
//
// When force=false (classify-document, worker): domain_name is only updated when the
// current value is NULL or "new_domain", preventing background goroutines from
// overwriting a value already set by finalize-discovery.
//
// When force=true (finalize-discovery): domain_name is always overwritten — finalize-discovery
// is the authoritative writer and must be able to update on re-finalize or extend-mode calls.
func (r *Repository) UpdateDomainClassification(
	ctx context.Context,
	documentID string,
	domainName *string,
	confidence *float32,
	signals map[string]any,
	force bool,
) error {
	now := time.Now().UTC()
	q := r.db.NewUpdate().
		Model((*Document)(nil)).
		Set("domain_confidence = ?", confidence).
		Set("classification_signals = ?", signals).
		Set("updated_at = ?", now).
		Where("id = ?", documentID)

	if domainName != nil {
		q = q.Set("domain_name = ?", domainName)
		// Non-authoritative callers (classify-document, worker) must not overwrite a
		// finalized domain_name set by finalize-discovery.
		if !force {
			q = q.Where("(domain_name IS NULL OR domain_name = 'new_domain')")
		}
	}

	_, err := q.Exec(ctx)
	if err != nil {
		return fmt.Errorf("update domain classification: %w", err)
	}
	return nil
}
