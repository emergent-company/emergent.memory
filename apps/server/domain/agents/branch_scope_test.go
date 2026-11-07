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

// TestWithBranchScopeDerivation covers the three ways a run's branch boundary is
// decided: a definition that declares a scope, a definition that declares nothing
// (main only, and it CLEARS an inherited caller scope), and an internal loopback
// whose boundary the caller already fixed (#265).
func TestWithBranchScopeDerivation(t *testing.T) {
	ae := &AgentExecutor{}
	parent, err := branchscope.New(branchscope.Config{Unmerged: true, Origins: []string{"extraction", "agent"}})
	if err != nil {
		t.Fatalf("parent scope: %v", err)
	}
	parentCtx := mcp.ContextWithBranchScope(context.Background(), parent)

	// A definition that declares its own scope narrows the caller's.
	declaring := &AgentDefinition{ID: "ad-1"}
	if err := declaring.SetBranchScope(&branchscope.Config{Unmerged: true, Origins: []string{"extraction"}}); err != nil {
		t.Fatalf("SetBranchScope() error = %v", err)
	}
	got := mcp.BranchScopeFromContext(ae.withBranchScope(parentCtx, declaring))
	if got.IsMainOnly() || !got.AllowsOrigin("extraction") || got.AllowsOrigin("agent") {
		t.Fatalf("declared scope = %+v, want extraction only", got.Origins())
	}

	// A definition that declares nothing is main only — and that must clear the
	// caller's scope, or a delegated child would inherit reads it was never given.
	silent := &AgentDefinition{ID: "ad-2"}
	got = mcp.BranchScopeFromContext(ae.withBranchScope(parentCtx, silent))
	if !got.IsMainOnly() {
		t.Fatalf("undeclared scope = %+v, want main only", got)
	}

	// An invalid declaration fails closed to main only, not to the inherited
	// boundary.
	broken := &AgentDefinition{ID: "ad-3", Config: map[string]any{
		"branchScope": map[string]any{"unmerged": true, "origins": []any{"not-an-origin"}},
	}}
	got = mcp.BranchScopeFromContext(ae.withBranchScope(parentCtx, broken))
	if !got.IsMainOnly() {
		t.Fatalf("invalid scope = %+v, want main only", got)
	}

	// A caller-fixed boundary survives an internal definition that declares
	// nothing (the /query hand-off behind search-knowledge) ...
	fixedCtx := mcp.ContextWithFixedBranchScope(context.Background(), parent)
	kept := ae.withBranchScope(fixedCtx, silent)
	got = mcp.BranchScopeFromContext(kept)
	if got.IsMainOnly() || !got.AllowsOrigin("agent") {
		t.Fatalf("fixed boundary = %+v, want the caller's scope preserved", got)
	}
	// ... and the exception applies to exactly this run: the marker is cleared, so
	// a child THIS run delegates to follows the ordinary rules.
	if mcp.IsFixedBranchScope(kept) {
		t.Fatal("the fixed marker must not survive derivation into a delegated child")
	}
	if child := mcp.BranchScopeFromContext(ae.withBranchScope(kept, silent)); !child.IsMainOnly() {
		t.Fatalf("a delegated child of the loopback run = %+v, want main only", child)
	}

	// A hand-off restating an ABSENT boundary is left exactly as it was: its
	// originating caller was a human MCP client that could name branches before
	// branch scopes existed.
	absentCtx := mcp.ContextWithAbsentBranchScope(context.Background())
	absent := ae.withBranchScope(absentCtx, silent)
	if mcp.IsBranchScopeInstalled(absent) || mcp.IsAbsentBranchScope(absent) != true {
		t.Fatalf("an absent hand-off must stay absent, got installed=%v", mcp.IsBranchScopeInstalled(absent))
	}
	// ... unless THIS definition declares a scope of its own, which is narrower.
	narrowed := mcp.BranchScopeFromContext(ae.withBranchScope(absentCtx, declaring))
	if narrowed.IsMainOnly() || !narrowed.AllowsOrigin("extraction") || narrowed.AllowsOrigin("agent") {
		t.Fatalf("absent + declared = %+v, want the declared scope", narrowed.Origins())
	}

	// ... and a defining internal run still narrows it.
	got = mcp.BranchScopeFromContext(ae.withBranchScope(fixedCtx, declaring))
	if got.IsMainOnly() || !got.AllowsOrigin("extraction") || got.AllowsOrigin("agent") {
		t.Fatalf("fixed + declared = %+v, want extraction only", got.Origins())
	}
}
