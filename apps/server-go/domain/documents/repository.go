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

	"github.com/emergent/emergent-core/pkg/apperror"
	"github.com/emergent/emergent-core/pkg/logger"
	"github.com/emergent/emergent-core/pkg/pgutils"
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
		Where("d.project_id = ?", params.ProjectID)

	// Apply filters
	if params.SourceType != nil {
		query = query.Where("d.source_type = ?", *params.SourceType)
	}
	if params.IntegrationID != nil {
		query = query.Where("d.data_source_integration_id = ?", *params.IntegrationID)
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
	if params.IntegrationID != nil {
		countQuery = countQuery.Where("data_source_integration_id = ?", *params.IntegrationID)
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
		Model(&doc).
		Where("id = ?", documentID).
		Where("project_id = ?", projectID).
		Scan(ctx)

	if err != nil {
		if err == sql.ErrNoRows {
			return nil, nil // Return nil, nil for not found (let caller decide error)
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

// GetByFileHash retrieves a document by file hash (for upload deduplication)
func (r *Repository) GetByFileHash(ctx context.Context, projectID, fileHash string) (*Document, error) {
	var doc Document
	err := r.db.NewSelect().
		Model(&doc).
		Where("project_id = ?", projectID).
		Where("file_hash = ?", fileHash).
		Scan(ctx)

	if err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, fmt.Errorf("get document by file hash: %w", err)
	}

	return &doc, nil
}

// GetDistinctSourceTypes returns all distinct source types with document counts
func (r *Repository) GetDistinctSourceTypes(ctx context.Context) ([]SourceTypeWithCount, error) {
	var results []SourceTypeWithCount
	err := r.db.NewSelect().
		TableExpr("kb.documents").
		ColumnExpr("source_type, COUNT(*)::int as count").
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
			// 3. Get graph object IDs from these jobs
			var objectIDs []string
			err = tx.NewSelect().
				TableExpr("kb.graph_objects").
				Column("id").
				Where("extraction_job_id IN (?)", bun.In(jobIDs)).
				Scan(ctx, &objectIDs)
			if err != nil && err != sql.ErrNoRows {
				return fmt.Errorf("get graph objects: %w", err)
			}

			if len(objectIDs) > 0 {
				// 4. Delete graph relationships involving these objects
				result, err = tx.NewDelete().
					TableExpr("kb.graph_relationships").
					Where("src_id IN (?) OR dst_id IN (?)", bun.In(objectIDs), bun.In(objectIDs)).
					Exec(ctx)
				if err != nil {
					return fmt.Errorf("delete relationships: %w", err)
				}
				if n, _ := result.RowsAffected(); n > 0 {
					summary.GraphRelationships = int(n)
				}

				// 5. Delete graph objects
				result, err = tx.NewDelete().
					TableExpr("kb.graph_objects").
					Where("id IN (?)", bun.In(objectIDs)).
					Exec(ctx)
				if err != nil {
					return fmt.Errorf("delete graph objects: %w", err)
				}
				if n, _ := result.RowsAffected(); n > 0 {
					summary.GraphObjects = int(n)
				}
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
			// 3. Get graph object IDs
			var objectIDs []string
			err = tx.NewSelect().
				TableExpr("kb.graph_objects").
				Column("id").
				Where("extraction_job_id IN (?)", bun.In(jobIDs)).
				Scan(ctx, &objectIDs)
			if err != nil && err != sql.ErrNoRows {
				return fmt.Errorf("get graph objects: %w", err)
			}

			if len(objectIDs) > 0 {
				// 4. Delete relationships
				result, err = tx.NewDelete().
					TableExpr("kb.graph_relationships").
					Where("src_id IN (?) OR dst_id IN (?)", bun.In(objectIDs), bun.In(objectIDs)).
					Exec(ctx)
				if err != nil {
					return fmt.Errorf("delete relationships: %w", err)
				}
				if n, _ := result.RowsAffected(); n > 0 {
					summary.GraphRelationships = int(n)
				}

				// 5. Delete graph objects
				result, err = tx.NewDelete().
					TableExpr("kb.graph_objects").
					Where("id IN (?)", bun.In(objectIDs)).
					Exec(ctx)
				if err != nil {
					return fmt.Errorf("delete graph objects: %w", err)
				}
				if n, _ := result.RowsAffected(); n > 0 {
					summary.GraphObjects = int(n)
				}
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
		result, err = tx.NewDelete().
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

	// Count graph objects and get object IDs
	var objectsCount int
	var objectIDs []string
	if len(jobIDs) > 0 {
		objectsCount, err = r.db.NewSelect().
			TableExpr("kb.graph_objects").
			Where("extraction_job_id IN (?)", bun.In(jobIDs)).
			Count(ctx)
		if err != nil {
			return nil, fmt.Errorf("count graph objects: %w", err)
		}

		err = r.db.NewSelect().
			TableExpr("kb.graph_objects").
			Column("id").
			Where("extraction_job_id IN (?)", bun.In(jobIDs)).
			Scan(ctx, &objectIDs)
		if err != nil && err != sql.ErrNoRows {
			return nil, fmt.Errorf("get object IDs: %w", err)
		}
	}

	// Count graph relationships
	var relationshipsCount int
	if len(objectIDs) > 0 {
		relationshipsCount, err = r.db.NewSelect().
			TableExpr("kb.graph_relationships").
			Where("src_id IN (?) OR dst_id IN (?)", bun.In(objectIDs), bun.In(objectIDs)).
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
	var allJobIDs []string
	for _, j := range jobs {
		jobsMap[j.DocumentID]++
		allJobIDs = append(allJobIDs, j.ID)
	}

	// Count graph objects per extraction job
	objectsMap := make(map[string]int)
	var allObjectIDs []string
	if len(allJobIDs) > 0 {
		type objectResult struct {
			ExtractionJobID string `bun:"extraction_job_id"`
			Count           int    `bun:"count"`
		}
		var objectsCounts []objectResult
		err = r.db.NewSelect().
			TableExpr("kb.graph_objects").
			ColumnExpr("extraction_job_id, COUNT(*)::int as count").
			Where("extraction_job_id IN (?)", bun.In(allJobIDs)).
			Group("extraction_job_id").
			Scan(ctx, &objectsCounts)
		if err != nil {
			return nil, fmt.Errorf("count graph objects: %w", err)
		}

		jobToDoc := make(map[string]string)
		for _, j := range jobs {
			jobToDoc[j.ID] = j.DocumentID
		}

		for _, o := range objectsCounts {
			if docID, ok := jobToDoc[o.ExtractionJobID]; ok {
				objectsMap[docID] += o.Count
			}
		}

		// Get all object IDs for relationship counting
		err = r.db.NewSelect().
			TableExpr("kb.graph_objects").
			Column("id").
			Where("extraction_job_id IN (?)", bun.In(allJobIDs)).
			Scan(ctx, &allObjectIDs)
		if err != nil && err != sql.ErrNoRows {
			return nil, fmt.Errorf("get object IDs: %w", err)
		}
	}

	// Count total relationships (approximate per-doc)
	relationshipsMap := make(map[string]int)
	var totalRelationships int
	if len(allObjectIDs) > 0 {
		totalRelationships, err = r.db.NewSelect().
			TableExpr("kb.graph_relationships").
			Where("src_id IN (?) OR dst_id IN (?)", bun.In(allObjectIDs), bun.In(allObjectIDs)).
			Count(ctx)
		if err != nil {
			return nil, fmt.Errorf("count relationships: %w", err)
		}

		// Distribute proportionally based on object count
		totalObjects := len(allObjectIDs)
		for docID, objCount := range objectsMap {
			if totalObjects > 0 {
				proportion := float64(objCount) / float64(totalObjects)
				relationshipsMap[docID] = int(float64(totalRelationships) * proportion)
			}
		}
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
			GraphObjects:       objectsMap[doc.ID],
			GraphRelationships: relationshipsMap[doc.ID],
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
		totalImpact.GraphObjects += impact.GraphObjects
		totalImpact.GraphRelationships += impact.GraphRelationships
		totalImpact.Notifications += impact.Notifications
	}

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
