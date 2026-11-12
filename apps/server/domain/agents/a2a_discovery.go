package agents

import (
	"log/slog"
	"strings"

	"github.com/labstack/echo/v4"

	"github.com/emergent-company/emergent.memory/domain/events"
	"github.com/emergent-company/emergent.memory/internal/config"
	"github.com/emergent-company/emergent.memory/pkg/a2ui"
	"github.com/emergent-company/emergent.memory/pkg/acpslug"
	"github.com/emergent-company/emergent.memory/pkg/apperror"
	"github.com/emergent-company/emergent.memory/pkg/auth"
)

// A2AHandler handles A2A v1.0 HTTP+JSON requests. Discovery is implemented
// here; message/stream bodies are implemented by a later lane.
type A2AHandler struct {
	repo      *Repository
	executor  *AgentExecutor
	eventsSvc *events.Service
	log       *slog.Logger

	// a2aOrigin is the configured public origin (config.A2AOrigin) advertised in
	// AgentCards. Empty means the origin is derived from the incoming request.
	a2aOrigin string

	// appURL is the configured application base URL (config.AppURL). It is used
	// as the advertised interface origin when A2A_ORIGIN is unset, before any
	// request-derived (and therefore spoofable) origin is considered.
	appURL string

	// frontendURL is the configured web-UI origin (config.FrontendURL():
	// WEB_UI_URL, falling back to APP_URL). Browser-facing assets such as the
	// agent-card icon are served by the gateway on this host, not by the API
	// host, so the icon must be anchored here rather than to appURL.
	frontendURL string
}

// NewA2AHandler creates a new A2A handler.
func NewA2AHandler(repo *Repository, executor *AgentExecutor, eventsSvc *events.Service, log *slog.Logger, cfg *config.Config) *A2AHandler {
	h := &A2AHandler{repo: repo, executor: executor, eventsSvc: eventsSvc, log: log}
	if cfg != nil {
		h.a2aOrigin = strings.TrimRight(cfg.A2AOrigin, "/")
		h.appURL = strings.TrimRight(cfg.AppURL, "/")
		h.frontendURL = cfg.FrontendURL()
	}
	return h
}

// acpProjectID extracts the project ID from the authenticated user context.
// Shared by the A2A v1.0 surface (message/stream/discovery) — kept under its
// original name after the ACP HTTP layer was removed.
//
// A missing project is a client-side mistake, not a server fault, so it is
// surfaced as an A2A PROJECT_REQUIRED (HTTP 400) envelope naming the expected
// selector rather than the generic INVALID_AGENT_RESPONSE.
func acpProjectID(c echo.Context) (string, error) {
	user := auth.GetUser(c)
	if user == nil {
		return "", apperror.ErrUnauthorized
	}
	// A project-scoped API token binds the request to its own project; the
	// X-Project-ID header must not be able to redirect it to another project.
	// Mirrors auth.GetProjectID, which also prefers the token-bound project.
	if user.APITokenProjectID != "" {
		return user.APITokenProjectID, nil
	}
	if user.ProjectID == "" {
		return "", NewA2AError(A2ACodeInvalidArgument, A2AReasonProjectRequired,
			"project context is required: send the X-Project-ID header, or use a project-scoped API token")
	}
	return user.ProjectID, nil
}

// A2ACardVersion is the static AgentCard version string.
const A2ACardVersion = "1.0.0"

// A2APlatformName is the static platform identity for the global AgentCard.
const A2APlatformName = "Memory"

// A2APlatformDescription is the static description for the global AgentCard.
const A2APlatformDescription = "Memory knowledge graph agent platform"

// A2AProviderOrganization is the provider organization advertised on cards.
const A2AProviderOrganization = "Memory"

// A2ABearerSchemeName is the key under which the bearer scheme is declared.
const A2ABearerSchemeName = "memoryApiToken"

// A2AAgentMetadataExtensionURI is the Gemini Enterprise "agent metadata"
// extension. Registries that consume it (Gemini Enterprise's agent gallery is
// the reference implementation) render `authorName` as the agent's author and
// `contactEmail` as its contact address; an agent that omits the extension is
// listed with no author at all. Both params are optional strings of at most 256
// characters.
const A2AAgentMetadataExtensionURI = "https://www.googleapis.com/gemini-enterprise/a2a/extensions/agent_metadata/v1"

// A2AAuthorName is the builder identity advertised through the agent metadata
// extension. It must not contain "Google": Gemini Enterprise suppresses names
// that do, so a vendor cannot impersonate them.
const A2AAuthorName = "Emergent Company"

// A2AContactEmail is the contact address advertised through the agent metadata
// extension. Gemini Enterprise ignores values that are not valid email
// addresses.
const A2AContactEmail = "support@emergent-company.ai"

// A2ADocumentationURL is advertised as `documentationUrl` on every card.
const A2ADocumentationURL = "https://emergent-company.github.io/emergent.memory/latest/developer-guide/a2a-protocol/"

// a2aIconPath is the gateway-served icon advertised as `iconUrl`. It is a path
// rather than a full URL so the advertised origin keeps following the
// deployment (see A2AHandler.resolveIconURL).
const a2aIconPath = "/assets/icon-512.png"

// GlobalAgentCard returns the static, tenant-blind platform AgentCard.
//
// It MUST NOT read agent definitions, projects, orgs, or any tenant row — this
// is a security invariant enforced by a unit test. The skills it carries are the
// static PlatformSkills directory, which describes what the platform does;
// per-project skills belong on the extended card, which is composed from agent
// definitions for one project.
func GlobalAgentCard() AgentCard {
	return AgentCard{
		Name:             A2APlatformName,
		Description:      A2APlatformDescription,
		Version:          A2ACardVersion,
		DocumentationURL: A2ADocumentationURL,
		Provider: &AgentProvider{
			Organization: A2AProviderOrganization,
		},
		SupportedInterfaces: []AgentInterface{
			{
				ProtocolBinding: "HTTP+JSON",
				ProtocolVersion: A2AProtocolVersion,
			},
		},
		Capabilities: A2AAgentCapabilities{
			Streaming:         true,
			ExtendedAgentCard: true,
			Extensions: []AgentExtension{
				{
					URI:    A2UIA2AExtensionURI,
					Params: map[string]any{"supportedCatalogIds": a2ui.SupportedCatalogIDs()},
				},
				AgentMetadataExtension(),
			},
		},
		DefaultInputModes:  []string{"application/json", "text/plain"},
		DefaultOutputModes: []string{"application/json", "text/plain"},
		Skills:             PlatformSkills(),
	}
}

// AgentMetadataExtension is the agent_metadata/v1 card extension carrying the
// builder identity. It is a function rather than a literal so the wire shape is
// asserted by the same tests as the card.
func AgentMetadataExtension() AgentExtension {
	return AgentExtension{
		URI:         A2AAgentMetadataExtensionURI,
		Description: "Author and contact metadata for agent galleries",
		Params: map[string]any{
			"authorName":   A2AAuthorName,
			"contactEmail": A2AContactEmail,
		},
	}
}

// PlatformSkills is the static, tenant-blind capability directory advertised on
// the global card. It describes the platform, not any project's agents. A fresh
// slice is returned on every call so a caller that mutates it cannot change what
// a later request sees.
func PlatformSkills() []AgentSkill {
	return []AgentSkill{
		{
			ID:          "knowledge-graph-search",
			Name:        "Knowledge graph search",
			Description: "Search and traverse the project knowledge graph for entities, relationships and extracted facts.",
			Tags:        []string{"knowledge-graph", "search", "retrieval"},
			Examples:    []string{"Which suppliers are linked to the Acme contract?"},
			InputModes:  []string{"text/plain"},
			OutputModes: []string{"text/plain", "application/json"},
		},
		{
			ID:          "document-retrieval",
			Name:        "Document retrieval",
			Description: "Retrieve indexed documents and their extracted structure, with citations back to the source.",
			Tags:        []string{"documents", "retrieval", "citations"},
			InputModes:  []string{"text/plain"},
			OutputModes: []string{"text/plain", "application/json"},
		},
		{
			ID:          "agent-delegation",
			Name:        "Agent delegation",
			Description: "Delegate a task to a project-configured agent and stream its progress, tool calls and generated UI.",
			Tags:        []string{"agents", "delegation", "streaming"},
			InputModes:  []string{"text/plain", "application/json"},
			OutputModes: []string{"text/plain", "application/json"},
		},
	}
}

// bearerSecurityDeclaration returns the bearer security scheme + requirement
// declared on the authenticated extended card. The server only implements
// bearer auth; no OAuth2/OIDC scheme is declared.
func bearerSecurityDeclaration() (map[string]SecurityScheme, []SecurityRequirement) {
	schemes := map[string]SecurityScheme{
		A2ABearerSchemeName: {
			HTTPAuth: &HTTPAuthSecurityScheme{
				Scheme:       "Bearer",
				BearerFormat: "emt",
			},
		},
	}
	requirements := []SecurityRequirement{
		{
			Schemes: map[string]SecuritySchemeReference{
				A2ABearerSchemeName: {List: []string{}},
			},
		},
	}
	return schemes, requirements
}

// ExtendedAgentCardFromSkills composes the extended card from a skill list,
// attaching the bearer security declaration. Pure and unit-testable.
func ExtendedAgentCardFromSkills(skills []AgentSkill) AgentCard {
	card := GlobalAgentCard()
	card.Skills = skills
	card.SecuritySchemes, card.SecurityRequirements = bearerSecurityDeclaration()
	return card
}

// AgentDefinitionToSkill derives an A2A AgentSkill from an agent definition.
// The skill id is the RFC 1123 slug (pkg/acpslug); name/description prefer
// ACPConfig values; tags is always non-nil; input/output modes prefer ACPConfig
// when present.
func AgentDefinitionToSkill(def *AgentDefinition) AgentSkill {
	name := def.Name
	description := derefString(def.Description)
	inputModes := []string{"text/plain"}
	outputModes := []string{"text/plain"}

	if def.ACPConfig != nil {
		if def.ACPConfig.DisplayName != "" {
			name = def.ACPConfig.DisplayName
		}
		if def.ACPConfig.Description != "" {
			description = def.ACPConfig.Description
		}
		if len(def.ACPConfig.InputModes) > 0 {
			inputModes = def.ACPConfig.InputModes
		}
		if len(def.ACPConfig.OutputModes) > 0 {
			outputModes = def.ACPConfig.OutputModes
		}
	}

	return AgentSkill{
		ID:          acpslug.FromName(def.Name),
		Name:        name,
		Description: description,
		Tags:        []string{},
		InputModes:  inputModes,
		OutputModes: outputModes,
	}
}

// extendedAgentCard returns the per-project AgentCard, deriving skills from
// external-visibility agent definitions for the resolved project.
func (h *A2AHandler) extendedAgentCard(c echo.Context, projectID string) (AgentCard, error) {
	defs, err := h.repo.FindExternalAgentDefinitions(c.Request().Context(), projectID)
	if err != nil {
		return AgentCard{}, apperror.NewInternal("failed to list external agents", err)
	}

	skills := make([]AgentSkill, 0, len(defs))
	for _, def := range defs {
		skills = append(skills, AgentDefinitionToSkill(def))
	}

	return ExtendedAgentCardFromSkills(skills), nil
}

// resolveInterfaceOrigin returns the public origin advertised as
// supportedInterfaces[].url. Precedence: configured A2A_ORIGIN, then configured
// APP_URL, then the incoming request (honouring X-Forwarded-Proto/Host as a last
// resort). A spoofed X-Forwarded-Host/Proto header therefore cannot point
// unauthenticated clients at an attacker-controlled origin when either config
// value is set.
func (h *A2AHandler) resolveInterfaceOrigin(c echo.Context) string {
	if h.a2aOrigin != "" {
		return h.a2aOrigin
	}
	if h.appURL != "" {
		return h.appURL
	}

	scheme := c.Scheme()
	if xf := c.Request().Header.Get("X-Forwarded-Proto"); xf != "" {
		scheme = xf
	}
	if scheme == "" {
		scheme = "http"
	}

	host := c.Request().Host
	if xf := c.Request().Header.Get("X-Forwarded-Host"); xf != "" {
		host = xf
	}
	if host == "" {
		host = "localhost"
	}

	return scheme + "://" + host
}

// resolveIconURL returns the absolute `iconUrl` advertised on cards. The icon is
// a web-UI asset served by the gateway under /assets/*, so it is anchored to the
// frontend origin (WEB_UI_URL, falling back to APP_URL) — not to the API origin,
// which serves neither the asset nor the UI. When no frontend origin is
// configured the interface origin is the last resort, so the value is always
// absolute. Registry galleries render this as the agent's tile; leaving it unset
// gives a blank tile even when every other field is populated.
func (h *A2AHandler) resolveIconURL(c echo.Context) string {
	base := h.frontendURL
	if base == "" {
		base = h.resolveInterfaceOrigin(c)
	}
	return base + a2aIconPath
}

// withInterfaceOrigin returns a copy of the card with the deployment-specific
// fields populated: the advertised interface URL (see resolveInterfaceOrigin)
// and the absolute icon URL (see resolveIconURL). Both are deployment
// coordinates, never tenant data.
func (h *A2AHandler) withInterfaceOrigin(c echo.Context, card AgentCard) AgentCard {
	for i := range card.SupportedInterfaces {
		card.SupportedInterfaces[i].URL = h.resolveInterfaceOrigin(c)
	}
	card.IconURL = h.resolveIconURL(c)
	return card
}

// GlobalAgentCardHandler serves GET /.well-known/agent-card.json (no auth).
func (h *A2AHandler) GlobalAgentCardHandler(c echo.Context) error {
	if _, a2aErr := ResolveA2AVersion(c); a2aErr != nil {
		return writeA2AError(c, a2aErr)
	}
	return writeA2AJSON(c, h.withInterfaceOrigin(c, GlobalAgentCard()))
}

// ExtendedAgentCardHandler serves GET /extendedAgentCard (agents:read).
func (h *A2AHandler) ExtendedAgentCardHandler(c echo.Context) error {
	if _, a2aErr := ResolveA2AVersion(c); a2aErr != nil {
		return writeA2AError(c, a2aErr)
	}

	projectID, err := acpProjectID(c)
	if err != nil {
		// 401 (no auth) / 400 PROJECT_REQUIRED (no project selector) — returned
		// as an A2AError so a2aErrorEnvelopeMiddleware preserves the specific
		// reason instead of collapsing it into INVALID_AGENT_RESPONSE.
		return err
	}

	card, err := h.extendedAgentCard(c, projectID)
	if err != nil {
		// Log the wrapped cause server-side and return a stable wire message so
		// DB/provider details never leak to authenticated callers.
		h.log.Error("failed to build extended agent card", "error", err)
		return writeA2AError(c, NewA2AError(A2ACodeInvalidAgentResponse, A2AReasonInvalidAgentResponse, "failed to build extended agent card"))
	}

	return writeA2AJSON(c, h.withInterfaceOrigin(c, card))
}
