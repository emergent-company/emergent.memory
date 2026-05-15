// Package main provides the entry point for the Memory API server
//
// @title Memory API
// @version 0.41.51
// @description Memory Knowledge Base API - AI-powered knowledge management system
// @contact.name Memory Team
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

	"github.com/emergent-company/emergent.memory/domain/agents"
	"github.com/emergent-company/emergent.memory/domain/apitoken"
	"github.com/emergent-company/emergent.memory/domain/authinfo"
	"github.com/emergent-company/emergent.memory/domain/autoprovision"
	"github.com/emergent-company/emergent.memory/domain/backups"
	"github.com/emergent-company/emergent.memory/domain/branches"
	"github.com/emergent-company/emergent.memory/domain/chat"
	"github.com/emergent-company/emergent.memory/domain/chunking"
	"github.com/emergent-company/emergent.memory/domain/chunks"
	"github.com/emergent-company/emergent.memory/domain/devtools"
	"github.com/emergent-company/emergent.memory/domain/discoveryjobs"
	"github.com/emergent-company/emergent.memory/domain/docs"
	"github.com/emergent-company/emergent.memory/domain/documents"
	"github.com/emergent-company/emergent.memory/domain/email"
	"github.com/emergent-company/emergent.memory/domain/embeddingpolicies"
	"github.com/emergent-company/emergent.memory/domain/events"
	"github.com/emergent-company/emergent.memory/domain/extraction"
	"github.com/emergent-company/emergent.memory/domain/graph"
	"github.com/emergent-company/emergent.memory/domain/health"
	"github.com/emergent-company/emergent.memory/domain/invites"
	"github.com/emergent-company/emergent.memory/domain/journal"
	"github.com/emergent-company/emergent.memory/domain/mcp"
	"github.com/emergent-company/emergent.memory/domain/mcpregistry"
	"github.com/emergent-company/emergent.memory/domain/mcprelay"
	"github.com/emergent-company/emergent.memory/domain/monitoring"
	"github.com/emergent-company/emergent.memory/domain/notifications"
	"github.com/emergent-company/emergent.memory/domain/orgs"
	"github.com/emergent-company/emergent.memory/domain/projects"
	"github.com/emergent-company/emergent.memory/domain/provider"
	"github.com/emergent-company/emergent.memory/domain/sandbox"
	"github.com/emergent-company/emergent.memory/domain/sandboximages"
	"github.com/emergent-company/emergent.memory/domain/scheduler"
	"github.com/emergent-company/emergent.memory/domain/schemaregistry"
	"github.com/emergent-company/emergent.memory/domain/schemas"
	"github.com/emergent-company/emergent.memory/domain/search"
	"github.com/emergent-company/emergent.memory/domain/sessiontodos"
	"github.com/emergent-company/emergent.memory/domain/skills"
	"github.com/emergent-company/emergent.memory/domain/standalone"
	"github.com/emergent-company/emergent.memory/domain/superadmin"
	"github.com/emergent-company/emergent.memory/domain/tasks"
	"github.com/emergent-company/emergent.memory/domain/tracing"
	"github.com/emergent-company/emergent.memory/domain/useraccess"
	"github.com/emergent-company/emergent.memory/domain/useractivity"
	"github.com/emergent-company/emergent.memory/domain/userprofile"
	"github.com/emergent-company/emergent.memory/domain/users"
	"github.com/emergent-company/emergent.memory/internal/config"
	"github.com/emergent-company/emergent.memory/internal/database"
	"github.com/emergent-company/emergent.memory/internal/server"
	"github.com/emergent-company/emergent.memory/internal/storage"
	"github.com/emergent-company/emergent.memory/pkg/adk"
	"github.com/emergent-company/emergent.memory/pkg/auth"
	"github.com/emergent-company/emergent.memory/pkg/embeddings"
	"github.com/emergent-company/emergent.memory/pkg/kreuzberg"
	"github.com/emergent-company/emergent.memory/pkg/logger"
	"github.com/emergent-company/emergent.memory/pkg/whisper"
)

func main() {
	// Load .env files if present (for local development)
	// Order matters: .env.local overrides .env
	// Note: Load() won't overwrite existing vars, Overload() will
	_ = godotenv.Load("../../.env")
	_ = godotenv.Overload("../../.env.local") // Overload ensures local values take precedence

	// Load feature flags from env before building the fx app.
	cfg, err := config.NewConfig(slog.Default())
	if err != nil {
		panic("config load failed: " + err.Error())
	}
	features := cfg.Features

	opts := []fx.Option{coreFxOptions()}
	opts = append(opts, featureFxOptions(features)...)

	fx.New(opts...).Run()
}

// coreFxOptions returns the always-on fx options (infrastructure + core domains).
func coreFxOptions() fx.Option {
	return fx.Options(
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

		// Whisper module (audio transcription service client)
		whisper.Module,

		// ADK module (Google Agent Development Kit for AI orchestration)
		adk.Module,

		// Core domain modules (always on)
		health.Module,
		authinfo.Module,
		documents.Module,
		chunking.Module,
		chunks.Module,
		orgs.Module,
		projects.Module,
		autoprovision.Module,
		users.Module,
		userprofile.Module,
		apitoken.Module,
		graph.Module,
		branches.Module,
		embeddingpolicies.Module,
		search.Module,
		journal.Module,
		notifications.Module,
		tasks.Module,
		sessiontodos.Module,
		skills.Module,
		schemas.Module,
		schemaregistry.Module,
		useraccess.Module,
		useractivity.Module,
		invites.Module,
		events.Module,
		discoveryjobs.Module,

		// Provider module (credential resolution, model catalog, LLM usage tracking)
		provider.Module,

		// Extraction module (background workers for document parsing, embeddings, etc.)
		extraction.Module,

		// Adapter: wires extraction retry capability into documents handler (avoids import cycle)
		fx.Provide(NewExtractionRetrierAdapter),

		// Email module (email job queue and worker)
		email.Module,

		// Scheduler module (cron-based scheduled tasks)
		scheduler.Module,

		// Documentation API (serves markdown files from docs/public)
		docs.Module,

		// Cross-domain wiring: give projects.Service access to revoke tokens on member removal
		fx.Invoke(func(svc *projects.Service, tokenRepo *apitoken.Repository) {
			svc.SetTokenRevoker(tokenRepo)
		}),
	)
}

// featureFxOptions returns conditional fx options based on the FeatureSet.
// Each block is only included when its feature flag is true.
func featureFxOptions(f config.FeatureSet) []fx.Option {
	var opts []fx.Option

	if f.Agents {
		opts = append(opts, agents.Module)
	}
	if f.MCP {
		opts = append(opts, mcp.Module)
		// Cross-domain wiring: set_session_title writes title to graph object Properties
		// so clients using the graph API can read the updated title.
		opts = append(opts, fx.Invoke(func(mcpSvc *mcp.Service, graphSvc *graph.Service) {
			mcpSvc.SetGraphObjectPatcher(graphSvc.PatchGraphObjectTitle)
		}))
	}
	if f.MCPRegistry {
		opts = append(opts, mcpregistry.Module)
	}
	if f.MCPRelay {
		opts = append(opts, mcprelay.Module)
	}
	if f.Sandbox {
		opts = append(opts, sandbox.Module)
	}
	if f.SandboxImages {
		opts = append(opts, sandboximages.Module)
	}
	if f.Backups {
		opts = append(opts, backups.Module)
	}
	if f.Monitoring {
		opts = append(opts, monitoring.Module)
	}
	if f.Tracing {
		opts = append(opts, tracing.Module)
	}
	if f.Devtools {
		opts = append(opts, devtools.Module)
	}
	if f.Chat {
		opts = append(opts, chat.Module)
	}
	if f.SuperAdmin {
		opts = append(opts, superadmin.Module)
	}

	return opts
}
