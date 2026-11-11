package documents

import (
	"context"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"
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

// List retrieves documents with pagination and filtering.
func (r *Repository) List(ctx context.Context, params ListParams) (*ListResult, error) {
	// Default limit
	if params.Limit <= 0 {
		params.Limit = 100
	}
	if params.Limit > 500 {
		params.Limit = 500
	}
	params = params.normalized()

	// The handler validates both of these and answers 400; re-checking here keeps
	// a future caller from getting a silently unfiltered page (an unknown bucket
	// would drop the filter) or a page walked with the wrong comparison (a cursor
	// from the other sort).
	if params.Status != "" {
		if _, ok := statusBucketStates[params.Status]; !ok {
			return nil, fmt.Errorf("list documents: unknown status bucket %q", params.Status)
		}
	}
	if params.Cursor != nil && params.Cursor.Sort != "" && params.Cursor.Sort != params.Sort {
		return nil, fmt.Errorf("list documents: cursor was minted for sort %q, not %q", params.Cursor.Sort, params.Sort)
	}

	query := applyListFilters(r.listQueryBase(), params)

	// Apply the keyset cursor on the active sort key. The direction flips the
	// comparison: descending reads backwards from the cursor, ascending forward.
	sortColumn := "d.created_at"
	if params.Sort == SortUpdated {
		sortColumn = "d.updated_at"
	}
	if params.Cursor != nil {
		op := "<"
		if params.Order == OrderAsc {
			op = ">"
		}
		query = query.Where(fmt.Sprintf("(%s, d.id) %s (?, ?)", sortColumn, op), params.Cursor.Value, params.Cursor.ID)
	}

	// Order by the sort key and id for a stable keyset.
	direction := "DESC"
	if params.Order == OrderAsc {
		direction = "ASC"
	}
	query = query.Order(fmt.Sprintf("%s %s", sortColumn, direction), "d.id "+direction).
		Limit(params.Limit + 1) // +1 to detect if there are more

	documents := []Document{}
	if err := query.Scan(ctx, &documents); err != nil {
		return nil, fmt.Errorf("list documents: %w", err)
	}

	// Check if there are more results
	var nextCursor *string
	if len(documents) > params.Limit {
		documents = documents[:params.Limit]
		last := documents[len(documents)-1]
		sortValue := last.CreatedAt
		if params.Sort == SortUpdated {
			sortValue = last.UpdatedAt
		}
		encoded, err := encodeCursor(Cursor{Sort: params.Sort, Value: sortValue, ID: last.ID})
		if err != nil {
			return nil, err
		}
		nextCursor = &encoded
	}

	total, err := r.count(ctx, params)
	if err != nil {
		return nil, err
	}

	var counts StatusCounts
	if !params.SkipStatusCounts {
		counts, err = r.statusCounts(ctx, params)
		if err != nil {
			return nil, err
		}
	}

	return &ListResult{
		Documents:    documents,
		Total:        total,
		StatusCounts: counts,
		NextCursor:   nextCursor,
	}, nil
}

// StatusCounts computes the per-status breakdown for the listing filters. The
// service layer caches it because the breakdown costs O(project) while the page
// is O(1) (see #2262); direct Repository callers get the uncached value.
func (r *Repository) StatusCounts(ctx context.Context, params ListParams) (StatusCounts, error) {
	return r.statusCounts(ctx, params)
}

// listQueryBase returns the listing select with its computed columns and the
// filters every listing carries, before caller-specific filters are applied.
func (r *Repository) listQueryBase() *bun.SelectQuery {
	return r.db.NewSelect().
		TableExpr("kb.documents AS d").
		// LEFT so a source-synced or pre-#2260 row (created_by_user_id NULL) still
		// lists; the display name is simply absent for it.
		Join("LEFT JOIN core.user_profiles AS up_created ON up_created.id = d.created_by_user_id").
		ColumnExpr("d.*").
		ColumnExpr("up_created.display_name AS created_by_display_name").
		ColumnExpr("COALESCE(LENGTH(d.content), 0) AS total_chars").
		ColumnExpr("(SELECT COUNT(*)::int FROM kb.chunks c WHERE c.document_id = d.id) AS chunks").
		ColumnExpr("(SELECT COUNT(*)::int FROM kb.chunks c WHERE c.document_id = d.id AND c.embedding IS NOT NULL) AS embedded_chunks").
		ColumnExpr("(SELECT ej.status FROM kb.object_extraction_jobs ej WHERE ej.document_id = d.id ORDER BY ej.created_at DESC LIMIT 1) AS extraction_status").
		ColumnExpr(processingStatusSQL()).
		ColumnExpr(lastExtractionAtSQL()).
		ColumnExpr(extractionObjectsCreatedSQL()).
		ColumnExpr(extractionRelationshipsCreatedSQL())
}

// applyListFilters applies the filters shared by the listing, its total count and
// the status breakdown. It exists so a new filter cannot land in one of those
// queries and be forgotten in the others — the three used to duplicate this
// block.
func applyListFilters(query *bun.SelectQuery, params ListParams) *bun.SelectQuery {
	query = query.Where("d.project_id = ?", params.ProjectID)

	// Listings are HEAD-only: a versioned document's superseded rows are history
	// and are only returned by the version-history endpoint. Without this filter
	// a document appears once per version and inflates the total.
	query = query.Where("d.supersedes_id IS NULL")

	// Default listings exclude documents removed at the source; history reads
	// opt back in via ListParams.IncludeDeletedFromSource.
	if !params.IncludeDeletedFromSource {
		query = query.Where("d.source_status <> ?", SourceStatusDeletedFromSource)
	}

	if params.SourceType != nil {
		query = query.Where("d.source_type = ?", *params.SourceType)
	}
	if params.SourceUnassigned {
		query = query.Where("d.source_id IS NULL")
	} else if params.SourceID != nil {
		query = query.Where("d.source_id = ?", *params.SourceID)
	}
	if params.RootOnly {
		query = query.Where("d.parent_document_id IS NULL")
	}
	if params.ParentDocumentID != nil {
		query = query.Where("d.parent_document_id = ?", *params.ParentDocumentID)
	}

	// Metadata search. Source documents may have no filename at all, so the
	// source URL is matched too: it is the string a user recognises for those
	// rows. Matching is ILIKE with the pattern's wildcards escaped, so a literal
	// % or _ in a filename cannot turn into a wildcard.
	if params.Query != "" {
		pattern := "%" + escapeLikePattern(params.Query) + "%"
		query = query.Where(
			"(COALESCE(d.filename, '') ILIKE ? ESCAPE '\\' OR COALESCE(d.source_url, '') ILIKE ? ESCAPE '\\')",
			pattern, pattern,
		)
	}

	// Status filter. The derived status is a single value per row, so filtering
	// its CASE output by the bucket's state set is exactly the four-bucket
	// partition. Qualifying the expression (rather than re-deriving conditions)
	// keeps the filter, the count and the rendered badge in agreement.
	if params.Status != "" {
		if states, ok := statusBucketStates[params.Status]; ok {
			query = query.Where("("+processingStatusExpr()+") IN (?)", bun.In(states))
		}
	}

	return query
}

// count returns the number of documents matching every filter, status included.
func (r *Repository) count(ctx context.Context, params ListParams) (int, error) {
	query := applyListFilters(r.db.NewSelect().TableExpr("kb.documents AS d"), params)
	total, err := query.Count(ctx)
	if err != nil {
		return 0, fmt.Errorf("count documents: %w", err)
	}
	return total, nil
}

// statusCountsRow is one (derived status, count) pair from the breakdown query.
type statusCountsRow struct {
	Status string `bun:"status"`
	Count  int    `bun:"count"`
}

// statusCounts aggregates the derived status over the filtered population,
// deliberately ignoring the status filter so filter counts stay stable while a
// status is selected. The search and source filters do apply, so narrowing the
// list narrows the counts with it.
func (r *Repository) statusCounts(ctx context.Context, params ListParams) (StatusCounts, error) {
	params.Status = ""
	var out StatusCounts

	rows := []statusCountsRow{}
	query := applyListFilters(r.db.NewSelect().TableExpr("kb.documents AS d"), params).
		ColumnExpr("(" + processingStatusExpr() + ") AS status").
		ColumnExpr("COUNT(*)::int AS count").
		GroupExpr("status")
	if err := query.Scan(ctx, &rows); err != nil {
		return out, fmt.Errorf("count documents by status: %w", err)
	}
	for _, row := range rows {
		out.add(row.Status, row.Count)
	}
	return out, nil
}

// escapeLikePattern escapes the LIKE metacharacters in a user-supplied search
// term so it matches literally. The ILIKE predicates pair this with
// `ESCAPE '\'`.
func escapeLikePattern(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	for _, r := range s {
		switch r {
		case '\\', '%', '_':
			b.WriteRune('\\')
		}
		b.WriteRune(r)
	}
	return b.String()
}

// MaxDocumentSearchTerm caps the search term accepted by the listing.
const MaxDocumentSearchTerm = 200

// NormalizeDocumentSearch trims a search term and caps its length so a pasted
// document cannot become an unbounded LIKE pattern. Empty or whitespace-only
// input means "no search".
func NormalizeDocumentSearch(raw string) string {
	s := strings.TrimSpace(raw)
	if r := []rune(s); len(r) > MaxDocumentSearchTerm {
		s = string(r[:MaxDocumentSearchTerm])
	}
	return s
}

// encodeCursor renders a cursor as the opaque token the API returns.
func encodeCursor(cursor Cursor) (string, error) {
	data, err := json.Marshal(cursor)
	if err != nil {
		return "", fmt.Errorf("encode cursor: %w", err)
	}
	return base64.URLEncoding.EncodeToString(data), nil
}

// GetByID retrieves a single document by ID
func (r *Repository) GetByID(ctx context.Context, projectID, documentID string) (*Document, error) {
	var doc Document
	err := r.db.NewSelect().
		TableExpr("kb.documents AS d").
		Join("LEFT JOIN core.user_profiles AS up_created ON up_created.id = d.created_by_user_id").
		ColumnExpr("d.*").
		ColumnExpr("up_created.display_name AS created_by_display_name").
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

// GetByContentHash retrieves a manual HEAD document by content hash (for
// deduplication). The scope mirrors the partial unique index
// idx_documents_project_content_hash_manual_head: only a live manual HEAD
// participates in dedupe. Source-synced rows, superseded versions, and file
// uploads may legitimately share content bytes, so they must never be returned
// here as a manual-create hit. File uploads are identified by file_hash, not by
// source_type: kb.documents.source_type defaults to 'upload' for every row,
// including manual text creates.
func (r *Repository) GetByContentHash(ctx context.Context, projectID, contentHash string) (*Document, error) {
	var doc Document
	err := r.db.NewSelect().
		Model(&doc).
		Where("project_id = ?", projectID).
		Where("content_hash = ?", contentHash).
		Where("supersedes_id IS NULL").
		Where("source_id IS NULL").
		Where("file_hash IS NULL").
		Scan(ctx)

	if err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, fmt.Errorf("get document by content hash: %w", err)
	}

	return &doc, nil
}

// FindUploadByFileHash returns the newest live document in the project whose
// stored bytes match fileHash, for best-effort upload dedupe (it is what keeps
// an agent's retried upload from paying for a second parse and a second
// extraction).
//
// A document qualifies when its parse is either still in flight or already
// produced text — a retry seconds after a timeout must match a document whose
// job has not run yet, which is exactly the window the cost is paid in. It does
// not qualify when its parse reached a terminal failure: an empty document must
// not be handed back as if the retry succeeded, so a retry re-uploads and the
// pipeline gets another attempt. HEAD only (supersedes_id IS NULL), like every
// other read: an older version is not what the caller's bytes produced.
//
// Best-effort by design — there is no unique index on (project_id, file_hash)
// since uploads stopped being deduplicated at insert time, so two concurrent
// identical uploads can both create a row. The caller that matters is a retry,
// which is sequential.
func (r *Repository) FindUploadByFileHash(ctx context.Context, projectID, fileHash string) (*Document, error) {
	if fileHash == "" {
		return nil, nil
	}
	var doc Document
	err := r.db.NewSelect().
		Model(&doc).
		// The alias must match the one bun qualifies the selected columns with,
		// so the EXISTS below can refer to the outer row.
		ModelTableExpr("kb.documents AS document").
		Where("document.project_id = ?", projectID).
		Where("document.file_hash = ?", fileHash).
		Where("document.supersedes_id IS NULL").
		Where("document.source_status <> ?", SourceStatusDeletedFromSource).
		Where("document.conversion_status IS DISTINCT FROM 'failed'").
		Where(`document.content IS NOT NULL OR EXISTS (
			SELECT 1 FROM kb.document_parsing_jobs dpj
			WHERE dpj.document_id = document.id
			  AND dpj.status IN ('pending', 'processing', 'retry_pending'))`).
		Order("document.created_at DESC").
		Limit(1).
		Scan(ctx)

	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, fmt.Errorf("find upload by file hash: %w", err)
	}

	return &doc, nil
}

// GetDistinctSourceTypes returns all distinct source types with document counts for a project.
// The population matches a default listing (HEAD-only, documents removed at the
// source excluded) so the counts line up with the rows a client can see.
func (r *Repository) GetDistinctSourceTypes(ctx context.Context, projectID string) ([]SourceTypeWithCount, error) {
	var results []SourceTypeWithCount
	err := r.db.NewSelect().
		TableExpr("kb.documents").
		ColumnExpr("source_type, COUNT(*)::int as count").
		Where("project_id = ?", projectID).
		Where("source_type IS NOT NULL").
		Where("supersedes_id IS NULL").
		Where("source_status <> ?", SourceStatusDeletedFromSource).
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

// GetDistinctSourceIDs returns the documents' provenance buckets by connected
// data source (empty id for direct uploads) with their counts. The population is
// the same as a default listing, so the source-filter counts line up with the
// rows.
func (r *Repository) GetDistinctSourceIDs(ctx context.Context, projectID string) ([]SourceCount, error) {
	var results []SourceCount
	query := applyListFilters(r.db.NewSelect().TableExpr("kb.documents AS d"), ListParams{ProjectID: projectID}).
		ColumnExpr("d.source_id").
		ColumnExpr("d.source_type").
		ColumnExpr("COUNT(*)::int AS count").
		GroupExpr("d.source_id, d.source_type").
		OrderExpr("count DESC")
	if err := query.Scan(ctx, &results); err != nil {
		r.log.Error("failed to get source counts", logger.Error(err))
		return nil, apperror.NewDatabase("failed to get source counts", err)
	}
	if results == nil {
		results = []SourceCount{}
	}
	return results, nil
}

// populateContentHash enforces the content identity invariant the repository
// owns: whenever a document carries content, content_hash is sha256(content) in
// lowercase hex, even when a caller supplied a different value. This keeps every
// insert path (manual create, source ingestion, version append) consistent.
//
// source_text_hash is a different thing and is deliberately not touched:
// content_hash identifies the stored bytes, while source_text_hash hashes the
// parsed source text so the ingestion seam can detect whether a re-sync changed
// the document at all.
//
// A nil Content leaves the hash as supplied (nil for uploads, which have no
// content until conversion fills it in).
func populateContentHash(doc *Document) {
	if doc == nil || doc.Content == nil {
		return
	}
	hash := computeContentHash(*doc.Content)
	doc.ContentHash = &hash
}

// Create creates a new document in the database
func (r *Repository) Create(ctx context.Context, doc *Document) error {
	populateContentHash(doc)
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

// ---------------------------------------------------------------------------
// Source-attributed document versioning (ingestion seam).
//
// These are the repository methods the ingestion path (Lane C) calls. They
// deliberately bypass the content-hash dedup in Create/GetByContentHash: a new
// version may share bytes with a prior version, and ingestion must not collapse
// versions (design D2/D3).
// ---------------------------------------------------------------------------

// GetHeadBySource resolves the HEAD version (supersedes_id IS NULL) of the
// document attributed to (source_id, source_external_id). Returns nil, nil when
// no HEAD exists.
func (r *Repository) GetHeadBySource(ctx context.Context, projectID, sourceID, externalID string) (*Document, error) {
	var doc Document
	err := r.db.NewSelect().
		Model(&doc).
		Where("project_id = ?", projectID).
		Where("source_id = ?", sourceID).
		Where("source_external_id = ?", externalID).
		Where("supersedes_id IS NULL").
		Scan(ctx)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, fmt.Errorf("get head by source: %w", err)
	}
	return &doc, nil
}

// InsertDocument inserts a new document row directly, with no content-hash
// dedup. The caller is responsible for setting ID, ProjectID, and any
// versioning/attribution fields; a zero-valued CanonicalID/Version/SourceStatus
// falls back to the column defaults (uuid_generate_v4() / 1 / 'active').
func (r *Repository) InsertDocument(ctx context.Context, doc *Document) error {
	populateContentHash(doc)
	_, err := r.db.NewInsert().
		Model(doc).
		Exec(ctx)
	if err != nil {
		return fmt.Errorf("insert document: %w", err)
	}
	return nil
}

// CreateVersion appends a new document version as the new HEAD and, in the same
// transaction, points the prior HEAD's supersedes_id forward at the new row —
// mirroring the graph-object versioning convention (HEAD is the row whose
// supersedes_id IS NULL). The caller sets doc.CanonicalID (shared), doc.Version
// (incremented), and doc.ID; doc.SupersedesID must be nil for the new head.
//
// Retired versions stay in kb.documents for history but are deliberately no
// longer chunk-searchable: their chunks are deleted here so stale text cannot be
// retrieved. A re-sync creates fresh chunks for the new HEAD.
func (r *Repository) CreateVersion(ctx context.Context, doc *Document) error {
	populateContentHash(doc)
	return r.db.RunInTx(ctx, nil, func(ctx context.Context, tx bun.Tx) error {
		// Retire the current HEAD by pointing it at the new version. This frees
		// the partial unique index slot (one HEAD per canonical) before the new
		// HEAD is inserted, avoiding a two-HEAD window. The FK on supersedes_id
		// is DEFERRABLE so pointing at the not-yet-inserted row is allowed until
		// commit.
		_, err := tx.NewUpdate().
			Model((*Document)(nil)).
			Set("supersedes_id = ?", doc.ID).
			Where("canonical_id = ?", doc.CanonicalID).
			Where("project_id = ?", doc.ProjectID).
			Where("supersedes_id IS NULL").
			Exec(ctx)
		if err != nil {
			return fmt.Errorf("retire prior head: %w", err)
		}

		// The just-retired row now points at doc.ID. Delete its chunks in the
		// same transaction: the FK cascade releases object_chunks,
		// chunk_revisions, and chunk_embedding_jobs. The row itself is retained
		// for version history.
		_, err = tx.NewDelete().
			TableExpr("kb.chunks").
			Where("document_id IN (SELECT id FROM kb.documents WHERE canonical_id = ? AND project_id = ? AND supersedes_id = ?)", doc.CanonicalID, doc.ProjectID, doc.ID).
			Exec(ctx)
		if err != nil {
			return fmt.Errorf("delete retired version chunks: %w", err)
		}

		if _, err := tx.NewInsert().Model(doc).Exec(ctx); err != nil {
			return fmt.Errorf("insert version: %w", err)
		}
		return nil
	})
}

// ListVersionsByCanonical returns every version of a canonical document,
// ordered by version ascending (v1 first). Deleted-from-source rows are
// included: history survives a source removal.
func (r *Repository) ListVersionsByCanonical(ctx context.Context, projectID, canonicalID string) ([]Document, error) {
	var docs []Document
	err := r.db.NewSelect().
		Model(&docs).
		Where("project_id = ?", projectID).
		Where("canonical_id = ?", canonicalID).
		Order("version ASC").
		Scan(ctx)
	if err != nil && err != sql.ErrNoRows {
		return nil, fmt.Errorf("list versions by canonical: %w", err)
	}
	return docs, nil
}

// versionExtractionSummary is the per-version extraction rollup returned by
// ListVersionExtractionSummaries, keyed by the version row's document id.
type versionExtractionSummary struct {
	DocumentID       string     `bun:"document_id"`
	ExtractionJobs   int        `bun:"extraction_jobs"`
	ObjectsCreated   int        `bun:"objects_created"`
	LastExtractionAt *time.Time `bun:"last_extraction_at"`
}

// ListVersionExtractionSummaries batches the extraction-job rollup for many
// document version rows in one query: job count, total objects created, and the
// most recent job creation timestamp per version. Returns an empty map (not an
// error) when versionIDs is empty.
func (r *Repository) ListVersionExtractionSummaries(ctx context.Context, projectID string, versionIDs []string) (map[string]versionExtractionSummary, error) {
	if len(versionIDs) == 0 {
		return nil, nil
	}
	var rows []versionExtractionSummary
	err := r.db.NewSelect().
		TableExpr("kb.object_extraction_jobs").
		Column("document_id").
		ColumnExpr("COUNT(*)::int AS extraction_jobs").
		ColumnExpr("COALESCE(SUM(objects_created), 0)::int AS objects_created").
		ColumnExpr("MAX(created_at) AS last_extraction_at").
		Where("project_id = ?", projectID).
		Where("document_id IN (?)", bun.In(versionIDs)).
		Group("document_id").
		Scan(ctx, &rows)
	if err != nil && err != sql.ErrNoRows {
		return nil, fmt.Errorf("list version extraction summaries: %w", err)
	}
	out := make(map[string]versionExtractionSummary, len(rows))
	for _, row := range rows {
		out[row.DocumentID] = row
	}
	return out, nil
}

// UpdateSourceState updates the source revision pointer, parsed-text hash, and
// sync timestamp on a document (typically the HEAD) without creating a version.
func (r *Repository) UpdateSourceState(ctx context.Context, documentID string, revisionID *string, textHash *string, syncedAt time.Time) error {
	_, err := r.db.NewUpdate().
		Model((*Document)(nil)).
		Set("source_revision_id = ?", revisionID).
		Set("source_text_hash = ?", textHash).
		Set("source_synced_at = ?", syncedAt).
		Set("updated_at = ?", time.Now().UTC()).
		Where("id = ?", documentID).
		Exec(ctx)
	if err != nil {
		return fmt.Errorf("update source state: %w", err)
	}
	return nil
}

// MarkSourceDeleted marks a document as removed at the source, retaining the
// row and its history.
func (r *Repository) MarkSourceDeleted(ctx context.Context, documentID string, deletedAt time.Time) error {
	_, err := r.db.NewUpdate().
		Model((*Document)(nil)).
		Set("source_status = ?", SourceStatusDeletedFromSource).
		Set("source_deleted_at = ?", deletedAt).
		Set("updated_at = ?", time.Now().UTC()).
		Where("id = ?", documentID).
		Exec(ctx)
	if err != nil {
		return fmt.Errorf("mark source deleted: %w", err)
	}
	return nil
}

// MarkSourceActive re-activates a previously deleted-from-source document.
func (r *Repository) MarkSourceActive(ctx context.Context, documentID string) error {
	_, err := r.db.NewUpdate().
		Model((*Document)(nil)).
		Set("source_status = ?", SourceStatusActive).
		Set("source_deleted_at = NULL").
		Set("updated_at = ?", time.Now().UTC()).
		Where("id = ?", documentID).
		Exec(ctx)
	if err != nil {
		return fmt.Errorf("mark source active: %w", err)
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

// documentExtractionBranchIDs returns the unmerged extraction branches a
// document's extraction jobs created, in either link shape the jobs use: a branch
// that names the job through origin_id, or an older branch a job still points at
// through staging_branch_id. Deleting a document deletes its jobs, and nothing
// else follows the link: origin_id has no foreign key, so the branch (and its
// staged objects) survived and kept showing in the Objects list as if the
// extraction were still pending (#246). The same two shapes the read boundary
// treats as "source deleted" are collected here, so cleanup and reads cannot
// disagree about which branches belong to the document.
func documentExtractionBranchIDs(ctx context.Context, db bun.IDB, projectID string, jobIDs []string) ([]string, error) {
	if len(jobIDs) == 0 {
		return nil, nil
	}
	var branchIDs []string
	err := db.NewSelect().
		TableExpr("kb.branches AS b").
		Column("b.id").
		Where("b.project_id = ?", projectID).
		Where("b.origin = 'extraction'").
		Where("b.merged_at IS NULL").
		Where("(b.origin_id IN (?) OR (b.origin_id IS NULL AND b.id IN (SELECT j.staging_branch_id FROM kb.object_extraction_jobs j WHERE j.id IN (?) AND j.staging_branch_id IS NOT NULL)))",
			bun.In(jobIDs), bun.In(jobIDs)).
		Scan(ctx, &branchIDs)
	if err != nil && err != sql.ErrNoRows {
		return nil, fmt.Errorf("resolve extraction branches: %w", err)
	}
	return branchIDs, nil
}

// deleteDocumentExtractionBranches removes those branches and everything staged
// on them. Merged branches are left alone: their content was merged into the main
// graph and the attribution cascade above already removes the rows it can
// attribute to the jobs, so the branch row is merge history rather than pending
// work.
func deleteDocumentExtractionBranches(ctx context.Context, db bun.IDB, projectID string, jobIDs []string) (branches, objects, relationships int, err error) {
	branchIDs, err := documentExtractionBranchIDs(ctx, db, projectID, jobIDs)
	if err != nil || len(branchIDs) == 0 {
		return 0, 0, 0, err
	}

	// Relationships first: they reference the objects by canonical id.
	result, err := db.NewDelete().
		TableExpr("kb.graph_relationships").
		Where("project_id = ?", projectID).
		Where("branch_id IN (?)", bun.In(branchIDs)).
		Exec(ctx)
	if err != nil {
		return 0, 0, 0, fmt.Errorf("delete branch relationships: %w", err)
	}
	if n, _ := result.RowsAffected(); n > 0 {
		relationships = int(n)
	}

	// Remember the rows first: a surviving version (a branch forked off this one)
	// may point at them through supersedes_id, and a version chain must not carry a
	// reference to a row that no longer exists.
	var objectIDs []string
	if err := db.NewSelect().
		TableExpr("kb.graph_objects").
		Column("id").
		Where("project_id = ?", projectID).
		Where("branch_id IN (?)", bun.In(branchIDs)).
		Scan(ctx, &objectIDs); err != nil && err != sql.ErrNoRows {
		return 0, 0, 0, fmt.Errorf("load branch objects: %w", err)
	}

	result, err = db.NewDelete().
		TableExpr("kb.graph_objects").
		Where("project_id = ?", projectID).
		Where("branch_id IN (?)", bun.In(branchIDs)).
		Exec(ctx)
	if err != nil {
		return 0, 0, 0, fmt.Errorf("delete branch objects: %w", err)
	}
	if n, _ := result.RowsAffected(); n > 0 {
		objects = int(n)
	}
	if len(objectIDs) > 0 {
		if _, err := db.NewUpdate().
			TableExpr("kb.graph_objects").
			Set("supersedes_id = NULL").
			Where("project_id = ?", projectID).
			Where("supersedes_id IN (?)", bun.In(objectIDs)).
			Exec(ctx); err != nil {
			return 0, 0, 0, fmt.Errorf("detach superseded branch objects: %w", err)
		}
	}

	// Lineage rows and child links are metadata; drop the former and detach the
	// latter so no branch is left pointing at a branch that no longer exists.
	if _, err := db.NewDelete().
		TableExpr("kb.branch_lineage").
		Where("branch_id IN (?) OR ancestor_branch_id IN (?)", bun.In(branchIDs), bun.In(branchIDs)).
		Exec(ctx); err != nil {
		return 0, 0, 0, fmt.Errorf("delete branch lineage: %w", err)
	}
	if _, err := db.NewUpdate().
		TableExpr("kb.branches").
		Set("parent_branch_id = NULL").
		Where("project_id = ?", projectID).
		Where("parent_branch_id IN (?)", bun.In(branchIDs)).
		Exec(ctx); err != nil {
		return 0, 0, 0, fmt.Errorf("detach child branches: %w", err)
	}

	result, err = db.NewDelete().
		TableExpr("kb.branches").
		Where("project_id = ?", projectID).
		Where("id IN (?)", bun.In(branchIDs)).
		Exec(ctx)
	if err != nil {
		return 0, 0, 0, fmt.Errorf("delete branches: %w", err)
	}
	if n, _ := result.RowsAffected(); n > 0 {
		branches = int(n)
	}
	return branches, objects, relationships, nil
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

			// 5b. Delete the branches these jobs created, with anything staged on
			// them. Must run before the jobs, which are what link a branch to the
			// document (#246).
			branches, branchObjects, branchRelationships, err := deleteDocumentExtractionBranches(ctx, tx, projectID, jobIDs)
			if err != nil {
				return err
			}
			summary.Branches += branches
			summary.GraphObjects += branchObjects
			summary.GraphRelationships += branchRelationships

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

			// 5b. Delete the branches these jobs created, with anything staged on
			// them. Must run before the jobs, which are what link a branch to the
			// document (#246).
			branches, branchObjects, branchRelationships, err := deleteDocumentExtractionBranches(ctx, tx, projectID, jobIDs)
			if err != nil {
				return err
			}
			summary.Branches += branches
			summary.GraphObjects += branchObjects
			summary.GraphRelationships += branchRelationships

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

// ParseCursor decodes a base64-encoded cursor and normalizes it to the keyset
// form. Cursors minted before the sort discriminator existed carry only
// createdAt + id; those are read as sort=created cursors so links created before
// a deploy keep working.
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

	if cursor.Sort == "" {
		if cursor.CreatedAt == nil {
			return nil, fmt.Errorf("invalid cursor: missing sort key")
		}
		cursor.Sort = SortCreated
		cursor.Value = *cursor.CreatedAt
	}
	if cursor.Sort != SortCreated && cursor.Sort != SortUpdated {
		return nil, fmt.Errorf("invalid cursor: unknown sort %q", cursor.Sort)
	}
	if cursor.ID == "" || cursor.Value.IsZero() {
		return nil, fmt.Errorf("invalid cursor: incomplete keyset")
	}
	// The legacy field is normalized away so callers never branch on it.
	cursor.CreatedAt = nil

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

	// The pending extraction branches the cascade removes with the jobs.
	branchIDs, err := documentExtractionBranchIDs(ctx, r.db, projectID, jobIDs)
	if err != nil {
		return nil, err
	}

	// Count the graph rows deletion will remove: the rows attributable to the jobs
	// plus everything staged on the branches, each row once (the cascade deletes
	// attribution rows first, so the union is exactly what it removes).
	objectsCount, err := r.db.NewSelect().
		TableExpr("kb.graph_objects").
		Where("project_id = ?", projectID).
		Where("(id IN (?) OR branch_id IN (?))", bun.In(objectRowIDs), bun.In(branchIDs)).
		Count(ctx)
	if err != nil {
		return nil, fmt.Errorf("count objects: %w", err)
	}
	relationshipsCount, err := r.db.NewSelect().
		TableExpr("kb.graph_relationships").
		Where("(src_id IN (?) OR dst_id IN (?) OR branch_id IN (?))",
			bun.In(canonicalIDs), bun.In(canonicalIDs), bun.In(branchIDs)).
		Count(ctx)
	if err != nil {
		return nil, fmt.Errorf("count relationships: %w", err)
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
			Branches:           len(branchIDs),
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
	var unionObjectRowIDs []string
	if len(allJobIDs) > 0 {
		unionRows, err = r.loadGraphVersionRows(ctx, r.db, projectID, allJobIDs)
		if err != nil {
			return nil, err
		}
		unionObjectRowIDs, unionCanonicalIDs = resolveGraphRemovals(unionRows, allJobIDs)
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

	// Pending extraction branches, in both link shapes the jobs use: a branch that
	// names the job through origin_id, or an older branch a job points at through
	// staging_branch_id. A branch with a live origin_id is never matched through a
	// staging link — the same rule pkg/branchsql applies to reads.
	type branchLink struct {
		BranchID   string `bun:"branch_id"`
		DocumentID string `bun:"document_id"`
	}
	var branchLinks []branchLink
	if len(existingIDs) > 0 {
		err = r.db.NewSelect().
			TableExpr("kb.branches AS b").
			ColumnExpr("b.id AS branch_id, j.document_id AS document_id").
			Join("JOIN kb.object_extraction_jobs AS j ON (j.id = b.origin_id OR (b.origin_id IS NULL AND j.staging_branch_id = b.id))").
			Where("b.project_id = ?", projectID).
			Where("b.origin = 'extraction'").
			Where("b.merged_at IS NULL").
			Where("j.document_id IN (?)", bun.In(existingIDs)).
			Distinct().
			Scan(ctx, &branchLinks)
		if err != nil && err != sql.ErrNoRows {
			return nil, fmt.Errorf("resolve extraction branches: %w", err)
		}
	}
	branchIDsByDocument := make(map[string][]string)
	branchIDSet := make(map[string]bool)
	for _, link := range branchLinks {
		branchIDsByDocument[link.DocumentID] = append(branchIDsByDocument[link.DocumentID], link.BranchID)
		branchIDSet[link.BranchID] = true
	}
	unionBranchIDs := make([]string, 0, len(branchIDSet))
	for id := range branchIDSet {
		unionBranchIDs = append(unionBranchIDs, id)
	}

	// The rows staged on those branches. The branch cleanup removes them after the
	// attribution pass, so a row is counted once when it matches either.
	type branchObjectRow struct {
		ID       string `bun:"id"`
		BranchID string `bun:"branch_id"`
	}
	var branchObjectRows []branchObjectRow
	type branchRelationshipRow struct {
		BranchID string `bun:"branch_id"`
		SrcID    string `bun:"src_id"`
		DstID    string `bun:"dst_id"`
	}
	var branchRelationshipRows []branchRelationshipRow
	if len(unionBranchIDs) > 0 {
		err = r.db.NewSelect().
			TableExpr("kb.graph_objects").
			Column("id", "branch_id").
			Where("project_id = ?", projectID).
			Where("branch_id IN (?)", bun.In(unionBranchIDs)).
			Scan(ctx, &branchObjectRows)
		if err != nil && err != sql.ErrNoRows {
			return nil, fmt.Errorf("load branch objects: %w", err)
		}
		err = r.db.NewSelect().
			TableExpr("kb.graph_relationships").
			Column("branch_id", "src_id", "dst_id").
			Where("project_id = ?", projectID).
			Where("branch_id IN (?)", bun.In(unionBranchIDs)).
			Scan(ctx, &branchRelationshipRows)
		if err != nil && err != sql.ErrNoRows {
			return nil, fmt.Errorf("load branch relationships: %w", err)
		}
	}

	// Union totals: branch rows that the attribution pass does not already remove.
	unionObjectSet := make(map[string]bool, len(unionObjectRowIDs))
	for _, id := range unionObjectRowIDs {
		unionObjectSet[id] = true
	}
	for _, row := range branchObjectRows {
		if !unionObjectSet[row.ID] {
			totalObjects++
		}
	}
	unionCanonicalSet := make(map[string]bool, len(unionCanonicalIDs))
	for _, id := range unionCanonicalIDs {
		unionCanonicalSet[id] = true
	}
	for _, rel := range branchRelationshipRows {
		if !unionCanonicalSet[rel.SrcID] && !unionCanonicalSet[rel.DstID] {
			totalRelationships++
		}
	}

	// Per-document graph impact. Each entry reflects deleting that document
	// alone, so entries for documents sharing entities may not sum to the
	// summary totals above.
	docObjects := make(map[string]int)
	docRelationships := make(map[string]int)
	branchesMap := make(map[string]int)
	for _, doc := range docs {
		jobIDs := docJobIDs[doc.ID]
		docBranches := branchIDsByDocument[doc.ID]
		branchesMap[doc.ID] = len(docBranches)
		if len(jobIDs) == 0 {
			continue
		}
		objectRowIDs, canonicalIDs := resolveGraphRemovals(unionRows, jobIDs)
		objectRowSet := make(map[string]bool, len(objectRowIDs))
		for _, id := range objectRowIDs {
			objectRowSet[id] = true
		}
		docBranchSet := make(map[string]bool, len(docBranches))
		for _, id := range docBranches {
			docBranchSet[id] = true
		}

		objects := len(objectRowIDs)
		for _, row := range branchObjectRows {
			if docBranchSet[row.BranchID] && !objectRowSet[row.ID] {
				objects++
			}
		}
		docObjects[doc.ID] = objects

		canonicalSet := make(map[string]bool, len(canonicalIDs))
		for _, c := range canonicalIDs {
			canonicalSet[c] = true
		}
		relationships := countRelationshipsTouching(relEndpoints, canonicalSet)
		for _, rel := range branchRelationshipRows {
			if docBranchSet[rel.BranchID] && !canonicalSet[rel.SrcID] && !canonicalSet[rel.DstID] {
				relationships++
			}
		}
		docRelationships[doc.ID] = relationships
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
			Branches:           branchesMap[doc.ID],
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
	totalImpact.Branches = len(unionBranchIDs)
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

// UpdateContentAndStatus updates the document content and conversion status after parsing completes.
// It also refreshes content_hash to sha256(content), so upload/conversion paths
// populate the content identity hash as soon as content exists.
func (r *Repository) UpdateContentAndStatus(ctx context.Context, documentID, content, status string) error {
	now := time.Now().UTC()
	contentHash := computeContentHash(content)
	_, err := r.db.NewUpdate().
		Model((*Document)(nil)).
		Set("content = ?", content).
		Set("content_hash = ?", contentHash).
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
// Precedence: converting > conversion_failed > extracting > extraction_failed > completed > ready_for_extraction.
// A dead_letter extraction job is terminal failure: object extraction never
// writes the intermediate 'failed' state, so omitting dead_letter here would
// report a permanently failed document as ready.
func processingStatusSQL() string {
	return processingStatusExpr() + " AS processing_status"
}

// processingStatusExpr returns the derived-status CASE without its column alias,
// so the same expression can be used in projections, WHERE clauses and GROUP BY
// aggregates. Every consumer shares this one definition: a status filter or a
// status count derived from a hand-written copy would inevitably disagree with
// the badge the row renders.
func processingStatusExpr() string {
	return `CASE
		WHEN EXISTS (
			SELECT 1 FROM kb.document_parsing_jobs dpj
			WHERE dpj.document_id = d.id AND dpj.status IN ('pending', 'processing', 'retry_pending')
		) THEN 'converting'
		WHEN d.conversion_status = 'failed' THEN 'conversion_failed'
		WHEN EXISTS (
			SELECT 1 FROM kb.object_extraction_jobs oej
			WHERE oej.document_id = d.id AND oej.status IN ('pending', 'processing')
		) THEN 'extracting'
		WHEN (
			SELECT oej2.status FROM kb.object_extraction_jobs oej2
			WHERE oej2.document_id = d.id ORDER BY oej2.created_at DESC LIMIT 1
		) IN ('failed', 'dead_letter') THEN 'extraction_failed'
		WHEN (
			SELECT oej3.status FROM kb.object_extraction_jobs oej3
			WHERE oej3.document_id = d.id ORDER BY oej3.created_at DESC LIMIT 1
		) = 'completed' THEN 'completed'
		ELSE 'ready_for_extraction'
	END`
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

// UserDisplayName returns a user's display name, or "" when the profile has
// none. It is used to attribute a document lifecycle notification to the member
// who produced it; an unknown user is not an error, because the notification is
// still worth delivering without a name.
func (r *Repository) UserDisplayName(ctx context.Context, userID string) (string, error) {
	var name string
	err := r.db.NewRaw(
		`SELECT COALESCE(display_name, '') FROM core.user_profiles WHERE id = ?`, userID,
	).Scan(ctx, &name)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return "", nil
		}
		return "", fmt.Errorf("look up user display name: %w", err)
	}
	return name, nil
}
