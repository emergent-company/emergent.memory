package agents

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/labstack/echo/v4"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/emergent-company/emergent.memory/pkg/apperror"
	"github.com/emergent-company/emergent.memory/pkg/auth"
)

func newTestA2AHandler() *A2AHandler {
	return &A2AHandler{
		repo:     nil,
		executor: nil,
		log:      slog.Default(),
	}
}

func newA2AEchoContextNoAuth(method, path string) (echo.Context, *httptest.ResponseRecorder) {
	e := echo.New()
	req := httptest.NewRequest(method, path, nil)
	rec := httptest.NewRecorder()
	return e.NewContext(req, rec), rec
}

func TestGlobalAgentCard_NoTenantLeak(t *testing.T) {
	card := GlobalAgentCard()
	j := mustJSON(t, card)

	// No project / org / tenant identifiers may leak.
	for _, sub := range []string{
		"projectId", "project_id", "orgId", "org_id", "tenant",
		"proj-", "org-",
	} {
		assert.NotContains(t, j, sub)
	}

	// The card is static platform identity, not tenant data.
	assert.Equal(t, A2APlatformName, card.Name)
	assert.Equal(t, A2APlatformDescription, card.Description)
	assert.Empty(t, card.Skills, "global card must not enumerate per-project skills")
}

func TestGlobalAgentCard_RequiredFields(t *testing.T) {
	card := GlobalAgentCard()

	assert.NotEmpty(t, card.Name)
	assert.NotEmpty(t, card.Description)
	assert.NotEmpty(t, card.Version)
	assert.NotNil(t, card.Provider)
	assert.NotEmpty(t, card.Provider.Organization)
	assert.NotNil(t, card.SupportedInterfaces)
	assert.NotNil(t, card.DefaultInputModes)
	assert.NotNil(t, card.DefaultOutputModes)
	assert.NotNil(t, card.Skills)
}

func TestGlobalAgentCard_SingleHTTPJSONInterface(t *testing.T) {
	card := GlobalAgentCard()
	require.Len(t, card.SupportedInterfaces, 1)

	iface := card.SupportedInterfaces[0]
	assert.Equal(t, "HTTP+JSON", iface.ProtocolBinding)
	assert.Equal(t, "1.0", iface.ProtocolVersion)
	assert.Empty(t, iface.Tenant, "interfaces must not advertise a tenant")
}

func TestGlobalAgentCard_Capabilities(t *testing.T) {
	card := GlobalAgentCard()
	assert.True(t, card.Capabilities.Streaming)
	assert.True(t, card.Capabilities.ExtendedAgentCard)
}

func TestGlobalAgentCard_A2UIExtension(t *testing.T) {
	card := GlobalAgentCard()
	require.Len(t, card.Capabilities.Extensions, 1)
	ext := card.Capabilities.Extensions[0]
	assert.Equal(t, A2UIA2AExtensionURI, ext.URI)
	ids, ok := ext.Params["supportedCatalogIds"].([]string)
	require.True(t, ok, "supportedCatalogIds must be a string array")
	assert.Equal(t, []string{"memory-basic"}, ids)

	// No tenant data leaks through the extension params.
	j := mustJSON(t, card)
	assert.NotContains(t, j, "projectId")
	assert.NotContains(t, j, "orgId")
}

func TestAgentDefinitionToSkill_SlugID(t *testing.T) {
	def := &AgentDefinition{Name: "My Cool Agent"}
	skill := AgentDefinitionToSkill(def)
	assert.Equal(t, "my-cool-agent", skill.ID)
}

func TestAgentDefinitionToSkill_DescriptionPrefersACPConfig(t *testing.T) {
	desc := "definition description"
	def := &AgentDefinition{
		Name:        "Agent",
		Description: &desc,
		ACPConfig:   &ACPConfig{Description: "acp description", DisplayName: "Display"},
	}
	skill := AgentDefinitionToSkill(def)
	assert.Equal(t, "acp description", skill.Description)
	assert.Equal(t, "Display", skill.Name)
}

func TestAgentDefinitionToSkill_NonNilTags(t *testing.T) {
	def := &AgentDefinition{Name: "Agent"}
	skill := AgentDefinitionToSkill(def)
	assert.NotNil(t, skill.Tags, "tags must be non-nil (empty array is acceptable)")
	assert.Empty(t, skill.Tags)

	j := mustJSON(t, skill)
	assert.Contains(t, j, `"tags":[]`)
}

func TestAgentDefinitionToSkill_InputOutputModes(t *testing.T) {
	def := &AgentDefinition{
		Name:      "Agent",
		ACPConfig: &ACPConfig{InputModes: []string{"application/json"}, OutputModes: []string{"text/plain"}},
	}
	skill := AgentDefinitionToSkill(def)
	assert.Equal(t, []string{"application/json"}, skill.InputModes)
	assert.Equal(t, []string{"text/plain"}, skill.OutputModes)
}

func TestExtendedAgentCard_BearerSchemeNoOAuth2OIDC(t *testing.T) {
	schemes, requirements := bearerSecurityDeclaration()

	scheme, ok := schemes[A2ABearerSchemeName]
	require.True(t, ok)
	require.NotNil(t, scheme.HTTPAuth)
	assert.Equal(t, "Bearer", scheme.HTTPAuth.Scheme)
	assert.Equal(t, "emt", scheme.HTTPAuth.BearerFormat)

	// No OAuth2/OIDC scheme is declared.
	j := mustJSON(t, scheme)
	assert.Contains(t, j, `"httpAuthSecurityScheme"`)
	assert.NotContains(t, j, `"oauth2SecurityScheme"`)
	assert.NotContains(t, j, `"openIdConnectSecurityScheme"`)

	require.Len(t, requirements, 1)
	_, ok = requirements[0].Schemes[A2ABearerSchemeName]
	assert.True(t, ok)
}

func TestExtendedAgentCardFromSkills(t *testing.T) {
	skills := []AgentSkill{
		{ID: "skill-a", Name: "Skill A", Description: "desc", Tags: []string{}},
	}
	card := ExtendedAgentCardFromSkills(skills)

	assert.Equal(t, skills, card.Skills)
	assert.NotNil(t, card.SecuritySchemes)
	assert.NotNil(t, card.SecurityRequirements)
	assert.True(t, card.Capabilities.ExtendedAgentCard)
}

func TestExtendedAgentCardHandler_NoAuth_Returns401(t *testing.T) {
	h := newTestA2AHandler()
	c, _ := newA2AEchoContextNoAuth(http.MethodGet, "/extendedAgentCard")

	err := h.ExtendedAgentCardHandler(c)
	require.Error(t, err)
	var appErr *apperror.Error
	require.ErrorAs(t, err, &appErr)
	assert.Equal(t, http.StatusUnauthorized, appErr.HTTPStatus)
}

func TestExtendedAgentCardHandler_NoProject_Returns400(t *testing.T) {
	h := newTestA2AHandler()
	e := echo.New()
	req := httptest.NewRequest(http.MethodGet, "/extendedAgentCard", nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)
	c.Set(string(auth.UserContextKey), &auth.AuthUser{ID: "u", Email: "u@x.com", ProjectID: ""})

	err := h.ExtendedAgentCardHandler(c)
	require.Error(t, err)

	// A missing project selector is a distinct 400 PROJECT_REQUIRED, not the
	// generic INVALID_AGENT_RESPONSE (see #762).
	var a2aErr *A2AError
	require.ErrorAs(t, err, &a2aErr)
	assert.Equal(t, http.StatusBadRequest, a2aErr.Code.HTTPStatus())
	assert.Equal(t, A2AReasonProjectRequired, a2aErr.Reason)
}

func TestGlobalAgentCardHandler_ContentType(t *testing.T) {
	h := newTestA2AHandler()
	c, rec := newA2AEchoContextNoAuth(http.MethodGet, "/.well-known/agent-card.json")

	require.NoError(t, h.GlobalAgentCardHandler(c))
	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, A2AContentType, rec.Header().Get(echo.HeaderContentType))

	var card map[string]any
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &card))
	assert.Equal(t, A2APlatformName, card["name"])
	assert.False(t, strings.Contains(rec.Body.String(), "projectId"))
}

func TestGlobalAgentCardHandler_InterfaceURLNonEmpty(t *testing.T) {
	h := newTestA2AHandler()
	c, rec := newA2AEchoContextNoAuth(http.MethodGet, "/.well-known/agent-card.json")

	require.NoError(t, h.GlobalAgentCardHandler(c))

	var card AgentCard
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &card))
	require.Len(t, card.SupportedInterfaces, 1)
	assert.NotEmpty(t, card.SupportedInterfaces[0].URL, "supportedInterfaces[0].url must not be empty")
	assert.False(t, strings.Contains(rec.Body.String(), `"url":""`))
}

func TestResolveInterfaceOrigin_ConfiguredOriginWins(t *testing.T) {
	h := &A2AHandler{a2aOrigin: "https://api.dev.emergent-company.ai"} // constructor trims trailing slash
	c, _ := newA2AEchoContextNoAuth(http.MethodGet, "/")

	assert.Equal(t, "https://api.dev.emergent-company.ai", h.resolveInterfaceOrigin(c))
}

func TestResolveInterfaceOrigin_DerivedFromRequest(t *testing.T) {
	h := newTestA2AHandler() // a2aOrigin == ""
	c, _ := newA2AEchoContextNoAuth(http.MethodGet, "/")

	origin := h.resolveInterfaceOrigin(c)
	assert.NotEmpty(t, origin)
	assert.True(t, strings.HasPrefix(origin, "http://"), "derived origin should be a valid scheme://host, got %q", origin)
}

func TestResolveInterfaceOrigin_AppURLWinsOverForwardedHeader(t *testing.T) {
	// A spoofed X-Forwarded-Host must not override the configured APP_URL.
	h := &A2AHandler{appURL: "https://app.emergent-company.ai"}
	e := echo.New()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("X-Forwarded-Host", "attacker.example.com")
	req.Header.Set("X-Forwarded-Proto", "https")
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	assert.Equal(t, "https://app.emergent-company.ai", h.resolveInterfaceOrigin(c))
}

func TestResolveInterfaceOrigin_A2AOriginWinsOverAppURL(t *testing.T) {
	h := &A2AHandler{a2aOrigin: "https://api.dev.emergent-company.ai", appURL: "https://app.emergent-company.ai"}
	c, _ := newA2AEchoContextNoAuth(http.MethodGet, "/")

	assert.Equal(t, "https://api.dev.emergent-company.ai", h.resolveInterfaceOrigin(c))
}

func TestResolveInterfaceOrigin_ForwardedHeaderLastResort(t *testing.T) {
	// With neither config set, the request-derived origin (including forwarded
	// headers) is used as a last resort.
	h := &A2AHandler{}
	e := echo.New()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("X-Forwarded-Host", "proxy.example.com")
	req.Header.Set("X-Forwarded-Proto", "https")
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	assert.Equal(t, "https://proxy.example.com", h.resolveInterfaceOrigin(c))
}

func TestExtendedAgentCardHandler_InternalErrorDoesNotLeak(t *testing.T) {
	// newUUIDSyntaxRepository makes every DB query fail with a Postgres 22P02
	// syntax error, which the old code would have leaked into the envelope via
	// err.Error(). The fix returns a stable wire message and logs the cause.
	h := &A2AHandler{repo: newUUIDSyntaxRepository(t), log: slog.Default()}
	e := echo.New()
	req := httptest.NewRequest(http.MethodGet, "/extendedAgentCard", nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)
	c.Set(string(auth.UserContextKey), &auth.AuthUser{ID: "u", ProjectID: "proj-test-id"})

	require.NoError(t, h.ExtendedAgentCardHandler(c))
	assert.Equal(t, http.StatusInternalServerError, rec.Code)

	body := rec.Body.String()
	assert.NotContains(t, body, "22P02", "internal SQLSTATE must not leak")
	assert.NotContains(t, body, "invalid input syntax", "internal driver detail must not leak")
	assert.Contains(t, body, "failed to build extended agent card")
}
