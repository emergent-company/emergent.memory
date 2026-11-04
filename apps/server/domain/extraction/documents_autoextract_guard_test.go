package extraction_test

import (
	"context"
	"net/http"
	"testing"

	"github.com/labstack/echo/v4"
	"github.com/stretchr/testify/require"

	"github.com/emergent-company/emergent.memory/domain/documents"
	"github.com/emergent-company/emergent.memory/domain/extraction"
	"github.com/emergent-company/emergent.memory/internal/testutil"
	"github.com/emergent-company/emergent.memory/pkg/apperror"
	"github.com/emergent-company/emergent.memory/pkg/auth"
)

// noopChunking satisfies documents.ChunkingService without touching the graph.
type noopChunking struct{}

func (noopChunking) RecreateChunks(context.Context, string, string) error { return nil }

// TestDocumentCreateAutoExtractSkipsWithoutGenerativeModel proves the document
// upload path does NOT fail when auto-extract is requested but no generative
// model resolves: the document is still created (201) and no doomed extraction
// job row is inserted. This is the #1522 non-regression contract.
func TestDocumentCreateAutoExtractSkipsWithoutGenerativeModel(t *testing.T) {
	ctx := context.Background()
	testDB := testutil.SetupTestDBOrFail(t, ctx, "documents_autoextract_guard")
	defer testDB.Close()

	projectID := setupGuardTestProject(t, ctx, testDB)
	log := guardTestLog()

	docsSvc := documents.NewService(documents.NewRepository(testDB.GetDB(), log), log)
	jobsSvc := extraction.NewObjectExtractionJobsService(testDB.GetDB(), log, extraction.DefaultObjectExtractionConfig()).
		WithGenerativeModelResolver(stubGenerativeResolver{}) // no model resolves
	handler := documents.NewHandler(docsSvc, nil, nil, noopChunking{}, jobsSvc, log)

	e := echo.New()
	e.HideBanner = true
	e.HidePort = true
	e.HTTPErrorHandler = apperror.HTTPErrorHandler(log)
	// Inject an authenticated caller scoped to the test project.
	e.Use(func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			c.Set(string(auth.UserContextKey), &auth.AuthUser{ID: testutil.AdminUser.ID, ProjectID: projectID})
			return next(c)
		}
	})
	e.POST("/api/documents", handler.Create)

	client := testutil.NewHTTPClient(e)
	resp := client.POST("/api/documents?autoExtract=true",
		testutil.WithJSONBody(map[string]any{"filename": "autoextract.txt", "content": "hello world"}))

	require.Equal(t, http.StatusCreated, resp.StatusCode,
		"upload must succeed even when auto-extract cannot resolve a model, got %d: %s", resp.StatusCode, resp.String())
	require.Equal(t, 0, countExtractionJobs(t, ctx, testDB, projectID),
		"no doomed extraction job may be inserted when no model resolves")
}

// TestDocumentCreateAutoExtractCreatesWithGenerativeModel proves the same path
// still enqueues extraction when a model resolves.
func TestDocumentCreateAutoExtractCreatesWithGenerativeModel(t *testing.T) {
	ctx := context.Background()
	testDB := testutil.SetupTestDBOrFail(t, ctx, "documents_autoextract_with_model")
	defer testDB.Close()

	projectID := setupGuardTestProject(t, ctx, testDB)
	log := guardTestLog()

	docsSvc := documents.NewService(documents.NewRepository(testDB.GetDB(), log), log)
	jobsSvc := extraction.NewObjectExtractionJobsService(testDB.GetDB(), log, extraction.DefaultObjectExtractionConfig()).
		WithGenerativeModelResolver(stubGenerativeResolver{model: "deepseek/deepseek-v4-flash"})
	handler := documents.NewHandler(docsSvc, nil, nil, noopChunking{}, jobsSvc, log)

	e := echo.New()
	e.HideBanner = true
	e.HidePort = true
	e.HTTPErrorHandler = apperror.HTTPErrorHandler(log)
	e.Use(func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			c.Set(string(auth.UserContextKey), &auth.AuthUser{ID: testutil.AdminUser.ID, ProjectID: projectID})
			return next(c)
		}
	})
	e.POST("/api/documents", handler.Create)

	client := testutil.NewHTTPClient(e)
	resp := client.POST("/api/documents?autoExtract=true",
		testutil.WithJSONBody(map[string]any{"filename": "autoextract.txt", "content": "hello world"}))

	require.Equal(t, http.StatusCreated, resp.StatusCode,
		"upload must succeed with a model, got %d: %s", resp.StatusCode, resp.String())
	require.Equal(t, 1, countExtractionJobs(t, ctx, testDB, projectID),
		"a job must be enqueued when a model resolves")
}
