package processingproblems

import (
	"context"
	"fmt"

	"github.com/uptrace/bun"
)

// Repository runs one read-only query per queue. Every query returns the most
// recent terminal failure per unit minus units that already have an active
// attempt, so a retried unit drops out of the panel as soon as its new run
// starts.
//
// Each query also counts every matching unit (COUNT(*) OVER ()) while returning
// at most problemRowLimit rows, so the report can state the real backlog size
// instead of presenting the display cap as the truth.
type Repository struct {
	db *bun.DB
}

// problemRowLimit is the number of rows one queue contributes to the report.
// The count carried on every row is not capped by it.
const problemRowLimit = 200

// NewRepository creates a repository over the shared database handle.
func NewRepository(db *bun.DB) *Repository {
	return &Repository{db: db}
}

// DB exposes the handle for membership checks in the handler.
func (r *Repository) DB() bun.IDB { return r.db }

// queueQuery is one queue's read model.
type queueQuery struct {
	name string
	run  func(ctx context.Context, projectID string) ([]problemRow, error)
}

// queries is the queue registry. Adding a pipeline means adding one query here
// (and its area mapping), not a second failure UI.
func (r *Repository) queries() []queueQuery {
	return []queueQuery{
		{QueueObjectExtraction, r.objectExtraction},
		{QueueDocumentParsing, r.documentParsing},
		{QueueChunkEmbedding, r.chunkEmbedding},
		{QueueGraphEmbedding, r.graphEmbedding},
		{QueueGraphRelationshipEmbedding, r.graphRelationshipEmbedding},
		{QueueSourceSync, r.sourceSync},
		{QueueEmail, r.email},
		{QueueDiscovery, r.discovery},
		{QueueAgentRun, r.agentRun},
		{QueueBlueprintUpdate, r.blueprintUpdate},
		{QueueSchemaMigration, r.schemaMigration},
	}
}

func (r *Repository) objectExtraction(ctx context.Context, projectID string) ([]problemRow, error) {
	rows := make([]problemRow, 0)
	err := r.db.NewRaw(`
		SELECT 'object_extraction' AS queue, x.id::text AS job_id,
		       COALESCE(x.document_id::text, x.chunk_id::text, x.id::text) AS unit_id,
		       COALESCE(d.filename, 'Chunk extraction') AS label,
		       x.document_id::text AS document_id, NULL::text AS source_id,
		       x.status::text AS status, x.retry_count AS attempts, x.max_retries AS max_attempts,
		       x.error_message, x.updated_at AS occurred_at,
		       COUNT(*) OVER () AS total_rows
		FROM kb.object_extraction_jobs x
		LEFT JOIN kb.documents d ON d.id = x.document_id
		WHERE x.project_id = ? AND x.status IN ('failed', 'dead_letter')
		  AND x.id IN (
		      SELECT DISTINCT ON (COALESCE(y.document_id::text, y.chunk_id::text, y.id::text)) y.id
		      FROM kb.object_extraction_jobs y
		      WHERE y.project_id = ?
		      ORDER BY COALESCE(y.document_id::text, y.chunk_id::text, y.id::text), y.created_at DESC, y.id DESC
		  )
		  AND NOT EXISTS (
		      SELECT 1 FROM kb.object_extraction_jobs a
		      WHERE a.project_id = x.project_id
		        AND a.status IN ('pending', 'processing')
		        AND COALESCE(a.document_id::text, a.chunk_id::text, a.id::text) =
		            COALESCE(x.document_id::text, x.chunk_id::text, x.id::text)
		  )
		ORDER BY x.updated_at DESC, x.id DESC
		LIMIT ?`, projectID, projectID, problemRowLimit).Scan(ctx, &rows)
	if err != nil {
		return nil, fmt.Errorf("object extraction problems: %w", err)
	}
	return rows, nil
}

func (r *Repository) documentParsing(ctx context.Context, projectID string) ([]problemRow, error) {
	rows := make([]problemRow, 0)
	err := r.db.NewRaw(`
		SELECT 'document_parsing' AS queue, x.id::text AS job_id,
		       COALESCE(x.document_id::text, x.id::text) AS unit_id,
		       COALESCE(x.source_filename, d.filename, 'Document conversion') AS label,
		       x.document_id::text AS document_id, NULL::text AS source_id,
		       x.status::text AS status, x.retry_count AS attempts, x.max_retries AS max_attempts,
		       x.error_message, x.updated_at AS occurred_at,
		       COUNT(*) OVER () AS total_rows
		FROM kb.document_parsing_jobs x
		LEFT JOIN kb.documents d ON d.id = x.document_id
		WHERE x.project_id = ? AND x.status IN ('failed', 'dead_letter')
		  AND x.id IN (
		      SELECT DISTINCT ON (COALESCE(y.document_id::text, y.id::text)) y.id
		      FROM kb.document_parsing_jobs y
		      WHERE y.project_id = ?
		      ORDER BY COALESCE(y.document_id::text, y.id::text), y.created_at DESC, y.id DESC
		  )
		  AND NOT EXISTS (
		      SELECT 1 FROM kb.document_parsing_jobs a
		      WHERE a.project_id = x.project_id AND a.status IN ('pending', 'processing', 'retry_pending')
		        AND COALESCE(a.document_id::text, a.id::text) = COALESCE(x.document_id::text, x.id::text)
		  )
		ORDER BY x.updated_at DESC, x.id DESC
		LIMIT ?`, projectID, projectID, problemRowLimit).Scan(ctx, &rows)
	if err != nil {
		return nil, fmt.Errorf("document parsing problems: %w", err)
	}
	return rows, nil
}

func (r *Repository) chunkEmbedding(ctx context.Context, projectID string) ([]problemRow, error) {
	rows := make([]problemRow, 0)
	err := r.db.NewRaw(`
		SELECT 'chunk_embedding' AS queue, x.id::text AS job_id,
		       x.chunk_id::text AS unit_id,
		       COALESCE(d.filename, 'Document chunk') AS label,
		       c.document_id::text AS document_id, NULL::text AS source_id,
		       x.status::text AS status, x.attempt_count AS attempts, 0 AS max_attempts,
		       x.last_error AS error_message, x.updated_at AS occurred_at,
		       COUNT(*) OVER () AS total_rows
		FROM kb.chunk_embedding_jobs x
		JOIN kb.chunks c ON c.id = x.chunk_id
		JOIN kb.documents d ON d.id = c.document_id
		WHERE d.project_id = ? AND x.status IN ('failed', 'dead_letter')
		  AND x.id IN (
		      SELECT DISTINCT ON (y.chunk_id) y.id
		      FROM kb.chunk_embedding_jobs y
		      JOIN kb.chunks c2 ON c2.id = y.chunk_id
		      JOIN kb.documents d2 ON d2.id = c2.document_id
		      WHERE d2.project_id = ?
		      ORDER BY y.chunk_id, y.updated_at DESC, y.id DESC
		  )
		  AND NOT EXISTS (
		      SELECT 1 FROM kb.chunk_embedding_jobs a
		      WHERE a.chunk_id = x.chunk_id AND a.status IN ('pending', 'processing')
		  )
		ORDER BY x.updated_at DESC, x.id DESC
		LIMIT ?`, projectID, projectID, problemRowLimit).Scan(ctx, &rows)
	if err != nil {
		return nil, fmt.Errorf("chunk embedding problems: %w", err)
	}
	return rows, nil
}

func (r *Repository) graphEmbedding(ctx context.Context, projectID string) ([]problemRow, error) {
	rows := make([]problemRow, 0)
	err := r.db.NewRaw(`
		SELECT 'graph_embedding' AS queue, x.id::text AS job_id,
		       x.object_id::text AS unit_id,
		       COALESCE(o.key, o.type, 'Graph object') AS label,
		       NULL::text AS document_id, NULL::text AS source_id,
		       x.status::text AS status, x.attempt_count AS attempts, 0 AS max_attempts,
		       x.last_error AS error_message, x.updated_at AS occurred_at,
		       COUNT(*) OVER () AS total_rows
		FROM kb.graph_embedding_jobs x
		JOIN kb.graph_objects o ON o.id = x.object_id
		WHERE o.project_id = ? AND o.deleted_at IS NULL AND x.status IN ('failed', 'dead_letter')
		  AND x.id IN (
		      SELECT DISTINCT ON (y.object_id) y.id
		      FROM kb.graph_embedding_jobs y
		      JOIN kb.graph_objects o2 ON o2.id = y.object_id
		      WHERE o2.project_id = ? AND o2.deleted_at IS NULL
		      ORDER BY y.object_id, y.updated_at DESC, y.id DESC
		  )
		  AND NOT EXISTS (
		      SELECT 1 FROM kb.graph_embedding_jobs a
		      WHERE a.object_id = x.object_id AND a.status IN ('pending', 'processing')
		  )
		ORDER BY x.updated_at DESC, x.id DESC
		LIMIT ?`, projectID, projectID, problemRowLimit).Scan(ctx, &rows)
	if err != nil {
		return nil, fmt.Errorf("graph embedding problems: %w", err)
	}
	return rows, nil
}

func (r *Repository) graphRelationshipEmbedding(ctx context.Context, projectID string) ([]problemRow, error) {
	rows := make([]problemRow, 0)
	err := r.db.NewRaw(`
		SELECT 'graph_relationship_embedding' AS queue, x.id::text AS job_id,
		       x.relationship_id::text AS unit_id,
		       COALESCE(gr.type, 'Graph relationship') AS label,
		       NULL::text AS document_id, NULL::text AS source_id,
		       x.status::text AS status, x.attempt_count AS attempts, 0 AS max_attempts,
		       x.last_error AS error_message, x.updated_at AS occurred_at,
		       COUNT(*) OVER () AS total_rows
		FROM kb.graph_relationship_embedding_jobs x
		JOIN kb.graph_relationships gr ON gr.id = x.relationship_id
		WHERE gr.project_id = ? AND gr.deleted_at IS NULL AND x.status IN ('failed', 'dead_letter')
		  AND x.id IN (
		      SELECT DISTINCT ON (y.relationship_id) y.id
		      FROM kb.graph_relationship_embedding_jobs y
		      JOIN kb.graph_relationships gr2 ON gr2.id = y.relationship_id
		      WHERE gr2.project_id = ? AND gr2.deleted_at IS NULL
		      ORDER BY y.relationship_id, y.updated_at DESC, y.id DESC
		  )
		  AND NOT EXISTS (
		      SELECT 1 FROM kb.graph_relationship_embedding_jobs a
		      WHERE a.relationship_id = x.relationship_id AND a.status IN ('pending', 'processing')
		  )
		ORDER BY x.updated_at DESC, x.id DESC
		LIMIT ?`, projectID, projectID, problemRowLimit).Scan(ctx, &rows)
	if err != nil {
		return nil, fmt.Errorf("graph relationship embedding problems: %w", err)
	}
	return rows, nil
}

func (r *Repository) sourceSync(ctx context.Context, projectID string) ([]problemRow, error) {
	rows := make([]problemRow, 0)
	err := r.db.NewRaw(`
		SELECT 'source_sync' AS queue, x.id::text AS job_id,
		       x.source_id::text AS unit_id,
		       COALESCE(s.display_name, s.provider, 'Data source') AS label,
		       NULL::text AS document_id, x.source_id::text AS source_id,
		       x.status::text AS status, x.attempt_count AS attempts, 0 AS max_attempts,
		       COALESCE(x.last_error, NULLIF(x.error::text, 'null')) AS error_message, x.updated_at AS occurred_at,
		       COUNT(*) OVER () AS total_rows
		FROM kb.source_sync_jobs x
		LEFT JOIN kb.sources s ON s.id = x.source_id
		WHERE x.project_id = ? AND x.status = 'failed'
		  AND x.id IN (
		      SELECT DISTINCT ON (y.source_id) y.id
		      FROM kb.source_sync_jobs y
		      WHERE y.project_id = ?
		      ORDER BY y.source_id, y.created_at DESC, y.id DESC
		  )
		  AND NOT EXISTS (
		      SELECT 1 FROM kb.source_sync_jobs a
		      WHERE a.source_id = x.source_id AND a.status IN ('pending', 'processing')
		  )
		ORDER BY x.updated_at DESC, x.id DESC
		LIMIT ?`, projectID, projectID, problemRowLimit).Scan(ctx, &rows)
	if err != nil {
		return nil, fmt.Errorf("source sync problems: %w", err)
	}
	return rows, nil
}

func (r *Repository) email(ctx context.Context, projectID string) ([]problemRow, error) {
	rows := make([]problemRow, 0)
	err := r.db.NewRaw(`
		SELECT 'email' AS queue, x.id::text AS job_id, x.id::text AS unit_id,
		       x.to_email || ' · ' || x.template_name AS label,
		       NULL::text AS document_id, NULL::text AS source_id,
		       x.status::text AS status, x.attempts AS attempts, x.max_attempts AS max_attempts,
		       x.last_error AS error_message, x.created_at AS occurred_at,
		       COUNT(*) OVER () AS total_rows
		FROM kb.email_jobs x
		WHERE x.project_id = ? AND x.status IN ('failed', 'dead_letter')
		  AND x.id IN (
		      SELECT DISTINCT ON (y.to_email, y.template_name) y.id
		      FROM kb.email_jobs y
		      WHERE y.project_id = ?
		      ORDER BY y.to_email, y.template_name, y.created_at DESC, y.id DESC
		  )
		AND NOT EXISTS (
		      SELECT 1 FROM kb.email_jobs a
		      WHERE a.project_id = x.project_id
		        AND a.to_email = x.to_email
		        AND a.template_name = x.template_name
		        AND a.status IN ('pending', 'processing')
		  )
		ORDER BY x.created_at DESC, x.id DESC
		LIMIT ?`, projectID, projectID, problemRowLimit).Scan(ctx, &rows)
	if err != nil {
		return nil, fmt.Errorf("email problems: %w", err)
	}
	return rows, nil
}

func (r *Repository) discovery(ctx context.Context, projectID string) ([]problemRow, error) {
	rows := make([]problemRow, 0)
	err := r.db.NewRaw(`
		SELECT 'discovery' AS queue, x.id::text AS job_id, x.id::text AS unit_id,
		       'Schema discovery' AS label, NULL::text AS document_id, NULL::text AS source_id,
		       x.status, x.retry_count AS attempts, 0 AS max_attempts,
		       x.error_message, x.updated_at AS occurred_at,
		       COUNT(*) OVER () AS total_rows
		FROM kb.discovery_jobs x
		WHERE x.project_id = ? AND x.status = 'failed'
		  AND x.id IN (
		      SELECT DISTINCT ON (y.project_id) y.id
		      FROM kb.discovery_jobs y
		      WHERE y.project_id = ?
		      ORDER BY y.project_id, y.created_at DESC, y.id DESC
		  )
		LIMIT ?`, projectID, projectID, problemRowLimit).Scan(ctx, &rows)
	if err != nil {
		return nil, fmt.Errorf("discovery problems: %w", err)
	}
	return rows, nil
}

func (r *Repository) agentRun(ctx context.Context, projectID string) ([]problemRow, error) {
	rows := make([]problemRow, 0)
	err := r.db.NewRaw(`
		SELECT 'agent_run' AS queue, x.id::text AS job_id, x.run_id::text AS unit_id,
		       COALESCE(a.name, 'Agent') || ' run' AS label,
		       NULL::text AS document_id, NULL::text AS source_id,
		       x.status::text AS status, x.attempt_count AS attempts, x.max_attempts AS max_attempts,
		       ar.error_message, x.created_at AS occurred_at,
		       COUNT(*) OVER () AS total_rows
		FROM kb.agent_run_jobs x
		JOIN kb.agent_runs ar ON ar.id = x.run_id
		JOIN kb.agents a ON a.id = ar.agent_id
		WHERE a.project_id = ? AND x.status = 'failed' AND x.attempt_count >= x.max_attempts
		  AND x.id IN (
		      SELECT DISTINCT ON (y.run_id) y.id
		      FROM kb.agent_run_jobs y
		      JOIN kb.agent_runs ar2 ON ar2.id = y.run_id
		      JOIN kb.agents a2 ON a2.id = ar2.agent_id
		      WHERE a2.project_id = ?
		      ORDER BY y.run_id, y.created_at DESC, y.id DESC
		  )
		ORDER BY x.created_at DESC, x.id DESC
		LIMIT ?`, projectID, projectID, problemRowLimit).Scan(ctx, &rows)
	if err != nil {
		return nil, fmt.Errorf("agent run problems: %w", err)
	}
	return rows, nil
}

func (r *Repository) blueprintUpdate(ctx context.Context, projectID string) ([]problemRow, error) {
	rows := make([]problemRow, 0)
	err := r.db.NewRaw(`
		SELECT 'blueprint_update' AS queue, x.id::text AS job_id, x.id::text AS unit_id,
		       x.blueprint_name || ' · ' || x.action AS label,
		       NULL::text AS document_id, NULL::text AS source_id,
		       x.status, x.attempts AS attempts, 0 AS max_attempts,
		       NULLIF(x.last_error, '') AS error_message, COALESCE(x.finished_at, x.created_at) AS occurred_at,
		       COUNT(*) OVER () AS total_rows
		FROM kb.blueprint_update_jobs x
		WHERE x.project_id = ? AND x.status = 'failed'
		  AND x.id IN (
		      SELECT DISTINCT ON (y.blueprint_name, y.action) y.id
		      FROM kb.blueprint_update_jobs y
		      WHERE y.project_id = ?
		      ORDER BY y.blueprint_name, y.action, y.created_at DESC, y.id DESC
		  )
		ORDER BY occurred_at DESC, x.id DESC
		LIMIT ?`, projectID, projectID, problemRowLimit).Scan(ctx, &rows)
	if err != nil {
		return nil, fmt.Errorf("blueprint update problems: %w", err)
	}
	return rows, nil
}

func (r *Repository) schemaMigration(ctx context.Context, projectID string) ([]problemRow, error) {
	rows := make([]problemRow, 0)
	err := r.db.NewRaw(`
		SELECT 'schema_migration' AS queue, x.id::text AS job_id, x.id::text AS unit_id,
		       'Schema migration' AS label, NULL::text AS document_id, NULL::text AS source_id,
		       x.status, 0 AS attempts, 0 AS max_attempts,
		       NULLIF(x.error, '') AS error_message, COALESCE(x.completed_at, x.created_at) AS occurred_at,
		       COUNT(*) OVER () AS total_rows
		FROM kb.schema_migration_jobs x
		WHERE x.project_id = ? AND x.status = 'failed'
		  AND x.id IN (
		      SELECT DISTINCT ON (y.project_id) y.id
		      FROM kb.schema_migration_jobs y
		      WHERE y.project_id = ?
		      ORDER BY y.project_id, y.created_at DESC, y.id DESC
		  )
		LIMIT ?`, projectID, projectID, problemRowLimit).Scan(ctx, &rows)
	if err != nil {
		return nil, fmt.Errorf("schema migration problems: %w", err)
	}
	return rows, nil
}
