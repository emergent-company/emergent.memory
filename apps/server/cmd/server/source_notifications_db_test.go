package main

import (
	"context"
	"io"
	"log/slog"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/emergent-company/emergent.memory/domain/notifications"
	"github.com/emergent-company/emergent.memory/domain/sources"
	"github.com/emergent-company/emergent.memory/internal/testdb"
)

// TestSourceNotificationPublisher_Lifecycle exercises the real seam between the
// sources domain and the notifications domain: the adapter in this package over
// a notifications service backed by Postgres. The sources-domain tests drive a
// recording publisher, and the notifications-domain tests drive repository
// rows directly; neither would catch an adapter that silently drops a field or
// a grouping key that does not coalesce. This one would.
func TestSourceNotificationPublisher_Lifecycle(t *testing.T) {
	if testing.Short() {
		testdb.SkipOrFatal(t, "skipping database integration test in short mode")
	}
	ctx := context.Background()
	tdb := testdb.SetupTestDBOrFail(t, ctx, "source_notify_adapter")
	t.Cleanup(tdb.Close)
	db := tdb.GetDB()

	// One project with two members: the fan-out is per member, and the grouping
	// is per (member, source).
	orgID, projectID := uuid.NewString(), uuid.NewString()
	_, err := db.NewRaw(`INSERT INTO kb.orgs (id, name, created_at, updated_at)
		VALUES (?, 'Adapter Org', now(), now())`, orgID).Exec(ctx)
	require.NoError(t, err)
	_, err = db.NewRaw(`INSERT INTO kb.projects (id, name, organization_id, created_at, updated_at)
		VALUES (?, 'Adapter Project', ?, now(), now())`, projectID, orgID).Exec(ctx)
	require.NoError(t, err)

	for i := 0; i < 2; i++ {
		userID := uuid.NewString()
		_, err = db.NewRaw(`INSERT INTO core.user_profiles (id, zitadel_user_id, display_name, created_at, updated_at)
			VALUES (?, ?, 'Member', now(), now())`, userID, "zitadel-"+userID).Exec(ctx)
		require.NoError(t, err)
		_, err = db.NewRaw(`INSERT INTO kb.project_memberships (project_id, user_id, role, created_at)
			VALUES (?, ?, 'member', now())`, projectID, userID).Exec(ctx)
		require.NoError(t, err)
	}

	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	svc := notifications.NewService(notifications.NewRepository(db, log), nil, log)
	pub := newSourceNotificationPublisher(svc)

	sourceA, sourceB := uuid.NewString(), uuid.NewString()
	publish := func(sourceID, message string) {
		t.Helper()
		require.NoError(t, pub.PublishDataSourceNeedsReconnect(ctx, projectID, sources.DataSourceReconnectNotice{
			SourceID:    sourceID,
			Provider:    sources.ProviderTypeGoogleDrive,
			SourceName:  "Docs",
			GroupKey:    sources.NotificationGroupKey(sourceID),
			Title:       "Google Drive (Docs) needs reconnecting",
			Message:     message,
			ActionURL:   "/data-sources/" + sourceID,
			ActionLabel: "Reconnect",
		}))
	}
	list := func() []notifications.Notification {
		t.Helper()
		var rows []notifications.Notification
		require.NoError(t, db.NewSelect().
			Model(&rows).
			Where("project_id = ?", projectID).
			Order("group_key ASC", "user_id ASC").
			Scan(ctx, &rows))
		return rows
	}

	// A park publishes one item per member, and a second publish for the same
	// source is the same item, not a second row.
	publish(sourceA, "reconnect the source")
	publish(sourceA, "reconnect the source again")
	publish(sourceB, "reconnect the source")

	rows := list()
	require.Len(t, rows, 4, "two sources × two members, with source A published twice")
	for _, row := range rows {
		require.True(t, row.RequiresAction)
		require.Nil(t, row.ActionStatus, "a fresh park is unresolved")
		require.NotNil(t, row.GroupKey)
		require.NotNil(t, row.RelatedResourceID)
		require.Equal(t, "data_source", *row.RelatedResourceType)
		require.Equal(t, "/data-sources/"+*row.RelatedResourceID, *row.ActionURL)
		require.Contains(t, row.Message, "reconnect the source")
	}
	byGroup := map[string]int{}
	for _, row := range rows {
		byGroup[*row.GroupKey]++
	}
	require.Equal(t, 2, byGroup["data_source:"+sourceA])
	require.Equal(t, 2, byGroup["data_source:"+sourceB])

	// Repairing source A resolves its item for every member and leaves source B.
	require.NoError(t, pub.ResolveDataSource(ctx, projectID, sourceA))
	rows = list()
	require.Len(t, rows, 4)
	for _, row := range rows {
		switch *row.RelatedResourceID {
		case sourceA:
			require.NotNil(t, row.ActionStatus)
			require.Equal(t, "resolved", *row.ActionStatus)
		case sourceB:
			require.Nil(t, row.ActionStatus)
		default:
			t.Fatalf("unexpected related resource %q", *row.RelatedResourceID)
		}
	}

	// A later park reopens source A's resolved rows (the unique index still holds
	// them) instead of silently coalescing into the resolved item, and carries the
	// latest remediation.
	publish(sourceA, "reconnect the source after the second failure")
	rows = list()
	require.Len(t, rows, 4, "a reopen must not add a row")
	for _, row := range rows {
		if *row.RelatedResourceID == sourceA {
			require.Nil(t, row.ActionStatus, "the reopened item is unresolved again")
			require.Contains(t, row.Message, "second failure")
		}
	}
}
