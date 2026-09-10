package schemas

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestPriorFromSchemaID(t *testing.T) {
	cases := []struct {
		name string
		job  *SchemaMigrationJob
		want string
	}{
		{"nil job", nil, ""},
		{"no chain falls back to overall from", &SchemaMigrationJob{FromSchemaID: "from-overall"}, "from-overall"},
		{
			"multi-hop chain uses final hop from",
			&SchemaMigrationJob{
				FromSchemaID: "from-overall",
				Chain: []MigrationHop{
					{FromSchemaID: "v1", ToSchemaID: "v1-1"},
					{FromSchemaID: "v1-1", ToSchemaID: "v2"},
				},
			},
			"v1-1",
		},
		{
			"chain final hop with empty from falls back",
			&SchemaMigrationJob{
				FromSchemaID: "from-overall",
				Chain:        []MigrationHop{{FromSchemaID: "", ToSchemaID: "v2"}},
			},
			"from-overall",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := priorFromSchemaID(tc.job); got != tc.want {
				t.Fatalf("priorFromSchemaID() = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestBuildRestoreTypeRegistryPlan(t *testing.T) {
	from := &GraphMemorySchema{
		ID: "from-id",
		ObjectTypeSchemas: json.RawMessage(`[
			{"name":"Person","properties":{"name":{"type":"string"}}},
			{"name":"Belief","properties":{"text":{"type":"string"}}}
		]`),
	}
	to := &GraphMemorySchema{
		ID: "to-id",
		ObjectTypeSchemas: json.RawMessage(`[
			{"name":"Person","properties":{"name":{"type":"string"},"age":{"type":"number"}}},
			{"name":"Organization","properties":{"title":{"type":"string"}}}
		]`),
	}

	actions, toOnly := buildRestoreTypeRegistryPlan(from, to)

	if len(actions) != 2 {
		t.Fatalf("expected 2 restore actions, got %d", len(actions))
	}
	byName := map[string]typeAction{}
	for _, a := range actions {
		byName[a.name] = a
		if a.action != "restore" {
			t.Errorf("action %q = %q, want restore", a.name, a.action)
		}
		if len(a.ownedSchemaIDs) != 2 || a.ownedSchemaIDs[0] != "from-id" || a.ownedSchemaIDs[1] != "to-id" {
			t.Errorf("action %q ownedSchemaIDs = %v, want [from-id to-id]", a.name, a.ownedSchemaIDs)
		}
	}
	if _, ok := byName["Person"]; !ok {
		t.Error("expected a Person restore action")
	}
	if _, ok := byName["Belief"]; !ok {
		t.Error("expected a Belief restore action")
	}

	// The restored Person definition must be the FROM definition (no "age").
	person := string(byName["Person"].incomingSchema)
	if !strings.Contains(person, `"name"`) {
		t.Errorf("Person restore schema missing from property: %s", person)
	}
	if strings.Contains(person, `"age"`) {
		t.Errorf("Person restore schema must not contain to-only property: %s", person)
	}

	if len(toOnly) != 1 || toOnly[0] != "Organization" {
		t.Fatalf("toOnly = %v, want [Organization]", toOnly)
	}
}

func TestBuildRestoreTypeRegistryPlanNilPacks(t *testing.T) {
	if actions, toOnly := buildRestoreTypeRegistryPlan(nil, &GraphMemorySchema{ID: "to"}); actions != nil || toOnly != nil {
		t.Fatalf("expected nil plan for nil from-pack, got %v / %v", actions, toOnly)
	}
	if actions, toOnly := buildRestoreTypeRegistryPlan(&GraphMemorySchema{ID: "from"}, nil); actions != nil || toOnly != nil {
		t.Fatalf("expected nil plan for nil to-pack, got %v / %v", actions, toOnly)
	}
}
