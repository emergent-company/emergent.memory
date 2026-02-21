package testutil

import (
	"bytes"
	"encoding/json"
	"io"
	"log/slog"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"

	"github.com/labstack/echo/v4"
	"github.com/uptrace/bun"

	"github.com/emergent-company/emergent/domain/agents"
	"github.com/emergent-company/emergent/domain/apitoken"
	"github.com/emergent-company/emergent/domain/branches"
	"github.com/emergent-company/emergent/domain/chat"
	"github.com/emergent-company/emergent/domain/chunks"
	"github.com/emergent-company/emergent/domain/datasource"
	"github.com/emergent-company/emergent/domain/documents"
	"github.com/emergent-company/emergent/domain/embeddingpolicies"
	"github.com/emergent-company/emergent/domain/events"
	"github.com/emergent-company/emergent/domain/extraction"
	"github.com/emergent-company/emergent/domain/graph"
	"github.com/emergent-company/emergent/domain/health"
	"github.com/emergent-company/emergent/domain/integrations"
	"github.com/emergent-company/emergent/domain/invites"
	"github.com/emergent-company/emergent/domain/mcp"
	"github.com/emergent-company/emergent/domain/mcpregistry"
	"github.com/emergent-company/emergent/domain/monitoring"
	"github.com/emergent-company/emergent/domain/notifications"
	"github.com/emergent-company/emergent/domain/orgs"
	"github.com/emergent-company/emergent/domain/projects"
	"github.com/emergent-company/emergent/domain/search"
	"github.com/emergent-company/emergent/domain/superadmin"
	"github.com/emergent-company/emergent/domain/tasks"
	"github.com/emergent-company/emergent/domain/templatepacks"
	"github.com/emergent-company/emergent/domain/typeregistry"
	"github.com/emergent-company/emergent/domain/useraccess"
	"github.com/emergent-company/emergent/domain/useractivity"
	"github.com/emergent-company/emergent/domain/userprofile"
	"github.com/emergent-company/emergent/domain/users"
	"github.com/emergent-company/emergent/internal/config"
	"github.com/emergent-company/emergent/internal/storage"
	"github.com/emergent-company/emergent/pkg/apperror"
	"github.com/emergent-company/emergent/pkg/auth"
	"github.com/emergent-company/emergent/pkg/embeddings"
	"github.com/emergent-company/emergent/pkg/encryption"
)

// TestServer wraps an Echo instance for testing
type TestServer struct {
	Echo           *echo.Echo
	TestDB         *TestDB
	DB             bun.IDB
	Config         *config.Config
	Log            *slog.Logger
	AuthMiddleware *auth.Middleware
}

// NewTestServer creates a test server with all routes registered.
func NewTestServer(testDB *TestDB) *TestServer {
	return newTestServerWithDB(testDB, testDB.GetDB())
}

// newTestServerWithDB creates a test server with a specific DB connection
func newTestServerWithDB(testDB *TestDB, db bun.IDB) *TestServer {
	log := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelWarn}))

	// Create Echo instance
	e := echo.New()
	e.HideBanner = true
	e.HidePort = true

	// Use custom error handler to properly handle apperror.Error types
	e.HTTPErrorHandler = apperror.HTTPErrorHandler(log)

	// Create auth components
	userSvc := auth.NewUserProfileService(db, log)
	authMiddleware := auth.NewMiddleware(db, testDB.Config, log, userSvc)

	// Register health routes (public)
	healthHandler := health.NewHandler(testDB.Pool, testDB.Config)
	e.GET("/health", healthHandler.Health)
	e.GET("/healthz", healthHandler.Healthz)
	e.GET("/ready", healthHandler.Ready)
	e.GET("/debug", healthHandler.Debug)

	// Register protected test routes for auth testing
	protected := e.Group("/api/test")
	protected.Use(authMiddleware.RequireAuth())

	// Simple endpoint that returns user info (for testing auth)
	protected.GET("/me", func(c echo.Context) error {
		user := auth.GetUser(c)
		if user == nil {
			return echo.NewHTTPError(http.StatusUnauthorized, "No user in context")
		}
		return c.JSON(http.StatusOK, map[string]any{
			"id":        user.ID,
			"sub":       user.Sub,
			"email":     user.Email,
			"scopes":    user.Scopes,
			"projectId": user.ProjectID,
			"orgId":     user.OrgID,
		})
	})

	// Endpoint requiring specific scopes
	scopedGroup := e.Group("/api/test/scoped")
	scopedGroup.Use(authMiddleware.RequireAuth())
	scopedGroup.Use(authMiddleware.RequireScopes("documents:read"))
	scopedGroup.GET("", func(c echo.Context) error {
		return c.JSON(http.StatusOK, map[string]any{"message": "You have documents:read scope"})
	})

	// Endpoint requiring project ID
	projectGroup := e.Group("/api/test/project")
	projectGroup.Use(authMiddleware.RequireAuth())
	projectGroup.Use(authMiddleware.RequireProjectID())
	projectGroup.GET("", func(c echo.Context) error {
		user := auth.GetUser(c)
		return c.JSON(http.StatusOK, map[string]any{
			"message":   "Project ID required endpoint",
			"projectId": user.ProjectID,
		})
	})

	// Register documents routes
	docsRepo := documents.NewRepository(db, log)
	docsSvc := documents.NewService(docsRepo, log)
	storageCfg := storage.NewConfig()
	storageSvc, _ := storage.NewService(storageCfg, log)
	docsHandler := documents.NewHandler(docsSvc, storageSvc, log)
	uploadHandler := documents.NewUploadHandler(docsSvc, storageSvc, nil, log)
	documents.RegisterRoutes(e, docsHandler, uploadHandler, authMiddleware)

	// Register orgs routes
	orgsRepo := orgs.NewRepository(db, log)
	orgsSvc := orgs.NewService(orgsRepo, log)
	orgsHandler := orgs.NewHandler(orgsSvc)
	orgs.RegisterRoutes(e, orgsHandler, authMiddleware)

	// Register projects routes
	projectsRepo := projects.NewRepository(db, log)
	projectsSvc := projects.NewService(projectsRepo, log)
	projectsHandler := projects.NewHandler(projectsSvc)
	projects.RegisterRoutes(e, projectsHandler, authMiddleware)

	// Register users routes
	usersRepo := users.NewRepository(db, log)
	usersSvc := users.NewService(usersRepo, log)
	usersHandler := users.NewHandler(usersSvc)
	users.RegisterRoutes(e, usersHandler, authMiddleware)

	// Register userprofile routes
	userProfileRepo := userprofile.NewRepository(db, log)
	userProfileSvc := userprofile.NewService(userProfileRepo, log)
	userProfileHandler := userprofile.NewHandler(userProfileSvc)
	userprofile.RegisterRoutes(e, userProfileHandler, authMiddleware)

	// Create encryption service (used by apitoken, integrations, datasource)
	encryptionSvc := encryption.NewService(testDB.DB, log)

	// Register apitoken routes
	apitokenRepo := apitoken.NewRepository(db, log)
	apitokenSvc := apitoken.NewService(apitokenRepo, encryptionSvc, log)
	apitokenHandler := apitoken.NewHandler(apitokenSvc)
	apitoken.RegisterRoutes(e, apitokenHandler, authMiddleware)

	// Register graph routes
	graphRepo := graph.NewRepository(db, log)
	graphSchemaProvider := graph.ProvideSchemaProvider(db, log)
	embeddingsSvc := embeddings.NewNoopService(log)
	graphSvc := graph.NewService(graphRepo, log, graphSchemaProvider, graph.ProvideInverseTypeProvider(db, log), embeddingsSvc)
	graphHandler := graph.NewHandler(graphSvc)
	graph.RegisterRoutes(e, graphHandler, authMiddleware)

	// Register embedding policies routes
	embPolicyStore := embeddingpolicies.NewStore(db, log)
	embPolicySvc := embeddingpolicies.NewService(embPolicyStore, log)
	embPolicyHandler := embeddingpolicies.NewHandler(embPolicySvc)
	embeddingpolicies.RegisterRoutes(e, embPolicyHandler, authMiddleware)

	// Register branches routes
	branchesStore := branches.NewStore(db)
	branchesSvc := branches.NewService(branchesStore)
	branchesHandler := branches.NewHandler(branchesSvc)
	branches.RegisterRoutes(e, branchesHandler, authMiddleware)

	// Register chunks routes
	chunksRepo := chunks.NewRepository(db, log)
	chunksSvc := chunks.NewService(chunksRepo, log)
	chunksHandler := chunks.NewHandler(chunksSvc)
	chunks.RegisterRoutes(e, chunksHandler, authMiddleware)

	// Register search routes
	searchRepo := search.NewRepository(db, log)
	searchSvc := search.NewService(searchRepo, graphSvc, embeddingsSvc, log)
	searchHandler := search.NewHandler(searchSvc)
	search.RegisterRoutes(e, searchHandler, authMiddleware)

	// Register chat routes
	chatRepo := chat.NewRepository(db, log)
	chatSvc := chat.NewService(chatRepo, log)
	agentRepo := agents.NewRepository(db)
	chatHandler := chat.NewHandler(chatSvc, nil, searchSvc, nil, agentRepo, log) // nil LLM client, executor for tests
	chat.RegisterRoutes(e, chatHandler, authMiddleware)

	// Register MCP routes
	mcpSvc := mcp.NewService(db, graphSvc, searchSvc, testDB.Config, log)
	mcpHandler := mcp.NewHandler(mcpSvc, log)
	mcpSSEHandler := mcp.NewSSEHandler(mcpSvc, mcpHandler, log)
	mcpStreamableHandler := mcp.NewStreamableHTTPHandler(mcpSvc, log)
	mcp.RegisterRoutes(e, mcpHandler, mcpSSEHandler, mcpStreamableHandler, authMiddleware)

	// Register MCP registry routes
	mcpRegistryRepo := mcpregistry.NewRepository(db)
	mcpRegistryClient := mcpregistry.NewRegistryClient()
	mcpRegistrySvc := mcpregistry.NewService(mcpRegistryRepo, mcpSvc, mcpRegistryClient, log)
	mcpRegistryHandler := mcpregistry.NewHandler(mcpRegistrySvc)
	mcpregistry.RegisterRoutes(e, mcpRegistryHandler, authMiddleware)

	// Register useraccess routes
	useraccessSvc := useraccess.NewService(db)
	useraccessHandler := useraccess.NewHandler(useraccessSvc)
	useraccess.RegisterRoutes(e, useraccessHandler, authMiddleware)

	// Register invites routes
	invitesSvc := invites.NewService(db)
	invitesHandler := invites.NewHandler(invitesSvc)
	invites.RegisterRoutes(e, invitesHandler, authMiddleware)

	// Register events routes
	eventsSvc := events.NewService(log)
	eventsHandler := events.NewHandler(eventsSvc, log)
	events.RegisterRoutesManual(e, eventsHandler, authMiddleware)

	// Register tasks routes
	tasksRepo := tasks.NewRepository(db, log)
	tasksSvc := tasks.NewService(tasksRepo, log)
	tasksHandler := tasks.NewHandler(tasksSvc)
	tasks.RegisterRoutes(e, tasksHandler, authMiddleware)

	// Register notifications routes
	notificationsRepo := notifications.NewRepository(db, log)
	notificationsSvc := notifications.NewService(notificationsRepo, log)
	notificationsHandler := notifications.NewHandler(notificationsSvc)
	notifications.RegisterRoutes(e, notificationsHandler, authMiddleware)

	// Register template packs routes
	templatepacksRepo := templatepacks.NewRepository(db, log)
	templatepacksSvc := templatepacks.NewService(templatepacksRepo, log)
	templatepacksHandler := templatepacks.NewHandler(templatepacksSvc)
	templatepacks.RegisterRoutes(e, templatepacksHandler, authMiddleware)

	// Register user activity routes
	useractivityRepo := useractivity.NewRepository(db, log)
	useractivitySvc := useractivity.NewService(useractivityRepo, log)
	useractivityHandler := useractivity.NewHandler(useractivitySvc)
	useractivity.RegisterRoutes(e, useractivityHandler, authMiddleware)

	// Register superadmin routes
	superadminRepo := superadmin.NewRepository(db)
	superadminHandler := superadmin.NewHandler(superadminRepo)
	superadmin.RegisterRoutes(e, superadminHandler, authMiddleware)

	// Register type registry routes
	typeregistryRepo := typeregistry.NewRepository(db)
	typeregistryHandler := typeregistry.NewHandler(typeregistryRepo)
	typeregistry.RegisterRoutes(e, typeregistryHandler, authMiddleware)

	// Register agents routes
	agentsRepo := agents.NewRepository(db)
	agentsHandler := agents.NewHandler(agentsRepo, nil, nil)
	agents.RegisterRoutes(e, agentsHandler, authMiddleware)

	// Register extraction admin routes
	extractionJobsSvc := extraction.NewObjectExtractionJobsService(db, log, extraction.DefaultObjectExtractionConfig())
	extractionAdminHandler := extraction.NewAdminHandler(extractionJobsSvc)
	extraction.RegisterAdminRoutes(e, extractionAdminHandler, authMiddleware)

	// Register monitoring routes
	monitoringRepo := monitoring.NewRepository(db, log)
	monitoringHandler := monitoring.NewHandler(monitoringRepo)
	monitoring.RegisterRoutes(e, monitoringHandler, authMiddleware)

	// Register integrations routes
	integrationsRepo := integrations.NewRepository(db, log)
	integrationsRegistry := integrations.NewIntegrationRegistry()
	integrationsHandler := integrations.NewHandler(integrationsRepo, integrationsRegistry, encryptionSvc)
	integrations.RegisterRoutes(e, integrationsHandler, authMiddleware)

	// Register data source integrations routes
	datasourceRepo := datasource.NewRepository(db, log)
	datasourceJobsSvc := datasource.NewJobsService(testDB.DB, log, testDB.Config)
	datasourceRegistry := datasource.NewProviderRegistry()
	// Register placeholder providers for testing
	datasourceRegistry.Register(datasource.NewNoOpProvider("clickup"))
	datasourceRegistry.Register(datasource.NewNoOpProvider("imap"))
	datasourceRegistry.Register(datasource.NewNoOpProvider("gmail_oauth"))
	datasourceRegistry.Register(datasource.NewNoOpProvider("google_drive"))
	datasourceHandler := datasource.NewHandler(datasourceRepo, datasourceJobsSvc, datasourceRegistry, encryptionSvc, log)
	datasource.RegisterRoutes(e, datasourceHandler, authMiddleware)

	return &TestServer{
		Echo:           e,
		TestDB:         testDB,
		DB:             db,
		Config:         testDB.Config,
		Log:            log,
		AuthMiddleware: authMiddleware,
	}
}

// Request performs an HTTP request against the test server
func (s *TestServer) Request(method, path string, opts ...RequestOption) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, nil)

	// Apply options
	for _, opt := range opts {
		opt(req)
	}

	rec := httptest.NewRecorder()
	s.Echo.ServeHTTP(rec, req)
	return rec
}

// GET performs a GET request
func (s *TestServer) GET(path string, opts ...RequestOption) *httptest.ResponseRecorder {
	return s.Request(http.MethodGet, path, opts...)
}

// POST performs a POST request
func (s *TestServer) POST(path string, opts ...RequestOption) *httptest.ResponseRecorder {
	return s.Request(http.MethodPost, path, opts...)
}

// PUT performs a PUT request
func (s *TestServer) PUT(path string, opts ...RequestOption) *httptest.ResponseRecorder {
	return s.Request(http.MethodPut, path, opts...)
}

// DELETE performs a DELETE request
func (s *TestServer) DELETE(path string, opts ...RequestOption) *httptest.ResponseRecorder {
	return s.Request(http.MethodDelete, path, opts...)
}

// PATCH performs a PATCH request
func (s *TestServer) PATCH(path string, opts ...RequestOption) *httptest.ResponseRecorder {
	return s.Request(http.MethodPatch, path, opts...)
}

// RequestOption modifies an HTTP request
type RequestOption func(*http.Request)

// WithHeader adds a header to the request
func WithHeader(key, value string) RequestOption {
	return func(r *http.Request) {
		r.Header.Set(key, value)
	}
}

// WithAuth adds an Authorization header
func WithAuth(token string) RequestOption {
	return WithHeader("Authorization", "Bearer "+token)
}

// WithProjectID adds an X-Project-ID header
func WithProjectID(projectID string) RequestOption {
	return WithHeader("X-Project-ID", projectID)
}

// WithOrgID adds an X-Org-ID header
func WithOrgID(orgID string) RequestOption {
	return WithHeader("X-Org-ID", orgID)
}

// WithJSON adds Content-Type: application/json header
func WithJSON() RequestOption {
	return WithHeader("Content-Type", "application/json")
}

// WithBody adds a request body
func WithBody(body string) RequestOption {
	return func(r *http.Request) {
		r.Body = io.NopCloser(strings.NewReader(body))
		r.ContentLength = int64(len(body))
	}
}

// WithAPIToken adds an Authorization header without Bearer prefix (for API tokens)
func WithAPIToken(token string) RequestOption {
	return WithHeader("Authorization", "Bearer "+token)
}

// WithRawAuth adds a raw Authorization header value
func WithRawAuth(value string) RequestOption {
	return WithHeader("Authorization", value)
}

// WithJSONBody sets Content-Type to application/json and marshals the body to JSON
func WithJSONBody(body any) RequestOption {
	return func(r *http.Request) {
		data, err := json.Marshal(body)
		if err != nil {
			panic(err)
		}
		r.Header.Set("Content-Type", "application/json")
		r.Body = io.NopCloser(strings.NewReader(string(data)))
		r.ContentLength = int64(len(data))
	}
}

// MultipartForm represents a multipart form for testing file uploads
type MultipartForm struct {
	body        *bytes.Buffer
	writer      *multipart.Writer
	contentType string
}

// NewMultipartForm creates a new multipart form builder
func NewMultipartForm() *MultipartForm {
	body := new(bytes.Buffer)
	writer := multipart.NewWriter(body)
	return &MultipartForm{
		body:   body,
		writer: writer,
	}
}

// AddFile adds a file to the multipart form
func (m *MultipartForm) AddFile(fieldName, filename string, content []byte) error {
	part, err := m.writer.CreateFormFile(fieldName, filename)
	if err != nil {
		return err
	}
	_, err = part.Write(content)
	return err
}

// AddField adds a regular field to the multipart form
func (m *MultipartForm) AddField(fieldName, value string) error {
	return m.writer.WriteField(fieldName, value)
}

// Close finalizes the multipart form and returns the content type
func (m *MultipartForm) Close() string {
	m.writer.Close()
	m.contentType = m.writer.FormDataContentType()
	return m.contentType
}

// WithMultipartForm adds a multipart form body to the request
func WithMultipartForm(form *MultipartForm) RequestOption {
	return func(r *http.Request) {
		r.Header.Set("Content-Type", form.contentType)
		r.Body = io.NopCloser(bytes.NewReader(form.body.Bytes()))
		r.ContentLength = int64(form.body.Len())
	}
}
