package githubapp

import (
	"context"

	"github.com/google/uuid"
	"github.com/uptrace/bun"
)

// Repository stores App credentials, installations, deliveries, and claims.
type Repository struct {
	db bun.IDB
}

// NewRepository creates a repository over the shared bun handle.
func NewRepository(db bun.IDB) *Repository {
	return &Repository{db: db}
}

// --- Apps ---

func (r *Repository) CreateApp(ctx context.Context, app *GitHubApp) error {
	_, err := r.db.NewInsert().Model(app).Returning("*").Exec(ctx)
	return err
}

func (r *Repository) FindAppByID(ctx context.Context, id uuid.UUID) (*GitHubApp, error) {
	app := new(GitHubApp)
	if err := r.db.NewSelect().Model(app).Where("ga.id = ?", id).Scan(ctx); err != nil {
		return nil, err
	}
	return app, nil
}

func (r *Repository) FindAppByOrgAndAppID(ctx context.Context, orgID uuid.UUID, appID int64) (*GitHubApp, error) {
	app := new(GitHubApp)
	if err := r.db.NewSelect().Model(app).
		Where("ga.org_id = ?", orgID).
		Where("ga.app_id = ?", appID).
		Scan(ctx); err != nil {
		return nil, err
	}
	return app, nil
}

func (r *Repository) ListAppsByOrg(ctx context.Context, orgID uuid.UUID) ([]*GitHubApp, error) {
	var apps []*GitHubApp
	if err := r.db.NewSelect().Model(&apps).
		Where("ga.org_id = ?", orgID).
		Order("ga.created_at ASC").
		Scan(ctx); err != nil {
		return nil, err
	}
	return apps, nil
}

func (r *Repository) UpdateAppCredentials(ctx context.Context, app *GitHubApp) error {
	_, err := r.db.NewUpdate().Model(app).
		Column("slug", "private_key_encrypted", "webhook_secret_encrypted", "updated_at").
		Where("id = ?", app.ID).
		Exec(ctx)
	return err
}

// --- Installations ---

func (r *Repository) CreateInstallation(ctx context.Context, inst *Installation) error {
	_, err := r.db.NewInsert().Model(inst).Returning("*").Exec(ctx)
	return err
}

func (r *Repository) FindInstallationByID(ctx context.Context, id uuid.UUID) (*Installation, error) {
	inst := new(Installation)
	if err := r.db.NewSelect().Model(inst).Where("gi.id = ?", id).Scan(ctx); err != nil {
		return nil, err
	}
	return inst, nil
}

// FindEnabledInstallationByInstallationID resolves the ingress routing row.
func (r *Repository) FindEnabledInstallationByInstallationID(ctx context.Context, installationID int64) (*Installation, error) {
	inst := new(Installation)
	if err := r.db.NewSelect().Model(inst).
		Where("gi.installation_id = ?", installationID).
		Where("gi.enabled = true").
		Scan(ctx); err != nil {
		return nil, err
	}
	return inst, nil
}

func (r *Repository) ListInstallationsByProject(ctx context.Context, projectID uuid.UUID) ([]*Installation, error) {
	var insts []*Installation
	if err := r.db.NewSelect().Model(&insts).
		Where("gi.project_id = ?", projectID).
		Order("gi.created_at ASC").
		Scan(ctx); err != nil {
		return nil, err
	}
	return insts, nil
}

func (r *Repository) UpdateInstallation(ctx context.Context, inst *Installation) error {
	_, err := r.db.NewUpdate().Model(inst).
		Column("repos", "agent_id", "queue", "enabled", "updated_at").
		Where("id = ?", inst.ID).
		Exec(ctx)
	return err
}

func (r *Repository) SetInstallationEnabled(ctx context.Context, installationID int64, enabled bool) error {
	_, err := r.db.NewUpdate().
		Table("core.github_app_installations").
		Set("enabled = ?", enabled).
		Set("updated_at = now()").
		Where("installation_id = ?", installationID).
		Exec(ctx)
	return err
}

// --- Deliveries ---

// InsertDelivery records a delivery id and reports whether it was new. A
// duplicate is a no-op and returns false.
func (r *Repository) InsertDelivery(ctx context.Context, d *Delivery) (bool, error) {
	res, err := r.db.NewInsert().Model(d).
		On("CONFLICT (delivery_id) DO NOTHING").
		Exec(ctx)
	if err != nil {
		return false, err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return false, err
	}
	return n > 0, nil
}

// --- Review claims ---

// ClaimReview inserts or re-uses the claim for a head and supersedes claims for
// older heads of the same PR. It returns the claim and the run ids that were
// superseded, so the caller can cancel them.
func (r *Repository) ClaimReview(ctx context.Context, installationID int64, repo string, pullRequest int, headSHA string) (*ReviewClaim, []uuid.UUID, error) {
	claim := new(ReviewClaim)
	var superseded []uuid.UUID

	err := r.db.RunInTx(ctx, nil, func(ctx context.Context, tx bun.Tx) error {
		var stale []ReviewClaim
		if err := tx.NewSelect().Model(&stale).
			Where("installation_id = ?", installationID).
			Where("repo = ?", repo).
			Where("pull_request = ?", pullRequest).
			Where("head_sha <> ?", headSHA).
			Where("state IN (?)", bun.In([]string{ClaimPending, ClaimRunning})).
			Scan(ctx); err != nil {
			return err
		}
		for _, s := range stale {
			if s.RunID != nil {
				superseded = append(superseded, *s.RunID)
			}
		}
		if len(stale) > 0 {
			if _, err := tx.NewUpdate().
				Table("core.github_pr_review_claims").
				Set("state = ?", ClaimSuperseded).
				Set("updated_at = now()").
				Where("installation_id = ?", installationID).
				Where("repo = ?", repo).
				Where("pull_request = ?", pullRequest).
				Where("head_sha <> ?", headSHA).
				Where("state IN (?)", bun.In([]string{ClaimPending, ClaimRunning})).
				Exec(ctx); err != nil {
				return err
			}
		}

		if _, err := tx.NewInsert().Model(&ReviewClaim{
			InstallationID: installationID,
			Repo:           repo,
			PullRequest:    pullRequest,
			HeadSHA:        headSHA,
			State:          ClaimPending,
		}).On("CONFLICT (installation_id, repo, pull_request, head_sha) DO NOTHING").Exec(ctx); err != nil {
			return err
		}

		return tx.NewSelect().Model(claim).
			Where("installation_id = ?", installationID).
			Where("repo = ?", repo).
			Where("pull_request = ?", pullRequest).
			Where("head_sha = ?", headSHA).
			Scan(ctx)
	})
	if err != nil {
		return nil, nil, err
	}
	return claim, superseded, nil
}

func (r *Repository) SetClaimRun(ctx context.Context, claimID, runID uuid.UUID, state string) error {
	_, err := r.db.NewUpdate().
		Table("core.github_pr_review_claims").
		Set("run_id = ?", runID).
		Set("state = ?", state).
		Set("updated_at = now()").
		Where("id = ?", claimID).
		Exec(ctx)
	return err
}

func (r *Repository) SetClaimState(ctx context.Context, claimID uuid.UUID, state string) error {
	_, err := r.db.NewUpdate().
		Table("core.github_pr_review_claims").
		Set("state = ?", state).
		Set("updated_at = now()").
		Where("id = ?", claimID).
		Exec(ctx)
	return err
}
