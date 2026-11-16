package extraction_test

import (
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/labstack/echo/v4"
	"github.com/stretchr/testify/require"
	"github.com/uptrace/bun"
	"github.com/uptrace/bun/dialect/pgdialect"

	"github.com/emergent-company/emergent.memory/domain/extraction"
	"github.com/emergent-company/emergent.memory/pkg/apperror"
	"github.com/emergent-company/emergent.memory/pkg/auth"
)

// appErrorEnvelope is the only client-visible error shape apperror.HTTPErrorHandler
// writes: {"error":{"code":...,"message":...}}.
type appErrorEnvelope struct {
	Error struct {
		Code    string `json:"code"`
		Message string `json:"message"`
	} `json:"error"`
}

// newEmbeddingControlSQLMockHandler builds the real control handler over a
// sqlmock-backed bun.DB, so handler error paths can be driven without a live
// database. The worker fields stay nil: none of the error paths below touch
// them (they fail before/instead of currentStatus).
func newEmbeddingControlSQLMockHandler(t *testing.T) (*extraction.EmbeddingControlHandler, sqlmock.Sqlmock) {
	t.Helper()

	sqldb, mock, err := sqlmock.New()
	require.NoError(t, err)
	// Progress and Coverage run their two aggregates concurrently, so the
	// expectations cannot be ordered.
	mock.MatchExpectationsInOrder(false)

	db := bun.NewDB(sqldb, pgdialect.New())
	t.Cleanup(func() { _ = db.Close() })

	log := slog.New(slog.NewTextHandler(io.Discard, &slog.HandlerOptions{Level: slog.LevelError}))
	cfg := extraction.DefaultGraphEmbeddingConfig()

	handler := extraction.NewEmbeddingControlHandler(
		nil, nil, nil, nil,
		extraction.NewGraphEmbeddingJobsService(db, log, cfg),
		extraction.NewGraphRelationshipEmbeddingJobsService(db, log, cfg),
		extraction.NewChunkEmbeddingJobsService(db, log, extraction.DefaultChunkEmbeddingConfig()),
	)
	return handler, mock
}

// serveEmbeddingControl drives one handler through a real Echo instance using
// the production apperror handler, with the given user stamped on both the Echo
// context (auth.MustGetUser) and the request context (auth.IsSuperadminFull).
func serveEmbeddingControl(
	t *testing.T,
	handler echo.HandlerFunc,
	method, target, body string,
	user *auth.AuthUser,
) *httptest.ResponseRecorder {
	t.Helper()

	log := slog.New(slog.NewTextHandler(io.Discard, &slog.HandlerOptions{Level: slog.LevelError}))
	e := echo.New()
	e.HideBanner = true
	e.HTTPErrorHandler = apperror.HTTPErrorHandler(log)
	e.Any("/*", func(c echo.Context) error {
		if user != nil {
			c.Set(string(auth.UserContextKey), user)
			c.SetRequest(c.Request().WithContext(auth.ContextWithUser(c.Request().Context(), user)))
		}
		return handler(c)
	})

	var reqBody io.Reader
	if body != "" {
		reqBody = strings.NewReader(body)
	}
	req := httptest.NewRequest(method, target, reqBody)
	req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)
	return rec
}

// requireAppErrorEnvelope asserts the response is the apperror envelope and
// that the internal cause text never reached the body.
func requireAppErrorEnvelope(t *testing.T, rec *httptest.ResponseRecorder, status int, code, message string) {
	t.Helper()

	require.Equal(t, status, rec.Code, "body: %s", rec.Body.String())

	var env appErrorEnvelope
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &env), "body: %s", rec.Body.String())
	require.Equal(t, code, env.Error.Code, "body: %s", rec.Body.String())
	require.Equal(t, message, env.Error.Message, "body: %s", rec.Body.String())

	// A repository cause is for the 5xx log line, never the wire.
	require.NotContains(t, rec.Body.String(), "db is down", "internal cause leaked: %s", rec.Body.String())
}

// TestEmbeddingControlErrorBodiesUseAppErrorEnvelope pins the #863 conversion:
// every failure path in embedding_control_handler.go used to answer with
// map[string]any{"error": err.Error()} (a raw repository/provider message with
// no code), which bypassed apperror.HTTPErrorHandler and the lint ratchet. Each
// path now returns *apperror.Error with a coded envelope, and the message is
// written for the caller while the cause stays internal.
func TestEmbeddingControlErrorBodiesUseAppErrorEnvelope(t *testing.T) {
	const projectID = "00000000-0000-0000-0000-000000000863"
	projectUser := &auth.AuthUser{ID: "00000000-0000-0000-0000-000000000001", APITokenProjectID: projectID}
	noProjectUser := &auth.AuthUser{ID: "00000000-0000-0000-0000-000000000001"}

	t.Run("config bind failure is a 400 envelope", func(t *testing.T) {
		h, _ := newEmbeddingControlSQLMockHandler(t)
		rec := serveEmbeddingControl(t, h.Config, http.MethodPatch, "/api/embeddings/config", `{"batch_size":`, projectUser)
		requireAppErrorEnvelope(t, rec, http.StatusBadRequest, "invalid_request_body", "invalid request body")
	})

	t.Run("progress stats failure is a 500 envelope", func(t *testing.T) {
		h, mock := newEmbeddingControlSQLMockHandler(t)
		mock.ExpectQuery(`graph_embedding_jobs`).WillReturnError(errors.New("db is down"))
		mock.ExpectQuery(`graph_relationship_embedding_jobs`).WillReturnError(errors.New("db is down"))

		rec := serveEmbeddingControl(t, h.Progress, http.MethodGet, "/api/embeddings/progress", "", projectUser)
		requireAppErrorEnvelope(t, rec, http.StatusInternalServerError, "internal_error", "get object embedding stats")
		require.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("coverage stats failure is a 500 envelope", func(t *testing.T) {
		h, mock := newEmbeddingControlSQLMockHandler(t)
		mock.ExpectQuery(`kb\.graph_objects`).WillReturnError(errors.New("db is down"))
		mock.ExpectQuery(`kb\.graph_relationships`).WillReturnError(errors.New("db is down"))

		rec := serveEmbeddingControl(t, h.Coverage, http.MethodGet, "/api/embeddings/coverage", "", projectUser)
		requireAppErrorEnvelope(t, rec, http.StatusInternalServerError, "internal_error", "get object embedding coverage")
		require.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("diagnose failure is a 500 envelope", func(t *testing.T) {
		h, mock := newEmbeddingControlSQLMockHandler(t)
		mock.ExpectQuery(`FROM kb\.graph_embedding_jobs`).WillReturnError(errors.New("db is down"))

		rec := serveEmbeddingControl(t, h.DiagnoseQueue, http.MethodGet, "/api/embeddings/diagnose", "", projectUser)
		requireAppErrorEnvelope(t, rec, http.StatusInternalServerError, "internal_error", "inspect embedding queue")
		require.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("reset schedule failure is a 500 envelope", func(t *testing.T) {
		h, mock := newEmbeddingControlSQLMockHandler(t)
		mock.ExpectExec(`UPDATE kb\.graph_embedding_jobs`).WillReturnError(errors.New("db is down"))

		rec := serveEmbeddingControl(t, h.ResetSchedule, http.MethodPost, "/api/embeddings/reset-schedule", "", projectUser)
		requireAppErrorEnvelope(t, rec, http.StatusInternalServerError, "internal_error", "reset object embedding job schedule")
		require.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("clear queue failure is a 500 envelope", func(t *testing.T) {
		h, mock := newEmbeddingControlSQLMockHandler(t)
		mock.ExpectExec(`DELETE FROM kb\.graph_embedding_jobs`).WillReturnError(errors.New("db is down"))

		rec := serveEmbeddingControl(t, h.ClearQueue, http.MethodDelete, "/api/embeddings/queue", "", projectUser)
		requireAppErrorEnvelope(t, rec, http.StatusInternalServerError, "internal_error", "clear object embedding job queue")
		require.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("superadmin lookup failure is a 500 envelope", func(t *testing.T) {
		h, mock := newEmbeddingControlSQLMockHandler(t)
		mock.ExpectQuery(`superadmins`).WillReturnError(errors.New("db is down"))

		rec := serveEmbeddingControl(t, h.Progress, http.MethodGet, "/api/embeddings/progress", "", noProjectUser)
		requireAppErrorEnvelope(t, rec, http.StatusInternalServerError, "internal_error", "check superadmin privilege")
		require.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("deployment-wide progress refusal is a coded 403 envelope", func(t *testing.T) {
		h, mock := newEmbeddingControlSQLMockHandler(t)
		mock.ExpectQuery(`superadmins`).WillReturnRows(sqlmock.NewRows([]string{"role"}))

		rec := serveEmbeddingControl(t, h.Progress, http.MethodGet, "/api/embeddings/progress", "", noProjectUser)
		requireAppErrorEnvelope(t, rec, http.StatusForbidden, "forbidden",
			"superadmin privilege required for deployment-wide embedding progress")
		require.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("deployment-wide coverage refusal is a coded 403 envelope", func(t *testing.T) {
		h, mock := newEmbeddingControlSQLMockHandler(t)
		mock.ExpectQuery(`superadmins`).WillReturnRows(sqlmock.NewRows([]string{"role"}))

		rec := serveEmbeddingControl(t, h.Coverage, http.MethodGet, "/api/embeddings/coverage", "", noProjectUser)
		requireAppErrorEnvelope(t, rec, http.StatusForbidden, "forbidden",
			"superadmin privilege required for deployment-wide embedding coverage")
		require.NoError(t, mock.ExpectationsWereMet())
	})
}
