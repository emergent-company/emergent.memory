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
	var appErr *apperror.Error
	require.ErrorAs(t, err, &appErr)
	assert.Equal(t, http.StatusBadRequest, appErr.HTTPStatus)
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
