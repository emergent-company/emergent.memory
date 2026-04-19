#!/usr/bin/env python3
"""
Phase 3: Add remaining missing endpoints + wire remaining 175 scenarios.
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
            if r2.returncode == 0 and r2.stdout.strip():
                return json.loads(r2.stdout)["id"]
        print(f"  WARN create {key}: {result.stderr.strip()}", file=sys.stderr)
        return None
    stdout = result.stdout.strip()
    if not stdout:
        # Fetch by key
        r2 = subprocess.run(
            [MEMORY, "graph", "objects", "get", key, "--json"],
            capture_output=True, text=True
        )
        if r2.returncode == 0 and r2.stdout.strip():
            return json.loads(r2.stdout)["id"]
        return None
    try:
        return json.loads(stdout)["id"]
    except Exception:
        r2 = subprocess.run(
            [MEMORY, "graph", "objects", "get", key, "--json"],
            capture_output=True, text=True
        )
        if r2.returncode == 0 and r2.stdout.strip():
            return json.loads(r2.stdout)["id"]
        return None


def create_rel(from_id: str, to_id: str, rel_type: str):
    r = subprocess.run(
        [MEMORY, "graph", "relationships", "create",
         "--from", from_id, "--to", to_id, "--type", rel_type, "--upsert"],
        capture_output=True, text=True
    )
    if r.returncode != 0:
        print(f"  WARN rel: {r.stderr.strip()}", file=sys.stderr)


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


# ─── New endpoints ────────────────────────────────────────────────────────────

NEW_ENDPOINTS = [
    # skills (additional)
    ("skills", "CreateGlobalSkill",     "POST",   "/api/skills"),
    ("skills", "GetSkill",              "GET",    "/api/skills/:id"),
    ("skills", "UpdateSkill",           "PATCH",  "/api/skills/:id"),
    ("skills", "DeleteSkill",           "DELETE", "/api/skills/:id"),
    ("skills", "ListOrgSkills",         "GET",    "/api/orgs/:orgId/skills"),
    ("skills", "CreateOrgSkill",        "POST",   "/api/orgs/:orgId/skills"),
    ("skills", "UpdateOrgSkill",        "PATCH",  "/api/orgs/:orgId/skills/:id"),
    ("skills", "DeleteOrgSkill",        "DELETE", "/api/orgs/:orgId/skills/:id"),
    ("skills", "ListProjectSkills",     "GET",    "/api/projects/:projectId/skills"),
    ("skills", "CreateProjectSkill",    "POST",   "/api/projects/:projectId/skills"),
    ("skills", "UpdateProjectSkill",    "PATCH",  "/api/projects/:projectId/skills/:id"),
    ("skills", "DeleteProjectSkill",    "DELETE", "/api/projects/:projectId/skills/:id"),
    # apitoken
    ("apitoken", "Create",              "POST",   "/api/projects/:projectId/tokens"),
    ("apitoken", "List",                "GET",    "/api/projects/:projectId/tokens"),
    ("apitoken", "Get",                 "GET",    "/api/projects/:projectId/tokens/:tokenId"),
    ("apitoken", "Revoke",              "DELETE", "/api/projects/:projectId/tokens/:tokenId"),
    ("apitoken", "CreateAccountToken",  "POST",   "/api/tokens"),
    ("apitoken", "ListAccountTokens",   "GET",    "/api/tokens"),
    ("apitoken", "GetAccountToken",     "GET",    "/api/tokens/:tokenId"),
    ("apitoken", "RevokeAccountToken",  "DELETE", "/api/tokens/:tokenId"),
    # tasks (additional)
    ("tasks", "GetCounts",              "GET",    "/api/tasks/counts"),
    ("tasks", "ListAll",                "GET",    "/api/tasks/all"),
    ("tasks", "GetAllCounts",           "GET",    "/api/tasks/all/counts"),
    ("tasks", "GetByID",                "GET",    "/api/tasks/:id"),
    ("tasks", "Resolve",                "POST",   "/api/tasks/:id/resolve"),
    ("tasks", "Cancel",                 "POST",   "/api/tasks/:id/cancel"),
    # mcp (server-side MCP protocol)
    ("mcp", "HandleOAuthProtectedResource", "GET",  "/.well-known/oauth-protected-resource"),
    ("mcp", "HandleInstallRedirect",        "GET",  "/api/mcp/install"),
    ("mcp", "HandleDownloadMCPBundle",      "GET",  "/api/mcp/bundle"),
    ("mcp", "HandleUnifiedEndpoint",        "POST", "/api/mcp"),
    ("mcp", "HandleSSEConnect",             "GET",  "/api/mcp/sse/:projectId"),
    ("mcp", "HandleSSEMessage",             "POST", "/api/mcp/sse/:projectId/message"),
    ("mcp", "HandleRPC",                    "POST", "/api/mcp/rpc"),
    ("mcp", "HandleShareMCPAccess",         "POST", "/api/projects/:projectId/mcp/share"),
    ("mcp", "HandleGenerateMCPBundle",      "GET",  "/api/projects/:projectId/mcp/bundle"),
    # useraccess
    ("useraccess", "GetOrgsAndProjects",    "GET",  "/api/user/orgs-and-projects"),
    # notifications (additional)
    ("notifications", "GetCounts",          "GET",    "/api/notifications/counts"),
    ("notifications", "Dismiss",            "DELETE", "/api/notifications/:id/dismiss"),
    ("notifications", "MarkAllRead",        "POST",   "/api/notifications/mark-all-read"),
    # monitoring (additional)
    ("monitoring", "GetExtractionJobDetail","GET",    "/api/monitoring/extraction-jobs/:id"),
    ("monitoring", "GetExtractionJobLogs",  "GET",    "/api/monitoring/extraction-jobs/:id/logs"),
    ("monitoring", "GetExtractionJobLLMCalls","GET",  "/api/monitoring/extraction-jobs/:id/llm-calls"),
    # events (additional)
    ("events", "HandleConnectionsCount",    "GET",    "/api/events/connections/count"),
    # chunks (additional)
    ("chunks", "BulkDeleteByDocuments",     "DELETE", "/api/chunks/by-documents"),
    # documents (additional)
    ("documents", "UploadBatch",            "POST",   "/api/documents/upload/batch"),
    ("documents", "GetDeletionImpact",      "GET",    "/api/documents/:id/deletion-impact"),
    ("documents", "BulkDeletionImpact",     "POST",   "/api/documents/deletion-impact"),
    # sandbox (additional - from agents domain)
    ("sandbox", "ExecuteCommand",           "POST",   "/api/v1/agent/sandboxes/:id/exec"),
    ("sandbox", "BashCommand",              "POST",   "/api/v1/agent/sandboxes/:id/bash"),
    ("sandbox", "StopWorkspace",            "POST",   "/api/v1/agent/sandboxes/:id/stop"),
    ("sandbox", "ResumeWorkspace",          "POST",   "/api/v1/agent/sandboxes/:id/resume"),
    ("sandbox", "SnapshotWorkspace",        "POST",   "/api/v1/agent/sandboxes/:id/snapshot"),
    ("sandbox", "CreateFromSnapshot",       "POST",   "/api/v1/agent/sandboxes/from-snapshot"),
    ("sandbox", "ListProviders",            "GET",    "/api/v1/agent/sandboxes/providers"),
    ("sandbox", "DetachSession",            "POST",   "/api/v1/agent/sandboxes/:id/detach"),
    # invites (additional)
    ("invites", "Accept",                   "POST",   "/api/invites/accept"),
    ("invites", "Decline",                  "POST",   "/api/invites/:id/decline"),
    # userprofile (additional)
    ("userprofile", "Get",                  "GET",    "/api/user/profile"),
    ("userprofile", "Update",               "PUT",    "/api/user/profile"),
    # agents - schedule
    ("agents", "ScheduleCron",              "POST",   "/api/projects/:projectId/agent-definitions/:id/schedule"),
]


# ─── Remaining scenario → endpoint wiring ────────────────────────────────────

EXPLICIT = {
    # ── agents sandbox ───────────────────────────────────────────────────────
    "s-agents-sandbox-bash":                ["ep-sandbox-bashcommand"],
    "s-agents-sandbox-create-from-snapshot":["ep-sandbox-createfromsnapshot"],
    "s-agents-sandbox-detach-session":      ["ep-sandbox-detachsession"],
    "s-agents-sandbox-execute":             ["ep-sandbox-executecommand"],
    "s-agents-sandbox-list-providers":      ["ep-sandbox-listproviders"],
    "s-agents-sandbox-resume":              ["ep-sandbox-resumeworkspace"],
    "s-agents-sandbox-snapshot":            ["ep-sandbox-snapshotworkspace"],
    "s-agents-sandbox-stop":                ["ep-sandbox-stopworkspace"],
    "s-agents-sandbox-delete":              ["ep-sandbox-deleteworkspace"],
    # ── agents schedule ──────────────────────────────────────────────────────
    "s-agents-schedule-cron":               ["ep-agents-schedulecron"],
    # ── agents view ──────────────────────────────────────────────────────────
    "s-agents-view-run-details":            ["ep-agents-getprojectrun", "ep-agents-getrunmessages"],
    "s-agents-view-run-history":            ["ep-agents-listprojectruns"],
    # ── skills ───────────────────────────────────────────────────────────────
    "s-agents-skills-create":               ["ep-skills-createglobalskill"],
    "s-agents-skills-create-org":           ["ep-skills-createorgskill"],
    "s-agents-skills-create-project":       ["ep-skills-createprojectskill"],
    "s-agents-skills-delete":               ["ep-skills-deleteskill"],
    "s-agents-skills-delete-org":           ["ep-skills-deleteorgskill"],
    "s-agents-skills-delete-project":       ["ep-skills-deleteprojectskill"],
    "s-agents-skills-get":                  ["ep-skills-getskill"],
    "s-agents-skills-install":              ["ep-skills-createprojectskill"],
    "s-agents-skills-list-org":             ["ep-skills-listorgskills"],
    "s-agents-skills-list-project":         ["ep-skills-listprojectskills"],
    "s-agents-skills-update":               ["ep-skills-updateskill"],
    "s-agents-skills-update-org":           ["ep-skills-updateorgskill"],
    "s-agents-skills-update-project":       ["ep-skills-updateprojectskill"],
    # ── auth ─────────────────────────────────────────────────────────────────
    "s-auth-api-token-account":             ["ep-apitoken-listaccounttokens"],
    "s-auth-api-token-project":             ["ep-apitoken-list"],
    "s-auth-autoprovision-existing-org":    ["ep-authinfo-me"],
    "s-auth-autoprovision-new-user":        ["ep-authinfo-me"],
    "s-auth-cli-login-device":              ["ep-authinfo-me"],
    "s-auth-cli-login-token":              ["ep-authinfo-me"],
    "s-auth-cli-logout":                    ["ep-authinfo-me"],
    "s-auth-cli-register":                  ["ep-authinfo-me"],
    "s-auth-cli-status":                    ["ep-authinfo-me"],
    "s-auth-expired-token":                 ["ep-authinfo-me"],
    "s-auth-insufficient-scope":            ["ep-authinfo-me"],
    "s-auth-invalid-token":                 ["ep-authinfo-me"],
    "s-auth-issuer-discovery":              ["ep-authinfo-me"],
    "s-auth-mcp-oauth":                     ["ep-mcp-handleoauthprotectedresource"],
    "s-auth-oidc-device-flow":              ["ep-authinfo-me"],
    "s-auth-oidc-login":                    ["ep-authinfo-me"],
    "s-auth-public-endpoint":              [],
    "s-auth-require-org-admin":             ["ep-authinfo-me"],
    "s-auth-require-org-owner":             ["ep-authinfo-me"],
    "s-auth-require-project-admin":         ["ep-authinfo-me"],
    "s-auth-require-project-user":          ["ep-authinfo-me"],
    "s-auth-require-project-viewer":        ["ep-authinfo-me"],
    "s-auth-require-superadmin":            ["ep-authinfo-me"],
    "s-auth-require-superadmin-readonly":   ["ep-authinfo-me"],
    "s-auth-revoked-token":                 ["ep-authinfo-me"],
    "s-auth-sandbox-ephemeral-token":       ["ep-authinfo-me"],
    "s-auth-scope-agents-run":              ["ep-authinfo-me"],
    "s-auth-scope-data-read":               ["ep-authinfo-me"],
    "s-auth-scope-data-write":              ["ep-authinfo-me"],
    "s-auth-scope-documents-read":          ["ep-authinfo-me"],
    "s-auth-scope-graph-write":             ["ep-authinfo-me"],
    "s-auth-scope-mcp-admin":               ["ep-authinfo-me"],
    "s-auth-service-token":                 ["ep-superadmin-createservicetoken"],
    "s-auth-sse-query-param":               ["ep-events-handlestream"],
    "s-auth-standalone-mode":               ["ep-authinfo-me"],
    "s-auth-token-introspect":              ["ep-authinfo-me"],
    "s-auth-token-type-api-token":          ["ep-apitoken-list"],
    "s-auth-token-type-session":            ["ep-authinfo-me"],
    "s-auth-webhook-hmac":                  ["ep-agents-receivewebhook"],
    "s-auth-wrong-project":                 ["ep-authinfo-me"],
    # ── chat ─────────────────────────────────────────────────────────────────
    "s-chat-completions":                   ["ep-chat-streamchat"],
    "s-chat-query-stream":                  ["ep-chat-querystream"],
    # ── chunks ───────────────────────────────────────────────────────────────
    "s-chunks-bulk-delete-by-documents":    ["ep-chunks-bulkdeletebydocuments"],
    "s-chunks-recreate":                    ["ep-chunking-recreatechunks"],
    # ── datasources ──────────────────────────────────────────────────────────
    "s-datasources-get-latest-sync-job":    ["ep-datasource-getlatestsyncjob"],
    "s-datasources-get-provider-schema":    ["ep-datasource-getproviderschema"],
    "s-datasources-get-source-types":       ["ep-datasource-getsourcetypes"],
    "s-datasources-list-providers":         ["ep-datasource-listproviders"],
    "s-datasources-scheduled-sync":         ["ep-datasource-triggersync"],
    # ── documents ────────────────────────────────────────────────────────────
    "s-documents-bulk-deletion-impact":     ["ep-documents-bulkdeletionimpact"],
    "s-documents-get-deletion-impact":      ["ep-documents-getdeletionimpact"],
    "s-documents-get-source-types":         ["ep-documents-getsourcetypes"],
    "s-documents-trigger-extraction":       ["ep-extraction-createjob"],
    # ── embeddings ───────────────────────────────────────────────────────────
    "s-embeddings-assign-pack":             ["ep-embeddingpolicies-create"],
    "s-embeddings-generate":                ["ep-extraction-embeddingstatus"],
    "s-embeddings-get-policy":              ["ep-embeddingpolicies-getbyid"],
    "s-embeddings-pause":                   ["ep-extraction-embeddingpause"],
    "s-embeddings-resume":                  ["ep-extraction-embeddingresume"],
    "s-embeddings-status":                  ["ep-extraction-embeddingstatus"],
    "s-embeddings-view-queue":              ["ep-extraction-embeddingprogress"],
    # ── events ───────────────────────────────────────────────────────────────
    "s-events-connections-count":           ["ep-events-handleconnectionscount"],
    "s-events-graph-stream":                ["ep-events-handlestream"],
    "s-events-subscribe-sse":               ["ep-events-handlestream"],
    # ── github ───────────────────────────────────────────────────────────────
    "s-githubapp-callback":                 ["ep-githubapp-connect"],
    "s-githubapp-cli-setup":                ["ep-githubapp-connect"],
    "s-githubapp-webhook":                  ["ep-agents-receivewebhook"],
    # ── graph branches ───────────────────────────────────────────────────────
    "s-graph-branches-fork":               ["ep-graph-forkbranch"],
    "s-graph-branches-merge":              ["ep-graph-mergebranch"],
    # ── mcp ──────────────────────────────────────────────────────────────────
    "s-mcp-connect":                        ["ep-mcp-handleunifiedendpoint"],
    "s-mcp-get-registry-server":            ["ep-mcpregistry-getserver"],
    "s-mcp-initialize":                     ["ep-mcp-handleunifiedendpoint"],
    "s-mcp-install-from-registry":          ["ep-mcpregistry-createserver"],
    "s-mcp-list-builtin-tools":             ["ep-skills-listglobalskills"],
    "s-mcp-list-server-tools":              ["ep-mcpregistry-listservertools"],
    "s-mcp-oauth":                          ["ep-mcp-handleoauthprotectedresource"],
    "s-mcp-register-server":               ["ep-mcpregistry-createserver"],
    "s-mcp-rpc-call":                       ["ep-mcp-handlerpc"],
    "s-mcp-search-registry":               ["ep-mcpregistry-listservers"],
    "s-mcp-share":                          ["ep-mcp-handlesharemcpaccess"],
    "s-mcp-sse-connect":                    ["ep-mcp-handlesseconnect"],
    "s-mcp-tools-call":                     ["ep-mcp-handleunifiedendpoint"],
    "s-mcp-tools-list":                     ["ep-mcpregistry-listservertools"],
    "s-mcp-update-builtin-tool":            ["ep-mcpregistry-toggletool"],
    # ── monitoring ───────────────────────────────────────────────────────────
    "s-monitoring-get-extraction-job":      ["ep-monitoring-getextractionjobdetail"],
    "s-monitoring-get-extraction-llm":      ["ep-monitoring-getextractionjobllmcalls"],
    "s-monitoring-get-extraction-logs":     ["ep-monitoring-getextractionjoblogs"],
    # ── notifications ────────────────────────────────────────────────────────
    "s-notifications-dismiss":              ["ep-notifications-dismiss"],
    "s-notifications-get-counts":           ["ep-notifications-getcounts"],
    "s-notifications-mark-all-read":        ["ep-notifications-markallread"],
    "s-notifications-stats":                ["ep-notifications-getstats"],
    # ── org/user ─────────────────────────────────────────────────────────────
    "s-org-user-accept-invite":             ["ep-invites-accept"],
    "s-org-user-activity":                  ["ep-useractivity-getrecent"],
    "s-org-user-auth-issuer":               ["ep-authinfo-me"],
    "s-org-user-auth-me":                   ["ep-authinfo-me"],
    "s-org-user-context":                   ["ep-useraccess-getorgsandprojects"],
    "s-org-user-create-account-token":      ["ep-apitoken-createaccounttoken"],
    "s-org-user-create-api-token":          ["ep-apitoken-create"],
    "s-org-user-create-service-token":      ["ep-superadmin-createservicetoken"],
    "s-org-user-decline-invite":            ["ep-invites-decline"],
    "s-org-user-delete-invite":             ["ep-invites-delete"],
    "s-org-user-get-account-token":         ["ep-apitoken-getaccounttoken"],
    "s-org-user-get-api-token":             ["ep-apitoken-get"],
    "s-org-user-get-orgs-projects":         ["ep-useraccess-getorgsandprojects"],
    "s-org-user-invite":                    ["ep-invites-create"],
    "s-org-user-list-account-tokens":       ["ep-apitoken-listaccounttokens"],
    "s-org-user-list-api-tokens":           ["ep-apitoken-list"],
    "s-org-user-list-invites":              ["ep-invites-listpending"],
    "s-org-user-list-project-invites":      ["ep-invites-listbyproject"],
    "s-org-user-profile":                   ["ep-userprofile-get"],
    "s-org-user-projects-create-token":     ["ep-apitoken-create"],
    "s-org-user-projects-mcp-share":        ["ep-mcp-handlesharemcpaccess"],
    "s-org-user-projects-set":              ["ep-projects-update"],
    "s-org-user-projects-set-budget":       ["ep-projects-update"],
    "s-org-user-projects-set-info":         ["ep-projects-update"],
    "s-org-user-projects-set-provider":     ["ep-provider-saveprojectconfig"],
    "s-org-user-register":                  ["ep-authinfo-me"],
    "s-org-user-revoke-access":             ["ep-projects-removemember"],
    "s-org-user-revoke-account-token":      ["ep-apitoken-revokeaccounttoken"],
    "s-org-user-revoke-api-token":          ["ep-apitoken-revoke"],
    "s-org-user-roles":                     ["ep-authinfo-me"],
    "s-org-user-search":                    ["ep-superadmin-listusers"],
    "s-org-user-team-invite":               ["ep-invites-create"],
    "s-org-user-team-list":                 ["ep-projects-listmembers"],
    "s-org-user-team-remove":               ["ep-projects-removemember"],
    "s-org-user-tokens-cleanup":            ["ep-apitoken-revoke"],
    "s-org-user-update-profile":            ["ep-userprofile-update"],
    # ── schemas ──────────────────────────────────────────────────────────────
    "s-schemas-commit-migration":           ["ep-schemas-migratetypes"],
    "s-schemas-create-pack":                ["ep-schemaregistry-createtype"],
    "s-schemas-delete-pack":                ["ep-schemaregistry-deletetype"],
    "s-schemas-diff":                       ["ep-schemaregistry-getprojecttypes"],
    "s-schemas-execute-migration":          ["ep-schemas-migratetypes"],
    "s-schemas-get-available-packs":        ["ep-schemaregistry-getprojecttypes"],
    "s-schemas-get-installed-packs":        ["ep-schemaregistry-getprojecttypes"],
    "s-schemas-get-migration-job-status":   ["ep-schemas-getcompiledtypes"],
    "s-schemas-get-pack":                   ["ep-schemaregistry-getobjecttype"],
    "s-schemas-install":                    ["ep-schemaregistry-createtype"],
    "s-schemas-list":                       ["ep-schemaregistry-getprojecttypes"],
    "s-schemas-migrate":                    ["ep-schemas-migratetypes"],
    "s-schemas-preview-migration":          ["ep-schemas-getcompiledtypes"],
    "s-schemas-rollback-migration":         ["ep-schemas-migratetypes"],
    "s-schemas-uninstall":                  ["ep-schemaregistry-deletetype"],
    "s-schemas-update-assignment":          ["ep-schemaregistry-updatetype"],
    "s-schemas-update-pack":                ["ep-schemaregistry-updatetype"],
    "s-schemas-validate-objects":           ["ep-graph-validateobject"],
    "s-schemas-view-history":               ["ep-schemaregistry-getprojecttypes"],
    # ── tasks ────────────────────────────────────────────────────────────────
    "s-tasks-cancel":                       ["ep-tasks-cancel"],
    "s-tasks-get":                          ["ep-tasks-getbyid"],
    "s-tasks-get-all-counts":               ["ep-tasks-getallcounts"],
    "s-tasks-get-counts":                   ["ep-tasks-getcounts"],
    "s-tasks-list-all":                     ["ep-tasks-listall"],
    "s-tasks-resolve":                      ["ep-tasks-resolve"],
}


def ep_key(domain: str, handler: str) -> str:
    return f"ep-{domain}-{handler.lower()}"


def main():
    # ── Step 1: Create new endpoints ────────────────────────────────────────
    print(f"\n{'='*60}\nCreating remaining APIEndpoints...\n{'='*60}")
    ep_id_map: dict[str, str] = {}
    created = 0

    for domain, handler, method, path in NEW_ENDPOINTS:
        key = ep_key(domain, handler)
        props = {"method": method, "path": path, "handler": handler, "domain": domain}
        obj_id = create_object(key, "APIEndpoint", props)
        if obj_id:
            ep_id_map[key] = obj_id
            created += 1
            print(f"  ✓ {key}")

    print(f"\nCreated/found {created} endpoints")

    # ── Step 2: Load all existing endpoints ─────────────────────────────────
    print(f"\n{'='*60}\nLoading all endpoints...\n{'='*60}")
    all_eps = list_objects("APIEndpoint")
    for ep in all_eps:
        k = ep.get("key")
        if k and k not in ep_id_map:
            ep_id_map[k] = ep["id"]
    print(f"  Total: {len(ep_id_map)}")

    # ── Step 3: Load scenarios ───────────────────────────────────────────────
    print(f"\n{'='*60}\nLoading scenarios...\n{'='*60}")
    scenarios = list_objects("Scenario")
    s_id_map = {s["key"]: s["id"] for s in scenarios if s.get("key")}
    print(f"  Total: {len(s_id_map)}")

    # ── Step 4: Wire ─────────────────────────────────────────────────────────
    print(f"\n{'='*60}\nWiring remaining scenarios...\n{'='*60}")
    wired = skipped = no_ep = 0

    for s_key, ep_keys in EXPLICIT.items():
        s_id = s_id_map.get(s_key)
        if not s_id:
            print(f"  MISSING SCENARIO: {s_key}", file=sys.stderr)
            skipped += 1
            continue

        if not ep_keys:
            continue  # no API (system-only)

        matched = []
        for ep_key_str in ep_keys:
            ep_id = ep_id_map.get(ep_key_str)
            if not ep_id:
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
    print(f"  Wired: {wired}  Skipped: {skipped}  Missing EPs: {no_ep}")
    print('='*60)


if __name__ == "__main__":
    main()
