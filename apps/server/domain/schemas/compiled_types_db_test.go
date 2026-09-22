package schemas_test

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/emergent-company/emergent.memory/domain/schemas"
	"github.com/emergent-company/emergent.memory/internal/testutil"
)

// TestCompiledTypesByProjectUIResolution exercises the compiled-types repository
// path (GetCompiledTypesByProject) end to end against a live database. It covers
// the type-level ui metadata resolution: inline "ui" blocks carried through the
// parse, the top-level ui_configs fallback, inline-over-ui_configs precedence,
// and explicit JSON nulls being treated as absent. Requires Postgres; the test
// skips when the database is unreachable.
func TestCompiledTypesByProjectUIResolution(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping database integration test in short mode")
	}

	ctx := context.Background()
	testDB := testutil.SetupTestDBOrFail(t, ctx, "schemasui")
	defer testDB.Close()

	db := testDB.GetDB()
	repo := schemas.NewRepository(db, slog.Default())

	orgID := uuid.NewString()
	if err := testutil.CreateTestOrganization(ctx, db, orgID, "UI Resolution Test Org"); err != nil {
		t.Fatalf("create org: %v", err)
	}

	inlineUI := `{"icon":"lucide--user","color":"#4F46E5"}`
	configUI := `{"icon":"lucide--folder","color":"#F59E0B"}`

	tests := []struct {
		name          string
		objectSchemas string
		uiConfigs     string
		wantUI        string // expected ui bytes on the Person type ("" = absent)
	}{
		{
			name:          "inline ui present only",
			objectSchemas: `[{"name":"Person","label":"Person","ui":` + inlineUI + `}]`,
			uiConfigs:     `{}`,
			wantUI:        inlineUI,
		},
		{
			name:          "ui_configs present only, fallback used",
			objectSchemas: `[{"name":"Person","label":"Person"}]`,
			uiConfigs:     `{"Person":` + configUI + `}`,
			wantUI:        configUI,
		},
		{
			name:          "inline ui present wins over ui_configs",
			objectSchemas: `[{"name":"Person","label":"Person","ui":` + inlineUI + `}]`,
			uiConfigs:     `{"Person":` + configUI + `}`,
			wantUI:        inlineUI,
		},
		{
			name:          "inline ui null falls back to ui_configs, no ui null emitted",
			objectSchemas: `[{"name":"Person","label":"Person","ui":null}]`,
			uiConfigs:     `{"Person":` + configUI + `}`,
			wantUI:        configUI,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			projectID := uuid.NewString()
			if err := testutil.CreateTestProject(ctx, db, testutil.TestProject{
				ID:    projectID,
				OrgID: orgID,
				Name:  "Project " + tt.name,
			}, testutil.AdminUser.ID); err != nil {
				t.Fatalf("create project: %v", err)
			}

			now := time.Now()
			schema := &schemas.GraphMemorySchema{
				ID:                      uuid.NewString(),
				Name:                    "schema-" + uuid.NewString(),
				Version:                 "1.0.0",
				ObjectTypeSchemas:       json.RawMessage(tt.objectSchemas),
				RelationshipTypeSchemas: json.RawMessage(`[]`),
				UIConfigs:               json.RawMessage(tt.uiConfigs),
				ExtractionPrompts:       json.RawMessage(`{}`),
				PublishedAt:             &now,
				CreatedAt:               now,
				UpdatedAt:               now,
			}
			if _, err := db.NewInsert().Model(schema).Exec(ctx); err != nil {
				t.Fatalf("insert graph schema: %v", err)
			}

			assignment := &schemas.ProjectMemorySchema{
				ID:          uuid.NewString(),
				ProjectID:   projectID,
				SchemaID:    schema.ID,
				Active:      true,
				InstalledAt: now,
				CreatedAt:   now,
				UpdatedAt:   now,
			}
			if _, err := db.NewInsert().Model(assignment).Exec(ctx); err != nil {
				t.Fatalf("insert project schema assignment: %v", err)
			}

			resp, err := repo.GetCompiledTypesByProject(ctx, projectID)
			if err != nil {
				t.Fatalf("GetCompiledTypesByProject: %v", err)
			}

			var person *schemas.ObjectTypeSchema
			for i := range resp.ObjectTypes {
				if resp.ObjectTypes[i].Name == "Person" {
					person = &resp.ObjectTypes[i]
					break
				}
			}
			if person == nil {
				t.Fatalf("expected compiled type Person, got %+v", resp.ObjectTypes)
			}
			// The jsonb column round-trips through Postgres, which normalizes
			// whitespace, so compare compacted JSON rather than raw bytes.
			gotUI := ""
			if len(person.UI) > 0 {
				gotUI = compactJSON(t, person.UI)
			}
			wantUI := ""
			if tt.wantUI != "" {
				wantUI = compactJSON(t, []byte(tt.wantUI))
			}
			if gotUI != wantUI {
				t.Errorf("Person ui = %q, want %q", gotUI, wantUI)
			}

			raw, err := json.Marshal(person)
			if err != nil {
				t.Fatalf("marshal compiled type: %v", err)
			}
			if bytes.Contains(raw, []byte(`"ui":null`)) {
				t.Errorf("compiled output contains \"ui\":null: %s", raw)
			}
		})
	}
}

// compactJSON minifies raw JSON for byte-for-byte comparison.
func compactJSON(t *testing.T, raw []byte) string {
	t.Helper()
	var buf bytes.Buffer
	if err := json.Compact(&buf, raw); err != nil {
		t.Fatalf("compact JSON %q: %v", raw, err)
	}
	return buf.String()
}
