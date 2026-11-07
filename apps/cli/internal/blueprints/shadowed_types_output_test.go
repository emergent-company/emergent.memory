package blueprints

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	sdkschemas "github.com/emergent-company/emergent.memory/apps/server/pkg/sdk/schemas"
)

// TestCreatePackOutput_ReportsShadowedTypes pins the #338 part-2 install
// output: when the server reports that a type declared by this pack is skipped
// because another pack owns it, the blueprint install must say so. Before the
// fix the CLI discarded the assign result and the operator saw a clean
// "created pack" while GET /api/schemas and compiled types named different
// packs.
func TestCreatePackOutput_ReportsShadowedTypes(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/api/schemas":
			_, _ = w.Write([]byte(`{"id":"pack-b","name":"bb-companies","version":"0.1.0"}`))
		case r.Method == http.MethodPost && r.URL.Path == "/api/schemas/projects/proj-1/assign":
			_, _ = w.Write([]byte(`{
				"schema_id":"pack-b",
				"schema_name":"bb-companies",
				"installed_types":[],
				"skipped_types":["Person"],
				"shadowed_types":[{
					"type_name":"Person",
					"registry_owner_pack":"aa-core",
					"registry_owner_schema_id":"pack-a",
					"shadowing_pack":"bb-companies",
					"shadowing_schema_id":"pack-b"
				}]
			}`))
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)

	var buf bytes.Buffer
	b := NewBlueprintsApplier(nil, "proj-1", sdkschemas.NewClient(http.DefaultClient, srv.URL, noopAuthRuntime{}, "", "proj-1"), nil, nil, nil, false, false, &buf)

	res := b.createPack(context.Background(), PackFile{
		Name:    "bb-companies",
		Version: "0.1.0",
		ObjectTypes: []ObjectTypeDef{{
			Name:       "Person",
			Properties: map[string]any{"legal_name": map[string]any{"type": "string"}},
		}},
	})
	if res.Action != BlueprintsActionCreated {
		t.Fatalf("expected created result, got %s (%v)", res.Action, res.Error)
	}
	if len(res.ShadowedTypes) != 1 || res.ShadowedTypes[0].TypeName != "Person" {
		t.Fatalf("result must carry the shadowed type: %+v", res.ShadowedTypes)
	}

	b.printResult(res)
	out := buf.String()
	for _, want := range []string{
		`created  pack "bb-companies"`,
		`shadowed type "Person": registry owner "aa-core", effective definition from "bb-companies"`,
		"pass --merge-types to merge",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("output missing %q:\n%s", want, out)
		}
	}
}

// TestPrintResult_NoShadowNoteWhenClean keeps the output quiet for the ordinary
// case: a pack result with no cross-pack skips prints no shadow line.
func TestPrintResult_NoShadowNoteWhenClean(t *testing.T) {
	var buf bytes.Buffer
	b := NewBlueprintsApplier(nil, "", nil, nil, nil, nil, false, false, &buf)

	b.printResult(BlueprintsResult{ResourceType: "pack", Name: "aa-core", Action: BlueprintsActionCreated})
	if strings.Contains(buf.String(), "shadowed type") {
		t.Fatalf("clean install must not print a shadow note:\n%s", buf.String())
	}
}
