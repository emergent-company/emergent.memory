package blueprints

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	sdkprojects "github.com/emergent-company/emergent.memory/apps/server/pkg/sdk/projects"
	sdkschemas "github.com/emergent-company/emergent.memory/apps/server/pkg/sdk/schemas"
)

func TestBuildMergePreviewPacks(t *testing.T) {
	packs := []PackFile{{
		Name: "incoming",
		ObjectTypes: []ObjectTypeDef{{
			Name: "Widget",
			Properties: map[string]any{
				"name": map[string]any{"type": "string"},
			},
		}},
	}}

	got, err := BuildMergePreviewPacks(packs)
	if err != nil {
		t.Fatalf("BuildMergePreviewPacks: %v", err)
	}
	if len(got) != 1 || got[0].Name != "incoming" {
		t.Fatalf("unexpected packs: %+v", got)
	}
	var types []map[string]any
	if err := json.Unmarshal(got[0].ObjectTypes, &types); err != nil {
		t.Fatalf("objectTypes must be a JSON array: %v (%s)", err, got[0].ObjectTypes)
	}
	if len(types) != 1 || types[0]["name"] != "Widget" {
		t.Fatalf("unexpected object types: %+v", types)
	}
}

func TestPrintMergeConflicts(t *testing.T) {
	var buf bytes.Buffer
	PrintMergeConflicts(&buf, []sdkschemas.MergePreviewConflict{{
		TypeName:              "Widget",
		IncomingPackName:      "incoming",
		ExistingSchemaName:    "existing",
		ExistingSchemaVersion: "1.0.0",
		AddedProperties:       []string{"zeta", "email"},
		ConflictingProperties: []sdkschemas.PropertyConflict{{Property: "name", Resolution: "existing_wins"}},
	}})

	out := buf.String()
	for _, want := range []string{
		`type "Widget"`,
		`existing pack "existing 1.0.0"`,
		`incoming pack "incoming"`,
		"added properties: email, zeta",
		"conflicting properties (existing definition wins): name",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("output missing %q:\n%s", want, out)
		}
	}
}

func TestFormatMergeConflictNames(t *testing.T) {
	got := FormatMergeConflictNames([]sdkschemas.MergePreviewConflict{
		{TypeName: "Zeta", IncomingPackName: "pack-b"},
		{TypeName: "Alpha", IncomingPackName: "pack-a"},
	})
	if got != "Alpha (pack pack-a), Zeta (pack pack-b)" {
		t.Fatalf("unexpected summary %q", got)
	}
}

// mergeAssignServer captures AssignPack request bodies for the create/update
// paths and returns minimal successful responses.
type mergeAssignServer struct {
	assignBodies []map[string]any
}

func newMergeAssignServer(t *testing.T, installed bool) (*httptest.Server, *mergeAssignServer) {
	t.Helper()
	state := &mergeAssignServer{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/available"):
			_, _ = w.Write([]byte(`[]`))
		case r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/installed"):
			if installed {
				_, _ = w.Write([]byte(`[{"schemaId":"pack-1","name":"p","version":"1.0.0","active":true}]`))
			} else {
				_, _ = w.Write([]byte(`[]`))
			}
		case r.Method == http.MethodPost && r.URL.Path == "/api/schemas":
			_, _ = w.Write([]byte(`{"id":"pack-1","name":"p","version":"1.0.0"}`))
		case r.Method == http.MethodPut && r.URL.Path == "/api/schemas/pack-1":
			_, _ = w.Write([]byte(`{"id":"pack-1","name":"p","version":"1.0.0"}`))
		case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/assign"):
			var body map[string]any
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Errorf("decode assign body: %v", err)
			}
			state.assignBodies = append(state.assignBodies, body)
			_, _ = w.Write([]byte(`{"schema_id":"pack-1","installed_types":[]}`))
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	return srv, state
}

func TestBlueprinter_SetMergeTypes_CreateAssignsWithMerge(t *testing.T) {
	srv, state := newMergeAssignServer(t, false)
	packClient := sdkschemas.NewClient(http.DefaultClient, srv.URL, noopAuthRuntime{}, "", "proj-1")
	a := NewBlueprintsApplier(sdkprojects.NewClient(http.DefaultClient, srv.URL, noopAuthRuntime{}), "proj-1", packClient, nil, nil, nil, false, false, nil)
	a.SetMergeTypes(true)

	_, err := a.Run(context.Background(), nil, []PackFile{{
		Name: "p", Version: "1.0.0",
		ObjectTypes: []ObjectTypeDef{{Name: "Widget"}},
	}}, nil, nil)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(state.assignBodies) != 1 {
		t.Fatalf("expected 1 assign call, got %d", len(state.assignBodies))
	}
	if state.assignBodies[0]["merge"] != true {
		t.Fatalf("expected merge=true on create assign, got %+v", state.assignBodies[0])
	}
}

func TestBlueprinter_SetMergeTypes_UpdateAssignsWithMerge(t *testing.T) {
	srv, state := newMergeAssignServer(t, true)
	packClient := sdkschemas.NewClient(http.DefaultClient, srv.URL, noopAuthRuntime{}, "", "proj-1")
	a := NewBlueprintsApplier(sdkprojects.NewClient(http.DefaultClient, srv.URL, noopAuthRuntime{}), "proj-1", packClient, nil, nil, nil, false, true, nil)
	a.SetMergeTypes(true)

	_, err := a.Run(context.Background(), nil, []PackFile{{
		Name: "p", Version: "2.0.0",
		ObjectTypes: []ObjectTypeDef{{Name: "Widget"}},
	}}, nil, nil)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(state.assignBodies) != 1 {
		t.Fatalf("expected 1 assign call after update, got %d", len(state.assignBodies))
	}
	if state.assignBodies[0]["merge"] != true {
		t.Fatalf("expected merge=true on update assign, got %+v", state.assignBodies[0])
	}
}

func TestBlueprinter_WithoutMergeTypes_CreateAssignsWithoutMerge(t *testing.T) {
	srv, state := newMergeAssignServer(t, false)
	packClient := sdkschemas.NewClient(http.DefaultClient, srv.URL, noopAuthRuntime{}, "", "proj-1")
	a := NewBlueprintsApplier(sdkprojects.NewClient(http.DefaultClient, srv.URL, noopAuthRuntime{}), "proj-1", packClient, nil, nil, nil, false, false, nil)

	_, err := a.Run(context.Background(), nil, []PackFile{{
		Name: "p", Version: "1.0.0",
		ObjectTypes: []ObjectTypeDef{{Name: "Widget"}},
	}}, nil, nil)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(state.assignBodies) != 1 {
		t.Fatalf("expected 1 assign call, got %d", len(state.assignBodies))
	}
	if _, ok := state.assignBodies[0]["merge"]; ok {
		t.Fatalf("expected merge omitted when not confirmed, got %+v", state.assignBodies[0])
	}
}
