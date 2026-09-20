package agents

import (
	"strings"
	"time"

	"github.com/uptrace/bun"
)

// ============================================================================
// Share link binding
// ============================================================================

// ShareLinkBinding is the resolved authorization result for a share request: the
// link, its backing agent definition, and the project/org it resolves to. Share
// routes set this in context (via RequireShareLink middleware) and ignore any
// client-supplied X-Project-ID / X-Org-ID headers.
type ShareLinkBinding struct {
	Link       *AgentShareLink
	Definition *AgentDefinition
	ProjectID  string
	OrgID      string
	Config     *ShareLinkConfig
}

// shareBindingCtxKey is the context key for the resolved share binding.
type shareBindingCtxKey struct{}

// ============================================================================
// Entities
// ============================================================================

// AgentShareLink is a public keyed agent-share link. It binds a reserved-scope
// API token (presented by the gateway as a Bearer credential) to one agent
// definition.
// Table: kb.agent_share_links
type AgentShareLink struct {
	bun.BaseModel `bun:"table:kb.agent_share_links,alias:asl"`

	ID                string           `bun:"id,pk,type:uuid,default:gen_random_uuid()"`
	ProjectID         string           `bun:"project_id,type:uuid,notnull"`
	AgentDefinitionID string           `bun:"agent_definition_id,type:uuid,notnull"`
	APITokenID        string           `bun:"api_token_id,type:uuid,notnull"`
	Label             string           `bun:"label,notnull"`
	Config            *ShareLinkConfig `bun:"config,type:jsonb"`
	CreatedByUserID   *string          `bun:"created_by_user_id,type:uuid"`
	CreatedAt         time.Time        `bun:"created_at,nullzero,notnull,default:current_timestamp"`
	UpdatedAt         time.Time        `bun:"updated_at,nullzero,notnull,default:current_timestamp"`
	LastUsedAt        *time.Time       `bun:"last_used_at"`
	RevokedAt         *time.Time       `bun:"revoked_at"`
	ExpiresAt         *time.Time       `bun:"expires_at"`
}

// EffectiveConfig returns the link's config with defaults applied when nil.
func (l *AgentShareLink) EffectiveConfig() *ShareLinkConfig {
	if l.Config == nil {
		return DefaultShareLinkConfig()
	}
	return l.Config
}

// IsRevoked reports whether the link is revoked.
func (l *AgentShareLink) IsRevoked() bool { return l.RevokedAt != nil }

// IsExpired reports whether the link is past its expiry (nil expiry = never).
func (l *AgentShareLink) IsExpired(now time.Time) bool {
	return l.ExpiresAt != nil && now.After(*l.ExpiresAt)
}

// AgentShareSession links a share link to an ACP session, keyed by the
// end-user's anonymous reference. Run linkage rides on kb.agent_runs
// .acp_session_id; no columns are added to kb.acp_sessions.
// Table: kb.agent_share_sessions
type AgentShareSession struct {
	bun.BaseModel `bun:"table:kb.agent_share_sessions,alias:ass"`

	ID             string     `bun:"id,pk,type:uuid,default:gen_random_uuid()"`
	ShareLinkID    string     `bun:"share_link_id,type:uuid,notnull"`
	ACPSessionID   string     `bun:"acp_session_id,type:uuid,notnull"`
	EndUserRef     string     `bun:"end_user_ref,notnull"`
	Title          *string    `bun:"title"`
	LastActivityAt *time.Time `bun:"last_activity_at"`
	IsArchived     bool       `bun:"is_archived,notnull,default:false"`
	CreatedAt      time.Time  `bun:"created_at,nullzero,notnull,default:current_timestamp"`
}

// AgentShareEndUser is the anonymous identity for one end user of one link.
// Access keys off end_user_ref ONLY — never off email. Unverified email grants
// no access; email is consent/contact metadata.
// Table: kb.agent_share_end_users
type AgentShareEndUser struct {
	bun.BaseModel `bun:"table:kb.agent_share_end_users,alias:aseu"`

	ID              string     `bun:"id,pk,type:uuid,default:gen_random_uuid()"`
	ShareLinkID     string     `bun:"share_link_id,type:uuid,notnull"`
	EndUserRef      string     `bun:"end_user_ref,notnull"`
	Email           *string    `bun:"email"`
	EmailNormalized *string    `bun:"email_normalized"`
	Verified        bool       `bun:"verified,notnull,default:false"`
	ConsentAt       *time.Time `bun:"consent_at"`
	CreatedAt       time.Time  `bun:"created_at,nullzero,notnull,default:current_timestamp"`
	LastSeenAt      *time.Time `bun:"last_seen_at"`
}

// AgentShareUsage is a rolling usage/budget counter for one link over one
// period. Composite PK (link_id, period_start).
// Table: kb.agent_share_usage
type AgentShareUsage struct {
	bun.BaseModel `bun:"table:kb.agent_share_usage,alias:asu"`

	LinkID      string    `bun:"link_id,pk,type:uuid"`
	PeriodStart time.Time `bun:"period_start,pk"`
	Messages    int       `bun:"messages,notnull,default:0"`
	Tokens      int64     `bun:"tokens,notnull,default:0"`
	CostUSD     float64   `bun:"cost_usd,notnull,default:0"`
	UpdatedAt   time.Time `bun:"updated_at,nullzero,notnull,default:current_timestamp"`
}

// ============================================================================
// Config (kb.agent_share_links.config jsonb schema)
// ============================================================================

// ShareLinkConfig is the effective per-link configuration. It is persisted as
// the link's config jsonb and always stored fully-merged (defaults + overrides)
// so plain bools can be read back without "absent vs false" ambiguity.
type ShareLinkConfig struct {
	LinkExpiryDays      int     `json:"link_expiry_days"`
	BudgetWindowSeconds int     `json:"budget_window_seconds"`
	BudgetMaxMessages   int     `json:"budget_max_messages"`
	BudgetMaxTokens     int64   `json:"budget_max_tokens"`
	BudgetMaxCostUSD    float64 `json:"budget_max_cost_usd"`
	// BudgetPerTurnTokens / BudgetPerTurnCostUSD are conservative per-turn
	// allowances reserved atomically before a run starts so a link near its
	// budget edge is denied before spending (reconciled to actuals after).
	BudgetPerTurnTokens      int64    `json:"budget_per_turn_tokens"`
	BudgetPerTurnCostUSD     float64  `json:"budget_per_turn_cost_usd"`
	MaxActiveSessionsPerUser int      `json:"max_active_sessions_per_user"`
	MaxConcurrentRuns        int      `json:"max_concurrent_runs"`
	MaxApprovalsPerSession   int      `json:"max_approvals_per_session"`
	ApprovalTimeoutSeconds   int      `json:"approval_timeout_seconds"`
	MaxMessageChars          int      `json:"max_message_chars"`
	RetentionDays            int      `json:"retention_days"`
	RequireEmail             bool     `json:"require_email"`
	ShowSessionList          bool     `json:"show_session_list"`
	SandboxEnabled           bool     `json:"sandbox_enabled"`
	AllowEndUserApprovals    bool     `json:"allow_end_user_approvals"`
	ToolAllowlist            []string `json:"tool_allowlist,omitempty"`
	// WelcomeMessage is an optional greeting shown to end users before they send
	// their first message.
	WelcomeMessage string `json:"welcome_message,omitempty"`
}

// DefaultShareLinkConfig returns the fully-populated default configuration.
func DefaultShareLinkConfig() *ShareLinkConfig {
	return &ShareLinkConfig{
		LinkExpiryDays:           30,
		BudgetWindowSeconds:      86400, // rolling 24h
		BudgetMaxMessages:        500,
		BudgetMaxTokens:          200000,
		BudgetMaxCostUSD:         5.0,
		BudgetPerTurnTokens:      8000,
		BudgetPerTurnCostUSD:     0.50,
		MaxActiveSessionsPerUser: 5,
		MaxConcurrentRuns:        3,
		MaxApprovalsPerSession:   10,
		ApprovalTimeoutSeconds:   600,
		MaxMessageChars:          8000,
		RetentionDays:            90,
		RequireEmail:             true,
		ShowSessionList:          true,
		SandboxEnabled:           false,
		AllowEndUserApprovals:    true,
	}
}

// ComputeShareToolDeny returns the tools hard-blocked on this link: every
// dangerous tool that is not explicitly allowlisted. The executor enforces this
// before the confirm gate, so an approval can never override a deny.
func (c *ShareLinkConfig) ComputeShareToolDeny() []string {
	if c == nil {
		c = DefaultShareLinkConfig()
	}
	allowed := make(map[string]bool, len(c.ToolAllowlist))
	for _, t := range c.ToolAllowlist {
		allowed[t] = true
	}
	deny := make([]string, 0, len(defaultDangerousShareTools))
	for _, t := range defaultDangerousShareTools {
		if !allowed[t] {
			deny = append(deny, t)
		}
	}
	return deny
}

// ShareLinkConfigInput is the request shape for create/PATCH config overrides.
// Pointer fields distinguish "absent" (keep default) from "explicit false".
type ShareLinkConfigInput struct {
	LinkExpiryDays           *int     `json:"link_expiry_days"`
	BudgetWindowSeconds      *int     `json:"budget_window_seconds"`
	BudgetMaxMessages        *int     `json:"budget_max_messages"`
	BudgetMaxTokens          *int64   `json:"budget_max_tokens"`
	BudgetMaxCostUSD         *float64 `json:"budget_max_cost_usd"`
	BudgetPerTurnTokens      *int64   `json:"budget_per_turn_tokens"`
	BudgetPerTurnCostUSD     *float64 `json:"budget_per_turn_cost_usd"`
	MaxActiveSessionsPerUser *int     `json:"max_active_sessions_per_user"`
	MaxConcurrentRuns        *int     `json:"max_concurrent_runs"`
	MaxApprovalsPerSession   *int     `json:"max_approvals_per_session"`
	ApprovalTimeoutSeconds   *int     `json:"approval_timeout_seconds"`
	MaxMessageChars          *int     `json:"max_message_chars"`
	RetentionDays            *int     `json:"retention_days"`
	RequireEmail             *bool    `json:"require_email"`
	ShowSessionList          *bool    `json:"show_session_list"`
	SandboxEnabled           *bool    `json:"sandbox_enabled"`
	AllowEndUserApprovals    *bool    `json:"allow_end_user_approvals"`
	ToolAllowlist            []string `json:"tool_allowlist"`
	WelcomeMessage           *string  `json:"welcome_message"`
}

// Apply merges input over the base config and returns the effective config.
func (in *ShareLinkConfigInput) Apply(base *ShareLinkConfig) *ShareLinkConfig {
	if base == nil {
		base = DefaultShareLinkConfig()
	}
	out := *base // copy
	if in == nil {
		return &out
	}
	if in.LinkExpiryDays != nil {
		out.LinkExpiryDays = *in.LinkExpiryDays
	}
	if in.BudgetWindowSeconds != nil {
		out.BudgetWindowSeconds = *in.BudgetWindowSeconds
	}
	if in.BudgetMaxMessages != nil {
		out.BudgetMaxMessages = *in.BudgetMaxMessages
	}
	if in.BudgetMaxTokens != nil {
		out.BudgetMaxTokens = *in.BudgetMaxTokens
	}
	if in.BudgetMaxCostUSD != nil {
		out.BudgetMaxCostUSD = *in.BudgetMaxCostUSD
	}
	if in.BudgetPerTurnTokens != nil {
		out.BudgetPerTurnTokens = *in.BudgetPerTurnTokens
	}
	if in.BudgetPerTurnCostUSD != nil {
		out.BudgetPerTurnCostUSD = *in.BudgetPerTurnCostUSD
	}
	if in.MaxActiveSessionsPerUser != nil {
		out.MaxActiveSessionsPerUser = *in.MaxActiveSessionsPerUser
	}
	if in.MaxConcurrentRuns != nil {
		out.MaxConcurrentRuns = *in.MaxConcurrentRuns
	}
	if in.MaxApprovalsPerSession != nil {
		out.MaxApprovalsPerSession = *in.MaxApprovalsPerSession
	}
	if in.ApprovalTimeoutSeconds != nil {
		out.ApprovalTimeoutSeconds = *in.ApprovalTimeoutSeconds
	}
	if in.MaxMessageChars != nil {
		out.MaxMessageChars = *in.MaxMessageChars
	}
	if in.RetentionDays != nil {
		out.RetentionDays = *in.RetentionDays
	}
	if in.RequireEmail != nil {
		out.RequireEmail = *in.RequireEmail
	}
	if in.ShowSessionList != nil {
		out.ShowSessionList = *in.ShowSessionList
	}
	if in.SandboxEnabled != nil {
		out.SandboxEnabled = *in.SandboxEnabled
	}
	if in.AllowEndUserApprovals != nil {
		out.AllowEndUserApprovals = *in.AllowEndUserApprovals
	}
	if in.ToolAllowlist != nil {
		out.ToolAllowlist = in.ToolAllowlist
	}
	if in.WelcomeMessage != nil {
		out.WelcomeMessage = *in.WelcomeMessage
	}
	return &out
}

// defaultDangerousShareTools is the built-in deny set for share runs: tools
// that mutate/delete data, manage schema/branch topology, configure providers,
// mint credentials, or otherwise grant destructive/admin capability. An owner
// may re-enable any of these via ToolAllowlist.
var defaultDangerousShareTools = []string{
	"entity-create", "entity-update", "entity-delete", "entity-restore",
	"relationship-create", "relationship-update", "relationship-delete",
	"schema-create", "schema-delete",
	"schema-migrate-preview", "schema-migrate-execute", "schema-migrate-rollback",
	"schema-migrate-commit", "schema-migration-job-status", "schema-migration-preview",
	"document-delete", "document-upload",
	"project-create",
	"provider-configure-org", "provider-configure-project", "provider-test",
	"token-create", "token-revoke", "token-delete",
	"skill-create", "skill-update", "skill-delete",
	"blueprint-create", "blueprint-update", "blueprint-publish",
	"blueprint-apply", "blueprint-unapply", "blueprint-new-version",
	"embedding-config-update", "embedding-pause", "embedding-resume",
	"queue-reextraction",
	"finalize-discovery",
	"agent-hook-create", "agent-hook-delete",
	"graph-branch-create", "graph-branch-delete", "graph-branch-merge",
	"forget",
}

// ============================================================================
// DTOs
// ============================================================================

// ShareLinkDTO is the owner-facing representation of a share link.
type ShareLinkDTO struct {
	ID                string           `json:"id"`
	ProjectID         string           `json:"projectId"`
	AgentDefinitionID string           `json:"agentDefinitionId"`
	Label             string           `json:"label"`
	Config            *ShareLinkConfig `json:"config"`
	APITokenPrefix    string           `json:"apiTokenPrefix"`
	Token             string           `json:"token,omitempty"` // only at create/rotate
	CreatedAt         time.Time        `json:"createdAt"`
	UpdatedAt         time.Time        `json:"updatedAt"`
	LastUsedAt        *time.Time       `json:"lastUsedAt,omitempty"`
	RevokedAt         *time.Time       `json:"revokedAt,omitempty"`
	ExpiresAt         *time.Time       `json:"expiresAt,omitempty"`
}

// SharePublicConfigDTO is the sanitized public config served to anonymous end
// users. It never exposes prompt/system config or project/org names.
type SharePublicConfigDTO struct {
	LinkID                   string  `json:"linkId"`
	AgentName                string  `json:"agentName"`
	AgentDescription         string  `json:"agentDescription,omitempty"`
	Model                    string  `json:"model,omitempty"`
	RequireEmail             bool    `json:"requireEmail"`
	ShowSessionList          bool    `json:"showSessionList"`
	MaxMessageChars          int     `json:"maxMessageChars"`
	AllowEndUserApprovals    bool    `json:"allowEndUserApprovals"`
	SandboxEnabled           bool    `json:"sandboxEnabled"`
	RetentionDays            int     `json:"retentionDays"`
	BudgetMaxMessages        int     `json:"budgetMaxMessages"`
	BudgetMaxTokens          int64   `json:"budgetMaxTokens"`
	BudgetMaxCostUSD         float64 `json:"budgetMaxCostUSD"`
	MaxActiveSessionsPerUser int     `json:"maxActiveSessionsPerUser"`
	MaxConcurrentRuns        int     `json:"maxConcurrentRuns"`
	WelcomeMessage           string  `json:"welcomeMessage,omitempty"`
	Icon                     string  `json:"icon,omitempty"`
	Color                    string  `json:"color,omitempty"`
}

// ShareSessionDTO is the end-user-facing representation of a share session.
type ShareSessionDTO struct {
	ID             string     `json:"id"`
	Title          *string    `json:"title,omitempty"`
	IsArchived     bool       `json:"isArchived"`
	LastActivityAt *time.Time `json:"lastActivityAt,omitempty"`
	CreatedAt      time.Time  `json:"createdAt"`
}

// ShareOwnerSessionDTO is the owner-facing representation of a share session,
// scoped to a project. Unlike ShareSessionDTO (the anonymous end-user view), it
// carries the backing agent definition id + name so the owner chat rail can
// filter and title shared sessions.
type ShareOwnerSessionDTO struct {
	ID                string     `json:"id"`
	AgentDefinitionID string     `json:"agentDefinitionId"`
	AgentName         string     `json:"agentName"`
	Title             string     `json:"title,omitempty"`
	ACPSessionID      string     `json:"acpSessionId"`
	IsArchived        bool       `json:"isArchived"`
	CreatedAt         time.Time  `json:"createdAt"`
	LastActivityAt    *time.Time `json:"lastActivityAt,omitempty"`
}

// ShareTranscriptMessage is a single chat message in a share-session transcript.
type ShareTranscriptMessage struct {
	Role    string `json:"role"`    // "user" | "assistant"
	Content string `json:"content"` // plain text
}

// ShareSessionDetailDTO is the transcript-bearing response for
// GET /api/share/agent/sessions/:id.
type ShareSessionDetailDTO struct {
	ID         string                   `json:"id"`
	Title      *string                  `json:"title,omitempty"`
	IsArchived bool                     `json:"isArchived"`
	Messages   []ShareTranscriptMessage `json:"messages"`
	CreatedAt  time.Time                `json:"createdAt"`
}

// ShareUsageDTO is the owner-facing usage summary for a link.
type ShareUsageDTO struct {
	LinkID      string    `json:"linkId"`
	PeriodStart time.Time `json:"periodStart"`
	Messages    int       `json:"messages"`
	Tokens      int64     `json:"tokens"`
	CostUSD     float64   `json:"costUsd"`
	UpdatedAt   time.Time `json:"updatedAt"`
}

// normalizeEmail lowercases and trims an email for the normalized column.
func normalizeEmail(email string) string {
	return strings.ToLower(strings.TrimSpace(email))
}
