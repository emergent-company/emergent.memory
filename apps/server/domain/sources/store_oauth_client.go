package sources

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/uptrace/bun"

	"github.com/emergent-company/emergent.memory/pkg/apperror"
	"github.com/emergent-company/emergent.memory/pkg/auth"
	"github.com/emergent-company/emergent.memory/pkg/logger"
)

// OAuthClientConfig is a kb.oauth_client_configs row: the per-org or per-project
// OAuth2 client material. EncryptedClient is the encrypted {client_id,
// client_secret} blob and is never serialized into a response (json:"-").
//
// A row is org-wide when ProjectID is nil; a non-nil ProjectID marks a
// project-scoped override. Both forms still carry OrganizationID (the column
// stays NOT NULL).
// OAuthProviderConfig is one provider's scope configuration inside a named,
// vendor-level OAuth app. A provider without an entry uses its declared default.
type OAuthProviderConfig struct {
	// Scopes overrides the provider default when non-empty.
	Scopes []string `json:"scopes,omitempty"`
	// Strategy is the provider's named scope strategy (e.g. Drive's drive.file).
	Strategy string `json:"strategy,omitempty"`
}

type OAuthClientConfig struct {
	bun.BaseModel `bun:"table:kb.oauth_client_configs,alias:occ"`

	ID             uuid.UUID  `bun:"id,pk,type:uuid" json:"-"`
	OrganizationID uuid.UUID  `bun:"organization_id,type:uuid,notnull" json:"-"`
	ProjectID      *uuid.UUID `bun:"project_id,type:uuid" json:"-"`
	// Vendor is the app's vendor identity (e.g. "google"). It is the unit that
	// backs every OAuth provider of that vendor.
	Vendor string `bun:"vendor,notnull" json:"vendor"`
	// Name is the user-supplied app name, unique per scope and vendor.
	Name string `bun:"name,notnull" json:"name"`
	// Provider is retained for the legacy per-provider shim; new apps set it to
	// the vendor.
	Provider        string   `bun:"provider,notnull" json:"provider"`
	EncryptedClient []byte   `bun:"encrypted_client,type:bytea,notnull" json:"-"`
	Scopes          []string `bun:"scopes,type:jsonb,notnull" json:"scopes,omitempty"`
	Strategy        string   `bun:"strategy,notnull" json:"strategy,omitempty"`
	// ProviderConfigs holds per-provider scope configuration. One vendor app can
	// back several providers with different scopes (Drive strategy vs Gmail).
	ProviderConfigs map[string]OAuthProviderConfig `bun:"provider_configs,type:jsonb,notnull" json:"providerConfigs,omitempty"`
	// Config carries provider-specific, non-secret extras (e.g. the Google
	// Picker developer key/app id). It is never used for secrets.
	Config    map[string]any `bun:"config,type:jsonb,notnull" json:"config,omitempty"`
	CreatedAt time.Time      `bun:"created_at,notnull,default:now()" json:"createdAt"`
	UpdatedAt time.Time      `bun:"updated_at,notnull,default:now()" json:"updatedAt"`
}

// OrgOAuthAppStore is the named-app surface layered over OrgOAuthClientStore.
// It is optional: the legacy per-provider resolution path keeps working when a
// store only implements the old interface.
type OrgOAuthAppStore interface {
	GetOAuthClientConfigByID(ctx context.Context, orgID, id uuid.UUID) (*OAuthClientConfig, error)
	ListOAuthClientConfigsByVendor(ctx context.Context, orgID uuid.UUID, vendor string) ([]*OAuthClientConfig, error)
	CreateOAuthClientConfig(ctx context.Context, cfg *OAuthClientConfig) (*OAuthClientConfig, error)
	UpdateOAuthClientConfig(ctx context.Context, cfg *OAuthClientConfig) (*OAuthClientConfig, error)
	DeleteOAuthClientConfigByID(ctx context.Context, orgID, id uuid.UUID) error
	CountSourcesUsingOAuthConfig(ctx context.Context, id uuid.UUID) (int, error)
}

// ProjectOAuthAppStore is the project-scoped mirror of OrgOAuthAppStore.
type ProjectOAuthAppStore interface {
	GetProjectOAuthClientConfigByID(ctx context.Context, projectID, id uuid.UUID) (*OAuthClientConfig, error)
	ListProjectOAuthClientConfigsByVendor(ctx context.Context, projectID uuid.UUID, vendor string) ([]*OAuthClientConfig, error)
	CreateProjectOAuthClientConfig(ctx context.Context, cfg *OAuthClientConfig) (*OAuthClientConfig, error)
	UpdateProjectOAuthClientConfig(ctx context.Context, cfg *OAuthClientConfig) (*OAuthClientConfig, error)
	DeleteProjectOAuthClientConfigByID(ctx context.Context, projectID, id uuid.UUID) error
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

// ProjectOAuthClientStore is the persistence + authorization surface the Service
// uses to resolve and manage project-scoped OAuth client configs. *Repository
// implements it; tests inject a fake. It mirrors OrgOAuthClientStore but is
// keyed by project (project_admin authority, no org resolution).
type ProjectOAuthClientStore interface {
	// IsProjectAdmin reports whether userID holds project-admin authority over
	// projectID.
	IsProjectAdmin(ctx context.Context, projectID, userID string) (bool, error)
	// ResolveOrgID returns the organization that owns projectID (so a
	// project-scoped row can still carry its owning organization_id).
	ResolveOrgID(ctx context.Context, projectID uuid.UUID) (uuid.UUID, error)
	// GetProjectOAuthClientConfig returns the project's client config for a
	// provider. A missing row resolves to not-found.
	GetProjectOAuthClientConfig(ctx context.Context, projectID uuid.UUID, provider string) (*OAuthClientConfig, error)
	// ListProjectOAuthClientConfigs returns the project's configured clients,
	// ordered by provider. Never includes secrets.
	ListProjectOAuthClientConfigs(ctx context.Context, projectID uuid.UUID) ([]*OAuthClientConfig, error)
	// UpsertProjectOAuthClientConfig creates or replaces the (project, provider)
	// client.
	UpsertProjectOAuthClientConfig(ctx context.Context, cfg *OAuthClientConfig) (*OAuthClientConfig, error)
	// DeleteProjectOAuthClientConfig removes the (project, provider) client. A
	// missing row resolves to not-found.
	DeleteProjectOAuthClientConfig(ctx context.Context, projectID uuid.UUID, provider string) error
}

// compile-time assertions that *Repository satisfies both store interfaces.
var (
	_ OrgOAuthClientStore     = (*Repository)(nil)
	_ ProjectOAuthClientStore = (*Repository)(nil)
	_ OrgOAuthAppStore        = (*Repository)(nil)
	_ ProjectOAuthAppStore    = (*Repository)(nil)
)

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

// IsProjectAdmin delegates to the single app-side project-administration
// decision (pkg/auth.CanAdministerProject) so the project-client surfaces cannot
// drift from the other project-scoped decision points. Fail closed on a lookup
// error.
func (r *Repository) IsProjectAdmin(ctx context.Context, projectID, userID string) (bool, error) {
	ok, err := auth.CanAdministerProject(ctx, r.db, projectID, userID)
	if err != nil {
		r.log.Error("failed to check project_admin authority", logger.Error(err))
		return false, apperror.NewDatabase("failed to check project_admin authority", err)
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
// not-found when none is configured. Project-scoped overrides are excluded.
func (r *Repository) GetOAuthClientConfig(ctx context.Context, orgID uuid.UUID, provider string) (*OAuthClientConfig, error) {
	cfg := &OAuthClientConfig{}
	err := r.db.NewSelect().
		Model(cfg).
		Where("organization_id = ?", orgID).
		Where("project_id IS NULL").
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
// Project-scoped overrides are excluded.
func (r *Repository) ListOAuthClientConfigs(ctx context.Context, orgID uuid.UUID) ([]*OAuthClientConfig, error) {
	var cfgs []*OAuthClientConfig
	err := r.db.NewSelect().
		Model(&cfgs).
		Where("organization_id = ?", orgID).
		Where("project_id IS NULL").
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

// UpsertOAuthClientConfig creates or replaces the (org, provider) client. It is
// the legacy per-provider path; the new app surface uses Create/Update by id.
func (r *Repository) UpsertOAuthClientConfig(ctx context.Context, cfg *OAuthClientConfig) (*OAuthClientConfig, error) {
	if cfg.ID == uuid.Nil {
		cfg.ID = uuid.New()
	}
	if cfg.Vendor == "" {
		cfg.Vendor = cfg.Provider
	}
	if cfg.Name == "" {
		cfg.Name = cfg.Vendor
	}
	var existing OAuthClientConfig
	err := r.db.NewSelect().
		Model(&existing).
		Where("organization_id = ?", cfg.OrganizationID).
		Where("project_id IS NULL").
		Where("provider = ?", cfg.Provider).
		Limit(1).
		Scan(ctx)
	switch {
	case err == nil:
		cfg.ID = existing.ID
		return r.UpdateOAuthClientConfig(ctx, cfg)
	case !errors.Is(err, sql.ErrNoRows):
		r.log.Error("failed to look up oauth client config", logger.Error(err))
		return nil, apperror.NewDatabase("failed to look up oauth client config", err)
	}
	return r.CreateOAuthClientConfig(ctx, cfg)
}

// DeleteOAuthClientConfig removes the (org, provider) client. A missing row
// resolves to not-found.
func (r *Repository) DeleteOAuthClientConfig(ctx context.Context, orgID uuid.UUID, provider string) error {
	res, err := r.db.NewDelete().
		Model((*OAuthClientConfig)(nil)).
		Where("organization_id = ?", orgID).
		Where("project_id IS NULL").
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

// GetProjectOAuthClientConfig returns the project's client config for a
// provider, or not-found when none is configured.
func (r *Repository) GetProjectOAuthClientConfig(ctx context.Context, projectID uuid.UUID, provider string) (*OAuthClientConfig, error) {
	cfg := &OAuthClientConfig{}
	err := r.db.NewSelect().
		Model(cfg).
		Where("project_id = ?", projectID).
		Where("provider = ?", provider).
		Scan(ctx)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, apperror.ErrNotFound
		}
		r.log.Error("failed to get project oauth client config", logger.Error(err))
		return nil, apperror.NewDatabase("failed to get project oauth client config", err)
	}
	return cfg, nil
}

// ListProjectOAuthClientConfigs returns the project's configured clients by
// provider.
func (r *Repository) ListProjectOAuthClientConfigs(ctx context.Context, projectID uuid.UUID) ([]*OAuthClientConfig, error) {
	var cfgs []*OAuthClientConfig
	err := r.db.NewSelect().
		Model(&cfgs).
		Where("project_id = ?", projectID).
		Order("provider ASC").
		Scan(ctx)
	if err != nil {
		r.log.Error("failed to list project oauth client configs", logger.Error(err))
		return nil, apperror.NewDatabase("failed to list project oauth client configs", err)
	}
	if cfgs == nil {
		cfgs = []*OAuthClientConfig{}
	}
	return cfgs, nil
}

// UpsertProjectOAuthClientConfig creates or replaces the (project, provider)
// client. It is the legacy per-provider path; the new app surface uses
// Create/Update by id.
func (r *Repository) UpsertProjectOAuthClientConfig(ctx context.Context, cfg *OAuthClientConfig) (*OAuthClientConfig, error) {
	if cfg.ProjectID == nil {
		return nil, apperror.NewInternal("project oauth client config is missing project_id", nil)
	}
	if cfg.ID == uuid.Nil {
		cfg.ID = uuid.New()
	}
	if cfg.Vendor == "" {
		cfg.Vendor = cfg.Provider
	}
	if cfg.Name == "" {
		cfg.Name = cfg.Vendor
	}
	var existing OAuthClientConfig
	err := r.db.NewSelect().
		Model(&existing).
		Where("project_id = ?", *cfg.ProjectID).
		Where("provider = ?", cfg.Provider).
		Limit(1).
		Scan(ctx)
	switch {
	case err == nil:
		cfg.ID = existing.ID
		return r.UpdateProjectOAuthClientConfig(ctx, cfg)
	case !errors.Is(err, sql.ErrNoRows):
		r.log.Error("failed to look up project oauth client config", logger.Error(err))
		return nil, apperror.NewDatabase("failed to look up project oauth client config", err)
	}
	return r.CreateProjectOAuthClientConfig(ctx, cfg)
}

// DeleteProjectOAuthClientConfig removes the (project, provider) client. A
// missing row resolves to not-found.
func (r *Repository) DeleteProjectOAuthClientConfig(ctx context.Context, projectID uuid.UUID, provider string) error {
	res, err := r.db.NewDelete().
		Model((*OAuthClientConfig)(nil)).
		Where("project_id = ?", projectID).
		Where("provider = ?", provider).
		Exec(ctx)
	if err != nil {
		r.log.Error("failed to delete project oauth client config", logger.Error(err))
		return apperror.NewDatabase("failed to delete project oauth client config", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return apperror.ErrNotFound
	}
	return nil
}

// ---- named, vendor-level app store ----

// GetOAuthClientConfigByID returns one org-wide app by id.
func (r *Repository) GetOAuthClientConfigByID(ctx context.Context, orgID, id uuid.UUID) (*OAuthClientConfig, error) {
	cfg := &OAuthClientConfig{}
	err := r.db.NewSelect().
		Model(cfg).
		Where("organization_id = ?", orgID).
		Where("project_id IS NULL").
		Where("id = ?", id).
		Scan(ctx)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, apperror.ErrNotFound
		}
		r.log.Error("failed to get oauth app by id", logger.Error(err))
		return nil, apperror.NewDatabase("failed to get oauth app by id", err)
	}
	return cfg, nil
}

// ListOAuthClientConfigsByVendor returns the org-wide apps for a vendor.
func (r *Repository) ListOAuthClientConfigsByVendor(ctx context.Context, orgID uuid.UUID, vendor string) ([]*OAuthClientConfig, error) {
	var cfgs []*OAuthClientConfig
	err := r.db.NewSelect().
		Model(&cfgs).
		Where("organization_id = ?", orgID).
		Where("project_id IS NULL").
		Where("vendor = ?", vendor).
		OrderExpr("lower(name) ASC, created_at ASC").
		Scan(ctx)
	if err != nil {
		r.log.Error("failed to list oauth apps by vendor", logger.Error(err))
		return nil, apperror.NewDatabase("failed to list oauth apps by vendor", err)
	}
	if cfgs == nil {
		cfgs = []*OAuthClientConfig{}
	}
	return cfgs, nil
}

// CreateOAuthClientConfig inserts one org-wide app.
func (r *Repository) CreateOAuthClientConfig(ctx context.Context, cfg *OAuthClientConfig) (*OAuthClientConfig, error) {
	if cfg.ID == uuid.Nil {
		cfg.ID = uuid.New()
	}
	if cfg.Provider == "" {
		cfg.Provider = cfg.Vendor
	}
	if cfg.ProviderConfigs == nil {
		cfg.ProviderConfigs = map[string]OAuthProviderConfig{}
	}
	if cfg.Config == nil {
		cfg.Config = map[string]any{}
	}
	if cfg.Scopes == nil {
		cfg.Scopes = []string{}
	}
	if _, err := r.db.NewInsert().Model(cfg).Exec(ctx); err != nil {
		r.log.Error("failed to create oauth app", logger.Error(err))
		return nil, apperror.NewDatabase("failed to create oauth app", err)
	}
	return r.GetOAuthClientConfigByID(ctx, cfg.OrganizationID, cfg.ID)
}

// UpdateOAuthClientConfig updates one org-wide app by id.
func (r *Repository) UpdateOAuthClientConfig(ctx context.Context, cfg *OAuthClientConfig) (*OAuthClientConfig, error) {
	res, err := r.db.NewUpdate().
		Model((*OAuthClientConfig)(nil)).
		Set("name = ?", cfg.Name).
		Set("encrypted_client = ?", cfg.EncryptedClient).
		Set("scopes = ?", cfg.Scopes).
		Set("strategy = ?", cfg.Strategy).
		Set("provider_configs = ?", cfg.ProviderConfigs).
		Set("config = ?", cfg.Config).
		Set("updated_at = now()").
		Where("organization_id = ?", cfg.OrganizationID).
		Where("project_id IS NULL").
		Where("id = ?", cfg.ID).
		Exec(ctx)
	if err != nil {
		r.log.Error("failed to update oauth app", logger.Error(err))
		return nil, apperror.NewDatabase("failed to update oauth app", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return nil, apperror.ErrNotFound
	}
	return r.GetOAuthClientConfigByID(ctx, cfg.OrganizationID, cfg.ID)
}

// DeleteOAuthClientConfigByID removes one org-wide app by id.
func (r *Repository) DeleteOAuthClientConfigByID(ctx context.Context, orgID, id uuid.UUID) error {
	res, err := r.db.NewDelete().
		Model((*OAuthClientConfig)(nil)).
		Where("organization_id = ?", orgID).
		Where("project_id IS NULL").
		Where("id = ?", id).
		Exec(ctx)
	if err != nil {
		r.log.Error("failed to delete oauth app", logger.Error(err))
		return apperror.NewDatabase("failed to delete oauth app", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return apperror.ErrNotFound
	}
	return nil
}

// CountSourcesUsingOAuthConfig reports how many data sources reference an app.
func (r *Repository) CountSourcesUsingOAuthConfig(ctx context.Context, id uuid.UUID) (int, error) {
	n, err := r.db.NewSelect().
		TableExpr("kb.sources").
		Where("oauth_app_id = ?", id).
		Count(ctx)
	if err != nil {
		r.log.Error("failed to count sources using oauth app", logger.Error(err))
		return 0, apperror.NewDatabase("failed to count sources using oauth app", err)
	}
	return n, nil
}

// GetProjectOAuthClientConfigByID returns one project app by id.
func (r *Repository) GetProjectOAuthClientConfigByID(ctx context.Context, projectID, id uuid.UUID) (*OAuthClientConfig, error) {
	cfg := &OAuthClientConfig{}
	err := r.db.NewSelect().
		Model(cfg).
		Where("project_id = ?", projectID).
		Where("id = ?", id).
		Scan(ctx)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, apperror.ErrNotFound
		}
		r.log.Error("failed to get project oauth app by id", logger.Error(err))
		return nil, apperror.NewDatabase("failed to get project oauth app by id", err)
	}
	return cfg, nil
}

// ListProjectOAuthClientConfigsByVendor returns the project's apps for a vendor.
func (r *Repository) ListProjectOAuthClientConfigsByVendor(ctx context.Context, projectID uuid.UUID, vendor string) ([]*OAuthClientConfig, error) {
	var cfgs []*OAuthClientConfig
	err := r.db.NewSelect().
		Model(&cfgs).
		Where("project_id = ?", projectID).
		Where("vendor = ?", vendor).
		OrderExpr("lower(name) ASC, created_at ASC").
		Scan(ctx)
	if err != nil {
		r.log.Error("failed to list project oauth apps by vendor", logger.Error(err))
		return nil, apperror.NewDatabase("failed to list project oauth apps by vendor", err)
	}
	if cfgs == nil {
		cfgs = []*OAuthClientConfig{}
	}
	return cfgs, nil
}

// CreateProjectOAuthClientConfig inserts one project app.
func (r *Repository) CreateProjectOAuthClientConfig(ctx context.Context, cfg *OAuthClientConfig) (*OAuthClientConfig, error) {
	if cfg.ProjectID == nil {
		return nil, apperror.NewInternal("project oauth app is missing project_id", nil)
	}
	if cfg.ID == uuid.Nil {
		cfg.ID = uuid.New()
	}
	if cfg.Provider == "" {
		cfg.Provider = cfg.Vendor
	}
	if cfg.ProviderConfigs == nil {
		cfg.ProviderConfigs = map[string]OAuthProviderConfig{}
	}
	if cfg.Config == nil {
		cfg.Config = map[string]any{}
	}
	if cfg.Scopes == nil {
		cfg.Scopes = []string{}
	}
	if _, err := r.db.NewInsert().Model(cfg).Exec(ctx); err != nil {
		r.log.Error("failed to create project oauth app", logger.Error(err))
		return nil, apperror.NewDatabase("failed to create project oauth app", err)
	}
	return r.GetProjectOAuthClientConfigByID(ctx, *cfg.ProjectID, cfg.ID)
}

// UpdateProjectOAuthClientConfig updates one project app by id.
func (r *Repository) UpdateProjectOAuthClientConfig(ctx context.Context, cfg *OAuthClientConfig) (*OAuthClientConfig, error) {
	if cfg.ProjectID == nil {
		return nil, apperror.NewInternal("project oauth app is missing project_id", nil)
	}
	res, err := r.db.NewUpdate().
		Model((*OAuthClientConfig)(nil)).
		Set("name = ?", cfg.Name).
		Set("encrypted_client = ?", cfg.EncryptedClient).
		Set("scopes = ?", cfg.Scopes).
		Set("strategy = ?", cfg.Strategy).
		Set("provider_configs = ?", cfg.ProviderConfigs).
		Set("config = ?", cfg.Config).
		Set("updated_at = now()").
		Where("project_id = ?", *cfg.ProjectID).
		Where("id = ?", cfg.ID).
		Exec(ctx)
	if err != nil {
		r.log.Error("failed to update project oauth app", logger.Error(err))
		return nil, apperror.NewDatabase("failed to update project oauth app", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return nil, apperror.ErrNotFound
	}
	return r.GetProjectOAuthClientConfigByID(ctx, *cfg.ProjectID, cfg.ID)
}

// DeleteProjectOAuthClientConfigByID removes one project app by id.
func (r *Repository) DeleteProjectOAuthClientConfigByID(ctx context.Context, projectID, id uuid.UUID) error {
	res, err := r.db.NewDelete().
		Model((*OAuthClientConfig)(nil)).
		Where("project_id = ?", projectID).
		Where("id = ?", id).
		Exec(ctx)
	if err != nil {
		r.log.Error("failed to delete project oauth app", logger.Error(err))
		return apperror.NewDatabase("failed to delete project oauth app", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return apperror.ErrNotFound
	}
	return nil
}

// duplicateError reports a PostgreSQL unique violation so callers can answer
// 409 instead of leaking a 500 for a concurrent duplicate app name.
func duplicateError(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	return strings.Contains(msg, "23505") || strings.Contains(msg, "duplicate key value")
}
