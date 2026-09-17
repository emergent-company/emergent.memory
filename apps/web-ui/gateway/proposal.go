package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"gopkg.in/yaml.v3"
)

// proposalEnvelope is the optional `proposal` argument an agent attaches to
// ask_user: a kind discriminator, a human-readable summary, and a structured
// body. Body stays raw — the gateway resolves `kind` through proposalKindBuilders
// and renders a kind-specific card; unknown kinds degrade to a summary-only
// card, never an error.
type proposalEnvelope struct {
	Kind    string          `json:"kind"`
	Summary string          `json:"summary"`
	Body    json.RawMessage `json:"body"`
}

// ProposalCard is the render model for a proposal card: a side-effect summary
// (diff counts vs the project's current types, always additions here) plus a
// kind-specific read-only preview and the proposal's own summary/kind for scope.
// Exactly one kind section is populated per card, selected by Kind.
type ProposalCard struct {
	Kind    string
	Summary string

	// blueprint (schema pack)
	ObjectTypes       []ObjectTypeDetail
	RelationshipTypes []RelationshipTypeDetail

	AddedObjectTypes       int
	AddedRelationshipTypes int

	// skill
	Skill *SkillProposal

	// agent
	Agent *AgentProposal

	// mcp_server
	MCPServer *MCPServerProposal

	// provider
	Provider *ProviderProposal

	// object (graph entities)
	Entities      []EntityProposal
	Relationships []ObjectRelationshipProposal
}

// SkillProposal is the render model for a `skill` proposal.
type SkillProposal struct {
	Name        string
	Description string
	Prompt      string
	Tools       []string
	BannedTools []string
}

// AgentProposal is the render model for an `agent` proposal. Field names mirror
// BundledAgent so the body reuses the agent-definition shape the apply path
// consumes.
type AgentProposal struct {
	Name         string
	Description  string
	Model        string
	SystemPrompt string
	Tools        []string
	Skills       []string
	BannedTools  []string
	FlowType     string
	Visibility   string
}

// MCPServerProposal is the render model for an `mcp_server` proposal. Auth
// headers are never carried — the body's `headers` map commonly holds
// credentials, so it is deliberately dropped from the card and only the name,
// type, URL, and tool enable/disable lists are rendered.
type MCPServerProposal struct {
	Name          string
	Type          string
	URL           string
	Enabled       bool
	EnabledTools  []string
	DisabledTools []string
}

// ProviderProposal is the render model for a `provider` proposal. The API key
// is never carried — only the slug, base URL, and models are rendered.
type ProviderProposal struct {
	Provider string
	BaseURL  string
	Models   []string
}

// EntityProposal is one proposed graph entity (type + key + stringified
// properties + tags).
type EntityProposal struct {
	Type       string
	Key        string
	Properties map[string]string
	Tags       []string
}

// ObjectRelationshipProposal is one proposed graph relationship.
type ObjectRelationshipProposal struct {
	Type   string
	Source string
	Target string
}

// proposalKindBuilder parses a single kind's body into a ProposalCard, returning
// nil when the body carries no renderable content (degrade to plain markdown).
type proposalKindBuilder func(kind, summary string, body map[string]any) *ProposalCard

// proposalKindBuilders maps a proposal `kind` to its builder. A kind absent
// from this registry renders as a summary-only card.
var proposalKindBuilders = map[string]proposalKindBuilder{
	"blueprint":  buildBlueprintCard,
	"skill":      buildSkillCard,
	"agent":      buildAgentCard,
	"mcp_server": buildMCPServerCard,
	"provider":   buildProviderCard,
	"object":     buildObjectCard,
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
	if build, ok := proposalKindBuilders[env.Kind]; ok {
		return build(env.Kind, env.Summary, body)
	}
	// Unknown kind: render a lossless summary-only card.
	return &ProposalCard{Kind: env.Kind, Summary: env.Summary}
}

// buildBlueprintCard renders a schema-pack proposal: object/relationship type
// previews with an additions-only diff summary (the stream transform has no
// project context, so every entry reads as an addition).
func buildBlueprintCard(kind, summary string, body map[string]any) *ProposalCard {
	objectTypes := objectTypesFromRaw(bodyField(body, "objectTypes"))
	relationshipTypes := relationshipTypesFromRaw(bodyField(body, "relationshipTypes"))
	if len(objectTypes) == 0 && len(relationshipTypes) == 0 {
		return nil
	}
	card := &ProposalCard{
		Kind:              kind,
		Summary:           summary,
		ObjectTypes:       objectTypes,
		RelationshipTypes: relationshipTypes,
	}
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

// buildSkillCard renders a `skill` proposal. A skill without a name is not
// renderable.
func buildSkillCard(kind, summary string, body map[string]any) *ProposalCard {
	name := strAny(body["name"])
	if name == "" {
		return nil
	}
	return &ProposalCard{
		Kind:    kind,
		Summary: summary,
		Skill: &SkillProposal{
			Name:        name,
			Description: strAny(body["description"]),
			Prompt:      strAny(body["prompt"]),
			Tools:       strSlice(body["tools"]),
			BannedTools: strSlice(body["bannedTools"]),
		},
	}
}

// buildAgentCard renders an `agent` proposal. An agent without a name is not
// renderable.
func buildAgentCard(kind, summary string, body map[string]any) *ProposalCard {
	name := strAny(body["name"])
	if name == "" {
		return nil
	}
	return &ProposalCard{
		Kind:    kind,
		Summary: summary,
		Agent: &AgentProposal{
			Name:         name,
			Description:  strAny(body["description"]),
			Model:        strAny(body["model"]),
			SystemPrompt: strAny(body["systemPrompt"]),
			Tools:        strSlice(body["tools"]),
			Skills:       strSlice(body["skills"]),
			BannedTools:  strSlice(body["bannedTools"]),
			FlowType:     strAny(body["flowType"]),
			Visibility:   strAny(body["visibility"]),
		},
	}
}

// buildMCPServerCard renders an `mcp_server` proposal. The `headers` field is
// deliberately ignored (auth headers carry credentials, so any headers/header
// field is never read and can't leak into the card). A server without a name
// is not renderable.
func buildMCPServerCard(kind, summary string, body map[string]any) *ProposalCard {
	name := strAny(body["name"])
	if name == "" {
		return nil
	}
	return &ProposalCard{
		Kind:    kind,
		Summary: summary,
		MCPServer: &MCPServerProposal{
			Name:          name,
			Type:          strAny(body["type"]),
			URL:           strAny(body["url"]),
			Enabled:       boolAny(body["enabled"]),
			EnabledTools:  strSlice(body["enabledTools"]),
			DisabledTools: strSlice(body["disabledTools"]),
		},
	}
}

// buildProviderCard renders a `provider` proposal. The API key is deliberately
// ignored (any apiKey/api_key field is never read, so it can't leak into the
// card). A provider without a slug is not renderable.
func buildProviderCard(kind, summary string, body map[string]any) *ProposalCard {
	provider := strAny(body["provider"])
	if provider == "" {
		return nil
	}
	baseURL := strAny(body["baseUrl"])
	if baseURL == "" {
		baseURL = strAny(body["base_url"])
	}
	return &ProposalCard{
		Kind:    kind,
		Summary: summary,
		Provider: &ProviderProposal{
			Provider: provider,
			BaseURL:  baseURL,
			Models:   strSlice(body["models"]),
		},
	}
}

// buildObjectCard renders an `object` (graph entities) proposal. Entities and
// relationships are previewed read-only; with neither present the body is not
// renderable.
func buildObjectCard(kind, summary string, body map[string]any) *ProposalCard {
	entities := parseEntities(body["entities"])
	relationships := parseObjectRelationships(body["relationships"])
	if len(entities) == 0 && len(relationships) == 0 {
		return nil
	}
	return &ProposalCard{
		Kind:          kind,
		Summary:       summary,
		Entities:      entities,
		Relationships: relationships,
	}
}

// parseEntities decodes a proposal's entities array into preview rows. Each
// entity is {type, key, properties, tags}; properties are stringified for
// display. Malformed entries are skipped.
func parseEntities(v any) []EntityProposal {
	list, ok := v.([]any)
	if !ok {
		return nil
	}
	out := make([]EntityProposal, 0, len(list))
	for _, item := range list {
		m, ok := item.(map[string]any)
		if !ok {
			continue
		}
		e := EntityProposal{
			Type:       strAny(m["type"]),
			Key:        strAny(m["key"]),
			Properties: stringMap(m["properties"]),
			Tags:       strSlice(m["tags"]),
		}
		if e.Type == "" && e.Key == "" {
			continue
		}
		out = append(out, e)
	}
	return out
}

// parseObjectRelationships decodes a proposal's relationships array into preview
// rows. Each relationship is {type, source, target} with sourceKey/targetKey and
// srcKey/dstKey tolerated as fallbacks. Malformed entries are skipped.
func parseObjectRelationships(v any) []ObjectRelationshipProposal {
	list, ok := v.([]any)
	if !ok {
		return nil
	}
	out := make([]ObjectRelationshipProposal, 0, len(list))
	for _, item := range list {
		m, ok := item.(map[string]any)
		if !ok {
			continue
		}
		source := strAny(m["source"])
		if source == "" {
			source = firstNonEmpty(strAny(m["sourceKey"]), strAny(m["srcKey"]))
		}
		target := strAny(m["target"])
		if target == "" {
			target = firstNonEmpty(strAny(m["targetKey"]), strAny(m["dstKey"]))
		}
		if source == "" && target == "" {
			continue
		}
		out = append(out, ObjectRelationshipProposal{
			Type:   strAny(m["type"]),
			Source: source,
			Target: target,
		})
	}
	return out
}

// strSlice decodes a value that is either a []string or a []any of strings into
// a []string; anything else yields nil.
func strSlice(v any) []string {
	switch t := v.(type) {
	case []string:
		return t
	case []any:
		out := make([]string, 0, len(t))
		for _, item := range t {
			if s := strAny(item); s != "" {
				out = append(out, s)
			}
		}
		return out
	default:
		return nil
	}
}

// stringMap stringifies a map[string]any of property values for display. Returns
// nil when the value is absent or not a map.
func stringMap(v any) map[string]string {
	m, ok := v.(map[string]any)
	if !ok || len(m) == 0 {
		return nil
	}
	out := make(map[string]string, len(m))
	for k, val := range m {
		out[k] = stringifyValue(val)
	}
	return out
}

// stringifyValue renders a scalar property value as a string: strings pass
// through, everything else is JSON-encoded (nil → "").
func stringifyValue(v any) string {
	if v == nil {
		return ""
	}
	if s, ok := v.(string); ok {
		return s
	}
	b, err := json.Marshal(v)
	if err != nil {
		return ""
	}
	return string(b)
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
	return renderProposalCardHTML(buildProposalCard(raw))
}

// renderProposalCardHTML renders a ProposalCard to sanitized HTML, or "" for a
// nil card (or a render failure). renderProposalHTML routes through this so the
// structured envelope and the fence fallback share one render path.
func renderProposalCardHTML(card *ProposalCard) string {
	if card == nil {
		return ""
	}
	var buf bytes.Buffer
	if err := proposalCard(card).Render(context.Background(), &buf); err != nil {
		return ""
	}
	return buf.String()
}

// fallbackProposalHTML renders the proposal HTML for an ask_user input: the
// structured proposal when present and valid, otherwise a card derived from a
// fenced manifest in the question text when one is derivable, otherwise ""
// (plain markdown, unchanged from today).
func fallbackProposalHTML(proposal json.RawMessage, question string) string {
	if html := renderProposalHTML(proposal); html != "" {
		return html
	}
	return renderProposalCardHTML(proposalFromQuestionText(question))
}

// proposalFromQuestionText derives a proposal card from a fenced blueprint
// manifest embedded in an ask_user question, for the legacy case where no
// structured `proposal` is attached. It recognizes the first json/yaml/yml
// fence whose content decodes to a blueprint manifest (a `packs` array or a
// bare pack object); anything else yields nil so the question keeps its
// markdown path. The card is built through the same blueprint builder the
// structured envelope uses, so the two render visually identically.
func proposalFromQuestionText(question string) *ProposalCard {
	lang, body := firstManifestFence(question)
	if body == "" {
		return nil
	}
	var manifest map[string]any
	switch lang {
	case "json":
		if err := json.Unmarshal([]byte(body), &manifest); err != nil {
			return nil
		}
	case "yaml", "yml":
		if err := yaml.Unmarshal([]byte(body), &manifest); err != nil {
			return nil
		}
	}
	if manifest == nil {
		return nil
	}
	cardBody := blueprintManifestBody(manifest)
	if cardBody == nil {
		return nil
	}
	return buildBlueprintCard("blueprint", blueprintManifestSummary(manifest), cardBody)
}

// firstManifestFence returns the language and content of the first fenced code
// block in question whose info string is json, yaml, or yml. Bare fences (no
// info string) and other languages are skipped to limit false positives; an
// unterminated fence is ignored.
func firstManifestFence(question string) (lang, body string) {
	lines := strings.Split(question, "\n")
	for i := range lines {
		info, ok := strings.CutPrefix(strings.TrimSpace(lines[i]), "```")
		if !ok {
			continue
		}
		info = strings.TrimSpace(info)
		if info != "json" && info != "yaml" && info != "yml" {
			continue
		}
		var b strings.Builder
		closed := false
		for j := i + 1; j < len(lines); j++ {
			if strings.HasPrefix(strings.TrimSpace(lines[j]), "```") {
				closed = true
				break
			}
			if b.Len() > 0 {
				b.WriteByte('\n')
			}
			b.WriteString(lines[j])
		}
		if !closed {
			continue
		}
		return info, b.String()
	}
	return "", ""
}

// blueprintManifestBody normalizes a decoded blueprint manifest into the
// {objectTypes, relationshipTypes} body shape the blueprint card builder
// consumes, merging the type arrays across every pack. It returns nil when the
// manifest carries no object or relationship types.
func blueprintManifestBody(manifest map[string]any) map[string]any {
	var packs []any
	if p, ok := manifest["packs"].([]any); ok {
		packs = p
	} else if manifest["objectTypes"] != nil || manifest["relationshipTypes"] != nil {
		// Bare pack object: top-level objectTypes/relationshipTypes.
		packs = []any{manifest}
	} else {
		return nil
	}
	var objectTypes, relationshipTypes []any
	for _, p := range packs {
		pm, ok := p.(map[string]any)
		if !ok {
			continue
		}
		objectTypes = appendManifestItems(objectTypes, pm["objectTypes"])
		relationshipTypes = appendManifestItems(relationshipTypes, pm["relationshipTypes"])
	}
	if len(objectTypes) == 0 && len(relationshipTypes) == 0 {
		return nil
	}
	body := make(map[string]any, 2)
	if len(objectTypes) > 0 {
		body["objectTypes"] = objectTypes
	}
	if len(relationshipTypes) > 0 {
		body["relationshipTypes"] = relationshipTypes
	}
	return body
}

// appendManifestItems appends the elements of v — a JSON/YAML-decoded array of
// type maps — to dst, tolerating both []any and []map[string]any forms.
func appendManifestItems(dst []any, v any) []any {
	switch t := v.(type) {
	case []any:
		return append(dst, t...)
	case []map[string]any:
		for _, m := range t {
			dst = append(dst, m)
		}
	}
	return dst
}

// blueprintManifestSummary builds a human summary line for a decoded blueprint
// manifest: the first pack's name + version, then a truncated description when
// present, and a "+N more packs" note for multi-pack manifests. No type counts —
// the card header chip reports those.
func blueprintManifestSummary(manifest map[string]any) string {
	name, version, description := "", "", ""
	extraPacks := 0
	if packs, ok := manifest["packs"].([]any); ok {
		if len(packs) == 0 {
			return ""
		}
		if first, ok := packs[0].(map[string]any); ok {
			name, version, description = strAny(first["name"]), strAny(first["version"]), strAny(first["description"])
		}
		if len(packs) > 1 {
			extraPacks = len(packs) - 1
		}
	} else {
		name, version, description = strAny(manifest["name"]), strAny(manifest["version"]), strAny(manifest["description"])
	}
	if name == "" {
		if description != "" {
			return truncateRunes(description, 80)
		}
		return ""
	}
	var b strings.Builder
	b.WriteString(name)
	if version != "" {
		b.WriteString(" ")
		b.WriteString(version)
	}
	if description != "" {
		b.WriteString(" — ")
		b.WriteString(truncateRunes(description, 80))
	}
	if extraPacks > 0 {
		fmt.Fprintf(&b, " (+%d more packs)", extraPacks)
	}
	return b.String()
}

// proposalKindIcon returns the lucide icon for a proposal kind's badge.
func proposalKindIcon(kind string) string {
	switch kind {
	case "skill":
		return "lucide--wand"
	case "agent":
		return "lucide--bot"
	case "mcp_server":
		return "lucide--server"
	case "provider":
		return "lucide--cloud"
	case "object":
		return "lucide--boxes"
	default:
		return "lucide--library"
	}
}

// proposalSummaryLabel renders the card's one-line side-effect summary, e.g.
// "Adds 2 object types, 3 relationship types", or "" when nothing to report.
// Only the blueprint kind carries diff counts today; other kinds rely on the
// envelope summary.
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
