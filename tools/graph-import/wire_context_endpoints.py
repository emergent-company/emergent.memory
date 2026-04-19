#!/usr/bin/env python3
"""
Wire CLI contexts → APIEndpoints via context_calls_endpoint relationships.
Maps each CLI context to the APIEndpoints it invokes based on domain/name matching.
"""
import json, subprocess, sys, tempfile, os

MEMORY_BIN = os.path.expanduser("~/.memory/bin/memory")

def run(args, input_data=None):
    cmd = [MEMORY_BIN] + args
    r = subprocess.run(cmd, capture_output=True, text=True, input=input_data)
    if r.returncode != 0:
        print(f"  ERROR: {r.stderr.strip()[:200]}", file=sys.stderr)
        return None
    return r.stdout

def load_objects(obj_type, limit=500):
    out = run(["graph", "objects", "list", "--type", obj_type, "--limit", str(limit), "--output", "json"])
    if not out:
        return []
    return json.loads(out).get("items", [])

def load_relationships(rel_type, limit=2000):
    out = run(["graph", "relationships", "list", "--type", rel_type, "--limit", str(limit), "--output", "json"])
    if not out:
        return []
    d = json.loads(out)
    return d.get("items", d) if isinstance(d, dict) else d

print("Loading CLI contexts...")
contexts = load_objects("Context")
cli_contexts = [c for c in contexts if c.get("properties", {}).get("context_type") == "cli"]
print(f"  {len(cli_contexts)} CLI contexts")

print("Loading APIEndpoints...")
endpoints = load_objects("APIEndpoint", limit=500)
print(f"  {len(endpoints)} APIEndpoints")

print("Loading existing context_calls_endpoint relationships...")
existing_rels = load_relationships("context_calls_endpoint")
existing_pairs = set()
for r in existing_rels:
    existing_pairs.add((r.get("src_id") or r.get("from"), r.get("dst_id") or r.get("to")))
print(f"  {len(existing_pairs)} already wired")

# Build endpoint lookup by domain prefix from key
# endpoint key format: ep-<domain>-<verb>-<resource>
ep_by_domain = {}
for ep in endpoints:
    key = ep.get("key", "")
    parts = key.split("-")
    if len(parts) >= 2:
        domain = parts[1]
        ep_by_domain.setdefault(domain, []).append(ep)

# Map context key → domain
# context key format: ctx-cli-<domain>-<action> or cli-<domain>
def get_domain(ctx):
    key = ctx.get("key", "")
    name = ctx.get("properties", {}).get("name", "")
    # Extract domain from key: ctx-cli-agents → agents, cli-agents → agents
    parts = key.replace("ctx-cli-", "").replace("cli-", "").split("-")
    return parts[0] if parts else ""

# Build relationships to create
to_create = []
for ctx in cli_contexts:
    domain = get_domain(ctx)
    ctx_id = ctx["id"]
    matching_eps = ep_by_domain.get(domain, [])
    for ep in matching_eps:
        ep_id = ep["id"]
        if (ctx_id, ep_id) not in existing_pairs:
            to_create.append({"type": "context_calls_endpoint", "from": ctx_id, "to": ep_id})

print(f"\nRelationships to create: {len(to_create)}")
if not to_create:
    print("Nothing to do.")
    sys.exit(0)

# Show coverage
print("\nContext → endpoint count by domain:")
domain_counts = {}
for r in to_create:
    # find ctx domain
    ctx = next((c for c in cli_contexts if c["id"] == r["from"]), None)
    if ctx:
        d = get_domain(ctx)
        domain_counts[d] = domain_counts.get(d, 0) + 1
for d, cnt in sorted(domain_counts.items()):
    print(f"  {d}: {cnt}")

# Batch create in chunks of 50
BATCH = 50
created = 0
for i in range(0, len(to_create), BATCH):
    batch = to_create[i:i+BATCH]
    print(f"\n  Batch {i//BATCH+1}: {len(batch)} relationships...")
    with tempfile.NamedTemporaryFile(mode="w", suffix=".json", delete=False) as f:
        json.dump(batch, f)
        fname = f.name
    try:
        out = run(["graph", "relationships", "create-batch", "--upsert", "--file", fname])
        if out:
            print(f"    ✓")
            created += len(batch)
        else:
            print(f"    ✗ failed")
    finally:
        os.unlink(fname)

print(f"\nDone. Created {created} context_calls_endpoint relationships.")
