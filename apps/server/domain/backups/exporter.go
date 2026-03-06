package backups

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"

	"github.com/uptrace/bun"
)

// Exporter handles exporting database data to NDJSON format
type Exporter struct {
	db  *bun.DB
	log *slog.Logger
}

// NewExporter creates a new database exporter
func NewExporter(db *bun.DB, log *slog.Logger) *Exporter {
	return &Exporter{
		db:  db,
		log: log.With(slog.String("component", "backups.exporter")),
	}
}

// ExportOptions configures what data to export
type ExportOptions struct {
	ProjectID      string
	IncludeChat    bool
	IncludeDeleted bool
}

// ExportResult contains statistics about the export
type ExportResult struct {
	Documents          int `json:"documents"`
	Chunks             int `json:"chunks"`
	GraphObjects       int `json:"graphObjects"`
	GraphRelationships int `json:"graphRelationships"`
	ChatConversations  int `json:"chatConversations"`
	ChatMessages       int `json:"chatMessages"`
	ExtractionJobs     int `json:"extractionJobs"`
	ProjectMemberships int `json:"projectMemberships"`
}

// ExportDocuments exports documents to NDJSON format
func (e *Exporter) ExportDocuments(ctx context.Context, w io.Writer, opts ExportOptions) (int, error) {
	query := e.db.NewSelect().
		Table("kb.documents").
		Where("project_id = ?", opts.ProjectID)

	if !opts.IncludeDeleted {
		query = query.Where("deleted_at IS NULL")
	}

	return e.streamQuery(ctx, query, w, "documents")
}

// ExportChunks exports chunks to NDJSON format
func (e *Exporter) ExportChunks(ctx context.Context, w io.Writer, opts ExportOptions) (int, error) {
	// Join with documents to filter by project
	query := e.db.NewSelect().
		Table("kb.chunks").
		Join("INNER JOIN kb.documents d ON d.id = kb.chunks.document_id").
		Where("d.project_id = ?", opts.ProjectID)

	if !opts.IncludeDeleted {
		query = query.Where("kb.chunks.deleted_at IS NULL")
	}

	// Select all chunk columns
	query = query.Column("kb.chunks.*")

	return e.streamQuery(ctx, query, w, "chunks")
}

// ExportGraphObjects exports graph objects to NDJSON format
func (e *Exporter) ExportGraphObjects(ctx context.Context, w io.Writer, opts ExportOptions) (int, error) {
	query := e.db.NewSelect().
		Table("kb.graph_objects").
		Where("project_id = ?", opts.ProjectID)

	if !opts.IncludeDeleted {
		query = query.Where("deleted_at IS NULL")
	}

	return e.streamQuery(ctx, query, w, "graph_objects")
}

// ExportGraphRelationships exports graph relationships to NDJSON format
func (e *Exporter) ExportGraphRelationships(ctx context.Context, w io.Writer, opts ExportOptions) (int, error) {
	// Join with graph_objects to filter by project
	query := e.db.NewSelect().
		Table("kb.graph_relationships").
		Join("INNER JOIN kb.graph_objects o ON o.id = kb.graph_relationships.source_object_id").
		Where("o.project_id = ?", opts.ProjectID)

	if !opts.IncludeDeleted {
		query = query.Where("kb.graph_relationships.deleted_at IS NULL")
	}

	// Select all relationship columns
	query = query.Column("kb.graph_relationships.*")

	return e.streamQuery(ctx, query, w, "graph_relationships")
}

// ExportChatConversations exports chat conversations to NDJSON format
func (e *Exporter) ExportChatConversations(ctx context.Context, w io.Writer, opts ExportOptions) (int, error) {
	if !opts.IncludeChat {
		return 0, nil
	}

	query := e.db.NewSelect().
		Table("kb.chat_conversations").
		Where("project_id = ?", opts.ProjectID)

	if !opts.IncludeDeleted {
		query = query.Where("deleted_at IS NULL")
	}

	return e.streamQuery(ctx, query, w, "chat_conversations")
}

// ExportChatMessages exports chat messages to NDJSON format
func (e *Exporter) ExportChatMessages(ctx context.Context, w io.Writer, opts ExportOptions) (int, error) {
	if !opts.IncludeChat {
		return 0, nil
	}

	// Join with conversations to filter by project
	query := e.db.NewSelect().
		Table("kb.chat_messages").
		Join("INNER JOIN kb.chat_conversations c ON c.id = kb.chat_messages.conversation_id").
		Where("c.project_id = ?", opts.ProjectID)

	if !opts.IncludeDeleted {
		query = query.Where("kb.chat_messages.deleted_at IS NULL")
	}

	// Select all message columns
	query = query.Column("kb.chat_messages.*")

	return e.streamQuery(ctx, query, w, "chat_messages")
}

// ExportExtractionJobs exports extraction jobs to NDJSON format
func (e *Exporter) ExportExtractionJobs(ctx context.Context, w io.Writer, opts ExportOptions) (int, error) {
	query := e.db.NewSelect().
		Table("kb.object_extraction_jobs").
		Where("project_id = ?", opts.ProjectID)

	// Only export completed jobs
	query = query.Where("status IN ('completed', 'failed')")

	return e.streamQuery(ctx, query, w, "extraction_jobs")
}

// ExportProjectMemberships exports project memberships to NDJSON format
func (e *Exporter) ExportProjectMemberships(ctx context.Context, w io.Writer, opts ExportOptions) (int, error) {
	query := e.db.NewSelect().
		Table("kb.project_memberships").
		Where("project_id = ?", opts.ProjectID)

	return e.streamQuery(ctx, query, w, "project_memberships")
}

// streamQuery executes a query and streams results as NDJSON
func (e *Exporter) streamQuery(ctx context.Context, query *bun.SelectQuery, w io.Writer, tableName string) (int, error) {
	encoder := json.NewEncoder(w)
	count := 0
	const batchSize = 1000

	var offset int
	for {
		// Fetch batch
		var rows []map[string]any
		err := query.
			Limit(batchSize).
			Offset(offset).
			Scan(ctx, &rows)

		if err != nil {
			e.log.Error("failed to export table",
				slog.String("table", tableName),
				slog.Int("offset", offset),
				slog.Any("error", err),
			)
			return count, fmt.Errorf("export %s: %w", tableName, err)
		}

		// No more rows
		if len(rows) == 0 {
			break
		}

		// Write each row as NDJSON
		for _, row := range rows {
			if err := encoder.Encode(row); err != nil {
				e.log.Error("failed to encode row",
					slog.String("table", tableName),
					slog.Any("error", err),
				)
				return count, fmt.Errorf("encode %s row: %w", tableName, err)
			}
			count++
		}

		offset += batchSize

		// Check for cancellation
		select {
		case <-ctx.Done():
			return count, ctx.Err()
		default:
		}
	}

	e.log.Debug("exported table",
		slog.String("table", tableName),
		slog.Int("rows", count),
	)

	return count, nil
}

// ExportAll exports all project data and returns statistics
func (e *Exporter) ExportAll(ctx context.Context, writers map[string]io.Writer, opts ExportOptions) (*ExportResult, error) {
	result := &ExportResult{}

	var err error

	// Export documents
	if w, ok := writers["documents"]; ok {
		result.Documents, err = e.ExportDocuments(ctx, w, opts)
		if err != nil {
			return result, fmt.Errorf("export documents: %w", err)
		}
	}

	// Export chunks
	if w, ok := writers["chunks"]; ok {
		result.Chunks, err = e.ExportChunks(ctx, w, opts)
		if err != nil {
			return result, fmt.Errorf("export chunks: %w", err)
		}
	}

	// Export graph objects
	if w, ok := writers["graph_objects"]; ok {
		result.GraphObjects, err = e.ExportGraphObjects(ctx, w, opts)
		if err != nil {
			return result, fmt.Errorf("export graph objects: %w", err)
		}
	}

	// Export graph relationships
	if w, ok := writers["graph_relationships"]; ok {
		result.GraphRelationships, err = e.ExportGraphRelationships(ctx, w, opts)
		if err != nil {
			return result, fmt.Errorf("export graph relationships: %w", err)
		}
	}

	// Export chat conversations
	if w, ok := writers["chat_conversations"]; ok {
		result.ChatConversations, err = e.ExportChatConversations(ctx, w, opts)
		if err != nil {
			return result, fmt.Errorf("export chat conversations: %w", err)
		}
	}

	// Export chat messages
	if w, ok := writers["chat_messages"]; ok {
		result.ChatMessages, err = e.ExportChatMessages(ctx, w, opts)
		if err != nil {
			return result, fmt.Errorf("export chat messages: %w", err)
		}
	}

	// Export extraction jobs
	if w, ok := writers["extraction_jobs"]; ok {
		result.ExtractionJobs, err = e.ExportExtractionJobs(ctx, w, opts)
		if err != nil {
			return result, fmt.Errorf("export extraction jobs: %w", err)
		}
	}

	// Export project memberships
	if w, ok := writers["project_memberships"]; ok {
		result.ProjectMemberships, err = e.ExportProjectMemberships(ctx, w, opts)
		if err != nil {
			return result, fmt.Errorf("export project memberships: %w", err)
		}
	}

	e.log.Info("export completed",
		slog.String("project_id", opts.ProjectID),
		slog.Int("documents", result.Documents),
		slog.Int("chunks", result.Chunks),
		slog.Int("graph_objects", result.GraphObjects),
		slog.Int("graph_relationships", result.GraphRelationships),
	)

	return result, nil
}
