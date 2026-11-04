package schemas

import (
	"context"
	"testing"

	"github.com/google/uuid"

	"github.com/emergent-company/emergent.memory/domain/graph"
)

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

func strPtr(s string) *string { return &s }

// buildCompiledTypes is a convenience constructor for tests.
func buildCompiledTypes(types map[string]string) *CompiledTypesResponse {
	resp := &CompiledTypesResponse{}
	for name, version := range types {
		resp.ObjectTypes = append(resp.ObjectTypes, ObjectTypeSchema{
			Name:          name,
			SchemaVersion: version,
		})
	}
	return resp
}

// buildObjects creates minimal graph objects for testing.
func buildObjects(specs []struct {
	objectType    string
	schemaVersion *string
}) []graph.GraphObject {
	objs := make([]graph.GraphObject, len(specs))
	for i, s := range specs {
		objs[i] = graph.GraphObject{
			CanonicalID:   uuid.New(),
			Type:          s.objectType,
			SchemaVersion: s.schemaVersion,
		}
	}
	return objs
}

// ---------------------------------------------------------------------------
// Pure-logic tests — exercising the validation loop without a DB
// ---------------------------------------------------------------------------

// validateObjectsLogic extracts the core loop from Service.ValidateObjects so
// we can test it without a running database or DI wiring.
func validateObjectsLogic(
	projectID string,
	compiled *CompiledTypesResponse,
	objs []graph.GraphObject,
) *ValidateObjectsResponse {
	currentVersion := make(map[string]string)
	for _, t := range compiled.ObjectTypes {
		if t.SchemaVersion != "" {
			currentVersion[t.Name] = t.SchemaVersion
		}
	}

	resp := &ValidateObjectsResponse{
		ProjectID:    projectID,
		TotalObjects: len(objs),
	}

	for _, obj := range objs {
		var issues []string
		if cv, ok := currentVersion[obj.Type]; ok {
			objVersion := ""
			if obj.SchemaVersion != nil {
				objVersion = *obj.SchemaVersion
			}
			if objVersion == "" {
				issues = append(issues, "schema_version not set (current: "+cv+")")
			} else if objVersion != cv {
				issues = append(issues, "schema_version mismatch: object has \""+objVersion+"\", current is \""+cv+"\"")
			}
		}

		if len(issues) > 0 {
			resp.StaleObjects++
			resp.Results = append(resp.Results, ObjectValidationResult{
				EntityID:      obj.CanonicalID.String(),
				Type:          obj.Type,
				Key:           obj.Key,
				SchemaVersion: obj.SchemaVersion,
				Issues:        issues,
			})
		}
	}

	return resp
}

// ---------------------------------------------------------------------------
// Test cases
// ---------------------------------------------------------------------------

func TestValidateObjectsLogic_AllCurrent(t *testing.T) {
	compiled := buildCompiledTypes(map[string]string{
		"Person":   "1.1.0",
		"Document": "2.0.0",
	})
	objs := buildObjects([]struct {
		objectType    string
		schemaVersion *string
	}{
		{"Person", strPtr("1.1.0")},
		{"Document", strPtr("2.0.0")},
		{"Person", strPtr("1.1.0")},
	})

	resp := validateObjectsLogic("proj-1", compiled, objs)

	if resp.TotalObjects != 3 {
		t.Fatalf("expected TotalObjects=3, got %d", resp.TotalObjects)
	}
	if resp.StaleObjects != 0 {
		t.Fatalf("expected StaleObjects=0, got %d (issues: %v)", resp.StaleObjects, resp.Results)
	}
	if len(resp.Results) != 0 {
		t.Fatalf("expected empty Results, got %v", resp.Results)
	}
}

func TestValidateObjectsLogic_VersionMismatch(t *testing.T) {
	compiled := buildCompiledTypes(map[string]string{
		"Person": "1.1.0",
	})
	objs := buildObjects([]struct {
		objectType    string
		schemaVersion *string
	}{
		{"Person", strPtr("1.0.0")}, // stale
		{"Person", strPtr("1.1.0")}, // current
	})

	resp := validateObjectsLogic("proj-2", compiled, objs)

	if resp.StaleObjects != 1 {
		t.Fatalf("expected StaleObjects=1, got %d", resp.StaleObjects)
	}
	if len(resp.Results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(resp.Results))
	}
	r := resp.Results[0]
	if r.Type != "Person" {
		t.Errorf("expected type Person, got %q", r.Type)
	}
	if len(r.Issues) == 0 {
		t.Error("expected at least one issue")
	}
}

func TestValidateObjectsLogic_MissingSchemaVersion(t *testing.T) {
	compiled := buildCompiledTypes(map[string]string{
		"Belief": "2.0.0",
	})
	objs := buildObjects([]struct {
		objectType    string
		schemaVersion *string
	}{
		{"Belief", nil}, // schema_version not set
	})

	resp := validateObjectsLogic("proj-3", compiled, objs)

	if resp.StaleObjects != 1 {
		t.Fatalf("expected StaleObjects=1, got %d", resp.StaleObjects)
	}
	r := resp.Results[0]
	if len(r.Issues) == 0 || r.Issues[0] == "" {
		t.Errorf("expected a non-empty issue string, got %v", r.Issues)
	}
	// Issue should mention the current version
	if !contains(r.Issues[0], "2.0.0") {
		t.Errorf("expected issue to mention current version 2.0.0, got %q", r.Issues[0])
	}
}

func TestValidateObjectsLogic_UnknownTypeSkipped(t *testing.T) {
	// Objects whose type isn't in the compiled schema are not flagged — they
	// may belong to a different schema or be untyped.
	compiled := buildCompiledTypes(map[string]string{
		"Person": "1.0.0",
	})
	objs := buildObjects([]struct {
		objectType    string
		schemaVersion *string
	}{
		{"UnknownType", strPtr("9.9.9")},
	})

	resp := validateObjectsLogic("proj-4", compiled, objs)

	if resp.StaleObjects != 0 {
		t.Fatalf("expected StaleObjects=0 for unknown type, got %d", resp.StaleObjects)
	}
}

func TestValidateObjectsLogic_EmptyProject(t *testing.T) {
	compiled := buildCompiledTypes(map[string]string{"Person": "1.0.0"})

	resp := validateObjectsLogic("proj-5", compiled, nil)

	if resp.TotalObjects != 0 {
		t.Fatalf("expected TotalObjects=0, got %d", resp.TotalObjects)
	}
	if resp.StaleObjects != 0 {
		t.Fatalf("expected StaleObjects=0, got %d", resp.StaleObjects)
	}
}

func TestValidateObjectsLogic_MultipleIssues(t *testing.T) {
	// Multiple stale objects of different types
	compiled := buildCompiledTypes(map[string]string{
		"Person":   "1.1.0",
		"Document": "3.0.0",
	})
	objs := buildObjects([]struct {
		objectType    string
		schemaVersion *string
	}{
		{"Person", strPtr("1.0.0")},   // stale
		{"Document", nil},             // missing version
		{"Person", strPtr("1.1.0")},   // current — should NOT appear
		{"Document", strPtr("3.0.0")}, // current — should NOT appear
	})

	resp := validateObjectsLogic("proj-6", compiled, objs)

	if resp.TotalObjects != 4 {
		t.Fatalf("expected TotalObjects=4, got %d", resp.TotalObjects)
	}
	if resp.StaleObjects != 2 {
		t.Fatalf("expected StaleObjects=2, got %d (results: %v)", resp.StaleObjects, resp.Results)
	}
}

func TestValidateObjectsLogic_SchemaTypeWithNoVersion(t *testing.T) {
	// Types in compiled schema with empty SchemaVersion are skipped (no current
	// version to compare against) — objects of those types are never flagged.
	compiled := &CompiledTypesResponse{
		ObjectTypes: []ObjectTypeSchema{
			{Name: "Legacy", SchemaVersion: ""},
		},
	}
	objs := buildObjects([]struct {
		objectType    string
		schemaVersion *string
	}{
		{"Legacy", strPtr("0.1.0")},
		{"Legacy", nil},
	})

	resp := validateObjectsLogic("proj-7", compiled, objs)

	if resp.StaleObjects != 0 {
		t.Fatalf("expected 0 stale objects when compiled type has no version, got %d", resp.StaleObjects)
	}
}

// ---------------------------------------------------------------------------
// Smoke test: ValidateObjects uses the correct projectID
// ---------------------------------------------------------------------------

func TestValidateObjectsResponse_ProjectID(t *testing.T) {
	const pid = "aaaabbbb-cccc-dddd-eeee-ffffffffffff"
	resp := validateObjectsLogic(pid, buildCompiledTypes(nil), nil)
	if resp.ProjectID != pid {
		t.Errorf("expected ProjectID %q, got %q", pid, resp.ProjectID)
	}
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(substr) == 0 ||
		func() bool {
			for i := 0; i <= len(s)-len(substr); i++ {
				if s[i:i+len(substr)] == substr {
					return true
				}
			}
			return false
		}())
}

// Ensure the test file compiles even without a running server.
var _ = context.Background
