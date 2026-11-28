package graph

import (
	"context"

	"github.com/google/uuid"

	"github.com/emergent-company/emergent.memory/domain/extraction/agents"
)

// ConflictReconciler resolves the residual conflicting property values of a
// single same-entity object under the "reconcile" merge strategy. It is given
// ONLY the keys that differ beyond case/whitespace normalisation (after the
// deterministic L0–L3 + provenance passes), together with the type's declared
// property schema, and must return a resolved value for each conflicting key —
// combining the two sides (preferring the richer/union of detail) rather than
// picking one. Implemented by the extraction module's generative client; wired
// via SetConflictReconciler. A nil reconciler makes "reconcile" fall back to
// preserve_target.
type ConflictReconciler interface {
	// Reconcile returns the resolved value per conflicting key and the model
	// name used (for the merge audit marker). Any error makes the caller fall
	// back to preserve_target for the object.
	Reconcile(ctx context.Context, req ConflictReconcileRequest) (values map[string]any, model string, err error)
}

// ConflictReconcileRequest is one object's residual conflict set. Conflicts maps
// each conflicting key (without a leading slash) to its two sides.
type ConflictReconcileRequest struct {
	ProjectID uuid.UUID
	TypeName  string
	// PropertyDefs is the type's declared property schema (key → definition),
	// used to validate the reconciler's answers before they are applied. It may
	// be nil/empty when the project has no schema, in which case values are
	// accepted as supplied.
	PropertyDefs map[string]agents.PropertyDef
	Conflicts    map[string]ConflictValue
}

// ConflictValue holds the source and target values of one conflicting key.
type ConflictValue struct {
	Source any
	Target any
}
