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

// applyWriteStampFilters adds the write-stamp predicates to an object query.
//
// The run filter is an OR over two columns, so it cannot use either partial
// index on its own; the project/branch predicates the caller has already applied
// bound the scan, and the planner can still use idx_graph_objects_project_run
// for the run_id arm.
func applyWriteStampFilters(q *bun.SelectQuery, f WriteStampFilters) *bun.SelectQuery {
	if f.RunID != nil {
		q = q.Where("(run_id = ? OR root_run_id = ?)", *f.RunID, *f.RunID)
	}
	if f.CredentialID != nil {
		q = q.Where("credential_id = ?", *f.CredentialID)
	}
	if f.CredentialKind != nil {
		q = q.Where("credential_kind = ?", *f.CredentialKind)
	}
	if f.DataClass != nil {
		q = q.Where("data_class = ?", *f.DataClass)
	}
	return q
}

// applyRelationshipWriteStampFilters is the relationship counterpart of
// applyWriteStampFilters. Relationships carry credential and run fields but no
// data class, so a DataClass filter is ignored here rather than mapped onto a
// column that does not exist; the HTTP boundary rejects it for callers.
func applyRelationshipWriteStampFilters(q *bun.SelectQuery, f WriteStampFilters) *bun.SelectQuery {
	if f.RunID != nil {
		q = q.Where("(run_id = ? OR root_run_id = ?)", *f.RunID, *f.RunID)
	}
	if f.CredentialID != nil {
		q = q.Where("credential_id = ?", *f.CredentialID)
	}
	if f.CredentialKind != nil {
		q = q.Where("credential_kind = ?", *f.CredentialKind)
	}
	return q
}
