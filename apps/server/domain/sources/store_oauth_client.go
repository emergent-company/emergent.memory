package sources

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/uptrace/bun"

	"github.com/emergent-company/emergent.memory/pkg/apperror"
	"github.com/emergent-company/emergent.memory/pkg/auth"
	"github.com/emergent-company/emergent.memory/pkg/logger"
)

// OAuthClientConfig is a kb.oauth_client_configs row: the org-scoped, per-provider
// OAuth2 client material. EncryptedClient is the encrypted {client_id,
// client_secret} blob and is never serialized into a response (json:"-").
type OAuthClientConfig struct {
	bun.BaseModel `bun:"table:kb.oauth_client_configs,alias:occ"`

	ID              uuid.UUID `bun:"id,pk,type:uuid" json:"-"`
	OrganizationID  uuid.UUID `bun:"organization_id,type:uuid,notnull" json:"-"`
	Provider        string    `bun:"provider,notnull" json:"provider"`
	EncryptedClient []byte    `bun:"encrypted_client,type:bytea,notnull" json:"-"`
	Scopes          []string  `bun:"scopes,type:jsonb,notnull" json:"scopes,omitempty"`
	Strategy        string    `bun:"strategy,notnull" json:"strategy,omitempty"`
	// Config carries provider-specific, non-secret extras (e.g. the Google
	// Picker developer key/app id). It is never used for secrets.
	Config    map[string]any `bun:"config,type:jsonb,notnull" json:"config,omitempty"`
	CreatedAt time.Time      `bun:"created_at,notnull,default:now()" json:"createdAt"`
	UpdatedAt time.Time      `bun:"updated_at,notnull,default:now()" json:"updatedAt"`
}

// OrgOAuthClientStore is the persistence + authorization surface the Service
// uses to resolve and manage org-scoped OAuth client configs. *Repository
// implements it; tests inject a fake. Keeping it an interface lets the
// resolution order (org → deployment default → error) be unit-tested without a
// database.
type OrgOAuthClientStore interface {
	// IsOrgAdmin reports whether userID holds org-admin authority over orgID.
	IsOrgAdmin(ctx context.Context, orgID, userID string) (bool, error)
	// ResolveOrgID returns the organization that owns projectID.
	ResolveOrgID(ctx context.Context, projectID uuid.UUID) (uuid.UUID, error)
	// GetOAuthClientConfig returns the org's client config for a provider.
	// A missing row resolves to not-found.
	GetOAuthClientConfig(ctx context.Context, orgID uuid.UUID, provider string) (*OAuthClientConfig, error)
	// ListOAuthClientConfigs returns the org's configured clients, ordered by
	// provider. Never includes secrets (the store model only carries the
	// encrypted blob).
	ListOAuthClientConfigs(ctx context.Context, orgID uuid.UUID) ([]*OAuthClientConfig, error)
	// UpsertOAuthClientConfig creates or replaces the (org, provider) client.
	UpsertOAuthClientConfig(ctx context.Context, cfg *OAuthClientConfig) (*OAuthClientConfig, error)
	// DeleteOAuthClientConfig removes the (org, provider) client. A missing row
	// resolves to not-found.
	DeleteOAuthClientConfig(ctx context.Context, orgID uuid.UUID, provider string) error
}

// compile-time assertion that *Repository satisfies the store interface.
var _ OrgOAuthClientStore = (*Repository)(nil)

// IsOrgAdmin delegates to the single app-side org-administration decision
// (pkg/auth.CanAdministerOrg) so the org-client surfaces cannot drift from the
// other org-scoped decision points. Fail closed on a lookup error.
func (r *Repository) IsOrgAdmin(ctx context.Context, orgID, userID string) (bool, error) {
	ok, err := auth.CanAdministerOrg(ctx, r.db, orgID, userID)
	if err != nil {
		r.log.Error("failed to check org_admin authority", logger.Error(err))
		return false, apperror.NewDatabase("failed to check org_admin authority", err)
	}
	return ok, nil
}

// ResolveOrgID returns the organization that owns a project. A missing project
// resolves to not-found.
func (r *Repository) ResolveOrgID(ctx context.Context, projectID uuid.UUID) (uuid.UUID, error) {
	var orgID uuid.UUID
	err := r.db.NewSelect().
		TableExpr("kb.projects").
		ColumnExpr("organization_id").
		Where("id = ?", projectID).
		Scan(ctx, &orgID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return uuid.Nil, apperror.ErrNotFound
		}
		r.log.Error("failed to resolve org for project", logger.Error(err))
		return uuid.Nil, apperror.NewDatabase("failed to resolve org for project", err)
	}
	return orgID, nil
}

// GetOAuthClientConfig returns the org's client config for a provider, or
// not-found when none is configured.
func (r *Repository) GetOAuthClientConfig(ctx context.Context, orgID uuid.UUID, provider string) (*OAuthClientConfig, error) {
	cfg := &OAuthClientConfig{}
	err := r.db.NewSelect().
		Model(cfg).
		Where("organization_id = ?", orgID).
		Where("provider = ?", provider).
		Scan(ctx)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, apperror.ErrNotFound
		}
		r.log.Error("failed to get oauth client config", logger.Error(err))
		return nil, apperror.NewDatabase("failed to get oauth client config", err)
	}
	return cfg, nil
}

// ListOAuthClientConfigs returns the org's configured clients by provider.
func (r *Repository) ListOAuthClientConfigs(ctx context.Context, orgID uuid.UUID) ([]*OAuthClientConfig, error) {
	var cfgs []*OAuthClientConfig
	err := r.db.NewSelect().
		Model(&cfgs).
		Where("organization_id = ?", orgID).
		Order("provider ASC").
		Scan(ctx)
	if err != nil {
		r.log.Error("failed to list oauth client configs", logger.Error(err))
		return nil, apperror.NewDatabase("failed to list oauth client configs", err)
	}
	if cfgs == nil {
		cfgs = []*OAuthClientConfig{}
	}
	return cfgs, nil
}

// UpsertOAuthClientConfig creates or replaces the (org, provider) client.
func (r *Repository) UpsertOAuthClientConfig(ctx context.Context, cfg *OAuthClientConfig) (*OAuthClientConfig, error) {
	if cfg.ID == uuid.Nil {
		cfg.ID = uuid.New()
	}
	_, err := r.db.NewInsert().
		Model(cfg).
		On(`CONFLICT (organization_id, provider) DO UPDATE SET
			encrypted_client = EXCLUDED.encrypted_client,
			scopes = EXCLUDED.scopes,
			strategy = EXCLUDED.strategy,
			config = EXCLUDED.config,
			updated_at = now()`).
		Exec(ctx)
	if err != nil {
		r.log.Error("failed to upsert oauth client config", logger.Error(err))
		return nil, apperror.NewDatabase("failed to upsert oauth client config", err)
	}
	// Return the persisted (possibly updated) row so the caller sees the
	// canonical created_at/updated_at.
	return r.GetOAuthClientConfig(ctx, cfg.OrganizationID, cfg.Provider)
}

// DeleteOAuthClientConfig removes the (org, provider) client. A missing row
// resolves to not-found.
func (r *Repository) DeleteOAuthClientConfig(ctx context.Context, orgID uuid.UUID, provider string) error {
	res, err := r.db.NewDelete().
		Model((*OAuthClientConfig)(nil)).
		Where("organization_id = ?", orgID).
		Where("provider = ?", provider).
		Exec(ctx)
	if err != nil {
		r.log.Error("failed to delete oauth client config", logger.Error(err))
		return apperror.NewDatabase("failed to delete oauth client config", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return apperror.ErrNotFound
	}
	return nil
}
