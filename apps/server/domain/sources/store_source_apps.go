package sources

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/uptrace/bun"

	"github.com/emergent-company/emergent.memory/pkg/apperror"
	"github.com/emergent-company/emergent.memory/pkg/pgutils"
)

// ErrSourceEventStaleHead is returned when the delivered head is not the
// subject's current head (out-of-order or replayed delivery).
var ErrSourceEventStaleHead = errors.New("sources: delivered head is not the current subject head")

// projectTx runs fn inside a transaction whose app.current_project_id is
// projectID, written with set_config(..., is_local = true).
//
// The 00291 policies on kb.source_triggers, kb.source_events and
// kb.source_event_claims are fail-closed: they match nothing (and refuse
// unset-context writes) unless the transaction carries the project. The
// session-less webhook ingress resolves the project from kb.sources (still on
// its baseline fail-open policy) and then opens this transaction for every
// read/write of the three child tables. is_local = true makes the setting die
// with the transaction, so it can never leak across a pooled connection into
// another project's request.
func (r *Repository) projectTx(ctx context.Context, projectID uuid.UUID, fn func(ctx context.Context, tx bun.Tx) error) error {
	if projectID == uuid.Nil {
		return errors.New("sources: project context is required for source trigger/event rows")
	}
	return r.db.RunInTx(ctx, nil, func(ctx context.Context, tx bun.Tx) error {
		if _, err := tx.NewRaw("SELECT set_config('app.current_project_id', ?, true)", projectID.String()).Exec(ctx); err != nil {
			return fmt.Errorf("sources: set source event project context: %w", err)
		}
		return fn(ctx, tx)
	})
}

// --- Source Apps ---

func (r *Repository) CreateSourceApp(ctx context.Context, app *SourceApp) error {
	if _, err := r.db.NewInsert().Model(app).Returning("*").Exec(ctx); err != nil {
		return apperror.NewDatabase("failed to create source app", err)
	}
	return nil
}

func (r *Repository) FindSourceAppByID(ctx context.Context, id uuid.UUID) (*SourceApp, error) {
	app := new(SourceApp)
	if err := r.db.NewSelect().Model(app).Where("sa.id = ?", id).Scan(ctx); err != nil {
		return nil, err
	}
	return app, nil
}

func (r *Repository) FindSourceAppByWebhookKey(ctx context.Context, key uuid.UUID) (*SourceApp, error) {
	app := new(SourceApp)
	if err := r.db.NewSelect().Model(app).Where("sa.webhook_key = ?", key).Scan(ctx); err != nil {
		return nil, err
	}
	return app, nil
}

func (r *Repository) FindSourceAppByOrgAppID(ctx context.Context, orgID uuid.UUID, provider string, appID int64) (*SourceApp, error) {
	app := new(SourceApp)
	if err := r.db.NewSelect().Model(app).
		Where("sa.org_id = ?", orgID).
		Where("sa.provider = ?", provider).
		Where("sa.app_id = ?", appID).
		Scan(ctx); err != nil {
		return nil, err
	}
	return app, nil
}

func (r *Repository) ListSourceAppsByOrg(ctx context.Context, orgID uuid.UUID) ([]*SourceApp, error) {
	var apps []*SourceApp
	if err := r.db.NewSelect().Model(&apps).
		Where("sa.org_id = ?", orgID).
		Order("sa.created_at ASC").
		Scan(ctx); err != nil {
		return nil, err
	}
	return apps, nil
}

// DeleteSourceApp removes an org-scoped App credential. It refuses while
// project sources are still bound to the App, naming them in the error: the
// App is org-scoped while the sources are project-scoped, so a delete must not
// silently destroy project data (issue #565).
func (r *Repository) DeleteSourceApp(ctx context.Context, id uuid.UUID) error {
	bound, err := r.listSourcesBoundToApp(ctx, id)
	if err != nil {
		return err
	}
	if len(bound) > 0 {
		return sourceAppInUseError(bound)
	}
	if _, err := r.db.NewDelete().Model((*SourceApp)(nil)).Where("id = ?", id).Exec(ctx); err != nil {
		if pgutils.IsForeignKeyViolation(err) {
			// Lost the race against a concurrent source bind: report the same
			// nameable refusal the precheck would have produced.
			bound, _ = r.listSourcesBoundToApp(ctx, id)
			return sourceAppInUseError(bound)
		}
		return apperror.NewDatabase("failed to delete source app", err)
	}
	return nil
}

func (r *Repository) listSourcesBoundToApp(ctx context.Context, appID uuid.UUID) ([]OAuthAppSourceRef, error) {
	refs := []OAuthAppSourceRef{}
	if err := r.db.NewSelect().
		TableExpr("kb.sources").
		ColumnExpr("id").
		ColumnExpr("coalesce(display_name, '') AS display_name").
		Where("app_id = ?", appID).
		OrderExpr("created_at, id").
		Scan(ctx, &refs); err != nil {
		return nil, apperror.NewDatabase("failed to list sources bound to source app", err)
	}
	return refs, nil
}

// sourceAppInUseError mirrors oauthAppInUseError for kb.source_apps: it names
// the project sources still bound to the org App so the operator can detach
// them instead of chasing a bare count. The kb.sources.app_id foreign key is
// ON DELETE RESTRICT as the database-level backstop (migration 00291).
func sourceAppInUseError(refs []OAuthAppSourceRef) error {
	if len(refs) == 0 {
		return apperror.NewConflict("source app is still referenced by a project source; detach it first")
	}
	return apperror.NewConflict(fmt.Sprintf(
		"source app is bound to %d project source(s): %s; detach them before deleting",
		len(refs), namedOAuthAppSources(refs)))
}

// --- Triggers ---

func (r *Repository) CreateSourceTrigger(ctx context.Context, trigger *SourceTrigger) error {
	if trigger.EventFilter == nil {
		trigger.EventFilter = map[string]any{}
	}
	err := r.projectTx(ctx, trigger.ProjectID, func(ctx context.Context, tx bun.Tx) error {
		_, err := tx.NewInsert().Model(trigger).Returning("*").Exec(ctx)
		return err
	})
	if err != nil {
		return apperror.NewDatabase("failed to create source trigger", err)
	}
	return nil
}

func (r *Repository) ListEnabledTriggersForSource(ctx context.Context, projectID, sourceID uuid.UUID) ([]*SourceTrigger, error) {
	var triggers []*SourceTrigger
	err := r.projectTx(ctx, projectID, func(ctx context.Context, tx bun.Tx) error {
		return tx.NewSelect().Model(&triggers).
			Where("st.source_id = ?", sourceID).
			Where("st.enabled = true").
			Order("st.created_at ASC").
			Scan(ctx)
	})
	if err != nil {
		return nil, err
	}
	return triggers, nil
}

func (r *Repository) ListSourceTriggers(ctx context.Context, projectID, sourceID uuid.UUID) ([]*SourceTrigger, error) {
	var triggers []*SourceTrigger
	err := r.projectTx(ctx, projectID, func(ctx context.Context, tx bun.Tx) error {
		return tx.NewSelect().Model(&triggers).
			Where("st.source_id = ?", sourceID).
			Order("st.event_type ASC").
			Scan(ctx)
	})
	if err != nil {
		return nil, err
	}
	return triggers, nil
}

func (r *Repository) UpdateSourceTrigger(ctx context.Context, trigger *SourceTrigger) error {
	if trigger.EventFilter == nil {
		trigger.EventFilter = map[string]any{}
	}
	err := r.projectTx(ctx, trigger.ProjectID, func(ctx context.Context, tx bun.Tx) error {
		_, err := tx.NewUpdate().Model(trigger).
			Column("event_type", "agent_id", "queue", "event_filter", "enabled").
			Set("updated_at = now()").
			Where("id = ?", trigger.ID).
			Exec(ctx)
		return err
	})
	if err != nil {
		return apperror.NewDatabase("failed to update source trigger", err)
	}
	return nil
}

func (r *Repository) DeleteSourceTrigger(ctx context.Context, projectID, id uuid.UUID) error {
	err := r.projectTx(ctx, projectID, func(ctx context.Context, tx bun.Tx) error {
		_, err := tx.NewDelete().Model((*SourceTrigger)(nil)).Where("id = ?", id).Exec(ctx)
		return err
	})
	if err != nil {
		return apperror.NewDatabase("failed to delete source trigger", err)
	}
	return nil
}

// --- Events and claims ---

// InsertSourceEvent records a delivery and reports whether it was new. The
// claim (per subject/head) is separate.
func (r *Repository) InsertSourceEvent(ctx context.Context, ev *SourceEvent) (bool, error) {
	if ev.Payload == nil {
		ev.Payload = map[string]any{}
	}
	if ev.State == "" {
		ev.State = SourceEventReceived
	}
	var inserted bool
	err := r.projectTx(ctx, ev.ProjectID, func(ctx context.Context, tx bun.Tx) error {
		res, err := tx.NewInsert().Model(ev).
			Returning("*").
			On("CONFLICT (source_id, delivery_id) DO NOTHING").
			Exec(ctx)
		if err != nil {
			return err
		}
		n, err := res.RowsAffected()
		if err != nil {
			return err
		}
		inserted = n > 0
		return nil
	})
	if err != nil {
		return false, err
	}
	return inserted, nil
}

func (r *Repository) FindSourceEventByID(ctx context.Context, projectID, id uuid.UUID) (*SourceEvent, error) {
	ev := new(SourceEvent)
	err := r.projectTx(ctx, projectID, func(ctx context.Context, tx bun.Tx) error {
		return tx.NewSelect().Model(ev).Where("se.id = ?", id).Scan(ctx)
	})
	if err != nil {
		return nil, err
	}
	return ev, nil
}

// ClaimSourceSubject records one active claim per subject/head and supersedes
// claims for older heads, returning their run ids for cancellation.
func (r *Repository) ClaimSourceSubject(ctx context.Context, projectID, sourceID uuid.UUID, subjectRef, currentHead, headSHA string) (*SourceEventClaim, []uuid.UUID, error) {
	if subjectRef == "" || headSHA == "" {
		return nil, nil, fmt.Errorf("sources: subject and head are required")
	}
	if currentHead == "" || currentHead != headSHA {
		return nil, nil, ErrSourceEventStaleHead
	}
	claim := new(SourceEventClaim)
	var superseded []uuid.UUID
	lockKey := fmt.Sprintf("source-claim:%s:%s", sourceID, subjectRef)

	err := r.projectTx(ctx, projectID, func(ctx context.Context, tx bun.Tx) error {
		if _, err := tx.NewRaw("SELECT pg_advisory_xact_lock(hashtextextended(?, 0))", lockKey).Exec(ctx); err != nil {
			return err
		}

		var stale []SourceEventClaim
		if err := tx.NewSelect().Model(&stale).
			Where("source_id = ?", sourceID).
			Where("subject_ref = ?", subjectRef).
			Where("head_sha <> ?", headSHA).
			Where("state IN (?)", bun.In([]string{SourceClaimPending, SourceClaimRunning})).
			For("UPDATE").
			Scan(ctx); err != nil {
			return err
		}
		if len(stale) > 0 {
			ids := make([]uuid.UUID, 0, len(stale))
			for _, c := range stale {
				ids = append(ids, c.ID)
				if c.RunID != nil {
					superseded = append(superseded, *c.RunID)
				}
			}
			if _, err := tx.NewUpdate().
				Table("kb.source_event_claims").
				Set("state = ?", SourceClaimSuperseded).
				Set("updated_at = now()").
				Where("id IN (?)", bun.In(ids)).
				Exec(ctx); err != nil {
				return err
			}
		}

		if _, err := tx.NewRaw(`INSERT INTO kb.source_event_claims AS c
			(project_id, source_id, subject_ref, head_sha, state)
			VALUES (?, ?, ?, ?, 'pending')
			ON CONFLICT (source_id, subject_ref, head_sha) DO UPDATE
			SET state = 'pending', run_id = NULL, updated_at = now()
			WHERE c.state IN ('superseded', 'cancelled', 'done')`,
			projectID, sourceID, subjectRef, headSHA).Exec(ctx); err != nil {
			return err
		}

		return tx.NewSelect().Model(claim).
			Where("source_id = ?", sourceID).
			Where("subject_ref = ?", subjectRef).
			Where("head_sha = ?", headSHA).
			Scan(ctx)
	})
	if err != nil {
		return nil, nil, err
	}
	return claim, superseded, nil
}

// SetSourceEventClaimRun binds a run to a pending claim (one binder wins).
func (r *Repository) SetSourceEventClaimRun(ctx context.Context, projectID, claimID, runID uuid.UUID, state string) (bool, error) {
	var updated bool
	err := r.projectTx(ctx, projectID, func(ctx context.Context, tx bun.Tx) error {
		res, err := tx.NewUpdate().
			Table("kb.source_event_claims").
			Set("run_id = ?", runID).
			Set("state = ?", state).
			Set("updated_at = now()").
			Where("id = ?", claimID).
			Where("run_id IS NULL").
			Where("state = ?", SourceClaimPending).
			Exec(ctx)
		if err != nil {
			return err
		}
		n, err := res.RowsAffected()
		if err != nil {
			return err
		}
		updated = n > 0
		return nil
	})
	if err != nil {
		return false, err
	}
	return updated, nil
}

func (r *Repository) SetSourceEventClaimState(ctx context.Context, projectID, claimID uuid.UUID, state string) (bool, error) {
	var updated bool
	err := r.projectTx(ctx, projectID, func(ctx context.Context, tx bun.Tx) error {
		res, err := tx.NewUpdate().
			Table("kb.source_event_claims").
			Set("state = ?", state).
			Set("updated_at = now()").
			Where("id = ?", claimID).
			Where("state IN (?)", bun.In([]string{SourceClaimPending, SourceClaimRunning})).
			Exec(ctx)
		if err != nil {
			return err
		}
		n, err := res.RowsAffected()
		if err != nil {
			return err
		}
		updated = n > 0
		return nil
	})
	if err != nil {
		return false, err
	}
	return updated, nil
}

// CancelSourceSubjectClaims cancels active claims for a subject (closed/draft)
// and returns their run ids.
func (r *Repository) CancelSourceSubjectClaims(ctx context.Context, projectID, sourceID uuid.UUID, subjectRef string) ([]uuid.UUID, error) {
	var cancelled []uuid.UUID
	lockKey := fmt.Sprintf("source-claim:%s:%s", sourceID, subjectRef)
	err := r.projectTx(ctx, projectID, func(ctx context.Context, tx bun.Tx) error {
		if _, err := tx.NewRaw("SELECT pg_advisory_xact_lock(hashtextextended(?, 0))", lockKey).Exec(ctx); err != nil {
			return err
		}
		var active []SourceEventClaim
		if err := tx.NewSelect().Model(&active).
			Where("source_id = ?", sourceID).
			Where("subject_ref = ?", subjectRef).
			Where("state IN (?)", bun.In([]string{SourceClaimPending, SourceClaimRunning})).
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
				cancelled = append(cancelled, *c.RunID)
			}
		}
		_, err := tx.NewUpdate().
			Table("kb.source_event_claims").
			Set("state = ?", SourceClaimCancelled).
			Set("updated_at = now()").
			Where("id IN (?)", bun.In(ids)).
			Exec(ctx)
		return err
	})
	if err != nil {
		return nil, err
	}
	return cancelled, nil
}

// FindSourceEventClaimByRunID resolves the claim bound to a run.
func (r *Repository) FindSourceEventClaimByRunID(ctx context.Context, projectID, runID uuid.UUID) (*SourceEventClaim, error) {
	claim := new(SourceEventClaim)
	err := r.projectTx(ctx, projectID, func(ctx context.Context, tx bun.Tx) error {
		return tx.NewSelect().Model(claim).Where("sc.run_id = ?", runID).Limit(1).Scan(ctx)
	})
	if err != nil {
		return nil, err
	}
	return claim, nil
}

// --- Trigger-backed source lookups ---

func (r *Repository) FindSourceByID(ctx context.Context, id uuid.UUID) (*Source, error) {
	src := new(Source)
	if err := r.db.NewSelect().Model(src).Where("s.id = ?", id).Scan(ctx); err != nil {
		return nil, err
	}
	return src, nil
}

// FindSourceByAppAndInstallation resolves the enabled github_app source for an
// App installation (the webhook routing row).
func (r *Repository) FindSourceByAppAndInstallation(ctx context.Context, appID uuid.UUID, installationID string) (*Source, error) {
	src := new(Source)
	if err := r.db.NewSelect().Model(src).
		Where("s.app_id = ?", appID).
		Where("s.external_account_id = ?", installationID).
		Where("s.provider = ?", ProviderTypeGitHubApp).
		Where("s.status <> ?", SourceStatusDisabled).
		Order("s.created_at ASC").
		Limit(1).
		Scan(ctx); err != nil {
		return nil, err
	}
	return src, nil
}

// FindGitHubAppSourcesForRepo returns every github_app source in the project
// whose repo allowlist covers repo (case-insensitively, through the same
// repoAllowed helper the dispatcher uses). It returns the candidates rather
// than one row so the caller can distinguish "exactly one" from "ambiguous":
// two Apps may serve the same repo, and a tool must fail closed instead of
// taking the first. Ordered oldest first like the dispatcher's resolution.
func (r *Repository) FindGitHubAppSourcesForRepo(ctx context.Context, projectID uuid.UUID, repo string) ([]Source, error) {
	var rows []Source
	if err := r.db.NewSelect().Model(&rows).
		Where("s.project_id = ?", projectID).
		Where("s.provider = ?", ProviderTypeGitHubApp).
		Where("s.status <> ?", SourceStatusDisabled).
		Where("s.app_id IS NOT NULL").
		Where("s.external_account_id IS NOT NULL").
		Order("s.created_at ASC").
		Scan(ctx); err != nil {
		return nil, apperror.NewDatabase("failed to list github_app sources for repo", err)
	}
	matches := make([]Source, 0, len(rows))
	for _, src := range rows {
		if repoAllowed(src.Config, repo) {
			matches = append(matches, src)
		}
	}
	return matches, nil
}

// UpdateSourceRepos rewrites the github_app repo allowlist in the source config.
func (r *Repository) UpdateSourceRepos(ctx context.Context, sourceID uuid.UUID, repos []string) error {
	src, err := r.FindSourceByID(ctx, sourceID)
	if err != nil {
		return err
	}
	if src.Config == nil {
		src.Config = map[string]any{}
	}
	src.Config["repos"] = repos
	if _, err := r.db.NewUpdate().Model(src).
		Column("config").
		Set("updated_at = now()").
		Where("id = ?", sourceID).
		Exec(ctx); err != nil {
		return apperror.NewDatabase("failed to update source repos", err)
	}
	return nil
}

// DisableSourcesByInstallation disables every source bound to a GitHub
// installation (the installation.deleted webhook path).
func (r *Repository) DisableSourcesByInstallation(ctx context.Context, appID uuid.UUID, installationID string) error {
	if _, err := r.db.NewUpdate().
		Table("kb.sources").
		Set("status = ?", SourceStatusDisabled).
		Set("updated_at = now()").
		Where("app_id = ?", appID).
		Where("external_account_id = ?", installationID).
		Exec(ctx); err != nil {
		return apperror.NewDatabase("failed to disable sources for installation", err)
	}
	return nil
}

// SetSourceEventState records the dispatch outcome on a delivery row.
func (r *Repository) SetSourceEventState(ctx context.Context, projectID, id uuid.UUID, state string, runID *uuid.UUID, errMsg *string) error {
	err := r.projectTx(ctx, projectID, func(ctx context.Context, tx bun.Tx) error {
		_, err := tx.NewUpdate().
			Table("kb.source_events").
			Set("state = ?", state).
			Set("run_id = ?", runID).
			Set("error = ?", errMsg).
			Set("updated_at = now()").
			Where("id = ?", id).
			Exec(ctx)
		return err
	})
	if err != nil {
		return apperror.NewDatabase("failed to update source event", err)
	}
	return nil
}

func (r *Repository) FindSourceEventByDelivery(ctx context.Context, projectID, sourceID uuid.UUID, deliveryID string) (*SourceEvent, error) {
	ev := new(SourceEvent)
	err := r.projectTx(ctx, projectID, func(ctx context.Context, tx bun.Tx) error {
		return tx.NewSelect().Model(ev).
			Where("se.source_id = ?", sourceID).
			Where("se.delivery_id = ?", deliveryID).
			Scan(ctx)
	})
	if err != nil {
		return nil, err
	}
	return ev, nil
}

func (r *Repository) FindSourceEventClaim(ctx context.Context, projectID, sourceID uuid.UUID, subjectRef, headSHA string) (*SourceEventClaim, error) {
	claim := new(SourceEventClaim)
	err := r.projectTx(ctx, projectID, func(ctx context.Context, tx bun.Tx) error {
		return tx.NewSelect().Model(claim).
			Where("sc.source_id = ?", sourceID).
			Where("sc.subject_ref = ?", subjectRef).
			Where("sc.head_sha = ?", headSHA).
			Scan(ctx)
	})
	if err != nil {
		return nil, err
	}
	return claim, nil
}

// ListSourcesByAppAndInstallation returns every enabled github_app source for
// an installation (one installation may serve several projects/repos).
func (r *Repository) ListSourcesByAppAndInstallation(ctx context.Context, appID uuid.UUID, installationID string) ([]*Source, error) {
	var sources []*Source
	if err := r.db.NewSelect().Model(&sources).
		Where("s.app_id = ?", appID).
		Where("s.external_account_id = ?", installationID).
		Where("s.provider = ?", ProviderTypeGitHubApp).
		Where("s.status <> ?", SourceStatusDisabled).
		Order("s.created_at ASC").
		Scan(ctx); err != nil {
		return nil, err
	}
	return sources, nil
}

// DeleteSourceEvent removes a delivery row after a processing failure so the
// provider's retry can re-attempt it.
func (r *Repository) DeleteSourceEvent(ctx context.Context, projectID, id uuid.UUID) error {
	err := r.projectTx(ctx, projectID, func(ctx context.Context, tx bun.Tx) error {
		_, err := tx.NewDelete().Table("kb.source_events").Where("id = ?", id).Exec(ctx)
		return err
	})
	if err != nil {
		return apperror.NewDatabase("failed to delete source event", err)
	}
	return nil
}

// CancelAllSourceClaims cancels every active claim for a source (uninstall).
func (r *Repository) CancelAllSourceClaims(ctx context.Context, projectID, sourceID uuid.UUID) ([]uuid.UUID, error) {
	return r.cancelClaims(ctx, projectID, sourceID, "", "")
}

// CancelSourceClaimsForRepo cancels active claims whose subject is in a repo
// (repository removed from the installation).
func (r *Repository) CancelSourceClaimsForRepo(ctx context.Context, projectID, sourceID uuid.UUID, repo string) ([]uuid.UUID, error) {
	return r.cancelClaims(ctx, projectID, sourceID, "prefix", repo+"#")
}

func (r *Repository) cancelClaims(ctx context.Context, projectID, sourceID uuid.UUID, mode, value string) ([]uuid.UUID, error) {
	var cancelled []uuid.UUID
	lockKey := fmt.Sprintf("source-claim-all:%s:%s:%s", sourceID, mode, value)
	err := r.projectTx(ctx, projectID, func(ctx context.Context, tx bun.Tx) error {
		if _, err := tx.NewRaw("SELECT pg_advisory_xact_lock(hashtextextended(?, 0))", lockKey).Exec(ctx); err != nil {
			return err
		}
		q := tx.NewSelect().Model((*SourceEventClaim)(nil)).
			Where("source_id = ?", sourceID).
			Where("state IN (?)", bun.In([]string{SourceClaimPending, SourceClaimRunning}))
		if mode == "prefix" {
			q = q.Where("subject_ref LIKE ?", value+"%")
		}
		var active []SourceEventClaim
		if err := q.For("UPDATE").Scan(ctx, &active); err != nil {
			return err
		}
		if len(active) == 0 {
			return nil
		}
		ids := make([]uuid.UUID, 0, len(active))
		for _, c := range active {
			ids = append(ids, c.ID)
			if c.RunID != nil {
				cancelled = append(cancelled, *c.RunID)
			}
		}
		_, err := tx.NewUpdate().
			Table("kb.source_event_claims").
			Set("state = ?", SourceClaimCancelled).
			Set("updated_at = now()").
			Where("id IN (?)", bun.In(ids)).
			Exec(ctx)
		return err
	})
	if err != nil {
		return nil, err
	}
	return cancelled, nil
}

// ProjectOrgID returns a project's owning org for source-app binding checks.
func (r *Repository) ProjectOrgID(ctx context.Context, projectID uuid.UUID) (uuid.UUID, error) {
	var orgID uuid.UUID
	if err := r.db.NewSelect().Table("kb.projects").Column("organization_id").Where("id = ?", projectID).Scan(ctx, &orgID); err != nil {
		return uuid.Nil, err
	}
	return orgID, nil
}

func (r *Repository) FindSourceTriggerByID(ctx context.Context, projectID, id uuid.UUID) (*SourceTrigger, error) {
	trigger := new(SourceTrigger)
	err := r.projectTx(ctx, projectID, func(ctx context.Context, tx bun.Tx) error {
		return tx.NewSelect().Model(trigger).Where("st.id = ?", id).Scan(ctx)
	})
	if err != nil {
		return nil, err
	}
	return trigger, nil
}
