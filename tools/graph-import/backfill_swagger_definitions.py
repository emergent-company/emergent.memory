#!/usr/bin/env python3
"""
Backfill DataModel objects from swagger definitions:
  - openapi_schema (JSON blob of properties)
Also create new DataModel objects for swagger definitions not yet in graph.
Wire endpoint → DataModel uses_schema from $ref links.
"""
import json, subprocess, os, sys, tempfile, re

MEMORY_BIN = os.path.expanduser("~/.memory/bin/memory")
SWAGGER_PATH = "/root/emergent.memory/apps/server/docs/swagger/swagger.json"

def run(args):
    r = subprocess.run([MEMORY_BIN]+args, capture_output=True, text=True)
    if r.returncode != 0:
        print(f"  ERROR: {r.stderr.strip()[:300]}", file=sys.stderr)
        return None
    return r.stdout

def load_objects(obj_type, limit=500):
    out = run(["graph", "objects", "list", "--type", obj_type, "--limit", str(limit), "--output", "json"])
    return json.loads(out).get("items", []) if out else []

def load_relationships(rel_type, limit=5000):
    out = run(["graph", "relationships", "list", "--type", rel_type, "--limit", str(limit), "--output", "json"])
    if not out: return []
    d = json.loads(out)
    return d.get("items", d) if isinstance(d, dict) else d

print("Loading swagger.json...")
with open(SWAGGER_PATH) as f:
    swagger = json.load(f)

defs = swagger.get("definitions", {})
paths = swagger.get("paths", {})
print(f"  {len(defs)} swagger definitions")

print("\nLoading graph DataModels...")
datamodels = load_objects("DataModel")
print(f"  {len(datamodels)} DataModels in graph")

print("\nLoading graph APIEndpoints...")
endpoints = load_objects("APIEndpoint")
print(f"  {len(endpoints)} endpoints in graph")

# Build DataModel lookup by name
dm_by_name = {dm.get("properties",{}).get("name",""): dm for dm in datamodels}

# Normalize swagger def name → simple name (last segment after dot)
def simple_name(def_name):
    # e.g. "domain_agents.AgentDefinition" → "AgentDefinition"
    # "github_com_...domain_sandbox.AgentSandboxConfig" → "AgentSandboxConfig"
    return def_name.split(".")[-1]

def def_key(def_name):
    # make a graph key: dm-swagger-<slug>
    slug = re.sub(r'[^a-z0-9]+', '-', def_name.lower()).strip('-')
    return f"dm-swagger-{slug[:60]}"

# Match swagger defs to existing DataModels by name
updates = []  # existing DMs to update with openapi_schema
to_create = []  # new DMs from swagger defs not in graph

for def_name, def_spec in defs.items():
    name = simple_name(def_name)
    schema_blob = json.dumps(def_spec)
    existing = dm_by_name.get(name)
    if existing:
        new_props = dict(existing.get("properties", {}))
        new_props["openapi_schema"] = schema_blob
        updates.append({"id": existing["id"], "key": existing.get("key",""), "properties": new_props})
    else:
        to_create.append({
            "type": "DataModel",
            "key": def_key(def_name),
            "properties": {
                "name": name,
                "description": f"OpenAPI definition: {def_name}",
                "openapi_schema": schema_blob,
                "language_type": def_name,  # full qualified name
            }
        })

print(f"\nExisting DMs to update: {len(updates)}")
print(f"New DMs to create from swagger defs: {len(to_create)}")

BATCH = 50

# Update existing DataModels
if updates:
    print(f"\nUpdating {len(updates)} DataModels with openapi_schema...")
    for i in range(0, len(updates), BATCH):
        batch = updates[i:i+BATCH]
        print(f"  Batch {i//BATCH+1}: {len(batch)}...", end=" ")
        with tempfile.NamedTemporaryFile(mode="w", suffix=".json", delete=False) as f:
            json.dump(batch, f); fname = f.name
        try:
            out = run(["graph", "objects", "update-batch", "--file", fname])
            if out: print("✓")
            else: print("✗")
        finally:
            os.unlink(fname)

# Create new DataModels for swagger-only definitions
created_dms = {}  # def_name → id
if to_create:
    print(f"\nCreating {len(to_create)} new DataModel objects from swagger defs...")
    for i in range(0, len(to_create), BATCH):
        batch = to_create[i:i+BATCH]
        print(f"  Batch {i//BATCH+1}: {len(batch)}...", end=" ")
        with tempfile.NamedTemporaryFile(mode="w", suffix=".json", delete=False) as f:
            json.dump(batch, f); fname = f.name
        try:
            out = run(["graph", "objects", "create-batch", "--file", fname])
            if out:
                print("✓")
                # Parse created IDs from output lines: "<uuid>  DataModel  <name>"
                for line in out.strip().split("\n"):
                    parts = line.split()
                    if len(parts) >= 3 and len(parts[0]) == 36:
                        created_dms[parts[2]] = parts[0]  # name → id
            else:
                print("✗")
        finally:
            os.unlink(fname)

# Build full DataModel name→id map (existing + newly created)
dm_id_by_name = {dm.get("properties",{}).get("name",""): dm["id"] for dm in datamodels}
dm_id_by_name.update(created_dms)

# Wire endpoint → DataModel uses_schema from $ref links
print("\nBuilding uses_schema relationships from $ref links...")

# Normalize path for matching
def norm_path(p):
    return re.sub(r'\{([^}]+)\}', r':\1', p)

# Build endpoint lookup by (method, path)
ep_lookup = {}
for ep in endpoints:
    props = ep.get("properties", {})
    ep_lookup[(props.get("method","").upper(), props.get("path",""))] = ep["id"]

def extract_refs(obj):
    """Recursively extract all $ref values from a swagger spec object."""
    refs = set()
    if isinstance(obj, dict):
        if "$ref" in obj:
            refs.add(obj["$ref"])
        for v in obj.values():
            refs.update(extract_refs(v))
    elif isinstance(obj, list):
        for item in obj:
            refs.update(extract_refs(item))
    return refs

def ref_to_name(ref):
    # "#/definitions/domain_agents.AgentDefinition" → "AgentDefinition"
    def_name = ref.replace("#/definitions/", "")
    return simple_name(def_name)

uses_schema_rels = []
for path, methods in paths.items():
    for method, spec in methods.items():
        if method not in ("get","post","put","patch","delete","head","options"):
            continue
        ep_id = ep_lookup.get((method.upper(), norm_path(path)))
        if not ep_id:
            continue
        refs = extract_refs(spec.get("parameters", [])) | extract_refs(spec.get("responses", {}))
        for ref in refs:
            dm_name = ref_to_name(ref)
            dm_id = dm_id_by_name.get(dm_name)
            if dm_id:
                uses_schema_rels.append({"type": "uses_schema", "from": ep_id, "to": dm_id})

# Deduplicate
seen = set()
unique_rels = []
for r in uses_schema_rels:
    k = (r["from"], r["to"])
    if k not in seen:
        seen.add(k)
        unique_rels.append(r)

print(f"  {len(unique_rels)} unique uses_schema relationships")

if unique_rels:
    print(f"\nCreating uses_schema relationships...")
    for i in range(0, len(unique_rels), BATCH):
        batch = unique_rels[i:i+BATCH]
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
