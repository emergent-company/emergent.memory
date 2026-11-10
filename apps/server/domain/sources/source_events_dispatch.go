package sources

import (
	"context"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"strconv"
	"strings"

	"github.com/google/uuid"

	"github.com/emergent-company/emergent.memory/domain/agents"
	"github.com/emergent-company/emergent.memory/domain/githubapp"
	"github.com/emergent-company/emergent.memory/internal/config"
	"github.com/emergent-company/emergent.memory/pkg/logger"
)

// Source event sentinel errors.
var (
	ErrSourceAppNotFound           = errors.New("sources: source app not found")
	ErrSourceEventInvalidSignature = errors.New("sources: invalid webhook signature")
	ErrSourceEventMalformed        = errors.New("sources: malformed webhook payload")
	ErrSourceEventNoCredentials    = errors.New("sources: source app credentials are not readable")
)

// SourceEventDispatcher verifies provider webhooks and dispatches matching
// source triggers to agent runs.
type SourceEventDispatcher struct {
	repo       *Repository
	agents     *agents.Repository
	key        []byte
	apiBaseURL string
	minter     *githubapp.TokenMinter
	newClient  func(baseURL, token string) *githubapp.Client
	log        *slog.Logger
}

// NewSourceEventDispatcher builds the dispatcher from the deployment config.
func NewSourceEventDispatcher(repo *Repository, agentsRepo *agents.Repository, cfg *config.Config, log *slog.Logger) *SourceEventDispatcher {
	keyHex, baseURL := "", ""
	if cfg != nil {
		keyHex = cfg.LLMProvider.EncryptionKey
		baseURL = cfg.GitHubSource.APIBaseURL
	}
	key, _ := hex.DecodeString(keyHex)
	return &SourceEventDispatcher{
		repo:       repo,
		agents:     agentsRepo,
		key:        key,
		apiBaseURL: baseURL,
		minter:     githubapp.NewTokenMinter(baseURL, nil),
		newClient: func(baseURL, token string) *githubapp.Client {
			return githubapp.NewClient(baseURL, token, nil)
		},
		log: log.With(logger.Scope("sources.events")),
	}
}

// HandleWebhook verifies and dispatches one provider delivery.
func (d *SourceEventDispatcher) HandleWebhook(ctx context.Context, webhookKey uuid.UUID, event, delivery, signature string, body []byte) error {
	app, err := d.repo.FindSourceAppByWebhookKey(ctx, webhookKey)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return ErrSourceAppNotFound
		}
		return err
	}
	if len(d.key) == 0 {
		return ErrSourceEventNoCredentials
	}
	secret, err := OpenSourceBlob(d.key, app.WebhookSecretEncrypted)
	if err != nil {
		return ErrSourceEventNoCredentials
	}
	if !githubapp.VerifyWebhook(string(secret), body, signature) {
		return ErrSourceEventInvalidSignature
	}
	payload, err := githubapp.ParseWebhook(body)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrSourceEventMalformed, err)
	}

	switch event {
	case "pull_request":
		return d.handlePullRequest(ctx, app, delivery, payload)
	case "installation":
		return d.handleInstallation(ctx, app, payload)
	case "installation_repositories":
		return d.handleInstallationRepositories(ctx, app, payload)
	default:
		return nil
	}
}

func (d *SourceEventDispatcher) handlePullRequest(ctx context.Context, app *SourceApp, delivery string, p *githubapp.WebhookPayload) error {
	instKey := strconv.FormatInt(p.Installation.ID, 10)
	src, err := d.repo.FindSourceByAppAndInstallation(ctx, app.ID, instKey)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil
		}
		return err
	}
	repoName := p.Repository.FullName
	if !repoAllowed(src.Config, repoName) {
		return nil
	}
	if p.AuthoredBy(app.Slug) {
		return nil
	}
	subject := fmt.Sprintf("%s#%d", repoName, p.PullNumber())

	// Lifecycle actions run before the draft/closed skip predicate.
	if p.Action == "closed" || p.Action == "converted_to_draft" {
		runs, err := d.repo.CancelSourceSubjectClaims(ctx, src.ID, subject)
		if err != nil {
			return err
		}
		d.cancelRuns(ctx, runs)
		return nil
	}
	if p.ShouldSkipPullRequest() || !p.IsRelevantPullRequestAction() {
		return nil
	}

	ev := &SourceEvent{
		ProjectID:   src.ProjectID,
		SourceID:    src.ID,
		DeliveryID:  delivery,
		EventType:   "pull_request",
		Action:      p.Action,
		SubjectType: "pull_request",
		SubjectRef:  subject,
		HeadSHA:     p.PullRequest.Head.SHA,
		Payload: map[string]any{
			"repo":     repoName,
			"number":   p.PullNumber(),
			"title":    p.PullRequest.Title,
			"headSha":  p.PullRequest.Head.SHA,
			"baseSha":  p.PullRequest.Base.SHA,
			"baseRef":  p.PullRequest.Base.Ref,
			"headRef":  p.PullRequest.Head.Ref,
			"action":   p.Action,
			"delivery": delivery,
		},
	}
	inserted, err := d.repo.InsertSourceEvent(ctx, ev)
	if err != nil {
		return err
	}
	if !inserted {
		return nil
	}

	token, err := d.mintToken(ctx, app, p.Installation.ID, repoName)
	if err != nil {
		d.markEvent(ctx, ev.ID, SourceEventFailed, nil, err)
		return err
	}
	state, err := d.newClient(d.apiBaseURL, token).GetPullRequest(ctx, repoName, p.PullNumber())
	if err != nil {
		d.markEvent(ctx, ev.ID, SourceEventFailed, nil, err)
		return err
	}
	if state.Draft || state.Merged || (state.State != "" && state.State != "open") {
		d.markEvent(ctx, ev.ID, SourceEventSkipped, nil, nil)
		return nil
	}

	claim, superseded, err := d.repo.ClaimSourceSubject(ctx, src.ProjectID, src.ID, subject, state.HeadSHA, p.PullRequest.Head.SHA)
	if errors.Is(err, ErrSourceEventStaleHead) {
		d.markEvent(ctx, ev.ID, SourceEventSkipped, nil, nil)
		return nil
	}
	if err != nil {
		d.markEvent(ctx, ev.ID, SourceEventFailed, nil, err)
		return err
	}
	d.cancelRuns(ctx, superseded)
	if claim.RunID != nil {
		d.markEvent(ctx, ev.ID, SourceEventDispatched, claim.RunID, nil)
		return nil
	}

	trigger, err := d.matchingTrigger(ctx, src.ID, "pull_request", p.Action)
	if err != nil {
		d.markEvent(ctx, ev.ID, SourceEventFailed, nil, err)
		return err
	}
	if trigger == nil {
		d.markEvent(ctx, ev.ID, SourceEventSkipped, nil, nil)
		return nil
	}

	message := fmt.Sprintf("Handle github_app pull_request %s (%s) for %s at %s", p.Action, subject, repoName, state.HeadSHA)
	metadata := map[string]any{
		"sourceId":       src.ID.String(),
		"provider":       ProviderTypeGitHubApp,
		"eventType":      "pull_request",
		"subjectType":    "pull_request",
		"subjectRef":     subject,
		"repo":           repoName,
		"prNumber":       p.PullNumber(),
		"headSha":        state.HeadSHA,
		"baseSha":        p.PullRequest.Base.SHA,
		"baseRef":        state.BaseRef,
		"headRef":        state.HeadRef,
		"installationId": p.Installation.ID,
		"deliveryId":     delivery,
	}
	run, err := d.agents.CreateRunQueued(ctx, trigger.AgentID.String(), 1, agents.CreateRunQueuedOptions{
		Queue:           defaultSourceQueue(trigger.Queue),
		TriggerMessage:  &message,
		TriggerMetadata: metadata,
		TrustedInternal: false,
	})
	if err != nil {
		_, _ = d.repo.SetSourceEventClaimState(ctx, claim.ID, SourceClaimPending)
		d.markEvent(ctx, ev.ID, SourceEventFailed, nil, err)
		return fmt.Errorf("sources: enqueue source trigger: %w", err)
	}
	runID, err := uuid.Parse(run.ID)
	if err != nil {
		d.markEvent(ctx, ev.ID, SourceEventFailed, nil, err)
		return err
	}
	attached, err := d.repo.SetSourceEventClaimRun(ctx, claim.ID, runID, SourceClaimRunning)
	if err != nil {
		d.markEvent(ctx, ev.ID, SourceEventFailed, nil, err)
		return err
	}
	if !attached {
		_, _ = d.agents.RequestRunCancellation(ctx, run.ID)
		d.markEvent(ctx, ev.ID, SourceEventSkipped, nil, nil)
		return nil
	}
	d.markEvent(ctx, ev.ID, SourceEventDispatched, &runID, nil)
	return nil
}

func (d *SourceEventDispatcher) handleInstallation(ctx context.Context, app *SourceApp, p *githubapp.WebhookPayload) error {
	instKey := strconv.FormatInt(p.Installation.ID, 10)
	switch p.Action {
	case "deleted":
		return d.repo.DisableSourcesByInstallation(ctx, app.ID, instKey)
	case "created", "new_permissions_accepted", "unsuspended":
		if len(p.Repositories) == 0 {
			return nil
		}
		src, err := d.repo.FindSourceByAppAndInstallation(ctx, app.ID, instKey)
		if errors.Is(err, sql.ErrNoRows) {
			return nil
		}
		if err != nil {
			return err
		}
		return d.repo.UpdateSourceRepos(ctx, src.ID, webhookRepoNames(p.Repositories))
	default:
		return nil
	}
}

func (d *SourceEventDispatcher) handleInstallationRepositories(ctx context.Context, app *SourceApp, p *githubapp.WebhookPayload) error {
	instKey := strconv.FormatInt(p.Installation.ID, 10)
	src, err := d.repo.FindSourceByAppAndInstallation(ctx, app.ID, instKey)
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	repos := applySourceRepoDelta(sourceRepoList(src.Config), p.RepositoriesAdded, p.RepositoriesRemoved)
	return d.repo.UpdateSourceRepos(ctx, src.ID, repos)
}

func (d *SourceEventDispatcher) matchingTrigger(ctx context.Context, sourceID uuid.UUID, eventType, action string) (*SourceTrigger, error) {
	triggers, err := d.repo.ListEnabledTriggersForSource(ctx, sourceID)
	if err != nil {
		return nil, err
	}
	for _, trigger := range triggers {
		if trigger.EventType != eventType {
			continue
		}
		if triggerAllowsAction(trigger.EventFilter, action) {
			return trigger, nil
		}
	}
	return nil, nil
}

func (d *SourceEventDispatcher) mintToken(ctx context.Context, app *SourceApp, installationID int64, repoName string) (string, error) {
	if len(d.key) == 0 {
		return "", ErrSourceEventNoCredentials
	}
	privateKey, err := OpenSourceBlob(d.key, app.PrivateKeyEncrypted)
	if err != nil {
		return "", ErrSourceEventNoCredentials
	}
	return d.minter.MintInstallationToken(ctx, app.AppID, privateKey, installationID, []string{repoName},
		map[string]string{
			"contents":      "read",
			"pull_requests": "write",
			"issues":        "write",
			"checks":        "write",
		})
}

func (d *SourceEventDispatcher) markEvent(ctx context.Context, id uuid.UUID, state string, runID *uuid.UUID, cause error) {
	var msg *string
	if cause != nil {
		s := cause.Error()
		msg = &s
	}
	if err := d.repo.SetSourceEventState(ctx, id, state, runID, msg); err != nil && d.log != nil {
		d.log.Warn("failed to record source event state", "event", id, "error", err)
	}
}

func (d *SourceEventDispatcher) cancelRuns(ctx context.Context, runIDs []uuid.UUID) {
	for _, id := range runIDs {
		if _, err := d.agents.RequestRunCancellation(ctx, id.String()); err != nil && d.log != nil {
			d.log.Warn("failed to cancel superseded source run", "runId", id, "error", err)
		}
	}
}

func triggerAllowsAction(filter map[string]any, action string) bool {
	if len(filter) == 0 {
		return true
	}
	raw, ok := filter["actions"]
	if !ok {
		return true
	}
	switch v := raw.(type) {
	case []any:
		for _, item := range v {
			if fmt.Sprint(item) == action {
				return true
			}
		}
	case []string:
		for _, item := range v {
			if item == action {
				return true
			}
		}
	}
	return false
}

func repoAllowed(cfg map[string]any, repoName string) bool {
	for _, repo := range sourceRepoList(cfg) {
		if strings.EqualFold(repo, repoName) {
			return true
		}
	}
	return false
}

func sourceRepoList(cfg map[string]any) []string {
	if cfg == nil {
		return nil
	}
	switch v := cfg["repos"].(type) {
	case []string:
		return v
	case []any:
		out := make([]string, 0, len(v))
		for _, item := range v {
			if s := strings.TrimSpace(fmt.Sprint(item)); s != "" {
				out = append(out, s)
			}
		}
		return out
	default:
		return nil
	}
}

func webhookRepoNames(repos []githubapp.WebhookRepo) []string {
	out := make([]string, 0, len(repos))
	for _, repo := range repos {
		if repo.FullName != "" {
			out = append(out, repo.FullName)
		}
	}
	return normalizeSourceRepos(out)
}

func applySourceRepoDelta(current []string, added, removed []githubapp.WebhookRepo) []string {
	set := map[string]string{}
	for _, repo := range current {
		set[strings.ToLower(repo)] = repo
	}
	for _, repo := range added {
		if repo.FullName != "" {
			set[strings.ToLower(repo.FullName)] = repo.FullName
		}
	}
	for _, repo := range removed {
		delete(set, strings.ToLower(repo.FullName))
	}
	out := make([]string, 0, len(set))
	for _, repo := range set {
		out = append(out, repo)
	}
	return normalizeSourceRepos(out)
}

func normalizeSourceRepos(repos []string) []string {
	seen := map[string]struct{}{}
	out := make([]string, 0, len(repos))
	for _, repo := range repos {
		repo = strings.TrimSpace(repo)
		if repo == "" {
			continue
		}
		key := strings.ToLower(repo)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, repo)
	}
	return out
}

func defaultSourceQueue(queue string) string {
	if strings.TrimSpace(queue) == "" {
		return "default"
	}
	return strings.TrimSpace(queue)
}
