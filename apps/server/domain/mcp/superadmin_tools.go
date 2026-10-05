package mcp

import (
	"context"
	"errors"
	"fmt"

	"github.com/emergent-company/emergent.memory/pkg/auth"
)

// SuperadminToolHandler is the cross-domain seam for the operator MCP tools
// served on the dedicated /api/mcp/admin endpoint. It wraps the platform-admin
// operations in domain/superadmin (and the apitoken service-token mint) so
// domain/mcp does not import domain/superadmin into its constructor graph.
//
// The adapter is provided in cmd/server/main.go under the MCP + SuperAdmin
// feature flags. A nil handler is fail-closed at every call site: each operator
// tool returns a clear "not available" error and performs no operation.
type SuperadminToolHandler interface {
	// Reads
	SuperadminGet(ctx context.Context) (any, error)
	ListUsers(ctx context.Context, page, limit int, search, orgID string) (any, error)
	GetUser(ctx context.Context, userID string) (any, error)
	ListOrganizations(ctx context.Context, page, limit int) (any, error)
	GetOrg(ctx context.Context, orgID string) (any, error)
	ListProjects(ctx context.Context, page, limit int, orgID string) (any, error)
	GetProject(ctx context.Context, projectID string) (any, error)
	ListProjectMembers(ctx context.Context, projectID string) (any, error)
	ListEmailJobs(ctx context.Context, page, limit int, status, recipient, fromDate, toDate string) (any, error)
	GetEmailJob(ctx context.Context, id string) (any, error)
	ListEmbeddingJobs(ctx context.Context, page, limit int, status string, hasError *bool, projectID, jobType string) (any, error)
	GetEmbeddingJobStats(ctx context.Context) (any, error)
	ListExtractionJobs(ctx context.Context, page, limit int, status, jobType, projectID string, hasError *bool) (any, error)
	GetExtractionJobStats(ctx context.Context) (any, error)
	ListDocumentParsingJobs(ctx context.Context, page, limit int, status, projectID string, hasError *bool) (any, error)
	GetDocumentParsingJobStats(ctx context.Context) (any, error)

	// Mutations
	SoftDeleteUser(ctx context.Context, userID, deletedBy string) error
	SoftDeleteOrg(ctx context.Context, orgID, deletedBy string) error
	SoftDeleteProject(ctx context.Context, projectID, deletedBy string) error
	GrantSuperadmin(ctx context.Context, userID, grantedBy string, notes *string) error
	AddProjectMember(ctx context.Context, projectID, userID, role string) error
	RemoveProjectMember(ctx context.Context, projectID, userID string) error
	CreateServiceToken(ctx context.Context, name string, notes *string, grantedBy string) (any, error)
	DeleteEmbeddingJobs(ctx context.Context, ids []string, jobType string) (int, error)
	CleanupEmbeddingJobs(ctx context.Context) (int, error)
	ResetEmbeddingJobs(ctx context.Context) (int, error)
	DeleteExtractionJobs(ctx context.Context, ids []string) (int, error)
	CancelExtractionJobs(ctx context.Context, ids []string) (int, error)
	DeleteDocumentParsingJobs(ctx context.Context, ids []string) (int, error)
	RetryDocumentParsingJobs(ctx context.Context, ids []string) (int, error)
}

// errSuperadminNotAvailable is the fail-closed error returned when the operator
// tool handler has not been wired (MCP or SuperAdmin feature disabled).
var errSuperadminNotAvailable = errors.New("superadmin operator tools are not available")

// superadminToolDefinitions returns the NEW operator tools. Every tool is
// SuperadminOnly (superadmin_full), kebab-case, with input schemas that require
// the identifiers each tool acts on. Org-targeted tools require an explicit
// org_id; there is no ambient auth-context fallback on the admin path.
func superadminToolDefinitions() []ToolDefinition {
	confirmDesc := "Explicit confirmation guard. Mutations are a no-op dry-run unless confirm=true."
	return []ToolDefinition{
		// ---- reads ----
		{
			Name:           "superadmin-get",
			OutputSchema:   objectOutputSchema(),
			SuperadminOnly: true,
			Description:    "Return the calling superadmin's identity and role. The role is resolved from the platform grant (core.superadmins), never from a token scope.",
			InputSchema:    emptySuperadminInput(),
		},
		{
			Name:           "user-list",
			OutputSchema:   objectOutputSchema(),
			SuperadminOnly: true,
			Description:    "List users with pagination and optional search / org filtering.",
			InputSchema: InputSchema{
				Type: "object",
				Properties: map[string]PropertySchema{
					"page":   {Type: "integer", Description: "Page number (default 1)"},
					"limit":  {Type: "integer", Description: "Results per page (default 20, max 100)"},
					"search": {Type: "string", Description: "Search by name or email"},
					"org_id": {Type: "string", Description: "Filter by organization ID (UUID)"},
				},
				Required: []string{},
			},
		},
		{
			Name:           "user-get",
			OutputSchema:   objectOutputSchema(),
			SuperadminOnly: true,
			Description:    "Get a single user by ID.",
			InputSchema: InputSchema{
				Type:       "object",
				Properties: map[string]PropertySchema{"user_id": {Type: "string", Description: "User ID (UUID)"}},
				Required:   []string{"user_id"},
			},
		},
		{
			Name:           "org-list",
			OutputSchema:   objectOutputSchema(),
			SuperadminOnly: true,
			Description:    "List organizations with member and project counts.",
			InputSchema: InputSchema{
				Type: "object",
				Properties: map[string]PropertySchema{
					"page":  {Type: "integer", Description: "Page number (default 1)"},
					"limit": {Type: "integer", Description: "Results per page (default 20, max 100)"},
				},
				Required: []string{},
			},
		},
		{
			Name:           "org-get",
			OutputSchema:   objectOutputSchema(),
			SuperadminOnly: true,
			Description:    "Get a single organization by ID.",
			InputSchema: InputSchema{
				Type:       "object",
				Properties: map[string]PropertySchema{"org_id": {Type: "string", Description: "Organization ID (UUID)"}},
				Required:   []string{"org_id"},
			},
		},
		{
			Name:           "project-list",
			OutputSchema:   objectOutputSchema(),
			SuperadminOnly: true,
			Description:    "List projects with organization and document counts, optionally filtered by org.",
			InputSchema: InputSchema{
				Type: "object",
				Properties: map[string]PropertySchema{
					"page":   {Type: "integer", Description: "Page number (default 1)"},
					"limit":  {Type: "integer", Description: "Results per page (default 20, max 100)"},
					"org_id": {Type: "string", Description: "Filter by organization ID (UUID)"},
				},
				Required: []string{},
			},
		},
		{
			Name:           "project-record-get",
			OutputSchema:   objectOutputSchema(),
			SuperadminOnly: true,
			Description:    "Get a single project record by ID.",
			InputSchema: InputSchema{
				Type:       "object",
				Properties: map[string]PropertySchema{"project_id": {Type: "string", Description: "Project ID (UUID)"}},
				Required:   []string{"project_id"},
			},
		},
		{
			Name:           "project-member-list",
			OutputSchema:   objectOutputSchema(),
			SuperadminOnly: true,
			Description:    "List the members of a project with their roles and profiles.",
			InputSchema: InputSchema{
				Type:       "object",
				Properties: map[string]PropertySchema{"project_id": {Type: "string", Description: "Project ID (UUID)"}},
				Required:   []string{"project_id"},
			},
		},
		{
			Name:           "email-job-list",
			OutputSchema:   objectOutputSchema(),
			SuperadminOnly: true,
			Description:    "List email jobs with status / recipient / date-range filters.",
			InputSchema: InputSchema{
				Type: "object",
				Properties: map[string]PropertySchema{
					"page":      {Type: "integer", Description: "Page number (default 1)"},
					"limit":     {Type: "integer", Description: "Results per page (default 20, max 100)"},
					"status":    {Type: "string", Description: "Filter by status"},
					"recipient": {Type: "string", Description: "Filter by recipient email"},
					"from_date": {Type: "string", Description: "Filter from date (ISO 8601)"},
					"to_date":   {Type: "string", Description: "Filter to date (ISO 8601)"},
				},
				Required: []string{},
			},
		},
		{
			Name:           "email-job-get",
			OutputSchema:   objectOutputSchema(),
			SuperadminOnly: true,
			Description:    "Get a single email job by ID.",
			InputSchema: InputSchema{
				Type:       "object",
				Properties: map[string]PropertySchema{"job_id": {Type: "string", Description: "Email job ID (UUID)"}},
				Required:   []string{"job_id"},
			},
		},
		{
			Name:           "embedding-job-list",
			OutputSchema:   objectOutputSchema(),
			SuperadminOnly: true,
			Description:    "List graph and chunk embedding jobs with status / project / type filters.",
			InputSchema: InputSchema{
				Type: "object",
				Properties: map[string]PropertySchema{
					"page":       {Type: "integer", Description: "Page number (default 1)"},
					"limit":      {Type: "integer", Description: "Results per page (default 20, max 100)"},
					"status":     {Type: "string", Description: "Filter by status"},
					"has_error":  {Type: "boolean", Description: "Filter jobs with/without errors"},
					"project_id": {Type: "string", Description: "Filter by project ID (UUID)"},
					"job_type":   {Type: "string", Description: "Filter by job type (graph or chunk)"},
				},
				Required: []string{},
			},
		},
		{
			Name:           "embedding-job-stats",
			OutputSchema:   objectOutputSchema(),
			SuperadminOnly: true,
			Description:    "Aggregate statistics for graph and chunk embedding jobs.",
			InputSchema:    emptySuperadminInput(),
		},
		{
			Name:           "extraction-job-list",
			OutputSchema:   objectOutputSchema(),
			SuperadminOnly: true,
			Description:    "List object extraction jobs with status / type / project / error filters.",
			InputSchema: InputSchema{
				Type: "object",
				Properties: map[string]PropertySchema{
					"page":       {Type: "integer", Description: "Page number (default 1)"},
					"limit":      {Type: "integer", Description: "Results per page (default 20, max 100)"},
					"status":     {Type: "string", Description: "Filter by status"},
					"job_type":   {Type: "string", Description: "Filter by job type"},
					"project_id": {Type: "string", Description: "Filter by project ID (UUID)"},
					"has_error":  {Type: "boolean", Description: "Filter jobs with/without errors"},
				},
				Required: []string{},
			},
		},
		{
			Name:           "extraction-job-stats",
			OutputSchema:   objectOutputSchema(),
			SuperadminOnly: true,
			Description:    "Aggregate statistics for object extraction jobs.",
			InputSchema:    emptySuperadminInput(),
		},
		{
			Name:           "document-parsing-job-list",
			OutputSchema:   objectOutputSchema(),
			SuperadminOnly: true,
			Description:    "List document parsing jobs with status / project / error filters.",
			InputSchema: InputSchema{
				Type: "object",
				Properties: map[string]PropertySchema{
					"page":       {Type: "integer", Description: "Page number (default 1)"},
					"limit":      {Type: "integer", Description: "Results per page (default 20, max 100)"},
					"status":     {Type: "string", Description: "Filter by status"},
					"project_id": {Type: "string", Description: "Filter by project ID (UUID)"},
					"has_error":  {Type: "boolean", Description: "Filter jobs with/without errors"},
				},
				Required: []string{},
			},
		},
		{
			Name:           "document-parsing-job-stats",
			OutputSchema:   objectOutputSchema(),
			SuperadminOnly: true,
			Description:    "Aggregate statistics for document parsing jobs.",
			InputSchema:    emptySuperadminInput(),
		},

		// ---- mutations ----
		{
			Name:           "user-delete",
			OutputSchema:   objectOutputSchema(),
			SuperadminOnly: true,
			Description:    "Soft-delete a user (account deactivated, data retained). " + confirmDesc,
			InputSchema: InputSchema{
				Type: "object",
				Properties: map[string]PropertySchema{
					"user_id": {Type: "string", Description: "User ID (UUID)"},
					"confirm": {Type: "boolean", Description: confirmDesc},
				},
				Required: []string{"user_id"},
			},
		},
		{
			Name:           "org-delete",
			OutputSchema:   objectOutputSchema(),
			SuperadminOnly: true,
			Description:    "Soft-delete an organization. " + confirmDesc,
			InputSchema: InputSchema{
				Type: "object",
				Properties: map[string]PropertySchema{
					"org_id":  {Type: "string", Description: "Organization ID (UUID)"},
					"confirm": {Type: "boolean", Description: confirmDesc},
				},
				Required: []string{"org_id"},
			},
		},
		{
			Name:           "project-delete",
			OutputSchema:   objectOutputSchema(),
			SuperadminOnly: true,
			Description:    "Soft-delete a project. " + confirmDesc,
			InputSchema: InputSchema{
				Type: "object",
				Properties: map[string]PropertySchema{
					"project_id": {Type: "string", Description: "Project ID (UUID)"},
					"confirm":    {Type: "boolean", Description: confirmDesc},
				},
				Required: []string{"project_id"},
			},
		},
		{
			Name:           "superadmin-grant",
			OutputSchema:   objectOutputSchema(),
			SuperadminOnly: true,
			Description:    "Grant a user the superadmin_readonly platform role. " + confirmDesc,
			InputSchema: InputSchema{
				Type: "object",
				Properties: map[string]PropertySchema{
					"user_id": {Type: "string", Description: "User ID (UUID) to grant superadmin_readonly"},
					"notes":   {Type: "string", Description: "Optional notes"},
					"confirm": {Type: "boolean", Description: confirmDesc},
				},
				Required: []string{"user_id"},
			},
		},
		{
			Name:           "project-member-create",
			OutputSchema:   objectOutputSchema(),
			SuperadminOnly: true,
			Description:    "Add a user to a project (upserts the role if already a member). " + confirmDesc,
			InputSchema: InputSchema{
				Type: "object",
				Properties: map[string]PropertySchema{
					"project_id": {Type: "string", Description: "Project ID (UUID)"},
					"user_id":    {Type: "string", Description: "User ID (UUID)"},
					"role":       {Type: "string", Description: "Membership role"},
					"confirm":    {Type: "boolean", Description: confirmDesc},
				},
				Required: []string{"project_id", "user_id", "role"},
			},
		},
		{
			Name:           "project-member-delete",
			OutputSchema:   objectOutputSchema(),
			SuperadminOnly: true,
			Description:    "Remove a user from a project. " + confirmDesc,
			InputSchema: InputSchema{
				Type: "object",
				Properties: map[string]PropertySchema{
					"project_id": {Type: "string", Description: "Project ID (UUID)"},
					"user_id":    {Type: "string", Description: "User ID (UUID)"},
					"confirm":    {Type: "boolean", Description: confirmDesc},
				},
				Required: []string{"project_id", "user_id"},
			},
		},
		{
			Name:           "service-token-create",
			OutputSchema:   objectOutputSchema(),
			SuperadminOnly: true,
			Description:    "Provision a synthetic service account with superadmin_readonly and a persistent account-level API token (shown once). " + confirmDesc,
			InputSchema: InputSchema{
				Type: "object",
				Properties: map[string]PropertySchema{
					"name":    {Type: "string", Description: "Service token name"},
					"notes":   {Type: "string", Description: "Optional notes"},
					"confirm": {Type: "boolean", Description: confirmDesc},
				},
				Required: []string{"name"},
			},
		},
		{
			Name:           "embedding-job-delete",
			OutputSchema:   objectOutputSchema(),
			SuperadminOnly: true,
			Description:    "Bulk-delete graph/chunk embedding jobs by ID. " + confirmDesc,
			InputSchema: InputSchema{
				Type: "object",
				Properties: map[string]PropertySchema{
					"ids":      {Type: "array", Items: &PropertySchema{Type: "string"}, Description: "Embedding job IDs (UUIDs)"},
					"job_type": {Type: "string", Description: "Job type to delete (graph or chunk). Omit to default to graph."},
					"confirm":  {Type: "boolean", Description: confirmDesc},
				},
				Required: []string{"ids"},
			},
		},
		{
			Name:           "embedding-job-cleanup",
			OutputSchema:   objectOutputSchema(),
			SuperadminOnly: true,
			Description:    "Delete orphaned embedding jobs (jobs referencing deleted objects/chunks). " + confirmDesc,
			InputSchema: InputSchema{
				Type:       "object",
				Properties: map[string]PropertySchema{"confirm": {Type: "boolean", Description: confirmDesc}},
				Required:   []string{},
			},
		},
		{
			Name:           "embedding-job-reset",
			OutputSchema:   objectOutputSchema(),
			SuperadminOnly: true,
			Description:    "Reset all dead_letter graph embedding jobs back to pending. " + confirmDesc,
			InputSchema: InputSchema{
				Type:       "object",
				Properties: map[string]PropertySchema{"confirm": {Type: "boolean", Description: confirmDesc}},
				Required:   []string{},
			},
		},
		{
			Name:           "extraction-job-delete",
			OutputSchema:   objectOutputSchema(),
			SuperadminOnly: true,
			Description:    "Bulk-delete object extraction jobs by ID. " + confirmDesc,
			InputSchema: InputSchema{
				Type: "object",
				Properties: map[string]PropertySchema{
					"ids":     {Type: "array", Items: &PropertySchema{Type: "string"}, Description: "Extraction job IDs (UUIDs)"},
					"confirm": {Type: "boolean", Description: confirmDesc},
				},
				Required: []string{"ids"},
			},
		},
		{
			Name:           "extraction-job-cancel",
			OutputSchema:   objectOutputSchema(),
			SuperadminOnly: true,
			Description:    "Bulk-cancel queued/processing object extraction jobs by ID. " + confirmDesc,
			InputSchema: InputSchema{
				Type: "object",
				Properties: map[string]PropertySchema{
					"ids":     {Type: "array", Items: &PropertySchema{Type: "string"}, Description: "Extraction job IDs (UUIDs)"},
					"confirm": {Type: "boolean", Description: confirmDesc},
				},
				Required: []string{"ids"},
			},
		},
		{
			Name:           "document-parsing-job-delete",
			OutputSchema:   objectOutputSchema(),
			SuperadminOnly: true,
			Description:    "Bulk-delete document parsing jobs by ID. " + confirmDesc,
			InputSchema: InputSchema{
				Type: "object",
				Properties: map[string]PropertySchema{
					"ids":     {Type: "array", Items: &PropertySchema{Type: "string"}, Description: "Document parsing job IDs (UUIDs)"},
					"confirm": {Type: "boolean", Description: confirmDesc},
				},
				Required: []string{"ids"},
			},
		},
		{
			Name:           "document-parsing-job-retry",
			OutputSchema:   objectOutputSchema(),
			SuperadminOnly: true,
			Description:    "Bulk-retry failed/retry_pending document parsing jobs by ID. " + confirmDesc,
			InputSchema: InputSchema{
				Type: "object",
				Properties: map[string]PropertySchema{
					"ids":     {Type: "array", Items: &PropertySchema{Type: "string"}, Description: "Document parsing job IDs (UUIDs)"},
					"confirm": {Type: "boolean", Description: confirmDesc},
				},
				Required: []string{"ids"},
			},
		},
	}
}

// emptySuperadminInput returns the empty-object input schema shared by the
// no-argument operator tools.
func emptySuperadminInput() InputSchema {
	return InputSchema{Type: "object", Properties: map[string]PropertySchema{}, Required: []string{}}
}

// superadminOperatorToolNames is the set of NEW operator tool names defined by
// superadminToolDefinitions. It is derived (not hand-maintained) from the same
// definition list the admin catalog and dispatch consume.
var superadminOperatorToolNames = func() map[string]bool {
	names := make(map[string]bool)
	for _, def := range superadminToolDefinitions() {
		names[def.Name] = true
	}
	return names
}()

// superadminCatalogToolNames is the full operator catalog served on
// /api/mcp/admin: the NEW operator tools plus the pre-existing SuperadminOnly
// tools (embedding-*, provider-*, trace-*). It is the membership predicate for
// the admin endpoint (a project-scoped tool name is not in this set and is
// refused as method-not-found).
var superadminCatalogToolNames = func() map[string]bool {
	names := make(map[string]bool)
	for name := range superadminOperatorToolNames {
		names[name] = true
	}
	for _, build := range dynamicToolBuilders {
		for _, def := range build() {
			if def.SuperadminOnly {
				names[def.Name] = true
			}
		}
	}
	return names
}()

// IsAdminOperatorTool reports whether the named tool is part of the operator
// catalog served on /api/mcp/admin.
func (s *Service) IsAdminOperatorTool(name string) bool {
	return superadminCatalogToolNames[name]
}

// superadminMutationToolNames is the authoritative set of operator tools that
// perform a mutation and therefore require confirm=true. It covers the NEW
// mutation tools (whose handlers enforce confirm after identifier validation)
// and the pre-existing SuperadminOnly mutators (which have no internal confirm
// guard, so ExecuteSuperadminTool applies the guard for them directly).
var superadminMutationToolNames = map[string]bool{
	// pre-existing SuperadminOnly mutators (no internal confirm guard)
	"provider-configure-org":  true,
	"embedding-pause":         true,
	"embedding-resume":        true,
	"embedding-config-update": true,
	// new operator mutations (confirm enforced in their handlers)
	"user-delete":                 true,
	"org-delete":                  true,
	"project-delete":              true,
	"superadmin-grant":            true,
	"project-member-create":       true,
	"project-member-delete":       true,
	"service-token-create":        true,
	"embedding-job-delete":        true,
	"embedding-job-cleanup":       true,
	"embedding-job-reset":         true,
	"extraction-job-delete":       true,
	"extraction-job-cancel":       true,
	"document-parsing-job-delete": true,
	"document-parsing-job-retry":  true,
}

// superadminPreExistingMutationDryRun returns the structured dry-run for a
// pre-existing SuperadminOnly mutator. It reuses superadminDryRun so the shape
// matches the new-tool dry-run (dry_run, action, targets, message).
func (s *Service) superadminPreExistingMutationDryRun(toolName string, args map[string]any) (*ToolResult, error) {
	switch toolName {
	case "provider-configure-org":
		return s.superadminDryRun("configure organization provider", map[string]any{
			"org_id":        superadminArgString(args, "org_id"),
			"provider_name": superadminArgString(args, "provider_name"),
		})
	case "embedding-pause":
		return s.superadminDryRun("pause all embedding workers", map[string]any{})
	case "embedding-resume":
		return s.superadminDryRun("resume all embedding workers", map[string]any{})
	case "embedding-config-update":
		return s.superadminDryRun("update embedding worker configuration", map[string]any{})
	default:
		return nil, fmt.Errorf("superadmin mutation dry-run not defined for %s", toolName)
	}
}

// GetSuperadminToolDefinitions returns the operator catalog served on the
// /api/mcp/admin endpoint: the NEW operator tools plus the pre-existing
// deployment-wide SuperadminOnly tools (embedding-*, provider-*, trace-*).
func (s *Service) GetSuperadminToolDefinitions() []ToolDefinition {
	tools := append([]ToolDefinition(nil), superadminToolDefinitions()...)
	for _, build := range dynamicToolBuilders {
		for _, def := range build() {
			if def.SuperadminOnly {
				tools = append(tools, def)
			}
		}
	}
	return tools
}

// ExecuteSuperadminTool executes an operator tool on the admin endpoint. It
// applies the same fail-closed superadmin_full gate as the in-process path and
// routes the NEW operator tools to dispatchSuperadminTool and the pre-existing
// SuperadminOnly tools through ExecuteTool's dispatch.
func (s *Service) ExecuteSuperadminTool(ctx context.Context, toolName string, args map[string]any) (*ToolResult, error) {
	if !s.IsAdminOperatorTool(toolName) {
		return nil, fmt.Errorf("tool not found: %s", toolName)
	}
	ok, err := s.IsSuperadminCaller(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to authorize tool %q: %w", toolName, err)
	}
	if !ok {
		return nil, fmt.Errorf("tool %q requires superadmin privileges", toolName)
	}
	if superadminOperatorToolNames[toolName] {
		return s.dispatchSuperadminTool(ctx, toolName, args)
	}
	// Pre-existing SuperadminOnly tools. Apply the confirm guard to the mutators
	// (the new tools above enforce confirm in their own handlers).
	if superadminMutationToolNames[toolName] && !s.superadminConfirm(args) {
		return s.superadminPreExistingMutationDryRun(toolName, args)
	}
	return s.dispatchTool(ctx, "", toolName, args)
}

// dispatchSuperadminTool routes a NEW operator tool name to its handler. It
// contains no authority checks — ExecuteSuperadminTool applies those first.
func (s *Service) dispatchSuperadminTool(ctx context.Context, toolName string, args map[string]any) (*ToolResult, error) {
	switch toolName {
	case "superadmin-get":
		return s.executeSuperadminGet(ctx, args)
	case "user-list":
		return s.executeSuperadminUserList(ctx, args)
	case "user-get":
		return s.executeSuperadminUserGet(ctx, args)
	case "org-list":
		return s.executeSuperadminOrgList(ctx, args)
	case "org-get":
		return s.executeSuperadminOrgGet(ctx, args)
	case "project-list":
		return s.executeSuperadminProjectList(ctx, args)
	case "project-record-get":
		return s.executeSuperadminProjectGet(ctx, args)
	case "project-member-list":
		return s.executeSuperadminProjectMemberList(ctx, args)
	case "email-job-list":
		return s.executeSuperadminEmailJobList(ctx, args)
	case "email-job-get":
		return s.executeSuperadminEmailJobGet(ctx, args)
	case "embedding-job-list":
		return s.executeSuperadminEmbeddingJobList(ctx, args)
	case "embedding-job-stats":
		return s.executeSuperadminEmbeddingJobStats(ctx, args)
	case "extraction-job-list":
		return s.executeSuperadminExtractionJobList(ctx, args)
	case "extraction-job-stats":
		return s.executeSuperadminExtractionJobStats(ctx, args)
	case "document-parsing-job-list":
		return s.executeSuperadminDocumentParsingJobList(ctx, args)
	case "document-parsing-job-stats":
		return s.executeSuperadminDocumentParsingJobStats(ctx, args)
	case "user-delete":
		return s.executeSuperadminUserDelete(ctx, args)
	case "org-delete":
		return s.executeSuperadminOrgDelete(ctx, args)
	case "project-delete":
		return s.executeSuperadminProjectDelete(ctx, args)
	case "superadmin-grant":
		return s.executeSuperadminGrant(ctx, args)
	case "project-member-create":
		return s.executeSuperadminProjectMemberCreate(ctx, args)
	case "project-member-delete":
		return s.executeSuperadminProjectMemberDelete(ctx, args)
	case "service-token-create":
		return s.executeSuperadminServiceTokenCreate(ctx, args)
	case "embedding-job-delete":
		return s.executeSuperadminEmbeddingJobDelete(ctx, args)
	case "embedding-job-cleanup":
		return s.executeSuperadminEmbeddingJobCleanup(ctx, args)
	case "embedding-job-reset":
		return s.executeSuperadminEmbeddingJobReset(ctx, args)
	case "extraction-job-delete":
		return s.executeSuperadminExtractionJobDelete(ctx, args)
	case "extraction-job-cancel":
		return s.executeSuperadminExtractionJobCancel(ctx, args)
	case "document-parsing-job-delete":
		return s.executeSuperadminDocumentParsingJobDelete(ctx, args)
	case "document-parsing-job-retry":
		return s.executeSuperadminDocumentParsingJobRetry(ctx, args)
	default:
		return nil, fmt.Errorf("tool not found: %s", toolName)
	}
}

// ============================================================================
// Read handlers
// ============================================================================

func (s *Service) superadminHandler() (SuperadminToolHandler, error) {
	if s.superadminToolHandler == nil {
		return nil, errSuperadminNotAvailable
	}
	return s.superadminToolHandler, nil
}

func (s *Service) executeSuperadminGet(ctx context.Context, _ map[string]any) (*ToolResult, error) {
	h, err := s.superadminHandler()
	if err != nil {
		return nil, err
	}
	res, err := h.SuperadminGet(ctx)
	if err != nil {
		return nil, fmt.Errorf("superadmin_get: %w", err)
	}
	return s.wrapResult(res)
}

func (s *Service) executeSuperadminUserList(ctx context.Context, args map[string]any) (*ToolResult, error) {
	h, err := s.superadminHandler()
	if err != nil {
		return nil, err
	}
	res, err := h.ListUsers(ctx, superadminArgPage(args), superadminArgLimit(args), superadminArgString(args, "search"), superadminArgString(args, "org_id"))
	if err != nil {
		return nil, fmt.Errorf("user_list: %w", err)
	}
	return s.wrapResult(res)
}

func (s *Service) executeSuperadminUserGet(ctx context.Context, args map[string]any) (*ToolResult, error) {
	h, err := s.superadminHandler()
	if err != nil {
		return nil, err
	}
	userID := superadminArgString(args, "user_id")
	if userID == "" {
		return nil, fmt.Errorf("user_get: 'user_id' is required")
	}
	res, err := h.GetUser(ctx, userID)
	if err != nil {
		return nil, fmt.Errorf("user_get: %w", err)
	}
	return s.wrapResult(map[string]any{"user": res})
}

func (s *Service) executeSuperadminOrgList(ctx context.Context, args map[string]any) (*ToolResult, error) {
	h, err := s.superadminHandler()
	if err != nil {
		return nil, err
	}
	res, err := h.ListOrganizations(ctx, superadminArgPage(args), superadminArgLimit(args))
	if err != nil {
		return nil, fmt.Errorf("org_list: %w", err)
	}
	return s.wrapResult(res)
}

func (s *Service) executeSuperadminOrgGet(ctx context.Context, args map[string]any) (*ToolResult, error) {
	h, err := s.superadminHandler()
	if err != nil {
		return nil, err
	}
	orgID := superadminArgString(args, "org_id")
	if orgID == "" {
		return nil, fmt.Errorf("org_get: 'org_id' is required")
	}
	res, err := h.GetOrg(ctx, orgID)
	if err != nil {
		return nil, fmt.Errorf("org_get: %w", err)
	}
	return s.wrapResult(map[string]any{"organization": res})
}

func (s *Service) executeSuperadminProjectList(ctx context.Context, args map[string]any) (*ToolResult, error) {
	h, err := s.superadminHandler()
	if err != nil {
		return nil, err
	}
	res, err := h.ListProjects(ctx, superadminArgPage(args), superadminArgLimit(args), superadminArgString(args, "org_id"))
	if err != nil {
		return nil, fmt.Errorf("project_list: %w", err)
	}
	return s.wrapResult(res)
}

func (s *Service) executeSuperadminProjectGet(ctx context.Context, args map[string]any) (*ToolResult, error) {
	h, err := s.superadminHandler()
	if err != nil {
		return nil, err
	}
	projectID := superadminArgString(args, "project_id")
	if projectID == "" {
		return nil, fmt.Errorf("project_record_get: 'project_id' is required")
	}
	res, err := h.GetProject(ctx, projectID)
	if err != nil {
		return nil, fmt.Errorf("project_record_get: %w", err)
	}
	return s.wrapResult(map[string]any{"project": res})
}

func (s *Service) executeSuperadminProjectMemberList(ctx context.Context, args map[string]any) (*ToolResult, error) {
	h, err := s.superadminHandler()
	if err != nil {
		return nil, err
	}
	projectID := superadminArgString(args, "project_id")
	if projectID == "" {
		return nil, fmt.Errorf("project_member_list: 'project_id' is required")
	}
	res, err := h.ListProjectMembers(ctx, projectID)
	if err != nil {
		return nil, fmt.Errorf("project_member_list: %w", err)
	}
	return s.wrapResult(res)
}

func (s *Service) executeSuperadminEmailJobList(ctx context.Context, args map[string]any) (*ToolResult, error) {
	h, err := s.superadminHandler()
	if err != nil {
		return nil, err
	}
	res, err := h.ListEmailJobs(ctx, superadminArgPage(args), superadminArgLimit(args),
		superadminArgString(args, "status"), superadminArgString(args, "recipient"),
		superadminArgString(args, "from_date"), superadminArgString(args, "to_date"))
	if err != nil {
		return nil, fmt.Errorf("email_job_list: %w", err)
	}
	return s.wrapResult(res)
}

func (s *Service) executeSuperadminEmailJobGet(ctx context.Context, args map[string]any) (*ToolResult, error) {
	h, err := s.superadminHandler()
	if err != nil {
		return nil, err
	}
	jobID := superadminArgString(args, "job_id")
	if jobID == "" {
		return nil, fmt.Errorf("email_job_get: 'job_id' is required")
	}
	res, err := h.GetEmailJob(ctx, jobID)
	if err != nil {
		return nil, fmt.Errorf("email_job_get: %w", err)
	}
	return s.wrapResult(map[string]any{"email_job": res})
}

func (s *Service) executeSuperadminEmbeddingJobList(ctx context.Context, args map[string]any) (*ToolResult, error) {
	h, err := s.superadminHandler()
	if err != nil {
		return nil, err
	}
	res, err := h.ListEmbeddingJobs(ctx, superadminArgPage(args), superadminArgLimit(args),
		superadminArgString(args, "status"), superadminArgBoolPtr(args, "has_error"),
		superadminArgString(args, "project_id"), superadminArgString(args, "job_type"))
	if err != nil {
		return nil, fmt.Errorf("embedding_job_list: %w", err)
	}
	return s.wrapResult(res)
}

func (s *Service) executeSuperadminEmbeddingJobStats(ctx context.Context, _ map[string]any) (*ToolResult, error) {
	h, err := s.superadminHandler()
	if err != nil {
		return nil, err
	}
	res, err := h.GetEmbeddingJobStats(ctx)
	if err != nil {
		return nil, fmt.Errorf("embedding_job_stats: %w", err)
	}
	return s.wrapResult(res)
}

func (s *Service) executeSuperadminExtractionJobList(ctx context.Context, args map[string]any) (*ToolResult, error) {
	h, err := s.superadminHandler()
	if err != nil {
		return nil, err
	}
	res, err := h.ListExtractionJobs(ctx, superadminArgPage(args), superadminArgLimit(args),
		superadminArgString(args, "status"), superadminArgString(args, "job_type"),
		superadminArgString(args, "project_id"), superadminArgBoolPtr(args, "has_error"))
	if err != nil {
		return nil, fmt.Errorf("extraction_job_list: %w", err)
	}
	return s.wrapResult(res)
}

func (s *Service) executeSuperadminExtractionJobStats(ctx context.Context, _ map[string]any) (*ToolResult, error) {
	h, err := s.superadminHandler()
	if err != nil {
		return nil, err
	}
	res, err := h.GetExtractionJobStats(ctx)
	if err != nil {
		return nil, fmt.Errorf("extraction_job_stats: %w", err)
	}
	return s.wrapResult(res)
}

func (s *Service) executeSuperadminDocumentParsingJobList(ctx context.Context, args map[string]any) (*ToolResult, error) {
	h, err := s.superadminHandler()
	if err != nil {
		return nil, err
	}
	res, err := h.ListDocumentParsingJobs(ctx, superadminArgPage(args), superadminArgLimit(args),
		superadminArgString(args, "status"), superadminArgString(args, "project_id"), superadminArgBoolPtr(args, "has_error"))
	if err != nil {
		return nil, fmt.Errorf("document_parsing_job_list: %w", err)
	}
	return s.wrapResult(res)
}

func (s *Service) executeSuperadminDocumentParsingJobStats(ctx context.Context, _ map[string]any) (*ToolResult, error) {
	h, err := s.superadminHandler()
	if err != nil {
		return nil, err
	}
	res, err := h.GetDocumentParsingJobStats(ctx)
	if err != nil {
		return nil, fmt.Errorf("document_parsing_job_stats: %w", err)
	}
	return s.wrapResult(res)
}

// ============================================================================
// Mutating handlers (confirm-guarded)
// ============================================================================

func (s *Service) superadminDryRun(action string, targets map[string]any) (*ToolResult, error) {
	return s.wrapResult(map[string]any{
		"dry_run": true,
		"action":  action,
		"targets": targets,
		"message": fmt.Sprintf("dry run: %s — set confirm=true to apply", action),
	})
}

func (s *Service) superadminConfirm(args map[string]any) bool {
	return superadminArgBool(args, "confirm")
}

func (s *Service) executeSuperadminUserDelete(ctx context.Context, args map[string]any) (*ToolResult, error) {
	h, err := s.superadminHandler()
	if err != nil {
		return nil, err
	}
	userID := superadminArgString(args, "user_id")
	if userID == "" {
		return nil, fmt.Errorf("user_delete: 'user_id' is required")
	}
	if !s.superadminConfirm(args) {
		return s.superadminDryRun("delete user", map[string]any{"user_id": userID})
	}
	if err := h.SoftDeleteUser(ctx, userID, superadminActorID(ctx)); err != nil {
		return nil, fmt.Errorf("user_delete: %w", err)
	}
	return s.wrapResult(map[string]any{"success": true, "message": "user deleted", "user_id": userID})
}

func (s *Service) executeSuperadminOrgDelete(ctx context.Context, args map[string]any) (*ToolResult, error) {
	h, err := s.superadminHandler()
	if err != nil {
		return nil, err
	}
	orgID := superadminArgString(args, "org_id")
	if orgID == "" {
		return nil, fmt.Errorf("org_delete: 'org_id' is required")
	}
	if !s.superadminConfirm(args) {
		return s.superadminDryRun("delete organization", map[string]any{"org_id": orgID})
	}
	if err := h.SoftDeleteOrg(ctx, orgID, superadminActorID(ctx)); err != nil {
		return nil, fmt.Errorf("org_delete: %w", err)
	}
	return s.wrapResult(map[string]any{"success": true, "message": "organization deleted", "org_id": orgID})
}

func (s *Service) executeSuperadminProjectDelete(ctx context.Context, args map[string]any) (*ToolResult, error) {
	h, err := s.superadminHandler()
	if err != nil {
		return nil, err
	}
	projectID := superadminArgString(args, "project_id")
	if projectID == "" {
		return nil, fmt.Errorf("project_delete: 'project_id' is required")
	}
	if !s.superadminConfirm(args) {
		return s.superadminDryRun("delete project", map[string]any{"project_id": projectID})
	}
	if err := h.SoftDeleteProject(ctx, projectID, superadminActorID(ctx)); err != nil {
		return nil, fmt.Errorf("project_delete: %w", err)
	}
	return s.wrapResult(map[string]any{"success": true, "message": "project deleted", "project_id": projectID})
}

func (s *Service) executeSuperadminGrant(ctx context.Context, args map[string]any) (*ToolResult, error) {
	h, err := s.superadminHandler()
	if err != nil {
		return nil, err
	}
	userID := superadminArgString(args, "user_id")
	if userID == "" {
		return nil, fmt.Errorf("superadmin_grant: 'user_id' is required")
	}
	if !s.superadminConfirm(args) {
		return s.superadminDryRun("grant superadmin_readonly", map[string]any{"user_id": userID})
	}
	var notes *string
	if n := superadminArgString(args, "notes"); n != "" {
		notes = &n
	}
	if err := h.GrantSuperadmin(ctx, userID, superadminActorID(ctx), notes); err != nil {
		return nil, fmt.Errorf("superadmin_grant: %w", err)
	}
	return s.wrapResult(map[string]any{"success": true, "message": "superadmin_readonly granted", "user_id": userID})
}

func (s *Service) executeSuperadminProjectMemberCreate(ctx context.Context, args map[string]any) (*ToolResult, error) {
	h, err := s.superadminHandler()
	if err != nil {
		return nil, err
	}
	projectID := superadminArgString(args, "project_id")
	userID := superadminArgString(args, "user_id")
	role := superadminArgString(args, "role")
	if projectID == "" || userID == "" || role == "" {
		return nil, fmt.Errorf("project_member_create: 'project_id', 'user_id' and 'role' are required")
	}
	if !s.superadminConfirm(args) {
		return s.superadminDryRun("add project member", map[string]any{"project_id": projectID, "user_id": userID, "role": role})
	}
	if err := h.AddProjectMember(ctx, projectID, userID, role); err != nil {
		return nil, fmt.Errorf("project_member_create: %w", err)
	}
	return s.wrapResult(map[string]any{"success": true, "message": "project member added", "project_id": projectID, "user_id": userID, "role": role})
}

func (s *Service) executeSuperadminProjectMemberDelete(ctx context.Context, args map[string]any) (*ToolResult, error) {
	h, err := s.superadminHandler()
	if err != nil {
		return nil, err
	}
	projectID := superadminArgString(args, "project_id")
	userID := superadminArgString(args, "user_id")
	if projectID == "" || userID == "" {
		return nil, fmt.Errorf("project_member_delete: 'project_id' and 'user_id' are required")
	}
	if !s.superadminConfirm(args) {
		return s.superadminDryRun("remove project member", map[string]any{"project_id": projectID, "user_id": userID})
	}
	if err := h.RemoveProjectMember(ctx, projectID, userID); err != nil {
		return nil, fmt.Errorf("project_member_delete: %w", err)
	}
	return s.wrapResult(map[string]any{"success": true, "message": "project member removed", "project_id": projectID, "user_id": userID})
}

func (s *Service) executeSuperadminServiceTokenCreate(ctx context.Context, args map[string]any) (*ToolResult, error) {
	h, err := s.superadminHandler()
	if err != nil {
		return nil, err
	}
	name := superadminArgString(args, "name")
	if name == "" {
		return nil, fmt.Errorf("service_token_create: 'name' is required")
	}
	if !s.superadminConfirm(args) {
		return s.superadminDryRun("create service token", map[string]any{"name": name})
	}
	var notes *string
	if n := superadminArgString(args, "notes"); n != "" {
		notes = &n
	}
	res, err := h.CreateServiceToken(ctx, name, notes, superadminActorID(ctx))
	if err != nil {
		return nil, fmt.Errorf("service_token_create: %w", err)
	}
	return s.wrapResult(res)
}

func (s *Service) executeSuperadminEmbeddingJobDelete(ctx context.Context, args map[string]any) (*ToolResult, error) {
	h, err := s.superadminHandler()
	if err != nil {
		return nil, err
	}
	ids := superadminArgStringSlice(args, "ids")
	if len(ids) == 0 {
		return nil, fmt.Errorf("embedding_job_delete: 'ids' is required")
	}
	if !s.superadminConfirm(args) {
		return s.superadminDryRun("delete embedding jobs", map[string]any{"ids": ids})
	}
	n, err := h.DeleteEmbeddingJobs(ctx, ids, superadminArgString(args, "job_type"))
	if err != nil {
		return nil, fmt.Errorf("embedding_job_delete: %w", err)
	}
	return s.wrapResult(map[string]any{"success": true, "deleted_count": n, "message": "embedding jobs deleted"})
}

func (s *Service) executeSuperadminEmbeddingJobCleanup(ctx context.Context, args map[string]any) (*ToolResult, error) {
	h, err := s.superadminHandler()
	if err != nil {
		return nil, err
	}
	if !s.superadminConfirm(args) {
		return s.superadminDryRun("cleanup orphan embedding jobs", map[string]any{})
	}
	n, err := h.CleanupEmbeddingJobs(ctx)
	if err != nil {
		return nil, fmt.Errorf("embedding_job_cleanup: %w", err)
	}
	return s.wrapResult(map[string]any{"success": true, "deleted_count": n, "message": "orphan embedding jobs cleaned up"})
}

func (s *Service) executeSuperadminEmbeddingJobReset(ctx context.Context, args map[string]any) (*ToolResult, error) {
	h, err := s.superadminHandler()
	if err != nil {
		return nil, err
	}
	if !s.superadminConfirm(args) {
		return s.superadminDryRun("reset dead-letter embedding jobs", map[string]any{})
	}
	n, err := h.ResetEmbeddingJobs(ctx)
	if err != nil {
		return nil, fmt.Errorf("embedding_job_reset: %w", err)
	}
	return s.wrapResult(map[string]any{"success": true, "reset_count": n, "message": "dead-letter embedding jobs reset"})
}

func (s *Service) executeSuperadminExtractionJobDelete(ctx context.Context, args map[string]any) (*ToolResult, error) {
	h, err := s.superadminHandler()
	if err != nil {
		return nil, err
	}
	ids := superadminArgStringSlice(args, "ids")
	if len(ids) == 0 {
		return nil, fmt.Errorf("extraction_job_delete: 'ids' is required")
	}
	if !s.superadminConfirm(args) {
		return s.superadminDryRun("delete extraction jobs", map[string]any{"ids": ids})
	}
	n, err := h.DeleteExtractionJobs(ctx, ids)
	if err != nil {
		return nil, fmt.Errorf("extraction_job_delete: %w", err)
	}
	return s.wrapResult(map[string]any{"success": true, "deleted_count": n, "message": "extraction jobs deleted"})
}

func (s *Service) executeSuperadminExtractionJobCancel(ctx context.Context, args map[string]any) (*ToolResult, error) {
	h, err := s.superadminHandler()
	if err != nil {
		return nil, err
	}
	ids := superadminArgStringSlice(args, "ids")
	if len(ids) == 0 {
		return nil, fmt.Errorf("extraction_job_cancel: 'ids' is required")
	}
	if !s.superadminConfirm(args) {
		return s.superadminDryRun("cancel extraction jobs", map[string]any{"ids": ids})
	}
	n, err := h.CancelExtractionJobs(ctx, ids)
	if err != nil {
		return nil, fmt.Errorf("extraction_job_cancel: %w", err)
	}
	return s.wrapResult(map[string]any{"success": true, "cancelled_count": n, "message": "extraction jobs cancelled"})
}

func (s *Service) executeSuperadminDocumentParsingJobDelete(ctx context.Context, args map[string]any) (*ToolResult, error) {
	h, err := s.superadminHandler()
	if err != nil {
		return nil, err
	}
	ids := superadminArgStringSlice(args, "ids")
	if len(ids) == 0 {
		return nil, fmt.Errorf("document_parsing_job_delete: 'ids' is required")
	}
	if !s.superadminConfirm(args) {
		return s.superadminDryRun("delete document parsing jobs", map[string]any{"ids": ids})
	}
	n, err := h.DeleteDocumentParsingJobs(ctx, ids)
	if err != nil {
		return nil, fmt.Errorf("document_parsing_job_delete: %w", err)
	}
	return s.wrapResult(map[string]any{"success": true, "deleted_count": n, "message": "document parsing jobs deleted"})
}

func (s *Service) executeSuperadminDocumentParsingJobRetry(ctx context.Context, args map[string]any) (*ToolResult, error) {
	h, err := s.superadminHandler()
	if err != nil {
		return nil, err
	}
	ids := superadminArgStringSlice(args, "ids")
	if len(ids) == 0 {
		return nil, fmt.Errorf("document_parsing_job_retry: 'ids' is required")
	}
	if !s.superadminConfirm(args) {
		return s.superadminDryRun("retry document parsing jobs", map[string]any{"ids": ids})
	}
	n, err := h.RetryDocumentParsingJobs(ctx, ids)
	if err != nil {
		return nil, fmt.Errorf("document_parsing_job_retry: %w", err)
	}
	return s.wrapResult(map[string]any{"success": true, "retried_count": n, "message": "document parsing jobs queued for retry"})
}

// ============================================================================
// Arg helpers
// ============================================================================

func superadminActorID(ctx context.Context) string {
	if u := auth.UserFromContext(ctx); u != nil {
		return u.ID
	}
	return ""
}

func superadminArgString(args map[string]any, key string) string {
	v, _ := args[key].(string)
	return v
}

func superadminArgInt(args map[string]any, key string, def int) int {
	switch v := args[key].(type) {
	case float64:
		if v != 0 {
			return int(v)
		}
	case int:
		if v != 0 {
			return v
		}
	case int64:
		if v != 0 {
			return int(v)
		}
	}
	return def
}

// superadminArgPage clamps the page number to >= 1 (default 1), matching the
// superadmin HTTP handler convention.
func superadminArgPage(args map[string]any) int {
	p := superadminArgInt(args, "page", 1)
	if p < 1 {
		return 1
	}
	return p
}

// superadminArgLimit clamps the page size to [1..100] (default 20), matching the
// superadmin HTTP handler convention.
func superadminArgLimit(args map[string]any) int {
	l := superadminArgInt(args, "limit", 20)
	if l < 1 {
		return 1
	}
	if l > 100 {
		return 100
	}
	return l
}

func superadminArgBool(args map[string]any, key string) bool {
	v, _ := args[key].(bool)
	return v
}

// superadminArgBoolPtr returns nil when the arg is absent, else a pointer to
// the parsed boolean. It mirrors the HTTP query `hasError` optional param.
func superadminArgBoolPtr(args map[string]any, key string) *bool {
	raw, ok := args[key]
	if !ok {
		return nil
	}
	b, ok := raw.(bool)
	if !ok {
		return nil
	}
	return &b
}

func superadminArgStringSlice(args map[string]any, key string) []string {
	raw, ok := args[key].([]any)
	if !ok {
		// tolerate a single string for convenience (some clients send one id)
		if s, ok := args[key].(string); ok && s != "" {
			return []string{s}
		}
		return nil
	}
	out := make([]string, 0, len(raw))
	for _, it := range raw {
		if s, ok := it.(string); ok && s != "" {
			out = append(out, s)
		}
	}
	return out
}
