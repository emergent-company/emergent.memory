package sources

import (
	"context"
	"fmt"
	"strings"

	"github.com/google/uuid"

	"github.com/emergent-company/emergent.memory/pkg/apperror"
	"github.com/emergent-company/emergent.memory/pkg/auth"
)

// RegisterSourceAppRequest is the org-admin source App registration payload.
type RegisterSourceAppRequest struct {
	Provider      string
	AppID         int64
	Slug          string
	PrivateKey    string
	WebhookSecret string
}

// SourceTriggerInput is the create/update shape for a source trigger.
type SourceTriggerInput struct {
	EventType   string
	AgentID     string
	Queue       string
	EventFilter map[string]any
	Enabled     *bool
}

// bindGitHubAppSource validates the org App and installation and writes the
// connection columns on a new github_app source.
func (s *Service) bindGitHubAppSource(ctx context.Context, projectID uuid.UUID, src *Source, cfg map[string]any) error {
	appID, err := uuid.Parse(strings.TrimSpace(fmt.Sprint(cfg["app_id"])))
	if err != nil {
		return apperror.NewValidation("github_app: app_id must be the id of a registered source App")
	}
	app, err := s.repo.FindSourceAppByID(ctx, appID)
	if err != nil {
		return apperror.NewValidation("github_app: register the App under Organization → Apps first")
	}
	if app.Provider != "github" {
		return apperror.NewValidation("github_app: selected App is not a GitHub App")
	}
	projectOrg, err := s.repo.ProjectOrgID(ctx, projectID)
	if err != nil {
		return apperror.NewInternal("failed to resolve the project organization", err)
	}
	if app.OrgID != projectOrg {
		return apperror.NewForbidden("github_app: the App belongs to another organization")
	}
	installationID := strings.TrimSpace(fmt.Sprint(cfg["installation_id"]))
	if installationID == "" || installationID == "<nil>" {
		return apperror.NewValidation("github_app: installation_id is required")
	}
	src.AppID = &app.ID
	src.ExternalAccountID = &installationID
	return nil
}

// RegisterSourceApp stores org-level App credentials, sealed with the source
// encryption key.
func (s *Service) RegisterSourceApp(ctx context.Context, orgID uuid.UUID, userID string, req RegisterSourceAppRequest) (*SourceApp, error) {
	if err := s.requireSourceAppAdmin(ctx, orgID, userID); err != nil {
		return nil, err
	}
	provider := strings.TrimSpace(req.Provider)
	if provider == "" {
		provider = "github"
	}
	if req.AppID <= 0 || strings.TrimSpace(req.PrivateKey) == "" || strings.TrimSpace(req.WebhookSecret) == "" {
		return nil, apperror.NewValidation("appId, privateKey, and webhookSecret are required")
	}
	if len(s.key) == 0 {
		return nil, apperror.NewValidation("LLM_ENCRYPTION_KEY is not configured")
	}
	privateKey, err := SealSourceBlob(s.key, []byte(req.PrivateKey))
	if err != nil {
		return nil, apperror.NewInternal("failed to seal the App private key", err)
	}
	webhookSecret, err := SealSourceBlob(s.key, []byte(req.WebhookSecret))
	if err != nil {
		return nil, apperror.NewInternal("failed to seal the App webhook secret", err)
	}
	app := &SourceApp{
		OrgID:                  orgID,
		Provider:               provider,
		AppID:                  req.AppID,
		Slug:                   strings.TrimSpace(req.Slug),
		PrivateKeyEncrypted:    privateKey,
		WebhookSecretEncrypted: webhookSecret,
	}
	if err := s.repo.CreateSourceApp(ctx, app); err != nil {
		return nil, mapSourceConflict(err, "an App with this id is already registered for the organization")
	}
	return app, nil
}

// ListSourceApps returns the org's Apps without sealed credentials.
func (s *Service) ListSourceApps(ctx context.Context, orgID uuid.UUID, userID string) ([]*SourceApp, error) {
	if err := s.requireSourceAppAdmin(ctx, orgID, userID); err != nil {
		return nil, err
	}
	return s.repo.ListSourceAppsByOrg(ctx, orgID)
}

// DeleteSourceApp removes an App and its installations (FK cascade).
func (s *Service) DeleteSourceApp(ctx context.Context, orgID uuid.UUID, userID string, id uuid.UUID) error {
	if err := s.requireSourceAppAdmin(ctx, orgID, userID); err != nil {
		return err
	}
	app, err := s.repo.FindSourceAppByID(ctx, id)
	if err != nil || app.OrgID != orgID {
		return apperror.NewNotFound("SourceApp", id.String())
	}
	return s.repo.DeleteSourceApp(ctx, id)
}

// ListSourceTriggers returns a source's trigger bindings.
func (s *Service) ListSourceTriggers(ctx context.Context, projectID, sourceID uuid.UUID) ([]*SourceTrigger, error) {
	if _, err := s.sourceForProject(ctx, projectID, sourceID); err != nil {
		return nil, err
	}
	return s.repo.ListSourceTriggers(ctx, sourceID)
}

// CreateSourceTrigger binds an event type to a runtime agent and queue.
func (s *Service) CreateSourceTrigger(ctx context.Context, projectID, sourceID uuid.UUID, in SourceTriggerInput) (*SourceTrigger, error) {
	src, err := s.sourceForProject(ctx, projectID, sourceID)
	if err != nil {
		return nil, err
	}
	if err := s.validateTriggerInput(ctx, projectID, src, in, true); err != nil {
		return nil, err
	}
	enabled := true
	if in.Enabled != nil {
		enabled = *in.Enabled
	}
	trigger := &SourceTrigger{
		ProjectID:   projectID,
		SourceID:    sourceID,
		EventType:   strings.TrimSpace(in.EventType),
		AgentID:     mustUUID(in.AgentID),
		Queue:       defaultSourceQueue(in.Queue),
		EventFilter: in.EventFilter,
		Enabled:     enabled,
	}
	if trigger.EventFilter == nil {
		trigger.EventFilter = map[string]any{}
	}
	if err := s.repo.CreateSourceTrigger(ctx, trigger); err != nil {
		return nil, mapSourceConflict(err, "this source already has a trigger for that event")
	}
	return trigger, nil
}

// UpdateSourceTrigger patches an existing binding.
func (s *Service) UpdateSourceTrigger(ctx context.Context, projectID, sourceID, triggerID uuid.UUID, in SourceTriggerInput) (*SourceTrigger, error) {
	src, err := s.sourceForProject(ctx, projectID, sourceID)
	if err != nil {
		return nil, err
	}
	trigger, err := s.repo.FindSourceTriggerByID(ctx, triggerID)
	if err != nil || trigger.SourceID != sourceID {
		return nil, apperror.NewNotFound("SourceTrigger", triggerID.String())
	}
	if in.EventType != "" {
		in.EventType = strings.TrimSpace(in.EventType)
	}
	if err := s.validateTriggerInput(ctx, projectID, src, in, false); err != nil {
		return nil, err
	}
	if in.EventType != "" {
		trigger.EventType = in.EventType
	}
	if in.AgentID != "" {
		trigger.AgentID = mustUUID(in.AgentID)
	}
	if in.Queue != "" {
		trigger.Queue = defaultSourceQueue(in.Queue)
	}
	if in.EventFilter != nil {
		trigger.EventFilter = in.EventFilter
	}
	if in.Enabled != nil {
		trigger.Enabled = *in.Enabled
	}
	if err := s.repo.UpdateSourceTrigger(ctx, trigger); err != nil {
		return nil, mapSourceConflict(err, "this source already has a trigger for that event")
	}
	return trigger, nil
}

// DeleteSourceTrigger removes a binding.
func (s *Service) DeleteSourceTrigger(ctx context.Context, projectID, sourceID, triggerID uuid.UUID) error {
	if _, err := s.sourceForProject(ctx, projectID, sourceID); err != nil {
		return err
	}
	trigger, err := s.repo.FindSourceTriggerByID(ctx, triggerID)
	if err != nil || trigger.SourceID != sourceID {
		return apperror.NewNotFound("SourceTrigger", triggerID.String())
	}
	return s.repo.DeleteSourceTrigger(ctx, triggerID)
}

func (s *Service) validateTriggerInput(ctx context.Context, projectID uuid.UUID, src *Source, in SourceTriggerInput, requireAll bool) error {
	eventType := strings.TrimSpace(in.EventType)
	if eventType == "" && requireAll {
		return apperror.NewValidation("eventType is required")
	}
	if eventType != "" && src.Provider == ProviderTypeGitHubApp && eventType != "pull_request" {
		return apperror.NewValidation("github_app supports only the pull_request event")
	}
	if strings.TrimSpace(in.AgentID) == "" {
		if requireAll {
			return apperror.NewValidation("agentId is required")
		}
		return nil
	}
	agentID, err := uuid.Parse(in.AgentID)
	if err != nil {
		return apperror.NewValidation("agentId must be a UUID")
	}
	if s.agentRepo == nil {
		return apperror.NewInternal("agent resolver is not configured", nil)
	}
	projectIDStr := projectID.String()
	agent, err := s.agentRepo.FindByID(ctx, agentID.String(), &projectIDStr)
	if err != nil || agent == nil {
		return apperror.NewNotFound("Agent", in.AgentID)
	}
	return nil
}

func (s *Service) sourceForProject(ctx context.Context, projectID, sourceID uuid.UUID) (*Source, error) {
	src, err := s.repo.FindSourceByID(ctx, sourceID)
	if err != nil || src.ProjectID != projectID {
		return nil, apperror.NewNotFound("Integration", sourceID.String())
	}
	return src, nil
}

func (s *Service) requireSourceAppAdmin(ctx context.Context, orgID uuid.UUID, userID string) error {
	ok, err := auth.CanAdministerOrg(ctx, s.repo.db, orgID.String(), userID)
	if err != nil {
		return apperror.NewInternal("failed to check org authority", err)
	}
	if !ok {
		return apperror.NewForbidden("org administrator required")
	}
	return nil
}

func mustUUID(raw string) uuid.UUID {
	id, _ := uuid.Parse(strings.TrimSpace(raw))
	return id
}

func mapSourceConflict(err error, message string) error {
	if err != nil && strings.Contains(err.Error(), "23505") {
		return apperror.NewConflict(message)
	}
	return apperror.NewInternal("database error", err)
}
