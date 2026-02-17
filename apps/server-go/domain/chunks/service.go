package chunks

import (
	"context"
	"log/slog"

	"github.com/google/uuid"

	"github.com/emergent-company/emergent/pkg/apperror"
	"github.com/emergent-company/emergent/pkg/logger"
)

// Service handles business logic for chunks
type Service struct {
	repo *Repository
	log  *slog.Logger
}

// NewService creates a new chunks service
func NewService(repo *Repository, log *slog.Logger) *Service {
	return &Service{
		repo: repo,
		log:  log.With(logger.Scope("chunks.svc")),
	}
}

// List returns all chunks for a project, optionally filtered by document ID
func (s *Service) List(ctx context.Context, projectID uuid.UUID, documentID *uuid.UUID) (*ListChunksResponse, error) {
	chunks, err := s.repo.ListByProject(ctx, projectID, documentID)
	if err != nil {
		return nil, err
	}

	// Convert to DTOs
	dtos := make([]*ChunkDTO, 0, len(chunks))
	for _, chunk := range chunks {
		dtos = append(dtos, chunk.ToDTO())
	}

	return &ListChunksResponse{
		Data:       dtos,
		TotalCount: len(dtos),
	}, nil
}

// GetByID retrieves a single chunk by ID
func (s *Service) GetByID(ctx context.Context, projectID, chunkID uuid.UUID) (*ChunkDTO, error) {
	chunk, err := s.repo.GetByID(ctx, projectID, chunkID)
	if err != nil {
		return nil, err
	}

	return chunk.ToDTO(), nil
}

// Delete deletes a chunk by ID
func (s *Service) Delete(ctx context.Context, projectID, chunkID uuid.UUID) error {
	return s.repo.Delete(ctx, projectID, chunkID)
}

// BulkDelete deletes multiple chunks by IDs
func (s *Service) BulkDelete(ctx context.Context, projectID uuid.UUID, chunkIDs []string) (*BulkDeletionSummary, error) {
	if len(chunkIDs) == 0 {
		return nil, apperror.NewBadRequest("ids array cannot be empty")
	}

	// Parse UUIDs
	uuids := make([]uuid.UUID, 0, len(chunkIDs))
	for _, id := range chunkIDs {
		parsed, err := uuid.Parse(id)
		if err != nil {
			return nil, apperror.NewBadRequest("invalid chunk ID: " + id)
		}
		uuids = append(uuids, parsed)
	}

	return s.repo.BulkDelete(ctx, projectID, uuids)
}

// DeleteByDocument deletes all chunks for a document
func (s *Service) DeleteByDocument(ctx context.Context, projectID, documentID uuid.UUID) (*DocumentChunksDeletionResult, error) {
	return s.repo.DeleteByDocument(ctx, projectID, documentID)
}

// BulkDeleteByDocuments deletes all chunks for multiple documents
func (s *Service) BulkDeleteByDocuments(ctx context.Context, projectID uuid.UUID, documentIDs []string) (*BulkDocumentChunksDeletionSummary, error) {
	if len(documentIDs) == 0 {
		return nil, apperror.NewBadRequest("documentIds array cannot be empty")
	}

	// Parse UUIDs
	uuids := make([]uuid.UUID, 0, len(documentIDs))
	for _, id := range documentIDs {
		parsed, err := uuid.Parse(id)
		if err != nil {
			return nil, apperror.NewBadRequest("invalid document ID: " + id)
		}
		uuids = append(uuids, parsed)
	}

	return s.repo.BulkDeleteByDocuments(ctx, projectID, uuids)
}

// Create creates a new chunk (used internally, e.g., by extraction pipeline)
func (s *Service) Create(ctx context.Context, req *CreateChunkRequest) (*Chunk, error) {
	docID, err := uuid.Parse(req.DocumentID)
	if err != nil {
		return nil, apperror.NewBadRequest("invalid document ID")
	}

	chunk := &Chunk{
		ID:         uuid.New(),
		DocumentID: docID,
		ChunkIndex: req.ChunkIndex,
		Text:       req.Text,
		Metadata:   req.Metadata,
	}

	if err := s.repo.Create(ctx, chunk); err != nil {
		return nil, err
	}

	return chunk, nil
}

// CreateBatch creates multiple chunks in a batch (used by extraction pipeline)
func (s *Service) CreateBatch(ctx context.Context, documentID uuid.UUID, chunks []CreateChunkRequest) error {
	if len(chunks) == 0 {
		return nil
	}

	entities := make([]*Chunk, 0, len(chunks))
	for _, req := range chunks {
		entities = append(entities, &Chunk{
			ID:         uuid.New(),
			DocumentID: documentID,
			ChunkIndex: req.ChunkIndex,
			Text:       req.Text,
			Metadata:   req.Metadata,
		})
	}

	return s.repo.CreateBatch(ctx, entities)
}

// UpdateEmbedding updates the embedding for a chunk
func (s *Service) UpdateEmbedding(ctx context.Context, chunkID uuid.UUID, embedding []float32) error {
	if len(embedding) != 768 {
		return apperror.NewBadRequest("embedding must be 768 dimensions")
	}

	return s.repo.UpdateEmbedding(ctx, chunkID, embedding)
}

// CountByDocument returns the number of chunks for a document
func (s *Service) CountByDocument(ctx context.Context, documentID uuid.UUID) (int, error) {
	return s.repo.CountByDocument(ctx, documentID)
}
