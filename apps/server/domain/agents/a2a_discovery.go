package agents

import (
	"log/slog"
	"strings"

	"github.com/labstack/echo/v4"

	"github.com/emergent-company/emergent.memory/domain/events"
	"github.com/emergent-company/emergent.memory/internal/config"
	"github.com/emergent-company/emergent.memory/pkg/acpslug"
	"github.com/emergent-company/emergent.memory/pkg/apperror"
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
}

// NewA2AHandler creates a new A2A handler.
func NewA2AHandler(repo *Repository, executor *AgentExecutor, eventsSvc *events.Service, log *slog.Logger, cfg *config.Config) *A2AHandler {
	h := &A2AHandler{repo: repo, executor: executor, eventsSvc: eventsSvc, log: log}
	if cfg != nil {
		h.a2aOrigin = strings.TrimRight(cfg.A2AOrigin, "/")
		h.appURL = strings.TrimRight(cfg.AppURL, "/")
	}
	return h
}

// A2ACardVersion is the static AgentCard version string.
const A2ACardVersion = "1.0.0"

// A2APlatformName is the static platform identity for the global AgentCard.
const A2APlatformName = "Memory"

// A2APlatformDescription is the static description for the global AgentCard.
const A2APlatformDescription = "Memory knowledge graph agent platform"

// A2AProviderOrganization is the provider organization advertised on cards.
const A2AProviderOrganization = "Emergent"

// A2ABearerSchemeName is the key under which the bearer scheme is declared.
const A2ABearerSchemeName = "memoryApiToken"

// GlobalAgentCard returns the static, tenant-blind platform AgentCard.
//
// It MUST NOT read agent definitions, projects, orgs, or any tenant row — this
// is a security invariant enforced by a unit test.
func GlobalAgentCard() AgentCard {
	return AgentCard{
		Name:        A2APlatformName,
		Description: A2APlatformDescription,
		Version:     A2ACardVersion,
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
		},
		DefaultInputModes:  []string{"application/json", "text/plain"},
		DefaultOutputModes: []string{"application/json", "text/plain"},
		Skills:             []AgentSkill{},
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

// withInterfaceOrigin returns a copy of the card with the advertised interface
// URL populated (see resolveInterfaceOrigin).
func (h *A2AHandler) withInterfaceOrigin(c echo.Context, card AgentCard) AgentCard {
	for i := range card.SupportedInterfaces {
		card.SupportedInterfaces[i].URL = h.resolveInterfaceOrigin(c)
	}
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
		// 401 (no auth) / 400 (no project) — surfaced via apperror so the Echo
		// error handler maps them to the correct HTTP status.
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
