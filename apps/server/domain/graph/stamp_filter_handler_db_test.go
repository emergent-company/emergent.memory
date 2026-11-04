package graph_test

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"
	"github.com/labstack/echo/v4"
	"github.com/stretchr/testify/require"

	"github.com/emergent-company/emergent.memory/domain/graph"
	"github.com/emergent-company/emergent.memory/internal/config"
	"github.com/emergent-company/emergent.memory/pkg/apperror"
	"github.com/emergent-company/emergent.memory/pkg/auth"
)

// TestWriteStampFilterHandlers exercises the HTTP layer of the stamp filters
// (#2468) without a full server: query parsing, enum validation, and the rows
// the listing endpoints actually return. The SDK and e2e suites cover the wire
// contract; this covers the handler itself against a real database.
func TestWriteStampFilterHandlers(t *testing.T) {
	ctx, _, _, svc, projectID := setupProvenanceTest(t)
	h := graph.NewHandler(svc, &config.Config{})

	credA, credB := uuid.New(), uuid.New()
	runA, runB := uuid.New(), uuid.New()

	create := func(key string, credential, runID uuid.UUID) {
		sctx := auth.ContextWithCredential(ctx, auth.CredentialAPIKey, &credential)
		sctx = auth.ContextWithRunIDs(sctx, &runID, &runID)
		_, err := svc.Create(sctx, projectID, &graph.CreateGraphObjectRequest{
			Type:       "StampFilterHTTP",
			Key:        strPtr(key),
			Properties: map[string]any{"title": key},
		}, nil)
		require.NoError(t, err)
	}
	create("stamphttp/a", credA, runA)
	create("stamphttp/b", credB, runB)

	// get issues a request against the handler with an authenticated user for the
	// test project, and returns the recorded response.
	get := func(t *testing.T, path string, handler echo.HandlerFunc) *httptest.ResponseRecorder {
		t.Helper()
		e := echo.New()
		req := httptest.NewRequest(http.MethodGet, path, nil)
		user := &auth.AuthUser{ID: uuid.NewString(), ProjectID: projectID.String()}
		req = req.WithContext(auth.InjectAuthContext(req.Context(), user))
		rec := httptest.NewRecorder()
		c := e.NewContext(req, rec)
		// The handlers read the principal from the echo context (set by the auth
		// middleware in production); the request context carries the credential
		// stamp the write paths read.
		c.Set(string(auth.UserContextKey), user)
		err := handler(c)
		if err != nil {
			// Mirror the server's error handler: apperror carries the status the
			// API would return, so a validation failure is asserted as a 400 here
			// rather than surfacing as a test error.
			switch e := err.(type) {
			case *apperror.Error:
				rec.Code = e.HTTPStatus
				body, _ := json.Marshal(map[string]any{"code": e.Code, "message": e.Message})
				if _, werr := rec.Body.Write(body); werr != nil {
					t.Fatalf("%s: writing error body: %v", path, werr)
				}
			case *echo.HTTPError:
				rec.Code = e.Code
				fmt.Fprintf(rec.Body, "%v", e.Message)
			default:
				t.Fatalf("%s: unexpected handler error %v", path, err)
			}
			return rec
		}
		return rec
	}

	objectKeys := func(t *testing.T, rec *httptest.ResponseRecorder) []string {
		t.Helper()
		var resp struct {
			Items []struct {
				Key *string `json:"key"`
			} `json:"items"`
			Total *int `json:"total"`
		}
		require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp), "body: %s", rec.Body.String())
		keys := make([]string, 0, len(resp.Items))
		for _, item := range resp.Items {
			if item.Key != nil {
				keys = append(keys, *item.Key)
			}
		}
		require.NotNil(t, resp.Total)
		require.Equal(t, len(keys), *resp.Total, "total must match the filtered page")
		return keys
	}

	t.Run("objects filter by credential id", func(t *testing.T) {
		rec := get(t, "/api/graph/objects/search?type=StampFilterHTTP&credential_id="+credA.String(), h.ListObjects)
		require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
		require.Equal(t, []string{"stamphttp/a"}, objectKeys(t, rec))
	})

	t.Run("objects filter by run id", func(t *testing.T) {
		rec := get(t, "/api/graph/objects/search?type=StampFilterHTTP&run_id="+runB.String(), h.ListObjects)
		require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
		require.Equal(t, []string{"stamphttp/b"}, objectKeys(t, rec))
	})

	t.Run("objects filter by data class", func(t *testing.T) {
		rec := get(t, "/api/graph/objects/search?type=StampFilterHTTP&data_class=synthesized", h.ListObjects)
		require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
		require.Len(t, objectKeys(t, rec), 2)
	})

	t.Run("objects reject an unknown credential kind", func(t *testing.T) {
		rec := get(t, "/api/graph/objects/search?credential_kind=oauth", h.ListObjects)
		require.Equal(t, http.StatusBadRequest, rec.Code)
		require.Contains(t, rec.Body.String(), "credential_kind")
	})

	t.Run("objects count honours the filters", func(t *testing.T) {
		rec := get(t, "/api/graph/objects/count?type=StampFilterHTTP&credential_kind=api_key", h.CountObjects)
		require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
		var body map[string]int
		require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
		require.Equal(t, 2, body["count"])
	})

	t.Run("relationships reject a data class filter", func(t *testing.T) {
		rec := get(t, "/api/graph/relationships/search?data_class=source", h.ListRelationships)
		require.Equal(t, http.StatusBadRequest, rec.Code)
		require.Contains(t, rec.Body.String(), "data_class")
	})

	// The count endpoint shares the parser, so it must reject the same filter
	// rather than silently counting every relationship (#2468 spec).
	t.Run("relationships count rejects a data class filter", func(t *testing.T) {
		rec := get(t, "/api/graph/relationships/count?data_class=source", h.CountRelationships)
		require.Equal(t, http.StatusBadRequest, rec.Code)
		require.Contains(t, rec.Body.String(), "data_class")
	})

	t.Run("relationships accept the run filter", func(t *testing.T) {
		rec := get(t, "/api/graph/relationships/search?run_id="+runA.String(), h.ListRelationships)
		require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
		var body struct {
			Items []json.RawMessage `json:"items"`
			Total int               `json:"total"`
		}
		require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
		require.Empty(t, body.Items)
		require.Zero(t, body.Total)
	})
}
