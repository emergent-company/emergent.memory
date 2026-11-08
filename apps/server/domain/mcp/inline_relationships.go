package mcp

import (
	"context"
	"fmt"
	"strings"

	"github.com/google/uuid"

	"github.com/emergent-company/emergent.memory/domain/graph"
)

// Machine-readable reasons reported for an inline relationship that could not
// be created. They mirror the per-item results returned by relationship-create
// so an agent can branch on the reason instead of parsing prose.
const (
	inlineRelReasonInvalidSpec     = "invalid_spec"
	inlineRelReasonMissingType     = "missing_type"
	inlineRelReasonMissingEndpoint = "missing_endpoint"
	inlineRelReasonTypeNotAllowed  = "type_not_allowed"
	inlineRelReasonUnknownSourceID = "unknown_source_id"
	inlineRelReasonUnknownTargetID = "unknown_target_id"
	inlineRelReasonSelfReference   = "self_reference"
	inlineRelReasonCreateFailed    = "relationship_create_failed"
	inlineRelReasonBranchMismatch  = "branch_mismatch"
)

// inlineRelationship is the created-edge shape returned for inline
// relationships. It matches relationship-create's returned relationship.
type inlineRelationship struct {
	ID       string `json:"id"`
	Type     string `json:"type"`
	SourceID string `json:"source_id"`
	TargetID string `json:"target_id"`
}

// inlineRelationshipResult is the per-inline-relationship outcome. It follows
// relationship-create's per-item result shape ({ok, relationship|error, index})
// and adds a machine-readable reason on failure (issue #401).
type inlineRelationshipResult struct {
	Ok           bool                `json:"ok"`
	Index        int                 `json:"index"`
	Relationship *inlineRelationship `json:"relationship,omitempty"`
	Reason       string              `json:"reason,omitempty"`
	Error        string              `json:"error,omitempty"`
}

// inlineRelationshipDeps are the effects an inline relationship batch needs.
// They are injected so the reporting logic can be exercised without a database
// or graph service.
type inlineRelationshipDeps struct {
	// ResolveID resolves a UUID string or entity key to a canonical entity id.
	ResolveID func(string) (uuid.UUID, error)
	// Create creates the relationship. projectID and branchID are captured by
	// the caller's closure.
	Create func(context.Context, *graph.CreateGraphRelationshipRequest) (*graph.GraphRelationshipResponse, error)
	// ObjectType optionally resolves the object type of a canonical id, used to
	// enforce agent object-type scope on the pre-existing endpoint. Nil means
	// "unknown", and the endpoint-type check is skipped.
	ObjectType func(uuid.UUID) (string, bool)
	// AllowsRel and AllowsObject apply the agent's type scope.
	AllowsRel    func(string) bool
	AllowsObject func(string) bool
}

// processInlineRelationships creates the inline relationship specs declared on
// one entity-create item and reports every attempt. A spec that cannot be
// resolved or created is returned as a failed per-item result instead of being
// silently dropped (issue #401): the caller decides how to surface it.
//
// Both source_id and target_id are supported: source_id means the pre-existing
// entity is the source (source_id → new entity), target_id means the new entity
// is the source (new entity → target_id). Each accepts a UUID string or an
// entity key.
func processInlineRelationships(
	ctx context.Context,
	entityType string,
	entityID uuid.UUID,
	relsRaw []any,
	branchID *uuid.UUID,
	deps inlineRelationshipDeps,
) (created []inlineRelationship, results []inlineRelationshipResult) {
	fail := func(index int, reason, msg string) {
		results = append(results, inlineRelationshipResult{
			Ok:     false,
			Index:  index,
			Reason: reason,
			Error:  msg,
		})
	}

	for i, relRaw := range relsRaw {
		relMap, ok := relRaw.(map[string]any)
		if !ok {
			fail(i, inlineRelReasonInvalidSpec, "invalid relationship specification")
			continue
		}

		relType, _ := relMap["type"].(string)
		if relType == "" {
			relType, _ = relMap["relationship_type"].(string)
		}
		if relType == "" {
			fail(i, inlineRelReasonMissingType, "missing relationship type")
			continue
		}
		if deps.AllowsRel != nil && !deps.AllowsRel(relType) {
			fail(i, inlineRelReasonTypeNotAllowed,
				fmt.Sprintf("relationship type %q is not allowed for this agent's scope", relType))
			continue
		}

		// resolvedID is the pre-existing endpoint; the new entity is the other.
		var (
			resolvedID uuid.UUID
			reason     string
			field      string
			err        error
		)
		if sourceIDStr, _ := relMap["source_id"].(string); sourceIDStr != "" {
			field, reason = "source_id", inlineRelReasonUnknownSourceID
			resolvedID, err = deps.ResolveID(sourceIDStr)
			if err != nil {
				fail(i, reason, fmt.Sprintf("cannot resolve source_id %q: %v", sourceIDStr, err))
				continue
			}
		} else {
			targetIDStr, _ := relMap["target_id"].(string)
			if targetIDStr == "" {
				fail(i, inlineRelReasonMissingEndpoint,
					"inline relationship requires a source_id or target_id")
				continue
			}
			field, reason = "target_id", inlineRelReasonUnknownTargetID
			resolvedID, err = deps.ResolveID(targetIDStr)
			if err != nil {
				fail(i, reason, fmt.Sprintf("cannot resolve target_id %q: %v", targetIDStr, err))
				continue
			}
		}

		// The pre-existing endpoint's object type and the new entity's type must
		// both be permitted by the agent's scope; this is the "type not allowed
		// between the two types" case.
		if deps.ObjectType != nil && deps.AllowsObject != nil {
			if otherType, ok := deps.ObjectType(resolvedID); ok && !deps.AllowsObject(otherType) {
				fail(i, inlineRelReasonTypeNotAllowed,
					fmt.Sprintf("relationship type %q is not allowed between %q and %q for this agent's scope", relType, otherType, entityType))
				continue
			}
		}

		req := &graph.CreateGraphRelationshipRequest{
			Type:     relType,
			BranchID: branchID,
		}
		if relProps, ok := relMap["properties"].(map[string]any); ok {
			req.Properties = relProps
		}
		if field == "source_id" {
			req.SrcID = resolvedID
			req.DstID = entityID
		} else {
			req.SrcID = entityID
			req.DstID = resolvedID
		}

		if req.SrcID == req.DstID {
			fail(i, inlineRelReasonSelfReference,
				fmt.Sprintf("%s resolves to the entity being created (%s); a relationship must connect two different entities", field, entityID))
			continue
		}

		relResult, err := deps.Create(ctx, req)
		if err != nil {
			fail(i, classifyInlineRelationshipError(err), err.Error())
			continue
		}

		edge := inlineRelationship{
			ID:       relResult.CanonicalID.String(),
			Type:     relResult.Type,
			SourceID: relResult.SrcID.String(),
			TargetID: relResult.DstID.String(),
		}
		created = append(created, edge)
		results = append(results, inlineRelationshipResult{
			Ok:           true,
			Index:        i,
			Relationship: &edge,
		})
	}

	return created, results
}

// classifyInlineRelationshipError maps a graph-layer create error to a
// machine-readable reason. Endpoint-not-found errors can surface here when an
// id resolved but the row is on another branch or was deleted after resolution.
func classifyInlineRelationshipError(err error) string {
	if err == nil {
		return ""
	}
	msg := strings.ToLower(err.Error())
	switch {
	case strings.Contains(msg, "src_object_not_found"), strings.Contains(msg, "src_object_deleted"):
		return inlineRelReasonUnknownSourceID
	case strings.Contains(msg, "dst_object_not_found"), strings.Contains(msg, "dst_object_deleted"):
		return inlineRelReasonUnknownTargetID
	case strings.Contains(msg, "branch_mismatch"):
		return inlineRelReasonBranchMismatch
	case strings.Contains(msg, "not allowed"), strings.Contains(msg, "not permitted"),
		strings.Contains(msg, "invalid relationship type"), strings.Contains(msg, "endpoint type"):
		return inlineRelReasonTypeNotAllowed
	default:
		return inlineRelReasonCreateFailed
	}
}
