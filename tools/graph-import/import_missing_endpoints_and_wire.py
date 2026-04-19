#!/usr/bin/env python3
"""
Phase 2 enrichment:
1. Create missing APIEndpoints for graph, extraction, provider, integrations,
   orgs, projects, health, superadmin, schemaregistry, sandboximages,
   useractivity, tracing, backups, invites, userprofile domains.
2. Wire ALL remaining scenarios → APIEndpoints via 'uses' relationship.
"""

import subprocess
import json
import sys
from pathlib import Path

MEMORY = str(Path.home() / ".memory/bin/memory")


# ─── helpers ────────────────────────────────────────────────────────────────

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


def create_rel(from_id: str, to_id: str, rel_type: str):
    r = subprocess.run(
        [MEMORY, "graph", "relationships", "create",
         "--from", from_id, "--to", to_id, "--type", rel_type, "--upsert"],
        capture_output=True, text=True
    )
    if r.returncode != 0:
        print(f"  WARN rel {rel_type}: {r.stderr.strip()}", file=sys.stderr)


def list_objects(type_: str) -> list[dict]:
    r = subprocess.run(
        [MEMORY, "graph", "objects", "list", "--type", type_, "--json", "--limit", "1000"],
        capture_output=True, text=True
    )
    if r.returncode != 0:
        return []
    return json.loads(r.stdout).get("items", [])


def get_id(key: str) -> str | None:
    r = subprocess.run(
        [MEMORY, "graph", "objects", "get", key, "--json"],
        capture_output=True, text=True
    )
    if r.returncode == 0:
        return json.loads(r.stdout)["id"]
    return None


# ─── 1. Missing APIEndpoints ─────────────────────────────────────────────────

NEW_ENDPOINTS = [
    # graph - objects
    ("graph", "ListObjects",            "GET",    "/api/graph/objects/search"),
    ("graph", "CountObjects",           "GET",    "/api/graph/objects/count"),
    ("graph", "FTSSearch",              "GET",    "/api/graph/objects/fts"),
    ("graph", "VectorSearch",           "POST",   "/api/graph/objects/vector-search"),
    ("graph", "GetTags",                "GET",    "/api/graph/objects/tags"),
    ("graph", "BulkUpdateStatus",       "POST",   "/api/graph/objects/bulk-update-status"),
    ("graph", "BulkCreateObjects",      "POST",   "/api/graph/objects/bulk"),
    ("graph", "BulkUpdateObjects",      "POST",   "/api/graph/objects/bulk-update"),
    ("graph", "ValidateObject",         "POST",   "/api/graph/objects/validate"),
    ("graph", "UpsertObject",           "PUT",    "/api/graph/objects/upsert"),
    ("graph", "GetObject",              "GET",    "/api/graph/objects/:id"),
    ("graph", "GetSimilarObjects",      "GET",    "/api/graph/objects/:id/similar"),
    ("graph", "CreateObject",           "POST",   "/api/graph/objects"),
    ("graph", "PatchObject",            "PATCH",  "/api/graph/objects/:id"),
    ("graph", "DeleteObject",           "DELETE", "/api/graph/objects/:id"),
    ("graph", "RestoreObject",          "POST",   "/api/graph/objects/:id/restore"),
    ("graph", "MoveObject",             "POST",   "/api/graph/objects/:id/move"),
    ("graph", "GetObjectHistory",       "GET",    "/api/graph/objects/:id/history"),
    ("graph", "GetObjectEdges",         "GET",    "/api/graph/objects/:id/edges"),
    # graph - search / traverse
    ("graph", "HybridSearch",           "POST",   "/api/graph/search"),
    ("graph", "SearchWithNeighbors",    "POST",   "/api/graph/search-with-neighbors"),
    ("graph", "ExpandGraph",            "POST",   "/api/graph/expand"),
    ("graph", "TraverseGraph",          "POST",   "/api/graph/traverse"),
    ("graph", "CreateSubgraph",         "POST",   "/api/graph/subgraph"),
    # graph - branches
    ("graph", "MergeBranch",            "POST",   "/api/graph/branches/:targetBranchId/merge"),
    ("graph", "ForkBranch",             "POST",   "/api/graph/branches/:id/fork"),
    # graph - analytics
    ("graph", "GetMostAccessed",        "GET",    "/api/graph/analytics/most-accessed"),
    ("graph", "GetUnused",              "GET",    "/api/graph/analytics/unused"),
    # graph - relationships
    ("graph", "ListRelationships",      "GET",    "/api/graph/relationships/search"),
    ("graph", "CountRelationships",     "GET",    "/api/graph/relationships/count"),
    ("graph", "BulkCreateRelationships","POST",   "/api/graph/relationships/bulk"),
    ("graph", "UpsertRelationship",     "PUT",    "/api/graph/relationships/upsert"),
    ("graph", "GetRelationship",        "GET",    "/api/graph/relationships/:id"),
    ("graph", "CreateRelationship",     "POST",   "/api/graph/relationships"),
    ("graph", "PatchRelationship",      "PATCH",  "/api/graph/relationships/:id"),
    ("graph", "DeleteRelationship",     "DELETE", "/api/graph/relationships/:id"),
    ("graph", "RestoreRelationship",    "POST",   "/api/graph/relationships/:id/restore"),
    ("graph", "GetRelationshipHistory", "GET",    "/api/graph/relationships/:id/history"),
    # extraction - jobs
    ("extraction", "ListJobs",          "GET",    "/api/admin/extraction-jobs/projects/:projectId"),
    ("extraction", "GetStatistics",     "GET",    "/api/admin/extraction-jobs/projects/:projectId/statistics"),
    ("extraction", "GetJob",            "GET",    "/api/admin/extraction-jobs/:jobId"),
    ("extraction", "GetLogs",           "GET",    "/api/admin/extraction-jobs/:jobId/logs"),
    ("extraction", "CreateJob",         "POST",   "/api/admin/extraction-jobs"),
    ("extraction", "BulkCancelJobs",    "POST",   "/api/admin/extraction-jobs/projects/:projectId/bulk-cancel"),
    ("extraction", "BulkDeleteJobs",    "DELETE", "/api/admin/extraction-jobs/projects/:projectId/bulk-delete"),
    ("extraction", "BulkRetryJobs",     "POST",   "/api/admin/extraction-jobs/projects/:projectId/bulk-retry"),
    ("extraction", "UpdateJob",         "PATCH",  "/api/admin/extraction-jobs/:jobId"),
    ("extraction", "DeleteJob",         "DELETE", "/api/admin/extraction-jobs/:jobId"),
    ("extraction", "CancelJob",         "POST",   "/api/admin/extraction-jobs/:jobId/cancel"),
    ("extraction", "RetryJob",          "POST",   "/api/admin/extraction-jobs/:jobId/retry"),
    # extraction - embedding controls
    ("extraction", "EmbeddingStatus",   "GET",    "/api/embeddings/status"),
    ("extraction", "EmbeddingProgress", "GET",    "/api/embeddings/progress"),
    ("extraction", "EmbeddingPause",    "POST",   "/api/embeddings/pause"),
    ("extraction", "EmbeddingResume",   "POST",   "/api/embeddings/resume"),
    ("extraction", "EmbeddingConfig",   "PATCH",  "/api/embeddings/config"),
    # provider
    ("provider", "SaveOrgConfig",           "PUT",    "/api/v1/organizations/:orgId/providers/:provider"),
    ("provider", "GetOrgConfig",            "GET",    "/api/v1/organizations/:orgId/providers/:provider"),
    ("provider", "DeleteOrgConfig",         "DELETE", "/api/v1/organizations/:orgId/providers/:provider"),
    ("provider", "ListOrgConfigs",          "GET",    "/api/v1/organizations/:orgId/providers"),
    ("provider", "ListProjectConfigs",      "GET",    "/api/v1/organizations/:orgId/project-providers"),
    ("provider", "GetOrgUsageSummary",      "GET",    "/api/v1/organizations/:orgId/usage"),
    ("provider", "GetOrgUsageTimeSeries",   "GET",    "/api/v1/organizations/:orgId/usage/timeseries"),
    ("provider", "GetOrgUsageByProject",    "GET",    "/api/v1/organizations/:orgId/usage/by-project"),
    ("provider", "SaveProjectConfig",       "PUT",    "/api/v1/projects/:projectId/providers/:provider"),
    ("provider", "GetProjectConfig",        "GET",    "/api/v1/projects/:projectId/providers/:provider"),
    ("provider", "DeleteProjectConfig",     "DELETE", "/api/v1/projects/:projectId/providers/:provider"),
    ("provider", "GetProjectUsageSummary",  "GET",    "/api/v1/projects/:projectId/usage"),
    ("provider", "GetProjectUsageTimeSeries","GET",   "/api/v1/projects/:projectId/usage/timeseries"),
    ("provider", "ListModels",              "GET",    "/api/v1/providers/:provider/models"),
    ("provider", "TestProvider",            "POST",   "/api/v1/providers/:provider/test"),
    # integrations
    ("integrations", "ListAvailable",   "GET",    "/api/integrations/available"),
    ("integrations", "List",            "GET",    "/api/integrations"),
    ("integrations", "Get",             "GET",    "/api/integrations/:name"),
    ("integrations", "GetPublic",       "GET",    "/api/integrations/:name/public"),
    ("integrations", "Create",          "POST",   "/api/integrations"),
    ("integrations", "Update",          "PUT",    "/api/integrations/:name"),
    ("integrations", "Delete",          "DELETE", "/api/integrations/:name"),
    ("integrations", "TestConnection",  "POST",   "/api/integrations/:name/test"),
    ("integrations", "TriggerSync",     "POST",   "/api/integrations/:name/sync"),
    # orgs
    ("orgs", "List",                    "GET",    "/api/orgs"),
    ("orgs", "Get",                     "GET",    "/api/orgs/:id"),
    ("orgs", "Create",                  "POST",   "/api/orgs"),
    ("orgs", "Delete",                  "DELETE", "/api/orgs/:id"),
    ("orgs", "ListToolSettings",        "GET",    "/api/admin/orgs/:orgId/tool-settings"),
    ("orgs", "UpsertToolSetting",       "PUT",    "/api/admin/orgs/:orgId/tool-settings/:toolName"),
    ("orgs", "DeleteToolSetting",       "DELETE", "/api/admin/orgs/:orgId/tool-settings/:toolName"),
    # projects
    ("projects", "List",                "GET",    "/api/projects"),
    ("projects", "Get",                 "GET",    "/api/projects/:id"),
    ("projects", "Create",              "POST",   "/api/projects"),
    ("projects", "Update",              "PATCH",  "/api/projects/:id"),
    ("projects", "Delete",              "DELETE", "/api/projects/:id"),
    ("projects", "ListMembers",         "GET",    "/api/projects/:id/members"),
    ("projects", "RemoveMember",        "DELETE", "/api/projects/:id/members/:userId"),
    # health
    ("health", "Health",                "GET",    "/health"),
    ("health", "Ready",                 "GET",    "/ready"),
    ("health", "Debug",                 "GET",    "/debug"),
    ("health", "Diagnose",              "GET",    "/api/diagnostics"),
    ("health", "JobMetrics",            "GET",    "/api/metrics/jobs"),
    ("health", "SchedulerMetrics",      "GET",    "/api/metrics/scheduler"),
    # superadmin
    ("superadmin", "GetMe",                         "GET",    "/api/superadmin/me"),
    ("superadmin", "DeleteUser",                    "DELETE", "/api/superadmin/users/:id"),
    ("superadmin", "ListOrganizations",             "GET",    "/api/superadmin/organizations"),
    ("superadmin", "DeleteOrganization",            "DELETE", "/api/superadmin/organizations/:id"),
    ("superadmin", "ListProjects",                  "GET",    "/api/superadmin/projects"),
    ("superadmin", "DeleteProject",                 "DELETE", "/api/superadmin/projects/:id"),
    ("superadmin", "ListEmailJobs",                 "GET",    "/api/superadmin/email-jobs"),
    ("superadmin", "GetEmailJobPreview",            "GET",    "/api/superadmin/email-jobs/:id/preview-json"),
    ("superadmin", "ListEmbeddingJobs",             "GET",    "/api/superadmin/embedding-jobs"),
    ("superadmin", "DeleteEmbeddingJobs",           "POST",   "/api/superadmin/embedding-jobs/delete"),
    ("superadmin", "CleanupOrphanEmbeddingJobs",    "POST",   "/api/superadmin/embedding-jobs/cleanup-orphans"),
    ("superadmin", "ResetDeadLetterEmbeddingJobs",  "POST",   "/api/superadmin/embedding-jobs/reset-dead-letter"),
    ("superadmin", "ListExtractionJobs",            "GET",    "/api/superadmin/extraction-jobs"),
    ("superadmin", "DeleteExtractionJobs",          "POST",   "/api/superadmin/extraction-jobs/delete"),
    ("superadmin", "CancelExtractionJobs",          "POST",   "/api/superadmin/extraction-jobs/cancel"),
    ("superadmin", "ListDocumentParsingJobs",       "GET",    "/api/superadmin/document-parsing-jobs"),
    ("superadmin", "DeleteDocumentParsingJobs",     "POST",   "/api/superadmin/document-parsing-jobs/delete"),
    ("superadmin", "RetryDocumentParsingJobs",      "POST",   "/api/superadmin/document-parsing-jobs/retry"),
    ("superadmin", "ListSyncJobs",                  "GET",    "/api/superadmin/sync-jobs"),
    ("superadmin", "GetSyncJobLogs",                "GET",    "/api/superadmin/sync-jobs/:id/logs"),
    ("superadmin", "DeleteSyncJobs",                "POST",   "/api/superadmin/sync-jobs/delete"),
    ("superadmin", "CancelSyncJobs",                "POST",   "/api/superadmin/sync-jobs/cancel"),
    ("superadmin", "CreateServiceToken",            "POST",   "/api/superadmin/service-tokens"),
    # schemaregistry (additional)
    ("schemaregistry", "GetObjectType",     "GET",    "/api/schema-registry/projects/:projectId/types/:typeName"),
    ("schemaregistry", "GetTypeStats",      "GET",    "/api/schema-registry/projects/:projectId/stats"),
    ("schemaregistry", "CreateType",        "POST",   "/api/schema-registry/projects/:projectId/types"),
    ("schemaregistry", "UpdateType",        "PUT",    "/api/schema-registry/projects/:projectId/types/:typeName"),
    ("schemaregistry", "DeleteType",        "DELETE", "/api/schema-registry/projects/:projectId/types/:typeName"),
    # sandboximages
    ("sandboximages", "List",               "GET",    "/api/admin/sandbox-images"),
    ("sandboximages", "Get",                "GET",    "/api/admin/sandbox-images/:id"),
    ("sandboximages", "Create",             "POST",   "/api/admin/sandbox-images"),
    ("sandboximages", "Delete",             "DELETE", "/api/admin/sandbox-images/:id"),
    # useractivity (additional)
    ("useractivity", "GetRecentByType",     "GET",    "/api/user-activity/recent/:type"),
    ("useractivity", "DeleteAll",           "DELETE", "/api/user-activity/recent"),
    ("useractivity", "DeleteByResource",    "DELETE", "/api/user-activity/recent/:type/:resourceId"),
    # tracing / observability
    ("tracing", "ListTraces",               "GET",    "/api/traces"),
    ("tracing", "SearchTraces",             "GET",    "/api/traces/search"),
    ("tracing", "GetTrace",                 "GET",    "/api/traces/:id"),
    # backups
    ("backups", "ListBackups",              "GET",    "/api/v1/organizations/:orgId/backups"),
    ("backups", "GetBackup",                "GET",    "/api/v1/organizations/:orgId/backups/:backupId"),
    ("backups", "DownloadBackup",           "GET",    "/api/v1/organizations/:orgId/backups/:backupId/download"),
    ("backups", "DeleteBackup",             "DELETE", "/api/v1/organizations/:orgId/backups/:backupId"),
    ("backups", "CreateBackup",             "POST",   "/api/v1/projects/:projectId/backups"),
    ("backups", "RestoreBackup",            "POST",   "/api/v1/projects/:projectId/restore"),
    ("backups", "GetRestoreStatus",         "GET",    "/api/v1/projects/:projectId/restores/:restoreId"),
    # invites
    ("invites", "ListPending",              "GET",    "/api/invites/pending"),
    ("invites", "ListByProject",            "GET",    "/api/projects/:projectId/invites"),
    ("invites", "Create",                   "POST",   "/api/invites"),
    ("invites", "Accept",                   "POST",   "/api/invites/accept"),
    ("invites", "Decline",                  "POST",   "/api/invites/:id/decline"),
    ("invites", "Delete",                   "DELETE", "/api/invites/:id"),
    # userprofile
    ("userprofile", "Get",                  "GET",    "/api/user/profile"),
    ("userprofile", "Update",               "PUT",    "/api/user/profile"),
]


# ─── 2. Complete scenario → endpoint mapping ─────────────────────────────────

EXPLICIT = {
    # ── agents ──────────────────────────────────────────────────────────────
    "s-agents-create-definition":           ["ep-agents-createdefinition"],
    "s-agents-list-definitions":            ["ep-agents-listdefinitions"],
    "s-agents-get-definition":              ["ep-agents-getdefinition"],
    "s-agents-update-definition":           ["ep-agents-updatedefinition"],
    "s-agents-delete-definition":           ["ep-agents-deletedefinition"],
    "s-agents-cli-manage":                  ["ep-agents-listagents", "ep-agents-listdefinitions", "ep-agents-triggeragent", "ep-agents-listprojectruns", "ep-agents-getprojectrun"],
    "s-agents-trigger-run":                 ["ep-agents-triggeragent"],
    "s-agents-cancel-run":                  ["ep-agents-cancelrun"],
    "s-agents-list-runs":                   ["ep-agents-listprojectruns"],
    "s-agents-get-run":                     ["ep-agents-getprojectrun"],
    "s-agents-get-run-messages":            ["ep-agents-getrunmessages"],
    "s-agents-get-run-tool-calls":          ["ep-agents-getruntoolcalls"],
    "s-agents-get-run-steps":               ["ep-agents-getrunsteps"],
    "s-agents-get-run-logs":                ["ep-agents-getrunlogs"],
    "s-agents-push-run-status-event":       ["ep-events-handlestream"],
    "s-agents-batch-trigger":               ["ep-agents-batchtrigger"],
    "s-agents-create-webhook":              ["ep-agents-createwebhookhook"],
    "s-agents-list-webhooks":               ["ep-agents-listwebhookhooks"],
    "s-agents-delete-webhook":              ["ep-agents-deletewebhookhook"],
    "s-agents-receive-webhook":             ["ep-agents-receivewebhook"],
    "s-agents-get-sandbox-config":          ["ep-agents-getsandboxconfig"],
    "s-agents-update-sandbox-config":       ["ep-agents-updatesandboxconfig"],
    "s-agents-respond-to-question":         ["ep-agents-handlerespondtoquestion"],
    "s-agents-list-questions-by-run":       ["ep-agents-handlelistquestionsbyrun"],
    "s-agents-list-questions-by-project":   ["ep-agents-handlelistquestionsbyproject"],
    "s-agents-list-questions":              ["ep-agents-handlelistquestionsbyproject"],
    "s-agents-get-adk-sessions":            ["ep-agents-getadksessions"],
    "s-agents-get-adk-session":             ["ep-agents-getadksessionbyid"],
    "s-agents-list-overrides":              ["ep-agents-listagentoverrides"],
    "s-agents-get-override":               ["ep-agents-getagentoverride"],
    "s-agents-set-override":               ["ep-agents-setagentoverride"],
    "s-agents-delete-override":            ["ep-agents-deleteagentoverride"],
    "s-agents-get-pending-events":          ["ep-agents-getpendingevents"],
    "s-agents-get-session":                 ["ep-agents-getsession"],
    "s-agents-sandbox-attach-session":      ["ep-agents-getsession"],
    "s-agents-skills-list":                 ["ep-skills-listglobalskills"],
    # ── sandbox ──────────────────────────────────────────────────────────────
    "s-agents-sandbox-create":              ["ep-sandbox-createworkspace"],
    "s-agents-sandbox-get":                 ["ep-sandbox-getworkspace"],
    "s-agents-sandbox-list":                ["ep-sandbox-listworkspaces"],
    "s-agents-sandbox-delete":              ["ep-sandbox-deleteworkspace"],
    # ── sandboximages ────────────────────────────────────────────────────────
    "s-sandboximages-list":                 ["ep-sandboximages-list"],
    "s-sandboximages-get":                  ["ep-sandboximages-get"],
    "s-sandboximages-create":               ["ep-sandboximages-create"],
    "s-sandboximages-delete":               ["ep-sandboximages-delete"],
    # ── branches ─────────────────────────────────────────────────────────────
    "s-graph-branches-list":                ["ep-branches-list"],
    "s-graph-branches-create":              ["ep-branches-create"],
    "s-graph-branches-get":                 ["ep-branches-getbyid"],
    "s-graph-branches-update":              ["ep-branches-update"],
    "s-graph-branches-delete":              ["ep-branches-delete"],
    # ── chat ─────────────────────────────────────────────────────────────────
    "s-chat-list-conversations":            ["ep-chat-listconversations"],
    "s-chat-create-conversation":           ["ep-chat-createconversation"],
    "s-chat-get-conversation":              ["ep-chat-getconversation"],
    "s-chat-update-conversation":           ["ep-chat-updateconversation"],
    "s-chat-delete-conversation":           ["ep-chat-deleteconversation"],
    "s-chat-add-message":                   ["ep-chat-addmessage"],
    "s-chat-stream":                        ["ep-chat-streamchat"],
    "s-chat-query":                         ["ep-chat-querystream"],
    "s-chat-ask":                           ["ep-chat-askstream"],
    # ── documents ────────────────────────────────────────────────────────────
    "s-documents-list":                     ["ep-documents-list"],
    "s-documents-get":                      ["ep-documents-getbyid"],
    "s-documents-create":                   ["ep-documents-create"],
    "s-documents-upload":                   ["ep-documents-upload"],
    "s-documents-delete":                   ["ep-documents-delete"],
    "s-documents-bulk-delete":              ["ep-documents-bulkdelete"],
    "s-documents-get-content":              ["ep-documents-getcontent"],
    "s-documents-download":                 ["ep-documents-download"],
    "s-documents-get-extraction-summary":   ["ep-documents-getextractionsummary"],
    "s-documents-recreate-chunks":          ["ep-chunking-recreatechunks"],
    # ── chunks ───────────────────────────────────────────────────────────────
    "s-chunks-list":                        ["ep-chunks-list"],
    "s-chunks-delete":                      ["ep-chunks-delete"],
    "s-chunks-bulk-delete":                 ["ep-chunks-bulkdelete"],
    "s-chunks-delete-by-document":          ["ep-chunks-deletebydocument"],
    # ── datasources ──────────────────────────────────────────────────────────
    "s-datasources-list":                   ["ep-datasource-list"],
    "s-datasources-get":                    ["ep-datasource-get"],
    "s-datasources-create":                 ["ep-datasource-create"],
    "s-datasources-update":                 ["ep-datasource-update"],
    "s-datasources-delete":                 ["ep-datasource-delete"],
    "s-datasources-test-connection":        ["ep-datasource-testconnection"],
    "s-datasources-trigger-sync":           ["ep-datasource-triggersync"],
    "s-datasources-list-sync-jobs":         ["ep-datasource-listsyncjobs"],
    "s-datasources-get-sync-job":           ["ep-datasource-getsyncjob"],
    "s-datasources-cancel-sync-job":        ["ep-datasource-cancelsyncjob"],
    # ── embedding policies ───────────────────────────────────────────────────
    "s-embeddings-list-policies":           ["ep-embeddingpolicies-list"],
    "s-embeddings-create-policy":           ["ep-embeddingpolicies-create"],
    "s-embeddings-update-policy":           ["ep-embeddingpolicies-update"],
    "s-embeddings-delete-policy":           ["ep-embeddingpolicies-delete"],
    # ── events / SSE ─────────────────────────────────────────────────────────
    "s-events-stream":                      ["ep-events-handlestream"],
    # ── github ───────────────────────────────────────────────────────────────
    "s-githubapp-get-status":               ["ep-githubapp-getstatus"],
    "s-githubapp-connect":                  ["ep-githubapp-connect"],
    "s-githubapp-disconnect":               ["ep-githubapp-disconnect"],
    # ── journal ──────────────────────────────────────────────────────────────
    "s-graph-objects-list-journal":         ["ep-journal-listjournal"],
    "s-graph-objects-add-journal-note":     ["ep-journal-addnote"],
    "s-graph-journal-list":                 ["ep-journal-listjournal"],
    "s-graph-journal-add-note":             ["ep-journal-addnote"],
    # ── mcp ──────────────────────────────────────────────────────────────────
    "s-mcp-list-servers":                   ["ep-mcpregistry-listservers"],
    "s-mcp-get-server":                     ["ep-mcpregistry-getserver"],
    "s-mcp-create-server":                  ["ep-mcpregistry-createserver"],
    "s-mcp-update-server":                  ["ep-mcpregistry-updateserver"],
    "s-mcp-delete-server":                  ["ep-mcpregistry-deleteserver"],
    "s-mcp-list-tools":                     ["ep-mcpregistry-listservertools"],
    "s-mcp-toggle-tool":                    ["ep-mcpregistry-toggletool"],
    "s-mcp-inspect-server":                 ["ep-mcpregistry-inspectserver"],
    "s-mcp-sync-tools":                     ["ep-mcpregistry-synctools"],
    # ── notifications ────────────────────────────────────────────────────────
    "s-notifications-list":                 ["ep-notifications-list"],
    "s-notifications-get-stats":            ["ep-notifications-getstats"],
    "s-notifications-mark-read":            ["ep-notifications-markread"],
    # ── schemas ──────────────────────────────────────────────────────────────
    "s-schemas-get-compiled-types":         ["ep-schemas-getcompiledtypes"],
    "s-schemas-migrate-types":              ["ep-schemas-migratetypes"],
    "s-schemas-get-project-types":          ["ep-schemaregistry-getprojecttypes"],
    # ── search ───────────────────────────────────────────────────────────────
    "s-search-unified":                     ["ep-search-search"],
    "s-search-query":                       ["ep-chat-querystream", "ep-search-search"],
    "s-graph-search-unified":               ["ep-graph-hybridsearch", "ep-search-search"],
    # ── monitoring ───────────────────────────────────────────────────────────
    "s-monitoring-list-extraction-jobs":    ["ep-monitoring-listextractionjobs"],
    # ── tasks ────────────────────────────────────────────────────────────────
    "s-tasks-list":                         ["ep-tasks-list"],
    # ── user activity ────────────────────────────────────────────────────────
    "s-org-user-record-activity":           ["ep-useractivity-record"],
    "s-org-user-get-recent-activity":       ["ep-useractivity-getrecent"],
    "s-useractivity-record":                ["ep-useractivity-record"],
    "s-useractivity-get-recent":            ["ep-useractivity-getrecent"],
    "s-useractivity-get-recent-by-type":    ["ep-useractivity-getrecentbytype"],
    "s-useractivity-delete-all":            ["ep-useractivity-deleteall"],
    "s-useractivity-delete-by-resource":    ["ep-useractivity-deletebyresource"],
    # ── auth ─────────────────────────────────────────────────────────────────
    "s-auth-me":                            ["ep-authinfo-me"],
    # ── discovery jobs ───────────────────────────────────────────────────────
    "s-discoveryjobs-start":                ["ep-discoveryjobs-startdiscovery"],
    "s-discoveryjobs-get-status":           ["ep-discoveryjobs-getjobstatus"],
    "s-discoveryjobs-list":                 ["ep-discoveryjobs-listjobs"],
    "s-discoveryjobs-cancel":               ["ep-discoveryjobs-canceljob"],
    "s-discoveryjobs-finalize":             ["ep-discoveryjobs-finalizediscovery"],
    # ── graph objects ────────────────────────────────────────────────────────
    "s-graph-object-list-paginated":        ["ep-graph-listobjects"],
    "s-graph-object-list-with-filters":     ["ep-graph-listobjects"],
    "s-graph-object-list-by-key":          ["ep-graph-listobjects"],
    "s-graph-object-read":                  ["ep-graph-getobject"],
    "s-graph-object-create":               ["ep-graph-createobject"],
    "s-graph-object-update":               ["ep-graph-patchobject"],
    "s-graph-object-upsert":               ["ep-graph-upsertobject"],
    "s-graph-object-delete":               ["ep-graph-deleteobject"],
    "s-graph-object-restore":              ["ep-graph-restoreobject"],
    "s-graph-object-move":                 ["ep-graph-moveobject"],
    "s-graph-object-history":              ["ep-graph-getobjecthistory"],
    "s-graph-object-edges":                ["ep-graph-getobjectedges"],
    "s-graph-object-similar":              ["ep-graph-getsimilarobjects"],
    "s-graph-object-count":                ["ep-graph-countobjects"],
    "s-graph-object-get-tags":             ["ep-graph-gettags"],
    "s-graph-object-validate":             ["ep-graph-validateobject"],
    "s-graph-object-bulk-create":          ["ep-graph-bulkcreateobjects"],
    "s-graph-object-bulk-update":          ["ep-graph-bulkupdateobjects"],
    "s-graph-object-bulk-update-status":   ["ep-graph-bulkupdatestatus"],
    "s-graph-object-create-subgraph":      ["ep-graph-createsubgraph"],
    "s-graph-object-search-text":          ["ep-graph-ftssearch"],
    "s-graph-object-search-vector":        ["ep-graph-vectorsearch"],
    "s-graph-object-search-hybrid":        ["ep-graph-hybridsearch"],
    "s-graph-object-search-neighbors":     ["ep-graph-searchwithneighbors"],
    "s-graph-object-expand":               ["ep-graph-expandgraph"],
    "s-graph-object-most-accessed":        ["ep-graph-getmostaccessed"],
    "s-graph-object-unused":               ["ep-graph-getunused"],
    "s-graph-object-update-batch":         ["ep-graph-bulkupdateobjects"],
    # ── graph relationships ──────────────────────────────────────────────────
    "s-graph-rel-list":                    ["ep-graph-listrelationships"],
    "s-graph-rel-count":                   ["ep-graph-countrelationships"],
    "s-graph-rel-create":                  ["ep-graph-createrelationship"],
    "s-graph-rel-get":                     ["ep-graph-getrelationship"],
    "s-graph-rel-patch":                   ["ep-graph-patchrelationship"],
    "s-graph-rel-delete":                  ["ep-graph-deleterelationship"],
    "s-graph-rel-restore":                 ["ep-graph-restorerelationship"],
    "s-graph-rel-history":                 ["ep-graph-getrelationshiphistory"],
    "s-graph-rel-upsert":                  ["ep-graph-upsertrelationship"],
    "s-graph-rel-bulk-create":             ["ep-graph-bulkcreaterelationships"],
    "s-graph-rel-traverse":                ["ep-graph-traversegraph"],
    # ── graph traversal ──────────────────────────────────────────────────────
    "s-graph-traverse":                    ["ep-graph-traversegraph"],
    "s-graph-merge-branch":                ["ep-graph-mergebranch"],
    "s-graph-object-fork-branch":          ["ep-graph-forkbranch"],
    "s-cli-graph-branches-fork":           ["ep-graph-forkbranch"],
    "s-cli-graph-branches-get":            ["ep-branches-getbyid"],
    # ── extraction ───────────────────────────────────────────────────────────
    "s-extraction-list-jobs":              ["ep-extraction-listjobs"],
    "s-extraction-get-job":                ["ep-extraction-getjob"],
    "s-extraction-create-job":             ["ep-extraction-createjob"],
    "s-extraction-update-job":             ["ep-extraction-updatejob"],
    "s-extraction-delete-job":             ["ep-extraction-deletejob"],
    "s-extraction-cancel-job":             ["ep-extraction-canceljob"],
    "s-extraction-retry-job":              ["ep-extraction-retryjob"],
    "s-extraction-get-logs":               ["ep-extraction-getlogs"],
    "s-extraction-get-statistics":         ["ep-extraction-getstatistics"],
    "s-extraction-bulk-cancel":            ["ep-extraction-bulkcanceljobs"],
    "s-extraction-bulk-delete":            ["ep-extraction-bulkdeletejobs"],
    "s-extraction-bulk-retry":             ["ep-extraction-bulkretryjobs"],
    "s-extraction-embedding-status":       ["ep-extraction-embeddingstatus"],
    "s-extraction-embedding-progress":     ["ep-extraction-embeddingprogress"],
    "s-extraction-embedding-pause":        ["ep-extraction-embeddingpause"],
    "s-extraction-embedding-resume":       ["ep-extraction-embeddingresume"],
    "s-extraction-embedding-config":       ["ep-extraction-embeddingconfig"],
    "s-extraction-embedding-sweep":        ["ep-extraction-embeddingresume"],
    "s-extraction-chunking-run":           ["ep-chunking-recreatechunks"],
    "s-extraction-monitor":                ["ep-extraction-getstatistics"],
    "s-extraction-llm-history":            ["ep-extraction-getlogs"],
    # ── provider ─────────────────────────────────────────────────────────────
    "s-provider-save-org-config":          ["ep-provider-saveorgconfig"],
    "s-provider-get-org-config":           ["ep-provider-getorgconfig"],
    "s-provider-delete-org-config":        ["ep-provider-deleteorgconfig"],
    "s-provider-list-org-configs":         ["ep-provider-listorgconfigs"],
    "s-provider-list-project-configs":     ["ep-provider-listprojectconfigs"],
    "s-provider-get-org-usage":            ["ep-provider-getorgusagesummary"],
    "s-provider-get-org-usage-ts":         ["ep-provider-getorgusagetimeseries"],
    "s-provider-get-org-usage-by-proj":    ["ep-provider-getorgusagebyproject"],
    "s-provider-save-project-config":      ["ep-provider-saveprojectconfig"],
    "s-provider-get-project-config":       ["ep-provider-getprojectconfig"],
    "s-provider-delete-project-config":    ["ep-provider-deleteprojectconfig"],
    "s-provider-get-project-usage":        ["ep-provider-getprojectusagesummary"],
    "s-provider-get-project-usage-ts":     ["ep-provider-getprojectusagetimeseries"],
    "s-provider-list-models":              ["ep-provider-listmodels"],
    "s-provider-test":                     ["ep-provider-testprovider"],
    "s-provider-cli-configure-org":        ["ep-provider-saveorgconfig"],
    "s-provider-cli-configure-project":    ["ep-provider-saveprojectconfig"],
    "s-provider-cli-list":                 ["ep-provider-listorgconfigs"],
    "s-provider-cli-list-models":          ["ep-provider-listmodels"],
    "s-provider-cli-test":                 ["ep-provider-testprovider"],
    "s-provider-cli-usage":                ["ep-provider-getorgusagesummary"],
    "s-provider-cli-usage-timeseries":     ["ep-provider-getorgusagetimeseries"],
    # ── integrations ─────────────────────────────────────────────────────────
    "s-integrations-list":                 ["ep-integrations-list"],
    "s-integrations-get":                  ["ep-integrations-get"],
    "s-integrations-get-public":           ["ep-integrations-getpublic"],
    "s-integrations-create":               ["ep-integrations-create"],
    "s-integrations-update":               ["ep-integrations-update"],
    "s-integrations-delete":               ["ep-integrations-delete"],
    "s-integrations-test-connection":      ["ep-integrations-testconnection"],
    "s-integrations-trigger-sync":         ["ep-integrations-triggersync"],
    "s-integrations-configure":            ["ep-integrations-create", "ep-integrations-update"],
    "s-integrations-github-connect":       ["ep-githubapp-connect"],
    "s-integrations-github-disconnect":    ["ep-githubapp-disconnect"],
    "s-integrations-github-callback":      ["ep-githubapp-connect"],
    "s-integrations-github-webhook":       ["ep-agents-receivewebhook"],
    # ── orgs ─────────────────────────────────────────────────────────────────
    "s-org-list":                          ["ep-orgs-list"],
    "s-org-get":                           ["ep-orgs-get"],
    "s-org-create":                        ["ep-orgs-create"],
    "s-org-delete":                        ["ep-orgs-delete"],
    # ── projects ─────────────────────────────────────────────────────────────
    "s-projects-list":                     ["ep-projects-list"],
    "s-projects-get":                      ["ep-projects-get"],
    "s-projects-create":                   ["ep-projects-create"],
    "s-projects-update":                   ["ep-projects-update"],
    "s-projects-delete":                   ["ep-projects-delete"],
    "s-projects-list-members":             ["ep-projects-listmembers"],
    "s-projects-remove-member":            ["ep-projects-removemember"],
    # ── backups ──────────────────────────────────────────────────────────────
    "s-projects-backup-list":              ["ep-backups-listbackups"],
    "s-projects-backup-get-restore-status":["ep-backups-getrestorestatus"],
    "s-projects-backup-create":            ["ep-backups-createbackup"],
    "s-projects-backup-delete":            ["ep-backups-deletebackup"],
    "s-projects-backup-download":          ["ep-backups-downloadbackup"],
    "s-projects-backup-restore":           ["ep-backups-restorebackup"],
    "s-projects-backup-status":            ["ep-backups-getrestorestatus"],
    # ── health ───────────────────────────────────────────────────────────────
    "s-health-check":                      ["ep-health-health"],
    "s-health-ready":                      ["ep-health-ready"],
    "s-health-debug":                      ["ep-health-debug"],
    "s-health-diagnose":                   ["ep-health-diagnose"],
    "s-health-metrics":                    ["ep-health-jobmetrics", "ep-health-schedulermetrics"],
    "s-health-cli-doctor":                 ["ep-health-diagnose"],
    # ── superadmin ───────────────────────────────────────────────────────────
    "s-superadmin-get-me":                 ["ep-superadmin-getme"],
    "s-superadmin-list-users":             ["ep-superadmin-listusers"],
    "s-superadmin-delete-user":            ["ep-superadmin-deleteuser"],
    "s-superadmin-list-orgs":              ["ep-superadmin-listorganizations"],
    "s-superadmin-delete-org":             ["ep-superadmin-deleteorganization"],
    "s-superadmin-list-projects":          ["ep-superadmin-listprojects"],
    "s-superadmin-delete-project":         ["ep-superadmin-deleteproject"],
    "s-superadmin-list-email-jobs":        ["ep-superadmin-listemailjobs"],
    "s-superadmin-preview-email-job":      ["ep-superadmin-getemailjobjobpreview"],
    "s-superadmin-view-embedding-queue":   ["ep-superadmin-listembeddingjobs"],
    "s-superadmin-delete-embedding-jobs":  ["ep-superadmin-deleteembeddingjobs"],
    "s-superadmin-cleanup-orphan-embeddings":["ep-superadmin-cleanuporphanembeddingjobs"],
    "s-superadmin-reset-dead-letter-embeddings":["ep-superadmin-resetdeadletterembeddingjobs"],
    "s-superadmin-list-extraction-jobs":   ["ep-superadmin-listextractionjobs"],
    "s-superadmin-delete-extraction-jobs": ["ep-superadmin-deleteextractionjobs"],
    "s-superadmin-cancel-extraction-jobs": ["ep-superadmin-cancelextractionjobs"],
    "s-superadmin-list-doc-parsing-jobs":  ["ep-superadmin-listdocumentparsingjobs"],
    "s-superadmin-delete-doc-parsing-jobs":["ep-superadmin-deletedocumentparsingjobs"],
    "s-superadmin-retry-doc-parsing-jobs": ["ep-superadmin-retrydocumentparsingjobs"],
    "s-superadmin-list-sync-jobs":         ["ep-superadmin-listsyncjobs"],
    "s-superadmin-get-sync-job-logs":      ["ep-superadmin-getsyncjoblogs"],
    "s-superadmin-delete-sync-jobs":       ["ep-superadmin-deletesyncjobs"],
    "s-superadmin-cancel-sync-jobs":       ["ep-superadmin-cancelsyncjobs"],
    "s-superadmin-extraction-monitoring":  ["ep-superadmin-listextractionjobs"],
    "s-superadmin-manage-jobs-users":      ["ep-superadmin-listusers", "ep-superadmin-listextractionjobs"],
    "s-superadmin-server-diagnostics":     ["ep-health-diagnose"],
    # ── schemaregistry ───────────────────────────────────────────────────────
    "s-schemaregistry-get-project-types":  ["ep-schemaregistry-getprojecttypes"],
    "s-schemaregistry-get-object-type":    ["ep-schemaregistry-getobjecttype"],
    "s-schemaregistry-get-type-stats":     ["ep-schemaregistry-gettypestats"],
    "s-schemaregistry-create-type":        ["ep-schemaregistry-createtype"],
    "s-schemaregistry-update-type":        ["ep-schemaregistry-updatetype"],
    "s-schemaregistry-delete-type":        ["ep-schemaregistry-deletetype"],
    # ── observability / tracing ──────────────────────────────────────────────
    "s-observability-get-trace":           ["ep-tracing-gettrace"],
    "s-observability-search-traces":       ["ep-tracing-searchtraces"],
    "s-observability-traces-list":         ["ep-tracing-listtraces"],
    "s-observability-traces-search":       ["ep-tracing-searchtraces"],
    # ── blueprints (CLI-only, no direct API) ─────────────────────────────────
    "s-blueprints-apply":                  ["ep-schemaregistry-createtype", "ep-graph-bulkcreateobjects"],
    "s-blueprints-export-graph":           ["ep-graph-listobjects"],
    "s-blueprints-inspect":                ["ep-schemaregistry-getprojecttypes"],
    "s-blueprints-scaffold-project":       ["ep-projects-create", "ep-schemaregistry-createtype"],
    "s-blueprints-validate":               ["ep-schemaregistry-getprojecttypes"],
    # ── CLI scenarios (map to underlying API endpoints) ──────────────────────
    "s-cli-agent-definitions":             ["ep-agents-listdefinitions", "ep-agents-createdefinition"],
    "s-cli-agents":                        ["ep-agents-listagents", "ep-agents-triggeragent"],
    "s-cli-ask":                           ["ep-chat-askstream"],
    "s-cli-auth":                          ["ep-authinfo-me"],
    "s-cli-blueprints":                    ["ep-schemaregistry-getprojecttypes", "ep-graph-bulkcreateobjects"],
    "s-cli-branches":                      ["ep-branches-list", "ep-branches-create", "ep-branches-getbyid"],
    "s-cli-browse":                        ["ep-graph-listobjects", "ep-graph-hybridsearch"],
    "s-cli-documents":                     ["ep-documents-list", "ep-documents-upload"],
    "s-cli-embeddings":                    ["ep-embeddingpolicies-list", "ep-extraction-embeddingstatus"],
    "s-cli-extraction":                    ["ep-extraction-listjobs", "ep-extraction-getjob"],
    "s-cli-graph":                         ["ep-graph-listobjects", "ep-graph-createobject"],
    "s-cli-graph-explore":                 ["ep-graph-hybridsearch", "ep-graph-traversegraph"],
    "s-cli-init-project":                  ["ep-projects-create", "ep-schemaregistry-createtype"],
    "s-cli-journal":                       ["ep-journal-listjournal", "ep-journal-addnote"],
    "s-cli-login":                         ["ep-authinfo-me"],
    "s-cli-logout":                        ["ep-authinfo-me"],
    "s-cli-mcp":                           ["ep-mcpregistry-listservers"],
    "s-cli-mcp-servers":                   ["ep-mcpregistry-listservers", "ep-mcpregistry-createserver"],
    "s-cli-mcp-share":                     ["ep-mcpregistry-listservers"],
    "s-cli-orgs":                          ["ep-orgs-list", "ep-orgs-get"],
    "s-cli-projects":                      ["ep-projects-list", "ep-projects-get"],
    "s-cli-provider":                      ["ep-provider-listorgconfigs", "ep-provider-listmodels"],
    "s-cli-query":                         ["ep-chat-querystream"],
    "s-cli-schemas":                       ["ep-schemas-getcompiledtypes", "ep-schemaregistry-getprojecttypes"],
    "s-cli-schemas-compiled-types":        ["ep-schemas-getcompiledtypes"],
    "s-cli-schemas-create":                ["ep-schemaregistry-createtype"],
    "s-cli-schemas-delete":                ["ep-schemaregistry-deletetype"],
    "s-cli-schemas-diff":                  ["ep-schemaregistry-getprojecttypes"],
    "s-cli-schemas-get":                   ["ep-schemaregistry-getobjecttype"],
    "s-cli-schemas-validate":              ["ep-schemaregistry-getprojecttypes"],
    "s-cli-skills":                        ["ep-skills-listglobalskills"],
    "s-cli-status":                        ["ep-health-health"],
    "s-cli-team":                          ["ep-projects-listmembers"],
    "s-cli-tokens":                        ["ep-authinfo-me"],
    "s-cli-traces":                        ["ep-tracing-listtraces", "ep-tracing-gettrace"],
    "s-cli-acp":                           ["ep-agents-getsession"],
    "s-cli-adk-sessions":                  ["ep-agents-getadksessions"],
    "s-cli-builtin-tools":                 ["ep-skills-listglobalskills"],
    # ── CLI system commands (no direct API) ──────────────────────────────────
    "s-cli-changelog":                     [],
    "s-cli-completion":                    [],
    "s-cli-ctl":                           ["ep-health-health"],
    "s-cli-db":                            [],
    "s-cli-db-bench":                      [],
    "s-cli-db-diagnose":                   ["ep-health-diagnose"],
    "s-cli-doctor":                        ["ep-health-diagnose"],
    "s-cli-register":                      ["ep-authinfo-me"],
    "s-cli-server-install":                [],
    "s-cli-server-uninstall":              [],
    "s-cli-server-upgrade":                [],
    "s-cli-set-token":                     [],
    "s-cli-upgrade":                       [],
    "s-cli-version":                       [],
}


def ep_key(domain: str, handler: str) -> str:
    """Generate endpoint key from domain + handler name."""
    return f"ep-{domain}-{handler.lower()}"


def main():
    # ── Step 1: Create missing endpoints ────────────────────────────────────
    print(f"\n{'='*60}\nCreating missing APIEndpoints...\n{'='*60}")
    created_eps = 0
    ep_id_map: dict[str, str] = {}

    for domain, handler, method, path in NEW_ENDPOINTS:
        key = ep_key(domain, handler)
        props = {
            "method": method,
            "path": path,
            "handler": handler,
            "domain": domain,
        }
        obj_id = create_object(key, "APIEndpoint", props)
        if obj_id:
            ep_id_map[key] = obj_id
            created_eps += 1
            print(f"  ✓ {key}")

    print(f"\nCreated/found {created_eps} endpoints")

    # ── Step 2: Load all existing endpoints ─────────────────────────────────
    print(f"\n{'='*60}\nLoading all endpoints from graph...\n{'='*60}")
    all_eps = list_objects("APIEndpoint")
    for ep in all_eps:
        k = ep.get("key")
        if k and k not in ep_id_map:
            ep_id_map[k] = ep["id"]
    print(f"  Total endpoints in graph: {len(ep_id_map)}")

    # ── Step 3: Load all scenarios ───────────────────────────────────────────
    print(f"\n{'='*60}\nLoading scenarios...\n{'='*60}")
    scenarios = list_objects("Scenario")
    s_id_map = {s["key"]: s["id"] for s in scenarios if s.get("key")}
    print(f"  Total scenarios: {len(s_id_map)}")

    # ── Step 4: Wire scenarios → endpoints ───────────────────────────────────
    print(f"\n{'='*60}\nWiring Scenario → APIEndpoint (uses)...\n{'='*60}")
    wired = skipped = no_ep = 0

    for s_key, s_id in sorted(s_id_map.items()):
        ep_keys = EXPLICIT.get(s_key)
        if ep_keys is None:
            skipped += 1
            continue
        if not ep_keys:
            # Explicitly mapped to empty list = no API (CLI system commands)
            continue

        matched = []
        for ep_key_str in ep_keys:
            ep_id = ep_id_map.get(ep_key_str)
            if not ep_id:
                # Try fetching
                ep_id = get_id(ep_key_str)
                if ep_id:
                    ep_id_map[ep_key_str] = ep_id
            if ep_id:
                create_rel(s_id, ep_id, "uses")
                matched.append(ep_key_str)
                wired += 1
            else:
                no_ep += 1
                print(f"  MISSING EP: {ep_key_str} (for {s_key})", file=sys.stderr)

        if matched:
            print(f"  ✓ {s_key} → {matched}")

    print(f"\n{'='*60}")
    print(f"Done.")
    print(f"  Endpoints created/found: {len(ep_id_map)}")
    print(f"  Scenarios wired: {wired}")
    print(f"  Scenarios skipped (no mapping): {skipped}")
    print(f"  Missing endpoint refs: {no_ep}")
    print('='*60)


if __name__ == "__main__":
    main()
