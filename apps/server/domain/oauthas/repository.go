package oauthas

import (
	"context"
	"database/sql"
	"errors"
	"log/slog"
	"time"

	"github.com/uptrace/bun"

	"github.com/emergent-company/emergent.memory/pkg/apperror"
	"github.com/emergent-company/emergent.memory/pkg/auth"
	"github.com/emergent-company/emergent.memory/pkg/logger"
)

// Store is the persistence surface the Service uses. *Repository implements
// it; service tests inject an in-memory fake so the security-critical logic
// (PKCE, single-use, expiry, scope narrowing) is covered without a database.
type Store interface {
	CreateClient(ctx context.Context, client *OAuthClient) error
	GetClient(ctx context.Context, clientID string) (*OAuthClient, error)
	TouchClient(ctx context.Context, clientID string, at time.Time) error

	CreateCode(ctx context.Context, code *OAuthAuthorizationCode) error
	GetCodeByHash(ctx context.Context, codeHash string) (*OAuthAuthorizationCode, error)
	ConsumeCode(ctx context.Context, codeHash, clientID, redirectURI string, now time.Time) (bool, error)

	CreateGrant(ctx context.Context, grant *OAuthGrant) error
	GetGrantByIDForUser(ctx context.Context, grantID, userID string) (*OAuthGrant, error)
	GetGrantByTokenIDAndClient(ctx context.Context, tokenID, clientID string) (*OAuthGrant, error)
	ListGrantsByUser(ctx context.Context, userID string) ([]grantRow, error)
	RevokeGrant(ctx context.Context, grantID, userID string, now time.Time) (bool, error)
	RevokeGrantByTokenID(ctx context.Context, tokenID, clientID string, now time.Time) (bool, error)
	MarkGrantUsed(ctx context.Context, grantID string, at time.Time) error

	GetProjectName(ctx context.Context, projectID string) (string, error)
	CanWriteProject(ctx context.Context, projectID, userID string) (bool, error)
	ListWritableProjects(ctx context.Context, userID string) ([]OAuthProject, error)
}

// Repository handles data access for the OAuth authorization server.
type Repository struct {
	db  bun.IDB
	log *slog.Logger
}

// NewRepository creates a new OAuth authorization-server repository.
func NewRepository(db bun.IDB, log *slog.Logger) *Repository {
	return &Repository{
		db:  db,
		log: log.With(logger.Scope("oauthas.repo")),
	}
}

// dbErr wraps a database failure in the standard database apperror, so the
// chaining is declared once and reused across the repository.
func dbErr(err error) *apperror.Error {
	return apperror.NewDatabase("Database operation failed", err)
}

// CreateClient inserts a dynamically registered public client.
func (r *Repository) CreateClient(ctx context.Context, client *OAuthClient) error {
	_, err := r.db.NewInsert().
		Model(client).
		Returning("id").
		Exec(ctx)
	if err != nil {
		return dbErr(err)
	}
	return nil
}

// GetClient returns a client by client_id, or nil when it does not exist.
func (r *Repository) GetClient(ctx context.Context, clientID string) (*OAuthClient, error) {
	var client OAuthClient
	err := r.db.NewSelect().
		Model(&client).
		Where("client_id = ?", clientID).
		Scan(ctx)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, dbErr(err)
	}
	return &client, nil
}

// TouchClient records the most recent successful token exchange for a client.
func (r *Repository) TouchClient(ctx context.Context, clientID string, at time.Time) error {
	_, err := r.db.NewUpdate().
		Model((*OAuthClient)(nil)).
		Set("last_used_at = ?", at).
		Where("client_id = ?", clientID).
		Exec(ctx)
	if err != nil {
		return dbErr(err)
	}
	return nil
}

// CreateCode stores a hashed, single-use authorization code.
func (r *Repository) CreateCode(ctx context.Context, code *OAuthAuthorizationCode) error {
	_, err := r.db.NewInsert().
		Model(code).
		Exec(ctx)
	if err != nil {
		return dbErr(err)
	}
	return nil
}

// GetCodeByHash returns the code row for a hash, or nil when not found.
func (r *Repository) GetCodeByHash(ctx context.Context, codeHash string) (*OAuthAuthorizationCode, error) {
	var code OAuthAuthorizationCode
	err := r.db.NewSelect().
		Model(&code).
		Where("code_hash = ?", codeHash).
		Scan(ctx)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, dbErr(err)
	}
	return &code, nil
}

// ConsumeCode atomically marks a code consumed. It returns false when the code
// was already consumed, expired, or bound to a different client/redirect URI —
// exactly one concurrent exchange can win. clientID and redirectURI are part of
// the predicate so an attacker cannot burn another client's code.
func (r *Repository) ConsumeCode(ctx context.Context, codeHash, clientID, redirectURI string, now time.Time) (bool, error) {
	res, err := r.db.NewUpdate().
		Model((*OAuthAuthorizationCode)(nil)).
		Set("consumed_at = ?", now).
		Where("code_hash = ?", codeHash).
		Where("client_id = ?", clientID).
		Where("redirect_uri = ?", redirectURI).
		Where("consumed_at IS NULL").
		Where("expires_at > ?", now).
		Exec(ctx)
	if err != nil {
		return false, dbErr(err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return false, dbErr(err)
	}
	return n > 0, nil
}

// CreateGrant records the client ↔ API token link.
func (r *Repository) CreateGrant(ctx context.Context, grant *OAuthGrant) error {
	_, err := r.db.NewInsert().
		Model(grant).
		Exec(ctx)
	if err != nil {
		return dbErr(err)
	}
	return nil
}

// GetGrantByIDForUser returns a grant only when it belongs to userID.
func (r *Repository) GetGrantByIDForUser(ctx context.Context, grantID, userID string) (*OAuthGrant, error) {
	var grant OAuthGrant
	err := r.db.NewSelect().
		Model(&grant).
		Where("id = ?", grantID).
		Where("user_id = ?", userID).
		Scan(ctx)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, dbErr(err)
	}
	return &grant, nil
}

// GetGrantByTokenIDAndClient returns the grant linking a minted token to a
// client, or nil. Used by RFC 7009 revocation.
func (r *Repository) GetGrantByTokenIDAndClient(ctx context.Context, tokenID, clientID string) (*OAuthGrant, error) {
	var grant OAuthGrant
	err := r.db.NewSelect().
		Model(&grant).
		Where("token_id = ?", tokenID).
		Where("client_id = ?", clientID).
		Order("created_at DESC").
		Limit(1).
		Scan(ctx)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, dbErr(err)
	}
	return &grant, nil
}

// ListGrantsByUser returns the caller's grants with client and project names.
func (r *Repository) ListGrantsByUser(ctx context.Context, userID string) ([]grantRow, error) {
	var rows []grantRow
	err := r.db.NewSelect().
		TableExpr("core.oauth_grants AS g").
		ColumnExpr("g.id, g.client_id, g.user_id, g.project_id, g.scopes, g.token_id, g.created_at").
		ColumnExpr("COALESCE(tok.last_used_at, g.last_used_at) AS last_used_at").
		ColumnExpr("g.revoked_at").
		ColumnExpr("COALESCE(c.client_name, '') AS client_name").
		ColumnExpr("COALESCE(p.name, '') AS project_name").
		Join("LEFT JOIN core.oauth_clients AS c ON c.client_id = g.client_id").
		Join("LEFT JOIN kb.projects AS p ON p.id = g.project_id").
		Join("LEFT JOIN core.api_tokens AS tok ON tok.id = g.token_id").
		Where("g.user_id = ?", userID).
		Order("g.created_at DESC").
		Scan(ctx, &rows)
	if err != nil {
		return nil, dbErr(err)
	}
	return rows, nil
}

// RevokeGrant marks one of the user's grants revoked and reports whether a row
// changed.
func (r *Repository) RevokeGrant(ctx context.Context, grantID, userID string, now time.Time) (bool, error) {
	res, err := r.db.NewUpdate().
		Model((*OAuthGrant)(nil)).
		Set("revoked_at = ?", now).
		Where("id = ?", grantID).
		Where("user_id = ?", userID).
		Where("revoked_at IS NULL").
		Exec(ctx)
	if err != nil {
		return false, dbErr(err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return false, dbErr(err)
	}
	return n > 0, nil
}

// RevokeGrantByTokenID marks the grant for a token/client pair revoked.
func (r *Repository) RevokeGrantByTokenID(ctx context.Context, tokenID, clientID string, now time.Time) (bool, error) {
	res, err := r.db.NewUpdate().
		Model((*OAuthGrant)(nil)).
		Set("revoked_at = ?", now).
		Where("token_id = ?", tokenID).
		Where("client_id = ?", clientID).
		Where("revoked_at IS NULL").
		Exec(ctx)
	if err != nil {
		return false, dbErr(err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return false, dbErr(err)
	}
	return n > 0, nil
}

// MarkGrantUsed records the last time the granted token was observed in use.
func (r *Repository) MarkGrantUsed(ctx context.Context, grantID string, at time.Time) error {
	_, err := r.db.NewUpdate().
		Model((*OAuthGrant)(nil)).
		Set("last_used_at = ?", at).
		Where("id = ?", grantID).
		Exec(ctx)
	if err != nil {
		return dbErr(err)
	}
	return nil
}

// GetProjectName returns a project's display name, or "" when it no longer
// exists.
func (r *Repository) GetProjectName(ctx context.Context, projectID string) (string, error) {
	var name string
	err := r.db.NewSelect().
		TableExpr("kb.projects").
		Column("name").
		Where("id = ?", projectID).
		Scan(ctx, &name)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return "", nil
		}
		return "", dbErr(err)
	}
	return name, nil
}

// CanWriteProject reports whether the user holds a write-capable project role
// (project_admin or project_user). project_viewer is read-only and is denied.
func (r *Repository) CanWriteProject(ctx context.Context, projectID, userID string) (bool, error) {
	var allowed bool
	err := r.db.NewRaw(`
		SELECT EXISTS (
			SELECT 1 FROM kb.project_memberships
			WHERE project_id = ? AND user_id = ? AND (role = ? OR role = ?)
		)
	`, projectID, userID, auth.RoleProjectAdmin, auth.RoleProjectUser).Scan(ctx, &allowed)
	if err != nil {
		return false, dbErr(err)
	}
	return allowed, nil
}

// ListWritableProjects returns the projects the user can write to, newest
// first. Slug is filled in by the service.
func (r *Repository) ListWritableProjects(ctx context.Context, userID string) ([]OAuthProject, error) {
	var projects []OAuthProject
	err := r.db.NewSelect().
		TableExpr("kb.projects AS p").
		ColumnExpr("p.id AS id").
		ColumnExpr("p.name AS name").
		Join("INNER JOIN kb.project_memberships AS pm ON pm.project_id = p.id").
		Where("pm.user_id = ?", userID).
		Where("(pm.role = ? OR pm.role = ?)", auth.RoleProjectAdmin, auth.RoleProjectUser).
		Where("p.deleted_at IS NULL").
		Order("p.created_at DESC").
		Scan(ctx, &projects)
	if err != nil {
		return nil, dbErr(err)
	}
	if projects == nil {
		projects = []OAuthProject{}
	}
	return projects, nil
}
