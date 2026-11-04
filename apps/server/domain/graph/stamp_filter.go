package graph

// Write-stamp query filters (the read half of slice 1 in #2376; issue #2468).
//
// actor.go records a write stamp on every version row: which credential
// authenticated the write, which agent run produced it, and what class of data
// the row is. These filters turn that record into a query, so "what did this
// run write" and "what did this key write" are answerable without opening each
// row. They are structured equality filters, not a query language: an unknown
// enum value is a client error (validated at the HTTP boundary) rather than a
// filter that silently matches nothing.

import (
	"slices"
	"strings"

	"github.com/google/uuid"
	"github.com/uptrace/bun"

	"github.com/emergent-company/emergent.memory/pkg/auth"
)

// WriteStampFilters narrows a listing to rows written by a particular
// credential, run or data class. The zero value filters nothing.
type WriteStampFilters struct {
	// RunID matches run_id OR root_run_id, so a filter on a root run returns the
	// writes of its whole delegation subtree.
	RunID *uuid.UUID
	// CredentialID is an exact match on the recorded credential. It does not
	// require CredentialKind: the id already identifies exactly one credential,
	// and the column is deliberately not a FK, so a purged token's rows stay
	// findable by attribution.
	CredentialID *uuid.UUID
	// CredentialKind is one of api_key|mcp_link|session|none.
	CredentialKind *string
	// DataClass is one of source|synthesized|adopted. Relationships record no
	// data class, so the relationship endpoints reject it at the boundary.
	DataClass *string
}

// CredentialKinds are the accepted values of the credential_kind filter. They
// are the same constants pkg/auth stamps on the row, so the query contract
// cannot drift from the written values.
var CredentialKinds = []string{
	auth.CredentialAPIKey,
	auth.CredentialMCPLink,
	auth.CredentialSession,
	auth.CredentialNone,
}

// DataClasses are the accepted values of the data_class filter.
var DataClasses = []string{
	DataClassSource,
	DataClassAdopted,
	DataClassSynthesized,
}

// validCredentialKind reports whether k is a recorded credential kind.
func validCredentialKind(k string) bool { return slices.Contains(CredentialKinds, k) }

// validDataClass reports whether c is a recorded data class.
func validDataClass(c string) bool { return slices.Contains(DataClasses, c) }

// IsZero reports whether the filter narrows nothing. The HTTP boundary uses it
// to decide whether `provenance` has a predicate to narrow.
func (f WriteStampFilters) IsZero() bool {
	return f.RunID == nil && f.CredentialID == nil && f.CredentialKind == nil && f.DataClass == nil
}

// resolveStampScope maps a requested provenance mode onto the version row the
// stamp predicates apply to. The empty mode — and anything unrecognised, which
// the HTTP boundary has already rejected — resolves to "updated": a client that
// only sends a stamp filter keeps matching the current version, which is the
// behaviour before the scope selector existed.
func resolveStampScope(mode string) string {
	switch mode {
	case ProvenanceCreated, ProvenanceAny:
		return mode
	default:
		return ProvenanceUpdated
	}
}

// stampPredicate renders the set filters as one AND-ed predicate plus its
// arguments. It returns an empty predicate when nothing is set. includeDataClass
// is false for relationships, which have no such column.
func stampPredicate(f WriteStampFilters, includeDataClass bool) (string, []any) {
	conds := make([]string, 0, 4)
	args := make([]any, 0, 4)
	if f.RunID != nil {
		conds = append(conds, "(run_id = ? OR root_run_id = ?)")
		args = append(args, *f.RunID, *f.RunID)
	}
	if f.CredentialID != nil {
		conds = append(conds, "credential_id = ?")
		args = append(args, *f.CredentialID)
	}
	if f.CredentialKind != nil {
		conds = append(conds, "credential_kind = ?")
		args = append(args, *f.CredentialKind)
	}
	if includeDataClass && f.DataClass != nil {
		conds = append(conds, "data_class = ?")
		args = append(args, *f.DataClass)
	}
	return strings.Join(conds, " AND "), args
}

// stampBranchScope renders the project/branch predicate the `created` subquery
// must carry, so a run filter cannot match a version in another project (or in
// the main branch when the caller asked for a named one). It mirrors the actor
// provenance filter's scoping.
func stampBranchScope(projectID uuid.UUID, branchID *uuid.UUID) (string, []any) {
	if branchID != nil {
		return "project_id = ? AND branch_id = ?", []any{projectID, *branchID}
	}
	return "project_id = ? AND branch_id IS NULL", []any{projectID}
}

// stampCreatedSubquery renders the "written at some point" arm: the object's
// root (version = 1) row must carry the predicate. Written as IN (SELECT …)
// rather than a correlated EXISTS so kb.idx_graph_objects_project_run /
// …_project_credential drive the inner scan — the same shape the actor
// provenance filter uses.
func stampCreatedSubquery(table, predicate string, projectID uuid.UUID, branchID *uuid.UUID) (string, []any) {
	scope, scopeArgs := stampBranchScope(projectID, branchID)
	return "SELECT canonical_id FROM " + table + " WHERE " + predicate +
		" AND version = 1 AND deleted_at IS NULL AND " + scope, scopeArgs
}

// applyWriteStampFilters adds the write-stamp predicates to an object query.
//
// The run filter is an OR over two columns, so it cannot use either partial
// index on its own; the project/branch predicates the caller has already applied
// bound the scan, and the planner can still use idx_graph_objects_project_run
// for the run_id arm.
//
// mode selects which version row must carry the stamp: "updated" (the default)
// checks the HEAD row, "created" the root version = 1 row, and "any" accepts
// either. See resolveStampScope.
func applyWriteStampFilters(q *bun.SelectQuery, f WriteStampFilters, mode string, projectID uuid.UUID, branchID *uuid.UUID) *bun.SelectQuery {
	predicate, args := stampPredicate(f, true)
	if predicate == "" {
		return q
	}
	switch resolveStampScope(mode) {
	case ProvenanceCreated:
		sub, scopeArgs := stampCreatedSubquery("kb.graph_objects", predicate, projectID, branchID)
		return q.Where("canonical_id IN ("+sub+")", append(args, scopeArgs...)...)
	case ProvenanceAny:
		sub, scopeArgs := stampCreatedSubquery("kb.graph_objects", predicate, projectID, branchID)
		anyArgs := append(append([]any{}, args...), args...)
		anyArgs = append(anyArgs, scopeArgs...)
		return q.Where("("+predicate+") OR canonical_id IN ("+sub+")", anyArgs...)
	default:
		return q.Where(predicate, args...)
	}
}

// applyRelationshipWriteStampFilters is the relationship counterpart of
// applyWriteStampFilters. Relationships carry credential and run fields but no
// data class, so a DataClass filter is ignored here rather than mapped onto a
// column that does not exist; the HTTP boundary rejects it for callers.
func applyRelationshipWriteStampFilters(q *bun.SelectQuery, f WriteStampFilters, mode string, projectID uuid.UUID, branchID *uuid.UUID) *bun.SelectQuery {
	predicate, args := stampPredicate(f, false)
	if predicate == "" {
		return q
	}
	switch resolveStampScope(mode) {
	case ProvenanceCreated:
		sub, scopeArgs := stampCreatedSubquery("kb.graph_relationships", predicate, projectID, branchID)
		return q.Where("canonical_id IN ("+sub+")", append(args, scopeArgs...)...)
	case ProvenanceAny:
		sub, scopeArgs := stampCreatedSubquery("kb.graph_relationships", predicate, projectID, branchID)
		anyArgs := append(append([]any{}, args...), args...)
		anyArgs = append(anyArgs, scopeArgs...)
		return q.Where("("+predicate+") OR canonical_id IN ("+sub+")", anyArgs...)
	default:
		return q.Where(predicate, args...)
	}
}
