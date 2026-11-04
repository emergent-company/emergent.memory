package graphexplore

// ObjectType represents a node type from compiled-types + registry enrichment.
type ObjectType struct {
	Name        string `json:"name"`
	Label       string `json:"label"`
	Description string `json:"description"`
	Color       string `json:"color"`
	Icon        string `json:"icon"`
	Count       int    `json:"count"`    // total objects in DB
	InGraph     int    `json:"in_graph"` // currently loaded in the graph
	Hidden      bool   `json:"hidden"`   // filter state
}

// RelationshipType represents an edge type from compiled-types.
type RelationshipType struct {
	Name         string `json:"name"`
	Label        string `json:"label"`
	InverseLabel string `json:"inverse_label"`
	Count        int    `json:"count"` // edges of this type in the graph
	Hidden       bool   `json:"hidden"`
	Color        string `json:"color"`
}

// NodeDetail holds data for the right panel when a node is selected.
type NodeDetail struct {
	ID         string          `json:"id"`
	Type       string          `json:"type"`
	TypeColor  string          `json:"type_color"`
	Key        string          `json:"key"`
	Labels     []string        `json:"labels"`
	Properties []PropertyField `json:"properties"`
}

// PropertyField is a key-value pair for the properties section.
type PropertyField struct {
	Key   string `json:"key"`
	Value string `json:"value"`
	Index int    `json:"index"` // for alternating row colors
}

// Relation represents a relationship in the detail panel.
type Relation struct {
	OtherID    string `json:"other_id"`
	OtherName  string `json:"other_name"`
	OtherType  string `json:"other_type"`
	OtherColor string `json:"other_color"`
	InGraph    bool   `json:"in_graph"`
	Index      int    `json:"index"`
}

// RelationGroup groups relations by type for display.
type RelationGroup struct {
	TypeLabel string     `json:"type_label"`
	Relations []Relation `json:"relations"`
}

// SchemaTypeDetail holds data for the right panel when a schema type node is selected.
type SchemaTypeDetail struct {
	Name        string
	Label       string
	Description string
	Color       string
	Icon        string
	Properties  []SchemaProperty
	Outgoing    []SchemaRelation // rel types where sourceType == this type
	Incoming    []SchemaRelation // rel types where targetType == this type
}

// SchemaProperty represents a property from the JSON schema definition.
type SchemaProperty struct {
	Name        string
	Type        string // "string", "integer", etc.
	Description string
	Required    bool
	Index       int
}

// SchemaRelation represents a relationship type involving a schema type.
type SchemaRelation struct {
	RelName    string // "belongs_to"
	RelLabel   string // "Belongs To"
	OtherType  string // "Organization"
	OtherColor string
	Direction  string // "out" or "in"
	Index      int
}
