package journal

import (
	"context"
	"log/slog"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// =============================================================================
// mockRepo — in-memory implementation of repoIface for unit tests
// =============================================================================

type mockRepo struct {
	insertedEntry *JournalEntry
	insertedNote  *JournalNote
	listEntries   []*JournalEntry
	listTotal     int
	listNotes     []*JournalNote
	err           error
}

func (m *mockRepo) Insert(_ context.Context, e *JournalEntry) error {
	m.insertedEntry = e
	return m.err
}

func (m *mockRepo) InsertNote(_ context.Context, n *JournalNote) error {
	m.insertedNote = n
	return m.err
}

func (m *mockRepo) List(_ context.Context, _ ListParams) ([]*JournalEntry, int, error) {
	return m.listEntries, m.listTotal, m.err
}

func (m *mockRepo) ListStandaloneNotes(_ context.Context, _ uuid.UUID, _ *uuid.UUID, _ *time.Time, _ int, _ bool) ([]*JournalNote, error) {
	return m.listNotes, m.err
}

// newTestService returns a Service wired to the given mockRepo.
func newTestService(repo *mockRepo) *Service {
	log := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	return &Service{repo: repo, log: log}
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
		listTotal: 1,
		listNotes: []*JournalNote{
			{ID: uuid.New(), ProjectID: projectID, Body: "standalone note", ActorType: ActorUser},
		},
	}
	svc := newTestService(repo)

	resp, err := svc.List(context.Background(), ListParams{ProjectID: projectID, Limit: 10, Page: 1})
	require.NoError(t, err)
	assert.Len(t, resp.Entries, 1)
	assert.Equal(t, EventTypeCreated, resp.Entries[0].EventType)
	assert.Len(t, resp.Notes, 1)
	assert.Equal(t, "standalone note", resp.Notes[0].Body)
	assert.Equal(t, 1, resp.Total)
}

func TestList_EmptyResults(t *testing.T) {
	repo := &mockRepo{}
	svc := newTestService(repo)

	resp, err := svc.List(context.Background(), ListParams{ProjectID: uuid.New(), Limit: 10, Page: 1})
	require.NoError(t, err)
	assert.Empty(t, resp.Entries)
	assert.Empty(t, resp.Notes)
	assert.Equal(t, 0, resp.Total)
}

func TestList_PropagatesEntriesError(t *testing.T) {
	repo := &mockRepo{err: errTest}
	svc := newTestService(repo)

	_, err := svc.List(context.Background(), ListParams{ProjectID: uuid.New()})
	assert.ErrorIs(t, err, errTest)
}

// =============================================================================
// Service.Log — fire-and-forget behavior
// =============================================================================

func TestLog_NilMetadataDefaultedToEmptyMap(t *testing.T) {
	repo := &mockRepo{}
	svc := newTestService(repo)

	projectID := uuid.New()
	svc.Log(context.Background(), LogParams{
		ProjectID: projectID,
		EventType: EventTypeCreated,
		ActorType: ActorAgent,
		Metadata:  nil, // should be normalised to empty map
	})

	// Give the goroutine time to complete.
	time.Sleep(50 * time.Millisecond)

	require.NotNil(t, repo.insertedEntry, "entry should have been inserted")
	assert.NotNil(t, repo.insertedEntry.Metadata, "nil metadata must be replaced with empty map")
	assert.Empty(t, repo.insertedEntry.Metadata)
	assert.Equal(t, projectID, repo.insertedEntry.ProjectID)
	assert.Equal(t, EventTypeCreated, repo.insertedEntry.EventType)
}

func TestLog_PopulatesAllFields(t *testing.T) {
	repo := &mockRepo{}
	svc := newTestService(repo)

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

	time.Sleep(50 * time.Millisecond)

	e := repo.insertedEntry
	require.NotNil(t, e)
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

	meta := map[string]any{"key": "value", "count": 42}
	svc.Log(context.Background(), LogParams{
		ProjectID: uuid.New(),
		EventType: EventTypeBatch,
		ActorType: ActorSystem,
		Metadata:  meta,
	})

	time.Sleep(50 * time.Millisecond)

	require.NotNil(t, repo.insertedEntry)
	assert.Equal(t, "value", repo.insertedEntry.Metadata["key"])
	assert.Equal(t, 42, repo.insertedEntry.Metadata["count"])
}

func TestLog_ErrorIsSilenced(t *testing.T) {
	// Log must not panic or expose errors to callers even when repo fails.
	repo := &mockRepo{err: errTest}
	svc := newTestService(repo)

	// Should not panic.
	assert.NotPanics(t, func() {
		svc.Log(context.Background(), LogParams{
			ProjectID: uuid.New(),
			EventType: EventTypeDeleted,
			ActorType: ActorAgent,
		})
		time.Sleep(50 * time.Millisecond)
	})
}

// =============================================================================
// Helpers
// =============================================================================

// errTest is a sentinel error used in tests.
var errTest = &testError{"repo error"}

type testError struct{ msg string }

func (e *testError) Error() string { return e.msg }
