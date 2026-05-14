package mcp

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/emergent-company/emergent.memory/pkg/auth"
)

// ============================================================================
// Interfaces (break import cycle with extraction/discoveryjobs packages)
// ============================================================================

// DomainClassifierHandler classifies a document against installed schemas.
// Implemented by a wrapper in the extraction package to avoid import cycles.
type DomainClassifierHandler interface {
	ClassifyDocument(ctx context.Context, projectID, documentID string) (ClassificationSnapshot, error)
}

// ClassificationSnapshot mirrors extraction.ClassificationResult for use in mcp.
type ClassificationSnapshot struct {
	SchemaID   *string `json:"schema_id"`
	Label      string  `json:"label"`
	Confidence float32 `json:"confidence"`
	Stage      string  `json:"stage"`
	LLMReason  string  `json:"llm_reason,omitempty"`
}

// SchemaIndexHandler returns lightweight summaries of installed schema packs.
// Implemented by extraction.MemorySchemaProvider.
type SchemaIndexHandler interface {
	ListInstalledSchemas(ctx context.Context, projectID string) ([]SchemaIndexEntry, error)
}

// SchemaIndexEntry mirrors extraction.InstalledSchemaSummary for use in mcp.
type SchemaIndexEntry struct {
	ID          string   `json:"id"`
	Name        string   `json:"name"`
	Description string   `json:"description"`
	Keywords    []string `json:"keywords"`
}

// ReextractionQueuer queues a document for re-extraction with a specific schema.
// Implemented by a wrapper around extraction.ObjectExtractionJobsService.
type ReextractionQueuer interface {
	QueueReextraction(ctx context.Context, projectID, documentID, schemaID string) (string, error)
}

// DiscoveryFinalizer finalizes a discovery job.
// The implementation uses interface{} to avoid import cycles.
type DiscoveryFinalizer interface {
	FinalizeDiscoveryFromMCP(ctx context.Context, req interface{}) (interface{}, error)
}

// DiscoveryFinalizeRequest mirrors discoveryjobs.FinalizeDiscoveryRequest for mcp use.
type DiscoveryFinalizeRequest struct {
	JobID          string
	DocumentID     string
	ProjectID      string
	OrgID          string
	Mode           string
	PackName       string
	ExistingPackID string
	IncludedTypes  []map[string]any
	IncludedRels   []map[string]any
}

// DiscoveryFinalizeResponse mirrors discoveryjobs.FinalizeDiscoveryResponse.
type DiscoveryFinalizeResponse struct {
	SchemaID string `json:"schema_id"`
	Message  string `json:"message"`
}

// ============================================================================
// Domain Tool Definitions
// ============================================================================

func domainToolDefinitions() []ToolDefinition {
	return []ToolDefinition{
		{
			Name:        "classify-document",
			Description: "Classify a document against installed domain schema packs. Returns the matched schema, label, confidence, and classification stage. Read-only — does not write to the document.",
			InputSchema: InputSchema{
				Type: "object",
				Properties: map[string]PropertySchema{
					"project_id": {
						Type:        "string",
						Description: "UUID of the project (optional — inferred from auth context if omitted)",
					},
					"document_id": {
						Type:        "string",
						Description: "UUID of the document to classify",
					},
				},
				Required: []string{"document_id"},
			},
		},
		{
			Name:        "list-installed-schemas",
			Description: "List all domain schema packs installed in a project, including their names, descriptions, and keywords used for classification.",
			InputSchema: InputSchema{
				Type: "object",
				Properties: map[string]PropertySchema{
					"project_id": {
						Type:        "string",
						Description: "UUID of the project (optional — inferred from auth context if omitted)",
					},
				},
				Required: []string{},
			},
		},
		{
			Name:        "finalize-discovery",
			Description: "Finalize domain discovery by creating a new schema pack or extending an existing one. Provide document_id to create a new discovery job on the fly (no job_id needed). job_id is only needed when resuming an existing pending discovery job.",
			InputSchema: InputSchema{
				Type: "object",
				Properties: map[string]PropertySchema{
					"document_id": {
						Type:        "string",
						Description: "UUID of the document being processed (preferred — used to create discovery job automatically)",
					},
					"job_id": {
						Type:        "string",
						Description: "UUID of an existing discovery job to finalize (optional — omit if providing document_id)",
					},
					"project_id": {
						Type:        "string",
						Description: "UUID of the project (optional — inferred from auth context if omitted)",
					},
					"org_id": {
						Type:        "string",
						Description: "UUID of the organization (optional — inferred from auth context if omitted)",
					},
					"mode": {
						Type:        "string",
						Description: "How to finalize: 'create' to create a new schema pack, 'extend' to add types to an existing pack",
					},
					"pack_name": {
						Type:        "string",
						Description: "Name for the new schema pack (required when mode='create')",
					},
					"existing_pack_id": {
						Type:        "string",
						Description: "UUID of the existing schema pack to extend (required when mode='extend')",
					},
					"included_types": {
						Type:        "array",
						Description: "List of discovered types to include. Each item: {type_name, description, properties, required_properties, example_instances, frequency}",
					},
					"included_relationships": {
						Type:        "array",
						Description: "List of discovered relationships to include. Each item: {source_type, target_type, relation_type, description, cardinality}",
					},
				},
				Required: []string{"mode", "included_types"},
			},
		},
		{
			Name:        "queue-reextraction",
			Description: "Queue a document for re-extraction using a specific domain schema pack. Use after domain discovery to enrich an existing document with typed entities.",
			InputSchema: InputSchema{
				Type: "object",
				Properties: map[string]PropertySchema{
					"project_id": {
						Type:        "string",
						Description: "UUID of the project (optional — inferred from auth context if omitted)",
					},
					"document_id": {
						Type:        "string",
						Description: "UUID of the document to re-extract",
					},
					"schema_id": {
						Type:        "string",
						Description: "UUID of the schema pack to use for extraction",
					},
				},
				Required: []string{"document_id", "schema_id"},
			},
		},
	}
}

// ============================================================================
// Domain Tool Handlers
// ============================================================================

func (s *Service) executeClassifyDocument(ctx context.Context, projectID string, args map[string]any) (*ToolResult, error) {
	if s.domainClassifier == nil {
		return errorResult("domain classifier not available"), nil
	}
	documentID, _ := args["document_id"].(string)
	if documentID == "" {
		return errorResult("document_id is required"), nil
	}

	result, err := s.domainClassifier.ClassifyDocument(ctx, projectID, documentID)
	if err != nil {
		return errorResult(fmt.Sprintf("classification failed: %s", err)), nil
	}

	out, _ := json.Marshal(result)
	return &ToolResult{Content: []ContentBlock{{Type: "text", Text: string(out)}}}, nil
}

func (s *Service) executeListInstalledSchemas(ctx context.Context, projectID string) (*ToolResult, error) {
	if s.schemaIndex == nil {
		return errorResult("schema index not available"), nil
	}

	entries, err := s.schemaIndex.ListInstalledSchemas(ctx, projectID)
	if err != nil {
		return errorResult(fmt.Sprintf("list schemas failed: %s", err)), nil
	}

	out, _ := json.Marshal(entries)
	return &ToolResult{Content: []ContentBlock{{Type: "text", Text: string(out)}}}, nil
}

func (s *Service) executeFinalizeDiscovery(ctx context.Context, projectID string, args map[string]any) (*ToolResult, error) {
	if s.discoverySvc == nil {
		return errorResult("discovery service not available"), nil
	}

	jobIDStr, _ := args["job_id"].(string)
	documentIDStr, _ := args["document_id"].(string)
	orgIDStr, _ := args["org_id"].(string)
	// Fall back to auth context if arg is missing or not a valid UUID
	if orgIDStr == "" {
		orgIDStr = auth.OrgIDFromContext(ctx)
	}
	mode, _ := args["mode"].(string)
	packName, _ := args["pack_name"].(string)
	existingPackIDStr, _ := args["existing_pack_id"].(string)

	// Parse included_types
	var includedTypes []map[string]any
	if rawTypes, ok := args["included_types"]; ok {
		b, _ := json.Marshal(rawTypes)
		_ = json.Unmarshal(b, &includedTypes)
	}

	// Parse included_relationships
	var includedRels []map[string]any
	if rawRels, ok := args["included_relationships"]; ok {
		b, _ := json.Marshal(rawRels)
		_ = json.Unmarshal(b, &includedRels)
	}

	resp, err := s.discoverySvc.FinalizeDiscoveryFromMCP(ctx, DiscoveryFinalizeRequest{
		JobID:          jobIDStr,
		DocumentID:     documentIDStr,
		ProjectID:      projectID,
		OrgID:          orgIDStr,
		Mode:           mode,
		PackName:       packName,
		ExistingPackID: existingPackIDStr,
		IncludedTypes:  includedTypes,
		IncludedRels:   includedRels,
	})
	if err != nil {
		return errorResult(fmt.Sprintf("finalize discovery failed: %s", err)), nil
	}

	out, _ := json.Marshal(resp)
	return &ToolResult{Content: []ContentBlock{{Type: "text", Text: string(out)}}}, nil
}

func (s *Service) executeQueueReextraction(ctx context.Context, projectID string, args map[string]any) (*ToolResult, error) {
	if s.reextractionQueuer == nil {
		return errorResult("reextraction queuer not available"), nil
	}

	documentID, _ := args["document_id"].(string)
	schemaID, _ := args["schema_id"].(string)
	if documentID == "" || schemaID == "" {
		return errorResult("document_id and schema_id are required"), nil
	}

	jobID, err := s.reextractionQueuer.QueueReextraction(ctx, projectID, documentID, schemaID)
	if err != nil {
		return errorResult(fmt.Sprintf("queue reextraction failed: %s", err)), nil
	}

	out, _ := json.Marshal(map[string]string{"job_id": jobID})
	return &ToolResult{Content: []ContentBlock{{Type: "text", Text: string(out)}}}, nil
}

// errorResult returns a ToolResult with an error message.
func errorResult(msg string) *ToolResult {
	return &ToolResult{
		Content: []ContentBlock{{Type: "text", Text: msg}},
		IsError: true,
	}
}
