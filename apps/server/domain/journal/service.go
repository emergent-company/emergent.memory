package journal

import (
	"context"
	"log/slog"
	"time"

	"github.com/google/uuid"

	"github.com/emergent-company/emergent.memory/pkg/logger"
)

// repoIface is the repository interface used by Service.
// *Repository satisfies this interface; it is also implemented by test mocks.
type repoIface interface {
	Insert(ctx context.Context, entry *JournalEntry) error
	InsertNote(ctx context.Context, note *JournalNote) error
	List(ctx context.Context, params ListParams) ([]*JournalEntry, int, error)
	ListStandaloneNotes(ctx context.Context, projectID uuid.UUID, branchID *uuid.UUID, since *time.Time, limit int, includeBranches bool) ([]*JournalNote, error)
}

// Service handles journal business logic.
type Service struct {
	repo repoIface
	log  *slog.Logger
}

// NewService creates a new journal Service.
func NewService(repo *Repository, log *slog.Logger) *Service {
	return &Service{
		repo: repo,
		log:  log.With(logger.Scope("journal.svc")),
	}
}

// Log records a graph mutation asynchronously (fire and forget).
// Errors are logged but never returned to the caller.
func (s *Service) Log(ctx context.Context, params LogParams) {
	go func() {
		bgCtx := context.WithoutCancel(ctx)
		entry := &JournalEntry{
			ProjectID:  params.ProjectID,
			BranchID:   params.BranchID,
			EventType:  params.EventType,
			EntityType: params.EntityType,
			EntityID:   params.EntityID,
			ObjectType: params.ObjectType,
			ActorType:  params.ActorType,
			ActorID:    params.ActorID,
			Metadata:   params.Metadata,
			CreatedAt:  time.Now().UTC(),
		}
		if entry.Metadata == nil {
			entry.Metadata = map[string]any{}
		}
		if err := s.repo.Insert(bgCtx, entry); err != nil {
			s.log.Warn("failed to log journal entry",
				slog.String("event_type", params.EventType),
				slog.String("error", err.Error()))
		}
	}()
}

// AddNote adds a standalone or entry-attached note.
func (s *Service) AddNote(ctx context.Context, projectID uuid.UUID, req *AddNoteRequest) (*JournalNote, error) {
	actorType := req.ActorType
	if actorType == "" {
		actorType = ActorUser
	}
	note := &JournalNote{
		ProjectID: projectID,
		BranchID:  req.BranchID,
		JournalID: req.JournalID,
		Body:      req.Body,
		ActorType: actorType,
		ActorID:   req.ActorID,
		CreatedAt: time.Now().UTC(),
	}
	if err := s.repo.InsertNote(ctx, note); err != nil {
		return nil, err
	}
	return note, nil
}

// List returns journal entries and standalone notes for a project.
func (s *Service) List(ctx context.Context, params ListParams) (*JournalResponse, error) {
	entries, total, err := s.repo.List(ctx, params)
	if err != nil {
		return nil, err
	}
	notes, err := s.repo.ListStandaloneNotes(ctx, params.ProjectID, params.BranchID, params.Since, params.Limit, params.IncludeBranches)
	if err != nil {
		return nil, err
	}
	return &JournalResponse{
		Entries: entries,
		Notes:   notes,
		Total:   total,
	}, nil
}
