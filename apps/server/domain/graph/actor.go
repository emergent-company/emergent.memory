package graph

import (
	"context"

	"github.com/google/uuid"

	"github.com/emergent-company/emergent.memory/pkg/apperror"
	"github.com/emergent-company/emergent.memory/pkg/auth"
)

// writeStamp is the attribution recorded on every graph version row (#2376).
//
// It is the actor pair plus the rest of the answer: which credential
// authenticated the write, which agent run produced it, and — where a caller
// supplies them — the data class and the observed-content digest. The fields
// live in dedicated columns, never in `properties`, because computeContentHash
// hashes properties/status/key/labels: a provenance property would flip the
// content hash on a metadata-only write and defeat carryEmbeddingIfUnchanged
// (#1478).
type writeStamp struct {
	ActorType      string
	ActorID        *uuid.UUID
	CredentialKind string
	CredentialID   *uuid.UUID
	RunID          *uuid.UUID
	RootRunID      *uuid.UUID
}

// writeStampFromContext resolves the stamp for a mutation.
//
// The actor follows the existing precedence: a WithActor-stamped actor (stamped
// at the agent run boundary, the per-agent MCP endpoint, or the extraction
// worker) wins, otherwise the fallback user actor passed by the HTTP handler.
// The credential and run are read from the request/run context, never from the
// request body, so a client cannot declare a credential it did not authenticate
// with.
func writeStampFromContext(ctx context.Context, fallbackActorID *uuid.UUID) writeStamp {
	actorType, actorID := actorFromContext(ctx, fallbackActorID)
	credentialKind, credentialID, _ := auth.CredentialFromContext(ctx)
	if credentialKind == "" {
		credentialKind = auth.CredentialNone
	}
	runID, rootRunID := auth.RunIDsFromContext(ctx)
	return writeStamp{
		ActorType:      actorType,
		ActorID:        actorID,
		CredentialKind: credentialKind,
		CredentialID:   credentialID,
		RunID:          runID,
		RootRunID:      rootRunID,
	}
}

// applyObject stamps an object version about to be written.
//
// DataClass and OwnerSourceID are only set when the caller already decided them;
// the column defaults (synthesized / none) cover every other path. A caller
// setting DataClass = source must also set OwnerSourceID — the database CHECK
// enforces the pairing.
func (w writeStamp) applyObject(obj *GraphObject) {
	if obj == nil {
		return
	}
	actorType := w.ActorType
	obj.ActorType = &actorType
	obj.ActorID = w.ActorID
	obj.CredentialKind = w.CredentialKind
	obj.CredentialID = w.CredentialID
	obj.RunID = w.RunID
	obj.RootRunID = w.RootRunID
}

// applyWriteStampFromContext fills the credential/run half of an object's stamp
// from the request context, at the repository insert points, so every write path
// is covered even when the caller did not resolve a stamp itself.
//
// An explicitly-set non-"none" credential wins: callers that already stamped
// (Service.Create via writeStampFromContext) and provenance-preserving paths are
// not overwritten, and a merge clone keeps whatever it was given. "none" is not
// treated as explicit, because the context is the authority for a write that is
// actually happening now.
func applyWriteStampFromContext(ctx context.Context, obj *GraphObject) {
	if obj == nil {
		return
	}
	if obj.CredentialKind == "" || obj.CredentialKind == auth.CredentialNone {
		kind, id, _ := auth.CredentialFromContext(ctx)
		if kind == "" {
			kind = auth.CredentialNone
		}
		obj.CredentialKind = kind
		obj.CredentialID = id
		obj.RunID, obj.RootRunID = auth.RunIDsFromContext(ctx)
	}
	if obj.DataClass == "" {
		obj.DataClass = DataClassSynthesized
	}
}

// applyRelationshipWriteStampFromContext is the relationship counterpart of
// applyWriteStampFromContext.
func applyRelationshipWriteStampFromContext(ctx context.Context, rel *GraphRelationship) {
	if rel == nil {
		return
	}
	if rel.CredentialKind == "" || rel.CredentialKind == auth.CredentialNone {
		kind, id, _ := auth.CredentialFromContext(ctx)
		if kind == "" {
			kind = auth.CredentialNone
		}
		rel.CredentialKind = kind
		rel.CredentialID = id
		rel.RunID, rel.RootRunID = auth.RunIDsFromContext(ctx)
	}
}

// Data classes, relative to external sources. Everything Memory owns — user
// notes, agent writes, extraction output — is synthesized; a source push sets
// source together with an owning source id, and an adopted synthesis is
// synthesized content a source has taken into its own write path.
const (
	DataClassSource      = "source"
	DataClassSynthesized = "synthesized"
	DataClassAdopted     = "adopted"
)

// resolveMergedOwner decides the data class and owner of a merged object.
//
// Merging two source-owned objects from different sources is refused: a merge
// mutates one object on behalf of another, and silently adopting either side's
// owner would let a source absorb another domain's object. Among objects that
// are not source-owned, an adopted side survives as adopted; everything else is
// synthesized.
func resolveMergedOwner(srcClass string, srcOwner *uuid.UUID, tgtClass string, tgtOwner *uuid.UUID) (string, *uuid.UUID, error) {
	srcIsSource := srcClass == DataClassSource
	tgtIsSource := tgtClass == DataClassSource

	// Only a clash between two real owners is a conflict. A source-owned row with
	// no owner is one whose source was deleted (the FK nulls the owner), and it
	// stays classified as source rather than being silently downgraded to
	// synthesized — losing the fact that a source once owned it would be worse
	// than carrying an empty owner.
	if srcIsSource && tgtIsSource && srcOwner != nil && tgtOwner != nil && *srcOwner != *tgtOwner {
		return "", nil, apperror.New(409, "owner_conflict",
			"cannot merge two objects owned by different sources without an explicit owner decision")
	}
	if tgtIsSource {
		return DataClassSource, tgtOwner, nil
	}
	if srcIsSource {
		return DataClassSource, srcOwner, nil
	}
	if srcClass == DataClassAdopted || tgtClass == DataClassAdopted {
		return DataClassAdopted, nil, nil
	}
	return DataClassSynthesized, nil, nil
}

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
