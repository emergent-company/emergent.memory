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
	"time"

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

const asyncSourceEventTimeout = 2 * time.Minute

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
	key, err := hex.DecodeString(keyHex)
	if err != nil && log != nil {
		log.Error("sources: invalid LLM_ENCRYPTION_KEY; source app credentials are unreadable", "error", err)
		key = nil
	}
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

// VerifiedSourceEvent is an authenticated, parsed delivery ready to process.
type VerifiedSourceEvent struct {
	Event    string
	Delivery string
	App      *SourceApp
	Payload  *githubapp.WebhookPayload
}

// Verify resolves the App, verifies the signature, and parses the payload. It
// never trusts the body before the App's secret is verified.
func (d *SourceEventDispatcher) Verify(ctx context.Context, webhookKey uuid.UUID, event, delivery, signature string, body []byte) (*VerifiedSourceEvent, error) {
	app, err := d.repo.FindSourceAppByWebhookKey(ctx, webhookKey)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrSourceAppNotFound
		}
		return nil, err
	}
	if len(d.key) == 0 {
		return nil, ErrSourceEventNoCredentials
	}
	secret, err := OpenSourceBlob(d.key, app.WebhookSecretEncrypted)
	if err != nil {
		return nil, ErrSourceEventNoCredentials
	}
	if !githubapp.VerifyWebhook(string(secret), body, signature) {
		return nil, ErrSourceEventInvalidSignature
	}
	if strings.TrimSpace(delivery) == "" {
		return nil, fmt.Errorf("%w: missing delivery id", ErrSourceEventMalformed)
	}
	payload, err := githubapp.ParseWebhook(body)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrSourceEventMalformed, err)
	}
	return &VerifiedSourceEvent{Event: event, Delivery: delivery, App: app, Payload: payload}, nil
}

// Process routes a verified delivery. It is safe to run after the inbound
// request has returned 202.
func (d *SourceEventDispatcher) Process(ctx context.Context, v *VerifiedSourceEvent) error {
	switch v.Event {
	case "pull_request":
		return d.handlePullRequest(ctx, v)
	case "installation":
		return d.handleInstallation(ctx, v)
	case "installation_repositories":
		return d.handleInstallationRepositories(ctx, v)
	default:
		return nil
	}
}

// ProcessAsync processes a verified delivery on a detached, bounded context.
func (d *SourceEventDispatcher) ProcessAsync(v *VerifiedSourceEvent) {
	ctx, cancel := context.WithTimeout(context.Background(), asyncSourceEventTimeout)
	defer cancel()
	if err := d.Process(ctx, v); err != nil && d.log != nil {
		d.log.Warn("source event processing failed", "event", v.Event, "delivery", v.Delivery, "error", err)
	}
}

// HandleWebhook is the synchronous verify+process path.
func (d *SourceEventDispatcher) HandleWebhook(ctx context.Context, webhookKey uuid.UUID, event, delivery, signature string, body []byte) error {
	v, err := d.Verify(ctx, webhookKey, event, delivery, signature, body)
	if err != nil || v == nil {
		return err
	}
	return d.Process(ctx, v)
}

func (d *SourceEventDispatcher) handlePullRequest(ctx context.Context, v *VerifiedSourceEvent) error {
	p := v.Payload
	repoName := p.Repository.FullName
	if p.AuthoredBy(v.App.Slug) {
		return nil
	}
	sources, err := d.repo.ListSourcesByAppAndInstallation(ctx, v.App.ID, strconv.FormatInt(p.Installation.ID, 10))
	if err != nil {
		return err
	}
	allowed := make([]*Source, 0, len(sources))
	for _, src := range sources {
		if repoAllowed(src.Config, repoName) {
			allowed = append(allowed, src)
		}
	}
	if len(allowed) == 0 {
		return nil
	}

	subject := fmt.Sprintf("%s#%d", repoName, p.PullNumber())
	if p.Action == "closed" || p.Action == "converted_to_draft" {
		for _, src := range allowed {
			runs, err := d.repo.CancelSourceSubjectClaims(ctx, src.ID, subject)
			if err != nil {
				return err
			}
			d.cancelRuns(ctx, runs)
		}
		return nil
	}
	if p.ShouldSkipPullRequest() || !p.IsRelevantPullRequestAction() {
		return nil
	}

	// One installation token scoped to the event repo is enough for every source.
	token, err := d.mintToken(ctx, v.App, p.Installation.ID, repoName)
	if err != nil {
		return err
	}
	state, err := d.newClient(d.apiBaseURL, token).GetPullRequest(ctx, repoName, p.PullNumber())
	if err != nil {
		return err
	}
	if state.Draft || state.Merged || (state.State != "" && state.State != "open") {
		return nil
	}

	for _, src := range allowed {
		if err := d.dispatchPullRequestSource(ctx, v, src, subject, state); err != nil {
			return err
		}
	}
	return nil
}

func (d *SourceEventDispatcher) dispatchPullRequestSource(ctx context.Context, v *VerifiedSourceEvent, src *Source, subject string, state *githubapp.PullRequestState) error {
	p := v.Payload
	repoName := p.Repository.FullName
	ev := &SourceEvent{
		ProjectID:   src.ProjectID,
		SourceID:    src.ID,
		DeliveryID:  v.Delivery,
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
			"delivery": v.Delivery,
		},
	}
	inserted, err := d.repo.InsertSourceEvent(ctx, ev)
	if err != nil {
		return err
	}
	if !inserted {
		return nil
	}
	if err := d.processPullRequestSource(ctx, v, src, subject, state, ev); err != nil {
		// Leave no row behind: a redelivery must be able to retry.
		_ = d.repo.DeleteSourceEvent(ctx, ev.ID)
		return err
	}
	return nil
}

func (d *SourceEventDispatcher) processPullRequestSource(ctx context.Context, v *VerifiedSourceEvent, src *Source, subject string, state *githubapp.PullRequestState, ev *SourceEvent) error {
	p := v.Payload
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

	repoName := p.Repository.FullName
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
		"deliveryId":     v.Delivery,
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

func (d *SourceEventDispatcher) handleInstallation(ctx context.Context, v *VerifiedSourceEvent) error {
	p := v.Payload
	instKey := strconv.FormatInt(p.Installation.ID, 10)
	sources, err := d.repo.ListSourcesByAppAndInstallation(ctx, v.App.ID, instKey)
	if err != nil {
		return err
	}
	switch p.Action {
	case "deleted", "suspend":
		for _, src := range sources {
			runs, err := d.repo.CancelAllSourceClaims(ctx, src.ID)
			if err != nil {
				return err
			}
			d.cancelRuns(ctx, runs)
		}
		return d.repo.DisableSourcesByInstallation(ctx, v.App.ID, instKey)
	case "created", "new_permissions_accepted", "unsuspend":
		if len(p.Repositories) == 0 {
			return nil
		}
		for _, src := range sources {
			if err := d.repo.UpdateSourceRepos(ctx, src.ID, webhookRepoNames(p.Repositories)); err != nil {
				return err
			}
		}
		return nil
	default:
		return nil
	}
}

func (d *SourceEventDispatcher) handleInstallationRepositories(ctx context.Context, v *VerifiedSourceEvent) error {
	p := v.Payload
	sources, err := d.repo.ListSourcesByAppAndInstallation(ctx, v.App.ID, strconv.FormatInt(p.Installation.ID, 10))
	if err != nil {
		return err
	}
	for _, src := range sources {
		repos := applySourceRepoDelta(sourceRepoList(src.Config), p.RepositoriesAdded, p.RepositoriesRemoved)
		if err := d.repo.UpdateSourceRepos(ctx, src.ID, repos); err != nil {
			return err
		}
		for _, removed := range p.RepositoriesRemoved {
			if removed.FullName == "" {
				continue
			}
			runs, err := d.repo.CancelSourceClaimsForRepo(ctx, src.ID, removed.FullName)
			if err != nil {
				return err
			}
			d.cancelRuns(ctx, runs)
		}
	}
	return nil
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
