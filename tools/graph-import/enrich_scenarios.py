#!/usr/bin/env python3
"""
Bulk enrich all 502 scenarios with ScenarioSteps, Actions, and context wiring.

For each scenario:
  1. Create 3 ScenarioSteps (invoke, process, respond)
  2. Create 1 Action (the CLI/API call)
  3. Wire: scenario --has_step--> steps
  4. Wire: step --occurs_in--> best matching context
  5. Wire: step --performed_by--> actor
  6. Wire: step --has_action--> action

Uses create-batch subgraph format for efficiency (500 obj/rel per call).
"""

import json
import subprocess
import sys
import tempfile
import os
import re
from collections import defaultdict

MEMORY_BIN = os.path.expanduser("~/.memory/bin/memory")

ACTOR_DEVELOPER_ID = "cd5813df-f89d-4bb6-8d1d-c782bc16929e"
ACTOR_SYSTEM_ID = "b65c5bfe-23df-4d29-b4e9-374e7b4e00ae"

# Context IDs by domain/keyword mapping
# Maps domain prefix -> context entity_id to use for occurs_in
DOMAIN_CONTEXT_MAP = {
    "agents": "a0429ac8-69d9-4f52-a4fc-c9ee15e310a8",       # cli-agents-trigger
    "auth": "e409976f-ed9b-481e-aea6-c3b30a5ffa49",          # cli-login
    "branches": "4e126d09-7605-4594-a4a4-f3544cafa622",      # cli-branches
    "blueprints": "e9fa0cd8-8d5b-4ed6-a4bb-5ee6741ace3f",    # cli-blueprints-install
    "browse": "8c7804f0-7b9c-47e0-9241-61b064545177",        # cli-browse
    "chunks": "bc54f00a-fada-4f92-91a0-63f410e23056",        # cli-embeddings
    "chunking": "bc54f00a-fada-4f92-91a0-63f410e23056",      # cli-embeddings
    "config": "76e4d70c-df22-4009-ad0d-0a6722822a55",        # cli-config
    "datasource": "8a33c392-b7fb-4aa2-913e-76ec9429e28c",    # cli-extraction
    "db": "35e33b86-7f3e-42cd-8825-7deed462e64c",            # cli-db
    "discoveryjobs": "8a33c392-b7fb-4aa2-913e-76ec9429e28c", # cli-extraction
    "docs": "5793984d-73a0-4279-b004-7a5d02171e4c",          # cli-documents-list
    "documents": "15b8b5f5-8f39-4ced-b876-547eda54fe5b",     # cli-documents-upload
    "embeddings": "bc54f00a-fada-4f92-91a0-63f410e23056",    # cli-embeddings
    "embeddingpolicies": "bc54f00a-fada-4f92-91a0-63f410e23056",
    "events": "298b9bac-3848-47fb-a790-af6b71652355",        # cli-traces
    "extraction": "8a33c392-b7fb-4aa2-913e-76ec9429e28c",    # cli-extraction
    "explore": "85770dc4-7b40-4984-8a60-8c04042d8cf0",       # cli-explore
    "githubapp": "db609307-bb00-4c37-92e0-ef76823f1ea0",     # cli-graph-objects
    "graph": "db609307-bb00-4c37-92e0-ef76823f1ea0",         # cli-graph-objects
    "health": "be450efb-8b17-4b24-a6d2-f409fba40ae8",        # cli-doctor
    "integrations": "db609307-bb00-4c37-92e0-ef76823f1ea0",  # cli-graph-objects
    "invites": "f9d29af5-cc31-4508-a6db-a68014db0181",       # cli-team
    "journal": "5dbe1652-44aa-4987-97d0-9a433053c2f5",       # cli-journal
    "mcp": "dceb48d9-9ad0-4dac-9f87-eba6add66c13",           # cli-mcp-servers
    "mcpregistry": "dceb48d9-9ad0-4dac-9f87-eba6add66c13",
    "monitoring": "be450efb-8b17-4b24-a6d2-f409fba40ae8",    # cli-doctor
    "notifications": "f9d29af5-cc31-4508-a6db-a68014db0181", # cli-team
    "orgs": "7eee3ee6-881d-43eb-aaee-24db75ed59cf",          # cli-orgs
    "projects": "0c127f84-4d9a-43d9-98e7-80fb1df295ad",      # cli-projects
    "provider": "5d2e090e-abf8-42ee-a551-7d71774fa7c3",      # cli-provider
    "query": "ef201226-b95a-4327-93e0-d66373d628f6",         # cli-query
    "sandbox": "db609307-bb00-4c37-92e0-ef76823f1ea0",       # cli-graph-objects
    "sandboximages": "db609307-bb00-4c37-92e0-ef76823f1ea0",
    "schemaregistry": "ff8f5ae8-8366-4617-bdbb-5275eb6d5bae", # cli-schemas
    "schemas": "ff8f5ae8-8366-4617-bdbb-5275eb6d5bae",       # cli-schemas
    "search": "ef201226-b95a-4327-93e0-d66373d628f6",        # cli-query
    "skills": "e8137831-624a-4aec-84fa-509a5c3c71ac",        # cli-skills
    "superadmin": "7eee3ee6-881d-43eb-aaee-24db75ed59cf",    # cli-orgs
    "tasks": "db609307-bb00-4c37-92e0-ef76823f1ea0",         # cli-graph-objects
    "team": "f9d29af5-cc31-4508-a6db-a68014db0181",          # cli-team
    "tokens": "0cf8ff96-45bb-4cc0-8d49-700896caca56",        # cli-tokens
    "tracing": "298b9bac-3848-47fb-a790-af6b71652355",       # cli-traces
    "traces": "298b9bac-3848-47fb-a790-af6b71652355",        # cli-traces
    "useraccess": "f9d29af5-cc31-4508-a6db-a68014db0181",    # cli-team
    "useractivity": "f9d29af5-cc31-4508-a6db-a68014db0181",
    "userprofile": "f9d29af5-cc31-4508-a6db-a68014db0181",
    "users": "f9d29af5-cc31-4508-a6db-a68014db0181",
    "version": "92701dac-ef94-4e28-9efc-e377ba77ad96",       # cli-version
    "upgrade": "f5572798-9745-4592-be52-1bbfbee9f994",       # cli-upgrade
    "install": "212c6cba-fd5b-431e-8623-dee1f41b4ba4",       # cli-install
    "init": "29dc6ee5-72ed-4ccc-af6d-bc40f5191e4b",          # cli-init
    "server": "7f6118e3-7175-4a33-9060-c5e0c94a8654",        # cli-server
    "adk": "3df9c7fc-9021-41c3-bf1a-e55297b3da81",           # cli-adk-sessions
    "acp": "dc0be006-13d2-4b61-a335-6944460b9ca9",           # cli-acp-runs
    "ask": "77c80938-77b9-476b-bf1a-25accfcaf730",           # cli-ask
}

# Default fallback context
DEFAULT_CONTEXT_ID = "a22e269a-4ab9-4477-893f-e72cea6333a1"  # ctx-cli-terminal


def extract_domain_from_key(key: str) -> str:
    """Extract domain from scenario key like s-agents-create-definition -> agents"""
    if not key:
        return ""
    parts = key.split("-")
    if len(parts) >= 2 and parts[0] == "s":
        return parts[1]
    return ""


def get_context_for_scenario(scenario: dict) -> str:
    """Get best matching context ID for a scenario."""
    key = scenario.get("key", "")
    props = scenario.get("properties", {})
    
    # Try domain from properties first
    domain = props.get("domain", "") or extract_domain_from_key(key)
    
    if domain in DOMAIN_CONTEXT_MAP:
        return DOMAIN_CONTEXT_MAP[domain]
    
    # Try partial match on key
    for dom_key, ctx_id in DOMAIN_CONTEXT_MAP.items():
        if dom_key in key:
            return ctx_id
    
    return DEFAULT_CONTEXT_ID


def get_actor_for_scenario(scenario: dict) -> str:
    """Determine actor: system for push/event/background scenarios, developer otherwise."""
    key = scenario.get("key", "")
    name = scenario.get("properties", {}).get("name", "").lower()
    
    system_keywords = ["push", "event", "background", "system", "auto", "trigger", "schedule", 
                       "webhook", "notify", "emit", "broadcast", "process", "index", "embed",
                       "extract", "chunk", "discover", "sync", "monitor", "health"]
    
    for kw in system_keywords:
        if kw in key or kw in name:
            return ACTOR_SYSTEM_ID
    
    return ACTOR_DEVELOPER_ID


def make_step_descriptions(scenario: dict) -> list:
    """Generate 3 steps for a scenario."""
    name = scenario.get("properties", {}).get("name", "Perform action")
    key = scenario.get("key", "")
    domain = extract_domain_from_key(key)
    
    # Determine action verb from name
    name_lower = name.lower()
    if any(w in name_lower for w in ["create", "add", "register", "provision", "upload", "import"]):
        steps = [
            {"order": 1, "name": f"Prepare {domain} creation request", "description": f"Gather required parameters and authenticate for: {name}"},
            {"order": 2, "name": f"Submit {domain} create request", "description": f"Call API to create the {domain} resource"},
            {"order": 3, "name": "Confirm creation success", "description": f"Verify {domain} was created and return confirmation"},
        ]
    elif any(w in name_lower for w in ["list", "get", "fetch", "retrieve", "read", "show", "view", "query", "search"]):
        steps = [
            {"order": 1, "name": f"Authenticate and prepare {domain} query", "description": f"Set up authentication and filters for: {name}"},
            {"order": 2, "name": f"Fetch {domain} data", "description": f"Call API to retrieve {domain} records"},
            {"order": 3, "name": "Return formatted results", "description": f"Format and display {domain} results to caller"},
        ]
    elif any(w in name_lower for w in ["update", "edit", "modify", "patch", "set", "configure"]):
        steps = [
            {"order": 1, "name": f"Identify {domain} resource to update", "description": f"Locate target resource and validate update payload for: {name}"},
            {"order": 2, "name": f"Apply {domain} update", "description": f"Call API to update the {domain} resource"},
            {"order": 3, "name": "Confirm update applied", "description": f"Verify {domain} update was persisted successfully"},
        ]
    elif any(w in name_lower for w in ["delete", "remove", "revoke", "cancel", "terminate", "stop"]):
        steps = [
            {"order": 1, "name": f"Identify {domain} resource to delete", "description": f"Locate target resource and confirm deletion intent for: {name}"},
            {"order": 2, "name": f"Execute {domain} deletion", "description": f"Call API to delete the {domain} resource"},
            {"order": 3, "name": "Confirm deletion complete", "description": f"Verify {domain} resource was removed"},
        ]
    elif any(w in name_lower for w in ["push", "emit", "broadcast", "notify", "send", "event"]):
        steps = [
            {"order": 1, "name": f"Detect {domain} state change", "description": f"System detects trigger condition for: {name}"},
            {"order": 2, "name": f"Publish {domain} event", "description": f"System emits event to connected subscribers"},
            {"order": 3, "name": "Confirm event delivered", "description": f"Verify event was received by all subscribers"},
        ]
    elif any(w in name_lower for w in ["login", "auth", "token", "scope", "access"]):
        steps = [
            {"order": 1, "name": "Present credentials", "description": f"User provides authentication credentials for: {name}"},
            {"order": 2, "name": "Validate and issue token", "description": f"Server validates credentials and issues access token"},
            {"order": 3, "name": "Return auth result", "description": f"Return token or rejection with appropriate status"},
        ]
    else:
        steps = [
            {"order": 1, "name": f"Initiate {name}", "description": f"Prepare and authenticate request for: {name}"},
            {"order": 2, "name": f"Execute {name}", "description": f"Call API endpoint to perform the operation"},
            {"order": 3, "name": "Return result", "description": f"Return success or error response to caller"},
        ]
    
    return steps


def run_batch(payload: dict) -> dict:
    """Run create-batch with subgraph payload."""
    with tempfile.NamedTemporaryFile(mode='w', suffix='.json', delete=False) as f:
        json.dump(payload, f)
        fname = f.name
    
    try:
        result = subprocess.run(
            [MEMORY_BIN, "graph", "objects", "create-batch", "--file", fname, "--output", "json"],
            capture_output=True, text=True
        )
        if result.returncode != 0:
            print(f"ERROR: {result.stderr[:500]}", file=sys.stderr)
            return {}
        return json.loads(result.stdout) if result.stdout.strip() else {}
    finally:
        os.unlink(fname)


def check_existing_steps() -> set:
    """Get scenario IDs that already have steps (via has_step relationship)."""
    result = subprocess.run(
        [MEMORY_BIN, "graph", "relationships", "list", "--type", "has_step", "--limit", "2000", "--output", "json"],
        capture_output=True, text=True
    )
    if result.returncode != 0:
        return set()
    try:
        data = json.loads(result.stdout)
        items = data.get("items", [])
        return {r["src_id"] for r in items}
    except:
        return set()


def check_existing_actions() -> set:
    """Get Action keys that already exist."""
    result = subprocess.run(
        [MEMORY_BIN, "graph", "objects", "list", "--type", "Action", "--limit", "2000", "--output", "json"],
        capture_output=True, text=True
    )
    if result.returncode != 0:
        return set()
    try:
        data = json.loads(result.stdout)
        items = data.get("items", [])
        return {obj.get("key", "") for obj in items}
    except:
        return set()


def main():
    print("Loading all scenarios...")
    result = subprocess.run(
        [MEMORY_BIN, "graph", "objects", "list", "--type", "Scenario", "--limit", "600", "--output", "json"],
        capture_output=True, text=True
    )
    if result.returncode != 0:
        print(f"Failed to load scenarios: {result.stderr}", file=sys.stderr)
        sys.exit(1)
    
    data = json.loads(result.stdout)
    scenarios = data["items"]
    print(f"Loaded {len(scenarios)} scenarios")
    
    print("Checking which scenarios already have steps...")
    already_enriched = check_existing_steps()
    print(f"Already enriched: {len(already_enriched)} scenarios")
    
    existing_action_keys = check_existing_actions()
    print(f"Existing action keys: {len(existing_action_keys)}")
    
    to_enrich = [s for s in scenarios if s["entity_id"] not in already_enriched]
    print(f"To enrich: {len(to_enrich)} scenarios")
    
    # Process in batches of ~80 scenarios (each scenario = 3 steps + 1 action = 4 objects + ~10 rels = ~14 items)
    # 80 * 4 = 320 objects, 80 * 10 = 800 rels -> split into 2 calls per batch
    BATCH_SIZE = 40  # scenarios per batch: 40 * 4 obj = 160 obj, 40 * 10 rels = 400 rels (under 500 limit)
    
    total_created_objects = 0
    total_created_rels = 0
    
    for batch_start in range(0, len(to_enrich), BATCH_SIZE):
        batch = to_enrich[batch_start:batch_start + BATCH_SIZE]
        print(f"\nProcessing batch {batch_start//BATCH_SIZE + 1}: scenarios {batch_start+1}-{batch_start+len(batch)}")
        
        objects = []
        relationships = []
        
        for scenario in batch:
            scenario_id = scenario["entity_id"]
            scenario_key = scenario.get("key", "")
            action_key = f"act-{scenario_key}"
            
            # Skip if action already exists (means this scenario was already processed)
            if action_key in existing_action_keys:
                continue
            
            steps = make_step_descriptions(scenario)
            context_id = get_context_for_scenario(scenario)
            actor_id = get_actor_for_scenario(scenario)
            
            # Create action ref
            action_ref = f"action-{scenario_key}"
            objects.append({
                "_ref": action_ref,
                "type": "Action",
                "key": f"act-{scenario_key}",
                "name": f"Execute: {scenario.get('properties', {}).get('name', scenario_key)}",
                "properties": {
                    "name": f"Execute: {scenario.get('properties', {}).get('name', scenario_key)}",
                    "action_type": "api_call",
                    "description": f"Primary action for scenario: {scenario_key}",
                }
            })
            
            step_refs = []
            for step in steps:
                step_ref = f"step-{scenario_key}-{step['order']}"
                step_refs.append(step_ref)
                objects.append({
                    "_ref": step_ref,
                    "type": "ScenarioStep",
                    "key": f"ss-{scenario_key}-{step['order']}",
                    "name": step["name"],
                    "properties": {
                        "name": step["name"],
                        "description": step["description"],
                        "order": step["order"],
                        "step_number": step["order"],
                    }
                })
                
                # Wire step -> scenario
                relationships.append({
                    "type": "has_step",
                    "src_id": scenario_id,
                    "dst_ref": step_ref,
                })
                # Wire step -> context
                relationships.append({
                    "type": "occurs_in",
                    "src_ref": step_ref,
                    "dst_id": context_id,
                })
                # Wire step -> actor
                relationships.append({
                    "type": "performed_by",
                    "src_ref": step_ref,
                    "dst_id": actor_id,
                })
            
            # Wire step 2 (the main action step) -> action
            if len(step_refs) >= 2:
                relationships.append({
                    "type": "has_action",
                    "src_ref": step_refs[1],
                    "dst_ref": action_ref,
                })
        
        payload = {"objects": objects, "relationships": relationships}
        print(f"  Submitting {len(objects)} objects, {len(relationships)} relationships...")
        
        resp = run_batch(payload)
        if resp:
            created_objs = len(resp.get("objects", []))
            created_rels = len(resp.get("relationships", []))
            total_created_objects += created_objs
            total_created_rels += created_rels
            print(f"  ✓ Created {created_objs} objects, {created_rels} relationships")
        else:
            print(f"  ✗ Batch failed or empty response")
    
    print(f"\n=== DONE ===")
    print(f"Total objects created: {total_created_objects}")
    print(f"Total relationships created: {total_created_rels}")


if __name__ == "__main__":
    main()
