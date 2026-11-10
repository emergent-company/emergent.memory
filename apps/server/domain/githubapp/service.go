package githubapp

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"

	"github.com/google/uuid"

	"github.com/emergent-company/emergent.memory/domain/agents"
	"github.com/emergent-company/emergent.memory/internal/config"
	"github.com/emergent-company/emergent.memory/pkg/apperror"
	"github.com/emergent-company/emergent.memory/pkg/auth"
)

// Webhook/producer sentinel errors.
var (
	ErrWebhookNotConfigured = errors.New("githubapp: webhook is not configured")
	ErrUnknownInstallation  = errors.New("githubapp: unknown or disabled installation")
	ErrInvalidSignature     = errors.New("githubapp: invalid webhook signature")
	ErrMissingCredentials   = errors.New("githubapp: app credentials are not readable")
)

// Service implements App registration, installation routing, webhook ingress,
// and the review-queue producer.
type Service struct {
	repo       *Repository
	agents     *agents.Repository
	cipher     *CredentialCipher
	minter     *TokenMinter
	apiBaseURL string
	log        *slog.Logger
	newClient  func(baseURL, token string) *Client
}

// NewService builds the service. A nil cipher is valid and fails closed.
func NewService(
	repo *Repository,
	agentsRepo *agents.Repository,
	cipher *CredentialCipher,
	minter *TokenMinter,
	cfg *config.Config,
	log *slog.Logger,
) *Service {
	baseURL := ""
	if cfg != nil {
		baseURL = cfg.GitHubSource.APIBaseURL
	}
	return &Service{
		repo:       repo,
		agents:     agentsRepo,
		cipher:     cipher,
		minter:     minter,
		apiBaseURL: baseURL,
		log:        log,
		newClient: func(baseURL, token string) *Client {
			return NewClient(baseURL, token, nil)
		},
	}
}

// RegisterAppRequest is the admin App registration payload.
type RegisterAppRequest struct {
	AppID         int64
	Slug          string
	PrivateKey    string
	WebhookSecret string
}

// RegisterApp stores App-level credentials, encrypted.
func (s *Service) RegisterApp(ctx context.Context, orgID uuid.UUID, userID string, req RegisterAppRequest) (*GitHubApp, error) {
	if err := s.requireOrgAdmin(ctx, orgID, userID); err != nil {
		return nil, err
	}
	if req.AppID <= 0 || strings.TrimSpace(req.PrivateKey) == "" || strings.TrimSpace(req.WebhookSecret) == "" {
		return nil, apperror.NewValidation("appId, privateKey, and webhookSecret are required")
	}
	if s.cipher == nil {
		return nil, apperror.NewValidation("GITHUB_APP_ENCRYPTION_KEY is not configured")
	}
	privateKey, err := s.cipher.Seal(req.PrivateKey)
	if err != nil {
		return nil, apperror.NewInternal("failed to seal private key", err)
	}
	webhookSecret, err := s.cipher.Seal(req.WebhookSecret)
	if err != nil {
		return nil, apperror.NewInternal("failed to seal webhook secret", err)
	}
	app := &GitHubApp{
		OrgID:                  orgID,
		AppID:                  req.AppID,
		Slug:                   strings.TrimSpace(req.Slug),
		PrivateKeyEncrypted:    privateKey,
		WebhookSecretEncrypted: webhookSecret,
	}
	if err := s.repo.CreateApp(ctx, app); err != nil {
		return nil, mapConflict(err, "app already registered for this org")
	}
	return app, nil
}

// OrgAppView is the credential-free admin projection.
type OrgAppView struct {
	App           *GitHubApp
	Installations []*Installation
}

// ListOrgApp returns the org's App (if any) and installations, without secrets.
func (s *Service) ListOrgApp(ctx context.Context, orgID uuid.UUID, userID string) (*OrgAppView, error) {
	if err := s.requireOrgAdmin(ctx, orgID, userID); err != nil {
		return nil, err
	}
	view := &OrgAppView{}
	app, err := s.repo.FindAnyAppByOrg(ctx, orgID)
	if err == nil {
		view.App = app
	}
	insts, err := s.repo.ListInstallationsByOrg(ctx, orgID)
	if err != nil {
		return nil, apperror.NewInternal("failed to list installations", err)
	}
	view.Installations = insts
	return view, nil
}

// CreateInstallationRequest binds a GitHub installation to a project.
type CreateInstallationRequest struct {
	ProjectID      uuid.UUID
	InstallationID int64
	Repos          []string
	AgentID        uuid.UUID
	Queue          string
}

// CreateInstallation validates the runtime agent belongs to the project.
func (s *Service) CreateInstallation(ctx context.Context, orgID uuid.UUID, userID string, req CreateInstallationRequest) (*Installation, error) {
	if err := s.requireOrgAdmin(ctx, orgID, userID); err != nil {
		return nil, err
	}
	if req.InstallationID <= 0 || req.ProjectID == uuid.Nil || req.AgentID == uuid.Nil {
		return nil, apperror.NewValidation("installationId, projectId, and agentId are required")
	}
	if len(req.Repos) == 0 {
		return nil, apperror.NewValidation("repos must not be empty")
	}
	projectIDStr := req.ProjectID.String()
	agent, err := s.agents.FindByID(ctx, req.AgentID.String(), &projectIDStr)
	if err != nil || agent == nil {
		return nil, apperror.NewNotFound("Agent", req.AgentID.String())
	}
	app, err := s.repo.FindAnyAppByOrg(ctx, orgID)
	if err != nil {
		return nil, apperror.NewValidation("register a GitHub App for this org first")
	}
	inst := &Installation{
		AppPK:          app.ID,
		OrgID:          orgID,
		ProjectID:      req.ProjectID,
		InstallationID: req.InstallationID,
		Repos:          normalizeRepos(req.Repos),
		AgentID:        req.AgentID,
		Queue:          defaultQueue(req.Queue),
		Enabled:        true,
	}
	if err := s.repo.CreateInstallation(ctx, inst); err != nil {
		return nil, mapConflict(err, "installation is already bound")
	}
	return inst, nil
}

// UpdateInstallationRequest patches the mutable routing fields.
type UpdateInstallationRequest struct {
	Repos   *[]string
	AgentID *uuid.UUID
	Queue   *string
	Enabled *bool
}

// UpdateInstallation patches an installation after checking org ownership.
func (s *Service) UpdateInstallation(ctx context.Context, orgID uuid.UUID, userID string, id uuid.UUID, req UpdateInstallationRequest) (*Installation, error) {
	if err := s.requireOrgAdmin(ctx, orgID, userID); err != nil {
		return nil, err
	}
	inst, err := s.repo.FindInstallationByID(ctx, id)
	if err != nil || inst.OrgID != orgID {
		return nil, apperror.NewNotFound("Installation", id.String())
	}
	if req.Repos != nil {
		inst.Repos = normalizeRepos(*req.Repos)
	}
	if req.AgentID != nil {
		projectIDStr := inst.ProjectID.String()
		agent, err := s.agents.FindByID(ctx, req.AgentID.String(), &projectIDStr)
		if err != nil || agent == nil {
			return nil, apperror.NewNotFound("Agent", req.AgentID.String())
		}
		inst.AgentID = *req.AgentID
	}
	if req.Queue != nil {
		inst.Queue = defaultQueue(*req.Queue)
	}
	if req.Enabled != nil {
		inst.Enabled = *req.Enabled
	}
	if err := s.repo.UpdateInstallation(ctx, inst); err != nil {
		return nil, apperror.NewInternal("failed to update installation", err)
	}
	return inst, nil
}

// DeleteInstallation removes an installation.
func (s *Service) DeleteInstallation(ctx context.Context, orgID uuid.UUID, userID string, id uuid.UUID) error {
	if err := s.requireOrgAdmin(ctx, orgID, userID); err != nil {
		return err
	}
	inst, err := s.repo.FindInstallationByID(ctx, id)
	if err != nil || inst.OrgID != orgID {
		return apperror.NewNotFound("Installation", id.String())
	}
	return s.repo.DeleteInstallation(ctx, id)
}

// DeleteApp removes the org's App and its installations.
func (s *Service) DeleteApp(ctx context.Context, orgID uuid.UUID, userID string, appPK uuid.UUID) error {
	if err := s.requireOrgAdmin(ctx, orgID, userID); err != nil {
		return err
	}
	app, err := s.repo.FindAppByID(ctx, appPK)
	if err != nil || app.OrgID != orgID {
		return apperror.NewNotFound("GitHubApp", appPK.String())
	}
	return s.repo.DeleteApp(ctx, appPK)
}

// HandleWebhook verifies and processes one GitHub delivery.
func (s *Service) HandleWebhook(ctx context.Context, event, delivery, signature string, body []byte) error {
	payload, err := ParseWebhook(body)
	if err != nil {
		return fmt.Errorf("githubapp: malformed payload: %w", err)
	}
	inst, err := s.repo.FindEnabledInstallationByInstallationID(ctx, payload.Installation.ID)
	if err != nil {
		return ErrUnknownInstallation
	}
	app, err := s.repo.FindAppByID(ctx, inst.AppPK)
	if err != nil {
		return ErrUnknownInstallation
	}
	if s.cipher == nil {
		return ErrMissingCredentials
	}
	secret, err := s.cipher.Open(app.WebhookSecretEncrypted)
	if err != nil {
		return ErrMissingCredentials
	}
	if !VerifyWebhook(secret, body, signature) {
		return ErrInvalidSignature
	}

	if delivery != "" {
		inserted, err := s.repo.InsertDelivery(ctx, &Delivery{
			DeliveryID:     delivery,
			InstallationID: payload.Installation.ID,
			Repo:           payload.Repository.FullName,
			PullRequest:    payload.PullNumber(),
			HeadSHA:        payload.PullRequest.Head.SHA,
		})
		if err != nil {
			return err
		}
		if !inserted {
			return nil
		}
	}

	switch event {
	case "pull_request":
		return s.handlePullRequest(ctx, app, inst, delivery, payload)
	case "installation":
		if payload.Action == "deleted" {
			return s.repo.DisableInstallationByInstallationID(ctx, inst.InstallationID)
		}
		return nil
	case "installation_repositories":
		repos := applyRepoDelta(inst.Repos, payload.Repositories.Added, payload.Repositories.Removed)
		return s.repo.UpdateInstallationRepos(ctx, inst.InstallationID, repos)
	default:
		return nil
	}
}

func (s *Service) handlePullRequest(ctx context.Context, app *GitHubApp, inst *Installation, delivery string, p *WebhookPayload) error {
	repo := p.Repository.FullName
	if !inst.RepoAllowed(repo) {
		return nil
	}
	if p.ShouldSkipPullRequest() || p.AuthoredBy(app.Slug) {
		return nil
	}
	if p.Action == "closed" || p.Action == "converted_to_draft" {
		runs, err := s.repo.SupersedeActivePR(ctx, inst.InstallationID, repo, p.PullNumber())
		if err != nil {
			return err
		}
		s.cancelRuns(ctx, runs)
		return nil
	}
	if !p.IsRelevantPullRequestAction() {
		return nil
	}

	token, err := s.mintToken(ctx, app, inst, repo)
	if err != nil {
		return err
	}
	state, err := s.newClient(s.apiBaseURL, token).GetPullRequest(ctx, repo, p.PullNumber())
	if err != nil {
		return err
	}
	if state.Draft || state.Merged || (state.State != "" && state.State != "open") {
		return nil
	}

	claim, superseded, err := s.repo.ClaimReview(ctx, inst.InstallationID, repo, p.PullNumber(), state.HeadSHA, p.PullRequest.Head.SHA)
	if errors.Is(err, ErrStaleHead) {
		return nil
	}
	if err != nil {
		return err
	}
	s.cancelRuns(ctx, superseded)
	if claim.RunID != nil {
		return nil
	}

	message := fmt.Sprintf("Review pull request #%d %q in %s at %s", p.PullNumber(), state.Title, repo, state.HeadSHA)
	metadata := map[string]any{
		"subjectType":    "pull_request",
		"subjectRef":     fmt.Sprintf("%s#%d", repo, p.PullNumber()),
		"repo":           repo,
		"prNumber":       p.PullNumber(),
		"headSha":        state.HeadSHA,
		"baseSha":        p.PullRequest.Base.SHA,
		"baseRef":        state.BaseRef,
		"headRef":        state.HeadRef,
		"installationId": inst.InstallationID,
		"deliveryId":     delivery,
	}
	run, err := s.agents.CreateRunQueued(ctx, inst.AgentID.String(), 1, agents.CreateRunQueuedOptions{
		Queue:           defaultQueue(inst.Queue),
		TriggerMessage:  &message,
		TriggerMetadata: metadata,
		TrustedInternal: false,
	})
	if err != nil {
		return fmt.Errorf("githubapp: enqueue review run: %w", err)
	}
	runID, err := uuid.Parse(run.ID)
	if err != nil {
		return fmt.Errorf("githubapp: invalid run id: %w", err)
	}
	attached, err := s.repo.SetClaimRun(ctx, claim.ID, runID, ClaimRunning)
	if err != nil {
		return err
	}
	if !attached {
		_, _ = s.agents.RequestRunCancellation(ctx, run.ID)
	}
	return nil
}

func (s *Service) mintToken(ctx context.Context, app *GitHubApp, inst *Installation, repo string) (string, error) {
	key, err := s.cipher.Open(app.PrivateKeyEncrypted)
	if err != nil {
		return "", ErrMissingCredentials
	}
	return s.minter.MintInstallationToken(ctx, app.AppID, []byte(key), inst.InstallationID,
		[]string{repo},
		map[string]string{
			"contents":      "read",
			"pull_requests": "write",
			"issues":        "write",
			"checks":        "write",
		})
}

func (s *Service) cancelRuns(ctx context.Context, runIDs []uuid.UUID) {
	for _, id := range runIDs {
		if _, err := s.agents.RequestRunCancellation(ctx, id.String()); err != nil && s.log != nil {
			s.log.Warn("githubapp: failed to cancel superseded review run", "runId", id, "error", err)
		}
	}
}

func (s *Service) requireOrgAdmin(ctx context.Context, orgID uuid.UUID, userID string) error {
	ok, err := auth.CanAdministerOrg(ctx, s.repo.db, orgID.String(), userID)
	if err != nil {
		return apperror.NewInternal("failed to check org authority", err)
	}
	if !ok {
		return apperror.NewForbidden("org administrator required")
	}
	return nil
}

func defaultQueue(queue string) string {
	if strings.TrimSpace(queue) == "" {
		return "default"
	}
	return strings.TrimSpace(queue)
}

func normalizeRepos(repos []string) []string {
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

func applyRepoDelta(current []string, added, removed []WebhookRepo) []string {
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
	return out
}

func mapConflict(err error, message string) error {
	if err != nil && strings.Contains(err.Error(), "23505") {
		return apperror.NewConflict(message)
	}
	return apperror.NewInternal("database error", err)
}
