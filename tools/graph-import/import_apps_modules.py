#!/usr/bin/env python3
"""
Import Apps, Modules, and shared Packages into the knowledge graph.
Wires: App -contains_module-> Module, Module -module_exposes_endpoint-> APIEndpoint
"""

import subprocess
import json
import sys
from pathlib import Path

MEMORY = str(Path.home() / ".memory/bin/memory")

def create_object(key: str, type_: str, props: dict) -> str | None:
    result = subprocess.run(
        [MEMORY, "graph", "objects", "create",
         "--key", key, "--type", type_,
         "--properties", json.dumps(props), "--json"],
        capture_output=True, text=True
    )
    if result.returncode != 0:
        if "already exists" in result.stderr or "already exists" in result.stdout:
            r2 = subprocess.run(
                [MEMORY, "graph", "objects", "get", key, "--json"],
                capture_output=True, text=True
            )
            if r2.returncode == 0:
                return json.loads(r2.stdout)["id"]
        print(f"  WARN create {key}: {result.stderr.strip()}", file=sys.stderr)
        return None
    return json.loads(result.stdout)["id"]

def create_rel(from_id: str, to_id: str, type_: str):
    r = subprocess.run(
        [MEMORY, "graph", "relationships", "create",
         "--from", from_id, "--to", to_id, "--type", type_, "--upsert"],
        capture_output=True, text=True
    )
    if r.returncode != 0:
        print(f"  WARN rel {type_}: {r.stderr.strip()}", file=sys.stderr)

def get_id(key: str) -> str | None:
    r = subprocess.run(
        [MEMORY, "graph", "objects", "get", key, "--json"],
        capture_output=True, text=True
    )
    if r.returncode == 0:
        return json.loads(r.stdout)["id"]
    return None

# ─── Apps ────────────────────────────────────────────────────────────────────

APPS = [
    {
        "key": "app-server",
        "name": "Memory API Server",
        "description": "Core backend HTTP API. Handles all knowledge graph operations, agent execution, auth, search, and integrations.",
        "app_type": "backend",
        "platform": ["linux"],
        "root_dir": "apps/server",
        "tech_stack": ["go", "echo", "bun-orm", "postgres", "fx"],
        "port": 3012,
    },
    {
        "key": "app-cli",
        "name": "Memory CLI",
        "description": "Developer CLI tool (`memory`) for managing projects, graph objects, agents, documents, schemas, and server operations.",
        "app_type": "cli",
        "platform": ["linux", "macos"],
        "root_dir": "tools/cli",
        "tech_stack": ["go", "cobra"],
    },
]

# ─── Server domain modules ────────────────────────────────────────────────────

DOMAIN_MODULES = [
    ("mod-agents",            "agents",           "AI agent execution, triggers, run management, webhooks, and tool orchestration",           "apps/server/domain/agents",           ["handler", "service", "repository", "executor", "toolpool", "module"]),
    ("mod-apitoken",          "apitoken",         "API key creation, validation, and revocation for project and account access",              "apps/server/domain/apitoken",         ["handler", "service", "repository", "routes", "entity", "module"]),
    ("mod-authinfo",          "authinfo",         "Auth session context — resolves current user identity from JWT/token",                     "apps/server/domain/authinfo",         ["handler", "routes", "module"]),
    ("mod-autoprovision",     "autoprovision",    "Automated provisioning of projects and resources on org creation",                         "apps/server/domain/autoprovision",    ["service", "module"]),
    ("mod-backups",           "backups",          "Full project data backup, export, and restore",                                            "apps/server/domain/backups",          ["handler", "service", "repository", "module"]),
    ("mod-branches",          "branches",         "Isolated graph branch management — create, merge, delete branches",                        "apps/server/domain/branches",         ["handler", "service", "store", "routes", "entity", "module"]),
    ("mod-chat",              "chat",             "Conversational chat sessions with message history and streaming responses",                 "apps/server/domain/chat",             ["handler", "service", "repository", "routes", "entity", "module"]),
    ("mod-chunking",          "chunking",         "Document chunking logic — splits documents into indexable text segments",                   "apps/server/domain/chunking",         ["handler", "service", "routes", "module"]),
    ("mod-chunks",            "chunks",           "Storage, retrieval, and deletion of text chunks with embedding vectors",                   "apps/server/domain/chunks",           ["handler", "service", "repository", "routes", "module"]),
    ("mod-datasource",        "datasource",       "External data source integrations (Slack, GitHub, etc.) with sync job management",         "apps/server/domain/datasource",       ["handler", "config", "provider", "worker", "repository", "module"]),
    ("mod-devtools",          "devtools",         "Internal developer utilities and debug endpoints",                                         "apps/server/domain/devtools",         ["handler", "module"]),
    ("mod-discoveryjobs",     "discoveryjobs",    "Background jobs for discovering and indexing new content from data sources",               "apps/server/domain/discoveryjobs",    ["handler", "service", "repository", "routes", "module"]),
    ("mod-docs",              "docs",             "Internal platform documentation management and serving",                                   "apps/server/domain/docs",             ["handler", "service", "routes", "entity", "module"]),
    ("mod-documents",         "documents",        "File upload, storage, metadata management, and content extraction for documents",          "apps/server/domain/documents",        ["handler", "service", "repository", "module"]),
    ("mod-email",             "email",            "Email delivery via Mailgun with templating and async job queue",                           "apps/server/domain/email",            ["worker", "template", "module"]),
    ("mod-embeddingpolicies", "embeddingpolicies","Rules controlling how graph objects and documents are vectorized for search",              "apps/server/domain/embeddingpolicies",["handler", "service", "store", "routes", "module"]),
    ("mod-events",            "events",           "Internal pub/sub event bus — broadcasts entity changes to SSE clients in real time",       "apps/server/domain/events",           ["handler", "service", "types", "module"]),
    ("mod-extraction",        "extraction",       "High-scale pipeline for parsing documents, generating embeddings, and extracting objects", "apps/server/domain/extraction",       ["worker", "jobs", "module"]),
    ("mod-githubapp",         "githubapp",        "GitHub App integration — OAuth, webhooks, repo access tokens",                            "apps/server/domain/githubapp",        ["handler", "service", "store", "module"]),
    ("mod-graph",             "graph",            "Core knowledge graph CRUD — objects, relationships, versioning, validation, migration",    "apps/server/domain/graph",            ["handler", "service", "repository", "module"]),
    ("mod-health",            "health",           "Health check endpoints and Prometheus metrics exposition",                                 "apps/server/domain/health",           ["handler", "service", "routes", "module"]),
    ("mod-integrations",      "integrations",     "Third-party service registry and integration lifecycle management",                        "apps/server/domain/integrations",     ["handler", "registry", "repository", "routes", "module"]),
    ("mod-invites",           "invites",          "Org and project invitation flows — create, accept, revoke invites",                        "apps/server/domain/invites",          ["handler", "service", "entity", "module"]),
    ("mod-journal",           "journal",          "Append-only audit log of all graph mutations with manual note support",                    "apps/server/domain/journal",          ["handler", "service", "store", "routes", "module"]),
    ("mod-mcp",               "mcp",              "Model Context Protocol server — exposes graph tools to MCP-compatible AI clients",         "apps/server/domain/mcp",              ["handler", "service", "sse", "module"]),
    ("mod-mcpregistry",       "mcpregistry",      "Registry of external MCP servers with tool discovery, sync, and proxy",                   "apps/server/domain/mcpregistry",      ["handler", "service", "repository", "proxy", "module"]),
    ("mod-monitoring",        "monitoring",       "System activity monitoring — extraction jobs, LLM call logs, process logs",               "apps/server/domain/monitoring",       ["handler", "repository", "routes", "module"]),
    ("mod-notifications",     "notifications",    "User-facing notification system — creation, listing, mark-as-read",                       "apps/server/domain/notifications",    ["handler", "service", "repository", "routes", "module"]),
    ("mod-orgs",              "orgs",             "Multi-tenant organization management — CRUD, memberships, tool settings",                  "apps/server/domain/orgs",             ["handler", "service", "repository", "module"]),
    ("mod-projects",          "projects",         "Project-level grouping of graph data — CRUD, memberships, settings",                      "apps/server/domain/projects",         ["handler", "service", "repository", "routes", "module"]),
    ("mod-provider",          "provider",         "LLM provider abstraction — OpenAI, Anthropic, Google; model catalog and pricing sync",    "apps/server/domain/provider",         ["handler", "service", "catalog", "module"]),
    ("mod-sandbox",           "sandbox",          "Secure code execution environments using gVisor and Firecracker microVMs",                "apps/server/domain/sandbox",          ["handler", "orchestrator", "module"]),
    ("mod-sandboximages",     "sandboximages",    "OCI container image management for agent sandbox environments",                           "apps/server/domain/sandboximages",    ["handler", "service", "store", "module"]),
    ("mod-schemaregistry",    "schemaregistry",   "Shared object type registry — resolves schema types across projects",                     "apps/server/domain/schemaregistry",   ["handler", "repository", "routes", "module"]),
    ("mod-schemas",           "schemas",          "Dynamic graph schema definitions, type migrations, and compiled type resolution",          "apps/server/domain/schemas",          ["handler", "service", "repository", "module"]),
    ("mod-search",            "search",           "Unified vector and hybrid search across graph objects, documents, and chunks",             "apps/server/domain/search",           ["handler", "service", "repository", "routes", "module"]),
    ("mod-skills",            "skills",           "Reusable agent skill definitions — install, list, and expose as agent tools",             "apps/server/domain/skills",           ["handler", "store", "routes", "module"]),
    ("mod-standalone",        "standalone",       "Single-tenant / local mode bootstrap — auto-creates org, project, and admin user",        "apps/server/domain/standalone",       ["bootstrap", "module"]),
    ("mod-superadmin",        "superadmin",       "Platform-wide administrative controls — user listing, impersonation, system ops",         "apps/server/domain/superadmin",       ["handler", "repository", "routes", "module"]),
    ("mod-tasks",             "tasks",            "Generic background task tracking — status, progress, cancellation",                       "apps/server/domain/tasks",            ["handler", "service", "repository", "routes", "module"]),
    ("mod-tracing",           "tracing",          "OpenTelemetry tracing integration — span export, trace query, config",                    "apps/server/domain/tracing",          ["handler", "config", "routes", "module"]),
    ("mod-useraccess",        "useraccess",       "RBAC and permission checks — org/project role enforcement",                               "apps/server/domain/useraccess",       ["handler", "service", "module"]),
    ("mod-useractivity",      "useractivity",     "Tracking recent user interactions for activity feeds and suggestions",                    "apps/server/domain/useractivity",     ["handler", "service", "repository", "module"]),
    ("mod-userprofile",       "userprofile",      "User settings, display name, avatar, and preferences",                                   "apps/server/domain/userprofile",      ["handler", "service", "repository", "module"]),
    ("mod-users",             "users",            "Core user identity management — lookup, creation, Zitadel sync",                         "apps/server/domain/users",            ["handler", "service", "repository", "module"]),
]

# ─── CLI sub-modules ──────────────────────────────────────────────────────────

CLI_MODULES = [
    ("mod-cli-cmd",         "cmd",          "Cobra command definitions for all CLI subcommands",                    "tools/cli/internal/cmd"),
    ("mod-cli-client",      "client",       "HTTP API client — wraps all server endpoints for CLI consumption",     "tools/cli/internal/client"),
    ("mod-cli-blueprints",  "blueprints",   "Declarative blueprint parsing and application logic",                  "tools/cli/internal/blueprints"),
    ("mod-cli-graphexplore","graphexplore", "Interactive terminal graph explorer (TUI)",                            "tools/cli/internal/graphexplore"),
    ("mod-cli-tui",         "tui",          "Terminal UI components shared across CLI commands",                    "tools/cli/internal/tui"),
    ("mod-cli-skillsfs",    "skillsfs",     "Filesystem abstraction for reading and installing agent skills",       "tools/cli/internal/skillsfs"),
]

# ─── Shared server packages ───────────────────────────────────────────────────

SERVER_PACKAGES = [
    ("mod-pkg-config",   "config",   "Configuration loading and validation from env vars and files",  "apps/server/internal/config"),
    ("mod-pkg-database", "database", "Bun/Postgres connection pool, transaction helpers, safe queries","apps/server/internal/database"),
    ("mod-pkg-jobs",     "jobs",     "Generic job queue and worker pool for background processing",    "apps/server/internal/jobs"),
    ("mod-pkg-migrate",  "migrate",  "In-app Goose DB migration runner",                              "apps/server/internal/migrate"),
    ("mod-pkg-server",   "server",   "Echo HTTP server setup, middleware, and routing bootstrap",      "apps/server/internal/server"),
    ("mod-pkg-storage",  "storage",  "S3/local file storage abstraction for document uploads",         "apps/server/internal/storage"),
]

# ─── Domain → endpoint key mapping (module_exposes_endpoint) ─────────────────

DOMAIN_ENDPOINTS = {
    "mod-agents":            [f"ep-agents-{h.lower()}" for h in [
        "ListAgents","GetAgent","GetAgentRuns","CreateAgent","UpdateAgent","DeleteAgent",
        "TriggerAgent","CancelRun","GetPendingEvents","BatchTrigger","CreateWebhookHook",
        "ListWebhookHooks","DeleteWebhookHook","ReceiveWebhook","ListDefinitions",
        "GetDefinition","CreateDefinition","UpdateDefinition","DeleteDefinition",
        "ListProjectRuns","GetProjectRun","GetRunMessages","GetRunToolCalls","GetRunSteps",
        "GetRunLogs","GetSession","GetSandboxConfig","UpdateSandboxConfig",
        "HandleRespondToQuestion","HandleListQuestionsByRun","HandleListQuestionsByProject",
        "GetADKSessions","GetADKSessionByID","ListAgentOverrides","GetAgentOverride",
        "SetAgentOverride","DeleteAgentOverride",
    ]],
    "mod-authinfo":          ["ep-authinfo-me"],
    "mod-branches":          ["ep-branches-list","ep-branches-getbyid","ep-branches-create","ep-branches-update","ep-branches-delete"],
    "mod-chat":              ["ep-chat-listconversations","ep-chat-getconversation","ep-chat-createconversation","ep-chat-updateconversation","ep-chat-deleteconversation","ep-chat-addmessage","ep-chat-streamchat","ep-chat-querystream","ep-chat-askstream"],
    "mod-chunking":          ["ep-chunking-recreatechunks"],
    "mod-chunks":            ["ep-chunks-list","ep-chunks-delete","ep-chunks-bulkdelete","ep-chunks-deletebydocument","ep-chunks-bulkdeletebydocuments"],
    "mod-datasource":        ["ep-datasource-listproviders","ep-datasource-getproviderschema","ep-datasource-testconfig","ep-datasource-list","ep-datasource-getsourcetypes","ep-datasource-get","ep-datasource-create","ep-datasource-update","ep-datasource-delete","ep-datasource-testconnection","ep-datasource-triggersync","ep-datasource-listsyncjobs","ep-datasource-getlatestsyncjob","ep-datasource-getsyncjob","ep-datasource-cancelsyncjob"],
    "mod-discoveryjobs":     ["ep-discoveryjobs-startdiscovery","ep-discoveryjobs-getjobstatus","ep-discoveryjobs-listjobs","ep-discoveryjobs-canceljob","ep-discoveryjobs-finalizediscovery"],
    "mod-docs":              ["ep-docs-listdocuments","ep-docs-getdocument"],
    "mod-documents":         ["ep-documents-list","ep-documents-getbyid","ep-documents-create","ep-documents-delete","ep-documents-bulkdelete","ep-documents-getsourcetypes","ep-documents-getcontent","ep-documents-download","ep-documents-getextractionsummary","ep-documents-upload"],
    "mod-embeddingpolicies": ["ep-embeddingpolicies-list","ep-embeddingpolicies-getbyid","ep-embeddingpolicies-create","ep-embeddingpolicies-update","ep-embeddingpolicies-delete"],
    "mod-events":            ["ep-events-handlestream"],
    "mod-githubapp":         ["ep-githubapp-getstatus","ep-githubapp-connect","ep-githubapp-disconnect"],
    "mod-journal":           ["ep-journal-listjournal","ep-journal-addnote"],
    "mod-mcpregistry":       ["ep-mcpregistry-listservers","ep-mcpregistry-getserver","ep-mcpregistry-createserver","ep-mcpregistry-updateserver","ep-mcpregistry-deleteserver","ep-mcpregistry-listservertools","ep-mcpregistry-toggletool","ep-mcpregistry-inspectserver","ep-mcpregistry-synctools"],
    "mod-monitoring":        ["ep-monitoring-listextractionjobs"],
    "mod-notifications":     ["ep-notifications-getstats","ep-notifications-list","ep-notifications-markread"],
    "mod-sandbox":           ["ep-sandbox-createworkspace","ep-sandbox-getworkspace","ep-sandbox-listworkspaces","ep-sandbox-deleteworkspace"],
    "mod-schemaregistry":    ["ep-schemaregistry-getprojecttypes"],
    "mod-schemas":           ["ep-schemas-getcompiledtypes","ep-schemas-migratetypes"],
    "mod-search":            ["ep-search-search"],
    "mod-skills":            ["ep-skills-listglobalskills"],
    "mod-superadmin":        ["ep-superadmin-listusers"],
    "mod-tasks":             ["ep-tasks-list"],
    "mod-useractivity":      ["ep-useractivity-record","ep-useractivity-getrecent"],
}

def main():
    ids: dict[str, str] = {}

    # 1. Create Apps
    print(f"\n{'='*60}\nCreating Apps...\n{'='*60}")
    for app in APPS:
        key = app.pop("key")
        obj_id = create_object(key, "App", app)
        if obj_id:
            ids[key] = obj_id
            print(f"  ✓ {key}  ({app['name']})")
        else:
            existing = get_id(key)
            if existing:
                ids[key] = existing
                print(f"  ~ {key} (existing)")

    # 2. Create server domain Modules + wire to app-server
    print(f"\n{'='*60}\nCreating {len(DOMAIN_MODULES)} server domain Modules...\n{'='*60}")
    server_id = ids.get("app-server")
    for key, domain, purpose, path, files in DOMAIN_MODULES:
        props = {
            "name": domain,
            "purpose": purpose,
            "path": path,
            "language": "go",
            "description": f"Server domain module: {domain}. Files: {', '.join(files)}.",
        }
        obj_id = create_object(key, "Module", props)
        if obj_id:
            ids[key] = obj_id
            print(f"  ✓ {key}")
        else:
            existing = get_id(key)
            if existing:
                ids[key] = existing
                print(f"  ~ {key} (existing)")
        # wire App → Module
        if server_id and ids.get(key):
            create_rel(server_id, ids[key], "contains_module")

    # 3. Create CLI sub-modules + wire to app-cli
    print(f"\n{'='*60}\nCreating {len(CLI_MODULES)} CLI Modules...\n{'='*60}")
    cli_id = ids.get("app-cli")
    for key, name, purpose, path in CLI_MODULES:
        props = {"name": name, "purpose": purpose, "path": path, "language": "go"}
        obj_id = create_object(key, "Module", props)
        if obj_id:
            ids[key] = obj_id
            print(f"  ✓ {key}")
        else:
            existing = get_id(key)
            if existing:
                ids[key] = existing
                print(f"  ~ {key} (existing)")
        if cli_id and ids.get(key):
            create_rel(cli_id, ids[key], "contains_module")

    # 4. Create shared server packages + wire to app-server
    print(f"\n{'='*60}\nCreating {len(SERVER_PACKAGES)} shared server packages...\n{'='*60}")
    for key, name, purpose, path in SERVER_PACKAGES:
        props = {"name": name, "purpose": purpose, "path": path, "language": "go"}
        obj_id = create_object(key, "Module", props)
        if obj_id:
            ids[key] = obj_id
            print(f"  ✓ {key}")
        else:
            existing = get_id(key)
            if existing:
                ids[key] = existing
                print(f"  ~ {key} (existing)")
        if server_id and ids.get(key):
            create_rel(server_id, ids[key], "contains_module")

    # 5. Wire domain Modules → APIEndpoints (module_exposes_endpoint)
    print(f"\n{'='*60}\nWiring Modules → APIEndpoints...\n{'='*60}")
    for mod_key, ep_keys in DOMAIN_ENDPOINTS.items():
        mod_id = ids.get(mod_key)
        if not mod_id:
            print(f"  SKIP {mod_key} — module not found")
            continue
        for ep_key in ep_keys:
            ep_id = get_id(ep_key)
            if not ep_id:
                print(f"  SKIP {ep_key} — endpoint not found")
                continue
            create_rel(mod_id, ep_id, "module_exposes_endpoint")
        print(f"  ✓ {mod_key} → {len(ep_keys)} endpoints")

    print(f"\n{'='*60}\nDone.\n{'='*60}")

if __name__ == "__main__":
    main()
