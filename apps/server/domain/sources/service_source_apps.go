package sources

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/google/uuid"

	"github.com/emergent-company/emergent.memory/domain/githubapp"
	"github.com/emergent-company/emergent.memory/pkg/apperror"
	"github.com/emergent-company/emergent.memory/pkg/auth"
	"github.com/emergent-company/emergent.memory/pkg/pgutils"
)

// SourceAppProviderGitHub is the only App provider the platform serves today.
const SourceAppProviderGitHub = "github"

// sourceAppProviders is the single list of App providers registration accepts
// and bindGitHubAppSource can bind. Both sides consult it so a provider cannot
// be accepted at registration and then refused at bind time, leaving a row that
// occupies the unique (org_id, provider, app_id) index forever.
var sourceAppProviders = map[string]struct{}{
	SourceAppProviderGitHub: {},
}

// SourceAppProviderSupported reports whether provider is served. Matching is
// exact: a typo or a different case is rejected, never folded into a supported
// provider, because the stored value is what a later bind must match.
func SourceAppProviderSupported(provider string) bool {
	_, ok := sourceAppProviders[provider]
	return ok
}

// RegisterSourceAppRequest is the org-admin source App registration payload.
type RegisterSourceAppRequest struct {
	Provider      string
	AppID         int64
	Slug          string
	PrivateKey    string
	WebhookSecret string
	// WebhookKey pins the delivery key before the App is registered (the
	// manifest wizard needs the URL at App-creation time). Empty lets the DB
	// generate one.
	WebhookKey string
}

// UpdateSourceAppRequest is the org-admin App edit payload. The provider and the
// GitHub App id are absent on purpose: `(org, provider, app_id)` is what binds
// project sources to this credential, so an edit renames the App's label and
// optionally rotates its sealed secrets instead of repointing it. A blank secret
// keeps the stored one — the sealed columns are write-only, so there is nothing
// the UI could prefill or the caller could round-trip.
type UpdateSourceAppRequest struct {
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
	// Membership in the shared set, not a literal: registration accepts exactly
	// what this binder accepts, so a provider cannot create a row that is
	// un-bindable forever. Today that is only SourceAppProviderGitHub; a provider
	// added to the set must be reviewed here too.
	if !SourceAppProviderSupported(app.Provider) {
		return apperror.NewValidation("github_app: selected App is not a GitHub App")
	}
	projectOrg, err := s.repo.ProjectOrgID(ctx, projectID)
	if err != nil {
		return apperror.NewInternal("failed to resolve the project organization", err)
	}
	if app.OrgID != projectOrg {
		return apperror.NewForbidden("github_app: the App belongs to another organization")
	}
	if app.ProjectID != nil && *app.ProjectID != projectID {
		return apperror.NewForbidden("github_app: the App belongs to another project")
	}
	installationID, err := normalizeInstallationID(cfg["installation_id"])
	if err != nil {
		return apperror.NewValidation("github_app: " + err.Error())
	}
	src.AppID = &app.ID
	src.ExternalAccountID = &installationID
	return nil
}

// RegisterSourceApp stores org-wide App credentials (org admin; usable by
// every project in the org).
func (s *Service) RegisterSourceApp(ctx context.Context, orgID uuid.UUID, userID string, req RegisterSourceAppRequest) (*SourceApp, error) {
	if err := s.requireSourceAppAdmin(ctx, orgID, userID); err != nil {
		return nil, err
	}
	return s.createSourceApp(ctx, orgID, nil, req)
}

// RegisterProjectSourceApp registers an App from the project surface. An org
// admin may choose org-wide (usable by every project) or project-owned; a
// project admin may create only a project-owned App.
func (s *Service) RegisterProjectSourceApp(ctx context.Context, projectID uuid.UUID, userID string, req RegisterSourceAppRequest, orgWide bool) (*SourceApp, error) {
	orgID, err := s.repo.ProjectOrgID(ctx, projectID)
	if err != nil {
		return nil, apperror.NewInternal("failed to resolve the project organization", err)
	}
	if orgWide {
		if err := s.requireSourceAppAdmin(ctx, orgID, userID); err != nil {
			return nil, err
		}
		return s.createSourceApp(ctx, orgID, nil, req)
	}
	if err := s.requireSourceAppProjectAdmin(ctx, projectID, userID); err != nil {
		return nil, err
	}
	pid := projectID
	return s.createSourceApp(ctx, orgID, &pid, req)
}

func (s *Service) createSourceApp(ctx context.Context, orgID uuid.UUID, projectID *uuid.UUID, req RegisterSourceAppRequest) (*SourceApp, error) {
	provider := strings.TrimSpace(req.Provider)
	if provider == "" {
		provider = SourceAppProviderGitHub
	}
	if req.AppID <= 0 || strings.TrimSpace(req.PrivateKey) == "" || strings.TrimSpace(req.WebhookSecret) == "" {
		return nil, apperror.NewBadRequest("appId, privateKey, and webhookSecret are required")
	}
	if !SourceAppProviderSupported(provider) {
		return nil, apperror.NewBadRequest(fmt.Sprintf(
			"provider %q is not supported; the only App provider served is %q", provider, SourceAppProviderGitHub))
	}
	if err := validateSourceAppPrivateKey(req.PrivateKey); err != nil {
		return nil, err
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
		ProjectID:              projectID,
		Provider:               provider,
		AppID:                  req.AppID,
		Slug:                   strings.TrimSpace(req.Slug),
		PrivateKeyEncrypted:    privateKey,
		WebhookSecretEncrypted: webhookSecret,
	}
	if raw := strings.TrimSpace(req.WebhookKey); raw != "" {
		key, err := uuid.Parse(raw)
		if err != nil {
			return nil, apperror.NewBadRequest("webhookKey must be a UUID")
		}
		app.WebhookKey = key
	}
	if err := s.repo.CreateSourceApp(ctx, app); err != nil {
		return nil, mapSourceConflict(err, "an App with this id is already registered for the organization")
	}
	return app, nil
}

// validateSourceAppPrivateKey rejects a credential that cannot mint an App JWT,
// naming the field and the reason. It shares githubapp.ParseRSAPrivateKey with
// the JWT minter, so registration accepts exactly what the dispatcher can use.
// No branch echoes key bytes; the parse detail is an x509/asn1 description of
// the DER structure only.
func validateSourceAppPrivateKey(raw string) error {
	_, err := githubapp.ParseRSAPrivateKey([]byte(raw))
	switch {
	case err == nil:
		return nil
	case errors.Is(err, githubapp.ErrPrivateKeyNotPEM):
		return apperror.NewBadRequest("privateKey is not PEM encoded; paste the whole PEM file, beginning with -----BEGIN RSA PRIVATE KEY----- or -----BEGIN PRIVATE KEY-----")
	case errors.Is(err, githubapp.ErrPrivateKeyUnsupportedPEMType):
		return apperror.NewBadRequest("privateKey has an unsupported PEM block type; only \"RSA PRIVATE KEY\" (PKCS#1) and \"PRIVATE KEY\" (PKCS#8) are accepted")
	case errors.Is(err, githubapp.ErrPrivateKeyNotRSA):
		return apperror.NewBadRequest("privateKey is not an RSA key; GitHub App private keys must be RSA")
	case errors.Is(err, githubapp.ErrPrivateKeyTooWeak):
		var weak *githubapp.WeakPrivateKeyError
		if errors.As(err, &weak) {
			return apperror.NewBadRequest(fmt.Sprintf(
				"privateKey is too weak; the RSA modulus must be at least %d bits, but this key is %d bits",
				githubapp.MinRSAPrivateKeyBits, weak.Bits))
		}
		return apperror.NewBadRequest(fmt.Sprintf(
			"privateKey is too weak; the RSA modulus must be at least %d bits", githubapp.MinRSAPrivateKeyBits))
	default:
		return apperror.NewBadRequest(fmt.Sprintf("privateKey could not be parsed as a PKCS#1 or PKCS#8 RSA private key: %v", err))
	}
}

// ListSourceApps returns the org-wide Apps without sealed credentials.
func (s *Service) ListSourceApps(ctx context.Context, orgID uuid.UUID, userID string) ([]*SourceApp, error) {
	if err := s.requireSourceAppAdmin(ctx, orgID, userID); err != nil {
		return nil, err
	}
	return s.repo.ListSourceAppsByOrg(ctx, orgID)
}

// ListSourceAppsForProject returns the Apps a project may bind: org-wide Apps
// plus Apps owned by that project. Project membership is enforced by the route.
func (s *Service) ListSourceAppsForProject(ctx context.Context, projectID uuid.UUID) ([]*SourceApp, error) {
	orgID, err := s.repo.ProjectOrgID(ctx, projectID)
	if err != nil {
		return nil, apperror.NewInternal("failed to resolve the project organization", err)
	}
	return s.repo.ListSourceAppsForProject(ctx, orgID, projectID)
}

// DeleteSourceApp removes an org-wide App credential (org admin).
func (s *Service) DeleteSourceApp(ctx context.Context, orgID uuid.UUID, userID string, id uuid.UUID) error {
	if err := s.requireSourceAppAdmin(ctx, orgID, userID); err != nil {
		return err
	}
	app, err := s.repo.FindSourceAppByID(ctx, id)
	if err != nil || app.OrgID != orgID || app.ProjectID != nil {
		return apperror.NewNotFound("SourceApp", id.String())
	}
	return s.repo.DeleteSourceApp(ctx, id)
}

// DeleteSourceAppForProject removes an App from the project surface. A
// project-owned App requires project admin; an org-wide App requires org admin.
func (s *Service) DeleteSourceAppForProject(ctx context.Context, projectID uuid.UUID, userID string, id uuid.UUID) error {
	app, err := s.repo.FindSourceAppByID(ctx, id)
	if err != nil {
		return apperror.NewNotFound("SourceApp", id.String())
	}
	if err := s.authorizeSourceAppManage(ctx, projectID, app, userID); err != nil {
		return err
	}
	return s.repo.DeleteSourceApp(ctx, id)
}

// UpdateSourceApp renames an org-wide App and optionally rotates its secrets.
func (s *Service) UpdateSourceApp(ctx context.Context, orgID uuid.UUID, userID string, id uuid.UUID, req UpdateSourceAppRequest) (*SourceApp, error) {
	if err := s.requireSourceAppAdmin(ctx, orgID, userID); err != nil {
		return nil, err
	}
	app, err := s.repo.FindSourceAppByID(ctx, id)
	if err != nil || app.OrgID != orgID || app.ProjectID != nil {
		return nil, apperror.NewNotFound("SourceApp", id.String())
	}
	return s.applySourceAppUpdate(ctx, app, req)
}

// UpdateSourceAppForProject edits an App from the project surface.
func (s *Service) UpdateSourceAppForProject(ctx context.Context, projectID uuid.UUID, userID string, id uuid.UUID, req UpdateSourceAppRequest) (*SourceApp, error) {
	app, err := s.repo.FindSourceAppByID(ctx, id)
	if err != nil {
		return nil, apperror.NewNotFound("SourceApp", id.String())
	}
	if err := s.authorizeSourceAppManage(ctx, projectID, app, userID); err != nil {
		return nil, err
	}
	return s.applySourceAppUpdate(ctx, app, req)
}

func (s *Service) applySourceAppUpdate(ctx context.Context, app *SourceApp, req UpdateSourceAppRequest) (*SourceApp, error) {
	slug := strings.TrimSpace(req.Slug)
	if slug == "" {
		return nil, apperror.NewBadRequest("slug is required")
	}
	app.Slug = slug
	if strings.TrimSpace(req.PrivateKey) != "" {
		if err := validateSourceAppPrivateKey(req.PrivateKey); err != nil {
			return nil, err
		}
		rotated, err := s.sealSourceAppSecret(req.PrivateKey, "the App private key")
		if err != nil {
			return nil, err
		}
		app.PrivateKeyEncrypted = rotated
	}
	if strings.TrimSpace(req.WebhookSecret) != "" {
		rotated, err := s.sealSourceAppSecret(req.WebhookSecret, "the App webhook secret")
		if err != nil {
			return nil, err
		}
		app.WebhookSecretEncrypted = rotated
	}
	if err := s.repo.UpdateSourceApp(ctx, app); err != nil {
		return nil, err
	}
	return app, nil
}

// authorizeSourceAppManage: a project-owned App requires project admin; an
// org-wide App requires org admin. A mismatched project is not found.
func (s *Service) authorizeSourceAppManage(ctx context.Context, projectID uuid.UUID, app *SourceApp, userID string) error {
	orgID, err := s.repo.ProjectOrgID(ctx, projectID)
	if err != nil {
		return apperror.NewInternal("failed to resolve the project organization", err)
	}
	if app.OrgID != orgID {
		return apperror.NewNotFound("SourceApp", app.ID.String())
	}
	if app.ProjectID == nil {
		return s.requireSourceAppAdmin(ctx, app.OrgID, userID)
	}
	if *app.ProjectID != projectID {
		return apperror.NewNotFound("SourceApp", app.ID.String())
	}
	return s.requireSourceAppProjectAdmin(ctx, projectID, userID)
}

func (s *Service) requireSourceAppProjectAdmin(ctx context.Context, projectID uuid.UUID, userID string) error {
	ok, err := auth.CanAdministerProject(ctx, s.repo.db, projectID.String(), userID)
	if err != nil {
		return apperror.NewInternal("failed to check project authority", err)
	}
	if !ok {
		return apperror.NewForbidden("project administrator required")
	}
	return nil
}

// sealSourceAppSecret seals one credential value with the source encryption key.
// what names the secret in the failure so a rotation error identifies the field.
func (s *Service) sealSourceAppSecret(raw, what string) ([]byte, error) {
	if len(s.key) == 0 {
		return nil, apperror.NewValidation("LLM_ENCRYPTION_KEY is not configured")
	}
	sealed, err := SealSourceBlob(s.key, []byte(raw))
	if err != nil {
		return nil, apperror.NewInternal("failed to seal "+what, err)
	}
	return sealed, nil
}

// ListSourceTriggers returns a source's trigger bindings.
func (s *Service) ListSourceTriggers(ctx context.Context, projectID, sourceID uuid.UUID) ([]*SourceTrigger, error) {
	if _, err := s.sourceForProject(ctx, projectID, sourceID); err != nil {
		return nil, err
	}
	triggers, err := s.repo.ListSourceTriggers(ctx, projectID, sourceID)
	if err != nil {
		return nil, err
	}
	s.fillTriggerAgentNames(ctx, projectID, triggers)
	return triggers, nil
}

// fillTriggerAgentNames labels a list of bindings with the bound agents' names in
// one lookup. The view cannot resolve them from the picker's list: the picker
// offers agent definitions while the binding stores a runtime kb.agents id
// (#727). Labels are cosmetic, so a failed lookup leaves them empty rather than
// failing the read.
func (s *Service) fillTriggerAgentNames(ctx context.Context, projectID uuid.UUID, triggers []*SourceTrigger) {
	if s.agentRepo == nil || len(triggers) == 0 {
		return
	}
	ids := make([]string, 0, len(triggers))
	for _, trigger := range triggers {
		if trigger != nil && trigger.AgentID != uuid.Nil {
			ids = append(ids, trigger.AgentID.String())
		}
	}
	if len(ids) == 0 {
		return
	}
	names, err := s.agentRepo.FindAgentNamesByIDs(ctx, projectID.String(), ids)
	if err != nil {
		return
	}
	for _, trigger := range triggers {
		if trigger != nil {
			trigger.AgentName = names[trigger.AgentID.String()]
		}
	}
}

// CreateSourceTrigger binds an event type to a runtime agent and queue.
func (s *Service) CreateSourceTrigger(ctx context.Context, projectID, sourceID uuid.UUID, in SourceTriggerInput) (*SourceTrigger, error) {
	src, err := s.sourceForProject(ctx, projectID, sourceID)
	if err != nil {
		return nil, err
	}
	// Check the unique (source, event type) conflict before resolving the agent:
	// resolution materializes a definition-backed kb.agents row, which a binding
	// that cannot be created would otherwise leave orphaned. The unique index
	// still guards the race below.
	if eventType := strings.TrimSpace(in.EventType); eventType != "" {
		existing, lerr := s.repo.ListSourceTriggers(ctx, projectID, sourceID)
		if lerr != nil {
			return nil, lerr
		}
		for _, t := range existing {
			if t != nil && strings.EqualFold(strings.TrimSpace(t.EventType), eventType) {
				return nil, apperror.NewConflict("this source already has a trigger for that event")
			}
		}
	}
	agentID, err := s.validateTriggerInput(ctx, projectID, src, in, true)
	if err != nil {
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
		AgentID:     mustUUID(agentID),
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
	trigger.AgentName = s.triggerAgentName(ctx, projectID, trigger.AgentID)
	return trigger, nil
}

// UpdateSourceTrigger patches an existing binding.
func (s *Service) UpdateSourceTrigger(ctx context.Context, projectID, sourceID, triggerID uuid.UUID, in SourceTriggerInput) (*SourceTrigger, error) {
	src, err := s.sourceForProject(ctx, projectID, sourceID)
	if err != nil {
		return nil, err
	}
	trigger, err := s.repo.FindSourceTriggerByID(ctx, projectID, triggerID)
	if err != nil || trigger.SourceID != sourceID {
		return nil, apperror.NewNotFound("SourceTrigger", triggerID.String())
	}
	if in.EventType != "" {
		in.EventType = strings.TrimSpace(in.EventType)
	}
	agentID, err := s.validateTriggerInput(ctx, projectID, src, in, false)
	if err != nil {
		return nil, err
	}
	if in.EventType != "" {
		trigger.EventType = in.EventType
	}
	if agentID != "" {
		trigger.AgentID = mustUUID(agentID)
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
	trigger.AgentName = s.triggerAgentName(ctx, projectID, trigger.AgentID)
	return trigger, nil
}

// DeleteSourceTrigger removes a binding.
func (s *Service) DeleteSourceTrigger(ctx context.Context, projectID, sourceID, triggerID uuid.UUID) error {
	if _, err := s.sourceForProject(ctx, projectID, sourceID); err != nil {
		return err
	}
	trigger, err := s.repo.FindSourceTriggerByID(ctx, projectID, triggerID)
	if err != nil || trigger.SourceID != sourceID {
		return apperror.NewNotFound("SourceTrigger", triggerID.String())
	}
	return s.repo.DeleteSourceTrigger(ctx, projectID, triggerID)
}

// validateTriggerInput validates a binding and returns the runtime agent id it
// must store ("" when the patch carries no agentId). The resolved id is not
// necessarily the input: the trigger picker offers agent definitions
// (ListAgentDefinitions), so an id naming a definition resolves to that
// definition's kb.agents row — the id kb.source_triggers.agent_id references and
// the id dispatch enqueues a run for (#727).
func (s *Service) validateTriggerInput(ctx context.Context, projectID uuid.UUID, src *Source, in SourceTriggerInput, requireAll bool) (string, error) {
	eventType := strings.TrimSpace(in.EventType)
	if eventType == "" && requireAll {
		return "", apperror.NewValidation("eventType is required")
	}
	if eventType != "" && src.Provider == ProviderTypeGitHubApp && eventType != "pull_request" {
		return "", apperror.NewValidation("github_app supports only the pull_request event")
	}
	if strings.TrimSpace(in.AgentID) == "" {
		if requireAll {
			return "", apperror.NewValidation("agentId is required")
		}
		return "", nil
	}
	return s.resolveTriggerAgentID(ctx, projectID, in.AgentID)
}

// resolveTriggerAgentID maps an id supplied by the trigger picker to a runtime
// kb.agents id. It accepts either a runtime agent id or an agent-definition id,
// and creates the definition-backed runtime row when a definition-only agent
// (one created from the UI) is bound for the first time.
func (s *Service) resolveTriggerAgentID(ctx context.Context, projectID uuid.UUID, raw string) (string, error) {
	agentID, err := uuid.Parse(strings.TrimSpace(raw))
	if err != nil {
		return "", apperror.NewValidation("agentId must be a UUID")
	}
	if s.agentRepo == nil {
		return "", apperror.NewInternal("agent resolver is not configured", nil)
	}
	projectIDStr := projectID.String()
	agent, err := s.agentRepo.ResolveRuntimeAgentByID(ctx, projectIDStr, agentID.String())
	if err != nil {
		return "", apperror.NewInternal("failed to resolve agent", err)
	}
	if agent == nil {
		return "", apperror.NewNotFound("Agent", raw)
	}
	return agent.ID, nil
}

// triggerAgentName resolves the display name of a bound agent for the trigger
// panel. The view cannot resolve it from the picker's list: the picker offers
// agent definitions while the binding stores a runtime kb.agents id (#727).
func (s *Service) triggerAgentName(ctx context.Context, projectID uuid.UUID, agentID uuid.UUID) string {
	if s.agentRepo == nil || agentID == uuid.Nil {
		return ""
	}
	projectIDStr := projectID.String()
	agent, err := s.agentRepo.FindByID(ctx, agentID.String(), &projectIDStr)
	if err != nil || agent == nil {
		return ""
	}
	return agent.Name
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
	if pgutils.IsUniqueViolation(err) {
		return apperror.NewConflict(message)
	}
	return apperror.NewInternal("database error", err)
}
