package apitoken

import (
	"time"

	"github.com/uptrace/bun"
)

// ApiToken represents an API token from core.api_tokens
type ApiToken struct {
	bun.BaseModel `bun:"table:core.api_tokens,alias:at"`

	ID             string     `bun:"id,pk,type:uuid,default:uuid_generate_v4()"`
	ProjectID      *string    `bun:"project_id,type:uuid"`
	UserID         *string    `bun:"user_id,type:uuid"`
	Name           string     `bun:"name,notnull"`
	TokenHash      string     `bun:"token_hash,notnull"`
	TokenPrefix    string     `bun:"token_prefix,notnull"`
	TokenEncrypted *string    `bun:"token_encrypted"`
	Scopes         []string   `bun:"scopes,array"`
	CreatedAt      time.Time  `bun:"created_at,notnull,default:now()"`
	LastUsedAt     *time.Time `bun:"last_used_at"`
	RevokedAt      *time.Time `bun:"revoked_at"`
	ExpiresAt      *time.Time `bun:"expires_at"`
}

// ApiTokenDTO is the response DTO for API token endpoints (without sensitive data)
type ApiTokenDTO struct {
	ID          string     `json:"id"`
	ProjectID   *string    `json:"projectId,omitempty"`
	Name        string     `json:"name"`
	TokenPrefix string     `json:"tokenPrefix"`
	Scopes      []string   `json:"scopes"`
	CreatedAt   time.Time  `json:"createdAt"`
	LastUsedAt  *time.Time `json:"lastUsedAt,omitempty"`
	IsRevoked   bool       `json:"isRevoked"`
	ExpiresAt   *time.Time `json:"expiresAt,omitempty"`
	// OwnedByCaller is true when the listed token belongs to the authenticated
	// caller. It is set by the list endpoints (not ToDTO) and omitted otherwise.
	OwnedByCaller bool `json:"ownedByCaller,omitempty"`
}

// CreateApiTokenResponseDTO extends ApiTokenDTO with the full token value (only at creation)
type CreateApiTokenResponseDTO struct {
	ApiTokenDTO
	Token string `json:"token"`
}

// GetApiTokenResponseDTO extends ApiTokenDTO with the full token value (decrypted from storage)
type GetApiTokenResponseDTO struct {
	ApiTokenDTO
	Token string `json:"token,omitempty"`
}

// ApiTokenListResponseDTO is the response for listing tokens
type ApiTokenListResponseDTO struct {
	Tokens []ApiTokenDTO `json:"tokens"`
	Total  int           `json:"total"`
}

// CreateApiTokenRequest is the request body for creating a token
type CreateApiTokenRequest struct {
	Name   string   `json:"name" validate:"required,min=1,max=255"`
	Scopes []string `json:"scopes" validate:"required,min=1,dive,oneof=schema:read schema:write agents:read agents:write projects:read projects:write chat:use graph:read graph:write schema:migrate branches:read branches:write search journal:read journal:write skills:read skills:write documents:read documents:write documents:delete email:read email:write sources:write ingest:write chunks:read chunks:write search:read graph:search:read extraction:read extraction:write tasks:read tasks:write user-activity:read user-activity:write notifications:read notifications:write admin admin:all project:admin"`
}

// CreateAccountTokenRequest is the request body for creating an account-level token (no project binding)
type CreateAccountTokenRequest struct {
	Name   string   `json:"name" validate:"required,min=1,max=255"`
	Scopes []string `json:"scopes" validate:"required,min=1,dive,oneof=schema:read schema:write agents:read agents:write projects:read projects:write chat:use graph:read graph:write schema:migrate branches:read branches:write search journal:read journal:write skills:read skills:write documents:read documents:write documents:delete email:read email:write sources:write ingest:write chunks:read chunks:write search:read graph:search:read extraction:read extraction:write tasks:read tasks:write user-activity:read user-activity:write notifications:read notifications:write admin admin:all project:admin"`
}

// UpdateApiTokenScopesRequest is the request body for updating token scopes
type UpdateApiTokenScopesRequest struct {
	Scopes []string `json:"scopes" validate:"required,min=1,dive,oneof=schema:read schema:write agents:read agents:write projects:read projects:write chat:use graph:read graph:write schema:migrate branches:read branches:write search journal:read journal:write skills:read skills:write documents:read documents:write documents:delete email:read email:write sources:write ingest:write chunks:read chunks:write search:read graph:search:read extraction:read extraction:write tasks:read tasks:write user-activity:read user-activity:write notifications:read notifications:write admin admin:all project:admin"`
}

// ValidApiTokenScopes is every scope a stored token may carry, including the
// scopes only internal mint paths produce (marker scopes and the legacy
// data:read/data:write umbrellas). User-facing create/update surfaces validate
// against UserMintableScopes instead.
var ValidApiTokenScopes = []string{
	// Coarse-grained (legacy, kept for backwards compatibility)
	"schema:read",
	"schema:write",
	// data:read/data:write are retired from user-facing minting: they remain
	// valid stored scopes so previously minted tokens, device/webhook
	// credentials and agent shares keep their effective access (see
	// pkg/auth.ScopeImplies), but no create/update surface accepts them. Use
	// the fine-grained scopes (documents:*, graph:*, search, journal:*)
	// instead — see UserMintableScopes.
	"data:read",
	"data:write",
	"agents:read",
	"agents:write",
	"projects:read",
	"projects:write",
	"chat:use",
	// Fine-grained MCP scopes
	"graph:read",
	"graph:write",
	"schema:migrate",
	"branches:read",
	"branches:write",
	"search",
	"journal:read",
	"journal:write",
	"skills:read",
	"skills:write",
	"documents:read",
	"documents:write",
	"email:read",
	"email:write",
	"sources:write",
	// REST scopes the retired data:read/data:write umbrellas used to imply.
	// They are mintable directly so a token minted after the umbrella
	// retirement can still reach the REST routes gated on them (#80).
	"documents:delete",
	"ingest:write",
	"chunks:read",
	"chunks:write",
	"search:read",
	"graph:search:read",
	"extraction:read",
	"extraction:write",
	"tasks:read",
	"tasks:write",
	"user-activity:read",
	"user-activity:write",
	"notifications:read",
	"notifications:write",
	"admin",
	"admin:all",
	// project:admin is a project-scoped admin umbrella, mintable by a project
	// admin (or the owning org's org_admin) on a project-bound token only. It is
	// NOT a platform scope: unlike admin/admin:all it grants no cross-tenant or
	// org/platform authority (see pkg/auth.ScopeImplies).
	"project:admin",
	// Marker scope minted on per-agent MCP share credentials. It is reserved for
	// the internal agent-share mint path (Service.CreateAgentShareToken): it is
	// deliberately absent from the CreateApiTokenRequest /
	// CreateAccountTokenRequest / UpdateApiTokenScopesRequest oneof tags and
	// Service.Create rejects it, so user-facing token creation cannot mint it.
	// The project MCP transports reject any credential carrying it (see
	// domain/mcp/agent_mcp_share.go) and only the per-agent endpoint accepts it.
	"mcp:agent-call",
	// Marker scope minted on public agent-share link keys. Like mcp:agent-call
	// it is reserved for an internal mint path (Service.CreateAgentChatShareToken)
	// and deliberately absent from the user-facing oneof tags; Service.Create /
	// CreateAccountToken / UpdateScopes reject it. The gateway presents the
	// share key as a Bearer credential; share routes resolve token -> link ->
	// agent -> project and ignore client-supplied project/org headers.
	"share:agent-chat",
	// Marker scope minted on scoped per-device credentials. Like mcp:agent-call
	// and share:agent-chat it is reserved for an internal mint path
	// (Service.CreateDeviceToken) and deliberately absent from the user-facing
	// oneof tags; Service.Create / CreateAccountToken / UpdateScopes reject it.
	// The gateway recognises a device credential by introspecting for this
	// marker (see rejectDeviceTokenOutsideSurface); the server enforces the
	// exact-set device ceiling at validation time.
	"device:api",
	// Marker scope minted on scoped webhook trigger credentials. Like the other
	// marker scopes it is reserved for an internal mint path
	// (Service.CreateWebhookTriggerToken) and deliberately absent from the
	// user-facing oneof tags; Service.Create / CreateAccountToken / UpdateScopes
	// reject it. The server enforces the exact-set webhook ceiling and the
	// trigger-route surface guard at validation time.
	"webhook:trigger",
}

// nonMintableScopes are the ValidApiTokenScopes members no user-facing
// create/update surface may request: the retired coarse umbrellas and the
// reserved marker scopes that only internal mint paths may attach.
var nonMintableScopes = map[string]bool{
	"data:read":        true,
	"data:write":       true,
	"mcp:agent-call":   true,
	"share:agent-chat": true,
	"device:api":       true,
	"webhook:trigger":  true,
}

// UserMintableScopes is the subset of ValidApiTokenScopes a user-facing
// create/update surface accepts (project tokens, account tokens, the superadmin
// service-token preset, the token picker and the CLI). Internal mint paths
// (device, webhook, ephemeral, agent shares) validate against
// ValidApiTokenScopes instead, so their hardcoded ceilings keep minting.
var UserMintableScopes = func() []string {
	out := make([]string, 0, len(ValidApiTokenScopes))
	for _, s := range ValidApiTokenScopes {
		if !nonMintableScopes[s] {
			out = append(out, s)
		}
	}
	return out
}()

// ToDTO converts an ApiToken entity to ApiTokenDTO
func (t *ApiToken) ToDTO() ApiTokenDTO {
	return ApiTokenDTO{
		ID:          t.ID,
		ProjectID:   t.ProjectID,
		Name:        t.Name,
		TokenPrefix: t.TokenPrefix,
		Scopes:      t.Scopes,
		CreatedAt:   t.CreatedAt,
		LastUsedAt:  t.LastUsedAt,
		IsRevoked:   t.RevokedAt != nil,
		ExpiresAt:   t.ExpiresAt,
	}
}
