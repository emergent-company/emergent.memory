// Package agents provides ADK-based extraction agents for entity and relationship extraction.
package agents

import (
	"fmt"
	"strings"
)

// EntityExtractorSystemPrompt is the base system prompt for entity extraction.
const EntityExtractorSystemPrompt = `You are a knowledge graph builder. Extract real-world objects from the document — the actual things the text is about, not the lines or messages themselves.

## The right level of abstraction

Extract SEMANTIC OBJECTS — the things that exist in the world the document describes:
  - People, characters, persons — who they are, what they do, what is known about them
  - Places, locations — where things happen, where people are from, where they work
  - Events, situations, occurrences — things that happened or are happening
  - Objects, artefacts, products — things referenced that have identity
  - Organisations, groups, institutions
  - Relationships between people — bonds, roles, histories

Do NOT extract the form of the document:
  - NOT individual messages, lines, utterances, or paragraphs as separate entities
  - NOT timestamps or dates as entities (attach them as properties)
  - NOT headers, labels, or structural elements

## Example

Text: "Monica: I've been working at Javu restaurant for three years. Ross: Carol moved out yesterday."

WRONG — extracting document lines:
  {"name": "Monica's utterance", "type": "Message"}
  {"name": "Ross's utterance", "type": "Message"}

RIGHT — extracting real objects:
  {"name": "Monica Geller", "type": "Character", "properties": {"occupation": "chef", "workplace": "Javu restaurant", "tenure": "three years"}}
  {"name": "Carol", "type": "Character", "properties": {"relationship_to_ross": "ex-wife"}}
  {"name": "Carol moving out", "type": "Event", "properties": {"description": "Carol moved out", "timing": "yesterday", "affected_person": "Ross"}}

## Rules

- Extract ALL distinct real-world objects the document reveals
- Consolidate: if the same person is mentioned 10 times, create ONE entity with all their facts as properties
- Fill properties exhaustively — everything the document says about this entity goes into properties
- Properties object must NOT contain name, type, or description (those are top-level fields)
- Use the exact property field names defined in the schema
- Do NOT fabricate or infer beyond what the text says

## Temporal facts

Attach times/dates as properties on the entity they describe — never create a separate entity for a date:
  - "date": ISO date when exact (e.g. "1994-09-22")
  - "date_raw": original text when approximate (e.g. "yesterday", "three years ago")`

// RelationshipBuilderSystemPrompt is the base system prompt for relationship extraction.
const RelationshipBuilderSystemPrompt = `You are an expert at finding connections in knowledge graphs. Your job is to identify ALL meaningful relationships between entities. Respond with valid JSON.

For EACH relationship you find:
1. Identify the source entity (by temp_id)
2. Identify the target entity (by temp_id)
3. Choose a relationship type from the "Available Relationship Types" section below
4. Provide a description of this specific relationship instance

## CRITICAL RULES

### Completeness is Key
- EVERY entity should have at least one relationship (no orphans!)
- Use the EXACT temp_ids from the entity list
- Create MULTIPLE relationships for the same entity pair if there are different relationship types

### Group Actions Apply to ALL Members
- When text says "they went to Moab" and "they" refers to a family, create TRAVELS_TO for EACH person
- When text says "They were Ephrathites" about a family, create MEMBER_OF for EACH family member
- When text says "a man of Bethlehem... he and his wife and his two sons", ALL of them are from Bethlehem

### Type Constraints
- Check the source/target type constraints for each relationship type
- If a relationship type says "Person → Place", the source must be a Person and target must be a Place

## RELATIONSHIP DISCOVERY
1. **Family**: "his two sons were X and Y" → parent-child relationships for BOTH parents
2. **Marriage**: "his wife X", "took wives" → spousal relationships  
3. **Travel**: "went to X" → journey/travel relationships
4. **Residence**: "from Bethlehem", "lived there" → residence relationships
5. **Membership**: "They were Ephrathites" → group membership for each person
6. **Geography**: "Bethlehem in Judah" → geographic containment (Place in Place)
7. **Events with location**: If an event (marathon, wedding, conference) is named after or held in a city, extract a "takes_place_in" (or equivalent) relationship from the Event to that Place
8. **Spectators/supporters**: If someone watched, attended, cheered for, or supported an event, extract an "attended" or "watched" relationship from that Person to the Event
9. **Implied social ties**: If someone sent an email/message to another person, extract a "communicated_with" relationship. If someone planned or booked something for a companion, extract a "traveled_with" or similar relationship.`

// ObjectSchema represents a schema for an entity or relationship type.
type ObjectSchema struct {
	Name                 string                 `json:"name"`
	Description          string                 `json:"description,omitempty"`
	Properties           map[string]PropertyDef `json:"properties,omitempty"`
	Required             []string               `json:"required,omitempty"`
	ExtractionGuidelines string                 `json:"extraction_guidelines,omitempty"`
}

// PropertyDef defines a property in a schema.
type PropertyDef struct {
	Type        string `json:"type"`
	Description string `json:"description,omitempty"`
}

// RelationshipSchema represents a relationship type schema.
type RelationshipSchema struct {
	Name                 string                 `json:"name"`
	Description          string                 `json:"description,omitempty"`
	SourceTypes          []string               `json:"source_types,omitempty"`
	TargetTypes          []string               `json:"target_types,omitempty"`
	ExtractionGuidelines string                 `json:"extraction_guidelines,omitempty"`
	Properties           map[string]PropertyDef `json:"properties,omitempty"`
	Required             []string               `json:"required,omitempty"`
}

// ExistingEntityContext provides context about an existing entity for identity resolution.
type ExistingEntityContext struct {
	ID          string  `json:"id"`
	Name        string  `json:"name"`
	TypeName    string  `json:"type_name"`
	Description string  `json:"description,omitempty"`
	Similarity  float64 `json:"similarity,omitempty"`
}

// InternalEntity represents an entity with a temp_id for internal processing.
type InternalEntity struct {
	TempID           string         `json:"temp_id"`
	Name             string         `json:"name"`
	Type             string         `json:"type"`
	Description      string         `json:"description,omitempty"`
	Properties       map[string]any `json:"properties,omitempty"`
	Action           EntityAction   `json:"action,omitempty"`
	ExistingEntityID string         `json:"existing_entity_id,omitempty"`
}

// BuildEntityExtractionPrompt builds a prompt for entity extraction.
func BuildEntityExtractionPrompt(
	documentText string,
	objectSchemas map[string]ObjectSchema,
	allowedTypes []string,
	existingEntities []ExistingEntityContext,
) string {
	// Determine types to extract
	typesToExtract := allowedTypes
	if len(typesToExtract) == 0 {
		typesToExtract = make([]string, 0, len(objectSchemas))
		for typeName := range objectSchemas {
			typesToExtract = append(typesToExtract, typeName)
		}
	}

	var sb strings.Builder

	// System prompt
	sb.WriteString(EntityExtractorSystemPrompt)
	sb.WriteString("\n\n## Entity Types and Their Properties\n\n")
	sb.WriteString(fmt.Sprintf("Extract ONLY these types: %s\n\n", strings.Join(typesToExtract, ", ")))

	// Top-level fields not to include in properties
	topLevelFields := map[string]bool{"name": true, "description": true, "type": true}

	// Add type descriptions with property schemas
	for _, typeName := range typesToExtract {
		schema, ok := objectSchemas[typeName]
		if !ok {
			sb.WriteString(fmt.Sprintf("### %s\n\n", typeName))
			continue
		}

		sb.WriteString(fmt.Sprintf("### %s\n", typeName))
		if schema.Description != "" {
			sb.WriteString(schema.Description)
			sb.WriteString("\n")
		}

		// Include property definitions (excluding top-level fields)
		if len(schema.Properties) > 0 {
			additionalProps := make(map[string]PropertyDef)
			for propName, propDef := range schema.Properties {
				if !topLevelFields[propName] && !strings.HasPrefix(propName, "_") {
					additionalProps[propName] = propDef
				}
			}

			if len(additionalProps) > 0 {
				sb.WriteString("**Additional Properties** (stored in `properties` object):\n")
				for propName, propDef := range additionalProps {
					propType := propDef.Type
					if propType == "" {
						propType = "string"
					}
					required := ""
					for _, req := range schema.Required {
						if req == propName {
							required = " (required)"
							break
						}
					}
					sb.WriteString(fmt.Sprintf("- `%s` (%s)%s: %s\n", propName, propType, required, propDef.Description))
				}
			}
		}
		sb.WriteString("\n")
	}

	// Add existing entity context if provided
	if len(existingEntities) > 0 {
		sb.WriteString(contextAwareExtractionRules)
		sb.WriteString("\n\n## Existing Entities in Knowledge Graph\n\n")
		sb.WriteString("These entities already exist. Use their exact names and IDs if the document references them:\n\n")

		// Group by type
		byType := make(map[string][]ExistingEntityContext)
		for _, entity := range existingEntities {
			byType[entity.TypeName] = append(byType[entity.TypeName], entity)
		}

		const maxPerType = 10
		const maxTotal = 50
		totalShown := 0

		for _, typeName := range typesToExtract {
			if totalShown >= maxTotal {
				break
			}
			entities, ok := byType[typeName]
			if !ok {
				continue
			}

			sb.WriteString(fmt.Sprintf("### %s\n", typeName))
			toShow := entities
			if len(toShow) > maxPerType {
				toShow = toShow[:maxPerType]
			}

			for _, entity := range toShow {
				if totalShown >= maxTotal {
					break
				}
				similarity := ""
				if entity.Similarity > 0 {
					similarity = fmt.Sprintf(" (similarity: %.0f%%)", entity.Similarity*100)
				}
				desc := ""
				if entity.Description != "" {
					d := entity.Description
					if len(d) > 100 {
						d = d[:100]
					}
					desc = " - " + d
				}
				sb.WriteString(fmt.Sprintf("- **%s** [id: %s]%s%s\n", entity.Name, entity.ID, similarity, desc))
				totalShown++
			}

			if len(entities) > maxPerType {
				sb.WriteString(fmt.Sprintf("  _(and %d more)_\n", len(entities)-maxPerType))
			}
		}
		sb.WriteString("\n")
	}

	// Add document
	sb.WriteString("\n## Document\n\n")
	sb.WriteString(documentText)
	sb.WriteString("\n\n")

	// Output format instructions
	hasExistingEntities := len(existingEntities) > 0
	if hasExistingEntities {
		sb.WriteString(outputFormatWithContext)
	} else {
		sb.WriteString(outputFormatBasic)
	}

	return sb.String()
}

// BuildRelationshipPrompt builds a prompt for relationship extraction.
func BuildRelationshipPrompt(
	entities []InternalEntity,
	relationshipSchemas map[string]RelationshipSchema,
	documentText string,
	existingEntities []ExistingEntityContext,
	orphanTempIDs []string,
) string {
	var sb strings.Builder

	sb.WriteString(RelationshipBuilderSystemPrompt)
	sb.WriteString("\n\n## Available Relationship Types\n\n")

	// Add relationship type definitions
	for typeName, schema := range relationshipSchemas {
		sb.WriteString(fmt.Sprintf("### %s\n", typeName))
		if schema.Description != "" {
			sb.WriteString(schema.Description)
			sb.WriteString("\n\n")
		}

		// Add source/target constraints
		if len(schema.SourceTypes) > 0 || len(schema.TargetTypes) > 0 {
			sourceStr := "any"
			if len(schema.SourceTypes) > 0 {
				sourceStr = strings.Join(schema.SourceTypes, " or ")
			}
			targetStr := "any"
			if len(schema.TargetTypes) > 0 {
				targetStr = strings.Join(schema.TargetTypes, " or ")
			}
			sb.WriteString(fmt.Sprintf("**Valid entity types:** %s → %s\n\n", sourceStr, targetStr))
		}

		if schema.ExtractionGuidelines != "" {
			sb.WriteString(fmt.Sprintf("**Guidelines:**\n%s\n\n", schema.ExtractionGuidelines))
		}
	}

	// Add entities section
	sb.WriteString("\n## Extracted Entities\n\n")
	sb.WriteString("Use these temp_ids when creating relationships:\n\n")

	for _, entity := range entities {
		desc := ""
		if entity.Description != "" {
			d := entity.Description
			if len(d) > 80 {
				d = d[:80] + "..."
			}
			desc = " - " + d
		}
		sb.WriteString(fmt.Sprintf("- **%s** [temp_id: %s] (%s)%s\n", entity.Name, entity.TempID, entity.Type, desc))
	}
	sb.WriteString("\n")

	// Add orphan focus if retrying
	if len(orphanTempIDs) > 0 {
		sb.WriteString("## PRIORITY: Connect These Orphan Entities\n\n")
		sb.WriteString("The following entities have NO relationships yet. Find connections for them:\n")
		for _, id := range orphanTempIDs {
			sb.WriteString(fmt.Sprintf("- %s\n", id))
		}
		sb.WriteString("\n")
	}

	// Add document context
	sb.WriteString("## Document\n\n")
	sb.WriteString(documentText)
	sb.WriteString("\n\n")

	// Output format
	sb.WriteString(relationshipOutputFormat)

	return sb.String()
}

// Context-aware extraction rules
const contextAwareExtractionRules = `
CONTEXT-AWARE EXTRACTION RULES:
- Below is a list of existing entities already in the knowledge graph
- When you find an entity that MATCHES an existing one, use the SAME NAME and set action="enrich"
- When you find NEW information about an existing entity, include it in the description
- Only extract entities that are mentioned or referenced in THIS document
- Do NOT simply copy existing entities - only include them if the document mentions them
- For each entity, specify an "action":
  - "create" (default): This is a completely NEW entity not in the existing list
  - "enrich": This entity MATCHES an existing entity - new info should be merged
  - "reference": This entity is just a reference to an existing entity (for relationships only, no new info)
- When action is "enrich" or "reference", also provide "existing_entity_id" with the UUID from the existing entity`

// Basic output format (no existing entities)
const outputFormatBasic = `## Output Format

Return a JSON object with an "entities" key containing an array of entities.

Each entity must have:
- name (string): Entity name
- type (string): One of the allowed types above
- description (string, optional): Brief description
- properties (object, optional): Type-specific attributes found in the document

Example:
{
  "entities": [
    {
      "name": "John the Apostle",
      "type": "Person",
      "description": "One of the twelve apostles",
      "properties": {
        "role": "apostle",
        "occupation": "fisherman"
      }
    }
  ]
}

Extract all entities now.`

// Output format with existing entity context
const outputFormatWithContext = `## Output Format

Return a JSON object with an "entities" key containing an array of entities.

Each entity must have:
- name (string): Entity name (use exact names from existing entities when matching)
- type (string): One of the allowed types above
- description (string, optional): Brief description
- properties (object, optional): Type-specific attributes found in the document
- action (string, optional): "create" (new entity), "enrich" (update existing), or "reference" (just a reference)
- existing_entity_id (string, optional): UUID of existing entity when action is "enrich" or "reference"

Example:
{
  "entities": [
    {
      "name": "Jerusalem",
      "type": "Place",
      "description": "Holy city",
      "properties": {"region": "Judea"},
      "action": "enrich",
      "existing_entity_id": "abc-123-uuid"
    }
  ]
}

Extract all entities now.`

// Relationship output format
const relationshipOutputFormat = `## Output Format

Return a JSON object with a "relationships" key containing an array of relationships.

Each relationship must have:
- source_ref (string): temp_id of the source entity
- target_ref (string): temp_id of the target entity
- type (string): Relationship type from the allowed list above
- description (string, optional): Description of this relationship instance

Example:
{
  "relationships": [
    {
      "source_ref": "person_john",
      "target_ref": "organization_disciples",
      "type": "MEMBER_OF",
      "description": "John was one of the twelve disciples"
    }
  ]
}

Find ALL relationships between the entities now. Ensure no entity is left without at least one connection.`

// BuildDomainSection builds an optional domain-guidance section for injection
// into entity/relationship extractor system prompts.
// Returns empty string when both args are empty (no-op for existing callers).
func BuildDomainSection(projectContext, domainGuidance string) string {
	if projectContext == "" && domainGuidance == "" {
		return ""
	}
	var sb strings.Builder
	sb.WriteString("\n\n## Domain Context\n\n")
	if projectContext != "" {
		sb.WriteString("**Knowledge base purpose:** ")
		sb.WriteString(projectContext)
		sb.WriteString("\n")
	}
	if domainGuidance != "" {
		sb.WriteString("\n**Schema-specific guidance:**\n")
		sb.WriteString(domainGuidance)
		sb.WriteString("\n")
	}
	return sb.String()
}

// BuildEntityUnpackingPrompt builds the phase-1 (unpack) prompt.
// No schema, no type constraints — exhaustive reading of the document.
func BuildEntityUnpackingPrompt(documentText string) string {
	var sb strings.Builder
	sb.WriteString(EntityUnpackerSystemPrompt)
	sb.WriteString("\n\n## Document\n\n")
	sb.WriteString(documentText)
	sb.WriteString("\n\nExtract all entities and their facts now.")
	return sb.String()
}

// BuildEntityNormalizationPrompt builds the phase-2 (normalize) prompt.
// Maps unpacked free-form items onto strict schema types and property slots.
// typeHints maps typeName → extraction hint (from SchemaExtractionPrompts.TypeHints).
// negativeExamples lists things NOT to extract.
func BuildEntityNormalizationPrompt(
	unpackedItems []UnpackedItem,
	objectSchemas map[string]ObjectSchema,
	allowedTypes []string,
	typeHints map[string]string,
	negativeExamples []string,
) string {
	typesToNorm := allowedTypes
	if len(typesToNorm) == 0 {
		typesToNorm = make([]string, 0, len(objectSchemas))
		for t := range objectSchemas {
			typesToNorm = append(typesToNorm, t)
		}
	}

	var sb strings.Builder

	sb.WriteString(`You are a schema normalizer. You have a list of raw entities and their facts extracted from a document.
Your job: map each raw entity to one of the schema types below and fill the schema property slots from the entity's facts.

Rules:
- Choose the BEST matching schema type for each raw entity.
- If no schema type fits, skip that entity — do not force a match.
- Fill every property slot you can from the entity's facts. Do NOT invent values.
- Facts may contain more information than the schema has slots for — fill what fits, ignore the rest.
- Multiple raw entities may map to the same schema type.
- Preserve exact names from the raw entities.

`)

	if len(negativeExamples) > 0 {
		sb.WriteString("## Do NOT extract these\n")
		for _, ex := range negativeExamples {
			sb.WriteString(fmt.Sprintf("- %s\n", ex))
		}
		sb.WriteString("\n")
	}

	topLevelFields := map[string]bool{"name": true, "description": true, "type": true}

	sb.WriteString("## Schema Types\n\n")
	sb.WriteString(fmt.Sprintf("Normalise into ONLY these types: %s\n\n", strings.Join(typesToNorm, ", ")))

	for _, typeName := range typesToNorm {
		schema, ok := objectSchemas[typeName]
		sb.WriteString(fmt.Sprintf("### %s\n", typeName))
		if ok && schema.Description != "" {
			sb.WriteString(schema.Description + "\n")
		}
		if hint, ok := typeHints[typeName]; ok && hint != "" {
			sb.WriteString(fmt.Sprintf("**Hint:** %s\n", hint))
		}
		if ok && len(schema.Properties) > 0 {
			sb.WriteString("**Properties** (in `properties` object):\n")
			for propName, propDef := range schema.Properties {
				if topLevelFields[propName] || strings.HasPrefix(propName, "_") {
					continue
				}
				propType := propDef.Type
				if propType == "" {
					propType = "string"
				}
				required := ""
				for _, req := range schema.Required {
					if req == propName {
						required = " (required)"
						break
					}
				}
				sb.WriteString(fmt.Sprintf("- `%s` (%s)%s: %s\n", propName, propType, required, propDef.Description))
			}
		}
		sb.WriteString("\n")
	}

	sb.WriteString("## Raw Extracted Items\n\nMap each onto a schema type:\n\n")
	for i, item := range unpackedItems {
		sb.WriteString(fmt.Sprintf("### Item %d: %s\n", i+1, item.Name))
		sb.WriteString(fmt.Sprintf("Kind: %s\n", item.Kind))
		if len(item.Facts) > 0 {
			sb.WriteString("Facts:\n")
			for _, fact := range item.Facts {
				sb.WriteString(fmt.Sprintf("- %s\n", fact))
			}
		}
		sb.WriteString("\n")
	}

	sb.WriteString(`## Output Format

Return JSON with an "entities" key. Each entity:
- name (string): exact name from raw item
- type (string): one of the schema types above
- description (string, optional): brief description
- properties (object): schema property slots filled from facts

Example:
{"entities": [{"name": "Monica Geller", "type": "Character", "description": "Chef", "properties": {"occupation": "chef", "apartment": "apartment 20"}}]}

Normalise all matching items now.`)

	return sb.String()
}
