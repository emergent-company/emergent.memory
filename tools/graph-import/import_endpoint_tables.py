#!/usr/bin/env python3
"""
Wire APIEndpoints → DataModels via service_reads_from / service_writes_to.
Data sourced from repository.go audit across all domains.
"""

import subprocess
import json
import sys
from pathlib import Path

MEMORY = str(Path.home() / ".memory/bin/memory")

def get_id(key: str) -> str | None:
    r = subprocess.run([MEMORY, "graph", "objects", "get", key, "--json"], capture_output=True, text=True)
    if r.returncode == 0:
        return json.loads(r.stdout)["id"]
    return None

def create_rel(from_id: str, to_id: str, rel_type: str):
    r = subprocess.run(
        [MEMORY, "graph", "relationships", "create",
         "--from", from_id, "--to", to_id, "--type", rel_type, "--upsert"],
        capture_output=True, text=True
    )
    if r.returncode != 0:
        print(f"  WARN {rel_type}: {r.stderr.strip()}", file=sys.stderr)

# table key helper — must match what bulk_import.py generated
def tk(domain: str, table: str) -> str:
    # table is like kb.agent_runs → find matching DataModel key
    # keys are table-<domain>-<struct-slug>
    # We map by table name directly via a lookup
    return TABLE_LOOKUP.get(table)

# Build lookup: table_name → graph key
TABLE_MAP = {
    "kb.agents":                          "table-agents-agent",
    "kb.agent_runs":                      "table-agents-agent-run",
    "kb.agent_processing_log":            "table-agents-agent-processing-log",
    "kb.agent_definitions":               "table-agents-agent-definition",
    "kb.agent_run_messages":              "table-agents-agent-run-message",
    "kb.agent_run_tool_calls":            "table-agents-agent-run-tool-call",
    "kb.agent_questions":                 "table-agents-agent-question",
    "kb.agent_run_jobs":                  "table-agents-agent-run-job",
    "kb.acp_sessions":                    "table-agents-a-c-p-session",
    "kb.agent_webhook_hooks":             "table-agents-webhook-hook",
    "core.api_tokens":                    "table-apitoken-api-token",
    "kb.backups":                         "table-backups-backup",
    "kb.branches":                        "table-branches-branch",
    "kb.chat_conversations":              "table-chat-conversation",
    "kb.chat_messages":                   "table-chat-message",
    "kb.chunks":                          "table-chunks-chunk",
    "kb.documents":                       "table-documents-document",
    "kb.data_source_integrations":        "table-datasource-data-source-integration",
    "kb.data_source_sync_jobs":           "table-datasource-data-source-sync-job",
    "kb.discovery_jobs":                  "table-discoveryjobs-discovery-job",
    "kb.embedding_policies":              "table-embeddingpolicies-embedding-policy",
    "kb.document_parsing_jobs":           "table-extraction-document-parsing-job",
    "kb.chunk_embedding_jobs":            "table-extraction-chunk-embedding-job",
    "kb.graph_embedding_jobs":            "table-extraction-graph-embedding-job",
    "kb.object_extraction_jobs":          "table-extraction-object-extraction-job",
    "core.github_app_config":             "table-githubapp-git-hub-app-config",
    "kb.graph_objects":                   "table-graph-graph-object",
    "kb.graph_relationships":             "table-graph-graph-relationship",
    "kb.integrations":                    "table-integrations-integration",
    "kb.invites":                         "table-invites-invite",
    "kb.project_journal":                 "table-journal-journal-entry",
    "kb.project_journal_notes":           "table-journal-journal-note",
    "kb.mcp_servers":                     "table-mcpregistry-m-c-p-server",
    "kb.mcp_server_tools":                "table-mcpregistry-m-c-p-server-tool",
    "kb.system_process_logs":             "table-monitoring-system-process-log",
    "kb.llm_call_logs":                   "table-monitoring-l-l-m-call-log",
    "kb.notifications":                   "table-notifications-notification",
    "kb.orgs":                            "table-orgs-org",
    "kb.organization_memberships":        "table-orgs-organization-membership",
    "kb.projects":                        "table-projects-project",
    "kb.project_memberships":             "table-projects-project-membership",
    "kb.org_provider_configs":            "table-provider-org-provider-config",
    "kb.project_provider_configs":        "table-provider-project-provider-config",
    "kb.provider_supported_models":       "table-provider-provider-supported-model",
    "kb.agent_sandboxes":                 "table-sandbox-agent-sandbox",
    "kb.sandbox_images":                  "table-sandboximages-sandbox-image",
    "kb.project_object_schema_registry":  "table-schemaregistry-project-object-schema-registry",
    "kb.schema_migration_jobs":           "table-schemas-schema-migration-job",
    "kb.skills":                          "table-skills-skill",
    "core.superadmins":                   "table-superadmin-superadmin",
    "core.user_profiles":                 "table-superadmin-user-profile",
    "kb.tasks":                           "table-tasks-task",
    "kb.user_recent_items":               "table-useractivity-user-recent-item",
}

TABLE_LOOKUP = TABLE_MAP

# endpoint key → (reads: [tables], writes: [tables])
# Derived from repository audit — mapped per handler function
ENDPOINT_TABLES = {
    # ── agents ──────────────────────────────────────────────────────────────
    "ep-agents-listagents":                (["kb.agents"], []),
    "ep-agents-getagent":                  (["kb.agents"], []),
    "ep-agents-getagentruns":              (["kb.agent_runs"], []),
    "ep-agents-createagent":               ([], ["kb.agents"]),
    "ep-agents-updateagent":               (["kb.agents"], ["kb.agents"]),
    "ep-agents-deleteagent":               (["kb.agents"], ["kb.agents"]),
    "ep-agents-triggeragent":              (["kb.agents", "kb.agent_definitions"], ["kb.agent_runs"]),
    "ep-agents-cancelrun":                 (["kb.agent_runs"], ["kb.agent_runs"]),
    "ep-agents-getpendingevents":          (["kb.agent_runs"], []),
    "ep-agents-batchtrigger":              (["kb.agents", "kb.agent_definitions"], ["kb.agent_runs", "kb.agent_run_jobs"]),
    "ep-agents-createwebhookhook":         ([], ["kb.agent_webhook_hooks"]),
    "ep-agents-listwebhookhooks":          (["kb.agent_webhook_hooks"], []),
    "ep-agents-deletewebhookhook":         (["kb.agent_webhook_hooks"], ["kb.agent_webhook_hooks"]),
    "ep-agents-receivewebhook":            (["kb.agent_webhook_hooks", "kb.agent_definitions"], ["kb.agent_runs"]),
    "ep-agents-listdefinitions":           (["kb.agent_definitions"], []),
    "ep-agents-getdefinition":             (["kb.agent_definitions"], []),
    "ep-agents-createdefinition":          ([], ["kb.agent_definitions"]),
    "ep-agents-updatedefinition":          (["kb.agent_definitions"], ["kb.agent_definitions"]),
    "ep-agents-deletedefinition":          (["kb.agent_definitions"], ["kb.agent_definitions"]),
    "ep-agents-listprojectruns":           (["kb.agent_runs"], []),
    "ep-agents-getprojectrun":             (["kb.agent_runs"], []),
    "ep-agents-getrunmessages":            (["kb.agent_run_messages"], []),
    "ep-agents-getruntoolcalls":           (["kb.agent_run_tool_calls"], []),
    "ep-agents-getrunsteps":               (["kb.agent_run_messages", "kb.agent_run_tool_calls"], []),
    "ep-agents-getrunlogs":                (["kb.agent_processing_log"], []),
    "ep-agents-getsession":                (["kb.acp_sessions"], []),
    "ep-agents-getsandboxconfig":          (["kb.agent_definitions"], []),
    "ep-agents-updatesandboxconfig":       (["kb.agent_definitions"], ["kb.agent_definitions"]),
    "ep-agents-handlerespondtoquestion":   (["kb.agent_questions"], ["kb.agent_questions"]),
    "ep-agents-handlelistquestionsbyrun":  (["kb.agent_questions"], []),
    "ep-agents-handlelistquestionsbyproject": (["kb.agent_questions"], []),
    "ep-agents-getadksessions":            (["kb.acp_sessions"], []),
    "ep-agents-getadksessionbyid":         (["kb.acp_sessions"], []),
    "ep-agents-listagentoverrides":        (["kb.agent_definitions"], []),
    "ep-agents-getagentoverride":          (["kb.agent_definitions"], []),
    "ep-agents-setagentoverride":          (["kb.agent_definitions"], ["kb.agent_definitions"]),
    "ep-agents-deleteagentoverride":       (["kb.agent_definitions"], ["kb.agent_definitions"]),
    # ── authinfo ─────────────────────────────────────────────────────────────
    "ep-authinfo-me":                      (["core.user_profiles", "core.api_tokens"], []),
    # ── branches ─────────────────────────────────────────────────────────────
    "ep-branches-list":                    (["kb.branches"], []),
    "ep-branches-getbyid":                 (["kb.branches"], []),
    "ep-branches-create":                  ([], ["kb.branches"]),
    "ep-branches-update":                  (["kb.branches"], ["kb.branches"]),
    "ep-branches-delete":                  (["kb.branches"], ["kb.branches"]),
    # ── chat ─────────────────────────────────────────────────────────────────
    "ep-chat-listconversations":           (["kb.chat_conversations"], []),
    "ep-chat-getconversation":             (["kb.chat_conversations", "kb.chat_messages"], []),
    "ep-chat-createconversation":          ([], ["kb.chat_conversations"]),
    "ep-chat-updateconversation":          (["kb.chat_conversations"], ["kb.chat_conversations"]),
    "ep-chat-deleteconversation":          (["kb.chat_conversations"], ["kb.chat_conversations"]),
    "ep-chat-addmessage":                  (["kb.chat_conversations"], ["kb.chat_messages"]),
    "ep-chat-streamchat":                  (["kb.chat_conversations", "kb.chat_messages"], ["kb.chat_messages"]),
    "ep-chat-querystream":                 (["kb.graph_objects", "kb.chunks"], []),
    "ep-chat-askstream":                   (["kb.graph_objects", "kb.chunks", "kb.agent_definitions"], ["kb.agent_runs"]),
    # ── chunking ─────────────────────────────────────────────────────────────
    "ep-chunking-recreatechunks":          (["kb.documents"], ["kb.chunks"]),
    # ── chunks ───────────────────────────────────────────────────────────────
    "ep-chunks-list":                      (["kb.chunks", "kb.documents"], []),
    "ep-chunks-delete":                    (["kb.chunks"], ["kb.chunks"]),
    "ep-chunks-bulkdelete":                (["kb.chunks"], ["kb.chunks"]),
    "ep-chunks-deletebydocument":          (["kb.chunks"], ["kb.chunks"]),
    "ep-chunks-bulkdeletebydocuments":     (["kb.chunks"], ["kb.chunks"]),
    # ── datasource ───────────────────────────────────────────────────────────
    "ep-datasource-listproviders":         ([], []),
    "ep-datasource-getproviderschema":     ([], []),
    "ep-datasource-testconfig":            ([], []),
    "ep-datasource-list":                  (["kb.data_source_integrations"], []),
    "ep-datasource-getsourcetypes":        (["kb.data_source_integrations"], []),
    "ep-datasource-get":                   (["kb.data_source_integrations"], []),
    "ep-datasource-create":                ([], ["kb.data_source_integrations"]),
    "ep-datasource-update":                (["kb.data_source_integrations"], ["kb.data_source_integrations"]),
    "ep-datasource-delete":                (["kb.data_source_integrations"], ["kb.data_source_integrations"]),
    "ep-datasource-testconnection":        (["kb.data_source_integrations"], []),
    "ep-datasource-triggersync":           (["kb.data_source_integrations"], ["kb.data_source_sync_jobs"]),
    "ep-datasource-listsyncjobs":          (["kb.data_source_sync_jobs"], []),
    "ep-datasource-getlatestsyncjob":      (["kb.data_source_sync_jobs"], []),
    "ep-datasource-getsyncjob":            (["kb.data_source_sync_jobs"], []),
    "ep-datasource-cancelsyncjob":         (["kb.data_source_sync_jobs"], ["kb.data_source_sync_jobs"]),
    # ── discoveryjobs ─────────────────────────────────────────────────────────
    "ep-discoveryjobs-startdiscovery":     (["kb.projects"], ["kb.discovery_jobs"]),
    "ep-discoveryjobs-getjobstatus":       (["kb.discovery_jobs"], []),
    "ep-discoveryjobs-listjobs":           (["kb.discovery_jobs"], []),
    "ep-discoveryjobs-canceljob":          (["kb.discovery_jobs"], ["kb.discovery_jobs"]),
    "ep-discoveryjobs-finalizediscovery":  (["kb.discovery_jobs"], ["kb.project_object_schema_registry"]),
    # ── docs ─────────────────────────────────────────────────────────────────
    "ep-docs-listdocuments":               ([], []),
    "ep-docs-getdocument":                 ([], []),
    # ── documents ────────────────────────────────────────────────────────────
    "ep-documents-list":                   (["kb.documents"], []),
    "ep-documents-getbyid":                (["kb.documents"], []),
    "ep-documents-create":                 ([], ["kb.documents"]),
    "ep-documents-delete":                 (["kb.documents"], ["kb.documents", "kb.chunks"]),
    "ep-documents-bulkdelete":             (["kb.documents"], ["kb.documents", "kb.chunks"]),
    "ep-documents-getsourcetypes":         (["kb.documents"], []),
    "ep-documents-getcontent":             (["kb.documents"], []),
    "ep-documents-download":               (["kb.documents"], []),
    "ep-documents-getextractionsummary":   (["kb.object_extraction_jobs"], []),
    "ep-documents-upload":                 ([], ["kb.documents"]),
    # ── embeddingpolicies ────────────────────────────────────────────────────
    "ep-embeddingpolicies-list":           (["kb.embedding_policies"], []),
    "ep-embeddingpolicies-getbyid":        (["kb.embedding_policies"], []),
    "ep-embeddingpolicies-create":         ([], ["kb.embedding_policies"]),
    "ep-embeddingpolicies-update":         (["kb.embedding_policies"], ["kb.embedding_policies"]),
    "ep-embeddingpolicies-delete":         (["kb.embedding_policies"], ["kb.embedding_policies"]),
    # ── events ───────────────────────────────────────────────────────────────
    "ep-events-handlestream":              ([], []),
    # ── githubapp ────────────────────────────────────────────────────────────
    "ep-githubapp-getstatus":              (["core.github_app_config"], []),
    "ep-githubapp-connect":                ([], ["core.github_app_config"]),
    "ep-githubapp-disconnect":             (["core.github_app_config"], ["core.github_app_config"]),
    # ── journal ──────────────────────────────────────────────────────────────
    "ep-journal-listjournal":              (["kb.project_journal"], []),
    "ep-journal-addnote":                  ([], ["kb.project_journal_notes"]),
    # ── mcpregistry ──────────────────────────────────────────────────────────
    "ep-mcpregistry-listservers":          (["kb.mcp_servers"], []),
    "ep-mcpregistry-getserver":            (["kb.mcp_servers", "kb.mcp_server_tools"], []),
    "ep-mcpregistry-createserver":         ([], ["kb.mcp_servers"]),
    "ep-mcpregistry-updateserver":         (["kb.mcp_servers"], ["kb.mcp_servers"]),
    "ep-mcpregistry-deleteserver":         (["kb.mcp_servers"], ["kb.mcp_servers", "kb.mcp_server_tools"]),
    "ep-mcpregistry-listservertools":      (["kb.mcp_server_tools"], []),
    "ep-mcpregistry-toggletool":           (["kb.mcp_server_tools"], ["kb.mcp_server_tools"]),
    "ep-mcpregistry-inspectserver":        (["kb.mcp_servers"], []),
    "ep-mcpregistry-synctools":            (["kb.mcp_servers"], ["kb.mcp_server_tools"]),
    # ── monitoring ───────────────────────────────────────────────────────────
    "ep-monitoring-listextractionjobs":    (["kb.object_extraction_jobs", "kb.system_process_logs", "kb.llm_call_logs"], []),
    # ── notifications ────────────────────────────────────────────────────────
    "ep-notifications-getstats":           (["kb.notifications"], []),
    "ep-notifications-list":               (["kb.notifications"], []),
    "ep-notifications-markread":           (["kb.notifications"], ["kb.notifications"]),
    # ── sandbox ──────────────────────────────────────────────────────────────
    "ep-sandbox-createworkspace":          ([], ["kb.agent_sandboxes"]),
    "ep-sandbox-getworkspace":             (["kb.agent_sandboxes"], []),
    "ep-sandbox-listworkspaces":           (["kb.agent_sandboxes"], []),
    "ep-sandbox-deleteworkspace":          (["kb.agent_sandboxes"], ["kb.agent_sandboxes"]),
    # ── schemaregistry ───────────────────────────────────────────────────────
    "ep-schemaregistry-getprojecttypes":   (["kb.project_object_schema_registry"], []),
    # ── schemas ──────────────────────────────────────────────────────────────
    "ep-schemas-getcompiledtypes":         (["kb.graph_schemas", "kb.project_schemas"], []),
    "ep-schemas-migratetypes":             (["kb.graph_schemas"], ["kb.schema_migration_jobs"]),
    # ── search ───────────────────────────────────────────────────────────────
    "ep-search-search":                    (["kb.graph_objects", "kb.chunks"], []),
    # ── skills ───────────────────────────────────────────────────────────────
    "ep-skills-listglobalskills":          (["kb.skills"], []),
    # ── superadmin ───────────────────────────────────────────────────────────
    "ep-superadmin-listusers":             (["core.user_profiles"], []),
    # ── tasks ────────────────────────────────────────────────────────────────
    "ep-tasks-list":                       (["kb.tasks"], []),
    # ── useractivity ─────────────────────────────────────────────────────────
    "ep-useractivity-record":              ([], ["kb.user_recent_items"]),
    "ep-useractivity-getrecent":           (["kb.user_recent_items"], []),
}

def main():
    print(f"\n{'='*60}\nWiring APIEndpoints → DataModels...\n{'='*60}")
    reads_total = writes_total = skipped = 0

    for ep_key, (reads, writes) in ENDPOINT_TABLES.items():
        ep_id = get_id(ep_key)
        if not ep_id:
            print(f"  SKIP {ep_key} — endpoint not found")
            skipped += 1
            continue

        for table in reads:
            tkey = TABLE_LOOKUP.get(table)
            if not tkey:
                continue
            tid = get_id(tkey)
            if tid:
                create_rel(ep_id, tid, "service_reads_from")
                reads_total += 1

        for table in writes:
            tkey = TABLE_LOOKUP.get(table)
            if not tkey:
                continue
            tid = get_id(tkey)
            if tid:
                create_rel(ep_id, tid, "service_writes_to")
                writes_total += 1

        if reads or writes:
            print(f"  ✓ {ep_key}  reads={len(reads)} writes={len(writes)}")

    print(f"\n{'='*60}")
    print(f"Done.  reads_wired={reads_total}  writes_wired={writes_total}  skipped={skipped}")
    print('='*60)

if __name__ == "__main__":
    main()
