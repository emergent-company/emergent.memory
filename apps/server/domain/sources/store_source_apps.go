package sources

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/uptrace/bun"

	"github.com/emergent-company/emergent.memory/pkg/apperror"
)

// ErrSourceEventStaleHead is returned when the delivered head is not the
// subject's current head (out-of-order or replayed delivery).
var ErrSourceEventStaleHead = errors.New("sources: delivered head is not the current subject head")

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

func (r *Repository) DeleteSourceApp(ctx context.Context, id uuid.UUID) error {
	if _, err := r.db.NewDelete().Model((*SourceApp)(nil)).Where("id = ?", id).Exec(ctx); err != nil {
		return apperror.NewDatabase("failed to delete source app", err)
	}
	return nil
}

// --- Triggers ---

func (r *Repository) CreateSourceTrigger(ctx context.Context, trigger *SourceTrigger) error {
	if trigger.EventFilter == nil {
		trigger.EventFilter = map[string]any{}
	}
	if _, err := r.db.NewInsert().Model(trigger).Returning("*").Exec(ctx); err != nil {
		return apperror.NewDatabase("failed to create source trigger", err)
	}
	return nil
}

func (r *Repository) ListEnabledTriggersForSource(ctx context.Context, sourceID uuid.UUID) ([]*SourceTrigger, error) {
	var triggers []*SourceTrigger
	if err := r.db.NewSelect().Model(&triggers).
		Where("st.source_id = ?", sourceID).
		Where("st.enabled = true").
		Order("st.created_at ASC").
		Scan(ctx); err != nil {
		return nil, err
	}
	return triggers, nil
}

func (r *Repository) ListSourceTriggers(ctx context.Context, sourceID uuid.UUID) ([]*SourceTrigger, error) {
	var triggers []*SourceTrigger
	if err := r.db.NewSelect().Model(&triggers).
		Where("st.source_id = ?", sourceID).
		Order("st.event_type ASC").
		Scan(ctx); err != nil {
		return nil, err
	}
	return triggers, nil
}

func (r *Repository) UpdateSourceTrigger(ctx context.Context, trigger *SourceTrigger) error {
	if trigger.EventFilter == nil {
		trigger.EventFilter = map[string]any{}
	}
	if _, err := r.db.NewUpdate().Model(trigger).
		Column("agent_id", "queue", "event_filter", "enabled").
		Set("updated_at = now()").
		Where("id = ?", trigger.ID).
		Exec(ctx); err != nil {
		return apperror.NewDatabase("failed to update source trigger", err)
	}
	return nil
}

func (r *Repository) DeleteSourceTrigger(ctx context.Context, id uuid.UUID) error {
	if _, err := r.db.NewDelete().Model((*SourceTrigger)(nil)).Where("id = ?", id).Exec(ctx); err != nil {
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
	res, err := r.db.NewInsert().Model(ev).
		Returning("*").
		On("CONFLICT (source_id, delivery_id) DO NOTHING").
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

func (r *Repository) FindSourceEventByID(ctx context.Context, id uuid.UUID) (*SourceEvent, error) {
	ev := new(SourceEvent)
	if err := r.db.NewSelect().Model(ev).Where("se.id = ?", id).Scan(ctx); err != nil {
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

	err := r.db.RunInTx(ctx, nil, func(ctx context.Context, tx bun.Tx) error {
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
func (r *Repository) SetSourceEventClaimRun(ctx context.Context, claimID, runID uuid.UUID, state string) (bool, error) {
	res, err := r.db.NewUpdate().
		Table("kb.source_event_claims").
		Set("run_id = ?", runID).
		Set("state = ?", state).
		Set("updated_at = now()").
		Where("id = ?", claimID).
		Where("run_id IS NULL").
		Where("state = ?", SourceClaimPending).
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

func (r *Repository) SetSourceEventClaimState(ctx context.Context, claimID uuid.UUID, state string) (bool, error) {
	res, err := r.db.NewUpdate().
		Table("kb.source_event_claims").
		Set("state = ?", state).
		Set("updated_at = now()").
		Where("id = ?", claimID).
		Where("state IN (?)", bun.In([]string{SourceClaimPending, SourceClaimRunning})).
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

// CancelSourceSubjectClaims cancels active claims for a subject (closed/draft)
// and returns their run ids.
func (r *Repository) CancelSourceSubjectClaims(ctx context.Context, sourceID uuid.UUID, subjectRef string) ([]uuid.UUID, error) {
	var cancelled []uuid.UUID
	lockKey := fmt.Sprintf("source-claim:%s:%s", sourceID, subjectRef)
	err := r.db.RunInTx(ctx, nil, func(ctx context.Context, tx bun.Tx) error {
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
func (r *Repository) FindSourceEventClaimByRunID(ctx context.Context, runID uuid.UUID) (*SourceEventClaim, error) {
	claim := new(SourceEventClaim)
	if err := r.db.NewSelect().Model(claim).Where("sc.run_id = ?", runID).Limit(1).Scan(ctx); err != nil {
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
func (r *Repository) SetSourceEventState(ctx context.Context, id uuid.UUID, state string, runID *uuid.UUID, errMsg *string) error {
	if _, err := r.db.NewUpdate().
		Table("kb.source_events").
		Set("state = ?", state).
		Set("run_id = ?", runID).
		Set("error = ?", errMsg).
		Set("updated_at = now()").
		Where("id = ?", id).
		Exec(ctx); err != nil {
		return apperror.NewDatabase("failed to update source event", err)
	}
	return nil
}

func (r *Repository) FindSourceEventByDelivery(ctx context.Context, sourceID uuid.UUID, deliveryID string) (*SourceEvent, error) {
	ev := new(SourceEvent)
	if err := r.db.NewSelect().Model(ev).
		Where("se.source_id = ?", sourceID).
		Where("se.delivery_id = ?", deliveryID).
		Scan(ctx); err != nil {
		return nil, err
	}
	return ev, nil
}

func (r *Repository) FindSourceEventClaim(ctx context.Context, sourceID uuid.UUID, subjectRef, headSHA string) (*SourceEventClaim, error) {
	claim := new(SourceEventClaim)
	if err := r.db.NewSelect().Model(claim).
		Where("sc.source_id = ?", sourceID).
		Where("sc.subject_ref = ?", subjectRef).
		Where("sc.head_sha = ?", headSHA).
		Scan(ctx); err != nil {
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
func (r *Repository) DeleteSourceEvent(ctx context.Context, id uuid.UUID) error {
	if _, err := r.db.NewDelete().Table("kb.source_events").Where("id = ?", id).Exec(ctx); err != nil {
		return apperror.NewDatabase("failed to delete source event", err)
	}
	return nil
}

// CancelAllSourceClaims cancels every active claim for a source (uninstall).
func (r *Repository) CancelAllSourceClaims(ctx context.Context, sourceID uuid.UUID) ([]uuid.UUID, error) {
	return r.cancelClaims(ctx, sourceID, "", "")
}

// CancelSourceClaimsForRepo cancels active claims whose subject is in a repo
// (repository removed from the installation).
func (r *Repository) CancelSourceClaimsForRepo(ctx context.Context, sourceID uuid.UUID, repo string) ([]uuid.UUID, error) {
	return r.cancelClaims(ctx, sourceID, "prefix", repo+"#")
}

func (r *Repository) cancelClaims(ctx context.Context, sourceID uuid.UUID, mode, value string) ([]uuid.UUID, error) {
	var cancelled []uuid.UUID
	lockKey := fmt.Sprintf("source-claim-all:%s:%s:%s", sourceID, mode, value)
	err := r.db.RunInTx(ctx, nil, func(ctx context.Context, tx bun.Tx) error {
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
