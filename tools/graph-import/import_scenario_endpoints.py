#!/usr/bin/env python3
"""
Wire Scenarios → APIEndpoints via 'uses' relationship.
Derives mapping from scenario key slug patterns matched against endpoint keys.
"""

import subprocess
import json
import sys
import re
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

def list_objects(type_: str) -> list[dict]:
    r = subprocess.run([MEMORY, "graph", "objects", "list", "--type", type_, "--json", "--limit", "1000"],
                       capture_output=True, text=True)
    if r.returncode != 0:
        return []
    return json.loads(r.stdout).get("items", [])

# Explicit overrides for scenarios that don't follow simple slug patterns
EXPLICIT = {
    # agents
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
    "s-agents-get-adk-sessions":            ["ep-agents-getadksessions"],
    "s-agents-get-adk-session":             ["ep-agents-getadksessionbyid"],
    "s-agents-list-overrides":              ["ep-agents-listagentoverrides"],
    "s-agents-get-override":                ["ep-agents-getagentoverride"],
    "s-agents-set-override":                ["ep-agents-setagentoverride"],
    "s-agents-delete-override":             ["ep-agents-deleteagentoverride"],
    "s-agents-get-pending-events":          ["ep-agents-getpendingevents"],
    # branches
    "s-graph-branches-list":                ["ep-branches-list"],
    "s-graph-branches-create":              ["ep-branches-create"],
    "s-graph-branches-get":                 ["ep-branches-getbyid"],
    "s-graph-branches-update":              ["ep-branches-update"],
    "s-graph-branches-delete":              ["ep-branches-delete"],
    # chat
    "s-chat-list-conversations":            ["ep-chat-listconversations"],
    "s-chat-create-conversation":           ["ep-chat-createconversation"],
    "s-chat-get-conversation":              ["ep-chat-getconversation"],
    "s-chat-update-conversation":           ["ep-chat-updateconversation"],
    "s-chat-delete-conversation":           ["ep-chat-deleteconversation"],
    "s-chat-add-message":                   ["ep-chat-addmessage"],
    "s-chat-stream":                        ["ep-chat-streamchat"],
    "s-chat-query":                         ["ep-chat-querystream"],
    "s-chat-ask":                           ["ep-chat-askstream"],
    # documents
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
    # chunks
    "s-chunks-list":                        ["ep-chunks-list"],
    "s-chunks-delete":                      ["ep-chunks-delete"],
    "s-chunks-bulk-delete":                 ["ep-chunks-bulkdelete"],
    "s-chunks-delete-by-document":          ["ep-chunks-deletebydocument"],
    # datasource
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
    # embedding policies
    "s-embeddings-list-policies":           ["ep-embeddingpolicies-list"],
    "s-embeddings-create-policy":           ["ep-embeddingpolicies-create"],
    "s-embeddings-update-policy":           ["ep-embeddingpolicies-update"],
    "s-embeddings-delete-policy":           ["ep-embeddingpolicies-delete"],
    # events / SSE
    "s-events-stream":                      ["ep-events-handlestream"],
    # github
    "s-githubapp-get-status":               ["ep-githubapp-getstatus"],
    "s-githubapp-connect":                  ["ep-githubapp-connect"],
    "s-githubapp-disconnect":               ["ep-githubapp-disconnect"],
    # journal
    "s-graph-objects-list-journal":         ["ep-journal-listjournal"],
    "s-graph-objects-add-journal-note":     ["ep-journal-addnote"],
    # mcp registry
    "s-mcp-list-servers":                   ["ep-mcpregistry-listservers"],
    "s-mcp-get-server":                     ["ep-mcpregistry-getserver"],
    "s-mcp-create-server":                  ["ep-mcpregistry-createserver"],
    "s-mcp-update-server":                  ["ep-mcpregistry-updateserver"],
    "s-mcp-delete-server":                  ["ep-mcpregistry-deleteserver"],
    "s-mcp-list-tools":                     ["ep-mcpregistry-listservertools"],
    "s-mcp-toggle-tool":                    ["ep-mcpregistry-toggletool"],
    "s-mcp-inspect-server":                 ["ep-mcpregistry-inspectserver"],
    "s-mcp-sync-tools":                     ["ep-mcpregistry-synctools"],
    # notifications
    "s-notifications-list":                 ["ep-notifications-list"],
    "s-notifications-get-stats":            ["ep-notifications-getstats"],
    "s-notifications-mark-read":            ["ep-notifications-markread"],
    # sandbox
    "s-agents-sandbox-create":              ["ep-sandbox-createworkspace"],
    "s-agents-sandbox-get":                 ["ep-sandbox-getworkspace"],
    "s-agents-sandbox-list":                ["ep-sandbox-listworkspaces"],
    "s-agents-sandbox-delete":              ["ep-sandbox-deleteworkspace"],
    # schemas
    "s-schemas-get-compiled-types":         ["ep-schemas-getcompiledtypes"],
    "s-schemas-migrate-types":              ["ep-schemas-migratetypes"],
    "s-schemas-get-project-types":          ["ep-schemaregistry-getprojecttypes"],
    # search
    "s-search-unified":                     ["ep-search-search"],
    "s-search-query":                       ["ep-chat-querystream", "ep-search-search"],
    # skills
    "s-agents-skills-list":                 ["ep-skills-listglobalskills"],
    # monitoring
    "s-monitoring-list-extraction-jobs":    ["ep-monitoring-listextractionjobs"],
    # tasks
    "s-tasks-list":                         ["ep-tasks-list"],
    # user activity
    "s-org-user-record-activity":           ["ep-useractivity-record"],
    "s-org-user-get-recent-activity":       ["ep-useractivity-getrecent"],
    # auth
    "s-auth-me":                            ["ep-authinfo-me"],
    # discovery jobs
    "s-discoveryjobs-start":                ["ep-discoveryjobs-startdiscovery"],
    "s-discoveryjobs-get-status":           ["ep-discoveryjobs-getjobstatus"],
    "s-discoveryjobs-list":                 ["ep-discoveryjobs-listjobs"],
    "s-discoveryjobs-cancel":               ["ep-discoveryjobs-canceljob"],
    "s-discoveryjobs-finalize":             ["ep-discoveryjobs-finalizediscovery"],
}

# Slug-fragment → endpoint key patterns for auto-matching
# Applied when no explicit mapping exists
SLUG_PATTERNS = [
    # agents
    (r"^s-agents-", "list-agents",         ["ep-agents-listagents"]),
    (r"^s-agents-", "create-agent",        ["ep-agents-createagent"]),
    (r"^s-agents-", "update-agent",        ["ep-agents-updateagent"]),
    (r"^s-agents-", "delete-agent",        ["ep-agents-deleteagent"]),
    # graph objects
    (r"^s-graph-objects-", "list",         ["ep-branches-list"]),  # handled by graph domain
    # documents
    (r"^s-documents-", "list",             ["ep-documents-list"]),
    (r"^s-documents-", "upload",           ["ep-documents-upload"]),
    # search
    (r"^s-search-", "search",              ["ep-search-search"]),
]

def auto_match(scenario_key: str, ep_keys: set[str]) -> list[str]:
    """Try to auto-match a scenario key to endpoint keys by slug similarity."""
    # Extract domain and action from scenario key: s-<domain>-<action...>
    parts = scenario_key.split("-")
    if len(parts) < 3:
        return []

    # domain is parts[1], rest is action slug
    domain = parts[1]
    action_slug = "-".join(parts[2:])

    # Map action verbs to HTTP method prefixes
    verb_map = {
        "list": "list", "create": "create", "get": "get", "update": "update",
        "delete": "delete", "remove": "delete", "add": "create", "view": "get",
        "fetch": "get", "search": "search", "trigger": "trigger", "cancel": "cancel",
        "upload": "upload", "download": "download", "stream": "stream",
        "send": "create", "receive": "receive", "sync": "sync", "toggle": "toggle",
        "inspect": "inspect", "connect": "connect", "disconnect": "disconnect",
        "respond": "respond", "answer": "respond", "batch": "batch",
    }

    first_word = action_slug.split("-")[0]
    verb = verb_map.get(first_word, first_word)

    # Build candidate endpoint key prefix
    ep_prefix = f"ep-{domain}-{verb}"
    matches = [k for k in ep_keys if k.startswith(ep_prefix)]
    return matches[:1]  # take best match only

def main():
    print(f"\n{'='*60}\nLoading scenarios and endpoints...\n{'='*60}")

    scenarios = list_objects("Scenario")
    endpoints = list_objects("APIEndpoint")
    ep_key_set = {e["key"] for e in endpoints if e.get("key")}
    ep_id_map = {e["key"]: e["id"] for e in endpoints if e.get("key")}

    print(f"  Scenarios: {len(scenarios)}")
    print(f"  Endpoints: {len(ep_key_set)}")

    print(f"\n{'='*60}\nWiring Scenario → APIEndpoint (uses)...\n{'='*60}")

    wired = skipped = auto = explicit = 0

    for s in scenarios:
        skey = s.get("key")
        if not skey:
            continue

        sid = s["id"]

        # 1. Check explicit mapping
        ep_keys = EXPLICIT.get(skey)

        # 2. Auto-match if no explicit
        if ep_keys is None:
            ep_keys = auto_match(skey, ep_key_set)
            if ep_keys:
                auto += 1
        else:
            explicit += 1

        if not ep_keys:
            skipped += 1
            continue

        matched = []
        for ep_key in ep_keys:
            ep_id = ep_id_map.get(ep_key)
            if not ep_id:
                ep_id = get_id(ep_key)
            if ep_id:
                create_rel(sid, ep_id, "uses")
                matched.append(ep_key)
                wired += 1

        if matched:
            print(f"  ✓ {skey} → {matched}")

    print(f"\n{'='*60}")
    print(f"Done.  wired={wired}  explicit={explicit}  auto_matched={auto}  skipped={skipped}")
    print('='*60)

if __name__ == "__main__":
    main()
