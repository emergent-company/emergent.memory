// Package main provides the entry point for the Emergent API server
//
// @title Emergent API
// @version 0.16.1
// @description Emergent Knowledge Base API - AI-powered knowledge management system
// @contact.name Emergent Team
// @contact.url https://emergent-company.ai
// @contact.email support@emergent-company.ai
// @license.name Proprietary
// @host localhost:5300
// @BasePath /
// @schemes http https
//
// @securityDefinitions.apikey BearerAuth
// @in header
// @name Authorization
// @description OAuth 2.0 access token (format: "Bearer <token>")
package main

import (
	"log/slog"

	"github.com/joho/godotenv"
	"go.uber.org/fx"
	"go.uber.org/fx/fxevent"

	"github.com/emergent-company/emergent/domain/agents"
	"github.com/emergent-company/emergent/domain/apitoken"
	"github.com/emergent-company/emergent/domain/authinfo"
	"github.com/emergent-company/emergent/domain/backups"
	"github.com/emergent-company/emergent/domain/branches"
	"github.com/emergent-company/emergent/domain/chat"
	"github.com/emergent-company/emergent/domain/chunking"
	"github.com/emergent-company/emergent/domain/chunks"
	"github.com/emergent-company/emergent/domain/datasource"
	"github.com/emergent-company/emergent/domain/devtools"
	"github.com/emergent-company/emergent/domain/discoveryjobs"
	"github.com/emergent-company/emergent/domain/docs"
	"github.com/emergent-company/emergent/domain/documents"
	"github.com/emergent-company/emergent/domain/email"
	"github.com/emergent-company/emergent/domain/embeddingpolicies"
	"github.com/emergent-company/emergent/domain/events"
	"github.com/emergent-company/emergent/domain/extraction"
	"github.com/emergent-company/emergent/domain/githubapp"
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
	"github.com/emergent-company/emergent/domain/scheduler"
	"github.com/emergent-company/emergent/domain/search"
	"github.com/emergent-company/emergent/domain/standalone"
	"github.com/emergent-company/emergent/domain/superadmin"
	"github.com/emergent-company/emergent/domain/tasks"
	"github.com/emergent-company/emergent/domain/templatepacks"
	"github.com/emergent-company/emergent/domain/typeregistry"
	"github.com/emergent-company/emergent/domain/useraccess"
	"github.com/emergent-company/emergent/domain/useractivity"
	"github.com/emergent-company/emergent/domain/userprofile"
	"github.com/emergent-company/emergent/domain/users"
	"github.com/emergent-company/emergent/domain/workspace"
	"github.com/emergent-company/emergent/internal/config"
	"github.com/emergent-company/emergent/internal/database"
	"github.com/emergent-company/emergent/internal/server"
	"github.com/emergent-company/emergent/internal/storage"
	"github.com/emergent-company/emergent/pkg/adk"
	"github.com/emergent-company/emergent/pkg/auth"
	"github.com/emergent-company/emergent/pkg/embeddings"
	"github.com/emergent-company/emergent/pkg/kreuzberg"
	"github.com/emergent-company/emergent/pkg/logger"
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
		authinfo.Module,
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
		mcpregistry.Module,
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

		// Documentation API (serves markdown files from docs/public)
		docs.Module,

		// Agent workspace infrastructure (isolated execution environments)
		workspace.Module,

		// GitHub App integration (repository access, credential management)
		githubapp.Module,
	).Run()
}
