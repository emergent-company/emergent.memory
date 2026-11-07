package mcp

import (
	"context"
	"fmt"

	"github.com/google/uuid"

	"github.com/emergent-company/emergent.memory/pkg/branchscope"
)

// BranchScope is the per-agent-run read boundary for unmerged graph branches
// (#265). It is the MCP-facing name for branchscope.Scope so a tool layer reads
// the exact value the executor installed, while domain/search — which this
// package imports — can depend on the leaf package and read it without an
// import cycle.
//
// Semantics are those of pkg/branchscope: a nil scope means main only (the
// default), the scope is read-only, and no tool argument can widen it.
type BranchScope = branchscope.Scope

// NewBranchScope validates a definition's branchScope configuration. It returns
// (nil, nil) when unmerged reads are not enabled, which is the main-only default.
func NewBranchScope(cfg branchscope.Config) (*BranchScope, error) {
	return branchscope.New(cfg)
}

// ContextWithBranchScope installs the per-run scope. A main-only (nil) scope
// still installs, clearing any inherited scope: a delegated run must not read
// its caller's branches just because it declared nothing of its own.
func ContextWithBranchScope(ctx context.Context, scope *BranchScope) context.Context {
	return branchscope.ContextWith(ctx, scope)
}

// BranchScopeFromContext returns the scope installed by ContextWithBranchScope,
// or nil (main only) when no scope was set — HTTP MCP transports and tests.
func BranchScopeFromContext(ctx context.Context) *BranchScope {
	return branchscope.FromContext(ctx)
}

// NarrowBranchScope combines an inherited (caller) scope with a declared
// (definition) scope. It is used where a run already carries a scope in context —
// a delegated sub-agent's caller scope — and then derives its own from the
// definition: the two must be combined WITHOUT letting the definition widen the
// inherited boundary (#265).
func NarrowBranchScope(inherited, declared *BranchScope) *BranchScope {
	return branchscope.Narrow(inherited, declared)
}

// branchAllowedByScope checks a model-supplied branch id against the run's branch
// scope. It is the id-level check; branch references (names or ids) funnel
// through Service.branchRefAllowedByScope, which is what every tool that takes a
// `branch` argument uses. A nil scope is not enforced here: nil is what a human
// MCP client carries, and a branch argument is existing, legitimate behaviour for
// them. The boundary this enforces is a scope-bearing agent's own declaration —
// an agent that may read some unmerged branches must not reach the rest by naming
// one, on a read or a write (#265).
func (s *Service) branchAllowedByScope(ctx context.Context, projectID, branchID uuid.UUID) error {
	scope := BranchScopeFromContext(ctx)
	if scope.IsMainOnly() {
		return nil
	}
	var branch struct {
		Name   string `bun:"name"`
		Origin string `bun:"origin"`
	}
	err := s.db.NewSelect().
		TableExpr("kb.branches").
		Column("name").
		Column("origin").
		Where("id = ? AND project_id = ?", branchID, projectID).
		Limit(1).
		Scan(ctx, &branch)
	if err != nil {
		return fmt.Errorf("branch %s is not readable: %w", branchID, err)
	}
	if !scope.AllowsOrigin(branch.Origin) || !scope.AllowsName(branch.Name) {
		return fmt.Errorf("branch %q is outside this agent's branch scope", branch.Name)
	}
	return nil
}
