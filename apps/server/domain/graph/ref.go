package graph

import (
	"strings"

	"github.com/google/uuid"
)

// ObjectRef returns the display-only reference for a canonical object ID:
// "#" plus the last 12 hex characters of the UUID (dashes stripped), e.g.
// "#a1b2c3d4e5f6".
//
// The ref is cosmetic. It is immutable (canonical_id never changes), stable
// across versions and branches, and MUST NOT be accepted as a write handle —
// callers address objects by UUID only. It is derived here and mirrored by the
// idx_graph_objects_ref expression index and the FTS trigger so SQL and Go
// agree on the format.
func ObjectRef(canonicalID uuid.UUID) string {
	if canonicalID == uuid.Nil {
		return ""
	}
	s := strings.ReplaceAll(canonicalID.String(), "-", "")
	if len(s) > 12 {
		s = s[len(s)-12:]
	}
	return "#" + s
}
