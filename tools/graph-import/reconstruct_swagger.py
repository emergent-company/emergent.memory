#!/usr/bin/env python3
"""
Reconstruct swagger.json from the Memory knowledge graph.
Queries APIContract, APIEndpoints, DataModels and their relationships.
"""
import json, subprocess, os, sys

MEMORY_BIN = os.path.expanduser("~/.memory/bin/memory")
OUTPUT_PATH = "/tmp/swagger_reconstructed.json"

def run(args):
    r = subprocess.run([MEMORY_BIN] + args, capture_output=True, text=True)
    if r.returncode != 0:
        print(f"ERROR: {r.stderr.strip()[:300]}", file=sys.stderr)
        return None
    return r.stdout

def load_objects(obj_type, limit=1000):
    out = run(["graph", "objects", "list", "--type", obj_type, "--limit", str(limit), "--output", "json"])
    return json.loads(out).get("items", []) if out else []

def load_relationships(rel_type, limit=5000):
    out = run(["graph", "relationships", "list", "--type", rel_type, "--limit", str(limit), "--output", "json"])
    if not out: return []
    d = json.loads(out)
    return d.get("items", d) if isinstance(d, dict) else d

# ── 1. Load APIContract ──────────────────────────────────────────────────────
print("Loading APIContract...")
contracts = load_objects("APIContract")
contract = next((c for c in contracts if c.get("key") == "contract-memory-api-v1"), contracts[0] if contracts else None)
if not contract:
    print("No APIContract found!", file=sys.stderr)
    sys.exit(1)
cp = contract.get("properties", {})
print(f"  Contract: {cp.get('title')} v{cp.get('version')}")

# ── 2. Load all APIEndpoints ─────────────────────────────────────────────────
print("Loading APIEndpoints...")
endpoints = load_objects("APIEndpoint", limit=500)
print(f"  {len(endpoints)} endpoints")

# ── 3. Load all DataModels ───────────────────────────────────────────────────
print("Loading DataModels...")
datamodels = load_objects("DataModel", limit=1000)
print(f"  {len(datamodels)} datamodels")

# ── 4. Build paths block ─────────────────────────────────────────────────────
print("Building paths...")
paths = {}

for ep in endpoints:
    props = ep.get("properties", {})
    path = props.get("path", "")
    method = (props.get("method") or "get").lower()

    if not path:
        continue

    # Convert :param → {param}
    import re
    swagger_path = re.sub(r":([a-zA-Z_][a-zA-Z0-9_]*)", r"{\1}", path)

    op = {}

    # Auth
    if props.get("auth_required", True):
        op["security"] = [{"bearerAuth": []}]

    # Standard fields
    if props.get("description"):
        op["description"] = props["description"]
    if props.get("summary"):
        op["summary"] = props["summary"]
    if props.get("tags"):
        tags = props["tags"]
        if isinstance(tags, str):
            try:
                tags = json.loads(tags)
            except Exception:
                tags = [tags]
        op["tags"] = tags

    op["consumes"] = ["application/json"]
    op["produces"] = ["application/json"]

    # Parameters
    if props.get("parameters"):
        params = props["parameters"]
        if isinstance(params, str):
            try:
                params = json.loads(params)
            except Exception:
                params = []
        op["parameters"] = params

    # Responses
    if props.get("responses"):
        responses = props["responses"]
        if isinstance(responses, str):
            try:
                responses = json.loads(responses)
            except Exception:
                responses = {}
        op["responses"] = responses
    else:
        op["responses"] = {"200": {"description": "OK"}}

    if swagger_path not in paths:
        paths[swagger_path] = {}
    paths[swagger_path][method] = op

# ── 5. Build definitions block ───────────────────────────────────────────────
print("Building definitions...")
definitions = {}

for dm in datamodels:
    props = dm.get("properties", {})
    # Use swagger_name (full domain_X.TypeName) if available, else name
    name = props.get("swagger_name") or props.get("name") or dm.get("key", "").replace("dm-", "")
    if not name:
        continue

    schema = props.get("openapi_schema")
    if schema:
        if isinstance(schema, str):
            try:
                schema = json.loads(schema)
            except Exception:
                schema = {}
        definitions[name] = schema
    else:
        # Minimal fallback — skip graph-native DMs without openapi_schema
        pass

# ── 6. Assemble swagger doc ──────────────────────────────────────────────────
swagger = {
    "swagger": "2.0",
    "info": {
        "description": cp.get("description", ""),
        "title": cp.get("title", "Memory API"),
        "version": cp.get("version", ""),
        "contact": {
            "name": "Memory Team",
            "url": "https://emergent-company.ai",
            "email": "support@emergent-company.ai"
        },
        "license": {"name": "Proprietary"}
    },
    "host": cp.get("base_url", "localhost:5300").replace("https://", "").replace("http://", ""),
    "basePath": "/",
    "schemes": ["http", "https"],
    "paths": dict(sorted(paths.items())),
    "definitions": dict(sorted(definitions.items())),
    "securityDefinitions": {
        "bearerAuth": {
            "type": "apiKey",
            "name": "Authorization",
            "in": "header"
        }
    }
}

with open(OUTPUT_PATH, "w") as f:
    json.dump(swagger, f, indent=2)

print(f"\nWritten to {OUTPUT_PATH}")
print(f"  paths: {len(paths)}")
print(f"  definitions: {len(definitions)}")
