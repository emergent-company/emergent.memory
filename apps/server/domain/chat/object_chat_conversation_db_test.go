package chat_test

import (
	"encoding/json"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/emergent-company/emergent.memory/domain/chat"
)

func strPtr(s string) *string { return &s }

// TestCreateObjectConversationOpensEmpty pins the object-chat context model:
// a conversation opened from an object page is created with its canonical id and
// no seeded message, so the first turn is the user's own words and the object
// reaches the agent through the per-turn system-prompt appendix instead.
func TestCreateObjectConversationOpensEmpty(t *testing.T) {
	env := newRepoEnv(t)
	canonical := uuid.New()

	req := chat.CreateConversationRequest{
		Title:       "Boiler service — confirm parts availability",
		CanonicalID: strPtr(canonical.String()),
	}

	conv, err := env.svc.CreateConversation(env.ctx, env.projectID, env.ownerID, req)
	require.NoError(t, err)
	require.NotNil(t, conv.CanonicalID)
	assert.Equal(t, canonical, *conv.CanonicalID)
	assert.Empty(t, conv.Messages, "an object chat must not seed a message")
	assert.Zero(t, env.countMessages(conv.ID))

	// Re-opening the same object resolves the same conversation: canonical id is
	// unique per project, so a click cannot mint an orphan each time.
	again, err := env.svc.CreateConversation(env.ctx, env.projectID, env.ownerID, req)
	require.NoError(t, err)
	assert.Equal(t, conv.ID, again.ID)

	var count int
	require.NoError(t, env.db.DB.NewRaw(
		`SELECT COUNT(*) FROM kb.chat_conversations WHERE project_id = ? AND canonical_id = ?`,
		env.projectID, canonical).Scan(env.ctx, &count))
	assert.Equal(t, 1, count)

	// A conversation with no object still seeds the first user message.
	plain, err := env.svc.CreateConversation(env.ctx, env.projectID, env.ownerID, chat.CreateConversationRequest{
		Title:   "plain chat",
		Message: "hello",
	})
	require.NoError(t, err)
	require.Len(t, plain.Messages, 1)
	assert.Equal(t, 1, env.countMessages(plain.ID))
}

// TestLoadObjectContextReturnsLiveHead pins what the appendix is rendered from:
// the canonical id resolves to the live head (supersedes_id IS NULL) with the
// workflow-status column, and an unknown or deleted object is (nil, nil) rather
// than an error the chat would have to recover from.
func TestLoadObjectContextReturnsLiveHead(t *testing.T) {
	env := newRepoEnv(t)
	canonical := uuid.NewString()
	v1, v2 := uuid.NewString(), uuid.NewString()

	insertVersion := func(id string, version int, name string, props map[string]any) {
		t.Helper()
		propsJSON, err := json.Marshal(props)
		require.NoError(t, err)
		_, err = env.db.DB.NewRaw(`
			INSERT INTO kb.graph_objects
				(id, project_id, branch_id, canonical_id, supersedes_id, version, type, key, name, status,
				 properties, labels, created_at, updated_at)
			VALUES (?, ?, NULL, ?, NULL, ?, 'Task', 'boiler-service-parts', ?, 'ready', ?::jsonb, '{}'::text[], now() - interval '1 day', now())
		`, id, env.projectID, canonical, version, name, string(propsJSON)).Exec(env.ctx)
		require.NoError(t, err)
	}

	insertVersion(v1, 1, "old title", map[string]any{"title": "old title"})
	_, err := env.db.DB.NewRaw(`UPDATE kb.graph_objects SET supersedes_id = ? WHERE id = ?`, v2, v1).Exec(env.ctx)
	require.NoError(t, err)
	insertVersion(v2, 2, "Boiler service — confirm parts availability", map[string]any{
		"title":    "Boiler service — confirm parts availability",
		"priority": "high",
	})

	oc, err := env.repo.LoadObjectContext(env.ctx, env.projectID, uuid.MustParse(canonical))
	require.NoError(t, err)
	require.NotNil(t, oc)
	assert.Equal(t, v2, oc.VersionID.String(), "the live head is the row the object page links to")
	assert.Equal(t, canonical, oc.CanonicalID.String())
	assert.Equal(t, 2, oc.Version)
	assert.Equal(t, "Task", oc.Type)
	assert.Equal(t, "ready", oc.Status)
	assert.Equal(t, "high", oc.Properties["priority"])

	unknown, err := env.repo.LoadObjectContext(env.ctx, env.projectID, uuid.New())
	require.NoError(t, err)
	assert.Nil(t, unknown)
}

// TestLoadObjectContextIgnoresBranchForks pins the default-read convention:
// branch_id IS NULL. A fork re-uses the canonical id on its branch, so both the
// main head and the branch draft are live heads (supersedes_id IS NULL) and
// match the canonical-id filter. Without the branch predicate the appendix can
// present an unmerged branch draft — and flip between turns. The branch row is
// inserted first so a plan that scans in insertion order would pick it.
func TestLoadObjectContextIgnoresBranchForks(t *testing.T) {
	env := newRepoEnv(t)
	canonical := uuid.NewString()
	mainHead, branchDraft := uuid.NewString(), uuid.NewString()
	// branch_id has no FK on kb.graph_objects (migrations/00128), so any id
	// identifies the fork.
	branch := uuid.NewString()

	_, err := env.db.DB.NewRaw(`
		INSERT INTO kb.graph_objects
			(id, project_id, branch_id, canonical_id, supersedes_id, version, type, key, name, status,
			 properties, labels, created_at, updated_at)
		VALUES (?, ?, ?, ?, NULL, 9, 'Task', 'boiler-service-parts', 'BRANCH DRAFT', 'draft',
			'{"priority": "low"}'::jsonb, '{}'::text[], now(), now())
	`, branchDraft, env.projectID, branch, canonical).Exec(env.ctx)
	require.NoError(t, err)

	_, err = env.db.DB.NewRaw(`
		INSERT INTO kb.graph_objects
			(id, project_id, branch_id, canonical_id, supersedes_id, version, type, key, name, status,
			 properties, labels, created_at, updated_at)
		VALUES (?, ?, NULL, ?, NULL, 1, 'Task', 'boiler-service-parts', 'Boiler service', 'ready',
			'{"priority": "high"}'::jsonb, '{}'::text[], now() - interval '1 day', now())
	`, mainHead, env.projectID, canonical).Exec(env.ctx)
	require.NoError(t, err)

	oc, err := env.repo.LoadObjectContext(env.ctx, env.projectID, uuid.MustParse(canonical))
	require.NoError(t, err)
	require.NotNil(t, oc)
	assert.Equal(t, mainHead, oc.VersionID.String(), "the appendix must read the main head, never a branch draft")
	assert.Equal(t, 1, oc.Version)
	assert.Equal(t, "ready", oc.Status)
	assert.Equal(t, "high", oc.Properties["priority"])
}
