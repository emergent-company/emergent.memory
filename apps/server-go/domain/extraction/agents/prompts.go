// Package agents provides ADK-based extraction agents for entity and relationship extraction.
package agents

import (
	"fmt"
	"strings"
)

// EntityExtractorSystemPrompt is the base system prompt for entity extraction.
const EntityExtractorSystemPrompt = `You are an expert knowledge graph builder. Extract entities from the document.

For EACH entity, you MUST provide these four fields:
1. name: Clear, descriptive name of the entity (REQUIRED, top-level field)
2. type: Entity type from the allowed list (REQUIRED, top-level field)
3. description: Brief description of what this entity represents (top-level field)
4. properties: An object containing type-specific attributes (CRITICAL - see below)

CRITICAL INSTRUCTIONS FOR PROPERTIES:
- The "properties" field is an object that MUST contain type-specific attributes extracted from the document
- For Person entities: include role, occupation, title, father, mother, tribe, age, significance, etc.
- For Location entities: include region, country, location_type, significance, etc.
- For Event entities: include date, location, participants, outcome, etc.
- For Organization entities: include type, purpose, members, location, etc.
- NEVER return an empty properties object {} if there is ANY relevant information in the document
- Extract ALL attributes mentioned or implied in the text for each entity
- The properties object should NOT contain name, type, or description - those are top-level fields

RULES:
- Extract ALL entities that match the allowed types
- Be thorough - don't miss important entities
- Use consistent naming
- Keep descriptions concise but informative
- Only include properties that are explicitly mentioned or clearly implied in the document
- Do NOT guess or fabricate property values`

// RelationshipBuilderSystemPrompt is the base system prompt for relationship extraction.
const RelationshipBuilderSystemPrompt = `You are an expert at finding connections in knowledge graphs. Your job is to identify ALL meaningful relationships between entities.

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
6. **Geography**: "Bethlehem in Judah" → geographic containment (Place in Place)`

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
	Name                  string   `json:"name"`
	Description           string   `json:"description,omitempty"`
	SourceTypes           []string `json:"source_types,omitempty"`
	TargetTypes           []string `json:"target_types,omitempty"`
	ExtractionGuidelines  string   `json:"extraction_guidelines,omitempty"`
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
