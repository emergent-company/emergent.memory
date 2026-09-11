package mcp

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/lib/pq"
	"github.com/uptrace/bun"

	"github.com/emergent-company/emergent.memory/domain/apitoken"
	"github.com/emergent-company/emergent.memory/pkg/acpslug"
	"github.com/emergent-company/emergent.memory/pkg/apperror"
)

// ============================================================================
// Constants
// ============================================================================

// legacyShareNamePrefixes are the token-name prefixes used by the legacy
// POST /api/projects/:projectId/mcp/share flow. Tokens matching these prefixes
// but with no bound instance row are surfaced as read-only legacy instances.
var legacyShareNamePrefixes = []string{"MCP Read-Only Share", "MCP Share →"}

// instanceScopeBaseline is always granted to a share-instance token so the
// project context can be resolved even for tools with no required scope.
const instanceScopeBaseline = "projects:read"

// agentExecutionTools start an agent run. Their side effects execute outside the
// share-instance request context (via the ADK ToolPool), so they cannot be
// safely constrained by a static per-instance tool allowlist and are rejected
// from any non-null allowlist.
var agentExecutionTools = map[string]bool{
	"trigger_agent":   true,
	"acp-trigger-run": true,
}

// agentMutationTools create/update/delete agents, definitions, hooks, or answer
// agent questions. They can affect agents outside an instance's agent allowlist
// and are rejected from any non-null tool allowlist.
var agentMutationTools = map[string]bool{
	"agent-create":            true,
	"update_agent":            true,
	"agent-delete":            true,
	"agent-def-create":        true,
	"update_agent_definition": true,
	"agent-def-delete":        true,
	"agent-hook-create":       true,
	"agent-hook-delete":       true,
	"agent-question-respond":  true,
}

// agentRunInspectionTools read run data by run ID and cannot be mapped to the
// runtime-agent allowlist on the hot path, so they are hidden/denied whenever an
// agent allowlist is active.
var agentRunInspectionTools = map[string]bool{
	"agent-run-list":              true,
	"agent-run-get":               true,
	"agent-run-messages":          true,
	"agent-run-tool-calls":        true,
	"agent-run-status":            true,
	"acp-get-run-status":          true,
	"acp-get-run-events":          true,
	"remember-status":             true,
	"agent-question-list":         true,
	"agent-question-list-project": true,
	"agent-hook-list":             true,
	"adk-session-list":            true,
	"adk-session-get":             true,
}

// agentAllowlistDeniedTools are tools that must not be visible or callable when
// an instance has an agent allowlist (fail closed). Agent-definition read tools
// are handled by result filtering instead because they can be mapped to allowed
// agent names; mutations and run-inspection cannot be mapped reliably.
func agentAllowlistDeniedTools(toolName string) bool {
	return agentRunInspectionTools[toolName] || agentMutationTools[toolName]
}

// ============================================================================
// Interfaces (test seams + default Bun implementations live below)
// ============================================================================

// shareTokenService is the subset of apitoken.Service needed for share-instance
// credential lifecycle. *apitoken.Service satisfies it.
type shareTokenService interface {
	Create(ctx context.Context, projectID, userID, name string, scopes []string) (*apitoken.CreateApiTokenResponseDTO, error)
	UpdateScopes(ctx context.Context, tokenID, projectID, userID string, scopes []string) (*apitoken.ApiTokenDTO, error)
	Revoke(ctx context.Context, tokenID, projectID, userID string) error
	Regenerate(ctx context.Context, tokenID, projectID, userID string) (*apitoken.CreateApiTokenResponseDTO, error)
	// RegenerateWith additionally runs after inside the same transaction with
	// the new token ID, so callers can update related rows atomically.
	RegenerateWith(ctx context.Context, tokenID, projectID, userID string, after func(ctx context.Context, tx bun.Tx, newTokenID string) error) (*apitoken.CreateApiTokenResponseDTO, error)
	GetUserProjectRole(ctx context.Context, projectID, userID string) (string, error)
}

// shareInstanceStore persists MCPShareInstance rows.
type shareInstanceStore interface {
	ListByProject(ctx context.Context, projectID string) ([]*MCPShareInstance, error)
	GetByID(ctx context.Context, projectID, id string) (*MCPShareInstance, error)
	GetByTokenID(ctx context.Context, tokenID string) (*MCPShareInstance, error)
	FindByName(ctx context.Context, projectID, name string) (*MCPShareInstance, error)
	Create(ctx context.Context, inst *MCPShareInstance) error
	Update(ctx context.Context, inst *MCPShareInstance) error
	ListLegacyTokens(ctx context.Context, projectID string) ([]*LegacyTokenRef, error)
}

// AgentRef is a minimal project-agent reference used for allowlist validation
// and filtering.
type AgentRef struct {
	ID      string
	Name    string
	Enabled bool
}

// agentDirectory resolves project agents. Implemented by Bun and injectable in
// tests.
type agentDirectory interface {
	ListProjectAgents(ctx context.Context, projectID string) ([]AgentRef, error)
	// FindProjectAgentByID returns one project agent by ID, or nil when it does
	// not exist in the project.
	FindProjectAgentByID(ctx context.Context, projectID, id string) (*AgentRef, error)
	FindAgentIDByName(ctx context.Context, projectID, name string) (string, bool, error)
	// FindAgentIDByNameOrSlug resolves either the raw kb.agents.name or its
	// ACP slug (agents.ACPSlugFromName). Used to gate acp-trigger-run, whose
	// agent_name argument is a slug rather than the raw name.
	FindAgentIDByNameOrSlug(ctx context.Context, projectID, nameOrSlug string) (string, bool, error)
}

// ============================================================================
// Model
// ============================================================================

// MCPShareInstance binds one core.api_tokens credential to a named, optional
// tool and agent allowlist. NULL allowlists mean "unrestricted" (all
// scope-permitted entries), matching legacy share tokens.
type MCPShareInstance struct {
	bun.BaseModel `bun:"table:core.mcp_share_instances,alias:msi"`

	ID            string      `bun:"id,pk,type:uuid,default:gen_random_uuid()"`
	ProjectID     string      `bun:"project_id,type:uuid,notnull"`
	Name          string      `bun:"name,notnull"`
	Description   *string     `bun:"description"`
	TokenID       string      `bun:"token_id,type:uuid,notnull"`
	AllowedTools  []string    `bun:"allowed_tools,array"`
	AllowedAgents []uuid.UUID `bun:"allowed_agents,type:uuid[]"`
	IsLegacy      bool        `bun:"is_legacy,notnull,default:false"`
	CreatedBy     *string     `bun:"created_by,type:uuid"`
	CreatedAt     time.Time   `bun:"created_at,notnull,default:now()"`
	UpdatedAt     time.Time   `bun:"updated_at,notnull,default:now()"`
	RevokedAt     *time.Time  `bun:"revoked_at"`

	// Transient fields populated by read queries that join core.api_tokens.
	TokenLastUsedAt *time.Time `bun:"token_last_used_at,scanonly"`
	TokenRevokedAt  *time.Time `bun:"token_revoked_at,scanonly"`
	TokenExpiresAt  *time.Time `bun:"token_expires_at,scanonly"`
}

// LegacyTokenRef is a view of an api_tokens row surfaced as a legacy instance.
type LegacyTokenRef struct {
	ID         string     `bun:"id"`
	Name       string     `bun:"name"`
	Scopes     []string   `bun:"scopes,array"`
	CreatedAt  time.Time  `bun:"created_at"`
	LastUsedAt *time.Time `bun:"last_used_at"`
	RevokedAt  *time.Time `bun:"revoked_at"`
	ExpiresAt  *time.Time `bun:"expires_at"`
}

// InstanceScope is the per-request allowlist view resolved from an API token.
// A nil *InstanceScope (or one with neither allowlist) means unrestricted.
type InstanceScope struct {
	HasToolAllowlist  bool
	AllowedTools      []string
	HasAgentAllowlist bool
	AllowedAgents     []string
}

type instanceScopeKey struct{}

// WithInstanceScope stores the resolved instance scope in the request context
// so the service layer (agent tools) can enforce the agent allowlist without
// changing the ExecuteTool signature.
func WithInstanceScope(ctx context.Context, scope *InstanceScope) context.Context {
	if scope == nil {
		return ctx
	}
	return context.WithValue(ctx, instanceScopeKey{}, scope)
}

// InstanceScopeFromContext retrieves the instance scope, if any.
func InstanceScopeFromContext(ctx context.Context) *InstanceScope {
	scope, _ := ctx.Value(instanceScopeKey{}).(*InstanceScope)
	return scope
}

// ============================================================================
// DTOs
// ============================================================================

// CreateShareInstanceRequest is the request body for POST .../shares.
// Tools == nil means unrestricted (null allowlist); an empty non-nil slice is
// rejected.
type CreateShareInstanceRequest struct {
	Name        string    `json:"name"`
	Description *string   `json:"description,omitempty"`
	Tools       *[]string `json:"tools"`
	Agents      []string  `json:"agents"`
}

// UpdateShareInstanceRequest is the request body for PATCH .../shares/:id.
// Nil pointers mean "leave unchanged".
type UpdateShareInstanceRequest struct {
	Name        *string   `json:"name"`
	Description *string   `json:"description"`
	Tools       *[]string `json:"tools"`
	Agents      *[]string `json:"agents"`
}

// ShareInstanceDTO is the non-secret representation of a share instance.
type ShareInstanceDTO struct {
	ID          string     `json:"id"`
	ProjectID   string     `json:"projectId"`
	Name        string     `json:"name"`
	Description *string    `json:"description,omitempty"`
	Tools       *[]string  `json:"tools"`
	Agents      *[]string  `json:"agents"`
	IsLegacy    bool       `json:"isLegacy"`
	Status      string     `json:"status"`
	CreatedAt   time.Time  `json:"createdAt"`
	UpdatedAt   time.Time  `json:"updatedAt"`
	LastUsedAt  *time.Time `json:"lastUsedAt,omitempty"`
}

// ShareInstanceListResponse is the response for GET .../shares.
type ShareInstanceListResponse struct {
	Instances []ShareInstanceDTO `json:"instances"`
	Total     int                `json:"total"`
}

// CreateShareInstanceResponse is returned by create/rotate and includes the raw
// token value exactly once.
type CreateShareInstanceResponse struct {
	ShareInstanceDTO
	Token  string `json:"token"`
	MCPURL string `json:"mcpUrl"`
}

// CatalogToolDTO is one includable tool in the catalog endpoint.
type CatalogToolDTO struct {
	Name          string `json:"name"`
	Description   string `json:"description"`
	RequiredScope string `json:"requiredScope"`
	Category      string `json:"category"`
}

// CatalogResponse is the response for GET .../mcp/tools.
type CatalogResponse struct {
	Tools []CatalogToolDTO `json:"tools"`
	Total int              `json:"total"`
}

// ============================================================================
// Pure helpers (unit-testable)
// ============================================================================

// normalizeInstanceName trims and validates a share-instance name.
func normalizeInstanceName(name string) (string, error) {
	trimmed := strings.TrimSpace(name)
	if trimmed == "" {
		return "", apperror.NewValidation("name is required")
	}
	if len(trimmed) > 255 {
		return "", apperror.NewValidation("name must be at most 255 characters")
	}
	return trimmed, nil
}

// dedupeStrings collapses duplicates preserving first-seen order.
func dedupeStrings(in []string) []string {
	if len(in) == 0 {
		return nil
	}
	seen := make(map[string]bool, len(in))
	out := make([]string, 0, len(in))
	for _, v := range in {
		if seen[v] {
			continue
		}
		seen[v] = true
		out = append(out, v)
	}
	return out
}

// toolLookupFunc resolves a tool name to its definition (nil when unknown).
type toolLookupFunc func(name string) *ToolDefinition

// isAdministrativeScope reports whether a tool's required scope grants the
// administrative ("admin"/"admin:all") or account-level surface. Such scopes
// are never derivable from a share-instance allowlist: emitting "admin" would
// let a share token satisfy the share-admin route guard and chain into other
// instances. This is the belt-and-suspenders complement to that route guard.
func isAdministrativeScope(scope string) bool {
	switch scope {
	case "admin", "admin:all":
		return true
	}
	return strings.HasPrefix(scope, "account:")
}

// normalizeToolAllowlist validates and de-duplicates an explicit tool
// allowlist. A nil input means unrestricted. An empty input is rejected.
func normalizeToolAllowlist(tools *[]string, lookup toolLookupFunc) ([]string, error) {
	if tools == nil {
		return nil, nil
	}
	if len(*tools) == 0 {
		return nil, apperror.NewValidation("tools must not be empty; omit the field for an unrestricted allowlist")
	}
	out := make([]string, 0, len(*tools))
	for _, raw := range *tools {
		name := strings.TrimSpace(raw)
		if name == "" {
			return nil, apperror.NewValidation("tool name must not be empty")
		}
		def := lookup(name)
		if def == nil {
			return nil, apperror.NewValidation("unknown tool: " + name)
		}
		if def.AgentOnly {
			return nil, apperror.NewValidation("agent-only tool cannot be shared: " + name)
		}
		if isAdministrativeScope(def.RequiredScope) {
			return nil, apperror.NewValidation("tool cannot be shared because it requires an administrative scope: " + name)
		}
		if agentExecutionTools[name] || agentMutationTools[name] {
			return nil, apperror.NewValidation("tool cannot be shared because its side effects run outside the instance context: " + name)
		}
		out = append(out, name)
	}
	return dedupeStrings(out), nil
}

// deriveScopesForToolNames returns the union of the required scopes of the
// selected tools plus the baseline projects:read scope.
func deriveScopesForToolNames(tools []string, lookup toolLookupFunc) ([]string, error) {
	seen := map[string]bool{instanceScopeBaseline: true}
	for _, name := range tools {
		def := lookup(name)
		if def == nil {
			return nil, apperror.NewValidation("unknown tool: " + name)
		}
		if isAdministrativeScope(def.RequiredScope) {
			return nil, apperror.NewValidation("tool requires an administrative scope: " + name)
		}
		if def.RequiredScope != "" {
			seen[def.RequiredScope] = true
		}
	}
	out := make([]string, 0, len(seen))
	for scope := range seen {
		out = append(out, scope)
	}
	sort.Strings(out)
	return out, nil
}

// toolCategoryDerived returns a deterministic grouping category for a tool,
// derived from its required scope (falling back to the tool name prefix).
func toolCategoryDerived(t ToolDefinition) string {
	scope := t.RequiredScope
	switch {
	case scope == "search":
		return "Search"
	case strings.HasPrefix(scope, "graph:"):
		return "Graph"
	case strings.HasPrefix(scope, "schema:"):
		return "Schema"
	case strings.HasPrefix(scope, "branches:"):
		return "Branches"
	case strings.HasPrefix(scope, "journal:"):
		return "Journal"
	case strings.HasPrefix(scope, "skills:"):
		return "Skills"
	case strings.HasPrefix(scope, "documents:"):
		return "Documents"
	case strings.HasPrefix(scope, "agents:"):
		return "Agents"
	case strings.HasPrefix(scope, "data:"):
		return "Data"
	case strings.HasPrefix(scope, "projects:"):
		return "Projects"
	case scope == "chat:use" || scope == "chat:admin":
		return "Chat"
	case scope == "admin" || scope == "admin:all":
		return "Admin"
	case scope == "":
		if i := strings.IndexByte(t.Name, '-'); i > 0 {
			return strings.ToUpper(t.Name[:1]) + t.Name[1:i]
		}
		return "General"
	default:
		return "Other"
	}
}

// shareInstanceStatus derives active/revoked/expired from the bound token and
// instance lifecycle.
func shareInstanceStatus(instanceRevokedAt, tokenRevokedAt, tokenExpiresAt *time.Time, now time.Time) string {
	if instanceRevokedAt != nil || tokenRevokedAt != nil {
		return "revoked"
	}
	if tokenExpiresAt != nil && !tokenExpiresAt.After(now) {
		return "expired"
	}
	return "active"
}

// InstanceAllowsTool reports whether a tool may be listed and called under the
// given scope. A nil scope or a scope without a tool allowlist allows all.
func InstanceAllowsTool(scope *InstanceScope, toolName string) bool {
	if scope == nil || !scope.HasToolAllowlist {
		return true
	}
	for _, t := range scope.AllowedTools {
		if t == toolName {
			return true
		}
	}
	return false
}

// InstanceDeniesTool reports whether the instance scope forbids the tool, either
// because of the tool allowlist or because the instance has an agent allowlist
// and the tool cannot be safely scoped to it (agent-definition, run-inspection,
// or agent-mutation). This is the single predicate used by transports and by
// Service.ExecuteTool.
func InstanceDeniesTool(scope *InstanceScope, toolName string) bool {
	if scope == nil {
		return false
	}
	if scope.HasToolAllowlist && !InstanceAllowsTool(scope, toolName) {
		return true
	}
	if scope.HasAgentAllowlist && agentAllowlistDeniedTools(toolName) {
		return true
	}
	return false
}

// InstanceRestrictsContent reports whether the instance imposes an explicit
// allowlist. MCP resources and prompts are not covered by the tool/agent
// allowlists, so they must fail closed for any restricted instance. A nil scope
// (legacy/unrestricted, including null-allowlist instances) keeps the historical
// behavior.
func InstanceRestrictsContent(scope *InstanceScope) bool {
	return scope != nil && (scope.HasToolAllowlist || scope.HasAgentAllowlist)
}

// FilterToolsForInstance applies the instance tool allowlist on top of scope
// filtering. When an agent allowlist is active it also removes agent-definition,
// run-inspection, and agent-mutation tools that cannot be safely scoped to the
// allowlist (fail closed). Order is preserved.
func FilterToolsForInstance(tools []ToolDefinition, scope *InstanceScope) []ToolDefinition {
	if scope == nil || (!scope.HasToolAllowlist && !scope.HasAgentAllowlist) {
		return tools
	}
	var allowed map[string]bool
	if scope.HasToolAllowlist {
		allowed = make(map[string]bool, len(scope.AllowedTools))
		for _, t := range scope.AllowedTools {
			allowed[t] = true
		}
	}
	out := make([]ToolDefinition, 0, len(tools))
	for _, t := range tools {
		if allowed != nil && !allowed[t.Name] {
			continue
		}
		if scope.HasAgentAllowlist && agentAllowlistDeniedTools(t.Name) {
			continue
		}
		out = append(out, t)
	}
	return out
}

// ============================================================================
// Bun store implementation
// ============================================================================

type bunShareInstanceStore struct {
	db bun.IDB
}

func newShareInstanceStore(db bun.IDB) *bunShareInstanceStore {
	return &bunShareInstanceStore{db: db}
}

// shareInstanceOrNil avoids installing a store whose underlying DB is nil.
func shareInstanceOrNil(db bun.IDB) shareInstanceStore {
	if db == nil {
		return nil
	}
	return newShareInstanceStore(db)
}

// agentDirectoryOrNil avoids installing a directory whose underlying DB is nil.
func agentDirectoryOrNil(db bun.IDB) agentDirectory {
	if db == nil {
		return nil
	}
	return newAgentDirectory(db)
}

const shareInstanceSelect = `SELECT msi.id, msi.project_id, msi.name, msi.description, msi.token_id,
	msi.allowed_tools, msi.allowed_agents, msi.is_legacy, msi.created_by,
	msi.created_at, msi.updated_at, msi.revoked_at,
	at.last_used_at AS token_last_used_at,
	at.revoked_at AS token_revoked_at,
	at.expires_at AS token_expires_at
FROM core.mcp_share_instances msi
LEFT JOIN core.api_tokens at ON at.id = msi.token_id`

func (r *bunShareInstanceStore) ListByProject(ctx context.Context, projectID string) ([]*MCPShareInstance, error) {
	var rows []*MCPShareInstance
	err := r.db.NewRaw(shareInstanceSelect+` WHERE msi.project_id = ? ORDER BY msi.created_at DESC`, projectID).Scan(ctx, &rows)
	if err != nil {
		return nil, apperror.NewDatabase(apperror.ErrDatabase.Message, err)
	}
	return rows, nil
}

func (r *bunShareInstanceStore) GetByID(ctx context.Context, projectID, id string) (*MCPShareInstance, error) {
	row := new(MCPShareInstance)
	err := r.db.NewRaw(shareInstanceSelect+` WHERE msi.project_id = ? AND msi.id = ?`, projectID, id).Scan(ctx, row)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, apperror.NewDatabase(apperror.ErrDatabase.Message, err)
	}
	return row, nil
}

func (r *bunShareInstanceStore) GetByTokenID(ctx context.Context, tokenID string) (*MCPShareInstance, error) {
	row := new(MCPShareInstance)
	err := r.db.NewSelect().
		Model(row).
		Where("token_id = ?", tokenID).
		Where("revoked_at IS NULL").
		Scan(ctx)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, apperror.NewDatabase(apperror.ErrDatabase.Message, err)
	}
	return row, nil
}

func (r *bunShareInstanceStore) FindByName(ctx context.Context, projectID, name string) (*MCPShareInstance, error) {
	row := new(MCPShareInstance)
	err := r.db.NewSelect().
		Model(row).
		Where("project_id = ?", projectID).
		Where("lower(name) = lower(?)", name).
		Where("revoked_at IS NULL").
		Scan(ctx)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, apperror.NewDatabase(apperror.ErrDatabase.Message, err)
	}
	return row, nil
}

func (r *bunShareInstanceStore) Create(ctx context.Context, inst *MCPShareInstance) error {
	_, err := r.db.NewInsert().Model(inst).Exec(ctx)
	if err != nil {
		if isUniqueViolation(err) {
			return apperror.New(409, "share_instance_name_exists", "A share instance with this name already exists")
		}
		return apperror.NewDatabase(apperror.ErrDatabase.Message, err)
	}
	return nil
}

func (r *bunShareInstanceStore) Update(ctx context.Context, inst *MCPShareInstance) error {
	_, err := r.db.NewUpdate().
		Model((*MCPShareInstance)(nil)).
		Set("name = ?", inst.Name).
		Set("description = ?", inst.Description).
		Set("token_id = ?", inst.TokenID).
		Set("allowed_tools = ?", pq.Array(inst.AllowedTools)).
		Set("allowed_agents = ?", pq.Array(inst.AllowedAgents)).
		Set("updated_at = ?", inst.UpdatedAt).
		Set("revoked_at = ?", inst.RevokedAt).
		Where("id = ?", inst.ID).
		Where("project_id = ?", inst.ProjectID).
		Exec(ctx)
	if err != nil {
		if isUniqueViolation(err) {
			return apperror.New(409, "share_instance_name_exists", "A share instance with this name already exists")
		}
		return apperror.NewDatabase(apperror.ErrDatabase.Message, err)
	}
	return nil
}

func (r *bunShareInstanceStore) ListLegacyTokens(ctx context.Context, projectID string) ([]*LegacyTokenRef, error) {
	var rows []*LegacyTokenRef
	prefix0 := legacyShareNamePrefixes[0] + "%"
	prefix1 := legacyShareNamePrefixes[1] + "%"
	err := r.db.NewSelect().
		TableExpr("core.api_tokens").
		Column("id", "name", "scopes", "created_at", "last_used_at", "revoked_at", "expires_at").
		Where("project_id = ?", projectID).
		Where("(name LIKE ? OR name LIKE ?)", prefix0, prefix1).
		Order("created_at DESC").
		Scan(ctx, &rows)
	if err != nil {
		return nil, apperror.NewDatabase(apperror.ErrDatabase.Message, err)
	}
	return rows, nil
}

// isUniqueViolation detects Postgres unique-constraint violations without
// importing the pgutils helper (kept local to this file's store).
func isUniqueViolation(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	return strings.Contains(msg, "SQLSTATE 23505") || strings.Contains(msg, "duplicate key value")
}

// ============================================================================
// Bun agent directory
// ============================================================================

type bunAgentDirectory struct {
	db bun.IDB
}

func newAgentDirectory(db bun.IDB) *bunAgentDirectory {
	return &bunAgentDirectory{db: db}
}

func (d *bunAgentDirectory) ListProjectAgents(ctx context.Context, projectID string) ([]AgentRef, error) {
	var rows []AgentRef
	err := d.db.NewSelect().
		TableExpr("kb.agents").
		Column("id", "name", "enabled").
		Where("project_id = ?", projectID).
		Scan(ctx, &rows)
	if err != nil {
		return nil, apperror.NewDatabase(apperror.ErrDatabase.Message, err)
	}
	return rows, nil
}

// FindProjectAgentByID returns a single project agent reference by ID, or nil
// when the agent does not exist in the project. Used by the per-agent MCP share
// lifecycle and endpoint to confirm the bound agent still exists.
func (d *bunAgentDirectory) FindProjectAgentByID(ctx context.Context, projectID, id string) (*AgentRef, error) {
	ref := new(AgentRef)
	err := d.db.NewSelect().
		TableExpr("kb.agents").
		Column("id", "name", "enabled").
		Where("project_id = ?", projectID).
		Where("id = ?", id).
		Limit(1).
		Scan(ctx, ref)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, apperror.NewDatabase(apperror.ErrDatabase.Message, err)
	}
	return ref, nil
}

func (d *bunAgentDirectory) FindAgentIDByName(ctx context.Context, projectID, name string) (string, bool, error) {
	var id string
	err := d.db.NewSelect().
		TableExpr("kb.agents").
		Column("id").
		Where("project_id = ?", projectID).
		Where("name = ?", name).
		Limit(1).
		Scan(ctx, &id)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return "", false, nil
		}
		return "", false, apperror.NewDatabase(apperror.ErrDatabase.Message, err)
	}
	return id, id != "", nil
}

// FindAgentIDByNameOrSlug resolves the raw agent name or its ACP slug. The slug
// is computed in Go (pkg/acpslug) so no SQL function is required.
func (d *bunAgentDirectory) FindAgentIDByNameOrSlug(ctx context.Context, projectID, nameOrSlug string) (string, bool, error) {
	target := strings.TrimSpace(nameOrSlug)
	if target == "" {
		return "", false, nil
	}
	lowerTarget := strings.ToLower(target)
	agents, err := d.ListProjectAgents(ctx, projectID)
	if err != nil {
		return "", false, err
	}
	for _, a := range agents {
		if a.Name == target || acpslug.FromName(a.Name) == lowerTarget {
			return a.ID, true, nil
		}
	}
	return "", false, nil
}

// ============================================================================
// Service accessors + lifecycle
// ============================================================================

func (s *Service) shareStore() shareInstanceStore {
	if s.shareInstances != nil {
		return s.shareInstances
	}
	if s.db != nil {
		return newShareInstanceStore(s.db)
	}
	return nil
}

func (s *Service) shareTokenSvc() shareTokenService {
	if s.shareTokens != nil {
		return s.shareTokens
	}
	if s.apitokenSvc != nil {
		return s.apitokenSvc
	}
	return nil
}

func (s *Service) agentDirectorySvc() agentDirectory {
	if s.agentDir != nil {
		return s.agentDir
	}
	if s.db != nil {
		return newAgentDirectory(s.db)
	}
	return nil
}

// scopesForAllowlist derives token scopes for an instance. A null allowlist
// (unrestricted) gets the legacy read-only scope set so the minted token is
// actually usable; an explicit allowlist gets the union of its tools' required
// scopes plus the baseline projects:read.
func (s *Service) scopesForAllowlist(tools []string) ([]string, error) {
	if tools == nil {
		return append([]string(nil), readOnlyMCPScopes...), nil
	}
	return deriveScopesForToolNames(tools, s.GetToolByName)
}

// instanceTokenName builds a stable token name for an instance.
func instanceTokenName(name string) string {
	return "MCP Share: " + name
}

// EnsureProjectAdmin returns nil when userID is a project admin.
func (s *Service) EnsureProjectAdmin(ctx context.Context, projectID, userID string) error {
	tokenSvc := s.shareTokenSvc()
	if tokenSvc == nil {
		return apperror.NewInternal("api token service unavailable", nil)
	}
	role, err := tokenSvc.GetUserProjectRole(ctx, projectID, userID)
	if err != nil {
		return err
	}
	if role != "project_admin" {
		return apperror.NewForbidden("project admin role required to manage MCP shares")
	}
	return nil
}

// CreateShareInstance validates the request, mints a project token whose scopes
// cover the selected tools, and persists the instance.
func (s *Service) CreateShareInstance(ctx context.Context, projectID, userID, baseURL string, req CreateShareInstanceRequest) (*CreateShareInstanceResponse, error) {
	if err := s.EnsureProjectAdmin(ctx, projectID, userID); err != nil {
		return nil, err
	}
	store := s.shareStore()
	tokenSvc := s.shareTokenSvc()
	if store == nil || tokenSvc == nil {
		return nil, apperror.NewInternal("share instance storage unavailable", nil)
	}

	name, err := normalizeInstanceName(req.Name)
	if err != nil {
		return nil, err
	}
	existing, err := store.FindByName(ctx, projectID, name)
	if err != nil {
		return nil, err
	}
	if existing != nil {
		return nil, apperror.New(409, "share_instance_name_exists", "A share instance named \""+name+"\" already exists for this project")
	}

	// Populate the tool index (including project/relay tools) before validating.
	s.GetToolDefinitionsForProject(ctx, projectID)
	tools, err := normalizeToolAllowlist(req.Tools, s.GetToolByName)
	if err != nil {
		return nil, err
	}
	agents, err := s.normalizeAgentAllowlist(ctx, projectID, req.Agents)
	if err != nil {
		return nil, err
	}
	scopes, err := s.scopesForAllowlist(tools)
	if err != nil {
		return nil, err
	}

	token, err := tokenSvc.Create(ctx, projectID, userID, instanceTokenName(name), scopes)
	if err != nil {
		return nil, err
	}

	now := time.Now().UTC()
	inst := &MCPShareInstance{
		ID:            uuid.NewString(),
		ProjectID:     projectID,
		Name:          name,
		Description:   req.Description,
		TokenID:       token.ID,
		AllowedTools:  tools,
		AllowedAgents: agents,
		CreatedBy:     &userID,
		CreatedAt:     now,
		UpdatedAt:     now,
	}
	if err := store.Create(ctx, inst); err != nil {
		// Best-effort cleanup so we do not strand an untracked credential.
		_ = tokenSvc.Revoke(ctx, token.ID, projectID, userID)
		return nil, err
	}

	return &CreateShareInstanceResponse{
		ShareInstanceDTO: inst.toDTO(now),
		Token:            token.Token,
		MCPURL:           mcpEndpointURL(baseURL),
	}, nil
}

// normalizeAgentAllowlist validates agent IDs against the project and
// de-duplicates. Empty input yields nil (unrestricted).
func (s *Service) normalizeAgentAllowlist(ctx context.Context, projectID string, agents []string) ([]uuid.UUID, error) {
	if len(agents) == 0 {
		return nil, nil
	}
	dir := s.agentDirectorySvc()
	if dir == nil {
		return nil, apperror.NewInternal("agent directory unavailable", nil)
	}
	projectAgents, err := dir.ListProjectAgents(ctx, projectID)
	if err != nil {
		return nil, err
	}
	known := make(map[string]bool, len(projectAgents))
	for _, a := range projectAgents {
		known[a.ID] = true
	}
	seen := make(map[uuid.UUID]bool, len(agents))
	out := make([]uuid.UUID, 0, len(agents))
	for _, raw := range agents {
		id, perr := uuid.Parse(strings.TrimSpace(raw))
		if perr != nil {
			return nil, apperror.NewValidation("invalid agent id: " + raw)
		}
		if !known[id.String()] {
			return nil, apperror.NewValidation("unknown agent: " + id.String())
		}
		if seen[id] {
			continue
		}
		seen[id] = true
		out = append(out, id)
	}
	return out, nil
}

// ListShareInstances returns bound instances plus unbound legacy share tokens.
func (s *Service) ListShareInstances(ctx context.Context, projectID, userID string) (*ShareInstanceListResponse, error) {
	if err := s.EnsureProjectAdmin(ctx, projectID, userID); err != nil {
		return nil, err
	}
	store := s.shareStore()
	if store == nil {
		return nil, apperror.NewInternal("share instance storage unavailable", nil)
	}
	instances, err := store.ListByProject(ctx, projectID)
	if err != nil {
		return nil, err
	}
	now := time.Now().UTC()
	out := make([]ShareInstanceDTO, 0, len(instances))
	boundTokens := make(map[string]bool, len(instances))
	for _, inst := range instances {
		boundTokens[inst.TokenID] = true
		out = append(out, inst.toDTO(now))
	}

	legacy, err := store.ListLegacyTokens(ctx, projectID)
	if err != nil {
		return nil, err
	}
	for _, tok := range legacy {
		if boundTokens[tok.ID] {
			continue
		}
		out = append(out, legacyTokenToDTO(projectID, tok, now))
	}

	return &ShareInstanceListResponse{Instances: out, Total: len(out)}, nil
}

// GetShareInstance returns a single instance within a project.
func (s *Service) GetShareInstance(ctx context.Context, projectID, userID, id string) (*ShareInstanceDTO, error) {
	if err := s.EnsureProjectAdmin(ctx, projectID, userID); err != nil {
		return nil, err
	}
	store := s.shareStore()
	if store == nil {
		return nil, apperror.NewInternal("share instance storage unavailable", nil)
	}
	inst, err := store.GetByID(ctx, projectID, id)
	if err != nil {
		return nil, err
	}
	if inst == nil {
		return nil, apperror.NewNotFound("Share instance", id)
	}
	dto := inst.toDTO(time.Now().UTC())
	return &dto, nil
}

// UpdateShareInstance updates name/description/allowlists and keeps the bound
// token scopes in sync. The token secret is never changed.
func (s *Service) UpdateShareInstance(ctx context.Context, projectID, userID, id string, req UpdateShareInstanceRequest) (*ShareInstanceDTO, error) {
	if err := s.EnsureProjectAdmin(ctx, projectID, userID); err != nil {
		return nil, err
	}
	store := s.shareStore()
	tokenSvc := s.shareTokenSvc()
	if store == nil || tokenSvc == nil {
		return nil, apperror.NewInternal("share instance storage unavailable", nil)
	}
	inst, err := store.GetByID(ctx, projectID, id)
	if err != nil {
		return nil, err
	}
	if inst == nil {
		return nil, apperror.NewNotFound("Share instance", id)
	}
	if inst.RevokedAt != nil {
		return nil, apperror.New(409, "share_instance_revoked", "Share instance is revoked and cannot be updated")
	}

	// Work on a copy so a validation failure never leaks partial mutations into
	// the caller's stored row (and so rollback can restore the exact prior state).
	prev := *inst
	updated := *inst

	if req.Name != nil {
		name, nerr := normalizeInstanceName(*req.Name)
		if nerr != nil {
			return nil, nerr
		}
		if name != inst.Name {
			conflict, cerr := store.FindByName(ctx, projectID, name)
			if cerr != nil {
				return nil, cerr
			}
			if conflict != nil && conflict.ID != inst.ID {
				return nil, apperror.New(409, "share_instance_name_exists", "A share instance named \""+name+"\" already exists for this project")
			}
			updated.Name = name
		}
	}
	if req.Description != nil {
		updated.Description = req.Description
	}

	// Validate/normalize EVERYTHING before persisting anything. In particular,
	// agent normalization must not run after token scopes have been written, or a
	// failed normalization would leave scopes and allowlist divergent.
	var newTools []string
	var newScopes []string
	scopesChanged := false
	if req.Tools != nil {
		s.GetToolDefinitionsForProject(ctx, projectID)
		tools, terr := normalizeToolAllowlist(req.Tools, s.GetToolByName)
		if terr != nil {
			return nil, terr
		}
		scopes, derr := deriveScopesForToolNames(tools, s.GetToolByName)
		if derr != nil {
			return nil, derr
		}
		newTools = tools
		newScopes = scopes
		scopesChanged = true
	}
	if req.Agents != nil {
		agents, aerr := s.normalizeAgentAllowlist(ctx, projectID, *req.Agents)
		if aerr != nil {
			return nil, aerr
		}
		updated.AllowedAgents = agents
	}
	if scopesChanged {
		updated.AllowedTools = newTools
	}

	updated.UpdatedAt = time.Now().UTC()

	// Persist the allowlist first. If this fails, token scopes are untouched, so
	// scopes and allowlist cannot diverge. Only once the allowlist is durable do
	// we update the token scopes, compensating on failure by restoring the prior
	// persisted allowlist (mirrors RotateShareInstance's rollback approach).
	if err := store.Update(ctx, &updated); err != nil {
		return nil, err
	}
	if scopesChanged {
		if _, uerr := tokenSvc.UpdateScopes(ctx, updated.TokenID, projectID, userID, newScopes); uerr != nil {
			if rbErr := store.Update(ctx, &prev); rbErr != nil {
				return nil, fmt.Errorf("update share instance scopes: %w (allowlist rollback failed: %v)", uerr, rbErr)
			}
			return nil, uerr
		}
	}
	dto := updated.toDTO(updated.UpdatedAt)
	return &dto, nil
}

// RevokeShareInstance revokes the bound token and marks the instance revoked.
// It is idempotent.
func (s *Service) RevokeShareInstance(ctx context.Context, projectID, userID, id string) error {
	if err := s.EnsureProjectAdmin(ctx, projectID, userID); err != nil {
		return err
	}
	store := s.shareStore()
	tokenSvc := s.shareTokenSvc()
	if store == nil || tokenSvc == nil {
		return apperror.NewInternal("share instance storage unavailable", nil)
	}
	inst, err := store.GetByID(ctx, projectID, id)
	if err != nil {
		return err
	}
	if inst == nil {
		return apperror.NewNotFound("Share instance", id)
	}
	if inst.RevokedAt != nil {
		return nil
	}
	if err := tokenSvc.Revoke(ctx, inst.TokenID, projectID, userID); err != nil && !isTokenAlreadyRevoked(err) {
		return err
	}
	now := time.Now().UTC()
	inst.RevokedAt = &now
	inst.UpdatedAt = now
	return store.Update(ctx, inst)
}

// RotateShareInstance issues a replacement token with the current scopes,
// invalidates the old one, and keeps the allowlist intact.
func (s *Service) RotateShareInstance(ctx context.Context, projectID, userID, id, baseURL string) (*CreateShareInstanceResponse, error) {
	if err := s.EnsureProjectAdmin(ctx, projectID, userID); err != nil {
		return nil, err
	}
	store := s.shareStore()
	tokenSvc := s.shareTokenSvc()
	if store == nil || tokenSvc == nil {
		return nil, apperror.NewInternal("share instance storage unavailable", nil)
	}
	inst, err := store.GetByID(ctx, projectID, id)
	if err != nil {
		return nil, err
	}
	if inst == nil {
		return nil, apperror.NewNotFound("Share instance", id)
	}
	if inst.RevokedAt != nil {
		return nil, apperror.New(409, "share_instance_revoked", "Share instance is revoked and cannot be rotated")
	}

	// Rotate atomically: the token service revokes the old token, inserts the
	// replacement, and (via the hook) updates the instance token_id in the same
	// transaction. If the transaction fails, no orphan active token remains.
	rotateUpdatedInTx := false
	newToken, err := tokenSvc.RegenerateWith(ctx, inst.TokenID, projectID, userID, func(ctx context.Context, tx bun.Tx, newID string) error {
		rotateUpdatedInTx = true
		inst.TokenID = newID
		inst.UpdatedAt = time.Now().UTC()
		_, uerr := tx.NewUpdate().
			Model((*MCPShareInstance)(nil)).
			Set("token_id = ?", newID).
			Set("updated_at = ?", inst.UpdatedAt).
			Where("id = ?", inst.ID).
			Where("project_id = ?", projectID).
			Exec(ctx)
		return uerr
	})
	if err != nil {
		return nil, err
	}
	if !rotateUpdatedInTx {
		// Test seam / non-transactional token service: update via the store and
		// compensate by revoking the replacement if the update fails.
		inst.TokenID = newToken.ID
		inst.UpdatedAt = time.Now().UTC()
		if uerr := store.Update(ctx, inst); uerr != nil {
			_ = tokenSvc.Revoke(ctx, newToken.ID, projectID, userID)
			return nil, uerr
		}
	}
	return &CreateShareInstanceResponse{
		ShareInstanceDTO: inst.toDTO(inst.UpdatedAt),
		Token:            newToken.Token,
		MCPURL:           mcpEndpointURL(baseURL),
	}, nil
}

// ListToolCatalog returns the includable tool catalog for a project, admin-only.
func (s *Service) ListToolCatalog(ctx context.Context, projectID, userID string) (*CatalogResponse, error) {
	if err := s.EnsureProjectAdmin(ctx, projectID, userID); err != nil {
		return nil, err
	}
	tools := BuildToolCatalog(ctx, s, projectID)
	return &CatalogResponse{Tools: tools, Total: len(tools)}, nil
}

// BuildToolCatalog returns includable tools (AgentOnly excluded) ordered by
// category then name. Exposed for unit tests.
func BuildToolCatalog(ctx context.Context, s *Service, projectID string) []CatalogToolDTO {
	defs := s.GetToolDefinitionsForProject(ctx, projectID)
	out := make([]CatalogToolDTO, 0, len(defs))
	for _, d := range defs {
		if d.AgentOnly {
			continue
		}
		// Tools requiring an administrative/account scope are never includable:
		// deriving "admin" onto a share token would let it satisfy the
		// share-admin route guard and chain into sibling instances.
		if isAdministrativeScope(d.RequiredScope) {
			continue
		}
		// Agent execution/mutation tools cannot be safely constrained by a
		// static allowlist (their side effects run outside the request
		// context), so they are never includable.
		if agentExecutionTools[d.Name] || agentMutationTools[d.Name] {
			continue
		}
		out = append(out, CatalogToolDTO{
			Name:          d.Name,
			Description:   d.Description,
			RequiredScope: d.RequiredScope,
			Category:      toolCategoryDerived(d),
		})
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Category != out[j].Category {
			return out[i].Category < out[j].Category
		}
		return out[i].Name < out[j].Name
	})
	return out
}

// ResolveInstanceScope resolves the allowlist for the authenticated API token
// (from AuthUser.APITokenID). It returns (nil, nil) for legacy/unknown tokens
// (including bound instances with a null allowlist), meaning "unrestricted".
//
// A non-nil error means the share-instance lookup itself failed. Callers MUST
// fail closed (deny) on error rather than treating it as unrestricted; silently
// returning nil would disable allowlist enforcement for the request.
func (s *Service) ResolveInstanceScope(ctx context.Context, apiTokenID string) (*InstanceScope, error) {
	if apiTokenID == "" {
		return nil, nil
	}
	store := s.shareStore()
	if store == nil {
		return nil, nil
	}
	inst, err := store.GetByTokenID(ctx, apiTokenID)
	if err != nil {
		return nil, err
	}
	if inst == nil || inst.RevokedAt != nil {
		return nil, nil
	}
	scope := &InstanceScope{}
	if len(inst.AllowedTools) > 0 {
		scope.HasToolAllowlist = true
		scope.AllowedTools = inst.AllowedTools
	}
	if len(inst.AllowedAgents) > 0 {
		scope.HasAgentAllowlist = true
		scope.AllowedAgents = make([]string, len(inst.AllowedAgents))
		for i, id := range inst.AllowedAgents {
			scope.AllowedAgents[i] = id.String()
		}
	}
	if !scope.HasToolAllowlist && !scope.HasAgentAllowlist {
		return nil, nil
	}
	return scope, nil
}

// mcpEndpointURL builds the canonical MCP endpoint from a base URL.
func mcpEndpointURL(baseURL string) string {
	if baseURL == "" {
		return "/api/mcp"
	}
	return strings.TrimRight(baseURL, "/") + "/api/mcp"
}

// isTokenAlreadyRevoked reports whether an apitoken failure is the
// "already revoked" conflict (treated as success for idempotency).
func isTokenAlreadyRevoked(err error) bool {
	var appErr *apperror.Error
	if errors.As(err, &appErr) {
		return appErr.Code == "token_already_revoked" || appErr.HTTPStatus == 404
	}
	return false
}

// ============================================================================
// DTO helpers
// ============================================================================

func (inst *MCPShareInstance) toDTO(now time.Time) ShareInstanceDTO {
	dto := ShareInstanceDTO{
		ID:          inst.ID,
		ProjectID:   inst.ProjectID,
		Name:        inst.Name,
		Description: inst.Description,
		IsLegacy:    inst.IsLegacy,
		Status:      shareInstanceStatus(inst.RevokedAt, inst.TokenRevokedAt, inst.TokenExpiresAt, now),
		CreatedAt:   inst.CreatedAt,
		UpdatedAt:   inst.UpdatedAt,
		LastUsedAt:  inst.TokenLastUsedAt,
	}
	if len(inst.AllowedTools) > 0 {
		tools := append([]string(nil), inst.AllowedTools...)
		dto.Tools = &tools
	}
	if len(inst.AllowedAgents) > 0 {
		agents := make([]string, len(inst.AllowedAgents))
		for i, id := range inst.AllowedAgents {
			agents[i] = id.String()
		}
		dto.Agents = &agents
	}
	return dto
}

func legacyTokenToDTO(projectID string, tok *LegacyTokenRef, now time.Time) ShareInstanceDTO {
	return ShareInstanceDTO{
		ID:         tok.ID,
		ProjectID:  projectID,
		Name:       tok.Name,
		IsLegacy:   true,
		Status:     shareInstanceStatus(nil, tok.RevokedAt, tok.ExpiresAt, now),
		CreatedAt:  tok.CreatedAt,
		UpdatedAt:  tok.CreatedAt,
		LastUsedAt: tok.LastUsedAt,
	}
}

// ============================================================================
// Agent scoping
// ============================================================================

// agentDenialResult builds a non-fatal "not found" tool error without invoking
// the underlying handler, so no run is started.
func agentDenialResult(identifier string) *ToolResult {
	msg := "agent not found"
	if identifier != "" {
		msg = "agent not found: " + identifier
	}
	text, _ := json.Marshal(map[string]string{"error": msg})
	return &ToolResult{Content: []ContentBlock{{Type: "text", Text: string(text)}}}
}

// agentReference extracts the referenced agent identity from tool args.
func agentReference(args map[string]any) (id, name string) {
	if args == nil {
		return "", ""
	}
	if v, ok := args["agent_id"].(string); ok {
		id = strings.TrimSpace(v)
	}
	if v, ok := args["agent_name"].(string); ok {
		name = strings.TrimSpace(v)
	}
	return id, name
}

// agentDeniedByAllowlist decides whether an agent-related call must be rejected
// when an agent allowlist is active. It fails CLOSED: tools that cannot be
// scoped to the allowlist, unresolvable agent references, and directory errors
// are all denied. It must be called before the underlying handler so no run
// starts.
func (s *Service) agentDeniedByAllowlist(ctx context.Context, projectID, toolName string, args map[string]any, scope *InstanceScope) bool {
	if scope == nil || !scope.HasAgentAllowlist {
		return false
	}
	if agentAllowlistDeniedTools(toolName) {
		return true
	}
	if !agentToolReferencesAgent(toolName) {
		return false
	}
	allowed := make(map[string]bool, len(scope.AllowedAgents))
	for _, a := range scope.AllowedAgents {
		allowed[a] = true
	}
	id, name := agentReference(args)
	if id != "" {
		// Fail closed: an unresolvable/foreign id is not allowed.
		return !allowed[id]
	}
	if name == "" {
		// Fail closed: cannot verify the referenced agent.
		return true
	}
	dir := s.agentDirectorySvc()
	if dir == nil {
		return true
	}
	// Resolve either the raw name or the ACP slug (acp-trigger-run passes a slug).
	resolved, found, err := dir.FindAgentIDByNameOrSlug(ctx, projectID, name)
	if err != nil || !found {
		return true
	}
	return !allowed[resolved]
}

// agentToolReferencesAgent reports whether a tool targets a specific agent.
func agentToolReferencesAgent(toolName string) bool {
	switch toolName {
	case "agent-get", "trigger_agent", "acp-trigger-run":
		return true
	default:
		return false
	}
}

// filterAgentResult post-processes discovery-tool results to hide agents
// outside the allowlist. Filtering fails CLOSED: a parse/shape mismatch yields
// an empty result rather than the unfiltered payload.
func (s *Service) filterAgentResult(ctx context.Context, projectID, toolName string, res *ToolResult, scope *InstanceScope) *ToolResult {
	if res == nil || scope == nil || !scope.HasAgentAllowlist || len(res.Content) == 0 {
		return res
	}
	allowed := make(map[string]bool, len(scope.AllowedAgents))
	for _, a := range scope.AllowedAgents {
		allowed[a] = true
	}
	switch toolName {
	case "agent-list":
		return filterAgentListResult(res, allowed)
	case "agent-list-available":
		return s.filterAvailableAgentsResult(ctx, projectID, res, scope)
	case "acp-list-agents":
		return s.filterACPListAgentsResult(ctx, projectID, res, scope)
	case "agent-def-list":
		return s.filterAgentDefinitionsListResult(ctx, projectID, res, scope)
	case "agent-def-get":
		return s.filterAgentDefinitionGetResult(ctx, projectID, res, scope)
	default:
		return res
	}
}

// filterAgentListResult filters a JSON array of agent objects by "id". Fails
// closed to an empty list.
func filterAgentListResult(res *ToolResult, allowed map[string]bool) *ToolResult {
	var arr []map[string]any
	if err := json.Unmarshal([]byte(res.Content[0].Text), &arr); err != nil {
		return replaceToolResultText(res, "[]")
	}
	filtered := make([]map[string]any, 0, len(arr))
	for _, item := range arr {
		if id, _ := item["id"].(string); id != "" && allowed[id] {
			filtered = append(filtered, item)
		}
	}
	text, err := json.MarshalIndent(filtered, "", "  ")
	if err != nil {
		return replaceToolResultText(res, "[]")
	}
	return replaceToolResultText(res, string(text))
}

// emptyAvailableAgentsText is the fail-closed payload for agent-list-available.
const emptyAvailableAgentsText = `{"agents":[],"count":0}`

// filterAvailableAgentsResult filters {agents:[...], count} by agent name,
// resolving allowed IDs to names. Deleted IDs resolve to nothing. Fails closed.
func (s *Service) filterAvailableAgentsResult(ctx context.Context, projectID string, res *ToolResult, scope *InstanceScope) *ToolResult {
	dir := s.agentDirectorySvc()
	if dir == nil {
		return replaceToolResultText(res, emptyAvailableAgentsText)
	}
	agents, err := dir.ListProjectAgents(ctx, projectID)
	if err != nil {
		return replaceToolResultText(res, emptyAvailableAgentsText)
	}
	allowedNames := allowedAgentNames(scope.AllowedAgents, agents)

	var payload map[string]any
	if err := json.Unmarshal([]byte(res.Content[0].Text), &payload); err != nil {
		return replaceToolResultText(res, emptyAvailableAgentsText)
	}
	rawAgents, ok := payload["agents"].([]any)
	if !ok {
		return replaceToolResultText(res, emptyAvailableAgentsText)
	}
	filtered := make([]any, 0, len(rawAgents))
	for _, raw := range rawAgents {
		item, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		name, _ := item["name"].(string)
		if allowedNames[name] {
			filtered = append(filtered, item)
		}
	}
	payload["agents"] = filtered
	payload["count"] = len(filtered)
	text, err := json.MarshalIndent(payload, "", "  ")
	if err != nil {
		return replaceToolResultText(res, emptyAvailableAgentsText)
	}
	return replaceToolResultText(res, string(text))
}

// filterACPListAgentsResult filters the ACP manifest array by manifest name
// (an ACP slug) against the allowlist's slugs. Fails closed to an empty list.
func (s *Service) filterACPListAgentsResult(ctx context.Context, projectID string, res *ToolResult, scope *InstanceScope) *ToolResult {
	dir := s.agentDirectorySvc()
	if dir == nil {
		return replaceToolResultText(res, "[]")
	}
	agents, err := dir.ListProjectAgents(ctx, projectID)
	if err != nil {
		return replaceToolResultText(res, "[]")
	}
	allowedNames := allowedAgentNames(scope.AllowedAgents, agents)
	allowedSlugs := make(map[string]bool, len(allowedNames))
	for name := range allowedNames {
		allowedSlugs[acpslug.FromName(name)] = true
	}

	var arr []map[string]any
	if err := json.Unmarshal([]byte(res.Content[0].Text), &arr); err != nil {
		return replaceToolResultText(res, "[]")
	}
	filtered := make([]map[string]any, 0, len(arr))
	for _, item := range arr {
		name, _ := item["name"].(string)
		if allowedSlugs[name] {
			filtered = append(filtered, item)
		}
	}
	text, err := json.MarshalIndent(filtered, "", "  ")
	if err != nil {
		return replaceToolResultText(res, "[]")
	}
	return replaceToolResultText(res, string(text))
}

// allowedAgentNameSets resolves the allowlisted runtime agents to their names
// and ACP slugs. ok is false when the directory is unavailable (fail closed).
func (s *Service) allowedAgentNameSets(ctx context.Context, projectID string, scope *InstanceScope) (names, slugs map[string]bool, ok bool) {
	dir := s.agentDirectorySvc()
	if dir == nil {
		return nil, nil, false
	}
	agents, err := dir.ListProjectAgents(ctx, projectID)
	if err != nil {
		return nil, nil, false
	}
	names = allowedAgentNames(scope.AllowedAgents, agents)
	slugs = make(map[string]bool, len(names))
	for name := range names {
		slugs[acpslug.FromName(name)] = true
	}
	return names, slugs, true
}

// filterAgentDefinitionsListResult filters an agent-definition summary array by
// definition name (or ACP slug) against the allowlist. Fails closed.
func (s *Service) filterAgentDefinitionsListResult(ctx context.Context, projectID string, res *ToolResult, scope *InstanceScope) *ToolResult {
	names, slugs, ok := s.allowedAgentNameSets(ctx, projectID, scope)
	if !ok {
		return replaceToolResultText(res, "[]")
	}
	var arr []map[string]any
	if err := json.Unmarshal([]byte(res.Content[0].Text), &arr); err != nil {
		return replaceToolResultText(res, "[]")
	}
	filtered := make([]map[string]any, 0, len(arr))
	for _, item := range arr {
		name, _ := item["name"].(string)
		if names[name] || slugs[acpslug.FromName(name)] {
			filtered = append(filtered, item)
		}
	}
	text, err := json.MarshalIndent(filtered, "", "  ")
	if err != nil {
		return replaceToolResultText(res, "[]")
	}
	return replaceToolResultText(res, string(text))
}

// filterAgentDefinitionGetResult returns a definition only when its name/slug is
// allowlisted; otherwise it reports not-found. Fails closed.
func (s *Service) filterAgentDefinitionGetResult(ctx context.Context, projectID string, res *ToolResult, scope *InstanceScope) *ToolResult {
	names, slugs, ok := s.allowedAgentNameSets(ctx, projectID, scope)
	if !ok {
		return replaceToolResultText(res, `{"error":"agent definition not found"}`)
	}
	var payload map[string]any
	if err := json.Unmarshal([]byte(res.Content[0].Text), &payload); err != nil {
		return replaceToolResultText(res, `{"error":"agent definition not found"}`)
	}
	name, _ := payload["name"].(string)
	if name == "" || (!names[name] && !slugs[acpslug.FromName(name)]) {
		return replaceToolResultText(res, `{"error":"agent definition not found"}`)
	}
	return res
}

// allowedAgentNames maps the allowlisted IDs to their current names. Missing
// (deleted) IDs are simply absent.
func allowedAgentNames(allowedIDs []string, agents []AgentRef) map[string]bool {
	allowed := make(map[string]bool, len(allowedIDs))
	for _, id := range allowedIDs {
		allowed[id] = true
	}
	names := make(map[string]bool, len(agents))
	for _, a := range agents {
		if allowed[a.ID] {
			names[a.Name] = true
		}
	}
	return names
}

func replaceToolResultText(res *ToolResult, text string) *ToolResult {
	content := make([]ContentBlock, len(res.Content))
	copy(content, res.Content)
	content[0].Text = text
	return &ToolResult{Content: content, IsError: res.IsError}
}
