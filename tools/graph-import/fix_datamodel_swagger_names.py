#!/usr/bin/env python3
"""
Patch DataModel objects: add swagger_name property = language_type
(which already stores the full domain_X.TypeName swagger key).
Also ensures name field matches the short name for display.
"""
import json, subprocess, os, sys, tempfile

MEMORY_BIN = os.path.expanduser("~/.memory/bin/memory")

def run(args, input_data=None):
    r = subprocess.run([MEMORY_BIN] + args, capture_output=True, text=True, input=input_data)
    if r.returncode != 0:
        print(f"  ERROR: {r.stderr.strip()[:300]}", file=sys.stderr)
        return None
    return r.stdout

def load_objects(obj_type, limit=1000):
    out = run(["graph", "objects", "list", "--type", obj_type, "--limit", str(limit), "--output", "json"])
    return json.loads(out).get("items", []) if out else []

print("Loading DataModels...")
datamodels = load_objects("DataModel", limit=1000)
print(f"  {len(datamodels)} datamodels")

# Build update batch: set swagger_name = language_type (full swagger key)
# Only update DataModels that have language_type (swagger-sourced ones)
updates = []
for dm in datamodels:
    props = dm.get("properties", {})
    lang_type = props.get("language_type", "")
    if not lang_type:
        continue
    if props.get("swagger_name") == lang_type:
        continue  # already set
    # swagger_name is the full key used in swagger definitions block
    entry = {
        "id": dm["id"],
        "properties": {
            **props,
            "swagger_name": lang_type
        }
    }
    if dm.get("key"):
        entry["key"] = dm["key"]
    updates.append(entry)

print(f"  Updating {len(updates)} DataModels with swagger_name...")

BATCH = 50
for i in range(0, len(updates), BATCH):
    batch = updates[i:i+BATCH]
    with tempfile.NamedTemporaryFile(mode="w", suffix=".json", delete=False) as f:
        json.dump(batch, f)
        fname = f.name
    out = run(["graph", "objects", "update-batch", "--file", fname])
    n = i // BATCH + 1
    total = (len(updates) + BATCH - 1) // BATCH
    if out is not None:
        print(f"  Batch {n}/{total}: {len(batch)}... ✓")
    else:
        print(f"  Batch {n}/{total}: {len(batch)}... ✗")
    os.unlink(fname)

print("Done.")
