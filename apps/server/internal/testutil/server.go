package testutil

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/joho/godotenv"
	"github.com/labstack/echo/v4"
	"github.com/uptrace/bun"
	"google.golang.org/adk/session"

	"github.com/emergent-company/emergent.memory/domain/agentcompat"
	"github.com/emergent-company/emergent.memory/domain/agents"
	"github.com/emergent-company/emergent.memory/domain/apitoken"
	"github.com/emergent-company/emergent.memory/domain/modelconfig"
	"github.com/emergent-company/emergent.memory/domain/authinfo"
	"github.com/emergent-company/emergent.memory/domain/branches"
	"github.com/emergent-company/emergent.memory/domain/chat"
	"github.com/emergent-company/emergent.memory/domain/chunks"
	"github.com/emergent-company/emergent.memory/domain/discoveryjobs"
	"github.com/emergent-company/emergent.memory/domain/documents"
	"github.com/emergent-company/emergent.memory/domain/embeddingpolicies"
	"github.com/emergent-company/emergent.memory/domain/events"
	"github.com/emergent-company/emergent.memory/domain/extraction"
	"github.com/emergent-company/emergent.memory/domain/graph"
	"github.com/emergent-company/emergent.memory/domain/health"
	"github.com/emergent-company/emergent.memory/domain/invites"
	"github.com/emergent-company/emergent.memory/domain/mcp"
	"github.com/emergent-company/emergent.memory/domain/mcpregistry"
	"github.com/emergent-company/emergent.memory/domain/monitoring"
	"github.com/emergent-company/emergent.memory/domain/notifications"
	"github.com/emergent-company/emergent.memory/domain/orgs"
	"github.com/emergent-company/emergent.memory/domain/projects"
	"github.com/emergent-company/emergent.memory/domain/provider"
	"github.com/emergent-company/emergent.memory/domain/sandbox"
	"github.com/emergent-company/emergent.memory/domain/schemaregistry"
	"github.com/emergent-company/emergent.memory/domain/schemas"
	"github.com/emergent-company/emergent.memory/domain/search"
	"github.com/emergent-company/emergent.memory/domain/skills"
	"github.com/emergent-company/emergent.memory/domain/superadmin"
	"github.com/emergent-company/emergent.memory/domain/tasks"
	"github.com/emergent-company/emergent.memory/domain/tracing"
	"github.com/emergent-company/emergent.memory/domain/useraccess"
	"github.com/emergent-company/emergent.memory/domain/useractivity"
	"github.com/emergent-company/emergent.memory/domain/userprofile"
	"github.com/emergent-company/emergent.memory/domain/users"
	"github.com/emergent-company/emergent.memory/internal/config"
	"github.com/emergent-company/emergent.memory/internal/storage"
	"github.com/emergent-company/emergent.memory/pkg/adk"
	"github.com/emergent-company/emergent.memory/pkg/adk/session/bunsession"
	"github.com/emergent-company/emergent.memory/pkg/apperror"
	"github.com/emergent-company/emergent.memory/pkg/auth"
	"github.com/emergent-company/emergent.memory/pkg/embeddings"
	"github.com/emergent-company/emergent.memory/pkg/encryption"
	"github.com/emergent-company/emergent.memory/pkg/kreuzberg"
	"github.com/emergent-company/emergent.memory/pkg/whisper"
)

// TestServer wraps an Echo instance for testing
type TestServer struct {
	Echo           *echo.Echo
	TestDB         *TestDB
	DB             bun.IDB
	Config         *config.Config
	Log            *slog.Logger
	AuthMiddleware *auth.Middleware
	// StopFn cancels background goroutines started by the server (e.g. extraction worker).
	// Call in TearDownTest / TearDownSuite to avoid goroutine leaks.
	StopFn func()
}

// NewTestServer creates a test server with all routes registered.
func NewTestServer(testDB *TestDB) *TestServer {
	return newTestServerWithDB(testDB, testDB.GetDB())
}

// LoadEnvFiles loads .env and .env.local from the repo root.
// It walks up from the caller's source file until it finds a go.mod, then
// loads .env and .env.local (local values win via Overload).
// Safe to call multiple times — failures are silently ignored so tests still
// run even if the files are absent.
func LoadEnvFiles() {
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		return
	}
	// Walk upward tracking every directory that contains a go.mod — the last
	// one found (topmost) is the repo root where .env / .env.local live.
	// This is necessary because the source file may be nested under a module
	// subdirectory (e.g. apps/server/go.mod) that would be found first.
	dir := filepath.Dir(thisFile)
	var rootDir string
	for i := 0; i < 15; i++ {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			rootDir = dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}
	if rootDir != "" {
		_ = godotenv.Load(filepath.Join(rootDir, ".env"))
		_ = godotenv.Overload(filepath.Join(rootDir, ".env.local"))
	}
}

// NewTestServerWithLLM creates a test server wired with a real LLM provider
// loaded from .env / .env.local files at the repo root.
// If no LLM credentials are found the server falls back to the same behaviour
// as NewTestServer (nil modelFactory), so callers must guard with skipIfNoLLM().
func NewTestServerWithLLM(testDB *TestDB) *TestServer {
	LoadEnvFiles()

	log := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelWarn}))

	// Re-parse config now that env vars are loaded from .env files.
	cfg, err := config.NewConfig(log)
	if err != nil || !cfg.LLM.IsEnabled() {
		// No LLM credentials — fall back to nil-LLM server.
		return newTestServerWithDB(testDB, testDB.GetDB())
	}

	db := testDB.GetDB()

	// Build ModelFactory from env-var credentials (no DB resolver needed in tests).
	modelFactory := adk.NewModelFactory(&cfg.LLM, log, nil, nil, nil)

	// Shared repositories / services — must match signatures in newTestServerWithDB.
	encryptionSvc := encryption.NewService(testDB.DB, log)
	agentRepo := agents.NewRepository(db)
	skillsRepo := skills.NewRepository(db, log)
	providerRepo := provider.NewRepository(db, log)
	eventsSvc := events.NewService(log)
	apitokenRepo := apitoken.NewRepository(db, log)
	apitokenSvc := apitoken.NewService(db, apitokenRepo, encryptionSvc, log)

	sessionSvc := session.Service(bunsession.NewService(testDB.DB))

	// Build real per-project embedding service so search-hybrid returns semantic
	// results in tests. Uses the provider credentials seeded by SetupFullTestProject.
	providerRegistry := provider.NewRegistry()
	providerCatalogSvc := provider.NewModelCatalogService(providerRepo, log)
	credSvc := provider.NewCredentialService(providerRepo, providerRegistry, providerCatalogSvc, cfg, log)
	modelconfigStore := modelconfig.NewStore(db, log)
	modelconfigSvc := modelconfig.NewService(modelconfigStore, log)
	embeddingResolver := modelconfig.NewEmbeddingResolverAdapter(modelconfigSvc, credSvc)
	embeddingsSvc := embeddings.NewTestEmbeddingsService(embeddingResolver, log)

	testGraphCfg := &config.Config{}
	testGraphCfg.Graph.MaxBatchObjects = 500
	testGraphCfg.Graph.MaxBatchRelationships = 500
	testGraphCfg.Graph.MaxListLimit = 1000
	testGraphCfg.Graph.DefaultListLimit = 100
	graphRepo := graph.NewRepository(db, log, testGraphCfg)
	graphSchemaProvider := graph.ProvideSchemaProvider(db, log)

	// Wire embedding enqueuer so entity-create triggers async embedding jobs.
	graphEmbJobsSvc := extraction.NewGraphEmbeddingJobsService(db, log, extraction.DefaultGraphEmbeddingConfig())
	graphEmbEnqueuer := extraction.NewEmbeddingEnqueuerAdapter(graphEmbJobsSvc)
	graphSvc := graph.NewService(graphRepo, log, graphSchemaProvider, graph.ProvideInverseTypeProvider(db, log), embeddingsSvc, graphEmbEnqueuer, nil, nil, nil, nil)

	docsRepo := documents.NewRepository(db, log)
	docsSvc := documents.NewService(docsRepo, log)

	searchRepo := search.NewRepository(db, log)
	searchSvc := search.NewService(searchRepo, graphSvc, embeddingsSvc, log)

	mcpSvc := mcp.NewService(mcp.ServiceParams{
		DB:           db,
		GraphService: graphSvc,
		SearchSvc:    searchSvc,
		Cfg:          testDB.Config,
		Log:          log,
		DocumentsSvc: docsSvc,
		SkillsRepo:   skillsRepo,
		ApitokenSvc:  apitokenSvc,
	})

	// Wire discovery service so finalize-discovery tool works in-process.
	discoveryRepo := discoveryjobs.NewRepository(db, log)
	discoverySvc := discoveryjobs.NewService(discoveryRepo, docsSvc, testDB.Config, modelFactory, log)
	mcpSvc.SetDiscoveryService(discoverySvc)

	toolPool := agents.NewToolPool(agents.ToolPoolConfig{
		MCPService: mcpSvc,
		Logger:     log,
	})

	executor := agents.NewAgentExecutor(
		modelFactory,
		toolPool,
		agentRepo,
		skillsRepo,
		embeddingsSvc,
		nil, // sandbox provisioner — disabled in tests
		cfg,
		sessionSvc,
		nil, // model limits lookup
		apitokenSvc,
		nil, // usage service
		eventsSvc,
		log,
	)

	// Build domain classifier so pre-classification works in-process tests.
	extractionSchemaProvider := extraction.NewMemorySchemaProvider(db, log)
	domainClassifier := extraction.NewDomainClassifierMCPAdapter(modelFactory, extractionSchemaProvider, docsSvc, log)

	// Wire all domain MCP adapters — mirrors registerDomainToolsWithMCP in module.go.
	jobsCfg := extraction.DefaultObjectExtractionConfig()
	objJobsSvc := extraction.NewObjectExtractionJobsService(db, log, jobsCfg)
	reextractionQueuer := extraction.NewReextractionQueuerMCPAdapter(objJobsSvc)
	schemaIndex := extraction.NewSchemaIndexMCPAdapter(extractionSchemaProvider)
	mcpSvc.SetDomainClassifier(domainClassifier)
	mcpSvc.SetSchemaIndex(schemaIndex)
	mcpSvc.SetReextractionQueuer(reextractionQueuer)
	mcpSvc.SetDocumentSignalsReader(docsSvc)

	// Start background extraction worker so queued jobs actually run.
	// Uses a fast poll interval (2s) so tests don't wait long.
	workerCfg := extraction.DefaultObjectExtractionWorkerConfig()
	workerCfg.PollInterval = 2 * time.Second
	workerCfg.Concurrency = 2
	worker := extraction.NewObjectExtractionWorker(
		objJobsSvc,
		graphSvc,
		nil, // branchService — objects go to main graph
		docsSvc,
		extractionSchemaProvider,
		modelFactory,
		embeddingsSvc,
		workerCfg,
		log,
		nil, // concurrency scaler
	)
	workerCtx, workerCancel := context.WithCancel(context.Background())
	worker.Start(workerCtx)

	// Start graph embedding worker so entity-create triggers real embeddings.
	// Uses a fast poll interval (2s) matching the extraction worker.
	graphEmbWorkerCfg := extraction.DefaultGraphEmbeddingConfig()
	graphEmbWorkerCfg.WorkerIntervalMs = 2000
	graphEmbWorker := extraction.NewGraphEmbeddingWorker(
		graphEmbJobsSvc,
		embeddingsSvc,
		db,
		graphEmbWorkerCfg,
		log,
		nil, // concurrency scaler
		nil, // usage recorder
		nil, // budget checker
		false,
	)
	graphEmbWorkerCtx, graphEmbWorkerCancel := context.WithCancel(context.Background())
	_ = graphEmbWorker.Start(graphEmbWorkerCtx)

	// Build the base server (registers all routes with nil LLM).
	ts := newTestServerWithDB(testDB, db)
	ts.StopFn = func() {
		workerCancel()
		worker.Stop()
		graphEmbWorkerCancel()
		_ = graphEmbWorker.Stop(context.Background())
	}

	// Re-register chat routes with the live executor + modelFactory, overriding
	// the nil-LLM registration from newTestServerWithDB.
	chatRepo := chat.NewRepository(db, log)
	chatSvc := chat.NewService(chatRepo, log)

	chatHandler := chat.NewHandler(
		chatSvc,
		nil, // legacy vertex client — unused when modelFactory is set
		searchSvc,
		executor,
		agentRepo,
		credSvc,
		modelFactory,
		apitokenSvc,
		docsSvc,
		nil,              // uploadHandler — file upload not needed in tests
		domainClassifier, // pre-classify documents before agent runs
		cfg,
		log,
	)
	chat.RegisterRoutes(ts.Echo, chatHandler, ts.AuthMiddleware)

	// Register OpenAI-compatible agentcompat routes (/v1/chat/completions, /v1/models).
	agentCompatSvc := agentcompat.NewService(agentRepo, executor, log)
	agentCompatHandler := agentcompat.NewHandler(agentCompatSvc)
	agentcompat.RegisterRoutes(ts.Echo, agentCompatHandler, ts.AuthMiddleware)

	return ts
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
	authMiddleware := auth.NewMiddleware(auth.MiddlewareParams{
		DB:      db,
		Cfg:     testDB.Config,
		Log:     log,
		UserSvc: userSvc,
	})

	// Create shared services used by multiple route registrations
	storageCfg := storage.NewConfig()
	storageSvc, _ := storage.NewService(storageCfg, log)
	kreuzbergClient := kreuzberg.NewClient(testDB.Config, log)
	whisperClient := whisper.NewClient(testDB.Config, log)
	embeddingsSvc := embeddings.NewNoopService(log)

	// Register health routes (public)
	healthHandler := health.NewHandler(testDB.Pool, testDB.Config, storageSvc, kreuzbergClient, whisperClient, embeddingsSvc)
	e.GET("/health", healthHandler.Health)
	e.GET("/healthz", healthHandler.Healthz)
	e.GET("/ready", healthHandler.Ready)
	e.GET("/debug", healthHandler.Debug)

	// Register auth info routes
	authInfoHandler := authinfo.NewHandler(db, testDB.Config)
	authinfo.RegisterRoutes(e, authInfoHandler, authMiddleware)

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
	docsHandler := documents.NewHandler(docsSvc, storageSvc, nil, log)
	uploadHandler := documents.NewUploadHandler(docsSvc, storageSvc, nil, nil, &config.Config{}, log)
	documents.RegisterRoutes(e, docsHandler, uploadHandler, authMiddleware)

	// Register orgs routes
	orgsRepo := orgs.NewRepository(db, log)
	orgsSvc := orgs.NewService(orgsRepo, log)
	orgsHandler := orgs.NewHandler(orgsSvc)
	orgs.RegisterRoutes(e, orgsHandler, authMiddleware)

	// Register projects routes
	projectsRepo := projects.NewRepository(db, log)
	agentRepo := agents.NewRepository(db)
	projectsSvc := projects.NewService(projectsRepo, agentRepo, log)
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

	// Create encryption service (used by apitoken)
	encryptionSvc := encryption.NewService(testDB.DB, log)

	// Register apitoken routes
	apitokenRepo := apitoken.NewRepository(db, log)
	apitokenSvc := apitoken.NewService(db, apitokenRepo, encryptionSvc, log)
	apitokenHandler := apitoken.NewHandler(apitokenSvc, userSvc)
	apitoken.RegisterRoutes(e, apitokenHandler, authMiddleware)

	// Register graph routes
	testGraphCfg := &config.Config{}
	testGraphCfg.Graph.MaxBatchObjects = 500
	testGraphCfg.Graph.MaxBatchRelationships = 500
	testGraphCfg.Graph.MaxListLimit = 1000
	testGraphCfg.Graph.DefaultListLimit = 100
	graphRepo := graph.NewRepository(db, log, testGraphCfg)
	graphSchemaProvider := graph.ProvideSchemaProvider(db, log)
	graphSvc := graph.NewService(graphRepo, log, graphSchemaProvider, graph.ProvideInverseTypeProvider(db, log), embeddingsSvc, nil, nil, nil, nil, nil)
	graphHandler := graph.NewHandler(graphSvc, testGraphCfg)
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
	chatHandler := chat.NewHandler(chatSvc, nil, searchSvc, nil, agentRepo, nil, nil, nil, nil, nil, nil, testDB.Config, log) // nil LLM client, executor, credSvc, modelFactory, apiTokenSvc, docSvc, uploadHandler, domainClassifier for tests
	chat.RegisterRoutes(e, chatHandler, authMiddleware)

	// Register MCP routes
	skillsRepo := skills.NewRepository(db, log)
	mcpSvc := mcp.NewService(mcp.ServiceParams{
		DB:           db,
		GraphService: graphSvc,
		SearchSvc:    searchSvc,
		Cfg:          testDB.Config,
		Log:          log,
		DocumentsSvc: docsSvc,
		SkillsRepo:   skillsRepo,
		ApitokenSvc:  apitokenSvc,
	})
	mcpHandler := mcp.NewHandler(mcpSvc, log, userSvc)
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

	// Register invites routes (nil email service in test mode — emails are no-op)
	invitesSvc := invites.NewService(db, nil, &config.Config{}, log)
	invitesHandler := invites.NewHandler(invitesSvc, &config.Config{})
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

	// Register schemas routes
	schemasRepo := schemas.NewRepository(db, log)
	schemasSvc := schemas.NewService(schemasRepo, graphSvc, log)
	schemasHandler := schemas.NewHandler(schemasSvc)
	schemas.RegisterRoutes(e, schemasHandler, authMiddleware)

	// Register skills routes (skillsRepo already created above for MCP injection)
	skillsHandler := skills.NewHandler(skillsRepo, embeddingsSvc, log)
	skills.RegisterRoutes(e, skillsHandler, authMiddleware)

	// Register user activity routes
	useractivityRepo := useractivity.NewRepository(db, log)
	useractivitySvc := useractivity.NewService(useractivityRepo, log)
	useractivityHandler := useractivity.NewHandler(useractivitySvc)
	useractivity.RegisterRoutes(e, useractivityHandler, authMiddleware)

	// Register superadmin routes
	superadminRepo := superadmin.NewRepository(db)
	superadminHandler := superadmin.NewHandler(superadminRepo, apitokenSvc)
	superadmin.RegisterRoutes(e, superadminHandler, authMiddleware)

	// Register type registry routes
	schemaregistryRepo := schemaregistry.NewRepository(db)
	schemaregistryHandler := schemaregistry.NewHandler(schemaregistryRepo)
	schemaregistry.RegisterRoutes(e, schemaregistryHandler, authMiddleware)

	// Register agents routes
	agentsRepo := agents.NewRepository(db)
	sandboxStore := sandbox.NewStore(db)
	providerRepo := provider.NewRepository(db, log)
	agentsHandler := agents.NewHandler(agentsRepo, nil, nil, "", nil, nil, providerRepo, sandboxStore)
	agents.RegisterRoutes(e, agentsHandler, authMiddleware)

	// Register extraction admin routes
	extractionJobsSvc := extraction.NewObjectExtractionJobsService(db, log, extraction.DefaultObjectExtractionConfig())
	extractionAdminHandler := extraction.NewAdminHandler(extractionJobsSvc)
	extraction.RegisterAdminRoutes(e, extractionAdminHandler, authMiddleware)

	// Register monitoring routes
	monitoringRepo := monitoring.NewRepository(db, log)
	monitoringHandler := monitoring.NewHandler(monitoringRepo)
	monitoring.RegisterRoutes(e, monitoringHandler, authMiddleware)

	// Register provider routes (LLM credential management, model catalog, usage)
	providerRegistry := provider.NewRegistry()
	providerCatalogSvc := provider.NewModelCatalogService(providerRepo, log)
	providerCredSvc := provider.NewCredentialService(providerRepo, providerRegistry, providerCatalogSvc, testDB.Config, log)
	providerHandler := provider.NewHandler(providerCredSvc, providerCatalogSvc, providerRepo)
	provider.RegisterRoutes(e, providerHandler, authMiddleware)

	// Register discovery jobs routes (nil modelFactory — LLM-dependent tests skip in-process)
	discoveryRepo := discoveryjobs.NewRepository(db, log)
	discoverySvc := discoveryjobs.NewService(discoveryRepo, docsSvc, testDB.Config, (*adk.ModelFactory)(nil), log)
	discoveryHandler := discoveryjobs.NewHandler(discoverySvc)
	discoveryjobs.RegisterRoutes(e, discoveryHandler, authMiddleware)

	// Register tracing (Tempo proxy) routes.
	// When cfg.Otel.Enabled() == false, GetTrace returns 503 — tests react accordingly.
	tracingHandler := tracing.NewHandler(testDB.Config)
	tracing.RegisterRoutes(e, tracingHandler, authMiddleware)

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

// WithAPIKey adds an X-API-Key header (used for standalone-mode tokens)
func WithAPIKey(key string) RequestOption {
	return WithHeader("X-API-Key", key)
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
