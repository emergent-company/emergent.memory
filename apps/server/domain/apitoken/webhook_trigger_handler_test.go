package apitoken

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/labstack/echo/v4"
	"github.com/stretchr/testify/require"

	"github.com/emergent-company/emergent.memory/internal/config"
	"github.com/emergent-company/emergent.memory/internal/testdb"
	"github.com/emergent-company/emergent.memory/pkg/auth"
)

// runWebhookMintHandler invokes the CreateWebhookTriggerToken handler directly
// with an authenticated user injected into the echo context, returning the
// recorded response and the handler's error. The auth gate (RequireAuth) is
// exercised separately in TestWebhookTriggerTokenRouteRequiresAuth.
func runWebhookMintHandler(t *testing.T, svc *Service, projectID, body string) (*httptest.ResponseRecorder, error) {
	t.Helper()
	e := echo.New()
	req := httptest.NewRequest(http.MethodPost, "/api/projects/"+projectID+"/webhook-trigger-tokens", strings.NewReader(body))
	req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)
	c.SetParamNames("projectId")
	c.SetParamValues(projectID)
	c.Set(string(auth.UserContextKey), &auth.AuthUser{ID: "user-1"})
	return rec, NewHandler(svc, nil).CreateWebhookTriggerToken(c)
}

// The mint surface produces a scoped webhook trigger credential with the exact
// ceiling, the operator-supplied name, and a default 90-day expiry.
func TestCreateWebhookTriggerTokenHandler(t *testing.T) {
	db := connectTestDB(t)
	svc := newWebhookTestService(t, db)
	projectID := seedProjectForWebhook(t, db)

	rec, err := runWebhookMintHandler(t, svc, projectID, `{"name":"github-pr-review-webhook"}`)
	require.NoError(t, err)
	require.Equal(t, http.StatusCreated, rec.Code)

	var dto CreateApiTokenResponseDTO
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &dto))
	require.NotEmpty(t, dto.Token, "the raw token is returned exactly once at mint")
	require.ElementsMatch(t, webhookTriggerScopes, dto.Scopes)
	require.Equal(t, "github-pr-review-webhook", dto.Name)
	require.Equal(t, projectID, *dto.ProjectID)
	require.NotNil(t, dto.ExpiresAt, "the minted credential must carry the default expiry")

	// user_id must be NULL (an integration is not a user).
	row, err := svc.repo.FindByHash(context.Background(), hashToken(dto.Token))
	require.NoError(t, err)
	require.NotNil(t, row)
	require.Nil(t, row.UserID)
}

// The mint surface requires authentication: an unauthenticated POST to the
// webhook-trigger-tokens route is refused with 401, mirroring the device-token
// route's RequireAuth gate.
func TestWebhookTriggerTokenRouteRequiresAuth(t *testing.T) {
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	cfg, err := config.NewConfig(log)
	require.NoError(t, err)
	m := auth.NewMiddleware(auth.MiddlewareParams{Cfg: cfg, Log: log})

	e := echo.New()
	RegisterRoutes(e, NewHandler(nil, nil), m)

	req := httptest.NewRequest(http.MethodPost, "/api/projects/"+uuid.NewString()+"/webhook-trigger-tokens", nil)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)
	require.Equal(t, http.StatusUnauthorized, rec.Code, "unauthenticated mint must be refused")
}

// End-to-end lifecycle over the real HTTP auth path: mint a webhook trigger
// credential, present it as a bearer (the gateway's AGENT_TRIGGER_TOKEN
// install), confirm it validates as marker-class and ceiling-bound, then revoke
// it and confirm it denies.
func TestWebhookTriggerCredentialEndToEnd(t *testing.T) {
	tdb := testdb.SetupTestDBOrFail(t, context.Background(), "apitoken_webhook_e2e")
	t.Cleanup(tdb.Close)
	db := tdb.DB
	ctx := context.Background()

	svc := newWebhookTestService(t, db)
	projectID := seedProjectForWebhook(t, db)

	// Mint through the HTTP mint surface (the operator path).
	rec, err := runWebhookMintHandler(t, svc, projectID, `{"name":"integration-e2e"}`)
	require.NoError(t, err)
	require.Equal(t, http.StatusCreated, rec.Code)
	var dto CreateApiTokenResponseDTO
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &dto))

	// Build the auth middleware against the same throwaway DB (NULL-user tokens
	// never touch the user-profile service).
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	m := auth.NewMiddleware(auth.MiddlewareParams{DB: db, Cfg: tdb.Config, Log: log})

	e := echo.New()
	e.Use(m.RequireAuth())
	e.POST("/api/projects/:projectId/query", func(c echo.Context) error {
		u := auth.GetUser(c)
		return c.JSON(http.StatusOK, map[string]any{"scopes": u.Scopes})
	})

	probe := func(token string) *httptest.ResponseRecorder {
		t.Helper()
		req := httptest.NewRequest(http.MethodPost, "/api/projects/"+projectID+"/query", nil)
		if token != "" {
			req.Header.Set("Authorization", "Bearer "+token)
		}
		rec := httptest.NewRecorder()
		e.ServeHTTP(rec, req)
		return rec
	}

	// Validate: the installed credential validates and carries the ceiling.
	recOK := probe(dto.Token)
	require.Equal(t, http.StatusOK, recOK.Code, "installed credential must validate")
	var body struct {
		Scopes []string `json:"scopes"`
	}
	require.NoError(t, json.Unmarshal(recOK.Body.Bytes(), &body))
	require.ElementsMatch(t, webhookTriggerScopes, body.Scopes)

	// Revoke through the shared project-token surface.
	require.NoError(t, svc.Revoke(ctx, dto.ID, projectID, ""))

	// The revoked credential now denies.
	require.Equal(t, http.StatusUnauthorized, probe(dto.Token).Code, "revoked credential must deny")
}

// The minted credential's expiry is enforced end-to-end: a credential whose
// expires_at has passed is denied on use.
func TestWebhookTriggerCredentialExpiryEndToEnd(t *testing.T) {
	tdb := testdb.SetupTestDBOrFail(t, context.Background(), "apitoken_webhook_expiry")
	t.Cleanup(tdb.Close)
	db := tdb.DB
	ctx := context.Background()

	svc := newWebhookTestService(t, db)
	projectID := seedProjectForWebhook(t, db)

	rec, err := runWebhookMintHandler(t, svc, projectID, `{"name":"integration-expiring"}`)
	require.NoError(t, err)
	require.Equal(t, http.StatusCreated, rec.Code)
	var dto CreateApiTokenResponseDTO
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &dto))

	// Force the credential into the past.
	_, err = db.ExecContext(ctx, `UPDATE core.api_tokens SET expires_at = NOW() - INTERVAL '1 minute' WHERE id = ?`, dto.ID)
	require.NoError(t, err)

	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	m := auth.NewMiddleware(auth.MiddlewareParams{DB: db, Cfg: tdb.Config, Log: log})

	e := echo.New()
	e.Use(m.RequireAuth())
	e.POST("/api/projects/:projectId/query", func(c echo.Context) error { return c.NoContent(http.StatusOK) })

	req := httptest.NewRequest(http.MethodPost, "/api/projects/"+projectID+"/query", nil)
	req.Header.Set("Authorization", "Bearer "+dto.Token)
	rec2 := httptest.NewRecorder()
	e.ServeHTTP(rec2, req)
	require.Equal(t, http.StatusUnauthorized, rec2.Code, "expired credential must deny")
}
