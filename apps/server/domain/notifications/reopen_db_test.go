package notifications

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"github.com/uptrace/bun"

	"github.com/emergent-company/emergent.memory/domain/notifications/taxonomy"
	"github.com/emergent-company/emergent.memory/internal/testdb"
)

// reopenHarness provisions a DB with one project and n members, plus the
// notification service under test.
type reopenHarness struct {
	ctx       context.Context
	db        bun.IDB
	repo      *Repository
	svc       *Service
	projectID string
	userIDs   []string
}

func newReopenHarness(t *testing.T, members int) *reopenHarness {
	t.Helper()
	ctx := context.Background()
	tdb := testdb.SetupTestDBOrFail(t, ctx, "notif_reopen")
	t.Cleanup(tdb.Close)
	db := tdb.GetDB()

	orgID, projectID := uuid.NewString(), uuid.NewString()
	_, err := db.NewRaw(`INSERT INTO kb.orgs (id, name, created_at, updated_at)
		VALUES (?, 'Reopen Org', now(), now())`, orgID).Exec(ctx)
	require.NoError(t, err)
	_, err = db.NewRaw(`INSERT INTO kb.projects (id, name, organization_id, created_at, updated_at)
		VALUES (?, 'Reopen Project', ?, now(), now())`, projectID, orgID).Exec(ctx)
	require.NoError(t, err)

	h := &reopenHarness{ctx: ctx, db: db, projectID: projectID}
	for i := 0; i < members; i++ {
		userID := uuid.NewString()
		_, err = db.NewRaw(`INSERT INTO core.user_profiles (id, zitadel_user_id, display_name, created_at, updated_at)
			VALUES (?, ?, 'Member', now(), now())`, userID, "zitadel-"+userID).Exec(ctx)
		require.NoError(t, err)
		_, err = db.NewRaw(`INSERT INTO kb.project_memberships (project_id, user_id, role, created_at)
			VALUES (?, ?, 'member', now())`, projectID, userID).Exec(ctx)
		require.NoError(t, err)
		h.userIDs = append(h.userIDs, userID)
	}

	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	h.repo = NewRepository(db, log)
	h.svc = NewService(h.repo, nil, log)
	return h
}

// reconnectInput is the payload the sources publisher sends for one park.
func (h *reopenHarness) reconnectInput(sourceID, title, message string) CreateInput {
	projectID := h.projectID
	sourceType := "data_source"
	groupKey := "data_source:" + sourceID
	actionURL := "/data-sources/" + sourceID
	actionLabel := "Reconnect"
	details, err := json.Marshal(map[string]any{"source_id": sourceID})
	if err != nil {
		panic(err)
	}
	return CreateInput{
		UserID:              h.userIDs[0],
		ProjectID:           &projectID,
		EventKey:            taxonomy.EventDataSourceReconnectRequired,
		Title:               title,
		Message:             message,
		Severity:            "warning",
		Importance:          "important",
		SourceType:          &sourceType,
		SourceID:            &sourceID,
		RelatedResourceType: &sourceType,
		RelatedResourceID:   &sourceID,
		ActionURL:           &actionURL,
		ActionLabel:         &actionLabel,
		RequiresAction:      true,
		GroupKey:            &groupKey,
		Details:             details,
	}
}

// TestDataSourceReconnectRequiredIsRegistered pins the taxonomy entry the whole
// feature depends on. notifications.Create rejects an unknown event key and the
// project-member fan-out swallows that rejection at warn, so a missing entry
// makes the feature a silent no-op that a fake publisher would still pass.
func TestDataSourceReconnectRequiredIsRegistered(t *testing.T) {
	entry, ok := taxonomy.Lookup(taxonomy.EventDataSourceReconnectRequired)
	require.True(t, ok, "the event key must resolve through the taxonomy")
	require.Equal(t, taxonomy.ScopeProject, entry.Scope)
	require.Equal(t, taxonomy.DeliveryRequired, entry.Delivery)
	require.True(t, entry.RequiresAction)
	require.True(t, entry.Actionable)
	require.Equal(t, "data-sources", entry.Category)
	require.True(t, taxonomy.ResolveDelivery(entry, nil),
		"a required event is delivered without an opt-in")
}

// TestCreateOrReopen_ReopensAResolvedItem covers the case a plain coalescing
// insert swallows: ResolveResource sets action_status, not cleared_at, so the
// resolved row still occupies the partial unique index and the next park has to
// reopen it rather than be dropped.
func TestCreateOrReopen_ReopensAResolvedItem(t *testing.T) {
	if testing.Short() {
		testdb.SkipOrFatal(t, "skipping database integration test in short mode")
	}
	h := newReopenHarness(t, 1)
	sourceID := uuid.NewString()
	groupKey := "data_source:" + sourceID

	first, created, err := h.svc.CreateOrReopen(h.ctx, h.reconnectInput(sourceID,
		"Google Drive (Team) needs reconnecting", "Token has been expired or revoked."))
	require.NoError(t, err)
	require.True(t, created)
	require.NotNil(t, first)
	require.True(t, first.RequiresAction)

	resolved, err := h.svc.ResolveResource(h.ctx, h.projectID, "data_source", sourceID)
	require.NoError(t, err)
	require.Equal(t, int64(1), resolved)

	second, created, err := h.svc.CreateOrReopen(h.ctx, h.reconnectInput(sourceID,
		"Google Drive (Team) needs reconnecting", "invalid_grant: revoked"))
	require.NoError(t, err)
	require.False(t, created, "a park after a resolve must reopen, not insert")
	require.Equal(t, first.ID, second.ID, "one unresolved item per source")
	require.Equal(t, "invalid_grant: revoked", second.Message,
		"the item must carry the latest remediation")
	require.Nil(t, second.ActionStatus, "the reopen un-resolves the item")
	require.Nil(t, second.ActionStatusAt)
	require.False(t, second.Read)
	require.Nil(t, second.SnoozedUntil)
	require.Equal(t, first.CreatedAt.UTC(), second.CreatedAt.UTC(), "age is history")

	require.Len(t, activeRowsForKey(t, h.repo, h.userIDs[0], groupKey), 1)
}

// TestCreateOrReopen_ReopensASnoozedAndReadItem is the column contract that
// breaks the feature if missed: Snooze does not clear the row, so the snoozed
// row is the one the reopen updates — and every inbox tab filters a live
// snooze, so an unreset snoozed_until leaves the item unresolved in the
// database and invisible in the inbox.
func TestCreateOrReopen_ReopensASnoozedAndReadItem(t *testing.T) {
	if testing.Short() {
		testdb.SkipOrFatal(t, "skipping database integration test in short mode")
	}
	h := newReopenHarness(t, 1)
	sourceID := uuid.NewString()

	first, created, err := h.svc.CreateOrReopen(h.ctx, h.reconnectInput(sourceID,
		"GitHub (repo) needs reconnecting", "Bad credentials."))
	require.NoError(t, err)
	require.True(t, created)

	require.NoError(t, h.repo.MarkRead(h.ctx, h.userIDs[0], first.ID))
	require.NoError(t, h.repo.Snooze(h.ctx, h.userIDs[0], first.ID, time.Now().Add(24*time.Hour)))

	listed, err := h.repo.List(h.ctx, h.userIDs[0], ListParams{Tab: TabAll})
	require.NoError(t, err)
	require.Empty(t, listed, "a live snooze hides the item")

	second, created, err := h.svc.CreateOrReopen(h.ctx, h.reconnectInput(sourceID,
		"GitHub (repo) needs reconnecting", "Bad credentials."))
	require.NoError(t, err)
	require.False(t, created)
	require.False(t, second.Read, "a reopened item must move the unread count")
	require.Nil(t, second.ReadAt)
	require.Nil(t, second.SnoozedUntil, "a reopened item must not stay snoozed")

	listed, err = h.repo.List(h.ctx, h.userIDs[0], ListParams{Tab: TabAll})
	require.NoError(t, err)
	require.Len(t, listed, 1)
	require.Equal(t, second.ID, listed[0].ID)
}

// TestCreateOrReopen_DismissedRowIsNotResurrected: Dismiss sets cleared_at, which
// takes the row off the partial index, so the insert is a genuine conflict-free
// insert. The member dismissed an alert they had seen; the next failure is news.
func TestCreateOrReopen_DismissedRowIsNotResurrected(t *testing.T) {
	if testing.Short() {
		testdb.SkipOrFatal(t, "skipping database integration test in short mode")
	}
	h := newReopenHarness(t, 1)
	sourceID := uuid.NewString()
	groupKey := "data_source:" + sourceID

	first, created, err := h.svc.CreateOrReopen(h.ctx, h.reconnectInput(sourceID,
		"Google Drive (Team) needs reconnecting", "Token has been expired or revoked."))
	require.NoError(t, err)
	require.True(t, created)
	require.NoError(t, h.repo.Dismiss(h.ctx, h.userIDs[0], first.ID))

	second, created, err := h.svc.CreateOrReopen(h.ctx, h.reconnectInput(sourceID,
		"Google Drive (Team) needs reconnecting", "Token has been expired or revoked."))
	require.NoError(t, err)
	require.True(t, created, "a dismissed item is not resurrected")
	require.NotEqual(t, first.ID, second.ID)

	require.Len(t, activeRowsForKey(t, h.repo, h.userIDs[0], groupKey), 1)

	dismissed := new(Notification)
	require.NoError(t, h.db.NewSelect().Model(dismissed).Where("id = ?", first.ID).Scan(h.ctx))
	require.NotNil(t, dismissed.ClearedAt, "the dismissed row stays dismissed")
}

// TestCreateCoalescing_DoesNotReopenAResolvedItem keeps the older producers on
// their established semantics: extraction's review-ready alert coalesces, and
// reopening someone's resolved item is a different decision from that.
func TestCreateCoalescing_DoesNotReopenAResolvedItem(t *testing.T) {
	if testing.Short() {
		testdb.SkipOrFatal(t, "skipping database integration test in short mode")
	}
	h := newReopenHarness(t, 1)
	sourceID := uuid.NewString()

	in := h.reconnectInput(sourceID, "title", "message")
	created, err := h.svc.Create(h.ctx, in)
	require.NoError(t, err)
	require.NotNil(t, created)

	resolved, err := h.svc.ResolveResource(h.ctx, h.projectID, "data_source", sourceID)
	require.NoError(t, err)
	require.Equal(t, int64(1), resolved)

	again, err := h.svc.Create(h.ctx, in)
	require.NoError(t, err)
	require.Nil(t, again, "Create still coalesces a repeated group key")

	stored := new(Notification)
	require.NoError(t, h.db.NewSelect().Model(stored).Where("id = ?", created.ID).Scan(h.ctx))
	require.NotNil(t, stored.ActionStatus, "the resolved item stays resolved")
}

// TestCreateOrReopenForProjectMembers_LeavesEachMemberAnItem covers the fan-out:
// one unresolved item per member, and a second park leaves exactly one each
// rather than stacking duplicates.
func TestCreateOrReopenForProjectMembers_LeavesEachMemberAnItem(t *testing.T) {
	if testing.Short() {
		testdb.SkipOrFatal(t, "skipping database integration test in short mode")
	}
	h := newReopenHarness(t, 2)
	sourceID := uuid.NewString()
	groupKey := "data_source:" + sourceID

	require.NoError(t, h.svc.CreateOrReopenForProjectMembers(h.ctx, h.projectID,
		h.reconnectInput(sourceID, "Google Drive (Team) needs reconnecting", "Token has been expired or revoked.")))

	rows := []Notification{}
	require.NoError(t, h.db.NewSelect().Model(&rows).
		Where("group_key = ?", groupKey).Scan(h.ctx))
	require.Len(t, rows, 2, "one item per project member")

	for _, row := range rows {
		require.True(t, row.RequiresAction)
		require.Nil(t, row.ActionStatus)
		require.Equal(t, "data-sources", ptrString(row.Category))
	}

	// A second park reopens the members' existing items instead of adding new
	// ones, even though they are still unresolved.
	require.NoError(t, h.svc.CreateOrReopenForProjectMembers(h.ctx, h.projectID,
		h.reconnectInput(sourceID, "Google Drive (Team) needs reconnecting", "invalid_grant: revoked")))

	rows = []Notification{}
	require.NoError(t, h.db.NewSelect().Model(&rows).
		Where("group_key = ?", groupKey).Scan(h.ctx))
	require.Len(t, rows, 2, "a second park must not stack a second item per member")
	for _, row := range rows {
		require.Equal(t, "invalid_grant: revoked", row.Message)
		require.Nil(t, row.ActionStatus)
	}
}

func ptrString(p *string) string {
	if p == nil {
		return ""
	}
	return *p
}
