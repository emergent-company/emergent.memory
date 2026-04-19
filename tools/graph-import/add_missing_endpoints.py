#!/usr/bin/env python3
"""
Add 47 missing APIEndpoint objects from swagger.json to the graph,
then wire them to the APIContract via grouped_in.
"""
import json, subprocess, os, sys, tempfile, re

MEMORY_BIN = os.path.expanduser("~/.memory/bin/memory")
SWAGGER_PATH = "/root/emergent.memory/apps/server/docs/swagger/swagger.json"
CONTRACT_KEY = "contract-memory-api-v1"

def run(args, input_data=None):
    r = subprocess.run([MEMORY_BIN] + args, capture_output=True, text=True, input=input_data)
    if r.returncode != 0:
        print(f"  ERROR: {r.stderr.strip()[:300]}", file=sys.stderr)
        return None
    return r.stdout

def load_objects(obj_type, limit=1000):
    out = run(["graph", "objects", "list", "--type", obj_type, "--limit", str(limit), "--output", "json"])
    return json.loads(out).get("items", []) if out else []

def swagger_to_graph_path(p):
    return re.sub(r"\{([^}]+)\}", r":\1", p)

def graph_to_swagger_path(p):
    return re.sub(r":([a-zA-Z_][a-zA-Z0-9_]*)", r"{\1}", p)

print("Loading swagger.json...")
with open(SWAGGER_PATH) as f:
    swagger = json.load(f)

print("Loading existing APIEndpoints...")
existing_eps = load_objects("APIEndpoint", limit=500)
existing_paths = set()
for ep in existing_eps:
    props = ep.get("properties", {})
    path = props.get("path", "")
    method = props.get("method", "").upper()
    if path and method:
        existing_paths.add((graph_to_swagger_path(path), method))

print(f"  {len(existing_eps)} existing endpoints, {len(existing_paths)} path+method combos")

print("Loading APIContract...")
contracts = load_objects("APIContract")
# APIContract has no key field — match by type (only one exists)
contract = contracts[0] if contracts else None
if not contract:
    print("ERROR: APIContract not found!", file=sys.stderr)
    sys.exit(1)
contract_id = contract["id"]
print(f"  Contract ID: {contract_id}")

# Build missing endpoints
missing = []
for swagger_path, methods in swagger["paths"].items():
    for method, op in methods.items():
        if (swagger_path, method.upper()) not in existing_paths:
            graph_path = swagger_to_graph_path(swagger_path)
            tags = op.get("tags", [])
            tag = tags[0] if tags else "misc"
            # Build a key
            slug = graph_path.replace("/", "-").replace(":", "").strip("-")
            slug = re.sub(r"-+", "-", slug)
            key = f"ep-missing-{method.lower()}-{slug}"[:80]

            params = op.get("parameters", [])
            responses = op.get("responses", {})
            auth = bool(op.get("security"))

            missing.append({
                "key": key,
                "swagger_path": swagger_path,
                "graph_path": graph_path,
                "method": method.upper(),
                "summary": op.get("summary", ""),
                "description": op.get("description", ""),
                "tags": tags,
                "auth_required": auth,
                "parameters": json.dumps(params),
                "responses": json.dumps(responses),
            })

print(f"\nMissing endpoints to create: {len(missing)}")

# Create batch
objects = []
for ep in missing:
    objects.append({
        "type": "APIEndpoint",
        "key": ep["key"],
        "properties": {
            "path": ep["graph_path"],
            "method": ep["method"],
            "summary": ep["summary"],
            "description": ep["description"],
            "tags": ep["tags"],
            "auth_required": ep["auth_required"],
            "parameters": ep["parameters"],
            "responses": ep["responses"],
        }
    })

BATCH = 50
created_keys = []
for i in range(0, len(objects), BATCH):
    batch = objects[i:i+BATCH]
    with tempfile.NamedTemporaryFile(mode="w", suffix=".json", delete=False) as f:
        json.dump(batch, f)
        fname = f.name
    out = run(["graph", "objects", "create-batch", "--file", fname])
    n = i // BATCH + 1
    total = (len(objects) + BATCH - 1) // BATCH
    if out is not None:
        result = json.loads(out) if out.strip().startswith("[") or out.strip().startswith("{") else {}
        items = result if isinstance(result, list) else result.get("items", result.get("created", []))
        for item in (items if isinstance(items, list) else []):
            if isinstance(item, dict) and item.get("id"):
                created_keys.append((item["id"], item.get("key", "")))
        print(f"  Batch {n}/{total}: {len(batch)}... ✓")
    else:
        print(f"  Batch {n}/{total}: {len(batch)}... ✗")
    os.unlink(fname)

# Re-load to get IDs of newly created endpoints
print("\nReloading endpoints to get IDs for wiring...")
all_eps = load_objects("APIEndpoint", limit=600)
missing_keys = {ep["key"] for ep in missing}
new_ep_ids = [ep["id"] for ep in all_eps if ep.get("key") in missing_keys]
print(f"  Found {len(new_ep_ids)} new endpoint IDs")

# Wire to contract via grouped_in
print(f"Wiring {len(new_ep_ids)} endpoints → contract via grouped_in...")
rels = [{"type": "grouped_in", "from": eid, "to": contract_id} for eid in new_ep_ids]

for i in range(0, len(rels), BATCH):
    batch = rels[i:i+BATCH]
    with tempfile.NamedTemporaryFile(mode="w", suffix=".json", delete=False) as f:
        json.dump(batch, f)
        fname = f.name
    out = run(["graph", "relationships", "create-batch", "--upsert", "--file", fname])
    n = i // BATCH + 1
    total = (len(rels) + BATCH - 1) // BATCH
    if out is not None:
        print(f"  Batch {n}/{total}: {len(batch)}... ✓")
    else:
        print(f"  Batch {n}/{total}: {len(batch)}... ✗")
    os.unlink(fname)

print("\nDone.")
