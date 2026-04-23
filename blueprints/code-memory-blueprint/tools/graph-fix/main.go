// Deprecated: use `codebase fix stale` instead. Run `codebase --help` for details.


package main

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/emergent-company/emergent.memory/apps/server/pkg/sdk"
	sdkgraph "github.com/emergent-company/emergent.memory/apps/server/pkg/sdk/graph"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
}

func run() error {
	client, err := sdk.New(sdk.Config{
		ServerURL: os.Getenv("MEMORY_SERVER_URL"),
		Auth:      sdk.AuthConfig{Mode: "apikey", APIKey: os.Getenv("MEMORY_API_KEY")},
		ProjectID: os.Getenv("MEMORY_PROJECT_ID"),
	})
	if err != nil {
		return err
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()

	// ── Fetch all APIEndpoints ────────────────────────────────────────────────
	fmt.Fprintln(os.Stderr, "→ Fetching all APIEndpoints...")
	eps, err := listAllObjects(ctx, client.Graph, "APIEndpoint")
	if err != nil {
		return err
	}
	fmt.Fprintf(os.Stderr, "  Found %d\n", len(eps))

	// Build indexes
	byKey := make(map[string]*sdkgraph.GraphObject)
	byMethodPath := make(map[string][]*sdkgraph.GraphObject)
	for _, ep := range eps {
		k := derefKey(ep.Key)
		if k != "" {
			byKey[k] = ep
		}
		method := strings.ToUpper(strProp(ep, "method"))
		path := strProp(ep, "path")
		if method != "" && path != "" {
			mk := method + " " + path
			byMethodPath[mk] = append(byMethodPath[mk], ep)
		}
	}

	// ── Fetch Services for orphan fix ─────────────────────────────────────────
	fmt.Fprintln(os.Stderr, "→ Fetching Services...")
	svcs, err := listAllObjects(ctx, client.Graph, "Service")
	if err != nil {
		return err
	}
	svcByDomain := make(map[string]*sdkgraph.GraphObject)
	for _, svc := range svcs {
		domain := strProp(svc, "domain")
		if domain != "" {
			svcByDomain[domain] = svc
		}
	}

	// ── 1. Delete stale no-key endpoints (no key, no domain, no handler) ──────
	fmt.Fprintln(os.Stderr, "→ Deleting stale no-key endpoints...")
	deleted := 0
	for _, ep := range eps {
		if derefKey(ep.Key) != "" {
			continue
		}
		domain := strProp(ep, "domain")
		handler := strProp(ep, "handler")
		if domain == "" && handler == "" {
			if err := client.Graph.DeleteObject(ctx, ep.EntityID); err != nil {
				fmt.Fprintf(os.Stderr, "  warn: delete %s: %v\n", ep.EntityID, err)
			} else {
				deleted++
			}
		}
	}
	fmt.Fprintf(os.Stderr, "  Deleted %d stale no-key endpoints\n", deleted)

	// ── 2. Delete duplicate weaker endpoints ──────────────────────────────────
	fmt.Fprintln(os.Stderr, "→ Deleting duplicate endpoints...")
	deleteKeys := []string{
		"ep-agent-runs-get",
		"ep-agent-runs-list",
		"ep-agent-definitions-list",
		"ep-agent-definitions-create",
		"ep-agents-trigger",  // wrong path /api/admin/agents/:id/trigger
		"ep-search-search",   // duplicate of ep-tracing-searchtraces
	}
	for _, k := range deleteKeys {
		ep, ok := byKey[k]
		if !ok {
			fmt.Fprintf(os.Stderr, "  skip (not found): %s\n", k)
			continue
		}
		if err := client.Graph.DeleteObject(ctx, ep.EntityID); err != nil {
			fmt.Fprintf(os.Stderr, "  warn: delete %s: %v\n", k, err)
		} else {
			fmt.Fprintf(os.Stderr, "  deleted: %s\n", k)
		}
	}

	// ── 3. Update file property on keyed stale endpoints ─────────────────────
	fmt.Fprintln(os.Stderr, "→ Updating file properties on keyed endpoints...")
	fileUpdates := map[string]string{
		"ep-tracing-listtraces":           "apps/server/domain/tracing/routes.go",
		"ep-tracing-searchtraces":         "apps/server/domain/tracing/routes.go",
		"ep-mcp-handleunifiedendpoint":    "apps/server/domain/mcp/routes.go",
		"ep-sandbox-executecommand":       "apps/server/domain/sandbox/routes.go",
		"ep-sandbox-bashcommand":          "apps/server/domain/sandbox/routes.go",
		"ep-sandbox-snapshotworkspace":    "apps/server/domain/sandbox/routes.go",
		"ep-agents-schedulecron":          "apps/server/domain/agents/routes.go",
		"ep-extraction-embeddingconfig":   "apps/server/domain/extraction/embedding_control_routes.go",
		"ep-extraction-embeddingresume":   "apps/server/domain/extraction/embedding_control_routes.go",
		"ep-extraction-embeddingpause":    "apps/server/domain/extraction/embedding_control_routes.go",
		"ep-extraction-embeddingprogress": "apps/server/domain/extraction/embedding_control_routes.go",
		"ep-extraction-embeddingstatus":   "apps/server/domain/extraction/embedding_control_routes.go",
		"ep-orgs-listtoolsettings":        "apps/server/domain/orgs/routes.go",
		"ep-orgs-upserttoolsetting":       "apps/server/domain/orgs/routes.go",
		"ep-orgs-deletetoolsetting":       "apps/server/domain/orgs/routes.go",
	}
	items := make([]sdkgraph.BulkUpdateObjectItem, 0, len(fileUpdates))
	for k, file := range fileUpdates {
		ep, ok := byKey[k]
		if !ok {
			fmt.Fprintf(os.Stderr, "  skip (not found): %s\n", k)
			continue
		}
		items = append(items, sdkgraph.BulkUpdateObjectItem{
			ID:         ep.EntityID,
			Properties: map[string]any{"file": file},
		})
	}
	if len(items) > 0 {
		resp, err := client.Graph.BulkUpdateObjects(ctx, &sdkgraph.BulkUpdateObjectsRequest{Items: items})
		if err != nil {
			fmt.Fprintf(os.Stderr, "  error bulk update: %v\n", err)
		} else {
			fmt.Fprintf(os.Stderr, "  Updated %d, failed %d\n", resp.Success, resp.Failed)
		}
	}

	// ── 4. Create new APIEndpoints for unmatched code routes ──────────────────
	fmt.Fprintln(os.Stderr, "→ Creating new APIEndpoints...")

	type NewEP struct {
		Key     string
		Domain  string
		Method  string
		Path    string
		Handler string
		File    string
	}

	newEPs := []NewEP{
		// ACP routes (agents)
		{"ep-agents-acp-ping",          "agents",      "GET",    "/acp/v1/ping",                                                    "Ping",                 "apps/server/domain/agents/acp_routes.go"},
		{"ep-agents-acp-getrun",        "agents",      "GET",    "/acp/v1/agents/:name/runs/:runId",                                "GetRun",               "apps/server/domain/agents/acp_routes.go"},
		{"ep-agents-acp-getrunevents",  "agents",      "GET",    "/acp/v1/agents/:name/runs/:runId/events",                        "GetRunEvents",         "apps/server/domain/agents/acp_routes.go"},
		{"ep-agents-acp-createrun",     "agents",      "POST",   "/acp/v1/agents/:name/runs",                                      "CreateRun",            "apps/server/domain/agents/acp_routes.go"},
		{"ep-agents-acp-resumerun",     "agents",      "POST",   "/acp/v1/agents/:name/runs/:runId/resume",                        "ResumeRun",            "apps/server/domain/agents/acp_routes.go"},
		{"ep-agents-acp-createsession", "agents",      "POST",   "/acp/v1/sessions",                                               "CreateSession",        "apps/server/domain/agents/acp_routes.go"},
		{"ep-agents-getrunbyid",        "agents",      "GET",    "/api/v1/runs/:runId",                                            "GetRunByID",           "apps/server/domain/agents/routes.go"},
		// authinfo
		{"ep-authinfo-issuer",          "authinfo",    "GET",    "/api/auth/issuer",                                               "Issuer",               "apps/server/domain/authinfo/routes.go"},
		// docs
		{"ep-docs-getcategories",       "docs",        "GET",    "/api/docs/categories",                                           "GetCategories",        "apps/server/domain/docs/routes.go"},
		// extraction embedding (already exist in graph, but skipped by sync — update via file_updates above)
		// githubapp
		{"ep-githubapp-callback",       "githubapp",   "GET",    "/api/v1/settings/github/callback",                               "Callback",             "apps/server/domain/githubapp/routes.go"},
		{"ep-githubapp-clisetup",       "githubapp",   "POST",   "/api/v1/settings/github/cli",                                    "CLISetup",             "apps/server/domain/githubapp/routes.go"},
		{"ep-githubapp-webhook",        "githubapp",   "POST",   "/api/v1/settings/github/webhook",                                "Webhook",              "apps/server/domain/githubapp/routes.go"},
		// health
		{"ep-health-healthz",           "health",      "GET",    "/healthz",                                                       "Healthz",              "apps/server/domain/health/routes.go"},
		// mcpregistry
		{"ep-mcpregistry-listbuiltintools",   "mcpregistry", "GET",   "/api/admin/builtin-tools",                                  "ListBuiltinTools",     "apps/server/domain/mcpregistry/routes.go"},
		{"ep-mcpregistry-updatebuiltintool",  "mcpregistry", "PATCH", "/api/admin/builtin-tools/:toolId",                          "UpdateBuiltinTool",    "apps/server/domain/mcpregistry/routes.go"},
		{"ep-mcpregistry-searchregistry",     "mcpregistry", "GET",   "/api/admin/mcp-registry/search",                            "SearchRegistry",       "apps/server/domain/mcpregistry/routes.go"},
		{"ep-mcpregistry-getregistryserver",  "mcpregistry", "GET",   "/api/admin/mcp-registry/servers/:name",                     "GetRegistryServer",    "apps/server/domain/mcpregistry/routes.go"},
		{"ep-mcpregistry-installfromregistry","mcpregistry", "POST",  "/api/admin/mcp-registry/install",                           "InstallFromRegistry",  "apps/server/domain/mcpregistry/routes.go"},
		// orgs tool-settings (already exist in graph, updated via file_updates above)
		// provider
		{"ep-provider-listallmodels",   "provider",    "GET",    "/api/v1/models",                                                 "ListAllModels",        "apps/server/domain/provider/routes.go"},
		// sandbox new routes
		{"ep-sandbox-attachsession",    "sandbox",     "POST",   "/api/v1/agent/sandboxes/:id/attach",                             "AttachSession",        "apps/server/domain/sandbox/routes.go"},
		{"ep-sandbox-createsnapshot",   "sandbox",     "POST",   "/api/v1/agent/sandboxes/:id/snapshot",                           "CreateSnapshot",       "apps/server/domain/sandbox/routes.go"},
		{"ep-sandbox-bashtool",         "sandbox",     "POST",   "/api/v1/agent/sandboxes/:id/bash",                               "BashTool",             "apps/server/domain/sandbox/routes.go"},
		{"ep-sandbox-readtool",         "sandbox",     "POST",   "/api/v1/agent/sandboxes/:id/read",                               "ReadTool",             "apps/server/domain/sandbox/routes.go"},
		{"ep-sandbox-writetool",        "sandbox",     "POST",   "/api/v1/agent/sandboxes/:id/write",                              "WriteTool",            "apps/server/domain/sandbox/routes.go"},
		{"ep-sandbox-edittool",         "sandbox",     "POST",   "/api/v1/agent/sandboxes/:id/edit",                               "EditTool",             "apps/server/domain/sandbox/routes.go"},
		{"ep-sandbox-globtool",         "sandbox",     "POST",   "/api/v1/agent/sandboxes/:id/glob",                               "GlobTool",             "apps/server/domain/sandbox/routes.go"},
		{"ep-sandbox-greptool",         "sandbox",     "POST",   "/api/v1/agent/sandboxes/:id/grep",                               "GrepTool",             "apps/server/domain/sandbox/routes.go"},
		{"ep-sandbox-gittool",          "sandbox",     "POST",   "/api/v1/agent/sandboxes/:id/git",                                "GitTool",              "apps/server/domain/sandbox/routes.go"},
		// schemas
		{"ep-schemas-createpack",       "schemas",     "POST",   "/api/schemas",                                                   "CreatePack",           "apps/server/domain/schemas/routes.go"},
		{"ep-schemas-getpack",          "schemas",     "GET",    "/api/schemas/:packId",                                           "GetPack",              "apps/server/domain/schemas/routes.go"},
		{"ep-schemas-updatepack",       "schemas",     "PUT",    "/api/schemas/:packId",                                           "UpdatePack",           "apps/server/domain/schemas/routes.go"},
		{"ep-schemas-deletepack",       "schemas",     "DELETE", "/api/schemas/:packId",                                           "DeletePack",           "apps/server/domain/schemas/routes.go"},
		{"ep-schemas-getavailablepacks","schemas",     "GET",    "/api/schemas/projects/:projectId/available",                     "GetAvailablePacks",    "apps/server/domain/schemas/routes.go"},
		{"ep-schemas-getinstalledpacks","schemas",     "GET",    "/api/schemas/projects/:projectId/installed",                     "GetInstalledPacks",    "apps/server/domain/schemas/routes.go"},
		{"ep-schemas-assignpack",       "schemas",     "POST",   "/api/schemas/projects/:projectId/assign",                        "AssignPack",           "apps/server/domain/schemas/routes.go"},
		{"ep-schemas-updateassignment", "schemas",     "PATCH",  "/api/schemas/projects/:projectId/assignments/:assignmentId",     "UpdateAssignment",     "apps/server/domain/schemas/routes.go"},
		{"ep-schemas-deleteassignment", "schemas",     "DELETE", "/api/schemas/projects/:projectId/assignments/:assignmentId",     "DeleteAssignment",     "apps/server/domain/schemas/routes.go"},
		{"ep-schemas-getschemahistory", "schemas",     "GET",    "/api/schemas/projects/:projectId/history",                       "GetSchemaHistory",     "apps/server/domain/schemas/routes.go"},
		{"ep-schemas-validateobjects",  "schemas",     "GET",    "/api/schemas/projects/:projectId/validate",                      "ValidateObjects",      "apps/server/domain/schemas/routes.go"},
		{"ep-schemas-previewmigration", "schemas",     "POST",   "/api/schemas/projects/:projectId/migrate/preview",               "PreviewMigration",     "apps/server/domain/schemas/routes.go"},
		{"ep-schemas-executemigration", "schemas",     "POST",   "/api/schemas/projects/:projectId/migrate/execute",               "ExecuteMigration",     "apps/server/domain/schemas/routes.go"},
		{"ep-schemas-rollbackmigration","schemas",     "POST",   "/api/schemas/projects/:projectId/migrate/rollback",              "RollbackMigration",    "apps/server/domain/schemas/routes.go"},
		{"ep-schemas-commitmigration",  "schemas",     "POST",   "/api/schemas/projects/:projectId/migrate/commit",                "CommitMigrationArchive","apps/server/domain/schemas/routes.go"},
		{"ep-schemas-getmigrationjob",  "schemas",     "GET",    "/api/schemas/projects/:projectId/migration-jobs/:jobId",         "GetMigrationJobStatus","apps/server/domain/schemas/routes.go"},
		// devtools
		{"ep-devtools-servedocsindex",  "devtools",    "GET",    "/docs",                                                          "ServeDocsIndex",       "apps/server/domain/devtools/module.go"},
		{"ep-devtools-servedocs",       "devtools",    "GET",    "/docs/*",                                                        "ServeDocs",            "apps/server/domain/devtools/module.go"},
		{"ep-devtools-serveopenapi",    "devtools",    "GET",    "/openapi.json",                                                  "ServeOpenAPISpec",     "apps/server/domain/devtools/module.go"},
		{"ep-devtools-servecoverage",   "devtools",    "GET",    "/coverage",                                                      "ServeCoverage",        "apps/server/domain/devtools/module.go"},
		{"ep-devtools-servecoveragefiles","devtools",  "GET",    "/coverage/*",                                                    "ServeCoverageFiles",   "apps/server/domain/devtools/module.go"},
	}

	// Filter out ones that already exist by key or by method+path
	createItems := make([]sdkgraph.CreateObjectRequest, 0)
	for _, ep := range newEPs {
		if _, exists := byKey[ep.Key]; exists {
			fmt.Fprintf(os.Stderr, "  skip (key exists): %s\n", ep.Key)
			continue
		}
		mk := ep.Method + " " + ep.Path
		if existing, exists := byMethodPath[mk]; exists {
			fmt.Fprintf(os.Stderr, "  skip (path exists, key=%s): %s\n", derefKey(existing[0].Key), ep.Key)
			continue
		}
		k := ep.Key
		createItems = append(createItems, sdkgraph.CreateObjectRequest{
			Type: "APIEndpoint",
			Key:  &k,
			Properties: map[string]any{
				"domain":  ep.Domain,
				"method":  ep.Method,
				"path":    ep.Path,
				"handler": ep.Handler,
				"file":    ep.File,
			},
		})
	}

	fmt.Fprintf(os.Stderr, "  Creating %d new APIEndpoints...\n", len(createItems))
	if len(createItems) > 0 {
		resp, err := client.Graph.BulkCreateObjects(ctx, &sdkgraph.BulkCreateObjectsRequest{Items: createItems})
		if err != nil {
			fmt.Fprintf(os.Stderr, "  error bulk create: %v\n", err)
		} else {
			fmt.Fprintf(os.Stderr, "  Created %d, failed %d\n", resp.Success, resp.Failed)
			// Store new entity IDs for belongs_to rels
			for _, r := range resp.Results {
				if r.Success && r.Object != nil {
					byKey[derefKey(r.Object.Key)] = r.Object
				}
			}
		}
	}

	// ── 5. Add belongs_to rels for new endpoints → their domain ───────────────
	fmt.Fprintln(os.Stderr, "→ Fetching Domain objects...")
	domains, err := listAllObjects(ctx, client.Graph, "Domain")
	if err != nil {
		return fmt.Errorf("listing domains: %w", err)
	}
	domainByName := make(map[string]*sdkgraph.GraphObject)
	for _, d := range domains {
		name := strProp(d, "name")
		domainByName[name] = d
		// Also index by key pattern: domain-<name>
		if derefKey(d.Key) != "" {
			parts := strings.SplitN(derefKey(d.Key), "-", 2)
			if len(parts) == 2 {
				domainByName[parts[1]] = d
			}
		}
	}

	// Add belongs_to rels for newly created endpoints
	var relItems []sdkgraph.CreateRelationshipRequest
	for _, ep := range newEPs {
		epObj, ok := byKey[ep.Key]
		if !ok {
			continue
		}
		domObj, ok := domainByName[ep.Domain]
		if !ok {
			fmt.Fprintf(os.Stderr, "  warn: domain not found for %s\n", ep.Domain)
			continue
		}
		relItems = append(relItems, sdkgraph.CreateRelationshipRequest{
			Type:  "belongs_to",
			SrcID: epObj.EntityID,
			DstID: domObj.EntityID,
		})
	}
	if len(relItems) > 0 {
		fmt.Fprintf(os.Stderr, "→ Creating %d belongs_to rels...\n", len(relItems))
		resp, err := client.Graph.BulkCreateRelationships(ctx, &sdkgraph.BulkCreateRelationshipsRequest{Items: relItems})
		if err != nil {
			fmt.Fprintf(os.Stderr, "  error: %v\n", err)
		} else {
			fmt.Fprintf(os.Stderr, "  Created %d, failed %d\n", resp.Success, resp.Failed)
		}
	}

	// ── 6. Fix orphans: add handles rels ──────────────────────────────────────
	fmt.Fprintln(os.Stderr, "→ Fixing orphan endpoints (adding handles rels)...")
	orphanKeys := []string{
		"ep-extraction-embeddingconfig",
		"ep-extraction-embeddingpause",
		"ep-extraction-embeddingprogress",
		"ep-extraction-embeddingresume",
		"ep-extraction-embeddingstatus",
		"ep-health-diagnose",
	}
	orphanDomains := map[string]string{
		"ep-extraction-embeddingconfig":   "extraction",
		"ep-extraction-embeddingpause":    "extraction",
		"ep-extraction-embeddingprogress": "extraction",
		"ep-extraction-embeddingresume":   "extraction",
		"ep-extraction-embeddingstatus":   "extraction",
		"ep-health-diagnose":              "health",
	}

	var orphanRels []sdkgraph.CreateRelationshipRequest
	for _, k := range orphanKeys {
		ep, ok := byKey[k]
		if !ok {
			fmt.Fprintf(os.Stderr, "  warn: orphan key not found: %s\n", k)
			continue
		}
		domain := orphanDomains[k]
		svc, ok := svcByDomain[domain]
		if !ok {
			fmt.Fprintf(os.Stderr, "  warn: service not found for domain: %s\n", domain)
			continue
		}
		orphanRels = append(orphanRels, sdkgraph.CreateRelationshipRequest{
			Type:  "handles",
			SrcID: svc.EntityID,
			DstID: ep.EntityID,
		})
	}
	if len(orphanRels) > 0 {
		resp, err := client.Graph.BulkCreateRelationships(ctx, &sdkgraph.BulkCreateRelationshipsRequest{Items: orphanRels})
		if err != nil {
			fmt.Fprintf(os.Stderr, "  error: %v\n", err)
		} else {
			fmt.Fprintf(os.Stderr, "  Created %d handles rels, failed %d\n", resp.Success, resp.Failed)
		}
	}

	fmt.Fprintln(os.Stderr, "✓ Done")
	return nil
}

func listAllObjects(ctx context.Context, g *sdkgraph.Client, objType string) ([]*sdkgraph.GraphObject, error) {
	const pageSize = 500
	var all []*sdkgraph.GraphObject
	var cursor string
	for {
		resp, err := g.ListObjects(ctx, &sdkgraph.ListObjectsOptions{Type: objType, Limit: pageSize, Cursor: cursor})
		if err != nil {
			return nil, fmt.Errorf("listing %s: %w", objType, err)
		}
		all = append(all, resp.Items...)
		if resp.NextCursor == nil || *resp.NextCursor == "" {
			break
		}
		cursor = *resp.NextCursor
	}
	return all, nil
}

func strProp(o *sdkgraph.GraphObject, key string) string {
	if v, ok := o.Properties[key]; ok {
		if s, ok := v.(string); ok {
			return s
		}
	}
	return ""
}

func derefKey(k *string) string {
	if k == nil {
		return ""
	}
	return *k
}
