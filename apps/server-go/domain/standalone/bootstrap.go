package standalone

import (
	"context"
	"log/slog"

	"github.com/uptrace/bun"

	"github.com/emergent/emergent-core/internal/config"
	"github.com/emergent/emergent-core/pkg/logger"
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
		return nil
	}

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
		VALUES (?, ?, 'owner', NOW())
	`

	_, err = tx.NewRaw(memberQuery, orgID, userID).Exec(ctx)
	if err != nil {
		return "", err
	}

	s.log.Info("standalone organization created", slog.String("org_id", orgID))
	return orgID, nil
}

func (s *BootstrapService) createProject(ctx context.Context, tx bun.Tx, orgID, userID string) (string, error) {
	var projectID string

	projectQuery := `
		INSERT INTO kb.projects (organization_id, name, created_at, updated_at)
		VALUES (?, ?, NOW(), NOW())
		RETURNING id
	`

	err := tx.NewRaw(projectQuery, orgID, s.cfg.Standalone.ProjectName).Scan(ctx, &projectID)
	if err != nil {
		return "", err
	}

	memberQuery := `
		INSERT INTO kb.project_memberships (project_id, user_id, role, created_at)
		VALUES (?, ?, 'owner', NOW())
	`

	_, err = tx.NewRaw(memberQuery, projectID, userID).Exec(ctx)
	if err != nil {
		return "", err
	}

	s.log.Info("standalone project created", slog.String("project_id", projectID))
	return projectID, nil
}
