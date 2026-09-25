package standalone

import (
	"context"
	"log/slog"

	"github.com/uptrace/bun"

	"github.com/emergent-company/emergent.memory/internal/config"
	"github.com/emergent-company/emergent.memory/pkg/logger"
)

type BootstrapService struct {
	db  *bun.DB
	cfg *config.Config
	log *slog.Logger
}

func NewBootstrapService(db *bun.DB, cfg *config.Config, log *slog.Logger) *BootstrapService {
	return &BootstrapService{
		db:  db,
		cfg: cfg,
		log: log.With(logger.Scope("standalone.bootstrap")),
	}
}

func (s *BootstrapService) Initialize(ctx context.Context) error {
	if !s.cfg.Standalone.IsEnabled() {
		return nil
	}

	s.log.Info("standalone mode enabled, checking initialization status")

	initialized, err := s.isInitialized(ctx)
	if err != nil {
		return err
	}

	if initialized {
		s.log.Info("standalone environment already initialized")
	} else {
		s.log.Info("initializing standalone environment",
			slog.String("user_email", s.cfg.Standalone.UserEmail),
			slog.String("org_name", s.cfg.Standalone.OrgName),
			slog.String("project_name", s.cfg.Standalone.ProjectName),
		)

		if err := s.createStandaloneResources(ctx); err != nil {
			s.log.Error("failed to initialize standalone environment", logger.Error(err))
			return err
		}

		s.log.Info("standalone environment initialized successfully")
	}

	// The secondary identity is provisioned independently and idempotently, so
	// a long-lived standalone DB that already holds the primary identity still
	// gets (or refreshes) the invitee on every startup. Without this, the
	// second-identity e2e tests silently skip on a reused database.
	if err := s.ensureSecondUser(ctx); err != nil {
		s.log.Error("failed to ensure secondary standalone identity", logger.Error(err))
		return err
	}

	return nil
}

func (s *BootstrapService) isInitialized(ctx context.Context) (bool, error) {
	count, err := s.db.NewSelect().
		TableExpr("core.user_profiles").
		Where("zitadel_user_id = ?", "standalone").
		Count(ctx)

	if err != nil {
		return false, err
	}

	return count > 0, nil
}

func (s *BootstrapService) createStandaloneResources(ctx context.Context) error {
	var userID, orgID, projectID string

	err := s.db.RunInTx(ctx, nil, func(ctx context.Context, tx bun.Tx) error {
		var err error
		userID, err = s.createUser(ctx, tx)
		if err != nil {
			return err
		}

		orgID, err = s.createOrganization(ctx, tx, userID)
		if err != nil {
			return err
		}

		projectID, err = s.createProject(ctx, tx, orgID, userID)
		if err != nil {
			return err
		}

		return nil
	})

	if err != nil {
		return err
	}

	s.log.Info("standalone resources created",
		slog.String("user_id", userID),
		slog.String("org_id", orgID),
		slog.String("project_id", projectID),
	)

	return nil
}

func (s *BootstrapService) createUser(ctx context.Context, tx bun.Tx) (string, error) {
	var userID string

	query := `
		INSERT INTO core.user_profiles (zitadel_user_id, display_name, created_at, updated_at)
		VALUES ('standalone', ?, NOW(), NOW())
		ON CONFLICT (zitadel_user_id) DO UPDATE SET display_name = EXCLUDED.display_name
		RETURNING id
	`

	err := tx.NewRaw(query, s.cfg.Standalone.UserEmail).Scan(ctx, &userID)
	if err != nil {
		return "", err
	}

	s.log.Info("standalone user created", slog.String("user_id", userID))
	return userID, nil
}

func (s *BootstrapService) createOrganization(ctx context.Context, tx bun.Tx, userID string) (string, error) {
	var orgID string

	orgQuery := `
		INSERT INTO kb.orgs (name, created_at, updated_at)
		VALUES (?, NOW(), NOW())
		RETURNING id
	`

	err := tx.NewRaw(orgQuery, s.cfg.Standalone.OrgName).Scan(ctx, &orgID)
	if err != nil {
		return "", err
	}

	memberQuery := `
		INSERT INTO kb.organization_memberships (organization_id, user_id, role, created_at)
		VALUES (?, ?, ?, NOW())
	`

	_, err = tx.NewRaw(memberQuery, orgID, userID, bootstrapOrgRole).Exec(ctx)
	if err != nil {
		return "", err
	}

	s.log.Info("standalone organization created", slog.String("org_id", orgID))
	return orgID, nil
}

// bootstrapOrgRole is the canonical kb.organization_memberships role written
// for the bootstrapped standalone organization. org_admin is the authoritative
// organization membership role (see domain/orgs/repository.go); 'owner' is not
// written by any server code path. See migration 00178.
const bootstrapOrgRole = "org_admin"

// bootstrapProjectRole is the canonical kb.project_memberships role written for
// the bootstrapped standalone project. It must remain one of the canonical
// project roles — a previous value of 'owner' failed every `project_admin` role
// check (e.g. embeddings/retrigger) because 'owner' is not a valid
// kb.project_memberships role. See migration 00165.
const bootstrapProjectRole = "project_admin"

func (s *BootstrapService) createProject(ctx context.Context, tx bun.Tx, orgID, userID string) (string, error) {
	var projectID string

	projectQuery := `
		INSERT INTO kb.projects (organization_id, name, budget_usd, created_at, updated_at)
		VALUES (?, ?, 10.0, NOW(), NOW())
		RETURNING id
	`

	err := tx.NewRaw(projectQuery, orgID, s.cfg.Standalone.ProjectName).Scan(ctx, &projectID)
	if err != nil {
		return "", err
	}

	memberQuery := `
		INSERT INTO kb.project_memberships (project_id, user_id, role, created_at)
		VALUES (?, ?, ?, NOW())
	`

	_, err = tx.NewRaw(memberQuery, projectID, userID, bootstrapProjectRole).Exec(ctx)
	if err != nil {
		return "", err
	}

	s.log.Info("standalone project created", slog.String("project_id", projectID))
	return projectID, nil
}

// ensureSecondUser provisions the secondary standalone identity (invitee in e2e
// tests) idempotently, independent of the primary bootstrap. createSecondUser
// uses ON CONFLICT upserts/no-ops, so it is safe to run on every startup even
// against a long-lived database where the primary identity already exists.
func (s *BootstrapService) ensureSecondUser(ctx context.Context) error {
	return s.db.RunInTx(ctx, nil, func(ctx context.Context, tx bun.Tx) error {
		return s.createSecondUser(ctx, tx)
	})
}

// createSecondUser seeds a secondary standalone identity (invitee in e2e
// tests) with a core.user_emails row so the invite accept/decline gate can
// match the invite email to this user.
func (s *BootstrapService) createSecondUser(ctx context.Context, tx bun.Tx) error {
	if s.cfg.Standalone.UserEmail2 == "" {
		return nil
	}

	var userID string
	err := tx.NewRaw(`
		INSERT INTO core.user_profiles (zitadel_user_id, display_name, created_at, updated_at)
		VALUES ('standalone-2', ?, NOW(), NOW())
		ON CONFLICT (zitadel_user_id) DO UPDATE SET display_name = EXCLUDED.display_name
		RETURNING id
	`, s.cfg.Standalone.UserEmail2).Scan(ctx, &userID)
	if err != nil {
		return err
	}

	_, err = tx.NewRaw(`
		INSERT INTO core.user_emails (user_id, email, verified, created_at)
		VALUES (?, ?, true, NOW())
		ON CONFLICT (email) DO NOTHING
	`, userID, s.cfg.Standalone.UserEmail2).Exec(ctx)
	if err != nil {
		return err
	}

	s.log.Info("standalone second user created", slog.String("user_id", userID))
	return nil
}
