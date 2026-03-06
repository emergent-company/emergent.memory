package apitoken

import (
	"time"

	"github.com/uptrace/bun"
)

// ApiToken represents an API token from core.api_tokens
type ApiToken struct {
	bun.BaseModel `bun:"table:core.api_tokens,alias:at"`

	ID             string     `bun:"id,pk,type:uuid,default:uuid_generate_v4()"`
	ProjectID      string     `bun:"project_id,notnull,type:uuid"`
	UserID         string     `bun:"user_id,notnull,type:uuid"`
	Name           string     `bun:"name,notnull"`
	TokenHash      string     `bun:"token_hash,notnull"`
	TokenPrefix    string     `bun:"token_prefix,notnull"`
	TokenEncrypted *string    `bun:"token_encrypted"`
	Scopes         []string   `bun:"scopes,array"`
	CreatedAt      time.Time  `bun:"created_at,notnull,default:now()"`
	LastUsedAt     *time.Time `bun:"last_used_at"`
	RevokedAt      *time.Time `bun:"revoked_at"`
}

// ApiTokenDTO is the response DTO for API token endpoints (without sensitive data)
type ApiTokenDTO struct {
	ID          string     `json:"id"`
	Name        string     `json:"name"`
	TokenPrefix string     `json:"tokenPrefix"`
	Scopes      []string   `json:"scopes"`
	CreatedAt   time.Time  `json:"createdAt"`
	LastUsedAt  *time.Time `json:"lastUsedAt,omitempty"`
	IsRevoked   bool       `json:"isRevoked"`
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
	Scopes []string `json:"scopes" validate:"required,min=1,dive,oneof=schema:read data:read data:write agents:read agents:write"`
}

// Available scopes for API tokens
var ValidApiTokenScopes = []string{
	"schema:read",
	"data:read",
	"data:write",
	"agents:read",
	"agents:write",
}

// ToDTO converts an ApiToken entity to ApiTokenDTO
func (t *ApiToken) ToDTO() ApiTokenDTO {
	return ApiTokenDTO{
		ID:          t.ID,
		Name:        t.Name,
		TokenPrefix: t.TokenPrefix,
		Scopes:      t.Scopes,
		CreatedAt:   t.CreatedAt,
		LastUsedAt:  t.LastUsedAt,
		IsRevoked:   t.RevokedAt != nil,
	}
}
