package oauthas

import (
	"time"

	"github.com/uptrace/bun"
)

// OAuthClient is a dynamically registered public OAuth 2.1 client
// (core.oauth_clients). There is no client secret: every client uses
// token_endpoint_auth_method = "none".
type OAuthClient struct {
	bun.BaseModel `bun:"table:core.oauth_clients,alias:oc"`

	ID                      string     `bun:"id,pk,type:uuid,default:gen_random_uuid()"`
	ClientID                string     `bun:"client_id,notnull"`
	ClientName              string     `bun:"client_name,notnull"`
	RedirectURIs            []string   `bun:"redirect_uris,type:jsonb,notnull"`
	GrantTypes              []string   `bun:"grant_types,type:jsonb,notnull"`
	TokenEndpointAuthMethod string     `bun:"token_endpoint_auth_method,notnull"`
	Scope                   string     `bun:"scope,notnull"`
	CreatedAt               time.Time  `bun:"created_at,notnull,default:now()"`
	LastUsedAt              *time.Time `bun:"last_used_at"`
	RevokedAt               *time.Time `bun:"revoked_at"`
}

// OAuthAuthorizationCode is a single-use authorization code
// (core.oauth_authorization_codes). Only the SHA-256 hex hash of the code is
// stored; the plaintext code exists solely in the redirect back to the client.
type OAuthAuthorizationCode struct {
	bun.BaseModel `bun:"table:core.oauth_authorization_codes,alias:oac"`

	ID                  string     `bun:"id,pk,type:uuid,default:gen_random_uuid()"`
	CodeHash            string     `bun:"code_hash,notnull"`
	ClientID            string     `bun:"client_id,notnull"`
	UserID              string     `bun:"user_id,type:uuid,notnull"`
	ProjectID           string     `bun:"project_id,type:uuid,notnull"`
	Scopes              []string   `bun:"scopes,type:jsonb,notnull"`
	RedirectURI         string     `bun:"redirect_uri,notnull"`
	CodeChallenge       string     `bun:"code_challenge,notnull"`
	CodeChallengeMethod string     `bun:"code_challenge_method,notnull"`
	ExpiresAt           time.Time  `bun:"expires_at,notnull"`
	ConsumedAt          *time.Time `bun:"consumed_at"`
	CreatedAt           time.Time  `bun:"created_at,notnull,default:now()"`
}

// OAuthGrant links a registered client to the project-scoped API token minted
// for it (core.oauth_grants). Revoking the grant revokes the API token.
type OAuthGrant struct {
	bun.BaseModel `bun:"table:core.oauth_grants,alias:og"`

	ID         string     `bun:"id,pk,type:uuid,default:gen_random_uuid()"`
	ClientID   string     `bun:"client_id,notnull"`
	UserID     string     `bun:"user_id,type:uuid,notnull"`
	ProjectID  string     `bun:"project_id,type:uuid,notnull"`
	Scopes     []string   `bun:"scopes,type:jsonb,notnull"`
	TokenID    string     `bun:"token_id,type:uuid,notnull"`
	CreatedAt  time.Time  `bun:"created_at,notnull,default:now()"`
	LastUsedAt *time.Time `bun:"last_used_at"`
	RevokedAt  *time.Time `bun:"revoked_at"`
}

// grantRow is the joined read model behind the grants list: a grant plus the
// client and project display names. It is never written back.
type grantRow struct {
	ID          string     `bun:"id"`
	ClientID    string     `bun:"client_id"`
	ClientName  string     `bun:"client_name"`
	UserID      string     `bun:"user_id"`
	ProjectID   string     `bun:"project_id"`
	ProjectName string     `bun:"project_name"`
	Scopes      []string   `bun:"scopes,type:jsonb"`
	TokenID     string     `bun:"token_id"`
	CreatedAt   time.Time  `bun:"created_at"`
	LastUsedAt  *time.Time `bun:"last_used_at"`
	RevokedAt   *time.Time `bun:"revoked_at"`
}
