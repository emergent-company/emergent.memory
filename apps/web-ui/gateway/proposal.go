package main

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
)

// proposalEnvelope is the optional `proposal` argument an agent attaches to
// ask_user: a kind discriminator, a human-readable summary, and a structured
// body. Body stays raw — only kind "blueprint" is rendered structurally today;
// other kinds degrade to a summary-only card, never an error.
type proposalEnvelope struct {
	Kind    string          `json:"kind"`
	Summary string          `json:"summary"`
	Body    json.RawMessage `json:"body"`
}

// proposalBlueprintBody is the "blueprint" proposal body — the same manifest
// shape a blueprint pack carries (objectTypes + relationshipTypes as raw maps),
// so the parsers in blueprints.go apply unchanged.
type proposalBlueprintBody struct {
	ObjectTypes       []map[string]any `json:"objectTypes"`
	RelationshipTypes []map[string]any `json:"relationshipTypes"`
}

// ProposalCard is the render model for a proposal card: a side-effect summary
// (diff counts vs the project's current types, empty here) plus read-only
// object/relationship previews and the proposal's own summary/kind for scope.
type ProposalCard struct {
	Kind    string
	Summary string

	ObjectTypes       []ObjectTypeDetail
	RelationshipTypes []RelationshipTypeDetail

	AddedObjectTypes         int
	ChangedObjectTypes       int
	RemovedObjectTypes       int
	AddedRelationshipTypes   int
	ChangedRelationshipTypes int
	RemovedRelationshipTypes int
}

// buildProposalCard parses a raw proposal envelope into the card model, or
// returns nil when the payload is absent, malformed, or carries no renderable
// content (invalid proposals degrade to the plain markdown path).
func buildProposalCard(raw json.RawMessage) *ProposalCard {
	var env proposalEnvelope
	if err := json.Unmarshal(raw, &env); err != nil || env.Kind == "" {
		return nil
	}
	if env.Kind != "blueprint" {
		// Unknown kind: render a lossless summary-only card.
		return &ProposalCard{Kind: env.Kind, Summary: env.Summary}
	}
	var body proposalBlueprintBody
	if err := json.Unmarshal(env.Body, &body); err != nil {
		return nil
	}
	objectTypes := objectTypesFromMaps(body.ObjectTypes)
	relationshipTypes := relationshipTypesFromMaps(body.RelationshipTypes)
	if len(objectTypes) == 0 && len(relationshipTypes) == 0 {
		return nil
	}
	card := &ProposalCard{
		Kind:              env.Kind,
		Summary:           env.Summary,
		ObjectTypes:       objectTypes,
		RelationshipTypes: relationshipTypes,
	}
	// Diff against an empty current state: the stream transform has no project
	// context, so every proposal entry reads as an addition.
	for _, d := range diffObjectTypes(nil, objectTypes) {
		switch d.Change {
		case DiffChangeAdded:
			card.AddedObjectTypes++
		case DiffChangeChanged:
			card.ChangedObjectTypes++
		case DiffChangeRemoved:
			card.RemovedObjectTypes++
		}
	}
	for _, d := range diffRelationshipTypes(nil, relationshipTypes) {
		switch d.Change {
		case DiffChangeAdded:
			card.AddedRelationshipTypes++
		case DiffChangeChanged:
			card.ChangedRelationshipTypes++
		case DiffChangeRemoved:
			card.RemovedRelationshipTypes++
		}
	}
	return card
}

// renderProposalHTML renders a proposal card to sanitized HTML, or "" when the
// payload is absent/invalid (the question then keeps the markdown questionHtml
// path exactly as today).
func renderProposalHTML(raw json.RawMessage) string {
	card := buildProposalCard(raw)
	if card == nil {
		return ""
	}
	var buf bytes.Buffer
	if err := proposalCard(card).Render(context.Background(), &buf); err != nil {
		return ""
	}
	return buf.String()
}

// proposalSummaryLabel renders the card's one-line side-effect summary, e.g.
// "Adds 2 object types, 3 relationship types", or "" when nothing to report.
func proposalSummaryLabel(c *ProposalCard) string {
	var parts []string
	if c.AddedObjectTypes > 0 {
		parts = append(parts, countLabel(c.AddedObjectTypes, "object type", "object types"))
	}
	if c.AddedRelationshipTypes > 0 {
		parts = append(parts, countLabel(c.AddedRelationshipTypes, "relationship type", "relationship types"))
	}
	if c.ChangedObjectTypes > 0 {
		parts = append(parts, countLabel(c.ChangedObjectTypes, "changed object type", "changed object types"))
	}
	if c.ChangedRelationshipTypes > 0 {
		parts = append(parts, countLabel(c.ChangedRelationshipTypes, "changed relationship type", "changed relationship types"))
	}
	if c.RemovedObjectTypes > 0 {
		parts = append(parts, countLabel(c.RemovedObjectTypes, "removed object type", "removed object types"))
	}
	if c.RemovedRelationshipTypes > 0 {
		parts = append(parts, countLabel(c.RemovedRelationshipTypes, "removed relationship type", "removed relationship types"))
	}
	if len(parts) == 0 {
		return ""
	}
	return "Adds " + strings.Join(parts, ", ")
}
