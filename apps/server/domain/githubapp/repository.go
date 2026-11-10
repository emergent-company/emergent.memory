package githubapp

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/uptrace/bun"
)

// ErrStaleHead is returned when the delivered head is not the PR's current
// head. The caller must re-read the head from GitHub before claiming.
var ErrStaleHead = errors.New("githubapp: delivered head is not the current PR head")

// ErrClaimNotActive is returned when a claim transition loses to a terminal
// state (cancel/supersede wins).
var ErrClaimNotActive = errors.New("githubapp: claim is no longer active")

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

// FindAnyAppByOrg returns the org's first App row. Installations are bound to a
// project, not to a specific App id in the admin API.
func (r *Repository) FindAnyAppByOrg(ctx context.Context, orgID uuid.UUID) (*GitHubApp, error) {
	app := new(GitHubApp)
	if err := r.db.NewSelect().Model(app).
		Where("ga.org_id = ?", orgID).
		Order("ga.created_at ASC").
		Limit(1).
		Scan(ctx); err != nil {
		return nil, err
	}
	return app, nil
}

func (r *Repository) CountApps(ctx context.Context) (int, error) {
	return r.db.NewSelect().Table("core.github_apps").Count(ctx)
}

func (r *Repository) UpdateAppCredentials(ctx context.Context, app *GitHubApp) error {
	_, err := r.db.NewUpdate().Model(app).
		Column("slug", "private_key_encrypted", "webhook_secret_encrypted").
		Set("updated_at = now()").
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

func (r *Repository) ListInstallationsByOrg(ctx context.Context, orgID uuid.UUID) ([]*Installation, error) {
	var insts []*Installation
	if err := r.db.NewSelect().Model(&insts).
		Where("gi.org_id = ?", orgID).
		Order("gi.created_at ASC").
		Scan(ctx); err != nil {
		return nil, err
	}
	return insts, nil
}

func (r *Repository) UpdateInstallation(ctx context.Context, inst *Installation) error {
	_, err := r.db.NewUpdate().Model(inst).
		Column("repos", "agent_id", "queue", "enabled").
		Set("updated_at = now()").
		Where("id = ?", inst.ID).
		Exec(ctx)
	return err
}

// SetInstallationEnabled flips a single installation row by primary key.
func (r *Repository) SetInstallationEnabled(ctx context.Context, id uuid.UUID, enabled bool) error {
	_, err := r.db.NewUpdate().
		Table("core.github_app_installations").
		Set("enabled = ?", enabled).
		Set("updated_at = now()").
		Where("id = ?", id).
		Exec(ctx)
	return err
}

// DisableInstallationByInstallationID disables the enabled row for a GitHub
// installation (the installation.deleted webhook path).
func (r *Repository) DisableInstallationByInstallationID(ctx context.Context, installationID int64) error {
	_, err := r.db.NewUpdate().
		Table("core.github_app_installations").
		Set("enabled = false").
		Set("updated_at = now()").
		Where("installation_id = ?", installationID).
		Where("enabled = true").
		Exec(ctx)
	return err
}

// UpdateInstallationRepos rewrites the allowlist for an installation.
func (r *Repository) UpdateInstallationRepos(ctx context.Context, installationID int64, repos []string) error {
	_, err := r.db.NewUpdate().
		Table("core.github_app_installations").
		Set("repos = ?", repos).
		Set("updated_at = now()").
		Where("installation_id = ?", installationID).
		Where("enabled = true").
		Exec(ctx)
	return err
}

// DeleteInstallation removes an installation row.
func (r *Repository) DeleteInstallation(ctx context.Context, id uuid.UUID) error {
	_, err := r.db.NewDelete().Table("core.github_app_installations").Where("id = ?", id).Exec(ctx)
	return err
}

// DeleteApp removes an App and, by cascade, its installations.
func (r *Repository) DeleteApp(ctx context.Context, id uuid.UUID) error {
	_, err := r.db.NewDelete().Table("core.github_apps").Where("id = ?", id).Exec(ctx)
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

// ClaimReview records a review claim for the current head.
//
// currentHead is the PR head read from GitHub immediately before this call;
// headSHA is the head named by the delivery. An out-of-order delivery for an
// older head must not cancel the review of the current head, so a mismatch
// returns ErrStaleHead without touching claims.
//
// The transaction is serialized per (installation, repo, PR) so concurrent
// deliveries cannot leave two live claims. Claims for other heads are marked
// superseded and their run ids are returned for cancellation.
func (r *Repository) ClaimReview(ctx context.Context, installationID int64, repo string, pullRequest int, currentHead, headSHA string) (*ReviewClaim, []uuid.UUID, error) {
	if currentHead == "" || headSHA == "" {
		return nil, nil, errors.New("githubapp: current and delivered head are required")
	}
	if currentHead != headSHA {
		return nil, nil, ErrStaleHead
	}

	claim := new(ReviewClaim)
	var superseded []uuid.UUID
	lockKey := fmt.Sprintf("githubapp:%d:%s:%d", installationID, repo, pullRequest)

	err := r.db.RunInTx(ctx, nil, func(ctx context.Context, tx bun.Tx) error {
		if _, err := tx.NewRaw("SELECT pg_advisory_xact_lock(hashtextextended(?, 0))", lockKey).Exec(ctx); err != nil {
			return err
		}

		var stale []ReviewClaim
		if err := tx.NewSelect().Model(&stale).
			Where("installation_id = ?", installationID).
			Where("repo = ?", repo).
			Where("pull_request = ?", pullRequest).
			Where("head_sha <> ?", currentHead).
			Where("state IN (?)", bun.In([]string{ClaimPending, ClaimRunning})).
			For("UPDATE").
			Scan(ctx); err != nil {
			return err
		}
		if len(stale) > 0 {
			ids := make([]uuid.UUID, 0, len(stale))
			for _, s := range stale {
				ids = append(ids, s.ID)
				if s.RunID != nil {
					superseded = append(superseded, *s.RunID)
				}
			}
			if _, err := tx.NewUpdate().
				Table("core.github_pr_review_claims").
				Set("state = ?", ClaimSuperseded).
				Set("updated_at = now()").
				Where("id IN (?)", bun.In(ids)).
				Exec(ctx); err != nil {
				return err
			}
		}

		// Insert-or-reuse. A terminal row is reactivated so a head that moves
		// H1 -> H2 -> H1 (force-push/revert) is reviewed again.
		if _, err := tx.NewRaw(`INSERT INTO core.github_pr_review_claims AS c
			(installation_id, repo, pull_request, head_sha, state)
			VALUES (?, ?, ?, ?, 'pending')
			ON CONFLICT (installation_id, repo, pull_request, head_sha) DO UPDATE
			SET state = 'pending', run_id = NULL, updated_at = now()
			WHERE c.state IN ('superseded', 'cancelled', 'done')`,
			installationID, repo, pullRequest, currentHead).Exec(ctx); err != nil {
			return err
		}

		return tx.NewSelect().Model(claim).
			Where("installation_id = ?", installationID).
			Where("repo = ?", repo).
			Where("pull_request = ?", pullRequest).
			Where("head_sha = ?", currentHead).
			Scan(ctx)
	})
	if err != nil {
		return nil, nil, err
	}
	return claim, superseded, nil
}

// SupersedeActivePR cancels every pending/running claim for a PR (closed or
// converted to draft) and returns their run ids for cancellation.
func (r *Repository) SupersedeActivePR(ctx context.Context, installationID int64, repo string, pullRequest int) ([]uuid.UUID, error) {
	var superseded []uuid.UUID
	lockKey := prLockKey(installationID, repo, pullRequest)

	err := r.db.RunInTx(ctx, nil, func(ctx context.Context, tx bun.Tx) error {
		if _, err := tx.NewRaw("SELECT pg_advisory_xact_lock(hashtextextended(?, 0))", lockKey).Exec(ctx); err != nil {
			return err
		}
		var active []ReviewClaim
		if err := tx.NewSelect().Model(&active).
			Where("installation_id = ?", installationID).
			Where("repo = ?", repo).
			Where("pull_request = ?", pullRequest).
			Where("state IN (?)", bun.In([]string{ClaimPending, ClaimRunning})).
			For("UPDATE").
			Scan(ctx); err != nil {
			return err
		}
		if len(active) == 0 {
			return nil
		}
		ids := make([]uuid.UUID, 0, len(active))
		for _, c := range active {
			ids = append(ids, c.ID)
			if c.RunID != nil {
				superseded = append(superseded, *c.RunID)
			}
		}
		_, err := tx.NewUpdate().
			Table("core.github_pr_review_claims").
			Set("state = ?", ClaimCancelled).
			Set("updated_at = now()").
			Where("id IN (?)", bun.In(ids)).
			Exec(ctx)
		return err
	})
	if err != nil {
		return nil, err
	}
	return superseded, nil
}

func prLockKey(installationID int64, repo string, pullRequest int) string {
	return fmt.Sprintf("githubapp:%d:%s:%d", installationID, repo, pullRequest)
}

// SetClaimRun attaches a run to a pending/running claim. A terminal claim is
// left untouched; false reports the cancel/supersede-wins outcome.
func (r *Repository) SetClaimRun(ctx context.Context, claimID, runID uuid.UUID, state string) (bool, error) {
	res, err := r.db.NewUpdate().
		Table("core.github_pr_review_claims").
		Set("run_id = ?", runID).
		Set("state = ?", state).
		Set("updated_at = now()").
		Where("id = ?", claimID).
		Where("state IN (?)", bun.In([]string{ClaimPending, ClaimRunning})).
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

// SetClaimState moves a pending/running claim to a terminal state. A terminal
// claim is left untouched; false reports the earlier transition won.
func (r *Repository) SetClaimState(ctx context.Context, claimID uuid.UUID, state string) (bool, error) {
	res, err := r.db.NewUpdate().
		Table("core.github_pr_review_claims").
		Set("state = ?", state).
		Set("updated_at = now()").
		Where("id = ?", claimID).
		Where("state IN (?)", bun.In([]string{ClaimPending, ClaimRunning})).
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
