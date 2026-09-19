package backups

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/uptrace/bun"

	"github.com/emergent-company/emergent.memory/internal/testutil"
)

// TestExportChatConversationsDanglingObjectRef pins the exporter guard that
// NULLs kb.chat_conversations.object_id when the referenced kb.graph_objects
// row is soft-deleted (and therefore excluded from the archive), while keeping
// the conversation row itself. It also proves the guard is conditional: when
// IncludeDeleted is set the graph object is exported, so the raw object_id is
// kept.
//
// Skips cleanly when Postgres is unavailable.
func TestExportChatConversationsDanglingObjectRef(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping database integration test in short mode")
	}
	ctx := context.Background()
	testDB, err := testutil.SetupTestDB(ctx, "bkupexp")
	if err != nil {
		t.Skipf("skipping: test database unavailable: %v", err)
	}
	t.Cleanup(testDB.Close)

	db := testDB.DB

	orgID := uuid.NewString()
	if err := testutil.CreateTestOrganization(ctx, db, orgID, "Backup Export Org"); err != nil {
		t.Fatalf("create org: %v", err)
	}
	projectID := uuid.NewString()
	if err := testutil.CreateTestProject(ctx, db, testutil.TestProject{
		ID:    projectID,
		OrgID: orgID,
		Name:  "Backup Export Project",
	}, testutil.AdminUser.ID); err != nil {
		t.Fatalf("create project: %v", err)
	}

	now := time.Now().UTC()
	liveObjID := insertGraphObject(t, ctx, db, projectID, "Note", now, nil)
	softDeletedObjID := insertGraphObject(t, ctx, db, projectID, "Note", now, &now)

	insertChatConversation(t, ctx, db, projectID, "live", liveObjID)
	insertChatConversation(t, ctx, db, projectID, "dangling", softDeletedObjID)

	cfg := chatConversationsConfig(t)
	exporter := NewExporter(db, slog.Default())

	// Excluding soft-deleted rows: the conversation pointing at the
	// soft-deleted object must export with a NULL object_id while the live
	// reference is preserved.
	var excluded bytes.Buffer
	if _, err := exporter.exportTable(ctx, cfg, &excluded, ExportOptions{ProjectID: projectID, IncludeChat: true}); err != nil {
		t.Fatalf("export excluding deleted: %v", err)
	}
	excludedRows := decodeNDJSON(t, excluded.Bytes())

	liveRow := rowByTitle(t, excludedRows, "live")
	if got := liveRow["object_id"]; got != liveObjID.String() {
		t.Errorf("live conversation object_id = %v, want %s", got, liveObjID)
	}
	danglingRow := rowByTitle(t, excludedRows, "dangling")
	if v := danglingRow["object_id"]; v != nil {
		t.Errorf("dangling conversation object_id = %v, want nil", v)
	}

	// Including soft-deleted rows: the graph object is present in the archive,
	// so object_id is emitted unchanged.
	var included bytes.Buffer
	if _, err := exporter.exportTable(ctx, cfg, &included, ExportOptions{ProjectID: projectID, IncludeChat: true, IncludeDeleted: true}); err != nil {
		t.Fatalf("export including deleted: %v", err)
	}
	includedRows := decodeNDJSON(t, included.Bytes())
	danglingIncluded := rowByTitle(t, includedRows, "dangling")
	if got := danglingIncluded["object_id"]; got != softDeletedObjID.String() {
		t.Errorf("dangling conversation object_id with IncludeDeleted = %v, want %s", got, softDeletedObjID)
	}
}

func insertGraphObject(t *testing.T, ctx context.Context, db bun.IDB, projectID, typ string, createdAt time.Time, deletedAt *time.Time) uuid.UUID {
	t.Helper()
	id := uuid.New()
	var deletedAtArg any
	if deletedAt != nil {
		deletedAtArg = *deletedAt
	}
	_, err := db.NewRaw(`
		INSERT INTO kb.graph_objects
			(id, project_id, branch_id, canonical_id, supersedes_id, version, type, status,
			 properties, labels, created_at, updated_at, deleted_at)
		VALUES (?, ?, NULL, ?, NULL, 1, ?, 'active', '{}'::jsonb, '{}'::text[], ?, ?, ?)
	`, id.String(), projectID, id.String(), typ, createdAt, createdAt, deletedAtArg).Exec(ctx)
	if err != nil {
		t.Fatalf("insert %s graph object: %v", typ, err)
	}
	return id
}

func insertChatConversation(t *testing.T, ctx context.Context, db bun.IDB, projectID, title string, objectID uuid.UUID) uuid.UUID {
	t.Helper()
	id := uuid.New()
	now := time.Now().UTC()
	_, err := db.NewRaw(`
		INSERT INTO kb.chat_conversations (id, title, project_id, object_id, is_private, created_at, updated_at)
		VALUES (?, ?, ?, ?, true, ?, ?)
	`, id.String(), title, projectID, objectID.String(), now, now).Exec(ctx)
	if err != nil {
		t.Fatalf("insert chat conversation %q: %v", title, err)
	}
	return id
}

func decodeNDJSON(t *testing.T, b []byte) []map[string]any {
	t.Helper()
	dec := json.NewDecoder(bytes.NewReader(b))
	var rows []map[string]any
	for {
		var row map[string]any
		if err := dec.Decode(&row); err != nil {
			if err == io.EOF {
				break
			}
			t.Fatalf("decode ndjson: %v", err)
		}
		rows = append(rows, row)
	}
	return rows
}

func rowByTitle(t *testing.T, rows []map[string]any, title string) map[string]any {
	t.Helper()
	for _, r := range rows {
		if r["title"] == title {
			return r
		}
	}
	t.Fatalf("row with title %q not found in %v", title, rows)
	return nil
}
