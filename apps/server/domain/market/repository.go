package market

import (
	"context"
	"database/sql"
	"errors"
	"log/slog"
	"time"

	"github.com/uptrace/bun"

	"github.com/emergent-company/emergent.memory/pkg/apperror"
	"github.com/emergent-company/emergent.memory/pkg/logger"
)

// InstallStateApplied is the terminal state of a successful install.
const InstallStateApplied = "applied"

// Install records one marketplace install's provenance in a project.
type Install struct {
	bun.BaseModel `bun:"table:kb.market_installs,alias:mi"`

	ID           string    `bun:"id,pk,type:uuid,default:uuid_generate_v4()" json:"id"`
	ProjectID    string    `bun:"project_id,notnull,type:uuid" json:"project_id"`
	BlueprintID  string    `bun:"blueprint_id,notnull,type:uuid" json:"blueprint_id"`
	MarketIssuer string    `bun:"market_issuer,notnull" json:"market_issuer"`
	ListingSlug  string    `bun:"listing_slug,notnull" json:"listing_slug"`
	Semver       string    `bun:"semver,notnull" json:"semver"`
	Digest       string    `bun:"digest,notnull" json:"digest"`
	InstallID    string    `bun:"install_id,notnull,default:''" json:"install_id"`
	LicenseJTI   string    `bun:"license_jti,notnull,default:''" json:"license_jti,omitempty"`
	InstalledBy  *string   `bun:"installed_by,type:uuid" json:"installed_by,omitempty"`
	InstalledAt  time.Time `bun:"installed_at,notnull,default:now()" json:"installed_at"`
	State        string    `bun:"state,notnull,default:'applied'" json:"state"`
}

// Repository stores and reads kb.market_installs rows.
type Repository struct {
	db  bun.IDB
	log *slog.Logger
}

func NewRepository(db bun.IDB, log *slog.Logger) *Repository {
	return &Repository{db: db, log: log.With(logger.Scope("market.repo"))}
}

// Get returns the install row for (project, issuer, listing, digest), or
// (nil, nil) when none exists.
func (r *Repository) Get(ctx context.Context, projectID, issuer, slug, digest string) (*Install, error) {
	var row Install
	err := r.db.NewSelect().
		Model(&row).
		Where("project_id = ?", projectID).
		Where("market_issuer = ?", issuer).
		Where("listing_slug = ?", slug).
		Where("digest = ?", CleanDigest(digest)).
		Scan(ctx)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		r.log.Error("failed to read market install", logger.Error(err))
		return nil, apperror.ErrDatabase.WithInternal(err)
	}
	return &row, nil
}

// Upsert records an install, converging on the unique
// (project_id, market_issuer, listing_slug, digest) key.
func (r *Repository) Upsert(ctx context.Context, install *Install) error {
	if install.InstalledAt.IsZero() {
		install.InstalledAt = time.Now()
	}
	if install.State == "" {
		install.State = InstallStateApplied
	}
	install.Digest = CleanDigest(install.Digest)
	_, err := r.db.NewInsert().
		Model(install).
		On("CONFLICT (project_id, market_issuer, listing_slug, digest) DO UPDATE").
		Set("blueprint_id = EXCLUDED.blueprint_id").
		Set("semver = EXCLUDED.semver").
		Set("install_id = EXCLUDED.install_id").
		Set("license_jti = EXCLUDED.license_jti").
		Set("installed_by = EXCLUDED.installed_by").
		Set("installed_at = EXCLUDED.installed_at").
		Set("state = EXCLUDED.state").
		Returning("id").
		Exec(ctx)
	if err != nil {
		r.log.Error("failed to record market install", logger.Error(err))
		return apperror.ErrDatabase.WithInternal(err)
	}
	return nil
}
