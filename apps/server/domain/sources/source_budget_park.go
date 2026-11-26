package sources

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/emergent-company/emergent.memory/domain/agents"
	"github.com/emergent-company/emergent.memory/domain/graph"
)

// reviewLaneBudgetCheckTimeout bounds the background check publication so a
// GitHub outage cannot hold the dispatch callback open indefinitely.
const reviewLaneBudgetCheckTimeout = 30 * time.Second

// reviewLaneBudgetTarget is the GitHub identity a budget-parked review lane
// needs to publish its check: the repo, head SHA, and the source whose App
// credentials mint the token.
type reviewLaneBudgetTarget struct {
	Repo     string
	HeadSHA  string
	SourceID string
}

// reviewLaneBudgetTargetFromHead extracts the target from a GitHubReviewLane
// head. A lane outside the source workflow (or one missing its GitHub
// properties) has no check to publish and is skipped.
func reviewLaneBudgetTargetFromHead(head *graph.WorkObjectHead) (reviewLaneBudgetTarget, bool) {
	if head == nil || head.Type != "GitHubReviewLane" {
		return reviewLaneBudgetTarget{}, false
	}
	props := head.Properties
	repo, _ := props["repo"].(string)
	headSHA, _ := props["head_sha"].(string)
	sourceID, _ := props["source_id"].(string)
	target := reviewLaneBudgetTarget{
		Repo:     strings.TrimSpace(repo),
		HeadSHA:  strings.TrimSpace(headSHA),
		SourceID: strings.TrimSpace(sourceID),
	}
	if target.Repo == "" || target.HeadSHA == "" || target.SourceID == "" {
		return reviewLaneBudgetTarget{}, false
	}
	return target, true
}

// OnWorkBudgetParked publishes a non-blocking, head-bound `reviewed` check when
// the spending gate refuses to schedule a review lane. Without it the required
// check stays in_progress forever and the PR looks blocked for no visible
// reason: the reviewer run was never created (#1032). The conclusion is
// neutral, not failure, because the head itself has not been judged — the
// review is unavailable until the operator raises budget_usd or resets the
// project's budget window.
func (d *SourceEventDispatcher) OnWorkBudgetParked(ctx context.Context, ev agents.WorkBudgetParkedEvent) {
	if d == nil {
		return
	}
	target, ok := reviewLaneBudgetTargetFromHead(ev.Head)
	if !ok {
		return
	}
	if d.repo == nil || d.newClient == nil {
		return
	}
	// The listener runs on the dispatch path. Publish in the background with a
	// bounded, cancellation-independent context so a slow GitHub call cannot
	// delay the trigger goroutine or the reconciler's scan.
	go func() {
		publishCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), reviewLaneBudgetCheckTimeout)
		defer cancel()
		d.publishReviewLaneBudgetUnavailable(publishCtx, ev, target)
	}()
}

// publishReviewLaneBudgetUnavailable resolves the lane's source App, mints a
// repo-scoped installation token, and posts the non-blocking check. Every
// failure is best-effort: the durable work-item park is the primary truth.
func (d *SourceEventDispatcher) publishReviewLaneBudgetUnavailable(ctx context.Context, ev agents.WorkBudgetParkedEvent, target reviewLaneBudgetTarget) {
	sourceID, err := uuid.Parse(target.SourceID)
	if err != nil {
		d.logWarn("review lane budget park has an invalid source id", "source_id", target.SourceID, "error", err)
		return
	}
	src, err := d.repo.FindSourceByID(ctx, sourceID)
	if err != nil {
		if !errors.Is(err, sql.ErrNoRows) {
			d.logWarn("could not resolve source for a budget-parked review lane", "source_id", target.SourceID, "error", err)
		}
		return
	}
	// The lane carries the project it belongs to; a mismatched source must not
	// let one project's park touch another project's checks.
	if src.ProjectID.String() != ev.ProjectID {
		d.logWarn("budget-parked review lane source belongs to another project",
			"source_id", target.SourceID, "project_id", ev.ProjectID)
		return
	}
	if src.AppID == nil || src.ExternalAccountID == nil {
		return
	}
	installationID, err := strconv.ParseInt(strings.TrimSpace(*src.ExternalAccountID), 10, 64)
	if err != nil {
		d.logWarn("budget-parked review lane source has an invalid installation id", "source_id", target.SourceID, "error", err)
		return
	}
	app, err := d.repo.FindSourceAppByID(ctx, *src.AppID)
	if err != nil {
		d.logWarn("could not resolve the App for a budget-parked review lane", "source_id", target.SourceID, "error", err)
		return
	}
	token, err := d.mintToken(ctx, app, installationID, target.Repo)
	if err != nil {
		d.logWarn("could not mint a token to publish a budget-parked review check",
			"repo", target.Repo, "head_sha", target.HeadSHA, "error", err)
		return
	}
	summary := fmt.Sprintf(
		"Review agent %q was not scheduled: %s. Raise budget_usd in the project's budget settings or reset the budget window; the lane resumes automatically once the cap allows.",
		ev.AgentName, ev.Reason,
	)
	if err := d.newClient(d.apiBaseURL, token).CreateReviewedCheck(ctx, target.Repo, target.HeadSHA,
		"neutral", "reviewed: unavailable — budget", summary); err != nil {
		d.logWarn("failed to publish the budget-parked reviewed check",
			"repo", target.Repo, "head_sha", target.HeadSHA, "error", err)
	}
}

// logWarn is a nil-safe logger shim for the budget-park listener.
func (d *SourceEventDispatcher) logWarn(msg string, args ...any) {
	if d != nil && d.log != nil {
		d.log.Warn(msg, args...)
	}
}
