package scheduler

import (
	"context"
	"log/slog"

	"github.com/uptrace/bun"

	"github.com/emergent-company/emergent.memory/pkg/auth"
	"github.com/emergent-company/emergent.memory/pkg/logger"
)

// ZitadelProfileSyncTask syncs user profiles (email, name) from Zitadel
// for all users that have a zitadel_user_id set.
type ZitadelProfileSyncTask struct {
	db      *bun.DB
	userSvc *auth.UserProfileService
	log     *slog.Logger
}

// NewZitadelProfileSyncTask creates a new ZitadelProfileSyncTask.
func NewZitadelProfileSyncTask(db *bun.DB, userSvc *auth.UserProfileService, log *slog.Logger) *ZitadelProfileSyncTask {
	return &ZitadelProfileSyncTask{
		db:      db,
		userSvc: userSvc,
		log:     log.With(logger.Scope("scheduler.zitadel_profile_sync")),
	}
}

// Run fetches all users with a zitadel_user_id and syncs their profiles.
func (t *ZitadelProfileSyncTask) Run(ctx context.Context) error {
	var users []struct {
		ID            string `bun:"id"`
		ZitadelUserID string `bun:"zitadel_user_id"`
	}

	err := t.db.NewSelect().
		TableExpr("core.user_profiles").
		Column("id", "zitadel_user_id").
		Where("zitadel_user_id != ''").
		Where("deleted_at IS NULL").
		Scan(ctx, &users)
	if err != nil {
		return err
	}

	t.log.Info("starting zitadel profile sync", slog.Int("users", len(users)))

	synced, skipped, failed := 0, 0, 0
	for _, u := range users {
		if err := t.userSvc.SyncFromZitadel(ctx, u.ID, u.ZitadelUserID); err != nil {
			t.log.Warn("failed to sync user profile from zitadel",
				slog.String("user_id", u.ID),
				slog.String("zitadel_user_id", u.ZitadelUserID),
				logger.Error(err),
			)
			failed++
			continue
		}
		// SyncFromZitadel is a no-op when management client is nil
		synced++
	}

	t.log.Info("zitadel profile sync complete",
		slog.Int("synced", synced),
		slog.Int("skipped", skipped),
		slog.Int("failed", failed),
	)
	return nil
}
