// Package agents provides ADK-based extraction agents for entity and relationship extraction.
package agents

import (
	"google.golang.org/genai"
)

// EntityAction represents the action to take for an extracted entity.
type EntityAction string

const (
	// EntityActionCreate indicates a new entity not in existing context.
	EntityActionCreate EntityAction = "create"
	// EntityActionEnrich indicates an entity that matches existing, merge new info.
	EntityActionEnrich EntityAction = "enrich"
	// EntityActionReference indicates a pure reference to an existing entity.
	EntityActionReference EntityAction = "reference"
)

// ExtractedEntity represents an entity extracted by the LLM.
type ExtractedEntity struct {
	// Name is the human-readable name of the entity.
	Name string `json:"name"`
	// Type is the entity type (e.g., "Person", "Organization", "Location").
	Type string `json:"type"`
	// Description is a brief description of the entity.
	Description string `json:"description,omitempty"`
	// Properties contains type-specific attributes extracted from the document.
	Properties map[string]any `json:"properties,omitempty"`
	// Action indicates what to do with this entity (create/enrich/reference).
	Action EntityAction `json:"action,omitempty"`
	// ExistingEntityID is the UUID of an existing entity (when action is enrich/reference).
	ExistingEntityID string `json:"existing_entity_id,omitempty"`
}

// EntityExtractionOutput is the output schema for entity extraction.
type EntityExtractionOutput struct {
	Entities []ExtractedEntity `json:"entities"`
}

// ExtractedRelationship represents a relationship extracted by the LLM.
type ExtractedRelationship struct {
	// SourceRef is the temp_id of the source entity.
	SourceRef string `json:"source_ref"`
	// TargetRef is the temp_id of the target entity.
	TargetRef string `json:"target_ref"`
	// Type is the relationship type (e.g., "WORKS_FOR", "LOCATED_IN").
	Type string `json:"type"`
	// Description is an optional description of the relationship.
	Description string `json:"description,omitempty"`
}

// RelationshipExtractionOutput is the output schema for relationship extraction.
type RelationshipExtractionOutput struct {
	Relationships []ExtractedRelationship `json:"relationships"`
}

// EntityExtractionSchema returns the genai.Schema for entity extraction output.
// This schema is used with ADK's OutputSchema for structured JSON output.
func EntityExtractionSchema() *genai.Schema {
	return &genai.Schema{
		Type:        genai.TypeObject,
		Description: "Output containing extracted entities from the document",
		Required:    []string{"entities"},
		Properties: map[string]*genai.Schema{
			"entities": {
				Type:        genai.TypeArray,
				Description: "Array of extracted entities",
				Items: &genai.Schema{
					Type:     genai.TypeObject,
					Required: []string{"name", "type"},
					Properties: map[string]*genai.Schema{
						"name": {
							Type:        genai.TypeString,
							Description: "Human-readable name of the entity",
						},
						"type": {
							Type:        genai.TypeString,
							Description: "Entity type (e.g., 'Person', 'Organization', 'Location')",
						},
						"description": {
							Type:        genai.TypeString,
							Description: "Brief description of the entity",
						},
						"properties": {
							Type:        genai.TypeObject,
							Description: "Type-specific attributes extracted from the document (e.g., role, occupation, location)",
						},
						"action": {
							Type:        genai.TypeString,
							Description: "Action: 'create' (new entity), 'enrich' (update existing), 'reference' (just a reference)",
							Enum:        []string{"create", "enrich", "reference"},
						},
						"existing_entity_id": {
							Type:        genai.TypeString,
							Description: "UUID of existing entity when action is 'enrich' or 'reference'",
						},
					},
				},
			},
		},
	}
}

// RelationshipExtractionSchema returns the genai.Schema for relationship extraction output.
func RelationshipExtractionSchema() *genai.Schema {
	return &genai.Schema{
		Type:        genai.TypeObject,
		Description: "Output containing extracted relationships between entities",
		Required:    []string{"relationships"},
		Properties: map[string]*genai.Schema{
			"relationships": {
				Type:        genai.TypeArray,
				Description: "Array of extracted relationships",
				Items: &genai.Schema{
					Type:     genai.TypeObject,
					Required: []string{"source_ref", "target_ref", "type"},
					Properties: map[string]*genai.Schema{
						"source_ref": {
							Type:        genai.TypeString,
							Description: "temp_id of the source entity",
						},
						"target_ref": {
							Type:        genai.TypeString,
							Description: "temp_id of the target entity",
						},
						"type": {
							Type:        genai.TypeString,
							Description: "Relationship type (e.g., 'WORKS_FOR', 'LOCATED_IN', 'PARENT_OF')",
						},
						"description": {
							Type:        genai.TypeString,
							Description: "Optional description of this specific relationship instance",
						},
					},
				},
			},
		},
	}
}

// BuildEntitySchemaFromTemplatePack builds a dynamic genai.Schema for entity extraction
// from template pack object schemas. The schema includes an enum of allowed entity types.
//
// This approach (ResponseSchema) is ~35% faster than embedding the schema in the prompt
// and guarantees valid JSON output from the LLM.
func BuildEntitySchemaFromTemplatePack(objectSchemas map[string]ObjectSchema) *genai.Schema {
	typeNames := make([]string, 0, len(objectSchemas))
	for typeName := range objectSchemas {
		typeNames = append(typeNames, typeName)
	}

	if len(typeNames) == 0 {
		return EntityExtractionSchema()
	}

	return &genai.Schema{
		Type:        genai.TypeObject,
		Description: "Output containing extracted entities from the document",
		Required:    []string{"entities"},
		Properties: map[string]*genai.Schema{
			"entities": {
				Type:        genai.TypeArray,
				Description: "Array of extracted entities",
				Items: &genai.Schema{
					Type:     genai.TypeObject,
					Required: []string{"name", "type"},
					Properties: map[string]*genai.Schema{
						"name": {
							Type:        genai.TypeString,
							Description: "Human-readable name of the entity",
						},
						"type": {
							Type:        genai.TypeString,
							Description: "Entity type from the template pack",
							Enum:        typeNames,
						},
						"description": {
							Type:        genai.TypeString,
							Description: "Brief description of the entity",
						},
						"properties": {
							Type:        genai.TypeObject,
							Description: "Type-specific attributes extracted from the document",
						},
						"action": {
							Type:        genai.TypeString,
							Description: "Action: 'create' (new entity), 'enrich' (update existing), 'reference' (just a reference)",
							Enum:        []string{"create", "enrich", "reference"},
						},
						"existing_entity_id": {
							Type:        genai.TypeString,
							Description: "UUID of existing entity when action is 'enrich' or 'reference'",
						},
					},
				},
			},
		},
	}
}

// BuildRelationshipSchemaFromTemplatePack builds a dynamic genai.Schema for relationship extraction
// from template pack relationship schemas. The schema includes an enum of allowed relationship types.
func BuildRelationshipSchemaFromTemplatePack(relationshipSchemas map[string]RelationshipSchema) *genai.Schema {
	typeNames := make([]string, 0, len(relationshipSchemas))
	for typeName := range relationshipSchemas {
		typeNames = append(typeNames, typeName)
	}

	if len(typeNames) == 0 {
		return RelationshipExtractionSchema()
	}

	return &genai.Schema{
		Type:        genai.TypeObject,
		Description: "Output containing extracted relationships between entities",
		Required:    []string{"relationships"},
		Properties: map[string]*genai.Schema{
			"relationships": {
				Type:        genai.TypeArray,
				Description: "Array of extracted relationships",
				Items: &genai.Schema{
					Type:     genai.TypeObject,
					Required: []string{"source_ref", "target_ref", "type"},
					Properties: map[string]*genai.Schema{
						"source_ref": {
							Type:        genai.TypeString,
							Description: "temp_id of the source entity",
						},
						"target_ref": {
							Type:        genai.TypeString,
							Description: "temp_id of the target entity",
						},
						"type": {
							Type:        genai.TypeString,
							Description: "Relationship type from the template pack",
							Enum:        typeNames,
						},
						"description": {
							Type:        genai.TypeString,
							Description: "Description of this specific relationship instance",
						},
					},
				},
			},
		},
	}
}
