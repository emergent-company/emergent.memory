// Package main provides the entry point for the Memory API server
//
// @title Memory API
// @version 0.87.1
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
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/joho/godotenv"
	"github.com/uptrace/bun"
	"go.uber.org/fx"
	"go.uber.org/fx/fxevent"

	"github.com/emergent-company/emergent.memory/domain/agentcompat"
	"github.com/emergent-company/emergent.memory/domain/agents"
	"github.com/emergent-company/emergent.memory/domain/apitoken"
	"github.com/emergent-company/emergent.memory/domain/authinfo"
	"github.com/emergent-company/emergent.memory/domain/autoprovision"
	"github.com/emergent-company/emergent.memory/domain/backups"
	"github.com/emergent-company/emergent.memory/domain/blueprints"
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
	"github.com/emergent-company/emergent.memory/domain/modelconfig"
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
	"github.com/emergent-company/emergent.memory/domain/sources"
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
	"github.com/emergent-company/emergent.memory/pkg/governor"
	"github.com/emergent-company/emergent.memory/pkg/logger"
	"github.com/emergent-company/emergent.memory/pkg/whisper"
	"github.com/emergent-company/emergent.memory/pkg/xberg"
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
	// fx bounds every App.Stop with its own stop timeout (DefaultTimeout, 15s).
	// StartServer's per-hook shutdownCtx is derived from that parent, so
	// context.WithTimeout can never EXTEND it: without headroom here the
	// configured SHUTDOWN_TIMEOUT (default 30s) is silently capped at 15s and
	// long retrieval/embedding requests are still cancelled early (issue
	// #1481). Keep this strictly greater than ShutdownTimeout.
	opts = append(opts, fx.StopTimeout(cfg.ShutdownTimeout+5*time.Second))

	fx.New(opts...).Run()
}

// coreFxOptions returns the always-on fx options (infrastructure + core domains).
func coreFxOptions() fx.Option {
	return fx.Options(
		// Logging
		fx.WithLogger(func(log *slog.Logger) fxevent.Logger {
			return &fxevent.SlogLogger{Logger: log}
		}),

		// Infrastructure modules.
		// database.Module must provide *bun.DB before server.StartServer runs;
		// StartServer depends on *bun.DB so fx orders construction (and thus
		// reverse-order shutdown: server drains, then bun, then the pool).
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

		// Xberg module (document extraction service client)
		xberg.Module,

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
		blueprints.Module,
		schemaregistry.Module,
		useraccess.Module,
		useractivity.Module,
		invites.Module,
		events.Module,
		discoveryjobs.Module,
		sources.Module,

		// Provider module (credential resolution, model catalog, LLM usage tracking)
		provider.Module,
		modelconfig.Module,

		// Extraction module (background workers for document parsing, embeddings, etc.)
		extraction.Module,

		// Resource governor / admission control (depends on syshealth.Monitor
		// provided by the extraction module; injected by graph + extraction).
		governor.Module,

		// Adapter: wires extraction retry capability into documents handler (avoids import cycle)
		fx.Provide(NewExtractionRetrierAdapter),

		// Email module (email job queue and worker)
		email.Module,

		// Scheduler module (cron-based scheduled tasks)
		scheduler.Module,

		// Documentation API (serves markdown files from docs/public)
		docs.Module,

		// Cross-domain wiring: expose apitoken.Repository as projects.TokenRevoker
		fx.Provide(func(tokenRepo *apitoken.Repository) projects.TokenRevoker {
			return tokenRepo
		}),

		// Cross-domain wiring: expose branches.Store as projects.BranchReader
		fx.Provide(func(branchStore *branches.Store) projects.BranchReader {
			return branchStore
		}),

		// Cross-domain wiring: expose orgs.Repository as projects.OrgMembershipReader
		fx.Provide(func(orgRepo *orgs.Repository) projects.OrgMembershipReader {
			return orgRepo
		}),
	)
}

// featureFxOptions returns conditional fx options based on the FeatureSet.
// Each block is only included when its feature flag is true.
func featureFxOptions(f config.FeatureSet) []fx.Option {
	var opts []fx.Option

	if f.Agents {
		opts = append(opts, agents.Module)
		opts = append(opts, agentcompat.Module)
		// Expose agents.Repository as blueprints.AgentRepo (agents feature only).
		opts = append(opts, fx.Provide(func(agentRepo *agents.Repository) blueprints.AgentRepo {
			return agentRepo
		}))
	}
	if f.MCP {
		opts = append(opts, mcp.Module)
		// Cross-domain wiring: set_session_title writes title to graph object Properties
		// so clients using the graph API can read the updated title.
		opts = append(opts, fx.Provide(func(graphSvc *graph.Service) mcp.GraphObjectPatcher {
			return graphSvc.PatchGraphObjectTitle
		}))
		opts = append(opts, fx.Provide(func(agentsRepo *agents.Repository) mcp.SessionHistoryProvider {
			return agentsRepo
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
		// Wire the agents deletion + restore seams into the backup restorer so
		// an overwrite restore tears down registrations for agent ids it
		// removes and re-registers the ids it re-creates or adds. Only possible
		// when the agents feature is also enabled.
		if f.Agents {
			opts = append(opts, fx.Invoke(func(r *backups.Restorer, agentRepo *agents.Repository) {
				r.SetAgentDeletionNotifier(agentRepo.NotifyAgentsDeleted)
				r.SetAgentRestoreNotifier(agentRepo.NotifyAgentsRestored)
			}))
		}
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
	if f.MCP && f.SuperAdmin {
		// Operator MCP surface: expose the superadmin repository + apitoken mint
		// as the mcp.SuperadminToolHandler seam, and the DB-backed audit writer.
		opts = append(opts,
			fx.Provide(func(repo *superadmin.Repository, tokenSvc *apitoken.Service) mcp.SuperadminToolHandler {
				return newSuperadminToolHandlerAdapter(repo, tokenSvc)
			}),
			fx.Provide(func(db bun.IDB, log *slog.Logger) mcp.SuperadminAuditWriter {
				return mcp.NewSuperadminAuditWriter(db, log)
			}),
		)
	}

	return opts
}

// ============================================================================
// Superadmin MCP adapter
// ============================================================================

// superadminToolHandlerAdapter implements mcp.SuperadminToolHandler by wrapping
// the superadmin repository and the apitoken service. It lives in package main
// so domain/mcp does not import domain/superadmin into its constructor graph
// (design: the seam is defined in domain/mcp, the adapter in cmd/server).
type superadminToolHandlerAdapter struct {
	repo     *superadmin.Repository
	tokenSvc *apitoken.Service
}

func newSuperadminToolHandlerAdapter(repo *superadmin.Repository, tokenSvc *apitoken.Service) mcp.SuperadminToolHandler {
	return &superadminToolHandlerAdapter{repo: repo, tokenSvc: tokenSvc}
}

func (a *superadminToolHandlerAdapter) SuperadminGet(ctx context.Context) (any, error) {
	user := auth.UserFromContext(ctx)
	if user == nil || user.ID == "" {
		return nil, fmt.Errorf("superadmin_get: no authenticated user")
	}
	ok, role, err := a.repo.IsSuperadmin(ctx, user.ID)
	if err != nil {
		return nil, err
	}
	if !ok {
		role = ""
	}
	return map[string]any{"user_id": user.ID, "role": role}, nil
}

func (a *superadminToolHandlerAdapter) ListUsers(ctx context.Context, page, limit int, search, orgID string) (any, error) {
	users, total, err := a.repo.ListUsers(ctx, page, limit, search, orgID)
	if err != nil {
		return nil, err
	}
	return map[string]any{"users": users, "total": total, "page": page, "limit": limit}, nil
}

func (a *superadminToolHandlerAdapter) GetUser(ctx context.Context, userID string) (any, error) {
	return a.repo.GetUser(ctx, userID)
}

func (a *superadminToolHandlerAdapter) ListOrganizations(ctx context.Context, page, limit int) (any, error) {
	orgs, total, err := a.repo.ListOrganizations(ctx, page, limit)
	if err != nil {
		return nil, err
	}
	return map[string]any{"organizations": orgs, "total": total, "page": page, "limit": limit}, nil
}

func (a *superadminToolHandlerAdapter) GetOrg(ctx context.Context, orgID string) (any, error) {
	return a.repo.GetOrg(ctx, orgID)
}

func (a *superadminToolHandlerAdapter) ListProjects(ctx context.Context, page, limit int, orgID string) (any, error) {
	projects, total, err := a.repo.ListProjects(ctx, page, limit, orgID)
	if err != nil {
		return nil, err
	}
	return map[string]any{"projects": projects, "total": total, "page": page, "limit": limit}, nil
}

func (a *superadminToolHandlerAdapter) GetProject(ctx context.Context, projectID string) (any, error) {
	return a.repo.GetProject(ctx, projectID)
}

func (a *superadminToolHandlerAdapter) ListProjectMembers(ctx context.Context, projectID string) (any, error) {
	return a.repo.ListProjectMembers(ctx, projectID)
}

func (a *superadminToolHandlerAdapter) ListEmailJobs(ctx context.Context, page, limit int, status, recipient, fromDate, toDate string) (any, error) {
	jobs, total, err := a.repo.ListEmailJobs(ctx, page, limit, status, recipient, fromDate, toDate)
	if err != nil {
		return nil, err
	}
	return map[string]any{"email_jobs": jobs, "total": total, "page": page, "limit": limit}, nil
}

func (a *superadminToolHandlerAdapter) GetEmailJob(ctx context.Context, id string) (any, error) {
	return a.repo.GetEmailJob(ctx, id)
}

func (a *superadminToolHandlerAdapter) ListEmbeddingJobs(ctx context.Context, page, limit int, status string, hasError *bool, projectID, jobType string) (any, error) {
	var allJobs []superadmin.EmbeddingJobDTO
	var total int
	if jobType == "" || jobType == "graph" {
		jobs, n, err := a.repo.ListGraphEmbeddingJobs(ctx, page, limit, status, hasError, projectID)
		if err != nil {
			return nil, err
		}
		allJobs = append(allJobs, jobs...)
		total += n
	}
	if jobType == "" || jobType == "chunk" {
		jobs, n, err := a.repo.ListChunkEmbeddingJobs(ctx, page, limit, status, hasError, projectID)
		if err != nil {
			return nil, err
		}
		allJobs = append(allJobs, jobs...)
		total += n
	}
	stats, err := a.repo.GetEmbeddingJobStats(ctx)
	if err != nil {
		return nil, err
	}
	return map[string]any{"jobs": allJobs, "stats": stats, "total": total, "page": page, "limit": limit}, nil
}

func (a *superadminToolHandlerAdapter) GetEmbeddingJobStats(ctx context.Context) (any, error) {
	return a.repo.GetEmbeddingJobStats(ctx)
}

func (a *superadminToolHandlerAdapter) ListExtractionJobs(ctx context.Context, page, limit int, status, jobType, projectID string, hasError *bool) (any, error) {
	jobs, total, err := a.repo.ListExtractionJobs(ctx, page, limit, status, jobType, projectID, hasError)
	if err != nil {
		return nil, err
	}
	stats, err := a.repo.GetExtractionJobStats(ctx)
	if err != nil {
		return nil, err
	}
	return map[string]any{"jobs": jobs, "stats": stats, "total": total, "page": page, "limit": limit}, nil
}

func (a *superadminToolHandlerAdapter) GetExtractionJobStats(ctx context.Context) (any, error) {
	return a.repo.GetExtractionJobStats(ctx)
}

func (a *superadminToolHandlerAdapter) ListDocumentParsingJobs(ctx context.Context, page, limit int, status, projectID string, hasError *bool) (any, error) {
	jobs, total, err := a.repo.ListDocumentParsingJobs(ctx, page, limit, status, projectID, hasError)
	if err != nil {
		return nil, err
	}
	stats, err := a.repo.GetDocumentParsingJobStats(ctx)
	if err != nil {
		return nil, err
	}
	return map[string]any{"jobs": jobs, "stats": stats, "total": total, "page": page, "limit": limit}, nil
}

func (a *superadminToolHandlerAdapter) GetDocumentParsingJobStats(ctx context.Context) (any, error) {
	return a.repo.GetDocumentParsingJobStats(ctx)
}

func (a *superadminToolHandlerAdapter) SoftDeleteUser(ctx context.Context, userID, deletedBy string) error {
	return a.repo.SoftDeleteUser(ctx, userID, deletedBy)
}

func (a *superadminToolHandlerAdapter) SoftDeleteOrg(ctx context.Context, orgID, deletedBy string) error {
	return a.repo.SoftDeleteOrg(ctx, orgID, deletedBy)
}

func (a *superadminToolHandlerAdapter) SoftDeleteProject(ctx context.Context, projectID, deletedBy string) error {
	return a.repo.SoftDeleteProject(ctx, projectID, deletedBy)
}

func (a *superadminToolHandlerAdapter) GrantSuperadmin(ctx context.Context, userID, grantedBy string, notes *string) error {
	return a.repo.GrantSuperadminToUser(ctx, userID, grantedBy, notes)
}

func (a *superadminToolHandlerAdapter) AddProjectMember(ctx context.Context, projectID, userID, role string) error {
	return a.repo.AddProjectMember(ctx, projectID, userID, role)
}

func (a *superadminToolHandlerAdapter) RemoveProjectMember(ctx context.Context, projectID, userID string) error {
	return a.repo.RemoveProjectMember(ctx, projectID, userID)
}

func (a *superadminToolHandlerAdapter) CreateServiceToken(ctx context.Context, name string, notes *string, grantedBy string) (any, error) {
	zitadelID := fmt.Sprintf("svc_%s", name)
	displayName := fmt.Sprintf("Service Account: %s", name)
	userID, err := a.repo.CreateServiceUser(ctx, zitadelID, displayName)
	if err != nil {
		return nil, err
	}
	if err := a.repo.GrantSuperadminToUser(ctx, userID, grantedBy, notes); err != nil {
		return nil, err
	}
	result, err := a.tokenSvc.CreateAccountToken(ctx, userID, name, serviceTokenScopes)
	if err != nil {
		return nil, err
	}
	return map[string]any{
		"user_id":  userID,
		"token_id": result.ID,
		"token":    result.Token,
		"name":     name,
	}, nil
}

func (a *superadminToolHandlerAdapter) DeleteEmbeddingJobs(ctx context.Context, ids []string, jobType string) (int, error) {
	if jobType == "chunk" {
		return a.repo.DeleteChunkEmbeddingJobs(ctx, ids)
	}
	return a.repo.DeleteGraphEmbeddingJobs(ctx, ids)
}

func (a *superadminToolHandlerAdapter) CleanupEmbeddingJobs(ctx context.Context) (int, error) {
	return a.repo.CleanupOrphanEmbeddingJobs(ctx)
}

func (a *superadminToolHandlerAdapter) ResetEmbeddingJobs(ctx context.Context) (int, error) {
	return a.repo.ResetDeadLetterEmbeddingJobs(ctx)
}

func (a *superadminToolHandlerAdapter) DeleteExtractionJobs(ctx context.Context, ids []string) (int, error) {
	return a.repo.DeleteExtractionJobs(ctx, ids)
}

func (a *superadminToolHandlerAdapter) CancelExtractionJobs(ctx context.Context, ids []string) (int, error) {
	return a.repo.CancelExtractionJobs(ctx, ids)
}

func (a *superadminToolHandlerAdapter) DeleteDocumentParsingJobs(ctx context.Context, ids []string) (int, error) {
	return a.repo.DeleteDocumentParsingJobs(ctx, ids)
}

func (a *superadminToolHandlerAdapter) RetryDocumentParsingJobs(ctx context.Context, ids []string) (int, error) {
	return a.repo.RetryDocumentParsingJobs(ctx, ids)
}

// serviceTokenScopes mirrors the superadmin HTTP service-token mint.
var serviceTokenScopes = []string{"data:read", "schema:read", "agents:read", "projects:read"}
