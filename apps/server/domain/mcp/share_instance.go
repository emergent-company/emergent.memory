package mcp

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/lib/pq"
	"github.com/uptrace/bun"

	"github.com/emergent-company/emergent.memory/domain/apitoken"
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
	"trigger_agent": true,
	// Consolidated family that folds trigger_agent: a family-level grant would
	// hand back the same out-of-context execution, so the whole tool is refused
	// from an allowlist (agentMutationTools lists it for the same reason).
	"agent-write": true,
}

// agentMutationTools create/update/delete agents, definitions, hooks, or answer
// agent questions. They can affect agents outside a share instance's scope and
// are rejected from any non-null tool allowlist.
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
	// Consolidated write families: each carries the actions above, so the whole
	// tool is non-shareable rather than silently shareable at family level.
	"agent-write":      true,
	"agent-def-write":  true,
	"agent-hook-write": true,
}

// ============================================================================
// Interfaces (test seams + default Bun implementations live below)
// ============================================================================

// shareTokenService is the subset of apitoken.Service needed for share-instance
// credential lifecycle. *apitoken.Service satisfies it.
type shareTokenService interface {
	Create(ctx context.Context, projectID, userID, name string, scopes []string) (*apitoken.CreateApiTokenResponseDTO, error)
	// CreateAgentShareToken mints a token permitted to carry the reserved
	// mcp:agent-call marker (per-agent MCP shares only).
	CreateAgentShareToken(ctx context.Context, projectID, userID, name string, scopes []string) (*apitoken.CreateApiTokenResponseDTO, error)
	// CreateAgentShareTokenWithExpiry is CreateAgentShareToken with an optional
	// token expiry, used by per-endpoint labeled keys.
	CreateAgentShareTokenWithExpiry(ctx context.Context, projectID, userID, name string, scopes []string, expiresAt *time.Time) (*apitoken.CreateApiTokenResponseDTO, error)
	UpdateScopes(ctx context.Context, tokenID, projectID, userID string, scopes []string) (*apitoken.ApiTokenDTO, error)
	Revoke(ctx context.Context, tokenID, projectID, userID string) error
	Regenerate(ctx context.Context, tokenID, projectID, userID string) (*apitoken.CreateApiTokenResponseDTO, error)
	// RegenerateWith additionally runs after inside the same transaction with
	// the new token ID, so callers can update related rows atomically.
	RegenerateWith(ctx context.Context, tokenID, projectID, userID string, after func(ctx context.Context, tx bun.Tx, newTokenID string) error) (*apitoken.CreateApiTokenResponseDTO, error)
	// GetByID returns the token with its plaintext value decrypted from
	// core.api_tokens.token_encrypted when the server has token encryption
	// configured. Empty Token means the ciphertext was never stored.
	GetByID(ctx context.Context, tokenID, projectID string) (*apitoken.GetApiTokenResponseDTO, error)
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
	// SetRecipientEmails replaces the recorded invite recipients. It touches
	// only that column so recording an invite can never rewrite the allowlist
	// or the bound token.
	SetRecipientEmails(ctx context.Context, projectID, id string, emails []string) error
	ListLegacyTokens(ctx context.Context, projectID string) ([]*LegacyTokenRef, error)
}

// AgentRef is a minimal project-agent reference.
type AgentRef struct {
	ID      string
	Name    string
	Enabled bool
}

// agentDirectory resolves project agents. Implemented by Bun and injectable in
// tests. Agent sharing is handled by the agent-scoped MCP endpoint; these
// lookups exist for that lifecycle only.
type agentDirectory interface {
	// FindProjectAgentByID returns one project agent by ID, or nil when it does
	// not exist in the project.
	FindProjectAgentByID(ctx context.Context, projectID, id string) (*AgentRef, error)
	// FindAgentRefsByDefinitionID returns the project runtime agents linked to
	// an agent definition, either via the agent_definition_id FK or via the
	// chat-session strategy_type marker. The FK-linked row is ordered first,
	// then oldest created_at. Returns an empty slice when none exist.
	FindAgentRefsByDefinitionID(ctx context.Context, projectID, definitionID string) ([]AgentRef, error)
	// AgentDefinitionExists reports whether a kb.agent_definitions row with the
	// given id exists in the project.
	AgentDefinitionExists(ctx context.Context, projectID, definitionID string) (bool, error)
}

// ============================================================================
// Model
// ============================================================================

// MCPShareInstance binds one core.api_tokens credential to a named, optional
// tool allowlist. A NULL tool allowlist means "unrestricted" (all
// scope-permitted tools), matching legacy share tokens.
//
// Instances scope TOOLS ONLY; agent sharing is handled by the agent-scoped MCP
// endpoint.
type MCPShareInstance struct {
	bun.BaseModel `bun:"table:core.mcp_share_instances,alias:msi"`

	ID           string   `bun:"id,pk,type:uuid,default:gen_random_uuid()"`
	ProjectID    string   `bun:"project_id,type:uuid,notnull"`
	Name         string   `bun:"name,notnull"`
	Description  *string  `bun:"description"`
	TokenID      string   `bun:"token_id,type:uuid,notnull"`
	AllowedTools []string `bun:"allowed_tools,array"`
	// RecipientEmails lists the addresses this share key was emailed to. It is
	// attribution metadata: the key itself is never stored here.
	RecipientEmails []string   `bun:"recipient_emails,array,notnull,default:'{}'"`
	IsLegacy        bool       `bun:"is_legacy,notnull,default:false"`
	CreatedBy       *string    `bun:"created_by,type:uuid"`
	CreatedAt       time.Time  `bun:"created_at,notnull,default:now()"`
	UpdatedAt       time.Time  `bun:"updated_at,notnull,default:now()"`
	RevokedAt       *time.Time `bun:"revoked_at"`

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

// InstanceScope is the per-request tool-allowlist view resolved from an API
// token. A nil *InstanceScope (or one without a tool allowlist) means
// unrestricted. Share instances scope TOOLS ONLY; there is no agent allowlist.
type InstanceScope struct {
	HasToolAllowlist bool
	AllowedTools     []string
}

type instanceScopeKey struct{}

// WithInstanceScope stores the resolved instance scope in the request context
// so the service layer (e.g. the ADK ToolPool) can enforce the tool allowlist
// without changing the ExecuteTool signature.
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
// Tools is required and must name at least one tool. A nil allowlist (the
// internal representation of a legacy unrestricted share) is rejected on
// create: omitting the field used to mint a token whose read-only scopes did
// not match the "unrestricted" label, and the UI already requires a selection.
//
// Agents is retained ONLY so a legacy client that supplies the retired agent
// allowlist can be rejected explicitly; instances scope tools only and the
// agent-scoped MCP endpoint handles agent sharing.
type CreateShareInstanceRequest struct {
	Name        string    `json:"name"`
	Description *string   `json:"description,omitempty"`
	Tools       *[]string `json:"tools"`
	Agents      *[]string `json:"agents"`
}

// UpdateShareInstanceRequest is the request body for PATCH .../shares/:id.
// Nil pointers mean "leave unchanged". Agents is retained ONLY so a request
// that supplies the retired agent allowlist can be rejected explicitly.
type UpdateShareInstanceRequest struct {
	Name        *string   `json:"name"`
	Description *string   `json:"description"`
	Tools       *[]string `json:"tools"`
	Agents      *[]string `json:"agents"`
}

// ShareInstanceDTO is the non-secret representation of a share instance. It
// carries the tool allowlist only — instances no longer have an agent allowlist.
type ShareInstanceDTO struct {
	ID          string    `json:"id"`
	ProjectID   string    `json:"projectId"`
	Name        string    `json:"name"`
	Description *string   `json:"description,omitempty"`
	Tools       *[]string `json:"tools"`
	// TokenID is the credential id that authenticates this share. It is the
	// value graph write stamps record as kb.graph_objects.credential_id, so it
	// is the join key behind the "objects written via this share" view. The
	// token's secret is never exposed.
	TokenID string `json:"tokenId,omitempty"`
	// RecipientEmails lists the addresses this share key was invited to. It is
	// the attribution label for the share, not a revocation list: the key is
	// shared by every recipient.
	RecipientEmails []string `json:"recipientEmails"`
	// ReadOnly reports whether the allowlist grants only read scopes. Invite
	// emails use it so a read-write share is never described as read-only.
	ReadOnly   bool       `json:"readOnly"`
	IsLegacy   bool       `json:"isLegacy"`
	Status     string     `json:"status"`
	CreatedAt  time.Time  `json:"createdAt"`
	UpdatedAt  time.Time  `json:"updatedAt"`
	LastUsedAt *time.Time `json:"lastUsedAt,omitempty"`
}

// ShareInstanceListResponse is the response for GET .../shares.
type ShareInstanceListResponse struct {
	Instances []ShareInstanceDTO `json:"instances"`
	Total     int                `json:"total"`
}

// CreateShareInstanceResponse is returned by create/rotate and includes the raw
// token value exactly once.
//
// EmailsSent lists the invite recipients the new key was queued to, and
// EmailError describes a delivery problem (email unavailable, queue failure) so
// a client can tell the share was created but the invite did not go out. Both
// are empty when no invite was requested.
type CreateShareInstanceResponse struct {
	ShareInstanceDTO
	Token      string   `json:"token"`
	MCPURL     string   `json:"mcpUrl"`
	EmailsSent []string `json:"emailsSent,omitempty"`
	EmailError string   `json:"emailError,omitempty"`
}

// CreateShareInstanceHTTPRequest is the HTTP body for POST .../shares: the
// instance definition plus the optional invite recipients that receive the
// one-time key, endpoint, and client snippets by email.
type CreateShareInstanceHTTPRequest struct {
	CreateShareInstanceRequest
	Emails []string `json:"emails,omitempty"`
}

// RotateShareInstanceHTTPRequest is the optional HTTP body for
// POST .../shares/:id/rotate. An empty body rotates without sending email.
type RotateShareInstanceHTTPRequest struct {
	Emails []string `json:"emails,omitempty"`
}

// InviteShareInstanceHTTPRequest is the body for POST .../shares/:id/invite:
// the recipients that receive the share's existing key and setup instructions.
type InviteShareInstanceHTTPRequest struct {
	Emails []string `json:"emails"`
}

// InviteShareInstanceResponse reports the outcome of an ad-hoc invite. The key
// itself is deliberately absent — it is emailed, never returned to the caller —
// while TokenPrefix names which key went out, so a caller can confirm the
// existing key was re-sent rather than a replacement minted.
type InviteShareInstanceResponse struct {
	TokenPrefix string   `json:"tokenPrefix"`
	EmailsSent  []string `json:"emailsSent,omitempty"`
	EmailError  string   `json:"emailError,omitempty"`
}

// CatalogToolDTO is one includable tool in the catalog endpoint.
type CatalogToolDTO struct {
	Name          string `json:"name"`
	Description   string `json:"description"`
	RequiredScope string `json:"requiredScope"`
	Category      string `json:"category"`
}

// CatalogResponse is the response for GET .../mcp/tools. Templates carries the
// named presets resolved against Tools (see share_templates.go); it is additive
// so a client that only reads tools/total is unaffected.
type CatalogResponse struct {
	Tools     []CatalogToolDTO   `json:"tools"`
	Total     int                `json:"total"`
	Templates []ShareTemplateDTO `json:"templates,omitempty"`
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

// isNonShareableScope reports whether a tool's required scope is one a share
// instance must never derive onto its bound token. Two families are excluded:
// the administrative ("admin"/"admin:all") and account-level ("account:*")
// scopes, which would let a share token satisfy the share-admin route guard and
// chain into other instances; and the project-management write scope
// ("projects:write"), which covers credential/provider/project mutations
// (token-*, provider-configure-project, project-create) that must never be
// reachable through a share link. This is the belt-and-suspenders complement to
// the route guards.
func isNonShareableScope(scope string) bool {
	switch scope {
	case "admin", "admin:all", "projects:write":
		return true
	}
	return strings.HasPrefix(scope, "account:")
}

// normalizeToolAllowlist validates and de-duplicates an explicit tool
// allowlist. A nil input means the legacy unrestricted allowlist (kept for
// stored rows; CreateShareInstance rejects it so new shares always carry an
// explicit list). An empty input is rejected.
func normalizeToolAllowlist(tools *[]string, lookup toolLookupFunc) ([]string, error) {
	if tools == nil {
		return nil, nil
	}
	if len(*tools) == 0 {
		return nil, apperror.NewValidation("tools must not be empty; select at least one tool")
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
		if def.SuperadminOnly {
			return nil, apperror.NewValidation("operator tool cannot be shared: " + name)
		}
		if isNonShareableScope(def.RequiredScope) {
			return nil, apperror.NewValidation("tool cannot be shared because it requires an administrative or project-management scope: " + name)
		}
		if agentExecutionTools[name] || agentMutationTools[name] {
			return nil, apperror.NewValidation("tool cannot be shared because its side effects run outside the instance context: " + name)
		}
		out = append(out, name)
	}
	return dedupeStrings(out), nil
}

// shareToolAdditionalScopes lists scopes a tool needs at runtime but does not
// declare as its transport-level RequiredScope. search-knowledge, remember and
// forget call the /query, /remember and /forget loopbacks with the caller's own
// token, and those routes require chat:use; without this a share that includes
// one of them would 403 on every call. The legacy read-only share token carried
// chat:use for the same reason.
//
// This map is maintained by hand: a new tool that forwards the caller token to a
// chat route must be added here (TestShareLoopbackToolsDeriveChatUse guards the
// tools known today).
var shareToolAdditionalScopes = map[string][]string{
	"search-knowledge": {"chat:use"},
	"remember":         {"chat:use"},
	"forget":           {"chat:use"},
}

// addShareToolScopes merges a tool's runtime-only scopes into seen, refusing any
// scope a share must never carry.
func addShareToolScopes(seen map[string]bool, name string, scopes []string) error {
	for _, scope := range scopes {
		if isNonShareableScope(scope) {
			return apperror.NewValidation("tool requires an administrative or project-management scope: " + name)
		}
		seen[scope] = true
	}
	return nil
}

// deriveScopesForToolNames returns the union of the required scopes of the
// selected tools, plus any runtime-only scope a tool needs, plus the baseline
// projects:read scope.
func deriveScopesForToolNames(tools []string, lookup toolLookupFunc) ([]string, error) {
	seen := map[string]bool{instanceScopeBaseline: true}
	for _, name := range tools {
		def := lookup(name)
		if def == nil {
			return nil, apperror.NewValidation("unknown tool: " + name)
		}
		if def.SuperadminOnly {
			return nil, apperror.NewValidation("tool requires operator privileges: " + name)
		}
		if isNonShareableScope(def.RequiredScope) {
			return nil, apperror.NewValidation("tool requires an administrative or project-management scope: " + name)
		}
		if def.RequiredScope != "" {
			seen[def.RequiredScope] = true
		}
		if err := addShareToolScopes(seen, name, shareToolAdditionalScopes[name]); err != nil {
			return nil, err
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
//
// Callers pass the resolved legacy dispatch name (see legacyDispatchName), so a
// consolidated call is matched against the per-verb name it replaced and a share
// instance keeps exactly the granularity it was created with: allowing
// `agent-create` does not also allow `agent-write` action=delete. The allowlist
// may also name the consolidated tool — that is what the share catalog and the
// presets emit since consolidation — so a family entry permits the whole family
// and nothing outside it.
func InstanceAllowsTool(scope *InstanceScope, toolName string) bool {
	if scope == nil || !scope.HasToolAllowlist {
		return true
	}
	canonical := canonicalToolName(toolName)
	for _, t := range scope.AllowedTools {
		if t == toolName || t == canonical {
			return true
		}
	}
	return false
}

// InstanceDeniesTool reports whether the instance scope forbids the tool
// because of the tool allowlist. This is the single predicate used by
// transports and by Service.ExecuteTool.
func InstanceDeniesTool(scope *InstanceScope, toolName string) bool {
	if scope == nil {
		return false
	}
	return scope.HasToolAllowlist && !InstanceAllowsTool(scope, toolName)
}

// InstanceRestrictsContent reports whether the instance imposes an explicit
// tool allowlist. MCP resources and prompts are not covered by the tool
// allowlist, so they must fail closed for any restricted instance. A nil scope
// (legacy/unrestricted, including null-allowlist instances) keeps the historical
// behavior.
func InstanceRestrictsContent(scope *InstanceScope) bool {
	return scope != nil && scope.HasToolAllowlist
}

// FilterToolsForInstance applies the instance tool allowlist on top of scope
// filtering. Order is preserved.
//
// An allowlist saved before the catalog consolidation names per-verb tools, so
// those entries select the consolidated tool that replaced them. When only some
// actions of a family are allowlisted, the advertised tool narrows its `action`
// enum to them: the share must not advertise an action that InstanceAllowsTool
// (which sees the resolved per-verb name) would refuse.
func FilterToolsForInstance(tools []ToolDefinition, scope *InstanceScope) []ToolDefinition {
	if scope == nil || !scope.HasToolAllowlist {
		return tools
	}
	exact := make(map[string]bool, len(scope.AllowedTools))
	allowed := make(map[string]bool, len(scope.AllowedTools))
	actions := make(map[string]map[string]bool, len(scope.AllowedTools))
	for _, t := range scope.AllowedTools {
		exact[t] = true
		allowed[t] = true
		canonical := canonicalToolName(t)
		allowed[canonical] = true
		if alias, ok := toolAliases[t]; ok && alias.Tool == canonical {
			if actions[canonical] == nil {
				actions[canonical] = make(map[string]bool, 2)
			}
			actions[canonical][alias.Action] = true
		}
	}
	out := make([]ToolDefinition, 0, len(tools))
	for _, t := range tools {
		if !allowed[t.Name] {
			continue
		}
		if !exact[t.Name] {
			t = narrowToolActions(t, actions[t.Name])
		}
		out = append(out, t)
	}
	return out
}

// narrowToolActions restricts an advertised family's action enum to the actions
// an allowlist actually grants. It returns the definition unchanged when the
// filter would not narrow anything.
func narrowToolActions(def ToolDefinition, allowed map[string]bool) ToolDefinition {
	fam, ok := familyByName[def.Name]
	if !ok || len(allowed) == 0 || len(allowed) >= len(fam.Actions) {
		return def
	}
	enum := make([]string, 0, len(allowed))
	help := make([]string, 0, len(allowed))
	for _, act := range fam.Actions {
		if !allowed[act.Action] {
			continue
		}
		enum = append(enum, act.Action)
		help = append(help, act.Action+" "+act.Description)
	}
	prop, ok := def.InputSchema.Properties["action"]
	if !ok {
		return def
	}
	prop.Enum = enum
	prop.Description = "Operation to perform. " + strings.Join(help, "; ")
	// Copy the map: the definitions are also held by the tool index, and a
	// narrowed enum for one share must not linger there.
	props := make(map[string]PropertySchema, len(def.InputSchema.Properties))
	for k, v := range def.InputSchema.Properties {
		props[k] = v
	}
	props["action"] = prop
	def.InputSchema.Properties = props
	return def
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
	msi.allowed_tools, msi.recipient_emails, msi.is_legacy, msi.created_by,
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

func (r *bunShareInstanceStore) SetRecipientEmails(ctx context.Context, projectID, id string, emails []string) error {
	if emails == nil {
		emails = []string{}
	}
	_, err := r.db.NewUpdate().
		Model((*MCPShareInstance)(nil)).
		Set("recipient_emails = ?", pq.Array(emails)).
		Where("id = ?", id).
		Where("project_id = ?", projectID).
		Exec(ctx)
	if err != nil {
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

// FindAgentRefsByDefinitionID returns the runtime agents linked to an agent
// definition in the project:
//   - rows whose agent_definition_id FK points at the definition (authoritative,
//     ordered first); these are the primary runtime agents.
//   - FK-less marker rows whose strategy_type is "chat-session:<definitionID>"
//     (chat dummy agents) or "agent-def:<definitionID>" (OpenAI-compat agents);
//     those rows are created without the FK.
//
// Marker fallbacks are only considered for rows with a NULL FK, so a row linked
// to a different definition can never match by marker. Ordering puts the
// FK-linked row first, then oldest created_at with id as a deterministic
// tie-breaker. Returns an empty slice when none exist.
func (d *bunAgentDirectory) FindAgentRefsByDefinitionID(ctx context.Context, projectID, definitionID string) ([]AgentRef, error) {
	var rows []AgentRef
	err := d.db.NewSelect().
		TableExpr("kb.agents").
		Column("id", "name", "enabled").
		Where("project_id = ?", projectID).
		Where("(agent_definition_id = ? OR (agent_definition_id IS NULL AND (strategy_type = 'chat-session:' || ? OR strategy_type = 'agent-def:' || ?)))", definitionID, definitionID, definitionID).
		// NULLS LAST keeps the FK-linked agent first: without it, the NULL
		// result of (agent_definition_id = ?) for marker rows sorts first under
		// DESC (Postgres defaults DESC to NULLS FIRST), so marker rows would
		// precede the FK-linked row and resolveAgentShareTarget would pick the
		// wrong primary runtime agent.
		OrderExpr("(agent_definition_id = ?) DESC NULLS LAST, created_at ASC, id ASC", definitionID).
		Scan(ctx, &rows)
	if err != nil {
		return nil, apperror.NewDatabase(apperror.ErrDatabase.Message, err)
	}
	return rows, nil
}

// AgentDefinitionExists reports whether the project contains an agent
// definition with the given id. Returns false (not an error) when absent.
func (d *bunAgentDirectory) AgentDefinitionExists(ctx context.Context, projectID, definitionID string) (bool, error) {
	var id string
	err := d.db.NewSelect().
		TableExpr("kb.agent_definitions").
		Column("id").
		Where("id = ?", definitionID).
		Where("project_id = ?", projectID).
		Limit(1).
		Scan(ctx, &id)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return false, nil
		}
		return false, apperror.NewDatabase(apperror.ErrDatabase.Message, err)
	}
	return id != "", nil
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

// scopesGrantWrites reports whether any scope in the set permits mutation.
// Share tokens only ever carry fine-grained scopes, and every mutating one is
// spelled "<resource>:write" (there is no mutating scope without that suffix),
// so the suffix is the whole test.
func scopesGrantWrites(scopes []string) bool {
	for _, scope := range scopes {
		if strings.HasSuffix(scope, ":write") {
			return true
		}
	}
	return false
}

// ensureToolIndex makes the process-global tool index available before a
// capability label is derived from an allowlist. The index is built by
// GetToolDefinitions(ForProject), which the read paths never call otherwise, so
// without this a freshly started server reports every allowlisted share as
// read-write — and an invite email would claim write access a read-only key does
// not have — until some unrelated MCP request warms it.
func (s *Service) ensureToolIndex(ctx context.Context, projectID string) {
	if toolIndexReady() {
		return
	}
	s.GetToolDefinitionsForProject(ctx, projectID)
}

// shareIsReadOnly reports whether a share's allowlist grants only read scopes.
// A legacy or unrestricted (nil) allowlist is minted read-only. An allowlist
// that cannot be resolved is reported as NOT read-only: an invite email must
// never promise read-only access it cannot verify.
func (s *Service) shareIsReadOnly(tools []string, isLegacy bool) bool {
	if isLegacy || len(tools) == 0 {
		return true
	}
	scopes, err := s.scopesForAllowlist(tools)
	if err != nil {
		return false
	}
	return !scopesGrantWrites(scopes)
}

// toShareDTO builds the API view of an instance, filling the derived fields
// (ReadOnly) that need the tool registry.
func (s *Service) toShareDTO(inst *MCPShareInstance, now time.Time) ShareInstanceDTO {
	dto := inst.toDTO(now)
	dto.ReadOnly = s.shareIsReadOnly(inst.AllowedTools, inst.IsLegacy)
	return dto
}

// shareBaseURL returns the deployment's public API origin when one is
// configured (MCP_PUBLIC_URL), falling back to the request-derived base URL.
//
// Share invite emails and client snippets must point at the public origin: a
// call proxied from the web UI carries the internal compose service name in
// Host, which a recipient's MCP client cannot resolve.
func (s *Service) shareBaseURL(fallback string) string {
	if s.publicBaseURL != "" {
		return s.publicBaseURL
	}
	return fallback
}

// RecordShareRecipients stores the addresses a share key was invited to so the
// grant records who holds it. Addresses are lowercased, trimmed and merged with
// the existing list; the caller treats a failure as non-fatal because the
// invite email has already been queued.
func (s *Service) RecordShareRecipients(ctx context.Context, projectID, userID, id string, emails []string) ([]string, error) {
	if len(emails) == 0 {
		return nil, nil
	}
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
	merged := mergeRecipientEmails(inst.RecipientEmails, emails)
	if len(merged) == len(inst.RecipientEmails) {
		return merged, nil
	}
	if err := store.SetRecipientEmails(ctx, projectID, id, merged); err != nil {
		return nil, err
	}
	return merged, nil
}

// mergeRecipientEmails lowercases and de-duplicates the union of two address
// lists, preserving first-seen order.
// copyRecipients returns a non-nil copy so the API answer always carries
// recipientEmails as an array. A nil slice marshals to null, which every client
// would then have to special-case, and the spec promises an empty array.
func copyRecipients(in []string) []string {
	out := make([]string, len(in))
	copy(out, in)
	return out
}

func mergeRecipientEmails(existing, add []string) []string {
	seen := make(map[string]bool, len(existing)+len(add))
	out := make([]string, 0, len(existing)+len(add))
	for _, raw := range append(append([]string(nil), existing...), add...) {
		addr := strings.ToLower(strings.TrimSpace(raw))
		if addr == "" || seen[addr] {
			continue
		}
		seen[addr] = true
		out = append(out, addr)
	}
	return out
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
	// Instances scope tools only. Reject a retired agent allowlist explicitly
	// rather than silently ignoring it, and point users at the agent-scoped
	// endpoint.
	if req.Agents != nil {
		return nil, errAgentsNotSupported()
	}
	// New shares must carry an explicit allowlist. A nil allowlist is the
	// legacy "unrestricted" representation: the token gets the read-only scope
	// set, so the request's apparent intent (all tools) and what the key can
	// actually do diverge until the first write. Requiring the list here keeps
	// every new share reviewable and matches the UI, which requires a
	// selection. Legacy rows keep their NULL allowlist and stay readable.
	if req.Tools == nil {
		return nil, apperror.NewValidation("tools is required: select at least one tool")
	}

	// Populate the tool index (including project/relay tools) before validating.
	s.GetToolDefinitionsForProject(ctx, projectID)
	tools, err := normalizeToolAllowlist(req.Tools, s.GetToolByName)
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
		ID:           uuid.NewString(),
		ProjectID:    projectID,
		Name:         name,
		Description:  req.Description,
		TokenID:      token.ID,
		AllowedTools: tools,
		// An explicit empty slice, not nil: the column is NOT NULL and bun would
		// otherwise emit NULL for a nil array on insert.
		RecipientEmails: []string{},
		CreatedBy:       &userID,
		CreatedAt:       now,
		UpdatedAt:       now,
	}
	if err := store.Create(ctx, inst); err != nil {
		// Best-effort cleanup so we do not strand an untracked credential.
		_ = tokenSvc.Revoke(ctx, token.ID, projectID, userID)
		return nil, err
	}

	return &CreateShareInstanceResponse{
		ShareInstanceDTO: s.toShareDTO(inst, now),
		Token:            token.Token,
		MCPURL:           mcpEndpointURL(baseURL),
	}, nil
}

// errAgentsNotSupported is the unprocessable-entity rejection for a request that
// supplies the retired `agents` field. Project share instances scope tools only;
// agent sharing uses the agent-scoped MCP endpoint.
func errAgentsNotSupported() error {
	return apperror.New(422, "validation_error",
		"agents are not supported on project share instances; share an agent with the agent-scoped MCP endpoint instead")
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
	s.ensureToolIndex(ctx, projectID)
	now := time.Now().UTC()
	out := make([]ShareInstanceDTO, 0, len(instances))
	boundTokens := make(map[string]bool, len(instances))
	for _, inst := range instances {
		// A revoked instance stays bound to its token so the management list
		// never leaks a same-ID legacy token row, but is itself hidden.
		boundTokens[inst.TokenID] = true
		if inst.RevokedAt != nil {
			continue
		}
		out = append(out, s.toShareDTO(inst, now))
	}

	legacy, err := store.ListLegacyTokens(ctx, projectID)
	if err != nil {
		return nil, err
	}
	for _, tok := range legacy {
		if tok.RevokedAt != nil {
			continue
		}
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
	s.ensureToolIndex(ctx, projectID)
	dto := s.toShareDTO(inst, time.Now().UTC())
	return &dto, nil
}

// UpdateShareInstance updates name/description/tool allowlist and keeps the
// bound token scopes in sync. The token secret is never changed.
func (s *Service) UpdateShareInstance(ctx context.Context, projectID, userID, id string, req UpdateShareInstanceRequest) (*ShareInstanceDTO, error) {
	if err := s.EnsureProjectAdmin(ctx, projectID, userID); err != nil {
		return nil, err
	}
	// Instances scope tools only: reject the retired agent allowlist explicitly.
	if req.Agents != nil {
		return nil, errAgentsNotSupported()
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

	// Validate/normalize EVERYTHING before persisting anything, so token scopes
	// are never written for an invalid tool allowlist.
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
	dto := s.toShareDTO(&updated, updated.UpdatedAt)
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

// RotateShareInstance issues a replacement token, invalidates the old one, and
// keeps the allowlist intact. The replacement's scopes are re-derived from the
// allowlist on a best-effort basis (see the refresh block below): names that no
// longer resolve, or a legacy non-shareable entry, fall back to the old scopes
// rather than blocking rotation.
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

	// Scopes are a function of the allowlist, and that derivation can widen as a
	// tool gains a runtime-only scope (shareToolAdditionalScopes). Refresh the
	// bound token before the replacement copies its scopes, so a share created by
	// an older build heals on rotate. A zero-length allowlist is unrestricted and
	// keeps whatever scopes it has.
	if len(inst.AllowedTools) > 0 {
		s.GetToolDefinitionsForProject(ctx, projectID)
		// Derive from the names that still exist. A tool renamed or removed since
		// the share was minted must not brick the only key the recipient can be
		// given, and the surviving names can still heal (so a mixed allowlist of
		// stale and current entries heals as far as it can).
		resolvable := make([]string, 0, len(inst.AllowedTools))
		for _, name := range inst.AllowedTools {
			if s.GetToolByName(name) != nil {
				resolvable = append(resolvable, name)
			}
		}
		if len(resolvable) > 0 {
			scopes, serr := s.scopesForAllowlist(resolvable)
			// A policy refusal (a legacy non-shareable or operator-only tool) is
			// also not allowed to brick rotation: the replacement keeps the old
			// scopes, and the allowlist guard still refuses those calls.
			if serr == nil {
				if _, uerr := tokenSvc.UpdateScopes(ctx, inst.TokenID, projectID, userID, scopes); uerr != nil {
					return nil, uerr
				}
			}
		}
		// Fail before this point and the old token is untouched; a failure after
		// it leaves a valid, correctly-scoped old token.
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
		ShareInstanceDTO: s.toShareDTO(inst, inst.UpdatedAt),
		Token:            newToken.Token,
		MCPURL:           mcpEndpointURL(baseURL),
	}, nil
}

// RevealShareInstanceKey returns the plaintext key bound to an existing share
// instance, decrypted from core.api_tokens.token_encrypted. It is the ad-hoc
// invite path: the key is not rotated, so everyone already using the share
// keeps working.
//
// A deployment without token encryption stores no ciphertext, and a share whose
// key predates it cannot be recovered; that is a clear 422 `not_recoverable`
// rather than an empty or partial key, so the caller can offer rotation.
func (s *Service) RevealShareInstanceKey(ctx context.Context, projectID, userID, id string) (string, error) {
	if err := s.EnsureProjectAdmin(ctx, projectID, userID); err != nil {
		return "", err
	}
	store := s.shareStore()
	tokenSvc := s.shareTokenSvc()
	if store == nil || tokenSvc == nil {
		return "", apperror.NewInternal("share instance storage unavailable", nil)
	}
	inst, err := store.GetByID(ctx, projectID, id)
	if err != nil {
		return "", err
	}
	if inst == nil {
		return "", apperror.NewNotFound("Share instance", id)
	}
	if inst.RevokedAt != nil {
		return "", apperror.New(409, "share_instance_revoked", "Share instance is revoked and its key cannot be emailed")
	}
	result, err := tokenSvc.GetByID(ctx, inst.TokenID, projectID)
	if err != nil {
		return "", err
	}
	if result == nil || result.Token == "" {
		return "", apperror.New(422, "not_recoverable",
			"this share's key was stored before key encryption was enabled, so it cannot be emailed")
	}
	return result.Token, nil
}

// shareKeyPrefix mirrors apitoken's token prefix rule (the first 12 characters)
// so a response can name the key it emailed without carrying the secret.
func shareKeyPrefix(key string) string {
	if len(key) < 12 {
		return key
	}
	return key[:12]
}

// ListToolCatalog returns the includable tool catalog for a project, admin-only.
func (s *Service) ListToolCatalog(ctx context.Context, projectID, userID string) (*CatalogResponse, error) {
	if err := s.EnsureProjectAdmin(ctx, projectID, userID); err != nil {
		return nil, err
	}
	tools := BuildToolCatalog(ctx, s, projectID)
	return &CatalogResponse{Tools: tools, Total: len(tools), Templates: ResolveShareTemplates(tools)}, nil
}

// BuildToolCatalog returns includable tools (AgentOnly excluded) ordered by
// category then name. Exposed for unit tests.
func BuildToolCatalog(ctx context.Context, s *Service, projectID string) []CatalogToolDTO {
	defs := s.GetToolDefinitionsForProject(ctx, projectID)
	out := make([]CatalogToolDTO, 0, len(defs))
	for _, d := range defs {
		if d.AgentOnly || d.SuperadminOnly {
			continue
		}
		// Tools requiring a non-shareable scope are never includable: deriving
		// "admin" would let a share token satisfy the share-admin route guard and
		// chain into sibling instances, and deriving "projects:write" would let a
		// share link mint credentials or reconfigure the project.
		if isNonShareableScope(d.RequiredScope) {
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

// ResolveInstanceScope resolves the tool allowlist for the authenticated API
// token (from AuthUser.APITokenID). It returns (nil, nil) for legacy/unknown
// tokens (including bound instances with a null allowlist), meaning
// "unrestricted".
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
	if len(inst.AllowedTools) == 0 {
		return nil, nil
	}
	return &InstanceScope{HasToolAllowlist: true, AllowedTools: inst.AllowedTools}, nil
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
		ID:              inst.ID,
		ProjectID:       inst.ProjectID,
		Name:            inst.Name,
		Description:     inst.Description,
		TokenID:         inst.TokenID,
		RecipientEmails: copyRecipients(inst.RecipientEmails),
		IsLegacy:        inst.IsLegacy,
		Status:          shareInstanceStatus(inst.RevokedAt, inst.TokenRevokedAt, inst.TokenExpiresAt, now),
		CreatedAt:       inst.CreatedAt,
		UpdatedAt:       inst.UpdatedAt,
		LastUsedAt:      inst.TokenLastUsedAt,
	}
	if len(inst.AllowedTools) > 0 {
		tools := append([]string(nil), inst.AllowedTools...)
		dto.Tools = &tools
	}
	return dto
}

func legacyTokenToDTO(projectID string, tok *LegacyTokenRef, now time.Time) ShareInstanceDTO {
	return ShareInstanceDTO{
		ID:        tok.ID,
		ProjectID: projectID,
		Name:      tok.Name,
		// A legacy share's row id IS its token id, and legacy shares are
		// minted read-only, so both attribution fields are unambiguous.
		TokenID:         tok.ID,
		RecipientEmails: []string{},
		ReadOnly:        true,
		IsLegacy:        true,
		Status:          shareInstanceStatus(nil, tok.RevokedAt, tok.ExpiresAt, now),
		CreatedAt:       tok.CreatedAt,
		UpdatedAt:       tok.CreatedAt,
		LastUsedAt:      tok.LastUsedAt,
	}
}
