package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/emergent-company/emergent.memory/domain/graph"
)

// inlineTestDeps wires a deterministic in-memory graph layer. resolveErr maps an
// id/key to the error the real key lookup would return.
type inlineTestDeps struct {
	resolveErr map[string]error
	createErr  map[string]error
	created    []*graph.CreateGraphRelationshipRequest
}

func (d *inlineTestDeps) deps() inlineRelationshipDeps {
	return inlineRelationshipDeps{
		ResolveID: func(idOrKey string) (uuid.UUID, error) {
			if err, ok := d.resolveErr[idOrKey]; ok {
				return uuid.Nil, err
			}
			// Deterministic id derived from the key so assertions can compare it.
			return uuid.NewSHA1(uuid.NameSpaceURL, []byte(idOrKey)), nil
		},
		Create: func(_ context.Context, req *graph.CreateGraphRelationshipRequest) (*graph.GraphRelationshipResponse, error) {
			d.created = append(d.created, req)
			if err, ok := d.createErr[req.Type]; ok {
				return nil, err
			}
			return &graph.GraphRelationshipResponse{
				CanonicalID: uuid.New(),
				Type:        req.Type,
				SrcID:       req.SrcID,
				DstID:       req.DstID,
			}, nil
		},
		AllowsRel:    func(string) bool { return true },
		AllowsObject: func(string) bool { return true },
	}
}

func TestProcessInlineRelationshipsReportsUnresolvableEndpoints(t *testing.T) {
	entityID := uuid.New()
	d := &inlineTestDeps{resolveErr: map[string]error{
		"PLACEHOLDER": errors.New(`key "PLACEHOLDER" not found`),
	}}

	created, results := processInlineRelationships(context.Background(), "Obligation", entityID, []any{
		map[string]any{"type": "has_subject", "source_id": "PLACEHOLDER"},
		map[string]any{"type": "has_owner", "target_id": "PLACEHOLDER"},
	}, nil, d.deps())

	// This is the #401 regression: an unresolvable spec must appear in the
	// response instead of vanishing.
	require.Len(t, results, 2)
	assert.Empty(t, created)

	assert.False(t, results[0].Ok)
	assert.Equal(t, inlineRelReasonUnknownSourceID, results[0].Reason)
	assert.Contains(t, results[0].Error, `cannot resolve source_id "PLACEHOLDER"`)

	assert.False(t, results[1].Ok)
	assert.Equal(t, inlineRelReasonUnknownTargetID, results[1].Reason)
	assert.Contains(t, results[1].Error, `cannot resolve target_id "PLACEHOLDER"`)
}

func TestProcessInlineRelationshipsReportsTypeNotAllowed(t *testing.T) {
	entityID := uuid.New()
	d := &inlineTestDeps{}

	// (a) the relationship type itself is outside the agent's scope.
	_, results := processInlineRelationships(context.Background(), "Obligation", entityID, []any{
		map[string]any{"type": "forbidden_rel", "target_id": "some-key"},
	}, nil, func() inlineRelationshipDeps {
		dd := d.deps()
		dd.AllowsRel = func(relType string) bool { return relType != "forbidden_rel" }
		return dd
	}())
	require.Len(t, results, 1)
	assert.Equal(t, inlineRelReasonTypeNotAllowed, results[0].Reason)

	// (b) the pre-existing endpoint's object type is outside the agent's scope,
	// i.e. the relationship type is not permitted between these two types.
	_, results = processInlineRelationships(context.Background(), "Obligation", entityID, []any{
		map[string]any{"type": "has_subject", "source_id": "secret-key"},
	}, nil, func() inlineRelationshipDeps {
		dd := d.deps()
		dd.ObjectType = func(uuid.UUID) (string, bool) { return "Secret", true }
		dd.AllowsObject = func(objType string) bool { return objType != "Secret" }
		return dd
	}())
	require.Len(t, results, 1)
	assert.Equal(t, inlineRelReasonTypeNotAllowed, results[0].Reason)
	assert.Contains(t, results[0].Error, "not allowed between")
	assert.Empty(t, d.created, "a disallowed relationship must not be attempted")
}

func TestProcessInlineRelationshipsCreatesResolvableAndIsolatesFailures(t *testing.T) {
	entityID := uuid.New()
	d := &inlineTestDeps{resolveErr: map[string]error{
		"missing-key": errors.New("not found"),
	}}

	created, results := processInlineRelationships(context.Background(), "Obligation", entityID, []any{
		map[string]any{"type": "has_subject", "source_id": "person-key"},
		map[string]any{"type": "has_owner", "target_id": "missing-key"},
		map[string]any{"type": "has_reviewer", "target_id": "reviewer-key"},
	}, nil, d.deps())

	require.Len(t, results, 3)
	require.Len(t, created, 2)
	assert.Equal(t, 2, countOkResults(results))

	// source_id -> new entity is the destination.
	srcID := uuid.NewSHA1(uuid.NameSpaceURL, []byte("person-key"))
	assert.Equal(t, srcID, d.created[0].SrcID)
	assert.Equal(t, entityID, d.created[0].DstID)

	// target_id -> new entity is the source.
	assert.Equal(t, entityID, d.created[1].SrcID)
	assert.Equal(t, uuid.NewSHA1(uuid.NameSpaceURL, []byte("reviewer-key")), d.created[1].DstID)

	assert.Equal(t, inlineRelReasonUnknownTargetID, results[1].Reason)
	assert.True(t, results[0].Ok)
	assert.True(t, results[2].Ok)
}

func TestProcessInlineRelationshipsReportsMalformedSpecs(t *testing.T) {
	entityID := uuid.New()
	d := &inlineTestDeps{}

	_, results := processInlineRelationships(context.Background(), "Obligation", entityID, []any{
		"not-a-map",
		map[string]any{"source_id": "person-key"},                           // no type
		map[string]any{"type": "has_subject"},                               // no endpoint
		map[string]any{"type": "self_loop", "target_id": entityID.String()}, // resolves to itself
	}, nil, func() inlineRelationshipDeps {
		dd := d.deps()
		dd.ResolveID = func(idOrKey string) (uuid.UUID, error) {
			if id, err := uuid.Parse(idOrKey); err == nil {
				return id, nil
			}
			return uuid.NewSHA1(uuid.NameSpaceURL, []byte(idOrKey)), nil
		}
		return dd
	}())

	require.Len(t, results, 4)
	assert.Equal(t, inlineRelReasonInvalidSpec, results[0].Reason)
	assert.Equal(t, inlineRelReasonMissingType, results[1].Reason)
	assert.Equal(t, inlineRelReasonMissingEndpoint, results[2].Reason)
	assert.Equal(t, inlineRelReasonSelfReference, results[3].Reason)
	assert.Empty(t, d.created)
}

func TestProcessInlineRelationshipsClassifiesCreateErrors(t *testing.T) {
	entityID := uuid.New()
	d := &inlineTestDeps{createErr: map[string]error{
		"dst_gone":   errors.New("dst_object_not_found"),
		"bad_type":   errors.New(`relationship type "has_subject" is not allowed between Obligation and Secret`),
		"mismatch":   errors.New("relationship_branch_mismatch"),
		"unexpected": errors.New("boom"),
	}}

	_, results := processInlineRelationships(context.Background(), "Obligation", entityID, []any{
		map[string]any{"type": "dst_gone", "target_id": "a"},
		map[string]any{"type": "bad_type", "target_id": "b"},
		map[string]any{"type": "mismatch", "target_id": "c"},
		map[string]any{"type": "unexpected", "target_id": "d"},
	}, nil, d.deps())

	require.Len(t, results, 4)
	assert.Equal(t, inlineRelReasonUnknownTargetID, results[0].Reason)
	assert.Equal(t, inlineRelReasonTypeNotAllowed, results[1].Reason)
	assert.Equal(t, inlineRelReasonBranchMismatch, results[2].Reason)
	assert.Equal(t, inlineRelReasonCreateFailed, results[3].Reason)
}

// TestInlineRelationshipResultJSONShape pins the machine-readable contract: the
// per-item result mirrors relationship-create (ok/relationship/error/index) and
// carries a reason on failure.
func TestInlineRelationshipResultJSONShape(t *testing.T) {
	edge := inlineRelationship{
		ID:       uuid.NewString(),
		Type:     "has_subject",
		SourceID: uuid.NewString(),
		TargetID: uuid.NewString(),
	}
	payload, err := json.Marshal([]inlineRelationshipResult{
		{Ok: true, Index: 0, Relationship: &edge},
		{Ok: false, Index: 1, Reason: inlineRelReasonUnknownSourceID, Error: "cannot resolve source_id"},
	})
	require.NoError(t, err)

	var decoded []map[string]any
	require.NoError(t, json.Unmarshal(payload, &decoded))

	require.Len(t, decoded, 2)
	assert.Equal(t, true, decoded[0]["ok"])
	assert.Contains(t, decoded[0], "relationship")
	assert.NotContains(t, decoded[0], "reason")

	assert.Equal(t, false, decoded[1]["ok"])
	assert.Equal(t, inlineRelReasonUnknownSourceID, decoded[1]["reason"])
	assert.Contains(t, decoded[1], "error")
	assert.Equal(t, float64(1), decoded[1]["index"])
}

func countOkResults(results []inlineRelationshipResult) int {
	n := 0
	for _, r := range results {
		if r.Ok {
			n++
		}
	}
	return n
}
