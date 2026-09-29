package graph

import (
	"context"
	"testing"

	"github.com/google/uuid"

	"github.com/emergent-company/emergent.memory/pkg/auth"
)

// TestActorTypeConstants locks the actor-type values used for provenance
// attribution so a rename cannot silently break filtering on the (actor_type,
// actor_id) pair.
func TestActorTypeConstants(t *testing.T) {
	if ActorUser != "user" {
		t.Errorf("expected ActorUser == %q, got %q", "user", ActorUser)
	}
	if ActorAgent != "agent" {
		t.Errorf("expected ActorAgent == %q, got %q", "agent", ActorAgent)
	}
	if ActorSystem != "system" {
		t.Errorf("expected ActorSystem == %q, got %q", "system", ActorSystem)
	}
}

// TestActorFromContext_FallbackUser covers the direct-HTTP path: no actor is
// stamped, so the fallback user actor is used verbatim.
func TestActorFromContext_FallbackUser(t *testing.T) {
	ctx := context.Background()
	userID := uuid.New()

	actorType, actorID := actorFromContext(ctx, &userID)
	if actorType != ActorUser {
		t.Errorf("expected fallback actor type user, got %q", actorType)
	}
	if actorID == nil || *actorID != userID {
		t.Errorf("expected fallback actor id %s, got %v", userID, actorID)
	}
}

// TestActorFromContext_FallbackNil covers a caller (e.g. a relationship mutator
// with no HTTP principal) that passes a nil fallback: it resolves to user with
// a nil id, never to a fabricated id.
func TestActorFromContext_FallbackNil(t *testing.T) {
	ctx := context.Background()

	actorType, actorID := actorFromContext(ctx, nil)
	if actorType != ActorUser {
		t.Errorf("expected fallback actor type user, got %q", actorType)
	}
	if actorID != nil {
		t.Errorf("expected nil fallback actor id, got %v", actorID)
	}
}

// TestActorFromContext_StampedAgent covers the agent run boundary / per-agent MCP
// endpoint: the stamped actor wins over the fallback.
func TestActorFromContext_StampedAgent(t *testing.T) {
	ctx := context.Background()
	agentID := uuid.New()
	ctx = auth.WithActor(ctx, ActorAgent, &agentID)

	fallbackUserID := uuid.New()
	actorType, actorID := actorFromContext(ctx, &fallbackUserID)
	if actorType != ActorAgent {
		t.Errorf("expected stamped actor type agent, got %q", actorType)
	}
	if actorID == nil || *actorID != agentID {
		t.Errorf("expected stamped actor id %s, got %v", agentID, actorID)
	}
}

// TestActorFromContext_StampedSystem covers the extraction worker: system actor
// with a nil id, overriding any fallback.
func TestActorFromContext_StampedSystem(t *testing.T) {
	ctx := context.Background()
	ctx = auth.WithActor(ctx, ActorSystem, nil)

	fallbackUserID := uuid.New()
	actorType, actorID := actorFromContext(ctx, &fallbackUserID)
	if actorType != ActorSystem {
		t.Errorf("expected stamped actor type system, got %q", actorType)
	}
	if actorID != nil {
		t.Errorf("expected nil stamped actor id, got %v", actorID)
	}
}

// TestActorFromContext_Cleared covers the delegated-run fallback: a parent agent
// actor that has been cleared (WithActor(ctx, "", nil)) must NOT inherit — the
// mutation falls back to the user actor instead of attributing to the parent.
func TestActorFromContext_Cleared(t *testing.T) {
	ctx := context.Background()
	parentID := uuid.New()
	ctx = auth.WithActor(ctx, ActorAgent, &parentID)
	ctx = auth.WithActor(ctx, "", nil) // clear the parent actor

	fallbackUserID := uuid.New()
	actorType, actorID := actorFromContext(ctx, &fallbackUserID)
	if actorType != ActorUser {
		t.Errorf("expected cleared actor to fall back to user, got %q", actorType)
	}
	if actorID == nil || *actorID != fallbackUserID {
		t.Errorf("expected fallback user id %s, got %v", fallbackUserID, actorID)
	}
}
