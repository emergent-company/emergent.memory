package discoveryjobs

import (
	"context"
	"log/slog"
	"os"
	"strconv"
	"strings"
	"time"

	"go.uber.org/fx"

	"github.com/emergent-company/emergent.memory/domain/documents"
	"github.com/emergent-company/emergent.memory/domain/extraction"
	"github.com/emergent-company/emergent.memory/domain/mcp"
	"github.com/emergent-company/emergent.memory/domain/projects"
	"github.com/emergent-company/emergent.memory/internal/config"
	"github.com/emergent-company/emergent.memory/pkg/adk"
)

var Module = fx.Module("discoveryjobs",
	fx.Provide(
		NewRepository,
		NewService,
		NewHandler,
		provideDiscoveryServiceWithMCP,
	),
	fx.Invoke(
		RegisterRoutes,
		wireAutoApply,
		wireAutoBackfill,
		startQueueRunner,
	),
)

// startQueueRunner begins draining kb.discovery_requests in the background. It
// uses a cancelable background context (the fx OnStart ctx carries a short
// start timeout and would kill the runner seconds after boot), cancelled on
// shutdown via OnStop.
func startQueueRunner(lc fx.Lifecycle, svc *Service) {
	runnerCtx, cancel := context.WithCancel(context.Background())
	lc.Append(fx.Hook{
		OnStart: func(context.Context) error {
			go svc.StartQueueRunner(runnerCtx, 30*time.Second)
			return nil
		},
		OnStop: func(context.Context) error {
			cancel()
			return nil
		},
	})
}

// wireAutoApply injects the project-policy reader into the discovery service so
// pipeline_mode=auto projects can have their discovered schema applied at
// completion. discoveryjobs imports projects here (projects never imports
// discoveryjobs), so there is no cycle; fx resolves *projects.Service as part
// of the same application.
func wireAutoApply(svc *Service, proj *projects.Service) {
	if svc == nil || proj == nil {
		return
	}
	svc.SetPipelinePolicyReader(proj)
}

// wireAutoBackfill injects the extraction backfill trigger into the discovery
// service so a schema applied by auto-apply can re-extract documents when the
// policy asks for it. discoveryjobs imports extraction (extraction never imports
// discoveryjobs), so there is no cycle; fx resolves the jobs service as part of
// the same application.
func wireAutoBackfill(svc *Service, jobs *extraction.ObjectExtractionJobsService) {
	if svc == nil || jobs == nil {
		return
	}
	svc.SetBackfillTrigger(jobs)
}

// NewService creates a new discovery jobs service.
// modelFactory is injected by the adk.Module registered in main.go.
func NewService(repo *Repository, docSvc *documents.Service, cfg *config.Config, modelFactory *adk.ModelFactory, log *slog.Logger) *Service {
	return &Service{
		repo:         repo,
		docSvc:       docSvc,
		cfg:          cfg,
		modelFactory: modelFactory,
		log:          log.With(slog.String("scope", "discoveryjobs.svc")),
		maxDocuments: maxDocumentsFromEnv(),
		staleAfter:   staleJobAfterFromEnv(),
	}
}

// staleJobAfterFromEnv reads how long a non-terminal job may go without
// progress before a new start supersedes it (DISCOVERY_STALE_JOB_MINUTES,
// default DefaultStaleJobAfter). A non-positive value disables takeover, which
// restores the strict one-run-per-project refusal.
func staleJobAfterFromEnv() time.Duration {
	raw := strings.TrimSpace(os.Getenv("DISCOVERY_STALE_JOB_MINUTES"))
	if raw == "" {
		return DefaultStaleJobAfter
	}
	n, err := strconv.Atoi(raw)
	if err != nil {
		return DefaultStaleJobAfter
	}
	if n <= 0 {
		return 0
	}
	return time.Duration(n) * time.Minute
}

// maxDocumentsFromEnv reads the per-run document cap, defaulting to
// DefaultDiscoveryMaxDocuments. An unparsable or non-positive value is ignored
// rather than failing startup: the default is the safe value for a tuning knob.
func maxDocumentsFromEnv() int {
	raw := strings.TrimSpace(os.Getenv("DISCOVERY_MAX_DOCUMENTS"))
	if raw == "" {
		return DefaultDiscoveryMaxDocuments
	}
	n, err := strconv.Atoi(raw)
	if err != nil || n <= 0 {
		return DefaultDiscoveryMaxDocuments
	}
	return n
}

// provideDiscoveryServiceWithMCP exposes the discovery service as mcp.DiscoveryFinalizer
// so the finalize-discovery tool is available to agents.
func provideDiscoveryServiceWithMCP(svc *Service) mcp.DiscoveryFinalizer {
	return svc
}
