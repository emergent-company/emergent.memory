package chunks

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/google/uuid"

	"github.com/emergent-company/emergent.memory/pkg/apperror"
	"github.com/emergent-company/emergent.memory/pkg/auth"
)

// MaxChunkTextBytes is the maximum accepted size of a single chunk's text.
// It mirrors the gateway's 4 MiB request cap so a direct API call cannot
// bypass the limit enforced at the transport edge.
const MaxChunkTextBytes = 4 << 20

// EditChunk replaces a chunk's text, records a revision, and schedules
// re-embedding.
func (s *Service) EditChunk(ctx context.Context, projectID, chunkID uuid.UUID, text string) (*EditChunkResponse, error) {
	if strings.TrimSpace(text) == "" {
		return nil, apperror.NewBadRequest("text cannot be empty")
	}
	if len(text) > MaxChunkTextBytes {
		return nil, apperror.NewBadRequest(fmt.Sprintf("chunk text exceeds maximum length of %d bytes", MaxChunkTextBytes))
	}
	seq, err := s.repo.ApplyChunkEdit(ctx, projectID, chunkID, text, authorFromContext(ctx))
	if err != nil {
		return nil, err
	}
	return &EditChunkResponse{ChunkID: chunkID.String(), Revision: seq}, nil
}

// ListRevisions returns a chunk's revisions in ascending seq order.
func (s *Service) ListRevisions(ctx context.Context, projectID, chunkID uuid.UUID) (*ListChunkRevisionsResponse, error) {
	revs, err := s.repo.ListRevisions(ctx, projectID, chunkID)
	if err != nil {
		return nil, err
	}
	// The repository returns the newest revisions first (DESC, capped); reverse
	// so callers receive ascending seq order.
	slices.Reverse(revs)
	dtos := make([]*ChunkRevisionDTO, 0, len(revs))
	for _, rev := range revs {
		dtos = append(dtos, rev.ToDTO())
	}
	return &ListChunkRevisionsResponse{Data: dtos, TotalCount: len(dtos)}, nil
}

// DiffRevisions returns the line diff between two revisions of a chunk.
func (s *Service) DiffRevisions(ctx context.Context, projectID, chunkID uuid.UUID, from, to int) (*ChunkDiffResponse, error) {
	if from < 1 || to < 1 {
		return nil, apperror.NewBadRequest("from and to must be positive revision numbers")
	}
	fromRev, err := s.repo.GetRevision(ctx, projectID, chunkID, from)
	if err != nil {
		return nil, err
	}
	toRev, err := s.repo.GetRevision(ctx, projectID, chunkID, to)
	if err != nil {
		return nil, err
	}
	lines, err := DiffLines(fromRev.Text, toRev.Text)
	if err != nil {
		if errors.Is(err, ErrDiffTooLarge) {
			return nil, apperror.NewBadRequest("revision texts are too large to diff")
		}
		return nil, apperror.NewInternal("failed to diff revisions", err)
	}
	return &ChunkDiffResponse{From: from, To: to, Lines: lines}, nil
}

// RollbackChunk restores a previous revision's text as a new revision.
func (s *Service) RollbackChunk(ctx context.Context, projectID, chunkID uuid.UUID, seq int) (*EditChunkResponse, error) {
	if seq < 1 {
		return nil, apperror.NewBadRequest("seq must be a positive revision number")
	}
	target, err := s.repo.GetRevision(ctx, projectID, chunkID, seq)
	if err != nil {
		return nil, err
	}
	// Rolling back to the current revision would record a duplicate no-op
	// revision; return the current state without an edit instead.
	chunk, err := s.repo.GetByID(ctx, projectID, chunkID)
	if err != nil {
		return nil, err
	}
	if target.Text == chunk.Text {
		return &EditChunkResponse{ChunkID: chunkID.String(), Revision: chunk.CurrentRevision}, nil
	}
	newSeq, err := s.repo.ApplyChunkEdit(ctx, projectID, chunkID, target.Text, authorFromContext(ctx))
	if err != nil {
		return nil, err
	}
	return &EditChunkResponse{ChunkID: chunkID.String(), Revision: newSeq}, nil
}

func authorFromContext(ctx context.Context) *string {
	if u := auth.UserFromContext(ctx); u != nil && u.ID != "" {
		id := u.ID
		return &id
	}
	return nil
}
