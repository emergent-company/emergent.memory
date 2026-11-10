package sources

import (
	"context"
	"io"
	"log/slog"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/emergent-company/emergent.memory/internal/testdb"
)

func TestSourceAppsTriggersAndClaims(t *testing.T) {
	if testing.Short() {
		testdb.SkipOrFatal(t, "skipping database integration test in short mode")
	}
	ctx := context.Background()
	db := testdb.SetupTestDBOrFail(t, ctx, "source_triggers")
	defer db.Close()
	repo := NewRepository(db.DB, slog.New(slog.NewTextHandler(io.Discard, nil)))

	orgID, projectID, agentID := uuid.New(), uuid.New(), uuid.New()
	sourceID := uuid.New()
	_, err := db.DB.ExecContext(ctx, `INSERT INTO kb.orgs (id, name) VALUES (?, ?)`, orgID, "source-trigger-org")
	require.NoError(t, err)
	_, err = db.DB.ExecContext(ctx, `INSERT INTO kb.projects (id, organization_id, name) VALUES (?, ?, ?)`, projectID, orgID, "source-trigger-project")
	require.NoError(t, err)
	_, err = db.DB.ExecContext(ctx, `INSERT INTO kb.agents (id, name, strategy_type, cron_schedule, project_id) VALUES (?, ?, 'definition', '', ?)`, agentID, "review-agent", projectID)
	require.NoError(t, err)
	_, err = db.DB.ExecContext(ctx, `INSERT INTO kb.sources (id, project_id, provider, display_name) VALUES (?, ?, 'github_app', 'bot')`, sourceID, projectID)
	require.NoError(t, err)

	// Source apps are org-scoped and allow several Apps of one provider.
	app1 := &SourceApp{OrgID: orgID, Provider: "github", AppID: 11, Slug: "bot-a", PrivateKeyEncrypted: []byte("k1"), WebhookSecretEncrypted: []byte("s1")}
	app2 := &SourceApp{OrgID: orgID, Provider: "github", AppID: 22, Slug: "bot-b", PrivateKeyEncrypted: []byte("k2"), WebhookSecretEncrypted: []byte("s2")}
	require.NoError(t, repo.CreateSourceApp(ctx, app1))
	require.NoError(t, repo.CreateSourceApp(ctx, app2))
	require.NoError(t, repo.CreateSourceApp(ctx, &SourceApp{OrgID: orgID, Provider: "github", AppID: 33, Slug: "bot-c", PrivateKeyEncrypted: []byte("k"), WebhookSecretEncrypted: []byte("s")}))
	require.Error(t, repo.CreateSourceApp(ctx, &SourceApp{OrgID: orgID, Provider: "github", AppID: 11, Slug: "dup", PrivateKeyEncrypted: []byte("k"), WebhookSecretEncrypted: []byte("s")}))
	byKey, err := repo.FindSourceAppByWebhookKey(ctx, app1.WebhookKey)
	require.NoError(t, err)
	require.Equal(t, app1.ID, byKey.ID)

	// Trigger binding.
	trigger := &SourceTrigger{ProjectID: projectID, SourceID: sourceID, EventType: "pull_request", AgentID: agentID, Queue: "security-review", EventFilter: map[string]any{}}
	require.NoError(t, repo.CreateSourceTrigger(ctx, trigger))
	require.Error(t, repo.CreateSourceTrigger(ctx, &SourceTrigger{ProjectID: projectID, SourceID: sourceID, EventType: "pull_request", AgentID: agentID, Queue: "default"}))
	enabled, err := repo.ListEnabledTriggersForSource(ctx, sourceID)
	require.NoError(t, err)
	require.Len(t, enabled, 1)

	// Delivery dedupe.
	ev := &SourceEvent{ProjectID: projectID, SourceID: sourceID, DeliveryID: "d1", EventType: "pull_request", Action: "opened", SubjectType: "pull_request", SubjectRef: "owner/repo#1", HeadSHA: "aaaa", Payload: map[string]any{"repo": "owner/repo"}}
	inserted, err := repo.InsertSourceEvent(ctx, ev)
	require.NoError(t, err)
	require.True(t, inserted)
	inserted, err = repo.InsertSourceEvent(ctx, &SourceEvent{ProjectID: projectID, SourceID: sourceID, DeliveryID: "d1", EventType: "pull_request", SubjectType: "pull_request", SubjectRef: "owner/repo#1", HeadSHA: "aaaa"})
	require.NoError(t, err)
	require.False(t, inserted)

	// One claim per head; retry reuses it; a newer head supersedes it.
	claim1, superseded, err := repo.ClaimSourceSubject(ctx, projectID, sourceID, "owner/repo#1", "aaaa")
	require.NoError(t, err)
	require.Empty(t, superseded)
	runID := uuid.New()
	ok, err := repo.SetSourceEventClaimRun(ctx, claim1.ID, runID, SourceClaimRunning)
	require.NoError(t, err)
	require.True(t, ok)
	ok, err = repo.SetSourceEventClaimRun(ctx, claim1.ID, uuid.New(), SourceClaimRunning)
	require.NoError(t, err)
	require.False(t, ok)

	claim1b, superseded, err := repo.ClaimSourceSubject(ctx, projectID, sourceID, "owner/repo#1", "aaaa")
	require.NoError(t, err)
	require.Empty(t, superseded)
	require.Equal(t, claim1.ID, claim1b.ID)

	claim2, superseded, err := repo.ClaimSourceSubject(ctx, projectID, sourceID, "owner/repo#1", "bbbb")
	require.NoError(t, err)
	require.Contains(t, superseded, runID)
	require.NotEqual(t, claim1.ID, claim2.ID)
	require.Equal(t, SourceClaimSuperseded, sourceClaimState(t, db, claim1.ID))

	// Cancelling a subject returns the active run for cancellation.
	runID2 := uuid.New()
	ok, err = repo.SetSourceEventClaimRun(ctx, claim2.ID, runID2, SourceClaimRunning)
	require.NoError(t, err)
	require.True(t, ok)
	cancelled, err := repo.CancelSourceSubjectClaims(ctx, sourceID, "owner/repo#1")
	require.NoError(t, err)
	require.Contains(t, cancelled, runID2)
	require.Equal(t, SourceClaimCancelled, sourceClaimState(t, db, claim2.ID))

	// A head that moves back reactivates the prior claim row.
	claim1c, _, err := repo.ClaimSourceSubject(ctx, projectID, sourceID, "owner/repo#1", "aaaa")
	require.NoError(t, err)
	require.Equal(t, claim1.ID, claim1c.ID)
	require.Equal(t, SourceClaimPending, claim1c.State)
	require.Nil(t, claim1c.RunID)
}

func sourceClaimState(t *testing.T, db *testdb.TestDB, id uuid.UUID) string {
	t.Helper()
	var state string
	require.NoError(t, db.DB.NewSelect().Table("kb.source_event_claims").Column("state").Where("id = ?", id).Scan(context.Background(), &state))
	return state
}
