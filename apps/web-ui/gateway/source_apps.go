package main

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/labstack/echo/v4"
)

// --- org source Apps (GitHub App credentials) ---
//
// A source App is an org-wide GitHub App registration: one App backs many
// project sources, and each github_app source binds one App plus an
// installation. The register/list/delete API is org-admin only, so the page
// renders an explicit forbidden state for everyone else rather than an empty
// list (see uiSourceApps).

// dsProviderTypeGitHubApp is the trigger-only GitHub App provider. Its sources
// carry an org App id in their config and accept pull_request triggers.
const dsProviderTypeGitHubApp = "github_app"

// dsSourceAppsPath is the org source-App settings page (project settings rail).
const dsSourceAppsPath = "/settings/source-apps"

// dsSourceAppsFieldKey is the github_app config key that names an org App. The
// connect/edit form renders it as a select of the org's Apps instead of the
// provider schema's raw text input; the schema itself stays provider-neutral.
const dsSourceAppsFieldKey = "app_id"

// dsSourceAppRow is one registered App as the settings page renders it: the API
// shape plus the webhook URL the provider must be configured with.
type dsSourceAppRow struct {
	SourceApp
	WebhookURL string
}

// dsSourceAppsPageData is the payload for SourceAppsPage.
type dsSourceAppsPageData struct {
	settingsNavCommon
	OrgID   string
	OrgName string
	// Forbidden renders the "organization administrators only" state instead of
	// the list and the register form (the API would reject those writes).
	Forbidden bool
	Rows      []dsSourceAppRow
	LoadErr   error
	FlashMsg  string
	FlashErr  error
}

// dsSourceAppWebhookURL builds the endpoint GitHub must deliver to for one App.
// It points at the memory API (not the gateway): base is the deployment's public
// memory URL, and webhookKey is the App's unguessable delivery key.
func dsSourceAppWebhookURL(base, webhookKey string) string {
	if webhookKey == "" {
		return ""
	}
	path := "/api/webhooks/source-apps/" + url.PathEscape(webhookKey)
	return strings.TrimRight(base, "/") + path
}

// dsSourceAppLabel is the App's option/row label: its slug plus the numeric
// GitHub App id, which is what a maintainer matches against GitHub's settings.
func dsSourceAppLabel(app SourceApp) string {
	slug := strings.TrimSpace(app.Slug)
	switch {
	case slug != "" && app.AppID > 0:
		return slug + " (" + strconv.FormatInt(app.AppID, 10) + ")"
	case app.AppID > 0:
		return strconv.FormatInt(app.AppID, 10)
	}
	return slug
}

// dsSourceAppRows decorates the API rows with their webhook URL, in API order.
func dsSourceAppRows(apps []SourceApp, publicBaseURL string) []dsSourceAppRow {
	rows := make([]dsSourceAppRow, 0, len(apps))
	for _, app := range apps {
		rows = append(rows, dsSourceAppRow{
			SourceApp:  app,
			WebhookURL: dsSourceAppWebhookURL(publicBaseURL, app.WebhookKey),
		})
	}
	return rows
}

// dsSourceAppsSubtitle describes the page without assuming the org name
// resolved (GetOrg is best-effort there).
func dsSourceAppsSubtitle(orgName string) string {
	if orgName != "" {
		return "GitHub App credentials shared by every project in " + orgName + ". A GitHub App source picks one of these Apps and a pull-request trigger starts an agent run."
	}
	return "GitHub App credentials shared by the organization. A GitHub App source picks one of these Apps and a pull-request trigger starts an agent run."
}

// uiSourceApps renders the org's source Apps page. Every project member can open
// it (the settings rail links here); the App list and the register/delete
// controls are org-admin only, so a non-admin gets the forbidden state.
func (s *Server) uiSourceApps(c echo.Context) error {
	ctx := c.Request().Context()
	project, err := s.memory.GetCurrentProject(ctx)
	if err != nil {
		project = nil
	}
	data := dsSourceAppsPageData{
		settingsNavCommon: s.settingsNavPayload(c, ctx),
		FlashMsg: flashFromQuery(c, []flashParam{
			{key: "saved", msg: "Source App registered."},
			{key: "deleted", msg: "Source App removed."},
		}),
		FlashErr: flashError(c),
	}
	if project == nil {
		if err != nil {
			data.LoadErr = userFacingError(err)
			return s.page(c, pageTitle("Source apps"), SourceAppsPage(data))
		}
		// No active project: land on the picker rather than an empty org page
		// (the same degrade as uiProjectCredentials).
		return c.Redirect(http.StatusSeeOther, "/orgs")
	}
	data.OrgID = project.OrgID
	if org, oerr := s.memory.GetOrg(ctx, project.OrgID); oerr == nil && org != nil {
		data.OrgName = org.Name
	} else if oerr != nil {
		captureError(oerr)
	}
	if !s.orgAdminForCaller(c, project.OrgID) {
		data.Forbidden = true
		return s.page(c, pageTitle("Source apps"), SourceAppsPage(data))
	}
	apps, aerr := s.memory.ListOrgSourceApps(ctx, project.OrgID)
	if aerr != nil {
		data.LoadErr = userFacingError(aerr)
	}
	data.Rows = dsSourceAppRows(apps, s.cfg.MemoryPublicURL)
	return s.page(c, pageTitle("Source apps"), SourceAppsPage(data))
}

// uiSourceAppCreate registers an org source App (org-admin only, PRG). The
// private key and webhook secret are write-only: they are forwarded once and
// never rendered back.
func (s *Server) uiSourceAppCreate(c echo.Context) error {
	ctx := c.Request().Context()
	back := dsSourceAppsPath
	orgID, ok := s.sourceAppsOrgForCaller(c, back)
	if !ok {
		return nil
	}
	rawAppID := strings.TrimSpace(c.FormValue("appId"))
	appID, err := strconv.ParseInt(rawAppID, 10, 64)
	if err != nil || appID <= 0 {
		return redirectWithError(c, back, errors.New("GitHub App ID must be the numeric App id from GitHub's settings, not the slug"))
	}
	slug := strings.TrimSpace(c.FormValue("slug"))
	if slug == "" {
		return redirectWithError(c, back, errors.New("App slug is required"))
	}
	privateKey := c.FormValue("privateKey")
	webhookSecret := c.FormValue("webhookSecret")
	if strings.TrimSpace(privateKey) == "" {
		return redirectWithError(c, back, errors.New("the App's private key is required"))
	}
	if strings.TrimSpace(webhookSecret) == "" {
		return redirectWithError(c, back, errors.New("the App's webhook secret is required"))
	}
	req := &DSSourceAppRequest{
		Provider:      "github",
		AppID:         appID,
		Slug:          slug,
		PrivateKey:    privateKey,
		WebhookSecret: webhookSecret,
	}
	if _, err := s.memory.CreateOrgSourceApp(ctx, orgID, req); err != nil {
		return redirectWithError(c, back, dsFormError(err))
	}
	redirectAfterMutation(c, back+"?saved=1")
	return nil
}

// uiSourceAppDelete removes an org source App (org-admin only, PRG). The server
// refuses with a 409 while a project source still references the App, and that
// message names the bound sources — it is the remediation text, so it is
// surfaced verbatim instead of being replaced by a generic failure.
func (s *Server) uiSourceAppDelete(c echo.Context) error {
	ctx := c.Request().Context()
	back := dsSourceAppsPath
	orgID, ok := s.sourceAppsOrgForCaller(c, back)
	if !ok {
		return nil
	}
	if err := s.memory.DeleteOrgSourceApp(ctx, orgID, c.Param("appId")); err != nil {
		return redirectWithError(c, back, dsFormError(err))
	}
	redirectAfterMutation(c, back+"?deleted=1")
	return nil
}

// sourceAppsOrgForCaller resolves the active project's org and enforces the
// page's org-admin gate for a write. It redirects with an explanation and
// reports false when the caller cannot write, so no handler runs a request the
// API would answer with 403.
func (s *Server) sourceAppsOrgForCaller(c echo.Context, back string) (string, bool) {
	ctx := c.Request().Context()
	project, err := s.memory.GetCurrentProject(ctx)
	if err != nil || project == nil || project.OrgID == "" {
		redirectWithError(c, back, errors.New("no active project organization"))
		return "", false
	}
	if !s.orgAdminForCaller(c, project.OrgID) {
		redirectWithError(c, back, errors.New("source Apps can only be changed by an organization administrator"))
		return "", false
	}
	return project.OrgID, true
}

// --- github_app triggers ---
//
// A trigger binds one source event type to a runtime agent. For github_app the
// server accepts pull_request only, so the create form fixes the event type and
// the gateway never offers a choice the API would reject.

// dsSourceTriggersView is the trigger panel's data on a source detail page.
type dsSourceTriggersView struct {
	SourceID string
	Triggers []DSSourceTrigger
	// Agents are the project's runtime agents, the create form's options.
	Agents []AgentDefinitionSummary
	// LoadErr records a failed trigger listing; the panel still renders so the
	// source stays editable.
	LoadErr error
}

// dsDataSourceTriggers loads the panel for a github_app source. Nil means the
// provider has no trigger surface, so the detail page renders no panel at all.
func (s *Server) dsDataSourceTriggers(c echo.Context, src *DSIntegration) *dsSourceTriggersView {
	if src == nil || src.ProviderType != dsProviderTypeGitHubApp {
		return nil
	}
	ctx := c.Request().Context()
	view := &dsSourceTriggersView{SourceID: src.ID}
	if triggers, err := s.memory.ListSourceTriggers(ctx, src.ID); err != nil {
		view.LoadErr = userFacingError(err)
	} else {
		view.Triggers = triggers
	}
	if agents, err := s.memory.ListAgentDefinitions(ctx); err != nil {
		captureError(err)
	} else {
		view.Agents = agents
	}
	return view
}

// dsTriggerAgentName resolves an agent id to its display name, falling back to
// the id when the agent is no longer listed (a deleted agent keeps its binding
// visible so the stale row can be removed).
func dsTriggerAgentName(agents []AgentDefinitionSummary, agentID string) string {
	for _, agent := range agents {
		if agent.ID == agentID {
			if name := strings.TrimSpace(agent.Name); name != "" {
				return name
			}
			break
		}
	}
	return agentID
}

// dsTriggerToggleLabel is the action that flips the current state.
func dsTriggerToggleLabel(enabled bool) string {
	if enabled {
		return "Disable"
	}
	return "Enable"
}

// dsTriggerNextEnabled is the value the toggle form submits.
func dsTriggerNextEnabled(enabled bool) string {
	if enabled {
		return "false"
	}
	return "true"
}

// dsTriggerQueueLabel renders an empty queue as the server's default.
func dsTriggerQueueLabel(queue string) string {
	if q := strings.TrimSpace(queue); q != "" {
		return q
	}
	return "default"
}

// uiDataSourceTriggerCreate binds pull_request to an agent (PRG). The event type
// is fixed by the form; a source that is not a github_app source has no trigger
// panel, so a stray POST is refused rather than forwarded.
func (s *Server) uiDataSourceTriggerCreate(c echo.Context) error {
	ctx := c.Request().Context()
	id := c.Param("id")
	detailPath := "/data-sources/" + url.PathEscape(id)
	src, err := s.memory.GetDSIntegration(ctx, id)
	if err != nil {
		return redirectWithError(c, detailPath, dsFormError(err))
	}
	if src.ProviderType != dsProviderTypeGitHubApp {
		return redirectWithError(c, detailPath, errors.New("this data source does not support triggers"))
	}
	agentID := strings.TrimSpace(c.FormValue("agentId"))
	if agentID == "" {
		return redirectWithError(c, detailPath+"#triggers", errors.New("pick an agent to run on pull requests"))
	}
	// The panel offers exactly one event type; defaulting an absent value keeps
	// an older cached form working without offering a choice the server rejects.
	eventType := strings.TrimSpace(c.FormValue("eventType"))
	if eventType == "" {
		eventType = dsTriggerEventPullRequest
	}
	enabled := true
	req := &DSSourceTriggerRequest{
		EventType: eventType,
		AgentID:   agentID,
		Queue:     strings.TrimSpace(c.FormValue("queue")),
		Enabled:   &enabled,
	}
	if _, err := s.memory.CreateSourceTrigger(ctx, id, req); err != nil {
		return redirectWithError(c, detailPath+"#triggers", dsFormError(err))
	}
	redirectAfterMutation(c, detailPath+"?triggerCreated=1#triggers")
	return nil
}

// dsTriggerEventPullRequest is the only event a github_app source accepts.
const dsTriggerEventPullRequest = "pull_request"

// uiDataSourceTriggerToggle flips a trigger's enabled flag (PATCH). Only
// enabled travels, so the binding's agent and queue stay as they are.
func (s *Server) uiDataSourceTriggerToggle(c echo.Context) error {
	ctx := c.Request().Context()
	id := c.Param("id")
	detailPath := "/data-sources/" + url.PathEscape(id)
	enabled := strings.TrimSpace(c.FormValue("enabled")) == "true"
	req := &DSSourceTriggerRequest{Enabled: &enabled}
	if _, err := s.memory.UpdateSourceTrigger(ctx, id, c.Param("triggerId"), req); err != nil {
		return redirectWithError(c, detailPath+"#triggers", dsFormError(err))
	}
	redirectAfterMutation(c, detailPath+"?triggerToggled=1#triggers")
	return nil
}

// uiDataSourceTriggerDelete removes a trigger binding (PRG).
func (s *Server) uiDataSourceTriggerDelete(c echo.Context) error {
	ctx := c.Request().Context()
	id := c.Param("id")
	detailPath := "/data-sources/" + url.PathEscape(id)
	if err := s.memory.DeleteSourceTrigger(ctx, id, c.Param("triggerId")); err != nil {
		return redirectWithError(c, detailPath+"#triggers", dsFormError(err))
	}
	redirectAfterMutation(c, detailPath+"?triggerDeleted=1#triggers")
	return nil
}

// --- connect-form App picker ---

// dsFieldSelect is a schema field rendered as a select whose values the gateway
// resolved. Empty Options with no LoadErr is a genuine empty state: the renderer
// shows EmptyMessage and links to EmptyHref instead of a dead input.
type dsFieldSelect struct {
	Placeholder  string
	Options      []dsFieldOption
	EmptyHref    string
	EmptyLabel   string
	EmptyMessage string
	LoadErr      error
}

// dsFieldOption is one select option (value submitted, label shown).
type dsFieldOption struct {
	Value string
	Label string
}

// dsAttachSourceAppSelect replaces github_app's app_id text input with a select
// of the org's registered Apps. It is a gateway-side special case on purpose:
// the provider schema stays provider-neutral and the server keeps validating
// that the id names an App in the project's org. A failed lookup or an empty
// registry is carried on the field so the form explains the remediation instead
// of offering a free-text id that cannot work.
func (s *Server) dsAttachSourceAppSelect(ctx context.Context, providerType string, fields []dsConfigField) []dsConfigField {
	if providerType != dsProviderTypeGitHubApp {
		return fields
	}
	index := -1
	for i := range fields {
		if fields[i].Key == dsSourceAppsFieldKey {
			index = i
			break
		}
	}
	if index < 0 {
		return fields
	}
	sel := &dsFieldSelect{
		Placeholder:  "Select a registered App",
		EmptyHref:    dsSourceAppsPath,
		EmptyLabel:   "Open source Apps",
		EmptyMessage: "No GitHub Apps are registered for this organization yet. An organization administrator must register one before this source can be connected.",
	}
	project, err := s.memory.GetCurrentProject(ctx)
	switch {
	case err != nil || project == nil || project.OrgID == "":
		sel.LoadErr = errors.New("no active project organization")
	default:
		apps, aerr := s.memory.ListOrgSourceApps(ctx, project.OrgID)
		if aerr != nil {
			sel.LoadErr = userFacingError(aerr)
			break
		}
		for _, app := range apps {
			sel.Options = append(sel.Options, dsFieldOption{Value: app.ID, Label: dsSourceAppLabel(app)})
		}
	}
	fields[index].Select = sel
	return fields
}
