package extraction

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"google.golang.org/adk/model"
	"google.golang.org/genai"
)

// EnrichSchemaProperties fills null/empty property maps in an existing object_type_schemas
// map by calling the LLM once with all sparse types batched together.
//
// For each type where properties == nil or {}: LLM generates 4-8 property definitions.
// Types that already have properties are passed through unchanged.
// Returns a new map (does not mutate existing).
func EnrichSchemaProperties(
	ctx context.Context,
	docText string,
	kbPurpose string,
	existing map[string]any,
	llm model.LLM,
) (map[string]any, error) {
	// Separate sparse types (need enrichment) from rich types (pass through).
	sparse := map[string]string{} // typeName -> description
	for typeName, raw := range existing {
		m, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		props, _ := m["properties"].(map[string]any)
		if len(props) == 0 {
			desc, _ := m["description"].(string)
			sparse[typeName] = desc
		}
	}

	if len(sparse) == 0 {
		// Nothing to enrich — return shallow copy.
		out := make(map[string]any, len(existing))
		for k, v := range existing {
			out[k] = v
		}
		return out, nil
	}

	// Build list for prompt.
	var typeList strings.Builder
	for typeName, desc := range sparse {
		if desc != "" {
			typeList.WriteString(fmt.Sprintf("- %s: %s\n", typeName, desc))
		} else {
			typeList.WriteString(fmt.Sprintf("- %s\n", typeName))
		}
	}

	excerpt := docText
	if len(excerpt) > 3000 {
		excerpt = excerpt[:3000]
	}

	prompt := fmt.Sprintf(`Project purpose: %s

Document excerpt:
%s

The following entity types were discovered from this document but have no property definitions:
%s
For EACH type above, generate a JSON Schema with 4-8 properties that could be extracted from this document.
Use snake_case field names. Each property must have "type" (string|number|boolean) and "description".
Also include a "description" for the type itself, and "required" (1-3 essential field names).

Return ONLY a JSON object where keys are type names:
{
  "TypeName": {
    "description": "...",
    "properties": {
      "field_name": {"type": "string", "description": "what to extract from text"}
    },
    "required": ["field_name"]
  }
}`, kbPurpose, excerpt, typeList.String())

	respText, err := callLLMJSON(ctx, llm, prompt)
	if err != nil {
		return nil, fmt.Errorf("EnrichSchemaProperties LLM call: %w", err)
	}

	// Parse response.
	clean := extractJSONObject(respText)
	var enriched map[string]map[string]any
	if err := json.Unmarshal([]byte(clean), &enriched); err != nil {
		return nil, fmt.Errorf("EnrichSchemaProperties parse: %w (raw: %s)", err, truncate(respText, 200))
	}

	// Build output: start from existing, overlay enriched types.
	out := make(map[string]any, len(existing))
	for k, v := range existing {
		out[k] = v
	}
	for typeName, enrichedDef := range enriched {
		if _, known := existing[typeName]; !known {
			continue // don't add types the LLM hallucinated
		}
		// Merge enriched definition into existing type entry.
		existing := map[string]any{
			"type":       "object",
			"required":   enrichedDef["required"],
			"properties": enrichedDef["properties"],
		}
		if desc, ok := enrichedDef["description"].(string); ok && desc != "" {
			existing["description"] = desc
		}
		out[typeName] = existing
	}
	return out, nil
}

// GenerateSchemaFromDocument creates a fresh object_type_schemas map with full
// property descriptions, derived from a document excerpt and project purpose.
// Used when no existing schema matches (new_domain path under schema_policy=enrich).
func GenerateSchemaFromDocument(
	ctx context.Context,
	docText string,
	kbPurpose string,
	llm model.LLM,
) (map[string]any, error) {
	excerpt := docText
	if len(excerpt) > 3000 {
		excerpt = excerpt[:3000]
	}

	prompt := fmt.Sprintf(`Project purpose: %s

Document excerpt:
%s

Identify 3-6 entity types (things with identity — people, places, events, objects) for this project.

IMPORTANT: Do NOT create a "Relationship" type. Bonds between entities are modelled as
typed graph edges (relationship_type_schemas), not as entity objects. Instead, model the
entities themselves: Character, Place, Event, Organisation, etc.

For each type provide:
- name: PascalCase, singular noun (e.g. Character, Event, Place)
- description: one sentence
- properties: 4-8 fields, each with "type" (string|number|boolean) and "description"
- required: 1-3 essential field names

Return ONLY JSON:
{
  "types": [
    {
      "name": "TypeName",
      "description": "...",
      "properties": {
        "field_name": {"type": "string", "description": "what to extract"}
      },
      "required": ["field_name"]
    }
  ]
}`, kbPurpose, excerpt)

	respText, err := callLLMJSON(ctx, llm, prompt)
	if err != nil {
		return nil, fmt.Errorf("GenerateSchemaFromDocument LLM call: %w", err)
	}

	type typesResult struct {
		Types []struct {
			Name        string         `json:"name"`
			Description string         `json:"description"`
			Properties  map[string]any `json:"properties"`
			Required    []string       `json:"required"`
		} `json:"types"`
	}

	clean := extractJSONObject(respText)
	var result typesResult
	if parseErr := json.Unmarshal([]byte(clean), &result); parseErr != nil || len(result.Types) == 0 {
		// Retry with a minimal prompt focused purely on JSON output.
		retryPrompt := fmt.Sprintf(
			`Project: %s. Return ONLY this JSON structure with 3-5 entity types for this document type:\n{"types":[{"name":"TypeName","description":"one sentence","properties":{"field":{"type":"string","description":"what to extract"}},"required":["field"]}]}`,
			kbPurpose)
		retryText, retryErr := callLLMJSON(ctx, llm, retryPrompt)
		if retryErr == nil {
			retryClean := extractJSONObject(retryText)
			var retryResult typesResult
			if json.Unmarshal([]byte(retryClean), &retryResult) == nil && len(retryResult.Types) > 0 {
				result = retryResult
			}
		}
		if len(result.Types) == 0 {
			return nil, fmt.Errorf("GenerateSchemaFromDocument parse: %w (raw: %s)", parseErr, truncate(respText, 200))
		}
	}

	out := make(map[string]any, len(result.Types))
	for _, t := range result.Types {
		if t.Name == "" || isRelationshipObjectType(t.Name) {
			continue // bonds belong in relationship_type_schemas, not object_type_schemas
		}
		entry := map[string]any{
			"type":       "object",
			"required":   t.Required,
			"properties": t.Properties,
		}
		if t.Description != "" {
			entry["description"] = t.Description
		}
		out[t.Name] = entry
	}
	return out, nil
}

// isRelationshipObjectType returns true for type names that should be modelled
// as graph edges rather than entity objects.
func isRelationshipObjectType(name string) bool {
	lower := strings.ToLower(name)
	return lower == "relationship" || lower == "bond" || lower == "connection" ||
		lower == "link" || lower == "association" || lower == "relation"
}

// GeneratedSchemaResult holds both object and relationship type schemas produced
// in a single or sequential generation pass.
type GeneratedSchemaResult struct {
	// ObjectTypes maps typeName → JSON Schema object (object_type_schemas JSONB format).
	ObjectTypes map[string]any
	// RelationshipTypes maps relTypeName → relationship schema (relationship_type_schemas JSONB format).
	// Includes inverseType / inverseLabel fields consumed by InverseTypeProvider.
	RelationshipTypes map[string]any
}

// GenerateSchemaWithRelationships generates object types AND relationship types in ONE
// LLM call (Variant A — combined). Returns a GeneratedSchemaResult with both maps.
// The combined prompt gives the LLM full context so relationship types reference actual
// object type names and don't need a second round-trip.
func GenerateSchemaWithRelationships(
	ctx context.Context,
	docText string,
	kbPurpose string,
	llm model.LLM,
) (*GeneratedSchemaResult, error) {
	excerpt := docText
	if len(excerpt) > 3000 {
		excerpt = excerpt[:3000]
	}

	prompt := fmt.Sprintf(`Project purpose: %s

Document excerpt:
%s

Define a complete schema for extracting knowledge from this document.

PART 1 — Entity types (3-6 types):
Model THINGS with identity — people, places, events, organisations, objects.
Do NOT create a "Relationship" type — bonds between entities go in Part 2 as typed graph edges.
For each type:
- name: PascalCase singular noun (Character, Event, Place — not "Relationship")
- description: one sentence
- properties: 4-8 fields, each {"type": "string|number|boolean", "description": "what to extract"}
- required: 1-3 essential field names

PART 2 — Graph edge types (3-8 types):
These are typed directed edges between entity nodes. They replace any "Relationship" object type.
Include bond types (KNOWS, SIBLING_OF, EX_SPOUSE_OF, FRIEND_OF) AND structural edges (LIVES_AT, INVOLVED_IN, OWNS).
For each edge type:
- name: UPPER_SNAKE_CASE verb (KNOWS, SIBLING_OF, LIVES_AT, INVOLVED_IN)
- sourceType: entity type from Part 1
- targetType: entity type from Part 1
- cardinality: "one-to-one" | "one-to-many" | "many-to-many"
- description: one sentence — when to create this edge
- properties: 0-3 edge-level attributes (e.g. since, current_state, how_met) with type+description
- inverseType: UPPER_SNAKE_CASE reverse (e.g. SIBLING_OF → SIBLING_OF, LIVES_AT → HOME_OF)
- inverseLabel: human-readable reverse label ("sibling of", "home of")

Return ONLY JSON:
{
  "types": [
    {
      "name": "TypeName",
      "description": "...",
      "properties": {"field": {"type": "string", "description": "..."}},
      "required": ["field"]
    }
  ],
  "relationships": [
    {
      "name": "REL_TYPE",
      "sourceType": "TypeA",
      "targetType": "TypeB",
      "cardinality": "many-to-many",
      "description": "...",
      "properties": {},
      "inverseType": "INVERSE_REL",
      "inverseLabel": "inverse label"
    }
  ]
}`, kbPurpose, excerpt)

	respText, err := callLLMJSON(ctx, llm, prompt)
	if err != nil {
		return nil, fmt.Errorf("GenerateSchemaWithRelationships LLM call: %w", err)
	}

	clean := extractJSONObject(respText)
	var raw struct {
		Types []struct {
			Name        string         `json:"name"`
			Description string         `json:"description"`
			Properties  map[string]any `json:"properties"`
			Required    []string       `json:"required"`
		} `json:"types"`
		Relationships []struct {
			Name         string         `json:"name"`
			SourceType   string         `json:"sourceType"`
			TargetType   string         `json:"targetType"`
			Cardinality  string         `json:"cardinality"`
			Description  string         `json:"description"`
			Properties   map[string]any `json:"properties"`
			InverseType  string         `json:"inverseType"`
			InverseLabel string         `json:"inverseLabel"`
		} `json:"relationships"`
	}
	if err := json.Unmarshal([]byte(clean), &raw); err != nil {
		return nil, fmt.Errorf("GenerateSchemaWithRelationships parse: %w (raw: %s)", err, truncate(respText, 200))
	}

	result := &GeneratedSchemaResult{
		ObjectTypes:       make(map[string]any, len(raw.Types)),
		RelationshipTypes: make(map[string]any, len(raw.Relationships)),
	}

	for _, t := range raw.Types {
		if t.Name == "" || isRelationshipObjectType(t.Name) {
			continue // bonds belong in relationship_type_schemas, not object_type_schemas
		}
		entry := map[string]any{
			"type":       "object",
			"required":   t.Required,
			"properties": t.Properties,
		}
		if t.Description != "" {
			entry["description"] = t.Description
		}
		result.ObjectTypes[t.Name] = entry
	}

	for _, r := range raw.Relationships {
		if r.Name == "" {
			continue
		}
		entry := map[string]any{
			"sourceTypes": []string{r.SourceType},
			"targetTypes": []string{r.TargetType},
			"cardinality": r.Cardinality,
			"description": r.Description,
		}
		if len(r.Properties) > 0 {
			entry["properties"] = r.Properties
		}
		if r.InverseType != "" {
			entry["inverseType"] = r.InverseType
		}
		if r.InverseLabel != "" {
			entry["inverseLabel"] = r.InverseLabel
		}
		result.RelationshipTypes[r.Name] = entry
	}

	return result, nil
}

// GenerateRelationshipsFromSchema generates graph relationship types in a SECOND LLM call,
// using the already-known object type names as anchors (Variant B — sequential).
// The prompt is tighter than the combined call because the LLM only needs to think about
// edges, not simultaneously design entity properties.
func GenerateRelationshipsFromSchema(
	ctx context.Context,
	docText string,
	kbPurpose string,
	objectTypeNames []string,
	llm model.LLM,
) (map[string]any, error) {
	excerpt := docText
	if len(excerpt) > 2000 {
		excerpt = excerpt[:2000]
	}

	typeList := strings.Join(objectTypeNames, ", ")

	prompt := fmt.Sprintf(`Project purpose: %s

Known entity types: %s

Document excerpt:
%s

Generate 3-8 typed graph EDGE types between the known entity types.
These are directed edges (not object nodes) — they replace any "Relationship" object type.
Include both bond edges (KNOWS, SIBLING_OF, EX_SPOUSE_OF, FRIEND_OF) and structural edges (LIVES_AT, INVOLVED_IN).
Focus on what the document actually reveals.

For each edge type:
- name: UPPER_SNAKE_CASE verb (KNOWS, SIBLING_OF, LIVES_AT, INVOLVED_IN)
- sourceType: one of the known entity types
- targetType: one of the known entity types
- cardinality: "one-to-one" | "one-to-many" | "many-to-many"
- description: one sentence — when to create this edge
- properties: 0-3 edge-level attributes with type+description (e.g. since: {"type":"string","description":"when they met"}, current_state: {"type":"string","description":"current status of the bond"})
- inverseType: UPPER_SNAKE_CASE reverse name (SIBLING_OF stays SIBLING_OF; LIVES_AT → HOME_OF)
- inverseLabel: short human-readable reverse label ("sibling of", "home of", "known by")

Return ONLY JSON:
{
  "relationships": [
    {
      "name": "REL_TYPE",
      "sourceType": "TypeA",
      "targetType": "TypeB",
      "cardinality": "many-to-many",
      "description": "...",
      "properties": {},
      "inverseType": "INVERSE_REL",
      "inverseLabel": "inverse label"
    }
  ]
}`, kbPurpose, typeList, excerpt)

	respText, err := callLLMJSON(ctx, llm, prompt)
	if err != nil {
		return nil, fmt.Errorf("GenerateRelationshipsFromSchema LLM call: %w", err)
	}

	type relRaw struct {
		Relationships []struct {
			Name         string         `json:"name"`
			SourceType   string         `json:"sourceType"`
			TargetType   string         `json:"targetType"`
			Cardinality  string         `json:"cardinality"`
			Description  string         `json:"description"`
			Properties   map[string]any `json:"properties"`
			InverseType  string         `json:"inverseType"`
			InverseLabel string         `json:"inverseLabel"`
		} `json:"relationships"`
	}

	clean := extractJSONObject(respText)
	var raw relRaw
	if parseErr := json.Unmarshal([]byte(clean), &raw); parseErr != nil || len(raw.Relationships) == 0 {
		// Retry with a minimal, ultra-constrained prompt.
		retryPrompt := fmt.Sprintf(
			`Entity types: %s. Return ONLY this JSON, no explanation: {"relationships":[{"name":"KNOWS","sourceType":"%s","targetType":"%s","cardinality":"many-to-many","description":"entities that know each other","inverseType":"KNOWN_BY","inverseLabel":"known by"}]}`,
			typeList, objectTypeNames[0], objectTypeNames[0])
		if len(objectTypeNames) > 1 {
			retryPrompt = fmt.Sprintf(
				`Entity types: %s.\nGenerate 3-5 UPPER_SNAKE_CASE relationship types between them.\nReturn ONLY JSON: {"relationships":[{"name":"REL","sourceType":"A","targetType":"B","cardinality":"many-to-many","description":"...","inverseType":"INV","inverseLabel":"inv label"}]}`,
				typeList)
		}
		retryText, retryErr := callLLMJSON(ctx, llm, retryPrompt)
		if retryErr == nil {
			retryClean := extractJSONObject(retryText)
			var retryRaw relRaw
			if json.Unmarshal([]byte(retryClean), &retryRaw) == nil {
				raw = retryRaw
			}
		}
		if len(raw.Relationships) == 0 {
			return nil, fmt.Errorf("GenerateRelationshipsFromSchema parse: %w (raw: %s)", parseErr, truncate(respText, 200))
		}
	}

	out := make(map[string]any, len(raw.Relationships))
	for _, r := range raw.Relationships {
		if r.Name == "" {
			continue
		}
		entry := map[string]any{
			"sourceTypes": []string{r.SourceType},
			"targetTypes": []string{r.TargetType},
			"cardinality": r.Cardinality,
			"description": r.Description,
		}
		if len(r.Properties) > 0 {
			entry["properties"] = r.Properties
		}
		if r.InverseType != "" {
			entry["inverseType"] = r.InverseType
		}
		if r.InverseLabel != "" {
			entry["inverseLabel"] = r.InverseLabel
		}
		out[r.Name] = entry
	}
	return out, nil
}

// callLLM makes a single non-streaming LLM call and returns the full text response.
func callLLM(ctx context.Context, llm model.LLM, prompt string) (string, error) {
	return callLLMWithConfig(ctx, llm, prompt, 2048)
}

// callLLMJSON is like callLLM but uses a larger token budget for JSON generation.
// The improved extractJSONObject handles deepseek-style prose preambles, so no
// special system injection is needed.
func callLLMJSON(ctx context.Context, llm model.LLM, prompt string) (string, error) {
	return callLLMWithConfig(ctx, llm, prompt, 4096)
}

func callLLMWithConfig(ctx context.Context, llm model.LLM, prompt string, maxTokens int32) (string, error) {
	req := &model.LLMRequest{
		Contents: []*genai.Content{
			{Role: "user", Parts: []*genai.Part{{Text: prompt}}},
		},
		Config: &genai.GenerateContentConfig{
			MaxOutputTokens: maxTokens,
		},
	}
	var sb strings.Builder
	var firstErr error
	for resp, err := range llm.GenerateContent(ctx, req, false) {
		if err != nil && firstErr == nil {
			firstErr = err
		}
		if resp != nil && resp.Content != nil {
			for _, part := range resp.Content.Parts {
				sb.WriteString(part.Text)
			}
		}
	}
	if firstErr != nil {
		return "", firstErr
	}
	return sb.String(), nil
}

// extractJSONObject strips markdown fences and prose preamble, returning the first
// complete JSON object found in s. Handles deepseek-style "We need to..." reasoning
// prefixes by scanning for the first '{' that begins a valid JSON object.
func extractJSONObject(s string) string {
	s = strings.TrimSpace(s)
	// Strip ```json ... ``` fences.
	if after, ok := strings.CutPrefix(s, "```json"); ok {
		if idx := strings.LastIndex(after, "```"); idx >= 0 {
			s = strings.TrimSpace(after[:idx])
		}
	} else if after, ok := strings.CutPrefix(s, "```"); ok {
		if idx := strings.LastIndex(after, "```"); idx >= 0 {
			s = strings.TrimSpace(after[:idx])
		}
	}
	s = strings.TrimSpace(s)

	// Scan for the first '{' that starts a valid JSON object.
	// This handles prose preambles like "We need to generate...\n{...}".
	for i := 0; i < len(s); i++ {
		if s[i] != '{' {
			continue
		}
		candidate := s[i:]
		// Quick check: find matching closing brace by counting depth.
		depth := 0
		inStr := false
		escape := false
		end := -1
		for j, ch := range candidate {
			if escape {
				escape = false
				continue
			}
			if ch == '\\' && inStr {
				escape = true
				continue
			}
			if ch == '"' {
				inStr = !inStr
				continue
			}
			if inStr {
				continue
			}
			if ch == '{' {
				depth++
			} else if ch == '}' {
				depth--
				if depth == 0 {
					end = j + 1
					break
				}
			}
		}
		if end > 0 {
			return candidate[:end]
		}
	}
	return s
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}
