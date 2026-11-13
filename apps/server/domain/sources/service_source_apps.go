package sources

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/google/uuid"

	"github.com/emergent-company/emergent.memory/domain/agents"
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
	EventType string
	// AgentID is an agent definition id or a runtime agent id; the service
	// stores the kb.agents id the runs execute as.
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
	return s.repo.ListSourceTriggers(ctx, projectID, sourceID)
}

// CreateSourceTrigger binds an event type to the agent its runs execute as.
func (s *Service) CreateSourceTrigger(ctx context.Context, projectID, sourceID uuid.UUID, in SourceTriggerInput) (*SourceTrigger, error) {
	src, err := s.sourceForProject(ctx, projectID, sourceID)
	if err != nil {
		return nil, err
	}
	agentID, err := s.resolveTriggerAgent(ctx, projectID, src, in, true)
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
		AgentID:     agentID,
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
	trigger, err := s.repo.FindSourceTriggerByID(ctx, projectID, triggerID)
	if err != nil || trigger.SourceID != sourceID {
		return nil, apperror.NewNotFound("SourceTrigger", triggerID.String())
	}
	if in.EventType != "" {
		in.EventType = strings.TrimSpace(in.EventType)
	}
	agentID, err := s.resolveTriggerAgent(ctx, projectID, src, in, false)
	if err != nil {
		return nil, err
	}
	if in.EventType != "" {
		trigger.EventType = in.EventType
	}
	if strings.TrimSpace(in.AgentID) != "" {
		trigger.AgentID = agentID
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
	trigger, err := s.repo.FindSourceTriggerByID(ctx, projectID, triggerID)
	if err != nil || trigger.SourceID != sourceID {
		return apperror.NewNotFound("SourceTrigger", triggerID.String())
	}
	return s.repo.DeleteSourceTrigger(ctx, projectID, triggerID)
}

// resolveTriggerAgent validates the trigger's agent and returns the id to store
// on the binding: always a kb.agents id, never the definition id the UI picker
// submits. kb.source_triggers.agent_id and kb.agent_runs.agent_id are foreign
// keys into kb.agents, so a definition id cannot be stored and the dispatcher
// could not enqueue a run for it.
func (s *Service) resolveTriggerAgent(ctx context.Context, projectID uuid.UUID, src *Source, in SourceTriggerInput, requireAll bool) (uuid.UUID, error) {
	eventType := strings.TrimSpace(in.EventType)
	if eventType == "" && requireAll {
		return uuid.Nil, apperror.NewValidation("eventType is required")
	}
	if eventType != "" && src.Provider == ProviderTypeGitHubApp && eventType != "pull_request" {
		return uuid.Nil, apperror.NewValidation("github_app supports only the pull_request event")
	}
	if strings.TrimSpace(in.AgentID) == "" {
		if requireAll {
			return uuid.Nil, apperror.NewValidation("agentId is required")
		}
		return uuid.Nil, nil
	}
	agentID, err := uuid.Parse(strings.TrimSpace(in.AgentID))
	if err != nil {
		return uuid.Nil, apperror.NewValidation("agentId must be a UUID")
	}
	if s.agentRepo == nil {
		return uuid.Nil, apperror.NewInternal("agent resolver is not configured", nil)
	}
	projectIDStr := projectID.String()
	// A runtime agent id binds as-is: triggers created before definition-backed
	// agents existed, and API callers that already hold a kb.agents id, keep
	// working.
	agent, err := s.agentRepo.FindByID(ctx, agentID.String(), &projectIDStr)
	if err != nil {
		return uuid.Nil, apperror.NewInternal("failed to resolve the agent", err)
	}
	if agent != nil {
		return agentID, nil
	}
	// The panel's picker lists agent definitions: resolve one to the runtime
	// agent its runs execute as, creating that row on first bind.
	def, err := s.agentRepo.FindDefinitionByID(ctx, agentID.String(), &projectIDStr)
	if err != nil {
		return uuid.Nil, apperror.NewInternal("failed to resolve the agent", err)
	}
	if def == nil {
		return uuid.Nil, apperror.NewNotFound("Agent", strings.TrimSpace(in.AgentID))
	}
	// A disabled definition's runs fail as "agent is disabled" in the worker, so
	// accepting the bind would leave a binding that queues work it can never run.
	if !def.Enabled {
		return uuid.Nil, apperror.NewValidation("agent " + def.Name + " is disabled; enable it before binding a trigger")
	}
	runtime, err := s.ensureDefinitionRuntimeAgent(ctx, def)
	if err != nil {
		return uuid.Nil, err
	}
	resolved, err := uuid.Parse(runtime.ID)
	if err != nil {
		return uuid.Nil, apperror.NewInternal("agent runtime row has an invalid id", err)
	}
	return resolved, nil
}

// ensureDefinitionRuntimeAgent returns the kb.agents row a definition's runs
// execute as, creating it on first use. The platform does the same lazily for
// object work rules (agents.Handler.syncDefinitionWorkTriggers) and A2A calls
// (agents.A2AHandler.resolveRuntimeAgent), so a definition can be bound before
// any of them has needed a runtime row.
func (s *Service) ensureDefinitionRuntimeAgent(ctx context.Context, def *agents.AgentDefinition) (*agents.Agent, error) {
	existing, err := s.agentRepo.FindAgentsByDefinitionID(ctx, def.ID)
	if err != nil {
		return nil, apperror.NewInternal("failed to resolve the agent runtime row", err)
	}
	// The link is not project-checked on write (the agents API accepts any
	// agentDefinitionId), so skip a row that belongs to another project: binding
	// it would enqueue this source's runs inside that project.
	for _, agent := range existing {
		if agent.ProjectID == def.ProjectID {
			return agent, nil
		}
	}
	// A row holding the definition's own strategy type but no link (an older
	// codepath that created one without writing agent_definition_id) is the only
	// same-name row worth adopting. A same-name row of any other shape is a
	// distinct agent — a Schedules-page agent, an API-created agent — whose own
	// runs must keep their prompt and model, so the bind gets its own row rather
	// than stamping that one and re-pointing its config at this definition.
	legacy, err := s.agentRepo.FindAgentByName(ctx, def.ProjectID, def.Name)
	if err != nil {
		return nil, apperror.NewInternal("failed to resolve the agent runtime row", err)
	}
	if legacy != nil && legacy.AgentDefinitionID == nil && legacy.StrategyType == agentDefStrategyType(def.ID) {
		defID := def.ID
		legacy.AgentDefinitionID = &defID
		if err := s.agentRepo.UpdateAgent(ctx, legacy); err != nil {
			return nil, apperror.NewInternal("failed to link the agent runtime row", err)
		}
		return legacy, nil
	}
	defID := def.ID
	created := &agents.Agent{
		ProjectID:         def.ProjectID,
		Name:              def.Name,
		StrategyType:      agentDefStrategyType(def.ID),
		CronSchedule:      "0 0 * * *",
		Enabled:           def.Enabled,
		TriggerType:       agents.TriggerTypeManual,
		AgentDefinitionID: &defID,
	}
	if err := s.agentRepo.CreateAgent(ctx, created); err != nil {
		return nil, apperror.NewInternal("failed to create the agent runtime row", err)
	}
	return created, nil
}

// agentDefStrategyType is the strategy type of the runtime row a definition's
// runs execute as. agents.ResolveDefinitionForAgent parses it when the row has
// no agent_definition_id.
func agentDefStrategyType(defID string) string { return "agent-def:" + defID }

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

func mapSourceConflict(err error, message string) error {
	if pgutils.IsUniqueViolation(err) {
		return apperror.NewConflict(message)
	}
	return apperror.NewInternal("database error", err)
}
