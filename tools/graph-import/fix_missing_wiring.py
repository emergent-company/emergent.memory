#!/usr/bin/env python3
"""
Fix missing has_step, occurs_in, performed_by, has_action relationships.

Finds scenarios that have ScenarioStep objects (by key pattern ss-<scenario-key>-N)
but are missing has_step relationships, then wires them up.
"""

import json
import subprocess
import sys
import tempfile
import os

MEMORY_BIN = os.path.expanduser("~/.memory/bin/memory")
ACTOR_DEVELOPER_ID = "cd5813df-f89d-4bb6-8d1d-c782bc16929e"
ACTOR_SYSTEM_ID = "b65c5bfe-23df-4d29-b4e9-374e7b4e00ae"
DEFAULT_CONTEXT_ID = "a22e269a-4ab9-4477-893f-e72cea6333a1"  # ctx-cli-terminal

DOMAIN_CONTEXT_MAP = {
    "agents": "a0429ac8-69d9-4f52-a4fc-c9ee15e310a8",
    "auth": "e409976f-ed9b-481e-aea6-c3b30a5ffa49",
    "branches": "4e126d09-7605-4594-a4a4-f3544cafa622",
    "blueprints": "e9fa0cd8-8d5b-4ed6-a4bb-5ee6741ace3f",
    "browse": "8c7804f0-7b9c-47e0-9241-61b064545177",
    "chunks": "bc54f00a-fada-4f92-91a0-63f410e23056",
    "chunking": "bc54f00a-fada-4f92-91a0-63f410e23056",
    "config": "76e4d70c-df22-4009-ad0d-0a6722822a55",
    "datasource": "8a33c392-b7fb-4aa2-913e-76ec9429e28c",
    "db": "35e33b86-7f3e-42cd-8825-7deed462e64c",
    "discoveryjobs": "8a33c392-b7fb-4aa2-913e-76ec9429e28c",
    "docs": "5793984d-73a0-4279-b004-7a5d02171e4c",
    "documents": "15b8b5f5-8f39-4ced-b876-547eda54fe5b",
    "embeddings": "bc54f00a-fada-4f92-91a0-63f410e23056",
    "embeddingpolicies": "bc54f00a-fada-4f92-91a0-63f410e23056",
    "events": "298b9bac-3848-47fb-a790-af6b71652355",
    "extraction": "8a33c392-b7fb-4aa2-913e-76ec9429e28c",
    "explore": "85770dc4-7b40-4984-8a60-8c04042d8cf0",
    "githubapp": "db609307-bb00-4c37-92e0-ef76823f1ea0",
    "graph": "db609307-bb00-4c37-92e0-ef76823f1ea0",
    "health": "be450efb-8b17-4b24-a6d2-f409fba40ae8",
    "integrations": "db609307-bb00-4c37-92e0-ef76823f1ea0",
    "invites": "f9d29af5-cc31-4508-a6db-a68014db0181",
    "journal": "5dbe1652-44aa-4987-97d0-9a433053c2f5",
    "mcp": "dceb48d9-9ad0-4dac-9f87-eba6add66c13",
    "mcpregistry": "dceb48d9-9ad0-4dac-9f87-eba6add66c13",
    "monitoring": "be450efb-8b17-4b24-a6d2-f409fba40ae8",
    "notifications": "f9d29af5-cc31-4508-a6db-a68014db0181",
    "orgs": "7eee3ee6-881d-43eb-aaee-24db75ed59cf",
    "projects": "0c127f84-4d9a-43d9-98e7-80fb1df295ad",
    "provider": "5d2e090e-abf8-42ee-a551-7d71774fa7c3",
    "query": "ef201226-b95a-4327-93e0-d66373d628f6",
    "sandbox": "db609307-bb00-4c37-92e0-ef76823f1ea0",
    "sandboximages": "db609307-bb00-4c37-92e0-ef76823f1ea0",
    "schemaregistry": "ff8f5ae8-8366-4617-bdbb-5275eb6d5bae",
    "schemas": "ff8f5ae8-8366-4617-bdbb-5275eb6d5bae",
    "search": "ef201226-b95a-4327-93e0-d66373d628f6",
    "skills": "e8137831-624a-4aec-84fa-509a5c3c71ac",
    "superadmin": "7eee3ee6-881d-43eb-aaee-24db75ed59cf",
    "tasks": "db609307-bb00-4c37-92e0-ef76823f1ea0",
    "team": "f9d29af5-cc31-4508-a6db-a68014db0181",
    "tokens": "0cf8ff96-45bb-4cc0-8d49-700896caca56",
    "tracing": "298b9bac-3848-47fb-a790-af6b71652355",
    "traces": "298b9bac-3848-47fb-a790-af6b71652355",
    "useraccess": "f9d29af5-cc31-4508-a6db-a68014db0181",
    "useractivity": "f9d29af5-cc31-4508-a6db-a68014db0181",
    "userprofile": "f9d29af5-cc31-4508-a6db-a68014db0181",
    "users": "f9d29af5-cc31-4508-a6db-a68014db0181",
    "version": "92701dac-ef94-4e28-9efc-e377ba77ad96",
    "upgrade": "f5572798-9745-4592-be52-1bbfbee9f994",
    "install": "212c6cba-fd5b-431e-8623-dee1f41b4ba4",
    "init": "29dc6ee5-72ed-4ccc-af6d-bc40f5191e4b",
    "server": "7f6118e3-7175-4a33-9060-c5e0c94a8654",
    "adk": "3df9c7fc-9021-41c3-bf1a-e55297b3da81",
    "acp": "dc0be006-13d2-4b61-a335-6944460b9ca9",
    "ask": "77c80938-77b9-476b-bf1a-25accfcaf730",
    "cli": "a22e269a-4ab9-4477-893f-e72cea6333a1",
}


def run_cmd(args):
    result = subprocess.run(args, capture_output=True, text=True)
    return result


def get_context_for_key(key: str) -> str:
    parts = key.split("-")
    if len(parts) >= 2 and parts[0] == "s":
        domain = parts[1]
        if domain in DOMAIN_CONTEXT_MAP:
            return DOMAIN_CONTEXT_MAP[domain]
    for dom, ctx in DOMAIN_CONTEXT_MAP.items():
        if dom in key:
            return ctx
    return DEFAULT_CONTEXT_ID


def get_actor_for_key(key: str) -> str:
    system_keywords = ["push", "event", "background", "system", "auto", "trigger", "schedule",
                       "webhook", "notify", "emit", "broadcast", "process", "index", "embed",
                       "extract", "chunk", "discover", "sync", "monitor", "health"]
    for kw in system_keywords:
        if kw in key:
            return ACTOR_SYSTEM_ID
    return ACTOR_DEVELOPER_ID


def run_batch_rels(relationships: list) -> bool:
    """Create relationships using relationships create-batch with upsert."""
    # Convert src_id/dst_id to from/to format
    converted = [{"type": r["type"], "from": r["src_id"], "to": r["dst_id"]} for r in relationships]
    with tempfile.NamedTemporaryFile(mode='w', suffix='.json', delete=False) as f:
        json.dump(converted, f)
        fname = f.name
    try:
        result = subprocess.run(
            [MEMORY_BIN, "graph", "relationships", "create-batch", "--file", fname, "--upsert"],
            capture_output=True, text=True
        )
        if result.returncode != 0:
            print(f"  ERROR: {result.stderr[:300]}", file=sys.stderr)
            return False
        return True
    finally:
        os.unlink(fname)


def main():
    print("Loading all scenarios...")
    r = run_cmd([MEMORY_BIN, "graph", "objects", "list", "--type", "Scenario", "--limit", "600", "--output", "json"])
    scenarios = json.loads(r.stdout)["items"]
    scenario_by_id = {s["entity_id"]: s for s in scenarios}
    scenario_by_key = {s.get("key", ""): s for s in scenarios}
    print(f"  {len(scenarios)} scenarios")

    print("Loading all ScenarioSteps...")
    r = run_cmd([MEMORY_BIN, "graph", "objects", "list", "--type", "ScenarioStep", "--limit", "2000", "--output", "json"])
    steps = json.loads(r.stdout)["items"]
    print(f"  {len(steps)} steps")

    print("Loading all Actions...")
    r = run_cmd([MEMORY_BIN, "graph", "objects", "list", "--type", "Action", "--limit", "2000", "--output", "json"])
    actions = json.loads(r.stdout)["items"]
    action_by_key = {a.get("key", ""): a for a in actions}
    print(f"  {len(actions)} actions")

    print("Loading existing has_step relationships...")
    r = run_cmd([MEMORY_BIN, "graph", "relationships", "list", "--type", "has_step", "--limit", "2000", "--output", "json"])
    has_step_rels = json.loads(r.stdout)["items"]
    enriched_scenario_ids = {rel["src_id"] for rel in has_step_rels}
    print(f"  {len(enriched_scenario_ids)} scenarios already have steps wired")

    print("Loading existing occurs_in relationships...")
    r = run_cmd([MEMORY_BIN, "graph", "relationships", "list", "--type", "occurs_in", "--limit", "2000", "--output", "json"])
    occurs_in_rels = json.loads(r.stdout)["items"]
    steps_with_context = {rel["src_id"] for rel in occurs_in_rels}
    print(f"  {len(steps_with_context)} steps already have occurs_in")

    print("Loading existing performed_by relationships...")
    r = run_cmd([MEMORY_BIN, "graph", "relationships", "list", "--type", "performed_by", "--limit", "2000", "--output", "json"])
    performed_by_rels = json.loads(r.stdout)["items"]
    steps_with_actor = {rel["src_id"] for rel in performed_by_rels}
    print(f"  {len(steps_with_actor)} steps already have performed_by")

    # Build step lookup by key pattern: ss-<scenario-key>-<N>
    step_by_key = {s.get("key", ""): s for s in steps}

    # Find scenarios that have steps by key but missing has_step wiring
    missing_wire = []
    for scenario in scenarios:
        sid = scenario["entity_id"]
        skey = scenario.get("key", "")
        if sid in enriched_scenario_ids:
            continue
        # Check if steps exist for this scenario
        step1_key = f"ss-{skey}-1"
        if step1_key in step_by_key:
            missing_wire.append(scenario)

    print(f"\nScenarios with steps but missing has_step wiring: {len(missing_wire)}")

    # Build relationships to create
    all_rels = []
    for scenario in missing_wire:
        sid = scenario["entity_id"]
        skey = scenario.get("key", "")
        context_id = get_context_for_key(skey)
        actor_id = get_actor_for_key(skey)
        action_key = f"act-{skey}"
        action = action_by_key.get(action_key)

        for order in [1, 2, 3]:
            step_key = f"ss-{skey}-{order}"
            step = step_by_key.get(step_key)
            if not step:
                continue
            step_id = step["entity_id"]

            # has_step: scenario -> step
            all_rels.append({"type": "has_step", "src_id": sid, "dst_id": step_id})

            # occurs_in: step -> context
            if step_id not in steps_with_context:
                all_rels.append({"type": "occurs_in", "src_id": step_id, "dst_id": context_id})

            # performed_by: step -> actor
            if step_id not in steps_with_actor:
                all_rels.append({"type": "performed_by", "src_id": step_id, "dst_id": actor_id})

            # has_action: step 2 -> action
            if order == 2 and action:
                all_rels.append({"type": "has_action", "src_id": step_id, "dst_id": action["entity_id"]})

    print(f"Relationships to create: {len(all_rels)}")

    # Batch in groups of 50 (remote server has timeout issues with large batches)
    BATCH_SIZE = 50
    total = 0
    for i in range(0, len(all_rels), BATCH_SIZE):
        batch = all_rels[i:i+BATCH_SIZE]
        print(f"  Batch {i//BATCH_SIZE+1}: {len(batch)} relationships...")
        if run_batch_rels(batch):
            total += len(batch)
            print(f"    ✓")
        else:
            print(f"    ✗")

    print(f"\nDone. Created {total} relationships.")


if __name__ == "__main__":
    main()
