package main

import (
	"context"
	"log"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/emergent-company/go-daisy/staticfs"
	"github.com/emergent-company/memory.web-ui/webui"
	"github.com/getsentry/sentry-go"
	"github.com/labstack/echo/v4"
	"github.com/labstack/echo/v4/middleware"
)

func main() {
	cfg := LoadConfig()
	if err := cfg.Validate(); err != nil {
		log.Fatalf("invalid configuration: %v", err)
	}
	if cfg.AuthMode != "session" {
		log.Printf("WARNING: AUTH_MODE=%s — authentication is relaxed; development only", cfg.AuthMode)
	}
	if err := initSentry(cfg.SentryDSN, cfg.SentryEnvironment, cfg.SentryTracesSampleRate); err != nil {
		log.Printf("sentry init: %v", err)
	}
	defer sentry.Flush(2 * time.Second)
	memory := NewMemoryClient(cfg.MemoryURL, cfg.MemoryToken, cfg.MemoryProjectID)
	sup := NewSupervisor(memory, cfg.BridgeBin, cfg.BridgeArgs, cfg.BridgeWorkdir, cfg.SupervisorInterval, cfg.WorkerInternalKey, "http://127.0.0.1:"+cfg.Port)
	s := &Server{cfg: cfg, memory: memory, supervisor: sup, bindings: newVoiceBindingStore(), shutdownCh: make(chan struct{})}
	s.hub = newConversationHub(s)
	s.registry = newAccountRegistry()

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	go sup.Run(ctx)

	e := echo.New()
	e.HideBanner = true
	e.Use(middleware.RequestLogger())
	// Compress text/HTML/CSS/JS/JSON responses. The 185KB app CSS and go-daisy's
	// 473KB CSS compress ~5-10x, and every HTMX partial swap shrinks too.
	// echo's Gzip compresses by length, not content-type, so it would also gzip
	// the live SSE streams — skip those to keep token/event delivery unbuffered.
	e.Use(middleware.GzipWithConfig(middleware.GzipConfig{
		Skipper: func(c echo.Context) bool {
			return strings.Contains(c.Request().Header.Get("Accept"), "text/event-stream")
		},
	}))
	// Bound request bodies to a sane size. Document and avatar uploads are
	// exempt: they legitimately carry large multipart payloads.
	e.Use(middleware.BodyLimitWithConfig(middleware.BodyLimitConfig{
		Limit:   "4M",
		Skipper: bodyLimitSkipper,
	}))
	e.Use(sentryRecoverMiddleware())
	e.Use(sentryTracingMiddleware())
	// Disable HTTP caching so deploys never serve stale pages/JS to browsers.
	e.Use(func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			c.Response().Header().Set("Cache-Control", "no-cache")
			return next(c)
		}
	})
	e.Use(sentryErrorMiddleware())
	// Normalize the request Host to PUBLIC_BASE_URL before any auth/cookie work,
	// so host-only cookies (oauth state + session) always match the pinned
	// redirect_uri (see auth.go canonicalHostRedirect).
	e.Use(s.canonicalHostRedirect)
	// Web-auth gate: dev mode keeps the current open UI + key-gated /api;
	// session mode (AUTH_MODE=session) requires a session for UI and
	// session-or-key for /api (see auth.go authDispatch).
	e.Use(s.authDispatch)

	// Internal worker endpoint — exempt from session auth (see publicAuthPath);
	// gated by the X-Worker-Key header instead.
	e.GET("/internal/voice-binding", s.voiceBindingHandler)

	// GitHub webhook ingress — HMAC-signature authenticated, NOT behind the
	// session/key gate (see publicAuthPath and webhook_github.go).
	e.POST("/webhooks/github", s.githubWebhook)

	api := e.Group("/api")
	api.GET("/health", s.health)
	api.GET("/workers", s.workers)
	api.GET("/agents", s.listAgents)
	api.POST("/agents", s.createAgent)
	api.GET("/agents/:id", s.getAgent)
	api.PUT("/agents/:id", s.updateAgent)
	api.DELETE("/agents/:id", s.deleteAgent)
	api.POST("/agents/:id/activate", s.activateAgent)
	api.POST("/agents/:id/deactivate", s.deactivateAgent)
	api.POST("/chat", s.chat)
	api.POST("/chat/questions/:questionId/respond", s.respondQuestion)
	api.POST("/chat/questions/:questionId/cancel", s.cancelQuestion)
	api.GET("/conversations", s.listConversations)
	api.GET("/conversations/:id", s.getConversation)
	api.GET("/conversations/:id/history", s.getConversationHistory)
	api.GET("/conversations/:id/dump", s.getConversationDump)
	api.GET("/conversations/:id/events", s.conversationEvents)
	api.GET("/runs/:runId/history", s.getRunHistory)
	api.GET("/mcp-servers", s.listMCPServers)
	api.POST("/mcp-servers", s.createMCPServer)
	api.GET("/mcp-servers/:id", s.getMCPServer)
	api.PATCH("/mcp-servers/:id", s.updateMCPServer)
	api.DELETE("/mcp-servers/:id", s.deleteMCPServer)
	// MCP server sync/inspect/tool JSON echo routes (page JS; see mcp_servers_handlers.go).
	api.POST("/mcp-servers/:id/sync", s.syncMCPServer)
	api.POST("/mcp-servers/:id/inspect", s.inspectMCPServer)
	api.GET("/mcp-servers/:id/tools", s.listMCPServerTools)
	api.PATCH("/mcp-servers/:id/tools/:toolId", s.setMCPServerToolEnabled)
	// MCP share instances (project-scoped MCP exposure; see mcp_shares_handlers.go).
	api.GET("/mcp-shares", s.listMCPShares)
	api.POST("/mcp-shares", s.createMCPShare)
	api.GET("/mcp-shares/tools", s.listMCPShareTools)
	api.GET("/mcp-shares/:id", s.getMCPShare)
	api.PATCH("/mcp-shares/:id", s.updateMCPShare)
	api.DELETE("/mcp-shares/:id", s.deleteMCPShare)
	api.POST("/mcp-shares/:id/rotate", s.rotateMCPShare)
	api.GET("/models", s.listModels)
	api.POST("/token", s.mintToken)
	api.GET("/sessions", s.listSessions)
	api.GET("/session", s.getSession)
	api.GET("/schedules", s.listScheduledAgents)
	api.POST("/schedules", s.createScheduledAgent)
	api.GET("/schedules/:id", s.getScheduledAgent)
	api.PATCH("/schedules/:id", s.updateScheduledAgent)
	api.POST("/schedules/:id/enable", s.enableScheduledAgent)
	api.DELETE("/schedules/:id", s.deleteScheduledAgent)
	api.POST("/schedules/:id/trigger", s.triggerScheduledAgent)
	api.GET("/schedules/:id/runs", s.listScheduledAgentRuns)
	api.GET("/memories/capability", s.memoryCapability)
	api.GET("/memories", s.listMemories)
	api.GET("/documents", s.listDocuments)
	api.POST("/documents", s.uploadDocument)
	api.GET("/documents/:id", s.getDocument)
	api.GET("/documents/:id/chunks", s.listDocumentChunks)
	api.POST("/documents/:id/extract", s.triggerExtraction)
	api.GET("/blueprints/installed", s.listInstalledBlueprints)
	api.GET("/blueprints/available", s.listAvailableBlueprints)
	api.GET("/blueprints/compiled-types", s.getCompiledTypes)
	api.POST("/blueprints/install", s.installBlueprint)
	api.POST("/blueprints/enable", s.enableBlueprint)
	api.POST("/blueprints/:id/install", s.installBlueprintById)
	api.POST("/blueprints/:id/unapply", s.unapplyBlueprint)
	// Derive a draft blueprint (or a new version) from the project's current
	// compiled types (spec blueprint-api; task 2.2).
	api.POST("/blueprints/derive", s.deriveSchemaBlueprint, s.requireSchemaWrite)
	api.POST("/blueprints/:id/versions/derive", s.deriveSchemaBlueprintVersion, s.requireSchemaWrite)

	// Org / project tenancy (session-scoped; see project_handlers.go).
	api.GET("/orgs", s.listOrgs)
	api.POST("/orgs", s.createOrg)
	api.GET("/projects", s.listProjects)
	api.POST("/projects", s.createProject)
	api.POST("/projects/:id/activate", s.activateProject)

	// Org members / invites / profile (see org_members_handlers.go).
	api.GET("/members", s.listMembers)
	api.DELETE("/members/:userId", s.removeMember)
	api.GET("/invites", s.listInvites)
	api.POST("/invites", s.createInvite)
	api.GET("/invites/pending", s.listPendingInvites)
	api.POST("/invites/accept", s.acceptInvite)
	api.POST("/invites/:id/decline", s.declineInvite)
	api.DELETE("/invites/:id", s.cancelInvite)
	api.GET("/users/search", s.searchUsers)
	api.GET("/user/profile", s.getProfile)
	api.PUT("/user/profile", s.updateProfile)

	// Web UI — same binary, same echo server. go-daisy assets under /static/*,
	// app-specific assets under /assets/*.
	e.GET("/static/*", echo.WrapHandler(cacheStatic(staticfs.Handler("/static/"))))
	e.GET("/assets/*", echo.WrapHandler(webui.Handler("/assets/")))
	// Device setup: NO auth — exchanges a one-time setup token (from the QR on
	// the settings page) for a per-device API key.
	e.POST("/api/setup", s.setupClient)
	// OIDC sign-in (auth.go/oidc.go): /auth/login renders the sign-in page,
	// /auth/start kicks off the OIDC redirect; /auth/callback and /auth/logout
	// are public; both auth routes no-op to "/" in dev mode.
	e.GET("/auth/login", s.authLogin)
	e.GET("/auth/start", s.authStart)
	e.GET("/auth/add", s.authAdd)
	e.GET("/auth/callback", s.authCallback)
	e.POST("/auth/logout", s.authLogout)
	e.POST("/auth/switch", s.authSwitch)
	// Project switcher + create (PRG form posts; see project_ui.go).
	e.POST("/projects", s.uiCreateProject)
	e.POST("/projects/activate", s.uiActivateProject)
	e.POST("/projects/delete", s.uiDeleteProjects)
	e.POST("/projects/restore", s.uiRestoreProject)
	e.POST("/projects/transfer", s.uiTransferProject)
	e.GET("/", s.uiRoot)
	e.GET("/agents", s.uiAgents)
	e.GET("/agents/:id", s.uiAgent)
	e.GET("/agents/:id/settings", s.uiAgentSettings)
	e.POST("/agents/:id/update", s.uiAgentUpdate)
	e.GET("/agents/:id/sandbox", s.uiAgentSandbox)
	e.POST("/agents/:id/sandbox/update", s.uiAgentSandboxUpdate)
	e.GET("/agents/:id/sessions", s.uiAgentSessions)
	e.GET("/agents/:id/memories", s.uiAgentMemories)
	e.GET("/chat", s.uiChat)
	e.GET("/partial/chat-rail", s.uiChatRail)
	e.GET("/documents", s.uiDocuments)
	e.GET("/documents/:id", s.uiDocument)
	e.POST("/documents", s.uiUploadDocument)
	e.POST("/documents/:id/extract", s.uiTriggerExtraction)
	e.POST("/documents/:id/delete", s.uiDeleteDocument)
	e.GET("/objects", s.uiObjects)
	e.GET("/objects/search", s.uiObjectSearch)
	e.GET("/objects/new", s.uiObjectNew)
	e.GET("/objects/:id", s.uiObject)
	e.GET("/objects/:id/merge", s.uiObjectMerge)
	e.GET("/objects/:id/chat", s.uiObjectChat)
	e.POST("/objects", s.uiObjectCreate)
	e.POST("/objects/:id", s.uiObjectUpdate)
	e.POST("/objects/:id/relationships", s.uiObjectRelationshipCreate)
	e.GET("/schema", s.uiSchema)
	e.GET("/schema/add", s.uiSchemaAdd)
	e.POST("/schema/add", s.uiSchemaInstall, s.requireSchemaWrite)
	e.GET("/schema/packs", s.uiSchemaPacks)
	e.GET("/schema/packs/:id", s.uiSchemaPack)
	e.GET("/schema/object-types/:name", s.uiSchemaObjectType)
	e.GET("/schema/object-types/:name/edit", s.uiSchemaObjectTypeEdit)
	e.POST("/schema/object-types/:name", s.uiSchemaObjectTypeUpdate, s.requireSchemaWrite)
	// Blueprint derivation from the project's effective types (design D5).
	e.POST("/schema/blueprints/derive", s.deriveSchemaBlueprint, s.requireSchemaWrite)
	e.POST("/schema/blueprints/:id/versions/derive", s.deriveSchemaBlueprintVersion, s.requireSchemaWrite)
	e.GET("/blueprints", s.uiBlueprints)
	e.GET("/blueprints/migrations", s.uiMigrations)
	e.GET("/blueprints/:id", s.uiBlueprint)
	e.POST("/blueprints/install", s.uiInstallBlueprint)
	e.POST("/blueprints/enable", s.uiEnableBlueprint)
	e.POST("/blueprints/:id/install", s.uiInstallBlueprintById)
	e.POST("/blueprints/:id/unapply", s.uiUnapplyBlueprint)
	e.POST("/blueprints/migrate", s.uiMigrate)
	e.POST("/blueprints/migrate/rollback", s.uiRollbackMigration)
	e.GET("/skills", s.uiSkills)
	e.GET("/skills/new", s.uiNewSkill)
	e.GET("/skills/:id", s.uiSkill)
	e.POST("/skills", s.uiCreateSkill)
	e.POST("/skills/:id/update", s.uiUpdateSkill)
	e.POST("/skills/:id/delete", s.uiDeleteSkill)
	e.GET("/backups", s.uiBackups)
	e.POST("/backups", s.uiBackupCreate)
	e.GET("/backups/:id", s.uiBackupDetail)
	e.GET("/backups/:id/download", s.uiBackupDownload)
	e.POST("/backups/:id/delete", s.uiBackupDelete)
	e.GET("/schedules", s.uiSchedules)
	e.GET("/schedules/new", s.uiSchedulesNew)
	e.GET("/schedules/:id", s.uiSchedule)
	e.POST("/schedules", s.uiScheduleCreate)
	e.POST("/schedules/:id/update", s.uiScheduleUpdate)
	e.POST("/schedules/:id/delete", s.uiScheduleDelete)
	e.POST("/schedules/:id/trigger", s.uiScheduleTrigger)
	e.POST("/schedules/:id/toggle", s.uiScheduleToggle)
	e.GET("/runs/:runId", s.uiRun)
	e.GET("/settings", s.uiProjectSettings)
	e.GET("/settings/assistant", s.uiProjectAssistantSettings)
	e.GET("/settings/overrides", s.uiProjectOverridesSettings)
	e.GET("/settings/providers", s.uiProjectProvidersSettings)
	e.GET("/settings/providers/new", s.uiProjectProviderNew)
	e.GET("/settings/providers/:provider/edit", s.uiProjectProviderEdit)
	e.GET("/settings/voice", s.uiProjectVoiceSettings)
	e.GET("/settings/devices", s.uiProjectDeviceSettings)
	e.GET("/settings/approvals", s.uiApprovals)
	e.GET("/settings/mcp-nodes", s.uiMCPNodes)
	e.POST("/settings/mcp-nodes/remove", s.uiMCPNodesRemove)
	e.POST("/settings/approvals/:questionId/respond", s.uiApproveReject)
	e.POST("/settings/approvals/:questionId/cancel", s.uiCancelApproval)
	e.POST("/settings/project/:field", s.uiProjectSettingsProjectField)
	e.POST("/settings/overrides", s.uiProjectSettingsOverride)
	e.POST("/settings/overrides/:agentName/delete", s.uiProjectSettingsOverrideDelete)
	e.POST("/settings/remember/:field", s.uiProjectSettingsRememberField)
	e.POST("/settings/assistant", s.uiProjectSettingsAssistant)
	e.POST("/settings/editor", s.uiProjectSettingsEditor)
	e.POST("/settings/voice", s.uiProjectSettingsVoice)
	e.POST("/settings/voice/:key", s.uiProjectVoiceField)
	e.POST("/settings/voice/group/:group", s.uiProjectVoiceGroup)
	e.POST("/settings/providers/:provider/:model", s.uiProjectSettingsProviderOverride)
	e.POST("/settings/providers/:provider/:model/delete", s.uiProjectSettingsProviderOverrideDelete)
	e.POST("/settings/providers/config", s.uiProjectProviderConfig)
	e.POST("/settings/providers/test", s.uiProjectProviderTestConnection)
	e.POST("/settings/providers/check-url", s.uiProjectProviderCheckBaseURL)
	e.POST("/settings/providers/model-config", s.uiProjectModelConfig)
	e.POST("/settings/providers/:provider/test", s.uiProjectProviderTest)
	e.POST("/settings/providers/:provider/remove", s.uiProjectProviderRemove)
	e.POST("/settings/devices/:key/revoke", s.uiRevokeDevice)
	// Project API tokens (table list + standalone create/edit pages; see api_tokens_handlers.go).
	e.GET("/settings/tokens", s.uiAPITokens)
	e.GET("/settings/tokens/new", s.uiAPITokensNewPage)
	e.POST("/settings/tokens/new", s.uiAPITokensCreate)
	e.GET("/settings/tokens/:tokenId/edit", s.uiAPITokensEditPage)
	e.POST("/settings/tokens/:tokenId/revoke", s.uiAPITokensRevoke)
	e.POST("/settings/tokens/:tokenId/scopes", s.uiAPITokensScopes)
	e.POST("/settings/tokens/:tokenId/regenerate", s.uiAPITokensRegenerate)
	// MCP server registry management (crumb-framed list + standalone
	// create/edit pages under /settings/mcp-servers; see mcp_servers_handlers.go).
	e.GET("/settings/mcp-servers", s.uiMCPServers)
	e.GET("/settings/mcp-servers/new", s.uiMCPNewPage)
	e.POST("/settings/mcp-servers/new", s.uiMCPCreate)
	// MCP Sharing: nested under the MCP Servers area (see mcp_shares_handlers.go).
	e.GET("/settings/mcp-servers/shares", s.uiMCPShares)
	e.GET("/settings/mcp-servers/shares/new", s.uiMCPShareNewPage)
	e.POST("/settings/mcp-servers/shares/new", s.uiMCPShareCreate)
	e.GET("/settings/mcp-servers/shares/:id/edit", s.uiMCPShareEditPage)
	e.POST("/settings/mcp-servers/shares/:id/update", s.uiMCPShareUpdate)
	e.POST("/settings/mcp-servers/shares/:id/revoke", s.uiMCPShareRevoke)
	e.POST("/settings/mcp-servers/shares/:id/rotate", s.uiMCPShareRotate)
	e.GET("/settings/mcp-servers/:id/edit", s.uiMCPEditPage)
	e.POST("/settings/mcp-servers/:id/update", s.uiMCPUpdate)
	e.POST("/settings/mcp-servers/:id/delete", s.uiMCPDelete)
	e.POST("/settings/mcp-servers/:id/toggle", s.uiMCPToggle)
	// Org/member/invite/profile management (PRG form handlers; see org_members_ui.go).
	e.GET("/orgs", s.uiOrgs)
	e.GET("/orgs/new", s.uiOrgCreatePage)
	e.POST("/orgs", s.uiCreateOrg)
	e.GET("/orgs/:id", s.uiOrg)
	e.GET("/orgs/:id/members", s.uiOrgMembers)
	// Org-level invitation: carries the org id + org_admin role and no
	// projectId, unlike the project-scoped /members/new flow.
	e.GET("/orgs/:id/invite", s.uiOrgInvitePage)
	e.POST("/orgs/:id/invite", s.uiOrgInviteCreate)
	// Org Settings hub: Tools section at /settings, Danger zone at
	// /settings/danger-zone. The tool-setting toggle/delete forms and the
	// delete-org form still POST below; only their page URLs moved.
	e.GET("/orgs/:id/settings", s.uiOrgSettings)
	e.GET("/orgs/:id/settings/general", s.uiOrgSettingsGeneral)
	e.GET("/orgs/:id/settings/danger-zone", s.uiOrgSettingsDangerZone)
	// Rename is a native PRG form (hx-boost="false"): POST then redirect to the
	// General section with a flash.
	e.POST("/orgs/:id/rename", s.uiOrgRename)
	// Backward-compat: the pre-hub Tool settings page URL moved into the
	// Settings hub; the old path now redirects permanently.
	e.GET("/orgs/:id/tool-settings", func(c echo.Context) error {
		return c.Redirect(http.StatusMovedPermanently, "/orgs/"+url.PathEscape(c.Param("id"))+"/settings")
	})
	e.POST("/orgs/:id/tool-settings/:toolName", s.uiOrgToolSettingUpdate)
	e.POST("/orgs/:id/tool-settings/:toolName/delete", s.uiOrgToolSettingDelete)
	e.POST("/orgs/:id/delete", s.uiDeleteOrg)
	e.GET("/members", s.uiMembers)
	e.GET("/members/new", s.uiMemberInvitePage)
	e.POST("/members", s.uiMemberInviteCreate)
	e.GET("/members/:userId", s.uiMemberDetails)
	e.POST("/members/:userId/remove", s.uiRemoveMember)
	e.POST("/members/:userId/role", s.uiChangeMemberRole)
	e.POST("/invites/:id/revoke", s.uiRevokeInvite)
	e.POST("/invites/:id/accept", s.uiAcceptInvite)
	e.POST("/invites/:id/decline", s.uiDeclineInvite)
	e.GET("/profile", s.uiProfile)
	e.GET("/profile/invitations", s.uiProfileInvitations)
	e.POST("/profile", s.uiUpdateProfile)
	// Profile photo (upload/remove PRG forms; the avatar image itself streams
	// through /api/user/avatar via avatarProxy — the URL AvatarUrl points at).
	e.POST("/profile/avatar", s.uiUploadAvatar)
	e.POST("/profile/avatar/remove", s.uiRemoveAvatar)
	e.GET("/api/user/avatar", s.avatarProxy)
	// Account API tokens (table list + standalone create/edit pages under the
	// profile area; see api_tokens_handlers.go).
	e.GET("/profile/tokens", s.uiProfileTokens)
	e.GET("/profile/tokens/new", s.uiProfileTokensNewPage)
	e.POST("/profile/tokens/new", s.uiProfileAPITokensCreate)
	e.GET("/profile/tokens/:tokenId/edit", s.uiProfileAPITokensEditPage)
	e.POST("/profile/tokens/:tokenId/revoke", s.uiProfileAPITokensRevoke)
	e.POST("/profile/tokens/:tokenId/scopes", s.uiProfileAPITokensScopes)
	e.POST("/profile/tokens/:tokenId/regenerate", s.uiProfileAPITokensRegenerate)
	e.GET("/partial/invite-user-search", s.uiInviteUserSearch)
	// Session trace/log viewer and usage dashboard.
	e.GET("/sessions", s.uiSessions)
	e.GET("/sessions/:id", s.uiSession)
	e.GET("/usage", s.uiUsage)

	srv := &http.Server{
		Addr:              "0.0.0.0:" + cfg.Port,
		Handler:           e,
		ReadHeaderTimeout: 15 * time.Second,
		ReadTimeout:       5 * time.Minute,
		IdleTimeout:       120 * time.Second,
		// WriteTimeout is intentionally unset: SSE streams (chat, conversation
		// events) must stay open indefinitely.
	}

	go func() {
		log.Printf("memory gateway listening on :%s", cfg.Port)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			captureError(err)
			log.Printf("gateway listen error: %v", err)
			sentry.Flush(2 * time.Second)
			os.Exit(1)
		}
	}()

	<-ctx.Done()
	log.Printf("shutting down…")
	s.beginShutdown()
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	_ = srv.Shutdown(shutdownCtx)
	select {
	case <-sup.Done():
	case <-time.After(5 * time.Second):
		log.Printf("supervisor did not stop within 5s")
	}
}

// bodyLimitSkipper exempts the large-upload routes from the global body limit.
func bodyLimitSkipper(c echo.Context) bool {
	p := c.Request().URL.Path
	return strings.HasPrefix(p, "/documents") ||
		strings.HasPrefix(p, "/api/documents") ||
		p == "/profile/avatar"
}

// cacheStatic serves go-daisy's embedded assets with immutable caching,
// mirroring webui.Handler's /assets/* behavior. Their URLs are content-hashed
// (?v=<hash> via staticfs.Hash()), so a rebuild with changed assets yields new
// URLs and busts the cache — but a rebuild that doesn't touch go-daisy's static
// files keeps serving the cached copy instead of re-downloading 473KB of CSS on
// every full-page navigation. Overrides the global no-cache middleware.
func cacheStatic(h http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
		h.ServeHTTP(w, r)
	})
}
