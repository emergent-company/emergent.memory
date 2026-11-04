package agents

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"github.com/emergent-company/emergent.memory/domain/mcp"
)

// TestGraphTypeAllowlistConfig covers the AgentDefinition.Config reader/mutator
// pair that backs the per-agent graph type allowlist (#1695).
func TestGraphTypeAllowlistConfig(t *testing.T) {
	t.Run("nil definition and nil config read as unset", func(t *testing.T) {
		if got := (*AgentDefinition)(nil).ObjectTypes(); got != nil {
			t.Fatalf("nil definition ObjectTypes = %#v, want nil", got)
		}
		if got := (*AgentDefinition)(nil).RelationshipTypes(); got != nil {
			t.Fatalf("nil definition RelationshipTypes = %#v, want nil", got)
		}
		if got := (&AgentDefinition{}).ObjectTypes(); got != nil {
			t.Fatalf("nil config ObjectTypes = %#v, want nil", got)
		}
	})

	t.Run("empty list reads as unset", func(t *testing.T) {
		def := &AgentDefinition{Config: map[string]any{objectTypesConfigKey: []string{}}}
		if got := def.ObjectTypes(); got != nil {
			t.Fatalf("empty list ObjectTypes = %#v, want nil", got)
		}
		if got := def.RelationshipTypes(); got != nil {
			t.Fatalf("absent RelationshipTypes = %#v, want nil", got)
		}
	})

	t.Run("nil-safe reader tolerates []string and JSONB []any", func(t *testing.T) {
		asStrings := &AgentDefinition{Config: map[string]any{
			objectTypesConfigKey:       []string{"Law", "Regulation"},
			relationshipTypesConfigKey: []string{"CITES"},
		}}
		if got := asStrings.ObjectTypes(); !reflect.DeepEqual(got, []string{"Law", "Regulation"}) {
			t.Fatalf("[]string reader = %#v", got)
		}
		if got := asStrings.RelationshipTypes(); !reflect.DeepEqual(got, []string{"CITES"}) {
			t.Fatalf("[]string relationship reader = %#v", got)
		}

		// JSONB round-trips through map[string]any as []any.
		asAny := &AgentDefinition{Config: map[string]any{
			objectTypesConfigKey:       []any{"Law", "Regulation"},
			relationshipTypesConfigKey: []any{"CITES", "AMENDS"},
		}}
		if got := asAny.ObjectTypes(); !reflect.DeepEqual(got, []string{"Law", "Regulation"}) {
			t.Fatalf("[]any reader = %#v", got)
		}
		if got := asAny.RelationshipTypes(); !reflect.DeepEqual(got, []string{"CITES", "AMENDS"}) {
			t.Fatalf("[]any relationship reader = %#v", got)
		}
	})

	t.Run("wrong-shaped value reads as unset", func(t *testing.T) {
		def := &AgentDefinition{Config: map[string]any{objectTypesConfigKey: "Law"}}
		if got := def.ObjectTypes(); got != nil {
			t.Fatalf("non-list ObjectTypes = %#v, want nil", got)
		}
	})

	t.Run("mutator round-trips and preserves other keys", func(t *testing.T) {
		def := &AgentDefinition{Config: map[string]any{"language": "Spanish"}}
		def.SetObjectTypes([]string{"Law", "LegalParagraph"})
		def.SetRelationshipTypes([]string{"CITES"})
		if got := def.ObjectTypes(); !reflect.DeepEqual(got, []string{"Law", "LegalParagraph"}) {
			t.Fatalf("round-trip ObjectTypes = %#v", got)
		}
		if got := def.RelationshipTypes(); !reflect.DeepEqual(got, []string{"CITES"}) {
			t.Fatalf("round-trip RelationshipTypes = %#v", got)
		}
		if def.Config["language"] != "Spanish" {
			t.Fatalf("mutator disturbed unrelated key: %#v", def.Config)
		}
	})

	t.Run("empty mutator input removes the key", func(t *testing.T) {
		def := &AgentDefinition{Config: map[string]any{
			objectTypesConfigKey:       []string{"Law"},
			relationshipTypesConfigKey: []string{"CITES"},
			"language":                 "Spanish",
		}}
		def.SetObjectTypes(nil)
		def.SetRelationshipTypes([]string{})
		for _, key := range []string{objectTypesConfigKey, relationshipTypesConfigKey} {
			if _, ok := def.Config[key]; ok {
				t.Fatalf("empty set must delete %q, got %#v", key, def.Config)
			}
		}
		if def.Config["language"] != "Spanish" {
			t.Fatalf("clearing allowlists disturbed unrelated key: %#v", def.Config)
		}
	})

	t.Run("mutator initialises a nil config", func(t *testing.T) {
		def := &AgentDefinition{}
		def.SetObjectTypes([]string{"Law"})
		if got := def.ObjectTypes(); !reflect.DeepEqual(got, []string{"Law"}) {
			t.Fatalf("set on nil config = %#v", got)
		}
	})
}

// TestGraphTypeAllowlistDTORoundTrip locks the read/write contract for the
// allowlists (#1695).
func TestGraphTypeAllowlistDTORoundTrip(t *testing.T) {
	t.Run("read DTO exposes the allowlists", func(t *testing.T) {
		def := &AgentDefinition{ID: "ad-1", Config: map[string]any{
			objectTypesConfigKey:       []string{"Law", "Regulation"},
			relationshipTypesConfigKey: []string{"CITES"},
		}}
		dto := def.ToDTO()
		if !reflect.DeepEqual(dto.ObjectTypes, []string{"Law", "Regulation"}) {
			t.Fatalf("DTO ObjectTypes = %#v", dto.ObjectTypes)
		}
		if !reflect.DeepEqual(dto.RelationshipTypes, []string{"CITES"}) {
			t.Fatalf("DTO RelationshipTypes = %#v", dto.RelationshipTypes)
		}
		b, err := json.Marshal(dto)
		if err != nil {
			t.Fatalf("marshal: %v", err)
		}
		if !strings.Contains(string(b), `"objectTypes":["Law","Regulation"]`) {
			t.Fatalf("read DTO JSON missing objectTypes: %s", b)
		}
		if !strings.Contains(string(b), `"relationshipTypes":["CITES"]`) {
			t.Fatalf("read DTO JSON missing relationshipTypes: %s", b)
		}
		// A default definition omits both.
		bare, err := json.Marshal((&AgentDefinition{ID: "ad-2"}).ToDTO())
		if err != nil {
			t.Fatalf("marshal bare: %v", err)
		}
		if strings.Contains(string(bare), "objectTypes") || strings.Contains(string(bare), "relationshipTypes") {
			t.Fatalf("default DTO should omit allowlists: %s", bare)
		}
	})

	t.Run("create DTO accepts the allowlists", func(t *testing.T) {
		var dto CreateAgentDefinitionDTO
		if err := json.Unmarshal([]byte(`{"name":"a","objectTypes":["Law"],"relationshipTypes":["CITES"]}`), &dto); err != nil {
			t.Fatalf("unmarshal create: %v", err)
		}
		if !reflect.DeepEqual(dto.ObjectTypes, []string{"Law"}) {
			t.Fatalf("create ObjectTypes = %#v", dto.ObjectTypes)
		}
		if !reflect.DeepEqual(dto.RelationshipTypes, []string{"CITES"}) {
			t.Fatalf("create RelationshipTypes = %#v", dto.RelationshipTypes)
		}
		var bare CreateAgentDefinitionDTO
		if err := json.Unmarshal([]byte(`{"name":"a"}`), &bare); err != nil {
			t.Fatalf("unmarshal bare create: %v", err)
		}
		if bare.ObjectTypes != nil || bare.RelationshipTypes != nil {
			t.Fatalf("absent create allowlists = %#v/%#v, want nil", bare.ObjectTypes, bare.RelationshipTypes)
		}
	})

	t.Run("update DTO distinguishes omitted from explicit-empty", func(t *testing.T) {
		var omitted UpdateAgentDefinitionDTO
		if err := json.Unmarshal([]byte(`{"objectTypes":["Law"]}`), &omitted); err != nil {
			t.Fatalf("unmarshal update: %v", err)
		}
		if !reflect.DeepEqual(omitted.ObjectTypes, []string{"Law"}) {
			t.Fatalf("update ObjectTypes = %#v", omitted.ObjectTypes)
		}
		if omitted.RelationshipTypes != nil {
			t.Fatalf("omitted update RelationshipTypes = %#v, want nil (leave unchanged)", omitted.RelationshipTypes)
		}
		var cleared UpdateAgentDefinitionDTO
		if err := json.Unmarshal([]byte(`{"objectTypes":[]}`), &cleared); err != nil {
			t.Fatalf("unmarshal cleared update: %v", err)
		}
		if cleared.ObjectTypes == nil || len(cleared.ObjectTypes) != 0 {
			t.Fatalf("explicit-empty update ObjectTypes = %#v, want non-nil empty", cleared.ObjectTypes)
		}
	})
}

// TestResolveTypeScope pins the single derivation seam and its cross-surface
// parity: the scope is computed once per run from the definition and is
// independent of which surface invoked the run (#1695).
func TestResolveTypeScope(t *testing.T) {
	t.Run("default definition is unrestricted", func(t *testing.T) {
		if got := resolveTypeScope(nil); got != nil {
			t.Fatalf("nil definition scope = %#v, want nil (all types)", got)
		}
		if got := resolveTypeScope(&AgentDefinition{}); got != nil {
			t.Fatalf("empty definition scope = %#v, want nil (all types)", got)
		}
	})

	t.Run("object-only allowlist leaves relationships unrestricted", func(t *testing.T) {
		def := &AgentDefinition{Config: map[string]any{objectTypesConfigKey: []string{"Law"}}}
		scope := resolveTypeScope(def)
		if scope == nil || !scope.AllowsObject("Law") || scope.AllowsObject("Person") {
			t.Fatalf("object scope wrong: %#v", scope)
		}
		if !scope.AllowsRelationship("ANY") {
			t.Fatal("object-only allowlist must leave relationship types unrestricted")
		}
	})

	t.Run("relationship-only allowlist leaves objects unrestricted", func(t *testing.T) {
		def := &AgentDefinition{Config: map[string]any{relationshipTypesConfigKey: []string{"CITES"}}}
		scope := resolveTypeScope(def)
		if scope == nil || !scope.AllowsRelationship("CITES") || scope.AllowsRelationship("AMENDS") {
			t.Fatalf("relationship scope wrong: %#v", scope)
		}
		if !scope.AllowsObject("ANY") {
			t.Fatal("relationship-only allowlist must leave object types unrestricted")
		}
	})

	t.Run("same definition yields the same scope on every surface", func(t *testing.T) {
		def := &AgentDefinition{Config: map[string]any{
			objectTypesConfigKey:       []string{"Law", "Regulation"},
			relationshipTypesConfigKey: []string{"CITES"},
		}}
		want := resolveTypeScope(def)
		for _, surface := range []string{"trigger", "a2a", "chat", "chat-completions"} {
			got := resolveTypeScope(def)
			if got == nil {
				t.Fatalf("%s: scope is nil", surface)
			}
			if !reflect.DeepEqual(got.ObjectTypes(), want.ObjectTypes()) ||
				!reflect.DeepEqual(got.RelationshipTypes(), want.RelationshipTypes()) {
				t.Fatalf("%s: scope %#v != %#v", surface, got, want)
			}
			if !got.AllowsObject("Law") || got.AllowsObject("Person") {
				t.Fatalf("%s: object enforcement wrong", surface)
			}
			if !got.AllowsRelationship("CITES") || got.AllowsRelationship("AMENDS") {
				t.Fatalf("%s: relationship enforcement wrong", surface)
			}
		}
	})

	t.Run("scope threads through context", func(t *testing.T) {
		def := &AgentDefinition{Config: map[string]any{objectTypesConfigKey: []string{"Law"}}}
		ctx := mcp.ContextWithTypeScope(t.Context(), resolveTypeScope(def))
		got := mcp.TypeScopeFromContext(ctx)
		if got == nil || !got.AllowsObject("Law") || got.AllowsObject("Person") {
			t.Fatalf("context scope = %#v", got)
		}
	})
}
