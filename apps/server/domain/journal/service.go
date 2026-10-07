package journal

import (
	"context"
	"log/slog"
	"sync"
	"sync/atomic"
	"time"

	"github.com/google/uuid"

	"github.com/emergent-company/emergent.memory/internal/config"
	"github.com/emergent-company/emergent.memory/pkg/logger"
)

// repoIface is the repository interface used by Service.
// *Repository satisfies this interface; it is also implemented by test mocks.
type repoIface interface {
	Insert(ctx context.Context, entry *JournalEntry) error
	InsertBatch(ctx context.Context, entries []*JournalEntry) error
	InsertNote(ctx context.Context, note *JournalNote) error
	List(ctx context.Context, params ListParams) ([]*JournalEntry, bool, string, error)
	ListStandaloneNotes(ctx context.Context, projectID uuid.UUID, branchID *uuid.UUID, since *time.Time, limit int, includeBranches bool) ([]*JournalNote, error)
}

// Service handles journal business logic.
type Service struct {
	repo repoIface
	log  *slog.Logger

	queue       chan *JournalEntry
	stop        chan struct{}
	stopOnce    sync.Once
	wg          sync.WaitGroup
	workerCount int
	batchSize   int

	// enqueueMu serialises Log's stopped-check-and-send against Stop. Stop
	// takes the write lock before flipping `stopped` and closing `stop`, so a
	// concurrent Log either completes its enqueue before Stop drains (and is
	// picked up by the drain) or observes `stopped` and is rejected with a
	// warning. This closes the window where a Log could otherwise enqueue
	// after Stop had already drained and returned.
	enqueueMu sync.RWMutex

	// stopped guards the enqueue path so a shutdown race cannot send on a
	// queue that workers have already drained.
	stopped atomic.Bool
}

// defaultJournalConfig returns the config defaults, used when a nil config or
// non-positive values are supplied so the writer never starts unbounded.
const (
	defaultWriterConcurrency = 8
	defaultWriterQueueSize   = 4096
	defaultWriterBatchSize   = 256
)

// NewService creates a new journal Service and starts its async writer pool.
func NewService(repo *Repository, cfg *config.Config, log *slog.Logger) *Service {
	return newService(repo, cfg, log)
}

// newService builds the Service with the given repo and starts workers.
func newService(repo repoIface, cfg *config.Config, log *slog.Logger) *Service {
	if log == nil {
		log = slog.Default()
	}
	concurrency := defaultWriterConcurrency
	queueSize := defaultWriterQueueSize
	batchSize := defaultWriterBatchSize
	if cfg != nil {
		if cfg.Journal.WriterConcurrency > 0 {
			concurrency = cfg.Journal.WriterConcurrency
		}
		if cfg.Journal.WriterQueueSize > 0 {
			queueSize = cfg.Journal.WriterQueueSize
		}
		if cfg.Journal.WriterBatchSize > 0 {
			batchSize = cfg.Journal.WriterBatchSize
		}
	}

	s := &Service{
		repo:        repo,
		log:         log.With(logger.Scope("journal.svc")),
		queue:       make(chan *JournalEntry, queueSize),
		stop:        make(chan struct{}),
		workerCount: concurrency,
		batchSize:   batchSize,
	}

	for i := 0; i < concurrency; i++ {
		s.wg.Add(1)
		go s.worker()
	}
	return s
}

// Log records a graph mutation. The entry is built synchronously and submitted
// to the bounded writer queue with blocking backpressure; it is never silently
// dropped on the normal path. The only drop paths are caller cancellation or a
// service that has already been stopped, both of which emit a warning.
func (s *Service) Log(ctx context.Context, params LogParams) {
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

	s.enqueueMu.RLock()
	if s.stopped.Load() {
		s.enqueueMu.RUnlock()
		s.log.Warn("journal entry dropped: writer stopped",
			slog.String("event_type", params.EventType))
		return
	}

	select {
	case s.queue <- entry:
		s.enqueueMu.RUnlock()
	case <-ctx.Done():
		s.enqueueMu.RUnlock()
		s.log.Warn("journal entry dropped: caller cancelled",
			slog.String("event_type", params.EventType),
			slog.String("error", ctx.Err().Error()))
	}
}

// worker drains the queue and flushes entries in batches until Stop is called.
func (s *Service) worker() {
	defer s.wg.Done()
	for {
		select {
		case entry := <-s.queue:
			s.flush(entry)
		case <-s.stop:
			return
		}
	}
}

// flush collects up to batchSize entries (the first is already received) and
// inserts them in one multi-row INSERT.
func (s *Service) flush(first *JournalEntry) {
	batch := make([]*JournalEntry, 1, s.batchSize)
	batch[0] = first
	for len(batch) < s.batchSize {
		select {
		case e := <-s.queue:
			batch = append(batch, e)
		default:
			s.insertBatch(batch)
			return
		}
	}
	s.insertBatch(batch)
}

// insertBatch writes a batch using a bounded background context so a cancelled
// caller never aborts the flush and a hung database cannot block shutdown
// (which waits for workers) indefinitely.
func (s *Service) insertBatch(batch []*JournalEntry) {
	if s.repo == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := s.repo.InsertBatch(ctx, batch); err != nil {
		s.log.Warn("failed to flush journal entries",
			slog.Int("count", len(batch)),
			slog.String("error", err.Error()))
	}
}

// Stop shuts down the writer pool and drains any remaining queued entries. It is
// idempotent and safe to call from an fx lifecycle hook.
func (s *Service) Stop(ctx context.Context) {
	// Exclude in-flight Log calls: once the write lock is held no producer can
	// be between its stopped-check and its enqueue, so every entry accepted
	// before shutdown is already (or about to be) in the queue when workers are
	// signalled and the drain below runs.
	s.enqueueMu.Lock()
	s.stopped.Store(true)
	s.stopOnce.Do(func() { close(s.stop) })
	s.enqueueMu.Unlock()
	s.wg.Wait()

	// Drain any entries that raced with shutdown. Bounded to 5s as a safety net;
	// the drain is non-blocking so it normally completes immediately.
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		var batch []*JournalEntry
		select {
		case e := <-s.queue:
			batch = append(batch, e)
		default:
			return
		}
	drain:
		for len(batch) < s.batchSize {
			select {
			case e := <-s.queue:
				batch = append(batch, e)
			default:
				break drain
			}
		}
		s.insertBatch(batch)
	}
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
	entries, hasMore, nextCursor, err := s.repo.List(ctx, params)
	if err != nil {
		return nil, err
	}
	notes, err := s.repo.ListStandaloneNotes(ctx, params.ProjectID, params.BranchID, params.Since, params.Limit, params.IncludeBranches)
	if err != nil {
		return nil, err
	}
	return &JournalResponse{
		Entries:    entries,
		Notes:      notes,
		HasMore:    hasMore,
		NextCursor: nextCursor,
	}, nil
}
