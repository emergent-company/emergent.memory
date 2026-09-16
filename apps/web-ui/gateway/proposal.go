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

// ProposalCard is the render model for a proposal card: a side-effect summary
// (diff counts vs the project's current types, always additions here) plus
// read-only object/relationship previews and the proposal's own summary/kind
// for scope.
type ProposalCard struct {
	Kind    string
	Summary string

	ObjectTypes       []ObjectTypeDetail
	RelationshipTypes []RelationshipTypeDetail

	AddedObjectTypes       int
	AddedRelationshipTypes int
}

// buildProposalCard parses a raw proposal envelope into the card model, or
// returns nil when the payload is absent, malformed, or carries no renderable
// content (invalid proposals degrade to the plain markdown path).
func buildProposalCard(raw json.RawMessage) *ProposalCard {
	// Mirror the server's parseProposal envelope validation: require a
	// non-empty kind, a non-empty summary, and an object body before rendering
	// any card — a proposal the server dropped must never render here.
	var env proposalEnvelope
	if err := json.Unmarshal(raw, &env); err != nil || env.Kind == "" || env.Summary == "" {
		return nil
	}
	var body map[string]any
	if err := json.Unmarshal(env.Body, &body); err != nil || body == nil {
		return nil
	}
	if env.Kind != "blueprint" {
		// Unknown kind: render a lossless summary-only card.
		return &ProposalCard{Kind: env.Kind, Summary: env.Summary}
	}
	objectTypes := objectTypesFromRaw(bodyField(body, "objectTypes"))
	relationshipTypes := relationshipTypesFromRaw(bodyField(body, "relationshipTypes"))
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
		if d.Change == DiffChangeAdded {
			card.AddedObjectTypes++
		}
	}
	for _, d := range diffRelationshipTypes(nil, relationshipTypes) {
		if d.Change == DiffChangeAdded {
			card.AddedRelationshipTypes++
		}
	}
	return card
}

// bodyField re-marshals one proposal body field back to raw JSON so the
// array-or-map tolerant parsers in blueprints.go apply unchanged.
func bodyField(body map[string]any, key string) json.RawMessage {
	v, ok := body[key]
	if !ok || v == nil {
		return nil
	}
	b, err := json.Marshal(v)
	if err != nil {
		return nil
	}
	return b
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
// The diff base is always nil here, so only additions are possible.
func proposalSummaryLabel(c *ProposalCard) string {
	var parts []string
	if c.AddedObjectTypes > 0 {
		parts = append(parts, countLabel(c.AddedObjectTypes, "object type", "object types"))
	}
	if c.AddedRelationshipTypes > 0 {
		parts = append(parts, countLabel(c.AddedRelationshipTypes, "relationship type", "relationship types"))
	}
	if len(parts) == 0 {
		return ""
	}
	return "Adds " + strings.Join(parts, ", ")
}
