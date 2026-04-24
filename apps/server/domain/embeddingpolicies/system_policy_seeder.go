package embeddingpolicies

import (
	"context"
	"log/slog"
	"time"

	"github.com/google/uuid"
	"github.com/uptrace/bun"
	"go.uber.org/fx"

	"github.com/emergent-company/emergent.memory/pkg/logger"
)

// SystemPolicies defines the built-in embedding policies applied to all projects.
// These are seeded at startup and cannot be deleted by users.
var SystemPolicies = []struct {
	ObjectType    string
	RelevantPaths []string
}{
	{
		ObjectType:    "Message",
		RelevantPaths: []string{"content"},
	},
}

// SystemPolicySeeder seeds system-level embedding policies for all projects.
type SystemPolicySeeder struct {
	store *Store
	db    bun.IDB
	log   *slog.Logger
}

// NewSystemPolicySeeder creates a new SystemPolicySeeder.
func NewSystemPolicySeeder(store *Store, db bun.IDB, log *slog.Logger) *SystemPolicySeeder {
	return &SystemPolicySeeder{
		store: store,
		db:    db,
		log:   log.With(logger.Scope("embeddingpolicies.system_seeder")),
	}
}

// RegisterSystemPolicySeederLifecycle registers the seeder with fx lifecycle.
func RegisterSystemPolicySeederLifecycle(lc fx.Lifecycle, seeder *SystemPolicySeeder) {
	lc.Append(fx.Hook{
		OnStart: func(ctx context.Context) error {
			return seeder.SeedAllProjects(ctx)
		},
	})
}

// SeedAllProjects ensures system policies exist for every project.
// This is idempotent — safe to run on every server boot.
func (s *SystemPolicySeeder) SeedAllProjects(ctx context.Context) error {
	// Get all project IDs
	type projectRow struct {
		ID string `bun:"id"`
	}
	var rows []projectRow
	err := s.db.NewSelect().
		TableExpr("kb.projects").
		Column("id").
		Scan(ctx, &rows)
	if err != nil {
		// Non-fatal: log and continue. On fresh DBs the projects table may be empty.
		s.log.Warn("failed to list projects for system policy seeding", logger.Error(err))
		return nil
	}

	for _, row := range rows {
		if seedErr := s.SeedForProject(ctx, row.ID); seedErr != nil {
			s.log.Warn("failed to seed system policies for project",
				slog.String("projectId", row.ID),
				logger.Error(seedErr))
		}
	}

	s.log.Info("system embedding policies seeded", slog.Int("projects", len(rows)))
	return nil
}

// SeedForProject ensures system embedding policies exist for a single project.
// It is idempotent — safe to call multiple times.
func (s *SystemPolicySeeder) SeedForProject(ctx context.Context, projectID string) error {
	for _, sp := range SystemPolicies {
		if err := s.upsertSystemPolicy(ctx, projectID, sp.ObjectType, sp.RelevantPaths); err != nil {
			return err
		}
	}
	return nil
}

func (s *SystemPolicySeeder) upsertSystemPolicy(ctx context.Context, projectID, objectType string, relevantPaths []string) error {
	// Check if system policy already exists for this project+type.
	type idRow struct {
		ID string `bun:"id"`
	}
	var existing idRow
	err := s.db.NewSelect().
		TableExpr("kb.embedding_policies").
		Column("id").
		Where("project_id = ?", projectID).
		Where("object_type = ?", objectType).
		Where("is_system = true").
		Limit(1).
		Scan(ctx, &existing)

	if err != nil || existing.ID == "" {
		// Insert new system policy.
		now := time.Now().UTC()
		policy := &EmbeddingPolicy{
			ID:               uuid.New().String(),
			ProjectID:        projectID,
			ObjectType:       objectType,
			Enabled:          true,
			RelevantPaths:    relevantPaths,
			RequiredLabels:   []string{},
			ExcludedLabels:   []string{},
			ExcludedStatuses: []string{},
			IsSystem:         true,
			CreatedAt:        now,
			UpdatedAt:        now,
		}
		if createErr := s.store.Create(ctx, policy); createErr != nil {
			return createErr
		}
	}
	return nil
}
