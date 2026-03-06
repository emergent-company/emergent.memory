package superadmin

import (
	"context"
	"database/sql"

	"github.com/uptrace/bun"
)

// Repository provides data access for superadmin operations
type Repository struct {
	db bun.IDB
}

// NewRepository creates a new superadmin repository
func NewRepository(db bun.IDB) *Repository {
	return &Repository{db: db}
}

// IsSuperadmin checks if a user is an active superadmin
func (r *Repository) IsSuperadmin(ctx context.Context, userID string) (bool, error) {
	exists, err := r.db.NewSelect().
		Model((*Superadmin)(nil)).
		Where("user_id = ?", userID).
		Where("revoked_at IS NULL").
		Exists(ctx)
	return exists, err
}

// ListUsers returns paginated users with optional search and org filter
func (r *Repository) ListUsers(ctx context.Context, page, limit int, search, orgID string) ([]UserProfile, int, error) {
	offset := (page - 1) * limit

	q := r.db.NewSelect().
		Model((*UserProfile)(nil)).
		Relation("Emails").
		Where("up.deleted_at IS NULL").
		Order("up.created_at DESC")

	// Apply search filter
	if search != "" {
		q = q.WhereGroup(" AND ", func(sq *bun.SelectQuery) *bun.SelectQuery {
			pattern := "%" + search + "%"
			return sq.
				Where("up.display_name ILIKE ?", pattern).
				WhereOr("up.first_name ILIKE ?", pattern).
				WhereOr("up.last_name ILIKE ?", pattern).
				WhereOr("EXISTS (SELECT 1 FROM core.user_emails ue2 WHERE ue2.user_id = up.id AND ue2.email ILIKE ?)", pattern)
		})
	}

	// Apply org filter
	if orgID != "" {
		q = q.Where("EXISTS (SELECT 1 FROM kb.organization_memberships om WHERE om.user_id = up.id AND om.organization_id = ?)", orgID)
	}

	// Get total count
	total, err := q.Count(ctx)
	if err != nil {
		return nil, 0, err
	}

	// Get paginated results
	var users []UserProfile
	err = q.Offset(offset).Limit(limit).Scan(ctx, &users)
	if err != nil {
		return nil, 0, err
	}

	return users, total, nil
}

// GetUserOrganizations returns all org memberships for a list of user IDs
func (r *Repository) GetUserOrganizations(ctx context.Context, userIDs []string) (map[string][]OrganizationMembership, error) {
	if len(userIDs) == 0 {
		return make(map[string][]OrganizationMembership), nil
	}

	var memberships []OrganizationMembership
	err := r.db.NewSelect().
		Model(&memberships).
		Relation("Organization").
		Where("user_id IN (?)", bun.In(userIDs)).
		Scan(ctx)
	if err != nil {
		return nil, err
	}

	// Group by user
	result := make(map[string][]OrganizationMembership)
	for _, m := range memberships {
		result[m.UserID] = append(result[m.UserID], m)
	}
	return result, nil
}

// OrgWithCounts is a struct for org query results with counts
type OrgWithCounts struct {
	Org
	MemberCount  int `bun:"member_count"`
	ProjectCount int `bun:"project_count"`
}

// ListOrganizations returns paginated organizations with member and project counts
func (r *Repository) ListOrganizations(ctx context.Context, page, limit int) ([]OrgWithCounts, int, error) {
	offset := (page - 1) * limit

	// Get total count
	total, err := r.db.NewSelect().Model((*Org)(nil)).Count(ctx)
	if err != nil {
		return nil, 0, err
	}

	// Get orgs with counts
	var orgs []OrgWithCounts
	err = r.db.NewSelect().
		TableExpr("kb.orgs AS o").
		ColumnExpr("o.*").
		ColumnExpr("(SELECT COUNT(*) FROM kb.organization_memberships om WHERE om.organization_id = o.id) AS member_count").
		ColumnExpr("(SELECT COUNT(*) FROM kb.projects p WHERE p.organization_id = o.id AND p.deleted_at IS NULL) AS project_count").
		Order("o.created_at DESC").
		Offset(offset).
		Limit(limit).
		Scan(ctx, &orgs)
	if err != nil {
		return nil, 0, err
	}

	return orgs, total, nil
}

// ProjectWithCounts is a struct for project query results with counts
type ProjectWithCounts struct {
	Project
	OrganizationName string `bun:"organization_name"`
	DocumentCount    int    `bun:"document_count"`
}

// ListProjects returns paginated projects with document counts
func (r *Repository) ListProjects(ctx context.Context, page, limit int, orgID string) ([]ProjectWithCounts, int, error) {
	offset := (page - 1) * limit

	// Count query
	countQ := r.db.NewSelect().
		Model((*Project)(nil)).
		Where("deleted_at IS NULL")
	if orgID != "" {
		countQ = countQ.Where("organization_id = ?", orgID)
	}
	total, err := countQ.Count(ctx)
	if err != nil {
		return nil, 0, err
	}

	// Get projects with counts
	q := r.db.NewSelect().
		TableExpr("kb.projects AS p").
		Join("LEFT JOIN kb.orgs AS o ON o.id = p.organization_id").
		ColumnExpr("p.*").
		ColumnExpr("o.name AS organization_name").
		ColumnExpr("(SELECT COUNT(*) FROM kb.documents d WHERE d.project_id = p.id) AS document_count").
		Where("p.deleted_at IS NULL").
		Order("p.created_at DESC").
		Offset(offset).
		Limit(limit)

	if orgID != "" {
		q = q.Where("p.organization_id = ?", orgID)
	}

	var projects []ProjectWithCounts
	err = q.Scan(ctx, &projects)
	if err != nil {
		return nil, 0, err
	}

	return projects, total, nil
}

// ListEmailJobs returns paginated email jobs with optional filters
func (r *Repository) ListEmailJobs(ctx context.Context, page, limit int, status, recipient, fromDate, toDate string) ([]EmailJob, int, error) {
	offset := (page - 1) * limit

	q := r.db.NewSelect().Model((*EmailJob)(nil))

	if status != "" {
		q = q.Where("status = ?", status)
	}
	if recipient != "" {
		q = q.Where("to_email ILIKE ?", "%"+recipient+"%")
	}
	if fromDate != "" {
		q = q.Where("created_at >= ?", fromDate)
	}
	if toDate != "" {
		q = q.Where("created_at <= ?", toDate)
	}

	total, err := q.Count(ctx)
	if err != nil {
		return nil, 0, err
	}

	var jobs []EmailJob
	err = q.Order("created_at DESC").Offset(offset).Limit(limit).Scan(ctx, &jobs)
	if err != nil {
		return nil, 0, err
	}

	return jobs, total, nil
}

// GetEmailJob returns a single email job by ID
func (r *Repository) GetEmailJob(ctx context.Context, id string) (*EmailJob, error) {
	var job EmailJob
	err := r.db.NewSelect().Model(&job).Where("id = ?", id).Scan(ctx)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	return &job, err
}

// EmbeddingJobStats contains stats for embedding jobs
type EmbeddingJobStats struct {
	Total      int
	Pending    int
	Completed  int
	Failed     int
	WithErrors int
}

// ListGraphEmbeddingJobs returns paginated graph embedding jobs
func (r *Repository) ListGraphEmbeddingJobs(ctx context.Context, page, limit int, status string, hasError *bool, projectID string) ([]EmbeddingJobDTO, int, error) {
	offset := (page - 1) * limit

	q := r.db.NewSelect().
		TableExpr("kb.graph_embedding_jobs AS gej").
		Join("LEFT JOIN kb.graph_objects AS obj ON obj.id = gej.object_id").
		Join("LEFT JOIN kb.projects AS proj ON proj.id = obj.project_id").
		ColumnExpr("gej.*").
		ColumnExpr("obj.project_id AS project_id").
		ColumnExpr("proj.name AS project_name")

	if status != "" {
		q = q.Where("gej.status = ?", status)
	}
	if hasError != nil {
		if *hasError {
			q = q.Where("gej.last_error IS NOT NULL")
		} else {
			q = q.Where("gej.last_error IS NULL")
		}
	}
	if projectID != "" {
		q = q.Where("obj.project_id = ?", projectID)
	}

	total, err := q.Count(ctx)
	if err != nil {
		return nil, 0, err
	}

	type result struct {
		GraphEmbeddingJob
		ProjectID   *string `bun:"project_id"`
		ProjectName *string `bun:"project_name"`
	}
	var results []result
	err = q.Order("gej.created_at DESC").Offset(offset).Limit(limit).Scan(ctx, &results)
	if err != nil {
		return nil, 0, err
	}

	jobs := make([]EmbeddingJobDTO, len(results))
	for i, r := range results {
		jobs[i] = EmbeddingJobDTO{
			ID:           r.ID,
			Type:         "graph",
			TargetID:     r.ObjectID,
			ProjectID:    r.ProjectID,
			ProjectName:  r.ProjectName,
			Status:       r.Status,
			AttemptCount: r.AttemptCount,
			LastError:    r.LastError,
			Priority:     r.Priority,
			ScheduledAt:  r.ScheduledAt,
			StartedAt:    r.StartedAt,
			CompletedAt:  r.CompletedAt,
			CreatedAt:    r.CreatedAt,
			UpdatedAt:    r.UpdatedAt,
		}
	}

	return jobs, total, nil
}

// ListChunkEmbeddingJobs returns paginated chunk embedding jobs
func (r *Repository) ListChunkEmbeddingJobs(ctx context.Context, page, limit int, status string, hasError *bool, projectID string) ([]EmbeddingJobDTO, int, error) {
	offset := (page - 1) * limit

	q := r.db.NewSelect().
		TableExpr("kb.chunk_embedding_jobs AS cej").
		Join("LEFT JOIN kb.chunks AS chunk ON chunk.id = cej.chunk_id").
		Join("LEFT JOIN kb.documents AS doc ON doc.id = chunk.document_id").
		Join("LEFT JOIN kb.projects AS proj ON proj.id = doc.project_id").
		ColumnExpr("cej.*").
		ColumnExpr("doc.project_id AS project_id").
		ColumnExpr("proj.name AS project_name")

	if status != "" {
		q = q.Where("cej.status = ?", status)
	}
	if hasError != nil {
		if *hasError {
			q = q.Where("cej.last_error IS NOT NULL")
		} else {
			q = q.Where("cej.last_error IS NULL")
		}
	}
	if projectID != "" {
		q = q.Where("doc.project_id = ?", projectID)
	}

	total, err := q.Count(ctx)
	if err != nil {
		return nil, 0, err
	}

	type result struct {
		ChunkEmbeddingJob
		ProjectID   *string `bun:"project_id"`
		ProjectName *string `bun:"project_name"`
	}
	var results []result
	err = q.Order("cej.created_at DESC").Offset(offset).Limit(limit).Scan(ctx, &results)
	if err != nil {
		return nil, 0, err
	}

	jobs := make([]EmbeddingJobDTO, len(results))
	for i, r := range results {
		jobs[i] = EmbeddingJobDTO{
			ID:           r.ID,
			Type:         "chunk",
			TargetID:     r.ChunkID,
			ProjectID:    r.ProjectID,
			ProjectName:  r.ProjectName,
			Status:       r.Status,
			AttemptCount: r.AttemptCount,
			LastError:    r.LastError,
			Priority:     r.Priority,
			ScheduledAt:  r.ScheduledAt,
			StartedAt:    r.StartedAt,
			CompletedAt:  r.CompletedAt,
			CreatedAt:    r.CreatedAt,
			UpdatedAt:    r.UpdatedAt,
		}
	}

	return jobs, total, nil
}

// GetEmbeddingJobStats returns stats for embedding jobs
func (r *Repository) GetEmbeddingJobStats(ctx context.Context) (EmbeddingJobStatsDTO, error) {
	var stats EmbeddingJobStatsDTO

	// Graph stats
	var graphStats struct {
		Total      int `bun:"total"`
		Pending    int `bun:"pending"`
		Completed  int `bun:"completed"`
		Failed     int `bun:"failed"`
		WithErrors int `bun:"with_errors"`
	}
	err := r.db.NewSelect().
		TableExpr("kb.graph_embedding_jobs").
		ColumnExpr("COUNT(*) AS total").
		ColumnExpr("SUM(CASE WHEN status = 'pending' THEN 1 ELSE 0 END) AS pending").
		ColumnExpr("SUM(CASE WHEN status = 'completed' THEN 1 ELSE 0 END) AS completed").
		ColumnExpr("SUM(CASE WHEN status = 'failed' THEN 1 ELSE 0 END) AS failed").
		ColumnExpr("SUM(CASE WHEN last_error IS NOT NULL THEN 1 ELSE 0 END) AS with_errors").
		Scan(ctx, &graphStats)
	if err != nil {
		return stats, err
	}

	stats.GraphTotal = graphStats.Total
	stats.GraphPending = graphStats.Pending
	stats.GraphCompleted = graphStats.Completed
	stats.GraphFailed = graphStats.Failed
	stats.GraphWithErrors = graphStats.WithErrors

	// Chunk stats
	var chunkStats struct {
		Total      int `bun:"total"`
		Pending    int `bun:"pending"`
		Completed  int `bun:"completed"`
		Failed     int `bun:"failed"`
		WithErrors int `bun:"with_errors"`
	}
	err = r.db.NewSelect().
		TableExpr("kb.chunk_embedding_jobs").
		ColumnExpr("COUNT(*) AS total").
		ColumnExpr("SUM(CASE WHEN status = 'pending' THEN 1 ELSE 0 END) AS pending").
		ColumnExpr("SUM(CASE WHEN status = 'completed' THEN 1 ELSE 0 END) AS completed").
		ColumnExpr("SUM(CASE WHEN status = 'failed' THEN 1 ELSE 0 END) AS failed").
		ColumnExpr("SUM(CASE WHEN last_error IS NOT NULL THEN 1 ELSE 0 END) AS with_errors").
		Scan(ctx, &chunkStats)
	if err != nil {
		return stats, err
	}

	stats.ChunkTotal = chunkStats.Total
	stats.ChunkPending = chunkStats.Pending
	stats.ChunkCompleted = chunkStats.Completed
	stats.ChunkFailed = chunkStats.Failed
	stats.ChunkWithErrors = chunkStats.WithErrors

	return stats, nil
}

// DeleteGraphEmbeddingJobs deletes graph embedding jobs by IDs
func (r *Repository) DeleteGraphEmbeddingJobs(ctx context.Context, ids []string) (int, error) {
	res, err := r.db.NewDelete().
		Model((*GraphEmbeddingJob)(nil)).
		Where("id IN (?)", bun.In(ids)).
		Exec(ctx)
	if err != nil {
		return 0, err
	}
	n, _ := res.RowsAffected()
	return int(n), nil
}

// DeleteChunkEmbeddingJobs deletes chunk embedding jobs by IDs
func (r *Repository) DeleteChunkEmbeddingJobs(ctx context.Context, ids []string) (int, error) {
	res, err := r.db.NewDelete().
		Model((*ChunkEmbeddingJob)(nil)).
		Where("id IN (?)", bun.In(ids)).
		Exec(ctx)
	if err != nil {
		return 0, err
	}
	n, _ := res.RowsAffected()
	return int(n), nil
}

// CleanupOrphanEmbeddingJobs deletes orphan embedding jobs
func (r *Repository) CleanupOrphanEmbeddingJobs(ctx context.Context) (int, error) {
	// Delete graph jobs with object_missing error
	res1, err := r.db.NewDelete().
		Model((*GraphEmbeddingJob)(nil)).
		Where("last_error = ?", "object_missing").
		Exec(ctx)
	if err != nil {
		return 0, err
	}
	n1, _ := res1.RowsAffected()

	// Delete chunk jobs with missing errors
	res2, err := r.db.NewDelete().
		Model((*ChunkEmbeddingJob)(nil)).
		Where("last_error LIKE ?", "%missing%").
		Exec(ctx)
	if err != nil {
		return int(n1), err
	}
	n2, _ := res2.RowsAffected()

	return int(n1 + n2), nil
}

// ListExtractionJobs returns paginated extraction jobs with optional filters
func (r *Repository) ListExtractionJobs(ctx context.Context, page, limit int, status, jobType, projectID string, hasError *bool) ([]ExtractionJobDTO, int, error) {
	offset := (page - 1) * limit

	q := r.db.NewSelect().
		TableExpr("kb.object_extraction_jobs AS oej").
		Join("LEFT JOIN kb.projects AS p ON p.id = oej.project_id").
		ColumnExpr("oej.*").
		ColumnExpr("p.name AS project_name")

	if status != "" {
		q = q.Where("oej.status = ?", status)
	}
	if jobType != "" {
		q = q.Where("oej.job_type = ?", jobType)
	}
	if projectID != "" {
		q = q.Where("oej.project_id = ?", projectID)
	}
	if hasError != nil {
		if *hasError {
			q = q.Where("oej.error_message IS NOT NULL")
		} else {
			q = q.Where("oej.error_message IS NULL")
		}
	}

	total, err := q.Count(ctx)
	if err != nil {
		return nil, 0, err
	}

	type result struct {
		ObjectExtractionJob
		ProjectName *string `bun:"project_name"`
	}
	var results []result
	err = q.Order("oej.created_at DESC").Offset(offset).Limit(limit).Scan(ctx, &results)
	if err != nil {
		return nil, 0, err
	}

	jobs := make([]ExtractionJobDTO, len(results))
	for i, r := range results {
		// Get document name from source_metadata if available
		var docName *string
		if r.SourceMetadata != nil {
			if m, ok := r.SourceMetadata.(map[string]interface{}); ok {
				if fn, ok := m["filename"].(string); ok {
					docName = &fn
				}
			}
		}

		// Determine document ID
		docID := r.DocumentID
		if docID == nil && r.SourceType != nil && *r.SourceType == "document" && r.SourceID != nil {
			docID = r.SourceID
		}

		jobs[i] = ExtractionJobDTO{
			ID:                   r.ID,
			ProjectID:            r.ProjectID,
			ProjectName:          r.ProjectName,
			DocumentID:           docID,
			DocumentName:         docName,
			ChunkID:              r.ChunkID,
			JobType:              r.JobType,
			Status:               r.Status,
			ObjectsCreated:       r.ObjectsCreated,
			RelationshipsCreated: r.RelationshipsCreated,
			RetryCount:           r.RetryCount,
			MaxRetries:           r.MaxRetries,
			ErrorMessage:         r.ErrorMessage,
			StartedAt:            r.StartedAt,
			CompletedAt:          r.CompletedAt,
			CreatedAt:            r.CreatedAt,
			UpdatedAt:            r.UpdatedAt,
			TotalItems:           r.TotalItems,
			ProcessedItems:       r.ProcessedItems,
			SuccessfulItems:      r.SuccessfulItems,
			FailedItems:          r.FailedItems,
		}
	}

	return jobs, total, nil
}

// GetExtractionJobStats returns stats for extraction jobs
func (r *Repository) GetExtractionJobStats(ctx context.Context) (ExtractionJobStatsDTO, error) {
	var stats ExtractionJobStatsDTO

	var raw struct {
		Total                     int `bun:"total"`
		Queued                    int `bun:"queued"`
		Processing                int `bun:"processing"`
		Completed                 int `bun:"completed"`
		Failed                    int `bun:"failed"`
		Cancelled                 int `bun:"cancelled"`
		WithErrors                int `bun:"with_errors"`
		TotalObjectsCreated       int `bun:"total_objects_created"`
		TotalRelationshipsCreated int `bun:"total_relationships_created"`
	}
	err := r.db.NewSelect().
		TableExpr("kb.object_extraction_jobs").
		ColumnExpr("COUNT(*) AS total").
		ColumnExpr("SUM(CASE WHEN status = 'queued' THEN 1 ELSE 0 END) AS queued").
		ColumnExpr("SUM(CASE WHEN status = 'processing' THEN 1 ELSE 0 END) AS processing").
		ColumnExpr("SUM(CASE WHEN status = 'completed' THEN 1 ELSE 0 END) AS completed").
		ColumnExpr("SUM(CASE WHEN status = 'failed' THEN 1 ELSE 0 END) AS failed").
		ColumnExpr("SUM(CASE WHEN status = 'cancelled' THEN 1 ELSE 0 END) AS cancelled").
		ColumnExpr("SUM(CASE WHEN error_message IS NOT NULL THEN 1 ELSE 0 END) AS with_errors").
		ColumnExpr("COALESCE(SUM(objects_created), 0) AS total_objects_created").
		ColumnExpr("COALESCE(SUM(relationships_created), 0) AS total_relationships_created").
		Scan(ctx, &raw)
	if err != nil {
		return stats, err
	}

	stats.Total = raw.Total
	stats.Queued = raw.Queued
	stats.Processing = raw.Processing
	stats.Completed = raw.Completed
	stats.Failed = raw.Failed
	stats.Cancelled = raw.Cancelled
	stats.WithErrors = raw.WithErrors
	stats.TotalObjectsCreated = raw.TotalObjectsCreated
	stats.TotalRelationshipsCreated = raw.TotalRelationshipsCreated

	return stats, nil
}

// DeleteExtractionJobs deletes extraction jobs by IDs
func (r *Repository) DeleteExtractionJobs(ctx context.Context, ids []string) (int, error) {
	res, err := r.db.NewDelete().
		Model((*ObjectExtractionJob)(nil)).
		Where("id IN (?)", bun.In(ids)).
		Exec(ctx)
	if err != nil {
		return 0, err
	}
	n, _ := res.RowsAffected()
	return int(n), nil
}

// CancelExtractionJobs cancels extraction jobs by IDs
func (r *Repository) CancelExtractionJobs(ctx context.Context, ids []string) (int, error) {
	res, err := r.db.NewUpdate().
		Model((*ObjectExtractionJob)(nil)).
		Set("status = ?", "cancelled").
		Where("id IN (?)", bun.In(ids)).
		Where("status IN (?)", bun.In([]string{"queued", "processing"})).
		Exec(ctx)
	if err != nil {
		return 0, err
	}
	n, _ := res.RowsAffected()
	return int(n), nil
}

// ListDocumentParsingJobs returns paginated document parsing jobs with optional filters
func (r *Repository) ListDocumentParsingJobs(ctx context.Context, page, limit int, status, projectID string, hasError *bool) ([]DocumentParsingJobDTO, int, error) {
	offset := (page - 1) * limit

	q := r.db.NewSelect().
		TableExpr("kb.document_parsing_jobs AS dpj").
		Join("LEFT JOIN kb.projects AS p ON p.id = dpj.project_id").
		Join("LEFT JOIN kb.orgs AS o ON o.id = dpj.organization_id").
		ColumnExpr("dpj.*").
		ColumnExpr("p.name AS project_name").
		ColumnExpr("o.name AS organization_name").
		ColumnExpr("LENGTH(dpj.parsed_content) AS parsed_content_length")

	if status != "" {
		q = q.Where("dpj.status = ?", status)
	}
	if projectID != "" {
		q = q.Where("dpj.project_id = ?", projectID)
	}
	if hasError != nil {
		if *hasError {
			q = q.Where("dpj.error_message IS NOT NULL")
		} else {
			q = q.Where("dpj.error_message IS NULL")
		}
	}

	total, err := q.Count(ctx)
	if err != nil {
		return nil, 0, err
	}

	type result struct {
		DocumentParsingJob
		ProjectName         *string `bun:"project_name"`
		OrganizationName    *string `bun:"organization_name"`
		ParsedContentLength *int    `bun:"parsed_content_length"`
	}
	var results []result
	err = q.Order("dpj.created_at DESC").Offset(offset).Limit(limit).Scan(ctx, &results)
	if err != nil {
		return nil, 0, err
	}

	jobs := make([]DocumentParsingJobDTO, len(results))
	for i, r := range results {
		jobs[i] = DocumentParsingJobDTO{
			ID:                  r.ID,
			OrganizationID:      r.OrganizationID,
			OrganizationName:    r.OrganizationName,
			ProjectID:           r.ProjectID,
			ProjectName:         r.ProjectName,
			Status:              r.Status,
			SourceType:          r.SourceType,
			SourceFilename:      r.SourceFilename,
			MimeType:            r.MimeType,
			FileSizeBytes:       r.FileSizeBytes,
			StorageKey:          r.StorageKey,
			DocumentID:          r.DocumentID,
			ExtractionJobID:     r.ExtractionJobID,
			ParsedContentLength: r.ParsedContentLength,
			ErrorMessage:        r.ErrorMessage,
			RetryCount:          r.RetryCount,
			MaxRetries:          r.MaxRetries,
			NextRetryAt:         r.NextRetryAt,
			CreatedAt:           r.CreatedAt,
			StartedAt:           r.StartedAt,
			CompletedAt:         r.CompletedAt,
			UpdatedAt:           r.UpdatedAt,
			Metadata:            r.Metadata,
		}
	}

	return jobs, total, nil
}

// GetDocumentParsingJobStats returns stats for document parsing jobs
func (r *Repository) GetDocumentParsingJobStats(ctx context.Context) (DocumentParsingJobStatsDTO, error) {
	var stats DocumentParsingJobStatsDTO

	var raw struct {
		Total              int   `bun:"total"`
		Pending            int   `bun:"pending"`
		Processing         int   `bun:"processing"`
		Completed          int   `bun:"completed"`
		Failed             int   `bun:"failed"`
		RetryPending       int   `bun:"retry_pending"`
		WithErrors         int   `bun:"with_errors"`
		TotalFileSizeBytes int64 `bun:"total_file_size_bytes"`
	}
	err := r.db.NewSelect().
		TableExpr("kb.document_parsing_jobs").
		ColumnExpr("COUNT(*) AS total").
		ColumnExpr("SUM(CASE WHEN status = 'pending' THEN 1 ELSE 0 END) AS pending").
		ColumnExpr("SUM(CASE WHEN status = 'processing' THEN 1 ELSE 0 END) AS processing").
		ColumnExpr("SUM(CASE WHEN status = 'completed' THEN 1 ELSE 0 END) AS completed").
		ColumnExpr("SUM(CASE WHEN status = 'failed' THEN 1 ELSE 0 END) AS failed").
		ColumnExpr("SUM(CASE WHEN status = 'retry_pending' THEN 1 ELSE 0 END) AS retry_pending").
		ColumnExpr("SUM(CASE WHEN error_message IS NOT NULL THEN 1 ELSE 0 END) AS with_errors").
		ColumnExpr("COALESCE(SUM(file_size_bytes), 0) AS total_file_size_bytes").
		Scan(ctx, &raw)
	if err != nil {
		return stats, err
	}

	stats.Total = raw.Total
	stats.Pending = raw.Pending
	stats.Processing = raw.Processing
	stats.Completed = raw.Completed
	stats.Failed = raw.Failed
	stats.RetryPending = raw.RetryPending
	stats.WithErrors = raw.WithErrors
	stats.TotalFileSizeBytes = raw.TotalFileSizeBytes

	return stats, nil
}

// DeleteDocumentParsingJobs deletes document parsing jobs by IDs
func (r *Repository) DeleteDocumentParsingJobs(ctx context.Context, ids []string) (int, error) {
	res, err := r.db.NewDelete().
		Model((*DocumentParsingJob)(nil)).
		Where("id IN (?)", bun.In(ids)).
		Exec(ctx)
	if err != nil {
		return 0, err
	}
	n, _ := res.RowsAffected()
	return int(n), nil
}

// RetryDocumentParsingJobs retries failed document parsing jobs
func (r *Repository) RetryDocumentParsingJobs(ctx context.Context, ids []string) (int, error) {
	res, err := r.db.NewUpdate().
		Model((*DocumentParsingJob)(nil)).
		Set("status = ?", "pending").
		Set("error_message = NULL").
		Set("retry_count = retry_count + 1").
		Where("id IN (?)", bun.In(ids)).
		Where("status IN (?)", bun.In([]string{"failed", "retry_pending"})).
		Exec(ctx)
	if err != nil {
		return 0, err
	}
	n, _ := res.RowsAffected()
	return int(n), nil
}

// ListSyncJobs returns paginated sync jobs with optional filters
func (r *Repository) ListSyncJobs(ctx context.Context, page, limit int, status, projectID string, hasError *bool) ([]SyncJobDTO, int, error) {
	offset := (page - 1) * limit

	q := r.db.NewSelect().
		TableExpr("kb.data_source_sync_jobs AS dssj").
		Join("LEFT JOIN kb.projects AS p ON p.id = dssj.project_id").
		Join("LEFT JOIN kb.data_source_integrations AS dsi ON dsi.id = dssj.integration_id").
		ColumnExpr("dssj.*").
		ColumnExpr("p.name AS project_name").
		ColumnExpr("dsi.name AS integration_name").
		ColumnExpr("dsi.provider_type AS provider_type")

	if status != "" {
		q = q.Where("dssj.status = ?", status)
	}
	if projectID != "" {
		q = q.Where("dssj.project_id = ?", projectID)
	}
	if hasError != nil {
		if *hasError {
			q = q.Where("dssj.error_message IS NOT NULL")
		} else {
			q = q.Where("dssj.error_message IS NULL")
		}
	}

	total, err := q.Count(ctx)
	if err != nil {
		return nil, 0, err
	}

	type result struct {
		DataSourceSyncJob
		ProjectName     *string `bun:"project_name"`
		IntegrationName *string `bun:"integration_name"`
		ProviderType    *string `bun:"provider_type"`
	}
	var results []result
	err = q.Order("dssj.created_at DESC").Offset(offset).Limit(limit).Scan(ctx, &results)
	if err != nil {
		return nil, 0, err
	}

	jobs := make([]SyncJobDTO, len(results))
	for i, r := range results {
		jobs[i] = SyncJobDTO{
			ID:              r.ID,
			IntegrationID:   r.IntegrationID,
			IntegrationName: r.IntegrationName,
			ProjectID:       r.ProjectID,
			ProjectName:     r.ProjectName,
			ProviderType:    r.ProviderType,
			Status:          r.Status,
			TotalItems:      r.TotalItems,
			ProcessedItems:  r.ProcessedItems,
			SuccessfulItems: r.SuccessfulItems,
			FailedItems:     r.FailedItems,
			SkippedItems:    r.SkippedItems,
			CurrentPhase:    r.CurrentPhase,
			StatusMessage:   r.StatusMessage,
			ErrorMessage:    r.ErrorMessage,
			TriggerType:     r.TriggerType,
			CreatedAt:       r.CreatedAt,
			StartedAt:       r.StartedAt,
			CompletedAt:     r.CompletedAt,
		}
	}

	return jobs, total, nil
}

// GetSyncJobStats returns stats for sync jobs
func (r *Repository) GetSyncJobStats(ctx context.Context) (SyncJobStatsDTO, error) {
	var stats SyncJobStatsDTO

	var raw struct {
		Total              int `bun:"total"`
		Pending            int `bun:"pending"`
		Running            int `bun:"running"`
		Completed          int `bun:"completed"`
		Failed             int `bun:"failed"`
		Cancelled          int `bun:"cancelled"`
		WithErrors         int `bun:"with_errors"`
		TotalItemsImported int `bun:"total_items_imported"`
	}
	err := r.db.NewSelect().
		TableExpr("kb.data_source_sync_jobs").
		ColumnExpr("COUNT(*) AS total").
		ColumnExpr("SUM(CASE WHEN status = 'pending' THEN 1 ELSE 0 END) AS pending").
		ColumnExpr("SUM(CASE WHEN status = 'running' THEN 1 ELSE 0 END) AS running").
		ColumnExpr("SUM(CASE WHEN status = 'completed' THEN 1 ELSE 0 END) AS completed").
		ColumnExpr("SUM(CASE WHEN status = 'failed' THEN 1 ELSE 0 END) AS failed").
		ColumnExpr("SUM(CASE WHEN status = 'cancelled' THEN 1 ELSE 0 END) AS cancelled").
		ColumnExpr("SUM(CASE WHEN error_message IS NOT NULL THEN 1 ELSE 0 END) AS with_errors").
		ColumnExpr("COALESCE(SUM(successful_items), 0) AS total_items_imported").
		Scan(ctx, &raw)
	if err != nil {
		return stats, err
	}

	stats.Total = raw.Total
	stats.Pending = raw.Pending
	stats.Running = raw.Running
	stats.Completed = raw.Completed
	stats.Failed = raw.Failed
	stats.Cancelled = raw.Cancelled
	stats.WithErrors = raw.WithErrors
	stats.TotalItemsImported = raw.TotalItemsImported

	return stats, nil
}

// GetSyncJob returns a single sync job by ID
func (r *Repository) GetSyncJob(ctx context.Context, id string) (*DataSourceSyncJob, error) {
	var job DataSourceSyncJob
	err := r.db.NewSelect().Model(&job).Where("id = ?", id).Scan(ctx)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	return &job, err
}

// DeleteSyncJobs deletes sync jobs by IDs
func (r *Repository) DeleteSyncJobs(ctx context.Context, ids []string) (int, error) {
	res, err := r.db.NewDelete().
		Model((*DataSourceSyncJob)(nil)).
		Where("id IN (?)", bun.In(ids)).
		Exec(ctx)
	if err != nil {
		return 0, err
	}
	n, _ := res.RowsAffected()
	return int(n), nil
}

// CancelSyncJobs cancels sync jobs by IDs
func (r *Repository) CancelSyncJobs(ctx context.Context, ids []string) (int, error) {
	res, err := r.db.NewUpdate().
		Model((*DataSourceSyncJob)(nil)).
		Set("status = ?", "cancelled").
		Set("completed_at = NOW()").
		Where("id IN (?)", bun.In(ids)).
		Where("status IN (?)", bun.In([]string{"pending", "running"})).
		Exec(ctx)
	if err != nil {
		return 0, err
	}
	n, _ := res.RowsAffected()
	return int(n), nil
}

// SoftDeleteUser soft deletes a user
func (r *Repository) SoftDeleteUser(ctx context.Context, userID, deletedBy string) error {
	_, err := r.db.NewUpdate().
		Model((*UserProfile)(nil)).
		Set("deleted_at = NOW()").
		Set("deleted_by = ?", deletedBy).
		Where("id = ?", userID).
		Where("deleted_at IS NULL").
		Exec(ctx)
	return err
}

// SoftDeleteOrg soft deletes an organization
func (r *Repository) SoftDeleteOrg(ctx context.Context, orgID, deletedBy string) error {
	_, err := r.db.NewUpdate().
		Model((*Org)(nil)).
		Set("deleted_at = NOW()").
		Set("deleted_by = ?", deletedBy).
		Where("id = ?", orgID).
		Where("deleted_at IS NULL").
		Exec(ctx)
	return err
}

// SoftDeleteProject soft deletes a project
func (r *Repository) SoftDeleteProject(ctx context.Context, projectID, deletedBy string) error {
	_, err := r.db.NewUpdate().
		Model((*Project)(nil)).
		Set("deleted_at = NOW()").
		Set("deleted_by = ?", deletedBy).
		Where("id = ?", projectID).
		Where("deleted_at IS NULL").
		Exec(ctx)
	return err
}

// GetUser returns a user by ID
func (r *Repository) GetUser(ctx context.Context, userID string) (*UserProfile, error) {
	var user UserProfile
	err := r.db.NewSelect().Model(&user).Where("id = ?", userID).Where("deleted_at IS NULL").Scan(ctx)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	return &user, err
}

// GetOrg returns an org by ID
func (r *Repository) GetOrg(ctx context.Context, orgID string) (*Org, error) {
	var org Org
	err := r.db.NewSelect().Model(&org).Where("id = ?", orgID).Where("deleted_at IS NULL").Scan(ctx)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	return &org, err
}

// GetProject returns a project by ID
func (r *Repository) GetProject(ctx context.Context, projectID string) (*Project, error) {
	var project Project
	err := r.db.NewSelect().Model(&project).Where("id = ?", projectID).Where("deleted_at IS NULL").Scan(ctx)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	return &project, err
}
