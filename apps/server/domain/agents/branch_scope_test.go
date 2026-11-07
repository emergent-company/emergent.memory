package agents

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/emergent-company/emergent.memory/domain/mcp"
	"github.com/emergent-company/emergent.memory/pkg/branchscope"
)

// TestBranchScopeAbsentIsMainOnly covers the backwards-compatible default: a
// definition that does not configure a scope grants no branch reads (#265).
func TestBranchScopeAbsentIsMainOnly(t *testing.T) {
	for _, def := range []*AgentDefinition{
		nil,
		{},
		{Config: map[string]any{}},
		{Config: map[string]any{branchScopeConfigKey: nil}},
	} {
		scope, err := def.BranchScope()
		if err != nil {
			t.Fatalf("BranchScope() error = %v, want nil", err)
		}
		if scope != nil {
			t.Fatalf("BranchScope() = %+v, want nil (main only)", scope)
		}
	}
}

// TestSetBranchScopeRoundTrip proves the stored shape survives the JSONB round
// trip every read performs: a Config value is marshalled into the map by bun and
// read back as map[string]any.
func TestSetBranchScopeRoundTrip(t *testing.T) {
	def := &AgentDefinition{}
	if err := def.SetBranchScope(&branchscope.Config{
		Unmerged:    true,
		Origins:     []string{"extraction"},
		Branches:    []string{"extraction/*"},
		MaxBranches: 2,
	}); err != nil {
		t.Fatalf("SetBranchScope() error = %v", err)
	}

	// Simulate the JSONB round trip.
	raw, err := json.Marshal(def.Config)
	if err != nil {
		t.Fatalf("marshal config: %v", err)
	}
	var stored map[string]any
	if err := json.Unmarshal(raw, &stored); err != nil {
		t.Fatalf("unmarshal config: %v", err)
	}
	def.Config = stored

	scope, err := def.BranchScope()
	if err != nil {
		t.Fatalf("BranchScope() error = %v", err)
	}
	if scope == nil {
		t.Fatal("BranchScope() = nil, want a scope")
	}
	if got := scope.Origins(); len(got) != 1 || got[0] != "extraction" {
		t.Fatalf("Origins() = %v, want [extraction]", got)
	}
	if got := scope.BranchPatterns(); len(got) != 1 || got[0] != "extraction/*" {
		t.Fatalf("BranchPatterns() = %v, want [extraction/*]", got)
	}
	if got := scope.MaxBranches(); got != 2 {
		t.Fatalf("MaxBranches() = %d, want 2", got)
	}
}

// TestSetBranchScopeRejectsInvalidConfig proves a typo is a write error, not a
// silently narrower scope.
func TestSetBranchScopeRejectsInvalidConfig(t *testing.T) {
	def := &AgentDefinition{}
	err := def.SetBranchScope(&branchscope.Config{Unmerged: true, Origins: []string{"documents"}})
	if err == nil {
		t.Fatal("SetBranchScope() = nil error, want a validation error")
	}
	if def.Config != nil {
		t.Fatal("a rejected configuration must not be stored")
	}
}

// TestSetBranchScopeDisabledRemovesKey proves `unmerged: false` (or nil) clears
// the key so a default definition carries no redundant entry.
func TestSetBranchScopeDisabledRemovesKey(t *testing.T) {
	def := &AgentDefinition{Config: map[string]any{branchScopeConfigKey: branchscope.Config{Unmerged: true}}}
	if err := def.SetBranchScope(&branchscope.Config{Unmerged: false}); err != nil {
		t.Fatalf("SetBranchScope() error = %v", err)
	}
	if _, ok := def.Config[branchScopeConfigKey]; ok {
		t.Fatal("disabled scope must delete the config key")
	}
}

// TestBranchScopeInvalidStoredValueFailsClosed proves a value written through the
// raw Config map (bypassing SetBranchScope) is an error, which the executor turns
// into main-only rather than a widened read.
func TestBranchScopeInvalidStoredValueFailsClosed(t *testing.T) {
	def := &AgentDefinition{Config: map[string]any{
		branchScopeConfigKey: map[string]any{"unmerged": "yes please"},
	}}
	if _, err := def.BranchScope(); err == nil {
		t.Fatal("BranchScope() = nil error, want a decode error")
	}
	if scope, err := resolveBranchScope(def); err == nil || scope != nil {
		t.Fatalf("resolveBranchScope() = (%v, %v), want (nil, error)", scope, err)
	}
}

// TestBranchScopeNarrowedByInherited proves the executor's derivation cannot
// widen a delegating caller's scope: the child's effective scope is the
// intersection, and a main-only parent stays main only (#265).
func TestBranchScopeNarrowedByInherited(t *testing.T) {
	parent, err := mcp.NewBranchScope(branchscope.Config{Unmerged: true, Origins: []string{"extraction"}})
	if err != nil {
		t.Fatalf("parent scope: %v", err)
	}
	child, err := mcp.NewBranchScope(branchscope.Config{
		Unmerged: true,
		Origins:  []string{"extraction", "agent"},
	})
	if err != nil {
		t.Fatalf("child scope: %v", err)
	}

	ctx := mcp.ContextWithBranchScope(context.Background(), parent)
	narrowed := mcp.NarrowBranchScope(mcp.BranchScopeFromContext(ctx), child)
	if narrowed == nil || narrowed.IsMainOnly() {
		t.Fatal("narrowing two branch scopes must still read branches")
	}
	if !narrowed.AllowsOrigin("extraction") || narrowed.AllowsOrigin("agent") {
		t.Fatalf("narrowed origins = %v, want only extraction", narrowed.Origins())
	}

	// A main-only caller (no scope in context) cannot gain branch reads by
	// delegating to a definition that declares them.
	mainOnly := mcp.NarrowBranchScope(mcp.BranchScopeFromContext(context.Background()), child)
	if mainOnly != nil {
		t.Fatalf("main-only caller narrowed to %+v, want nil", mainOnly)
	}
}

// TestToDTOCarriesBranchScope proves the API response exposes the configured
// scope, and omits it when there is none.
func TestToDTOCarriesBranchScope(t *testing.T) {
	def := &AgentDefinition{ID: "ad-1"}
	if dto := def.ToDTO(); dto.BranchScope != nil {
		t.Fatalf("BranchScope = %+v, want nil for a definition with no scope", dto.BranchScope)
	}

	if err := def.SetBranchScope(&branchscope.Config{Unmerged: true, Origins: []string{"extraction"}}); err != nil {
		t.Fatalf("SetBranchScope() error = %v", err)
	}
	dto := def.ToDTO()
	if dto.BranchScope == nil || !dto.BranchScope.Unmerged {
		t.Fatalf("BranchScope = %+v, want the configured scope", dto.BranchScope)
	}
	if len(dto.BranchScope.Origins) != 1 || dto.BranchScope.Origins[0] != "extraction" {
		t.Fatalf("BranchScope.Origins = %v, want [extraction]", dto.BranchScope.Origins)
	}
}
