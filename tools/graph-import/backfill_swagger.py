#!/usr/bin/env python3
"""
Backfill APIEndpoint objects from swagger.json:
  - summary, tags, description, auth_required, parameters, responses
Create APIContract object and wire all endpoints via grouped_in.
"""
import json, subprocess, os, sys, tempfile, re

MEMORY_BIN = os.path.expanduser("~/.memory/bin/memory")
SWAGGER_PATH = "/root/emergent.memory/apps/server/docs/swagger/swagger.json"

def run(args, input_data=None):
    r = subprocess.run([MEMORY_BIN]+args, capture_output=True, text=True, input=input_data)
    if r.returncode != 0:
        print(f"  ERROR: {r.stderr.strip()[:300]}", file=sys.stderr)
        return None
    return r.stdout

def load_objects(obj_type, limit=500):
    out = run(["graph", "objects", "list", "--type", obj_type, "--limit", str(limit), "--output", "json"])
    return json.loads(out).get("items", []) if out else []

def load_relationships(rel_type, limit=2000):
    out = run(["graph", "relationships", "list", "--type", rel_type, "--limit", str(limit), "--output", "json"])
    if not out: return []
    d = json.loads(out)
    return d.get("items", d) if isinstance(d, dict) else d

print("Loading swagger.json...")
with open(SWAGGER_PATH) as f:
    swagger = json.load(f)

info = swagger.get("info", {})
paths = swagger.get("paths", {})

# Build flat list of (method, path, spec) from swagger
swagger_ops = []
for path, methods in paths.items():
    for method, spec in methods.items():
        if method in ("get","post","put","patch","delete","head","options"):
            swagger_ops.append((method.upper(), path, spec))

print(f"  {len(swagger_ops)} swagger operations")

print("\nLoading graph APIEndpoints...")
endpoints = load_objects("APIEndpoint")
print(f"  {len(endpoints)} endpoints in graph")

# Normalize path for matching: {id} → :id
def norm_path(p):
    return re.sub(r'\{([^}]+)\}', r':\1', p)

# Build swagger lookup: (METHOD, normalized_path) → spec
swagger_lookup = {}
for method, path, spec in swagger_ops:
    swagger_lookup[(method, norm_path(path))] = spec

# Match graph endpoints to swagger ops
updates = []
unmatched = []
for ep in endpoints:
    props = ep.get("properties", {})
    method = props.get("method", "").upper()
    path = props.get("path", "")
    key = (method, path)
    spec = swagger_lookup.get(key)
    if spec:
        new_props = dict(props)
        new_props["summary"] = spec.get("summary", "")
        new_props["description"] = spec.get("description", "")
        new_props["auth_required"] = bool(spec.get("security"))
        new_props["tags"] = spec.get("tags", [])
        new_props["parameters"] = json.dumps(spec.get("parameters", []))
        new_props["responses"] = json.dumps(spec.get("responses", {}))
        updates.append({"id": ep["id"], "key": ep.get("key",""), "properties": new_props})
    else:
        unmatched.append((method, path, ep.get("key","")))

print(f"\nMatched: {len(updates)}/{len(endpoints)}")
if unmatched:
    print(f"Unmatched ({len(unmatched)}):")
    for m, p, k in unmatched[:10]:
        print(f"  {m} {p}  [{k}]")

# Batch update endpoints
print(f"\nUpdating {len(updates)} endpoints...")
BATCH = 50
updated = 0
for i in range(0, len(updates), BATCH):
    batch = updates[i:i+BATCH]
    payload = [{"id": u["id"], "key": u["key"], "properties": u["properties"]} for u in batch]
    print(f"  Batch {i//BATCH+1}: {len(batch)}...", end=" ")
    with tempfile.NamedTemporaryFile(mode="w", suffix=".json", delete=False) as f:
        json.dump(payload, f); fname = f.name
    try:
        out = run(["graph", "objects", "update-batch", "--file", fname])
        if out: print("✓"); updated += len(batch)
        else: print("✗")
    finally:
        os.unlink(fname)

print(f"\nUpdated {updated} endpoints.")

# Create APIContract object
print("\nCreating APIContract object...")
contract_payload = [{
    "type": "APIContract",
    "key": "contract-memory-api-v1",
    "properties": {
        "name": "Memory API",
        "format": "openapi",
        "version": info.get("version", ""),
        "file_path": "apps/server/docs/swagger/swagger.json",
        "base_url": "https://memory.emergent-company.ai",
        "description": info.get("description", "")
    }
}]
with tempfile.NamedTemporaryFile(mode="w", suffix=".json", delete=False) as f:
    json.dump(contract_payload, f); fname = f.name
try:
    out = run(["graph", "objects", "create-batch", "--file", fname])
    if out:
        print(f"  ✓ {out.strip()}")
        # Extract contract ID
        contract_id = out.strip().split()[0]
    else:
        print("  ✗ Failed to create contract"); sys.exit(1)
finally:
    os.unlink(fname)

# Wire all endpoints → contract via grouped_in
print(f"\nWiring {len(updates)} endpoints → contract via grouped_in...")
rels = [{"type": "grouped_in", "from": u["id"], "to": contract_id} for u in updates]
for i in range(0, len(rels), BATCH):
    batch = rels[i:i+BATCH]
    print(f"  Batch {i//BATCH+1}: {len(batch)}...", end=" ")
    with tempfile.NamedTemporaryFile(mode="w", suffix=".json", delete=False) as f:
        json.dump(batch, f); fname = f.name
    try:
        out = run(["graph", "relationships", "create-batch", "--upsert", "--file", fname])
        if out: print("✓")
        else: print("✗")
    finally:
        os.unlink(fname)

print("\nDone.")
