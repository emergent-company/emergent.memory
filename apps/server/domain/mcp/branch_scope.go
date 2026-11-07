package mcp

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	"github.com/uptrace/bun"

	"github.com/emergent-company/emergent.memory/pkg/branchscope"
	"github.com/emergent-company/emergent.memory/pkg/branchsql"
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

// ContextWithFixedBranchScope installs a boundary the caller already decided, for
// a server-to-server hand-off that runs on the originating run's behalf (the
// /query loopback behind search-knowledge). Never call it from a model-visible
// surface.
func ContextWithFixedBranchScope(ctx context.Context, scope *BranchScope) context.Context {
	return branchscope.ContextWithFixed(ctx, scope)
}

// IsFixedBranchScope reports whether the installed scope is a caller-fixed
// boundary, which run-level derivation must preserve rather than replace.
func IsFixedBranchScope(ctx context.Context) bool {
	return branchscope.IsFixed(ctx)
}

// ContextWithAbsentBranchScope marks a context as having no branch boundary at
// all, for a hand-off whose originating caller had none (a human MCP client). Run
// derivation leaves it alone instead of installing main-only, so that caller keeps
// exactly the behaviour it had before branch scopes existed. Never install it from
// a model-visible surface.
func ContextWithAbsentBranchScope(ctx context.Context) context.Context {
	return branchscope.ContextWithAbsent(ctx)
}

// IsAbsentBranchScope reports whether the context was marked boundary-absent.
func IsAbsentBranchScope(ctx context.Context) bool {
	return branchscope.IsAbsent(ctx)
}

// IsBranchScopeInstalled reports whether a boundary was installed at all,
// including the main-only one. Every agent run has one; a human MCP client has
// none. Reads enforce main-only for the former and keep the pre-existing
// behaviour for the latter (#265).
func IsBranchScopeInstalled(ctx context.Context) bool {
	return branchscope.Installed(ctx)
}

// NarrowBranchScope combines an inherited (caller) scope with a declared
// (definition) scope. It is used where a run already carries a scope in context —
// a delegated sub-agent's caller scope — and then derives its own from the
// definition: the two must be combined WITHOUT letting the definition widen the
// inherited boundary (#265).
func NarrowBranchScope(inherited, declared *BranchScope) *BranchScope {
	return branchscope.Narrow(inherited, declared)
}

// branchReadAllowedByScope is the READ boundary. A run with a boundary installed
// — every agent run, including the default main-only one — may read only the
// branches inside it, and a main-only run reads no branch at all. A context with
// no boundary is a human MCP client, whose branch arguments predate the scope and
// keep working.
//
// Writes and branch management go through branchRefAllowedByScope instead: an
// agent could already create a branch and write to it, and the read scope must
// not take that away. Merging a branch can still surface its content in main, which
// is a write-permission question, not a read-scope one.
func (s *Service) branchReadAllowedByScope(ctx context.Context, projectID string, branchID uuid.UUID) error {
	if !IsBranchScopeInstalled(ctx) {
		return nil
	}
	if scope := BranchScopeFromContext(ctx); scope.IsMainOnly() {
		return fmt.Errorf("branch %s is not readable: this run reads the main graph only (declare a branchScope on the agent definition to read unmerged branches)", branchID)
	}
	projectUUID, err := uuid.Parse(projectID)
	if err != nil {
		return fmt.Errorf("invalid project_id: %w", err)
	}
	if err := s.branchAllowedByScope(ctx, projectUUID, branchID); err != nil {
		return err
	}
	return s.branchSourceReadable(ctx, projectUUID, branchID)
}

// branchSourceReadable is the source half of the read boundary: an extraction
// branch whose source document was deleted is not readable, whatever the scope
// says, because its staged objects must not appear in any scoped read (#265,
// #246). The search fan-out applies the same predicate, so naming the branch
// cannot reach what a scoped search would not return.
func (s *Service) branchSourceReadable(ctx context.Context, projectID, branchID uuid.UUID) error {
	var row struct {
		Readable bool `bun:"readable"`
	}
	err := s.db.NewSelect().
		TableExpr("kb.branches AS b").
		ColumnExpr("("+branchsql.DeletedExtractionSourceExclusion+") AS readable").
		Where("b.id = ? AND b.project_id = ?", branchID, projectID).
		Limit(1).
		Scan(ctx, &row)
	if err != nil {
		return fmt.Errorf("branch %s is not readable: %w", branchID, err)
	}
	if !row.Readable {
		return fmt.Errorf("branch %s is not readable: its extraction source document was deleted", branchID)
	}
	return nil
}

// branchAllowedByScope is the id-level check: the branch must be inside a
// scope-bearing run's own boundary. It is reached through
// branchReadAllowedByScope (reads) and branchRefAllowedByScope (everything that
// resolves a `branch` argument), so a scoped run cannot reach a branch outside its
// declaration by name or by id. A main-only scope is a no-op here by design — the
// caller decides whether main-only means "refuse" (reads) or "unchanged"
// (management) — and a context with no boundary never gets here.
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

// inScopeBranchIDs returns the IDs of the unmerged branches a non-main-only
// scope admits, ordered by recency and capped by the scope's MaxBranches. It is
// the raw-SQL counterpart of the graph repository's inScopeBranchIDs, used by the
// MCP read paths that issue their own queries (#308).
func (s *Service) inScopeBranchIDs(ctx context.Context, projectID uuid.UUID, scope *BranchScope) ([]uuid.UUID, error) {
	var branches []struct {
		ID     uuid.UUID `bun:"id"`
		Name   string    `bun:"name"`
		Origin string    `bun:"origin"`
	}
	err := s.db.NewSelect().
		TableExpr("kb.branches AS b").
		Column("b.id").
		Column("b.name").
		Column("b.origin").
		Where("b.project_id = ?", projectID).
		Where("b.merged_at IS NULL").
		Where(branchsql.DeletedExtractionSourceExclusion).
		OrderExpr("b.created_at DESC").
		Scan(ctx, &branches)
	if err != nil {
		return nil, fmt.Errorf("list in-scope branches: %w", err)
	}
	ids := make([]uuid.UUID, 0, len(branches))
	for _, b := range branches {
		if !scope.AllowsOrigin(b.Origin) || !scope.AllowsName(b.Name) {
			continue
		}
		ids = append(ids, b.ID)
		if len(ids) >= scope.MaxBranches() {
			break
		}
	}
	return ids, nil
}

// branchScopeClause returns the SQL fragment and args bounding an object read to
// the run's branch scope, for the raw-SQL read paths in this package (#308). It
// is the graph_objects-specific form (alias `go`) of branchScopeDedupClause.
func (s *Service) branchScopeClause(ctx context.Context, projectID string, branchID *uuid.UUID) (string, []any, error) {
	return s.branchScopeDedupClause(ctx, projectID, branchID, "go", "kb.graph_objects")
}

// branchScopeDedupClause returns the SQL fragment and args bounding a read of a
// table that carries both branch_id and canonical_id to the run's branch scope,
// deduplicated main-first. `alias` is that table's SQL alias (go, src, dst, gr)
// and `table` its qualified name (kb.graph_objects, kb.graph_relationships);
// both are internal literals, never caller input.
//
// An explicit branch is honoured as-is (already validated in scope before this
// point). A caller with no boundary keeps the pre-existing main-only default; a
// main-only agent run reads main only; a scoped run reads main plus its in-scope
// unmerged branches, deduplicated main-first so a forked canonical row is
// returned once as its main head and a branch-only row is still reachable.
func (s *Service) branchScopeDedupClause(ctx context.Context, projectID string, branchID *uuid.UUID, alias, table string) (string, []any, error) {
	if branchID != nil {
		return fmt.Sprintf(" AND %s.branch_id = ?", alias), []any{*branchID}, nil
	}
	if !IsBranchScopeInstalled(ctx) {
		return fmt.Sprintf(" AND %s.branch_id IS NULL", alias), nil, nil
	}
	scope := BranchScopeFromContext(ctx)
	if scope.IsMainOnly() {
		return fmt.Sprintf(" AND %s.branch_id IS NULL", alias), nil, nil
	}
	projectUUID, err := uuid.Parse(projectID)
	if err != nil {
		return "", nil, fmt.Errorf("invalid project_id: %w", err)
	}
	ids, err := s.inScopeBranchIDs(ctx, projectUUID, scope)
	if err != nil {
		return "", nil, err
	}
	if len(ids) == 0 {
		return fmt.Sprintf(" AND %s.branch_id IS NULL", alias), nil, nil
	}
	// Main-first dedup: keep the main head when it exists, else the branch head.
	return fmt.Sprintf(" AND (%s.branch_id IS NULL OR (%s.branch_id IN (?) AND %s.canonical_id NOT IN ("+
			"SELECT canonical_id FROM %s WHERE project_id = ? "+
			"AND branch_id IS NULL AND supersedes_id IS NULL AND deleted_at IS NULL)))",
			alias, alias, alias, table),
		[]any{bun.In(ids), projectUUID}, nil
}
