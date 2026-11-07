package skills

import (
	"context"
	"log/slog"
	"time"

	"github.com/google/uuid"

	"github.com/emergent-company/emergent.memory/pkg/logger"
)

// Curator performs the deterministic lifecycle pass for agent-created skills:
// active -> stale -> archived, based on last activity. It never deletes and
// never touches user-authored, CLI, blueprint or marketplace skills, and it
// skips pinned skills. LLM consolidation is deliberately out of scope.
type Curator struct {
	repo   *Repository
	reader ProjectSkillCaptureReader
	log    *slog.Logger
}

// NewCurator creates the curator.
func NewCurator(repo *Repository, log *slog.Logger) *Curator {
	return &Curator{repo: repo, log: log.With(logger.Scope("skills.curator"))}
}

// SetProjectConfigReader installs the project policy reader.
func (c *Curator) SetProjectConfigReader(r ProjectSkillCaptureReader) { c.reader = r }

// Run ages every agent-created skill in every capture-enabled project.
func (c *Curator) Run(ctx context.Context) error {
	if c.reader == nil {
		return nil
	}
	projects, err := c.reader.ListSkillCaptureProjects(ctx)
	if err != nil {
		return err
	}
	now := time.Now()
	totalStale, totalArchived := 0, 0
	for _, projectID := range projects {
		cfg, err := c.reader.SkillCaptureConfigForProject(ctx, projectID)
		if err != nil || cfg == nil || !cfg.Enabled() {
			continue
		}
		norm := cfg.Normalized()
		staleCutoff := now.AddDate(0, 0, -norm.StaleAfterDays)
		archiveCutoff := now.AddDate(0, 0, -norm.ArchiveAfterDays)

		skills, err := c.repo.FindAgentCreatedSkills(ctx, projectID)
		if err != nil {
			c.log.Warn("curator: failed to list agent-created skills",
				slog.String("project_id", projectID), logger.Error(err))
			continue
		}
		ids := make([]uuid.UUID, 0, len(skills))
		for _, sk := range skills {
			ids = append(ids, sk.ID)
		}
		usage, err := c.repo.UsageBySkillIDs(ctx, ids)
		if err != nil {
			c.log.Warn("curator: failed to load skill usage",
				slog.String("project_id", projectID), logger.Error(err))
			usage = nil
		}

		for _, sk := range skills {
			if sk.IsPinned() {
				continue
			}
			last := sk.UpdatedAt
			if sk.CreatedAt.After(last) {
				last = sk.CreatedAt
			}
			if u := usage[sk.ID]; u != nil && u.LastUsedAt != nil && u.LastUsedAt.After(last) {
				last = *u.LastUsedAt
			}

			target := SkillLifecycleActive
			switch {
			case last.Before(archiveCutoff):
				target = SkillLifecycleArchived
			case last.Before(staleCutoff):
				target = SkillLifecycleStale
			}
			if target == sk.LifecycleStateOrDefault() {
				continue
			}
			if err := c.repo.SetSkillLifecycleState(ctx, sk.ID, target); err != nil {
				c.log.Warn("curator: failed to transition skill",
					slog.String("skill_id", sk.ID.String()), slog.String("state", target), logger.Error(err))
				continue
			}
			switch target {
			case SkillLifecycleStale:
				totalStale++
			case SkillLifecycleArchived:
				totalArchived++
			}
		}
	}
	c.log.Info("skill curator pass complete",
		slog.Int("newly_stale", totalStale),
		slog.Int("newly_archived", totalArchived),
	)
	return nil
}
