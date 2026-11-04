package graph

import (
	"context"
	"testing"

	"github.com/google/uuid"

	"github.com/emergent-company/emergent.memory/pkg/auth"
)

// TestWriteStampFromContext_HTTPUser covers the direct-HTTP path with a human
// session (no API token): the actor is the fallback user, the credential kind is
// `session` with no credential id, and a context with nothing stamped resolves to
// an explicit "none" rather than an empty string, so the stored stamp is never
// silently blank (#2376).
func TestWriteStampFromContext_HTTPUser(t *testing.T) {
	ctx := auth.ContextWithCredential(context.Background(), auth.CredentialSession, nil)
	userID := uuid.New()

	stamp := writeStampFromContext(ctx, &userID)
	if stamp.ActorType != ActorUser {
		t.Errorf("expected actor type user, got %q", stamp.ActorType)
	}
	if stamp.ActorID == nil || *stamp.ActorID != userID {
		t.Errorf("expected actor id %s, got %v", userID, stamp.ActorID)
	}
	if stamp.CredentialKind != auth.CredentialSession {
		t.Errorf("expected credential kind %q, got %q", auth.CredentialSession, stamp.CredentialKind)
	}
	if stamp.CredentialID != nil {
		t.Errorf("expected nil credential id for a session write, got %v", stamp.CredentialID)
	}
	if stamp.RunID != nil || stamp.RootRunID != nil {
		t.Errorf("expected no run ids outside a run, got %v/%v", stamp.RunID, stamp.RootRunID)
	}
}

// TestWriteStampFromContext_BackgroundWrite covers extraction and other
// background work: system actor, no credential, and "none" rather than empty.
func TestWriteStampFromContext_BackgroundWrite(t *testing.T) {
	ctx := auth.WithActor(context.Background(), ActorSystem, nil)

	stamp := writeStampFromContext(ctx, stampTestUUIDPtr())
	if stamp.ActorType != ActorSystem {
		t.Errorf("expected actor type system, got %q", stamp.ActorType)
	}
	if stamp.ActorID != nil {
		t.Errorf("expected nil actor id for a system write, got %v", stamp.ActorID)
	}
	if stamp.CredentialKind != auth.CredentialNone {
		t.Errorf("expected credential kind %q when nothing was stamped, got %q", auth.CredentialNone, stamp.CredentialKind)
	}
}

// TestWriteStampFromContext_AgentRun covers the case the actor pair alone cannot
// answer: an agent write records the agent definition as the actor AND the run
// separately, so "which session wrote this" is answerable from the row.
func TestWriteStampFromContext_AgentRun(t *testing.T) {
	agentID := uuid.New()
	runID := uuid.New()
	rootRunID := uuid.New()
	credentialID := uuid.New()

	ctx := context.Background()
	ctx = auth.WithActor(ctx, ActorAgent, &agentID)
	ctx = auth.ContextWithCredential(ctx, auth.CredentialAPIKey, &credentialID)
	ctx = auth.ContextWithRunIDs(ctx, &runID, &rootRunID)

	stamp := writeStampFromContext(ctx, stampTestUUIDPtr())
	if stamp.ActorType != ActorAgent || stamp.ActorID == nil || *stamp.ActorID != agentID {
		t.Fatalf("expected agent actor %s, got %q/%v", agentID, stamp.ActorType, stamp.ActorID)
	}
	if stamp.CredentialKind != auth.CredentialAPIKey {
		t.Errorf("expected credential kind %q, got %q", auth.CredentialAPIKey, stamp.CredentialKind)
	}
	if stamp.CredentialID == nil || *stamp.CredentialID != credentialID {
		t.Errorf("expected credential id %s, got %v", credentialID, stamp.CredentialID)
	}
	if stamp.RunID == nil || *stamp.RunID != runID {
		t.Errorf("expected run id %s, got %v", runID, stamp.RunID)
	}
	if stamp.RootRunID == nil || *stamp.RootRunID != rootRunID {
		t.Errorf("expected root run id %s, got %v", rootRunID, stamp.RootRunID)
	}
}

// TestApplyWriteStampFromContext_FillsFromContext covers the repository insert
// path: a write that did not resolve a stamp itself still records the
// credential and run from the context, and defaults its data class.
func TestApplyWriteStampFromContext_FillsFromContext(t *testing.T) {
	credentialID := uuid.New()
	runID := uuid.New()
	rootRunID := uuid.New()

	ctx := context.Background()
	ctx = auth.ContextWithCredential(ctx, auth.CredentialAPIKey, &credentialID)
	ctx = auth.ContextWithRunIDs(ctx, &runID, &rootRunID)

	obj := &GraphObject{}
	applyWriteStampFromContext(ctx, obj)

	if obj.CredentialKind != auth.CredentialAPIKey {
		t.Errorf("expected credential kind %q, got %q", auth.CredentialAPIKey, obj.CredentialKind)
	}
	if obj.CredentialID == nil || *obj.CredentialID != credentialID {
		t.Errorf("expected credential id %s, got %v", credentialID, obj.CredentialID)
	}
	if obj.RunID == nil || *obj.RunID != runID {
		t.Errorf("expected run id %s, got %v", runID, obj.RunID)
	}
	if obj.DataClass != DataClassSynthesized {
		t.Errorf("expected default data class %q, got %q", DataClassSynthesized, obj.DataClass)
	}
}

// TestApplyWriteStampFromContext_ExplicitCredentialWins covers a provenance-
// preserving path that already assigned a stamp (a merge clone keeping an
// mcp_link credential): the context must not overwrite it.
func TestApplyWriteStampFromContext_ExplicitCredentialWins(t *testing.T) {
	ctx := auth.ContextWithCredential(context.Background(), auth.CredentialAPIKey, stampTestUUIDPtr())

	obj := &GraphObject{CredentialKind: auth.CredentialMCPLink}
	applyWriteStampFromContext(ctx, obj)

	if obj.CredentialKind != auth.CredentialMCPLink {
		t.Errorf("expected explicit credential kind %q to survive, got %q", auth.CredentialMCPLink, obj.CredentialKind)
	}
}

// TestApplyWriteStampFromContext_NoneIsNotExplicit covers the "none" case: an
// object left at the column default must still pick up the context credential,
// because "none" describes the absence of information, not a decision.
func TestApplyWriteStampFromContext_NoneIsNotExplicit(t *testing.T) {
	credentialID := uuid.New()
	ctx := auth.ContextWithCredential(context.Background(), auth.CredentialAPIKey, &credentialID)

	obj := &GraphObject{CredentialKind: auth.CredentialNone}
	applyWriteStampFromContext(ctx, obj)

	if obj.CredentialKind != auth.CredentialAPIKey {
		t.Errorf("expected context credential %q to replace %q, got %q",
			auth.CredentialAPIKey, auth.CredentialNone, obj.CredentialKind)
	}
	if obj.CredentialID == nil || *obj.CredentialID != credentialID {
		t.Errorf("expected credential id %s, got %v", credentialID, obj.CredentialID)
	}
}

// TestApplyRelationshipWriteStampFromContext covers relationships, which carry
// the credential and run but no data class or source owner in this slice.
func TestApplyRelationshipWriteStampFromContext(t *testing.T) {
	credentialID := uuid.New()
	ctx := auth.ContextWithCredential(context.Background(), auth.CredentialAPIKey, &credentialID)

	rel := &GraphRelationship{}
	applyRelationshipWriteStampFromContext(ctx, rel)

	if rel.CredentialKind != auth.CredentialAPIKey {
		t.Errorf("expected credential kind %q, got %q", auth.CredentialAPIKey, rel.CredentialKind)
	}
	if rel.CredentialID == nil || *rel.CredentialID != credentialID {
		t.Errorf("expected credential id %s, got %v", credentialID, rel.CredentialID)
	}
}

// TestResolveMergedOwner locks the owner decision for merges: one owner, never a
// silent adoption of a second source's ownership.
func TestResolveMergedOwner(t *testing.T) {
	sourceA := uuid.New()
	sourceB := uuid.New()

	tests := []struct {
		name      string
		srcClass  string
		srcOwner  *uuid.UUID
		tgtClass  string
		tgtOwner  *uuid.UUID
		wantClass string
		wantOwner *uuid.UUID
		wantErr   bool
	}{
		{
			name:      "two synthesized objects stay synthesized",
			srcClass:  DataClassSynthesized,
			tgtClass:  DataClassSynthesized,
			wantClass: DataClassSynthesized,
		},
		{
			name:      "a source-owned side owns the merge",
			srcClass:  DataClassSource,
			srcOwner:  &sourceA,
			tgtClass:  DataClassSynthesized,
			wantClass: DataClassSource,
			wantOwner: &sourceA,
		},
		{
			name:      "the same source on both sides merges cleanly",
			srcClass:  DataClassSource,
			srcOwner:  &sourceA,
			tgtClass:  DataClassSource,
			tgtOwner:  &sourceA,
			wantClass: DataClassSource,
			wantOwner: &sourceA,
		},
		{
			name:     "two different source owners are refused",
			srcClass: DataClassSource,
			srcOwner: &sourceA,
			tgtClass: DataClassSource,
			tgtOwner: &sourceB,
			wantErr:  true,
		},
		{
			// A source row whose source was deleted keeps data_class='source' with a
			// NULL owner (the FK nulls it). Downgrading it to synthesized would
			// erase the fact that a source once owned it.
			name:      "a source row with a purged owner keeps its class",
			srcClass:  DataClassSource,
			tgtClass:  DataClassSynthesized,
			wantClass: DataClassSource,
		},
		{
			name:      "adopted survives alongside synthesized",
			srcClass:  DataClassAdopted,
			tgtClass:  DataClassSynthesized,
			wantClass: DataClassAdopted,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			class, owner, err := resolveMergedOwner(tt.srcClass, tt.srcOwner, tt.tgtClass, tt.tgtOwner)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("expected an error for two different source owners, got class=%q owner=%v", class, owner)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if class != tt.wantClass {
				t.Errorf("expected class %q, got %q", tt.wantClass, class)
			}
			switch {
			case tt.wantOwner == nil && owner != nil:
				t.Errorf("expected no owner, got %v", owner)
			case tt.wantOwner != nil && (owner == nil || *owner != *tt.wantOwner):
				t.Errorf("expected owner %s, got %v", *tt.wantOwner, owner)
			}
		})
	}
}

// TestGraphObjectResponse_ExposesWriteStamp guards the read surface: the stamp
// must reach the API response, otherwise the columns are recorded and invisible.
func TestGraphObjectResponse_ExposesWriteStamp(t *testing.T) {
	credentialID := uuid.New()
	runID := uuid.New()
	rootRunID := uuid.New()
	ownerSourceID := uuid.New()
	inputHash := "abc123"

	obj := &GraphObject{
		Type:           "Note",
		CredentialKind: auth.CredentialMCPLink,
		CredentialID:   &credentialID,
		RunID:          &runID,
		RootRunID:      &rootRunID,
		DataClass:      DataClassSource,
		OwnerSourceID:  &ownerSourceID,
		InputHash:      &inputHash,
	}

	resp := obj.ToResponse()
	if resp.CredentialKind != auth.CredentialMCPLink {
		t.Errorf("expected credential kind %q in response, got %q", auth.CredentialMCPLink, resp.CredentialKind)
	}
	if resp.CredentialID == nil || *resp.CredentialID != credentialID {
		t.Errorf("expected credential id %s in response, got %v", credentialID, resp.CredentialID)
	}
	if resp.RunID == nil || *resp.RunID != runID {
		t.Errorf("expected run id %s in response, got %v", runID, resp.RunID)
	}
	if resp.RootRunID == nil || *resp.RootRunID != rootRunID {
		t.Errorf("expected root run id %s in response, got %v", rootRunID, resp.RootRunID)
	}
	if resp.DataClass != DataClassSource {
		t.Errorf("expected data class %q in response, got %q", DataClassSource, resp.DataClass)
	}
	if resp.OwnerSourceID == nil || *resp.OwnerSourceID != ownerSourceID {
		t.Errorf("expected owner source id %s in response, got %v", ownerSourceID, resp.OwnerSourceID)
	}
	if resp.InputHash == nil || *resp.InputHash != inputHash {
		t.Errorf("expected input hash %q in response, got %v", inputHash, resp.InputHash)
	}
}

// TestGraphRelationshipResponse_ExposesWriteStamp is the relationship counterpart.
func TestGraphRelationshipResponse_ExposesWriteStamp(t *testing.T) {
	credentialID := uuid.New()
	runID := uuid.New()

	rel := &GraphRelationship{
		Type:           "RELATES_TO",
		CredentialKind: auth.CredentialAPIKey,
		CredentialID:   &credentialID,
		RunID:          &runID,
	}

	resp := rel.ToResponse()
	if resp.CredentialKind != auth.CredentialAPIKey {
		t.Errorf("expected credential kind %q in response, got %q", auth.CredentialAPIKey, resp.CredentialKind)
	}
	if resp.CredentialID == nil || *resp.CredentialID != credentialID {
		t.Errorf("expected credential id %s in response, got %v", credentialID, resp.CredentialID)
	}
	if resp.RunID == nil || *resp.RunID != runID {
		t.Errorf("expected run id %s in response, got %v", runID, resp.RunID)
	}
}

// stampTestUUIDPtr returns a pointer to a fresh UUID, for the fallback-actor
// arguments these tests pass but do not assert on.
func stampTestUUIDPtr() *uuid.UUID {
	id := uuid.New()
	return &id
}
