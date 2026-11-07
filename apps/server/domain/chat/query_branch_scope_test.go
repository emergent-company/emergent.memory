package chat

import (
	"context"
	"testing"

	"github.com/emergent-company/emergent.memory/domain/mcp"
	"github.com/emergent-company/emergent.memory/pkg/branchscope"
)

// TestWithQueryBranchScope covers the /query hand-off used by the
// search-knowledge MCP tool: a caller-supplied branch boundary must be installed
// as the internal run's FIXED boundary, an absent one leaves the context alone,
// and an invalid one is an error rather than a silently unscoped run (#265).
func TestWithQueryBranchScope(t *testing.T) {
	ctx := context.Background()

	// Absent: unchanged, and the internal run keeps its own behaviour.
	got, err := withQueryBranchScope(ctx, QueryStreamRequest{})
	if err != nil {
		t.Fatalf("absent branch scope: %v", err)
	}
	if mcp.BranchScopeFromContext(got) != nil {
		t.Fatal("absent branch scope must leave the context unscoped")
	}

	// Present: installed and marked fixed, so the executor preserves it instead of
	// resetting the hand-off to main only.
	got, err = withQueryBranchScope(ctx, QueryStreamRequest{BranchScope: &branchscope.Config{
		Unmerged: true,
		Origins:  []string{"extraction"},
	}})
	if err != nil {
		t.Fatalf("valid branch scope: %v", err)
	}
	scope := mcp.BranchScopeFromContext(got)
	if scope.IsMainOnly() || !scope.AllowsOrigin("extraction") {
		t.Fatalf("installed scope = %+v, want extraction", scope)
	}
	if !mcp.IsFixedBranchScope(got) {
		t.Fatal("a loopback scope must be marked fixed")
	}

	// Invalid: an error, never an unscoped run.
	if _, err := withQueryBranchScope(ctx, QueryStreamRequest{BranchScope: &branchscope.Config{
		Unmerged: true,
		Origins:  []string{"nope"},
	}}); err == nil {
		t.Fatal("an invalid branch scope must be an error")
	}

	// main_only: the internal run must refuse to name a branch.
	got, err = withQueryBranchScope(ctx, QueryStreamRequest{BranchScopeMode: "main_only"})
	if err != nil {
		t.Fatalf("main_only mode: %v", err)
	}
	if !mcp.IsBranchScopeInstalled(got) || !mcp.BranchScopeFromContext(got).IsMainOnly() {
		t.Fatal("main_only must install an enforced main-only boundary")
	}

	// absent: the internal run keeps no boundary at all, so a human MCP client's
	// branch argument still works.
	got, err = withQueryBranchScope(ctx, QueryStreamRequest{BranchScopeMode: "absent"})
	if err != nil {
		t.Fatalf("absent mode: %v", err)
	}
	if mcp.IsBranchScopeInstalled(got) || !mcp.IsAbsentBranchScope(got) {
		t.Fatal("absent must not install a boundary")
	}

	// A mode that claims a scope without one, or an unknown mode, is an error.
	if _, err := withQueryBranchScope(ctx, QueryStreamRequest{BranchScopeMode: "scoped"}); err == nil {
		t.Fatal("scoped mode without a scope must be an error")
	}
	if _, err := withQueryBranchScope(ctx, QueryStreamRequest{BranchScopeMode: "everything"}); err == nil {
		t.Fatal("an unknown mode must be an error")
	}
}
