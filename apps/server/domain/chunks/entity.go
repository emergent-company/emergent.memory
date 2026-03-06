package chunks

import (
	"encoding/json"
	"time"

	"github.com/google/uuid"
	"github.com/uptrace/bun"
)

// Chunk represents a chunk of text from a document in the kb.chunks table
type Chunk struct {
	bun.BaseModel `bun:"table:kb.chunks,alias:c"`

	ID          uuid.UUID        `bun:"id,pk,type:uuid,default:uuid_generate_v4()" json:"id"`
	DocumentID  uuid.UUID        `bun:"document_id,type:uuid,notnull" json:"documentId"`
	ChunkIndex  int              `bun:"chunk_index,notnull" json:"chunkIndex"`
	Text        string           `bun:"text,notnull" json:"text"`
	Embedding   []byte           `bun:"embedding,type:vector(768)" json:"-"` // pgvector stored as bytes
	TSV         string           `bun:"tsv,type:tsvector" json:"-"`          // Full-text search vector
	Metadata    *ChunkMetadata   `bun:"metadata,type:jsonb" json:"metadata,omitempty"`
	CreatedAt   time.Time        `bun:"created_at,notnull,default:now()" json:"createdAt"`
	UpdatedAt   time.Time        `bun:"updated_at,notnull,default:now()" json:"updatedAt"`
}

// ChunkMetadata contains metadata about how the chunk was created
type ChunkMetadata struct {
	Strategy     string `json:"strategy,omitempty"`     // character, sentence, paragraph
	StartOffset  int    `json:"startOffset,omitempty"`  // Character offset in original document
	EndOffset    int    `json:"endOffset,omitempty"`    // Character offset in original document
	BoundaryType string `json:"boundaryType,omitempty"` // sentence, paragraph, character, section
}

// ChunkDTO is the response format for chunks
type ChunkDTO struct {
	ID             string         `json:"id"`
	DocumentID     string         `json:"documentId"`
	DocumentTitle  string         `json:"documentTitle,omitempty"`
	Index          int            `json:"index"`
	Size           int            `json:"size"`          // Size in characters
	HasEmbedding   bool           `json:"hasEmbedding"`
	Text           string         `json:"text"`
	CreatedAt      string         `json:"createdAt,omitempty"`
	Metadata       *ChunkMetadata `json:"metadata,omitempty"`
	TotalChars     *int           `json:"totalChars,omitempty"`     // Total chars across all doc chunks
	ChunkCount     *int           `json:"chunkCount,omitempty"`     // Total chunks in document
	EmbeddedChunks *int           `json:"embeddedChunks,omitempty"` // Chunks with embeddings in document
}

// ChunkWithDocInfo is used for queries that join with documents table
type ChunkWithDocInfo struct {
	Chunk
	DocumentFilename  *string `bun:"document_filename"`
	DocumentSourceURL *string `bun:"document_source_url"`
	TotalChars        *int    `bun:"total_chars"`
	ChunkCount        *int    `bun:"chunk_count"`
	EmbeddedChunks    *int    `bun:"embedded_chunks"`
}

// ToDTO converts a ChunkWithDocInfo to a ChunkDTO
func (c *ChunkWithDocInfo) ToDTO() *ChunkDTO {
	// Determine document title from filename or source_url
	title := ""
	if c.DocumentFilename != nil && *c.DocumentFilename != "" {
		title = *c.DocumentFilename
	} else if c.DocumentSourceURL != nil && *c.DocumentSourceURL != "" {
		title = *c.DocumentSourceURL
	}

	return &ChunkDTO{
		ID:             c.ID.String(),
		DocumentID:     c.DocumentID.String(),
		DocumentTitle:  title,
		Index:          c.ChunkIndex,
		Size:           len(c.Text),
		HasEmbedding:   len(c.Embedding) > 0,
		Text:           c.Text,
		CreatedAt:      c.CreatedAt.Format(time.RFC3339),
		Metadata:       c.Metadata,
		TotalChars:     c.TotalChars,
		ChunkCount:     c.ChunkCount,
		EmbeddedChunks: c.EmbeddedChunks,
	}
}

// ListChunksResponse is the response for listing chunks
type ListChunksResponse struct {
	Data       []*ChunkDTO `json:"data"`
	TotalCount int         `json:"totalCount"`
}

// BulkDeleteRequest is the request for bulk deleting chunks
type BulkDeleteRequest struct {
	IDs []string `json:"ids" validate:"required,min=1"`
}

// BulkDeleteByDocumentsRequest is the request for bulk deleting chunks by documents
type BulkDeleteByDocumentsRequest struct {
	DocumentIDs []string `json:"documentIds" validate:"required,min=1"`
}

// DeletionResult represents the result of a single deletion
type DeletionResult struct {
	ID      string `json:"id"`
	Success bool   `json:"success"`
	Error   string `json:"error,omitempty"`
}

// BulkDeletionSummary is the response for bulk deletion
type BulkDeletionSummary struct {
	TotalRequested int               `json:"totalRequested"`
	TotalDeleted   int               `json:"totalDeleted"`
	TotalFailed    int               `json:"totalFailed"`
	Results        []*DeletionResult `json:"results"`
}

// DocumentChunksDeletionResult is the result of deleting all chunks for a document
type DocumentChunksDeletionResult struct {
	DocumentID   string `json:"documentId"`
	ChunksDeleted int    `json:"chunksDeleted"`
	Success      bool   `json:"success"`
	Error        string `json:"error,omitempty"`
}

// BulkDocumentChunksDeletionSummary is the response for bulk document chunks deletion
type BulkDocumentChunksDeletionSummary struct {
	TotalDocuments int                           `json:"totalDocuments"`
	TotalChunks    int                           `json:"totalChunks"`
	Results        []*DocumentChunksDeletionResult `json:"results"`
}

// CreateChunkRequest is the request for creating a chunk (used internally)
type CreateChunkRequest struct {
	DocumentID string         `json:"documentId" validate:"required,uuid"`
	ChunkIndex int            `json:"chunkIndex" validate:"gte=0"`
	Text       string         `json:"text" validate:"required"`
	Metadata   *ChunkMetadata `json:"metadata,omitempty"`
}

// Scan implements the sql.Scanner interface for ChunkMetadata
func (m *ChunkMetadata) Scan(src interface{}) error {
	if src == nil {
		return nil
	}
	switch v := src.(type) {
	case []byte:
		return json.Unmarshal(v, m)
	case string:
		return json.Unmarshal([]byte(v), m)
	}
	return nil
}
