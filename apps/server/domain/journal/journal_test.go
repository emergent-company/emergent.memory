package journal

import (
	"context"
	"log/slog"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/emergent-company/emergent.memory/internal/config"
)

// =============================================================================
// mockRepo — in-memory implementation of repoIface for unit tests
// =============================================================================

type mockRepo struct {
	mu              sync.Mutex
	insertedEntry   *JournalEntry
	insertedBatches [][]*JournalEntry
	insertedNote    *JournalNote
	listEntries     []*JournalEntry
	listHasMore     bool
	listNextCursor  string
	listNotes       []*JournalNote
	err             error

	// blockInsert, when non-nil, blocks each InsertBatch call until the channel
	// is closed. Used to test drain-on-stop deterministically.
	blockInsert chan struct{}
}

func (m *mockRepo) Insert(_ context.Context, e *JournalEntry) error {
	m.mu.Lock()
	m.insertedEntry = e
	m.mu.Unlock()
	return m.err
}

func (m *mockRepo) InsertBatch(_ context.Context, entries []*JournalEntry) error {
	if m.blockInsert != nil {
		<-m.blockInsert
	}
	m.mu.Lock()
	m.insertedBatches = append(m.insertedBatches, entries)
	m.mu.Unlock()
	return m.err
}

func (m *mockRepo) InsertNote(_ context.Context, n *JournalNote) error {
	m.mu.Lock()
	m.insertedNote = n
	m.mu.Unlock()
	return m.err
}

func (m *mockRepo) List(_ context.Context, _ ListParams) ([]*JournalEntry, bool, string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.listEntries, m.listHasMore, m.listNextCursor, m.err
}

func (m *mockRepo) ListStandaloneNotes(_ context.Context, _ uuid.UUID, _ *uuid.UUID, _ *time.Time, _ int, _ bool) ([]*JournalNote, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.listNotes, m.err
}

// totalInserted returns the number of entries flushed via InsertBatch.
func (m *mockRepo) totalInserted() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	n := 0
	for _, b := range m.insertedBatches {
		n += len(b)
	}
	return n
}

// flushedEntries returns all entries flushed via InsertBatch, in order.
func (m *mockRepo) flushedEntries() []*JournalEntry {
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []*JournalEntry
	for _, b := range m.insertedBatches {
		out = append(out, b...)
	}
	return out
}

// waitInserted polls until at least n entries have been flushed via InsertBatch
// or the deadline passes.
func (m *mockRepo) waitInserted(t *testing.T, n int) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if m.totalInserted() >= n {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %d entries to flush; got %d", n, m.totalInserted())
}

// newTestService returns a Service wired to the given mockRepo.
func newTestService(repo *mockRepo) *Service {
	log := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	return newService(repo, &config.Config{}, log)
}

// =============================================================================
// parseSince
// =============================================================================

func TestParseSince_RelativeDurations(t *testing.T) {
	tests := []struct {
		input  string
		approx time.Duration
	}{
		{"7d", 7 * 24 * time.Hour},
		{"1d", 24 * time.Hour},
		{"24h", 24 * time.Hour},
		{"1h", time.Hour},
		{"30m", 30 * time.Minute},
		{"5s", 5 * time.Second},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			before := time.Now().UTC()
			got, err := parseSince(tt.input)
			after := time.Now().UTC()
			require.NoError(t, err)

			// Result should be approximately (now - duration), within 2 seconds of tolerance.
			expected := before.Add(-tt.approx)
			assert.WithinDuration(t, expected, got, 2*time.Second,
				"parseSince(%q) should be approximately now-%v", tt.input, tt.approx)
			_ = after
		})
	}
}

func TestParseSince_ISO8601(t *testing.T) {
	ts := "2026-01-15T10:30:00Z"
	got, err := parseSince(ts)
	require.NoError(t, err)
	assert.Equal(t, 2026, got.Year())
	assert.Equal(t, time.January, got.Month())
	assert.Equal(t, 15, got.Day())
	assert.Equal(t, 10, got.Hour())
	assert.Equal(t, 30, got.Minute())
}

func TestParseSince_Invalid(t *testing.T) {
	tests := []string{
		"not-a-date",
		"xyz",
		"99q",  // unknown unit letter
		"d",    // unit with no number
		"abch", // non-numeric prefix with unit
	}

	for _, input := range tests {
		t.Run(input, func(t *testing.T) {
			_, err := parseSince(input)
			assert.Error(t, err, "expected error for input %q", input)
		})
	}
}

func TestParseSince_EmptyString(t *testing.T) {
	_, err := parseSince("")
	assert.Error(t, err)
}

// =============================================================================
// Entity / actor constants
// =============================================================================

func TestEventTypeConstants(t *testing.T) {
	assert.Equal(t, "created", EventTypeCreated)
	assert.Equal(t, "updated", EventTypeUpdated)
	assert.Equal(t, "deleted", EventTypeDeleted)
	assert.Equal(t, "restored", EventTypeRestored)
	assert.Equal(t, "related", EventTypeRelated)
	assert.Equal(t, "batch", EventTypeBatch)
	assert.Equal(t, "merge", EventTypeMerge)
	assert.Equal(t, "note", EventTypeNote)
}

func TestActorTypeConstants(t *testing.T) {
	assert.Equal(t, "user", ActorUser)
	assert.Equal(t, "agent", ActorAgent)
	assert.Equal(t, "system", ActorSystem)
}

func TestEntityTypeConstants(t *testing.T) {
	assert.Equal(t, "graph_object", EntityObject)
	assert.Equal(t, "graph_relationship", EntityRelationship)
}

// =============================================================================
// Service.AddNote
// =============================================================================

func TestAddNote_DefaultsActorTypeToUser(t *testing.T) {
	repo := &mockRepo{}
	svc := newTestService(repo)
	defer svc.Stop(context.Background())

	projectID := uuid.New()
	req := &AddNoteRequest{
		Body: "test note",
		// ActorType intentionally omitted → should default to ActorUser
	}
	note, err := svc.AddNote(context.Background(), projectID, req)
	require.NoError(t, err)
	require.NotNil(t, note)

	assert.Equal(t, ActorUser, note.ActorType)
	assert.Equal(t, "test note", note.Body)
	assert.Equal(t, projectID, note.ProjectID)
	assert.Nil(t, note.JournalID)
	assert.WithinDuration(t, time.Now().UTC(), note.CreatedAt, 5*time.Second)

	// Verify the repo received exactly the same note.
	require.NotNil(t, repo.insertedNote)
	assert.Equal(t, note, repo.insertedNote)
}

func TestAddNote_AttachesJournalID(t *testing.T) {
	repo := &mockRepo{}
	svc := newTestService(repo)
	defer svc.Stop(context.Background())

	projectID := uuid.New()
	entryID := uuid.New()
	req := &AddNoteRequest{
		Body:      "attached note",
		ActorType: ActorAgent,
		JournalID: &entryID,
	}
	note, err := svc.AddNote(context.Background(), projectID, req)
	require.NoError(t, err)
	require.NotNil(t, note.JournalID)
	assert.Equal(t, entryID, *note.JournalID)
	assert.Equal(t, ActorAgent, note.ActorType)
}

func TestAddNote_ExplicitActorTypePreserved(t *testing.T) {
	repo := &mockRepo{}
	svc := newTestService(repo)
	defer svc.Stop(context.Background())

	projectID := uuid.New()
	actorID := uuid.New()
	req := &AddNoteRequest{
		Body:      "system note",
		ActorType: ActorSystem,
		ActorID:   &actorID,
	}
	note, err := svc.AddNote(context.Background(), projectID, req)
	require.NoError(t, err)
	assert.Equal(t, ActorSystem, note.ActorType)
	require.NotNil(t, note.ActorID)
	assert.Equal(t, actorID, *note.ActorID)
}

func TestAddNote_PropagatesRepoError(t *testing.T) {
	repo := &mockRepo{err: errTest}
	svc := newTestService(repo)
	defer svc.Stop(context.Background())

	_, err := svc.AddNote(context.Background(), uuid.New(), &AddNoteRequest{Body: "x"})
	assert.ErrorIs(t, err, errTest)
}

// =============================================================================
// Service.List
// =============================================================================

func TestList_ReturnsEntriesAndNotes(t *testing.T) {
	projectID := uuid.New()
	repo := &mockRepo{
		listEntries: []*JournalEntry{
			{ID: uuid.New(), ProjectID: projectID, EventType: EventTypeCreated, ActorType: ActorAgent},
		},
		listHasMore:    true,
		listNextCursor: "cursor-abc",
		listNotes: []*JournalNote{
			{ID: uuid.New(), ProjectID: projectID, Body: "standalone note", ActorType: ActorUser},
		},
	}
	svc := newTestService(repo)
	defer svc.Stop(context.Background())

	resp, err := svc.List(context.Background(), ListParams{ProjectID: projectID, Limit: 10})
	require.NoError(t, err)
	assert.Len(t, resp.Entries, 1)
	assert.Equal(t, EventTypeCreated, resp.Entries[0].EventType)
	assert.Len(t, resp.Notes, 1)
	assert.Equal(t, "standalone note", resp.Notes[0].Body)
	assert.True(t, resp.HasMore)
	assert.Equal(t, "cursor-abc", resp.NextCursor)
}

func TestList_EmptyResults(t *testing.T) {
	repo := &mockRepo{}
	svc := newTestService(repo)
	defer svc.Stop(context.Background())

	resp, err := svc.List(context.Background(), ListParams{ProjectID: uuid.New(), Limit: 10})
	require.NoError(t, err)
	assert.Empty(t, resp.Entries)
	assert.Empty(t, resp.Notes)
	assert.False(t, resp.HasMore)
	assert.Empty(t, resp.NextCursor)
}

func TestList_PropagatesEntriesError(t *testing.T) {
	repo := &mockRepo{err: errTest}
	svc := newTestService(repo)
	defer svc.Stop(context.Background())

	_, err := svc.List(context.Background(), ListParams{ProjectID: uuid.New()})
	assert.ErrorIs(t, err, errTest)
}

// =============================================================================
// Service.Log — bounded async writer
// =============================================================================

func TestLog_NilMetadataDefaultedToEmptyMap(t *testing.T) {
	repo := &mockRepo{}
	svc := newTestService(repo)
	defer svc.Stop(context.Background())

	projectID := uuid.New()
	svc.Log(context.Background(), LogParams{
		ProjectID: projectID,
		EventType: EventTypeCreated,
		ActorType: ActorAgent,
		Metadata:  nil, // should be normalised to empty map
	})

	repo.waitInserted(t, 1)

	entries := repo.flushedEntries()
	require.Len(t, entries, 1, "entry should have been inserted")
	assert.NotNil(t, entries[0].Metadata, "nil metadata must be replaced with empty map")
	assert.Empty(t, entries[0].Metadata)
	assert.Equal(t, projectID, entries[0].ProjectID)
	assert.Equal(t, EventTypeCreated, entries[0].EventType)
}

func TestLog_PopulatesAllFields(t *testing.T) {
	repo := &mockRepo{}
	svc := newTestService(repo)
	defer svc.Stop(context.Background())

	projectID := uuid.New()
	actorID := uuid.New()
	entityID := uuid.New()
	entityType := EntityObject
	objType := "Service"

	svc.Log(context.Background(), LogParams{
		ProjectID:  projectID,
		EventType:  EventTypeUpdated,
		EntityType: &entityType,
		EntityID:   &entityID,
		ObjectType: &objType,
		ActorType:  ActorUser,
		ActorID:    &actorID,
		Metadata:   map[string]any{"fields_changed": []string{"name"}},
	})

	repo.waitInserted(t, 1)

	entries := repo.flushedEntries()
	require.Len(t, entries, 1)
	e := entries[0]
	assert.Equal(t, EventTypeUpdated, e.EventType)
	assert.Equal(t, entityType, *e.EntityType)
	assert.Equal(t, entityID, *e.EntityID)
	assert.Equal(t, objType, *e.ObjectType)
	assert.Equal(t, ActorUser, e.ActorType)
	assert.Equal(t, actorID, *e.ActorID)
	assert.Equal(t, projectID, e.ProjectID)
	assert.WithinDuration(t, time.Now().UTC(), e.CreatedAt, 5*time.Second)
}

func TestLog_ExistingMetadataPreserved(t *testing.T) {
	repo := &mockRepo{}
	svc := newTestService(repo)
	defer svc.Stop(context.Background())

	meta := map[string]any{"key": "value", "count": 42}
	svc.Log(context.Background(), LogParams{
		ProjectID: uuid.New(),
		EventType: EventTypeBatch,
		ActorType: ActorSystem,
		Metadata:  meta,
	})

	repo.waitInserted(t, 1)

	entries := repo.flushedEntries()
	require.Len(t, entries, 1)
	assert.Equal(t, "value", entries[0].Metadata["key"])
	assert.Equal(t, 42, entries[0].Metadata["count"])
}

func TestLog_ErrorIsSilenced(t *testing.T) {
	// Log must not panic or expose errors to callers even when repo fails.
	repo := &mockRepo{err: errTest}
	svc := newTestService(repo)
	defer svc.Stop(context.Background())

	// Should not panic.
	assert.NotPanics(t, func() {
		svc.Log(context.Background(), LogParams{
			ProjectID: uuid.New(),
			EventType: EventTypeDeleted,
			ActorType: ActorAgent,
		})
		repo.waitInserted(t, 1)
	})
}

func TestLog_FlushesEntriesViaInsertBatch(t *testing.T) {
	repo := &mockRepo{}
	svc := newTestService(repo)
	defer svc.Stop(context.Background())

	const n = 20
	for i := 0; i < n; i++ {
		svc.Log(context.Background(), LogParams{
			ProjectID: uuid.New(),
			EventType: EventTypeCreated,
			ActorType: ActorAgent,
		})
	}

	repo.waitInserted(t, n)
	assert.Equal(t, n, repo.totalInserted(), "all entries should be flushed via InsertBatch")
}

func TestStop_DrainsQueue(t *testing.T) {
	repo := &mockRepo{blockInsert: make(chan struct{})}
	cfg := &config.Config{Journal: config.JournalConfig{
		WriterConcurrency: 1,
		WriterQueueSize:   64,
		WriterBatchSize:   8,
	}}
	svc := newService(repo, cfg, slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError})))

	const n = 11
	for i := 0; i < n; i++ {
		svc.Log(context.Background(), LogParams{
			ProjectID: uuid.New(),
			EventType: EventTypeCreated,
			ActorType: ActorAgent,
		})
	}

	// Unblock the worker's in-flight insert, then stop: the worker flushes its
	// batch and exits, and Stop drains whatever remains in the queue.
	close(repo.blockInsert)
	svc.Stop(context.Background())

	assert.Equal(t, n, repo.totalInserted(), "Stop must drain all queued entries")
}

// =============================================================================
// Cursor encode/decode
// =============================================================================

func TestCursor_RoundTrip(t *testing.T) {
	ts := time.Date(2026, time.January, 15, 10, 30, 0, 123456789, time.UTC)
	id := uuid.New()

	c := encodeCursor(ts, id)
	gotTS, gotID, err := decodeCursor(c)
	require.NoError(t, err)
	assert.True(t, ts.Equal(gotTS), "created_at should round-trip")
	assert.Equal(t, id, gotID)
}

func TestCursor_DecodeInvalid(t *testing.T) {
	for _, bad := range []string{"", "nopipe", "|", "badtime|" + uuid.New().String(), time.Now().Format(time.RFC3339Nano) + "|not-a-uuid", "garbage||extra"} {
		_, _, err := decodeCursor(bad)
		assert.ErrorIs(t, err, ErrInvalidCursor, "expected ErrInvalidCursor for %q", bad)
	}
}

// =============================================================================
// Helpers
// =============================================================================

// errTest is a sentinel error used in tests.
var errTest = &testError{"repo error"}

type testError struct{ msg string }

func (e *testError) Error() string { return e.msg }
