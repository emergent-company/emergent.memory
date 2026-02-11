package main

import (
	"log/slog"

	"github.com/joho/godotenv"
	"go.uber.org/fx"
	"go.uber.org/fx/fxevent"

	"github.com/emergent/emergent-core/domain/agents"
	"github.com/emergent/emergent-core/domain/apitoken"
	"github.com/emergent/emergent-core/domain/backups"
	"github.com/emergent/emergent-core/domain/branches"
	"github.com/emergent/emergent-core/domain/chat"
	"github.com/emergent/emergent-core/domain/chunking"
	"github.com/emergent/emergent-core/domain/chunks"
	"github.com/emergent/emergent-core/domain/datasource"
	"github.com/emergent/emergent-core/domain/devtools"
	"github.com/emergent/emergent-core/domain/discoveryjobs"
	"github.com/emergent/emergent-core/domain/documents"
	"github.com/emergent/emergent-core/domain/email"
	"github.com/emergent/emergent-core/domain/embeddingpolicies"
	"github.com/emergent/emergent-core/domain/events"
	"github.com/emergent/emergent-core/domain/extraction"
	"github.com/emergent/emergent-core/domain/graph"
	"github.com/emergent/emergent-core/domain/health"
	"github.com/emergent/emergent-core/domain/integrations"
	"github.com/emergent/emergent-core/domain/invites"
	"github.com/emergent/emergent-core/domain/mcp"
	"github.com/emergent/emergent-core/domain/monitoring"
	"github.com/emergent/emergent-core/domain/notifications"
	"github.com/emergent/emergent-core/domain/orgs"
	"github.com/emergent/emergent-core/domain/projects"
	"github.com/emergent/emergent-core/domain/scheduler"
	"github.com/emergent/emergent-core/domain/search"
	"github.com/emergent/emergent-core/domain/standalone"
	"github.com/emergent/emergent-core/domain/superadmin"
	"github.com/emergent/emergent-core/domain/tasks"
	"github.com/emergent/emergent-core/domain/templatepacks"
	"github.com/emergent/emergent-core/domain/typeregistry"
	"github.com/emergent/emergent-core/domain/useraccess"
	"github.com/emergent/emergent-core/domain/useractivity"
	"github.com/emergent/emergent-core/domain/userprofile"
	"github.com/emergent/emergent-core/domain/users"
	"github.com/emergent/emergent-core/internal/config"
	"github.com/emergent/emergent-core/internal/database"
	"github.com/emergent/emergent-core/internal/server"
	"github.com/emergent/emergent-core/internal/storage"
	"github.com/emergent/emergent-core/pkg/adk"
	"github.com/emergent/emergent-core/pkg/auth"
	"github.com/emergent/emergent-core/pkg/embeddings"
	"github.com/emergent/emergent-core/pkg/kreuzberg"
	"github.com/emergent/emergent-core/pkg/logger"
)

func main() {
	// Load .env files if present (for local development)
	// Order matters: .env.local overrides .env
	// Note: Load() won't overwrite existing vars, Overload() will
	_ = godotenv.Load("../../.env")
	_ = godotenv.Overload("../../.env.local") // Overload ensures local values take precedence

	fx.New(
		// Logging
		fx.WithLogger(func(log *slog.Logger) fxevent.Logger {
			return &fxevent.SlogLogger{Logger: log}
		}),

		// Infrastructure modules
		logger.Module,
		config.Module,
		database.Module,
		server.Module,
		storage.Module,

		// Auth module
		auth.Module,

		// Standalone mode bootstrap (auto-init default resources when STANDALONE_MODE=true)
		standalone.Module,

		// Embeddings module (provides embedding client)
		embeddings.Module,

		// Kreuzberg module (document extraction service client)
		kreuzberg.Module,

		// ADK module (Google Agent Development Kit for AI orchestration)
		adk.Module,

		// Domain modules
		health.Module,
		agents.Module,
		backups.Module,
		documents.Module,
		chunking.Module,
		chunks.Module,
		orgs.Module,
		projects.Module,
		users.Module,
		userprofile.Module,
		apitoken.Module,
		graph.Module,
		branches.Module,
		embeddingpolicies.Module,
		search.Module,
		chat.Module,
		mcp.Module,
		monitoring.Module,
		notifications.Module,
		superadmin.Module,
		tasks.Module,
		templatepacks.Module,
		typeregistry.Module,
		useraccess.Module,
		useractivity.Module,
		invites.Module,
		events.Module,
		integrations.Module,
		discoveryjobs.Module,

		// Extraction module (background workers for document parsing, embeddings, etc.)
		extraction.Module,

		// Email module (email job queue and worker)
		email.Module,

		// Data source sync module (syncs from external sources like ClickUp, Gmail)
		datasource.Module,

		// Scheduler module (cron-based scheduled tasks)
		scheduler.Module,

		// Developer tools (coverage, docs) - only enabled in debug mode
		devtools.Module,
	).Run()
}
