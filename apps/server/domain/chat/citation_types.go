package chat

import (
	"context"

	"github.com/emergent-company/emergent.memory/domain/chat/citations"
)

// citationTypeMeta loads the active project's compiled type metadata for
// citation enrichment. Best-effort: a nil repository yields nil, so citations
// keep empty (omitted) labels and the turn never fails because of it. The
// compiled-type lookup and label mapping are shared with the read-side
// derivations (history, run transcripts, share) via schemas.Repository.
func (h *Handler) citationTypeMeta(ctx context.Context, projectID string) map[string]citations.TypeMeta {
	if h.schemaRepo == nil {
		return nil
	}
	return h.schemaRepo.CitationTypeMeta(ctx, projectID)
}
