#!/usr/bin/env python3
"""
Bulk import script for emergent.memory knowledge graph.
Creates APIEndpoint, DatabaseTable, CLICommand objects and wires them.
Uses the memory CLI for all operations.
"""

import subprocess
import json
import sys
import re
import time
from pathlib import Path

MEMORY = str(Path.home() / ".memory/bin/memory")
HANDLER_DIR = Path("/root/emergent.memory/apps/server/domain")
CLI_CMD_DIR = Path("/root/emergent.memory/tools/cli/internal/cmd")

# ─── helpers ────────────────────────────────────────────────────────────────

def run(args: list[str]) -> dict | None:
    result = subprocess.run([MEMORY] + args, capture_output=True, text=True)
    if result.returncode != 0:
        # skip "already exists" silently
        if "already exists" in result.stderr or "already exists" in result.stdout:
            return None
        print(f"  WARN: {result.stderr.strip() or result.stdout.strip()}", file=sys.stderr)
        return None
    try:
        return json.loads(result.stdout)
    except Exception:
        return None

def create_object(key: str, type_: str, props: dict) -> str | None:
    """Create object, return ID or None if already exists."""
    result = subprocess.run(
        [MEMORY, "graph", "objects", "create",
         "--key", key, "--type", type_,
         "--properties", json.dumps(props), "--json"],
        capture_output=True, text=True
    )
    if result.returncode != 0:
        if "already exists" in result.stderr or "already exists" in result.stdout:
            # fetch existing ID
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
    subprocess.run(
        [MEMORY, "graph", "relationships", "create",
         "--from", from_id, "--to", to_id, "--type", type_, "--upsert"],
        capture_output=True, text=True
    )

def get_id(key: str) -> str | None:
    r = subprocess.run(
        [MEMORY, "graph", "objects", "get", key, "--json"],
        capture_output=True, text=True
    )
    if r.returncode == 0:
        return json.loads(r.stdout)["id"]
    return None

# ─── 1. APIEndpoints ─────────────────────────────────────────────────────────

ENDPOINTS = [
    # agents
    ("agents", "ListAgents",                  "GET",    "/api/admin/agents"),
    ("agents", "GetAgent",                    "GET",    "/api/admin/agents/:id"),
    ("agents", "GetAgentRuns",                "GET",    "/api/admin/agents/:id/runs"),
    ("agents", "CreateAgent",                 "POST",   "/api/admin/agents"),
    ("agents", "UpdateAgent",                 "PATCH",  "/api/admin/agents/:id"),
    ("agents", "DeleteAgent",                 "DELETE", "/api/admin/agents/:id"),
    ("agents", "TriggerAgent",                "POST",   "/api/admin/agents/:id/trigger"),
    ("agents", "CancelRun",                   "POST",   "/api/admin/agents/:id/runs/:runId/cancel"),
    ("agents", "GetPendingEvents",            "GET",    "/api/admin/agents/:id/pending-events"),
    ("agents", "BatchTrigger",                "POST",   "/api/admin/agents/:id/batch-trigger"),
    ("agents", "CreateWebhookHook",           "POST",   "/api/admin/agents/:id/hooks"),
    ("agents", "ListWebhookHooks",            "GET",    "/api/admin/agents/:id/hooks"),
    ("agents", "DeleteWebhookHook",           "DELETE", "/api/admin/agents/:id/hooks/:hookId"),
    ("agents", "ReceiveWebhook",              "POST",   "/api/webhooks/agents/:hookId"),
    ("agents", "ListDefinitions",             "GET",    "/api/projects/:projectId/agent-definitions"),
    ("agents", "GetDefinition",               "GET",    "/api/projects/:projectId/agent-definitions/:id"),
    ("agents", "CreateDefinition",            "POST",   "/api/projects/:projectId/agent-definitions"),
    ("agents", "UpdateDefinition",            "PATCH",  "/api/projects/:projectId/agent-definitions/:id"),
    ("agents", "DeleteDefinition",            "DELETE", "/api/projects/:projectId/agent-definitions/:id"),
    ("agents", "ListProjectRuns",             "GET",    "/api/projects/:projectId/agent-runs"),
    ("agents", "GetProjectRun",               "GET",    "/api/projects/:projectId/agent-runs/:runId"),
    ("agents", "GetRunMessages",              "GET",    "/api/projects/:projectId/agent-runs/:runId/messages"),
    ("agents", "GetRunToolCalls",             "GET",    "/api/projects/:projectId/agent-runs/:runId/tool-calls"),
    ("agents", "GetRunSteps",                 "GET",    "/api/projects/:projectId/agent-runs/:runId/steps"),
    ("agents", "GetRunLogs",                  "GET",    "/api/v1/runs/:runId/logs"),
    ("agents", "GetSession",                  "GET",    "/api/v1/agent/sessions/:id"),
    ("agents", "GetSandboxConfig",            "GET",    "/api/admin/agent-definitions/:id/sandbox-config"),
    ("agents", "UpdateSandboxConfig",         "PUT",    "/api/admin/agent-definitions/:id/sandbox-config"),
    ("agents", "HandleRespondToQuestion",     "POST",   "/api/projects/:projectId/agent-questions/:questionId/respond"),
    ("agents", "HandleListQuestionsByRun",    "GET",    "/api/projects/:projectId/agent-runs/:runId/questions"),
    ("agents", "HandleListQuestionsByProject","GET",    "/api/projects/:projectId/agent-questions"),
    ("agents", "GetADKSessions",              "GET",    "/api/projects/:projectId/adk-sessions"),
    ("agents", "GetADKSessionByID",           "GET",    "/api/projects/:projectId/adk-sessions/:sessionId"),
    ("agents", "ListAgentOverrides",          "GET",    "/api/projects/:projectId/agent-definitions/overrides"),
    ("agents", "GetAgentOverride",            "GET",    "/api/projects/:projectId/agent-definitions/overrides/:agentName"),
    ("agents", "SetAgentOverride",            "PUT",    "/api/projects/:projectId/agent-definitions/overrides/:agentName"),
    ("agents", "DeleteAgentOverride",         "DELETE", "/api/projects/:projectId/agent-definitions/overrides/:agentName"),
    # authinfo
    ("authinfo", "Me",                        "GET",    "/api/auth/me"),
    # branches
    ("branches", "List",                      "GET",    "/api/graph/branches"),
    ("branches", "GetByID",                   "GET",    "/api/graph/branches/:id"),
    ("branches", "Create",                    "POST",   "/api/graph/branches"),
    ("branches", "Update",                    "PATCH",  "/api/graph/branches/:id"),
    ("branches", "Delete",                    "DELETE", "/api/graph/branches/:id"),
    # chat
    ("chat", "ListConversations",             "GET",    "/api/chat/conversations"),
    ("chat", "GetConversation",               "GET",    "/api/chat/:id"),
    ("chat", "CreateConversation",            "POST",   "/api/chat/conversations"),
    ("chat", "UpdateConversation",            "PATCH",  "/api/chat/:id"),
    ("chat", "DeleteConversation",            "DELETE", "/api/chat/:id"),
    ("chat", "AddMessage",                    "POST",   "/api/chat/:id/messages"),
    ("chat", "StreamChat",                    "POST",   "/api/chat/stream"),
    ("chat", "QueryStream",                   "POST",   "/api/projects/:projectId/query"),
    ("chat", "AskStream",                     "POST",   "/api/projects/:projectId/ask"),
    # chunking
    ("chunking", "RecreateChunks",            "POST",   "/api/documents/:id/recreate-chunks"),
    # chunks
    ("chunks", "List",                        "GET",    "/chunks"),
    ("chunks", "Delete",                      "DELETE", "/chunks/:id"),
    ("chunks", "BulkDelete",                  "DELETE", "/chunks"),
    ("chunks", "DeleteByDocument",            "DELETE", "/chunks/by-document/:documentId"),
    ("chunks", "BulkDeleteByDocuments",       "DELETE", "/chunks/by-documents"),
    # datasource
    ("datasource", "ListProviders",           "GET",    "/api/data-source-integrations/providers"),
    ("datasource", "GetProviderSchema",       "GET",    "/api/data-source-integrations/providers/:providerType/schema"),
    ("datasource", "TestConfig",              "POST",   "/api/data-source-integrations/test-config"),
    ("datasource", "List",                    "GET",    "/api/data-source-integrations"),
    ("datasource", "GetSourceTypes",          "GET",    "/api/data-source-integrations/source-types"),
    ("datasource", "Get",                     "GET",    "/api/data-source-integrations/:id"),
    ("datasource", "Create",                  "POST",   "/api/data-source-integrations"),
    ("datasource", "Update",                  "PATCH",  "/api/data-source-integrations/:id"),
    ("datasource", "Delete",                  "DELETE", "/api/data-source-integrations/:id"),
    ("datasource", "TestConnection",          "POST",   "/api/data-source-integrations/:id/test-connection"),
    ("datasource", "TriggerSync",             "POST",   "/api/data-source-integrations/:id/sync"),
    ("datasource", "ListSyncJobs",            "GET",    "/api/data-source-integrations/:id/sync-jobs"),
    ("datasource", "GetLatestSyncJob",        "GET",    "/api/data-source-integrations/:id/sync-jobs/latest"),
    ("datasource", "GetSyncJob",              "GET",    "/api/data-source-integrations/:id/sync-jobs/:jobId"),
    ("datasource", "CancelSyncJob",           "POST",   "/api/data-source-integrations/:id/sync-jobs/:jobId/cancel"),
    # discoveryjobs
    ("discoveryjobs", "StartDiscovery",       "POST",   "/discovery-jobs/projects/:projectId/start"),
    ("discoveryjobs", "GetJobStatus",         "GET",    "/discovery-jobs/:jobId"),
    ("discoveryjobs", "ListJobs",             "GET",    "/discovery-jobs/projects/:projectId"),
    ("discoveryjobs", "CancelJob",            "DELETE", "/discovery-jobs/:jobId"),
    ("discoveryjobs", "FinalizeDiscovery",    "POST",   "/discovery-jobs/:jobId/finalize"),
    # docs
    ("docs", "ListDocuments",                 "GET",    "/api/docs"),
    ("docs", "GetDocument",                   "GET",    "/api/docs/:slug"),
    # documents
    ("documents", "List",                     "GET",    "/api/documents"),
    ("documents", "GetByID",                  "GET",    "/api/documents/:id"),
    ("documents", "Create",                   "POST",   "/api/documents"),
    ("documents", "Delete",                   "DELETE", "/api/documents/:id"),
    ("documents", "BulkDelete",               "DELETE", "/api/documents"),
    ("documents", "GetSourceTypes",           "GET",    "/api/documents/source-types"),
    ("documents", "GetContent",               "GET",    "/api/documents/:id/content"),
    ("documents", "Download",                 "GET",    "/api/documents/:id/download"),
    ("documents", "GetExtractionSummary",     "GET",    "/api/documents/:id/extraction-summary"),
    ("documents", "Upload",                   "POST",   "/api/documents/upload"),
    # embeddingpolicies
    ("embeddingpolicies", "List",             "GET",    "/api/graph/embedding-policies"),
    ("embeddingpolicies", "GetByID",          "GET",    "/api/graph/embedding-policies/:id"),
    ("embeddingpolicies", "Create",           "POST",   "/api/graph/embedding-policies"),
    ("embeddingpolicies", "Update",           "PATCH",  "/api/graph/embedding-policies/:id"),
    ("embeddingpolicies", "Delete",           "DELETE", "/api/graph/embedding-policies/:id"),
    # events
    ("events", "HandleStream",                "GET",    "/api/events/stream"),
    # githubapp
    ("githubapp", "GetStatus",                "GET",    "/api/v1/settings/github"),
    ("githubapp", "Connect",                  "POST",   "/api/v1/settings/github/connect"),
    ("githubapp", "Disconnect",               "DELETE", "/api/v1/settings/github"),
    # journal
    ("journal", "ListJournal",                "GET",    "/api/graph/journal"),
    ("journal", "AddNote",                    "POST",   "/api/graph/journal/notes"),
    # mcpregistry
    ("mcpregistry", "ListServers",            "GET",    "/api/admin/mcp-servers"),
    ("mcpregistry", "GetServer",              "GET",    "/api/admin/mcp-servers/:id"),
    ("mcpregistry", "CreateServer",           "POST",   "/api/admin/mcp-servers"),
    ("mcpregistry", "UpdateServer",           "PATCH",  "/api/admin/mcp-servers/:id"),
    ("mcpregistry", "DeleteServer",           "DELETE", "/api/admin/mcp-servers/:id"),
    ("mcpregistry", "ListServerTools",        "GET",    "/api/admin/mcp-servers/:id/tools"),
    ("mcpregistry", "ToggleTool",             "PATCH",  "/api/admin/mcp-servers/:id/tools/:toolId"),
    ("mcpregistry", "InspectServer",          "POST",   "/api/admin/mcp-servers/:id/inspect"),
    ("mcpregistry", "SyncTools",              "POST",   "/api/admin/mcp-servers/:id/sync"),
    # monitoring
    ("monitoring", "ListExtractionJobs",      "GET",    "/api/monitoring/extraction-jobs"),
    # notifications
    ("notifications", "GetStats",             "GET",    "/api/notifications/stats"),
    ("notifications", "List",                 "GET",    "/api/notifications"),
    ("notifications", "MarkRead",             "PATCH",  "/api/notifications/:id/read"),
    # sandbox
    ("sandbox", "CreateWorkspace",            "POST",   "/api/v1/agent/sandboxes"),
    ("sandbox", "GetWorkspace",               "GET",    "/api/v1/agent/sandboxes/:id"),
    ("sandbox", "ListWorkspaces",             "GET",    "/api/v1/agent/sandboxes"),
    ("sandbox", "DeleteWorkspace",            "DELETE", "/api/v1/agent/sandboxes/:id"),
    # schemaregistry
    ("schemaregistry", "GetProjectTypes",     "GET",    "/api/schema-registry/projects/:projectId"),
    # schemas
    ("schemas", "GetCompiledTypes",           "GET",    "/api/schemas/projects/:projectId/compiled-types"),
    ("schemas", "MigrateTypes",               "POST",   "/api/schemas/projects/:projectId/migrate"),
    # search
    ("search", "Search",                      "POST",   "/search/unified"),
    # skills
    ("skills", "ListGlobalSkills",            "GET",    "/api/skills"),
    # superadmin
    ("superadmin", "ListUsers",               "GET",    "/api/superadmin/users"),
    # tasks
    ("tasks", "List",                         "GET",    "/api/tasks"),
    # useractivity
    ("useractivity", "Record",                "POST",   "/api/user-activity/record"),
    ("useractivity", "GetRecent",             "GET",    "/api/user-activity/recent"),
]

# ─── 2. DatabaseTables ───────────────────────────────────────────────────────

DB_TABLES = [
    ("agents",            "WebhookHook",                   "kb.agent_webhook_hooks"),
    ("agents",            "Agent",                         "kb.agents"),
    ("agents",            "AgentRun",                      "kb.agent_runs"),
    ("agents",            "AgentProcessingLog",            "kb.agent_processing_log"),
    ("agents",            "AgentDefinition",               "kb.agent_definitions"),
    ("agents",            "AgentRunMessage",               "kb.agent_run_messages"),
    ("agents",            "AgentRunToolCall",              "kb.agent_run_tool_calls"),
    ("agents",            "AgentQuestion",                 "kb.agent_questions"),
    ("agents",            "AgentRunJob",                   "kb.agent_run_jobs"),
    ("agents",            "ACPSession",                    "kb.acp_sessions"),
    ("apitoken",          "ApiToken",                      "core.api_tokens"),
    ("backups",           "Backup",                        "kb.backups"),
    ("branches",          "Branch",                        "kb.branches"),
    ("chat",              "Conversation",                  "kb.chat_conversations"),
    ("chat",              "Message",                       "kb.chat_messages"),
    ("chunks",            "Chunk",                         "kb.chunks"),
    ("datasource",        "DataSourceIntegration",         "kb.data_source_integrations"),
    ("datasource",        "DataSourceSyncJob",             "kb.data_source_sync_jobs"),
    ("discoveryjobs",     "DiscoveryJob",                  "kb.discovery_jobs"),
    ("documents",         "Document",                      "kb.documents"),
    ("email",             "EmailJob",                      "kb.email_jobs"),
    ("embeddingpolicies", "EmbeddingPolicy",               "kb.embedding_policies"),
    ("extraction",        "DocumentParsingJob",            "kb.document_parsing_jobs"),
    ("extraction",        "ChunkEmbeddingJob",             "kb.chunk_embedding_jobs"),
    ("extraction",        "GraphEmbeddingJob",             "kb.graph_embedding_jobs"),
    ("extraction",        "ObjectExtractionJob",           "kb.object_extraction_jobs"),
    ("githubapp",         "GitHubAppConfig",               "core.github_app_config"),
    ("graph",             "GraphObject",                   "kb.graph_objects"),
    ("graph",             "GraphRelationship",             "kb.graph_relationships"),
    ("integrations",      "Integration",                   "kb.integrations"),
    ("invites",           "Invite",                        "kb.invites"),
    ("journal",           "JournalEntry",                  "kb.project_journal"),
    ("journal",           "JournalNote",                   "kb.project_journal_notes"),
    ("mcpregistry",       "MCPServer",                     "kb.mcp_servers"),
    ("mcpregistry",       "MCPServerTool",                 "kb.mcp_server_tools"),
    ("monitoring",        "SystemProcessLog",              "kb.system_process_logs"),
    ("monitoring",        "LLMCallLog",                    "kb.llm_call_logs"),
    ("notifications",     "Notification",                  "kb.notifications"),
    ("orgs",              "Org",                           "kb.orgs"),
    ("orgs",              "OrganizationMembership",        "kb.organization_memberships"),
    ("projects",          "Project",                       "kb.projects"),
    ("projects",          "ProjectMembership",             "kb.project_memberships"),
    ("provider",          "OrgProviderConfig",             "kb.org_provider_configs"),
    ("provider",          "ProjectProviderConfig",         "kb.project_provider_configs"),
    ("provider",          "ProviderSupportedModel",        "kb.provider_supported_models"),
    ("sandbox",           "AgentSandbox",                  "kb.agent_sandboxes"),
    ("sandboximages",     "SandboxImage",                  "kb.sandbox_images"),
    ("schemaregistry",    "ProjectObjectSchemaRegistry",   "kb.project_object_schema_registry"),
    ("schemas",           "SchemaMigrationJob",            "kb.schema_migration_jobs"),
    ("skills",            "Skill",                         "kb.skills"),
    ("superadmin",        "Superadmin",                    "core.superadmins"),
    ("superadmin",        "UserProfile",                   "core.user_profiles"),
    ("tasks",             "Task",                          "kb.tasks"),
    ("useractivity",      "UserRecentItem",                "kb.user_recent_items"),
]

# ─── 3. CLICommands ──────────────────────────────────────────────────────────

CLI_COMMANDS = [
    ("memory ask",                      "ask.go",               "Ask a natural language question against the knowledge graph"),
    ("memory login",                    "auth.go",              "Authenticate with the Memory server"),
    ("memory logout",                   "auth.go",              "Log out and clear stored credentials"),
    ("memory agent-definitions list",   "agent_definitions.go", "List all agent definitions in the project"),
    ("memory agent-definitions create", "agent_definitions.go", "Create a new agent definition"),
    ("memory agents list",              "agents.go",            "List all agents in the project"),
    ("memory agents trigger",           "agents.go",            "Trigger an agent run"),
    ("memory blueprints install",       "blueprints.go",        "Install a blueprint from a source"),
    ("memory branches",                 "graph_branches.go",    "Manage graph branches"),
    ("memory browse",                   "browse.go",            "Browse the knowledge graph interactively"),
    ("memory config",                   "config.go",            "View or edit CLI configuration"),
    ("memory ctl start",                "ctl.go",               "Start the Memory server"),
    ("memory db",                       "db.go",                "Database management commands"),
    ("memory doctor",                   "doctor.go",            "Diagnose CLI and server health"),
    ("memory documents list",           "documents.go",         "List documents in the project"),
    ("memory documents upload",         "documents.go",         "Upload a document to the project"),
    ("memory embeddings",               "embeddings.go",        "Manage embedding jobs"),
    ("memory explore",                  "graph_explore.go",     "Explore the knowledge graph"),
    ("memory extraction",               "extraction.go",        "Manage document extraction jobs"),
    ("memory graph objects",            "graph.go",             "Manage graph objects (CRUD)"),
    ("memory graph relationships",      "graph.go",             "Manage graph relationships (CRUD)"),
    ("memory init",                     "init_project.go",      "Initialize a new Memory project"),
    ("memory install",                  "install.go",           "Install Memory server"),
    ("memory journal",                  "journal.go",           "View and annotate the project journal"),
    ("memory mcp-servers",              "mcp_servers.go",       "Manage MCP server registrations"),
    ("memory orgs",                     "orgs.go",              "Manage organizations"),
    ("memory projects",                 "projects.go",          "Manage projects"),
    ("memory provider",                 "provider.go",          "Configure LLM provider credentials"),
    ("memory query",                    "query.go",             "Run a semantic query against the knowledge graph"),
    ("memory schemas",                  "schemas.go",           "Manage graph schemas"),
    ("memory server",                   "server.go",            "Server management commands"),
    ("memory skills",                   "skills.go",            "Manage agent skills"),
    ("memory team",                     "team.go",              "Manage team members and invites"),
    ("memory tokens",                   "tokens.go",            "Manage API tokens"),
    ("memory traces",                   "traces.go",            "Query and inspect traces"),
    ("memory upgrade",                  "upgrade.go",           "Upgrade the Memory CLI or server"),
    ("memory version",                  "version.go",           "Print CLI version"),
    ("memory acp ping",                 "acp.go",               "Ping an ACP-compatible agent"),
    ("memory acp agents list",          "acp.go",               "List ACP agents"),
    ("memory acp runs",                 "acp.go",               "List ACP agent runs"),
    ("memory acp sessions",             "acp.go",               "List ACP sessions"),
    ("memory adk-sessions",             "adksessions.go",       "Manage ADK sessions"),
    ("memory builtin-tools",            "builtin_tools.go",     "List built-in agent tools"),
]

# ─── CLI → Endpoint wiring ───────────────────────────────────────────────────
# Maps CLI command key → list of endpoint keys it calls

CLI_TO_ENDPOINTS = {
    "cli-agent-definitions-list":   ["ep-agents-listdefinitions"],
    "cli-agent-definitions-create": ["ep-agents-createdefinition"],
    "cli-agents-list":              ["ep-agents-listagents"],
    "cli-agents-trigger":           ["ep-agents-triggeragent"],
    "cli-documents-list":           ["ep-documents-list"],
    "cli-documents-upload":         ["ep-documents-upload"],
    "cli-branches":                 ["ep-branches-list", "ep-branches-create", "ep-branches-getbyid", "ep-branches-update", "ep-branches-delete"],
    "cli-graph-objects":            ["ep-graph-objects-list"] if False else [],  # graph domain not in handler list
    "cli-journal":                  ["ep-journal-listjournal", "ep-journal-addnote"],
    "cli-mcp-servers":              ["ep-mcpregistry-listservers", "ep-mcpregistry-createserver"],
    "cli-query":                    ["ep-chat-querystream"],
    "cli-ask":                      ["ep-chat-askstream"],
    "cli-schemas":                  ["ep-schemas-getcompiledtypes", "ep-schemas-migratetypes"],
    "cli-tokens":                   ["ep-apitoken-me"] if False else [],
    "cli-traces":                   [],
    "cli-provider":                 [],
    "cli-skills":                   ["ep-skills-listglobalskills"],
}

# ─── key helpers ─────────────────────────────────────────────────────────────

def ep_key(domain: str, handler: str) -> str:
    return f"ep-{domain}-{handler.lower()}"

def table_key(domain: str, struct: str) -> str:
    slug = re.sub(r'(?<!^)(?=[A-Z])', '-', struct).lower()
    return f"table-{domain}-{slug}"

def cli_key(command: str) -> str:
    slug = command.replace("memory ", "").replace(" ", "-")
    return f"cli-{slug}"

# ─── main ────────────────────────────────────────────────────────────────────

def main():
    ep_ids: dict[str, str] = {}
    table_ids: dict[str, str] = {}
    cli_ids: dict[str, str] = {}

    # 1. Create APIEndpoints
    print(f"\n{'='*60}")
    print(f"Creating {len(ENDPOINTS)} APIEndpoints...")
    print('='*60)
    for domain, handler, method, path in ENDPOINTS:
        key = ep_key(domain, handler)
        file = f"apps/server/domain/{domain}/handler.go"
        props = {"method": method, "path": path, "handler": handler, "file": file, "domain": domain}
        obj_id = create_object(key, "APIEndpoint", props)
        if obj_id:
            ep_ids[key] = obj_id
            print(f"  ✓ {key}  {method} {path}")
        else:
            existing = get_id(key)
            if existing:
                ep_ids[key] = existing
                print(f"  ~ {key} (existing)")

    # 2. Create DataModels (Bun entities / DB tables) — schema type: DataModel
    print(f"\n{'='*60}")
    print(f"Creating {len(DB_TABLES)} DataModels (DB tables)...")
    print('='*60)
    for domain, struct, table in DB_TABLES:
        key = table_key(domain, struct)
        schema_name, tname = table.split(".", 1)
        props = {"name": struct, "table": table, "schema": schema_name, "domain": domain,
                 "file": f"apps/server/domain/{domain}/entity.go",
                 "description": f"Bun ORM model for {table}"}
        obj_id = create_object(key, "DataModel", props)
        if obj_id:
            table_ids[key] = obj_id
            print(f"  ✓ {key}  ({table})")
        else:
            existing = get_id(key)
            if existing:
                table_ids[key] = existing
                print(f"  ~ {key} (existing)")

    # 3. Create CLICommands — schema type: Context with context_type=cli
    print(f"\n{'='*60}")
    print(f"Creating {len(CLI_COMMANDS)} CLICommand contexts...")
    print('='*60)
    for command, file, description in CLI_COMMANDS:
        key = cli_key(command)
        props = {"name": command, "description": description,
                 "context_type": "cli",
                 "route": command,
                 "file": f"tools/cli/internal/cmd/{file}"}
        obj_id = create_object(key, "Context", props)
        if obj_id:
            cli_ids[key] = obj_id
            print(f"  ✓ {key}")
        else:
            existing = get_id(key)
            if existing:
                cli_ids[key] = existing
                print(f"  ~ {key} (existing)")

    # 4. Wire CLICommand → APIEndpoint
    print(f"\n{'='*60}")
    print("Wiring CLICommand → APIEndpoint (calls)...")
    print('='*60)
    for cli_k, ep_keys in CLI_TO_ENDPOINTS.items():
        cli_id = cli_ids.get(cli_k)
        if not cli_id:
            print(f"  SKIP {cli_k} — not found")
            continue
        for ep_k in ep_keys:
            ep_id = ep_ids.get(ep_k)
            if not ep_id:
                print(f"  SKIP wire {cli_k} → {ep_k} — endpoint not found")
                continue
            create_rel(cli_id, ep_id, "context_calls_endpoint")
            print(f"  ✓ {cli_k} → {ep_k}")

    print(f"\n{'='*60}")
    print("Done.")
    print(f"  APIEndpoints:   {len(ep_ids)}")
    print(f"  DatabaseTables: {len(table_ids)}")
    print(f"  CLICommands:    {len(cli_ids)}")
    print('='*60)

if __name__ == "__main__":
    main()
