package graph

import (
	"context"

	"github.com/google/uuid"

	"github.com/emergent-company/emergent.memory/pkg/auth"
)

// actorFromContext resolves the acting actor for a graph mutation.
//
// When the context carries a WithActor-stamped actor (stamped at the agent run
// boundary, the per-agent MCP endpoint, or the extraction worker), that actor is
// used verbatim and the fallback is ignored. Otherwise the fallback user actor —
// the authenticated user UUID passed by HTTP handlers — is used, preserving the
// existing direct-HTTP behavior.
func actorFromContext(ctx context.Context, fallbackActorID *uuid.UUID) (actorType string, actorID *uuid.UUID) {
	if t, id, ok := auth.ActorFromContext(ctx); ok {
		return t, id
	}
	return ActorUser, fallbackActorID
}
