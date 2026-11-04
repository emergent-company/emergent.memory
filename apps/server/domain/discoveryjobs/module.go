package discoveryjobs

import (
	"log/slog"
	"os"
	"strconv"
	"strings"
	"time"

	"go.uber.org/fx"

	"github.com/emergent-company/emergent.memory/domain/documents"
	"github.com/emergent-company/emergent.memory/domain/mcp"
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
	),
)

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
