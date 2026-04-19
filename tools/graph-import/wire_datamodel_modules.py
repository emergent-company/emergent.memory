#!/usr/bin/env python3
"""Wire DataModels → Modules via stores_model relationship."""
import json, subprocess, sys, tempfile, os

MEMORY_BIN = os.path.expanduser("~/.memory/bin/memory")

def run(args):
    r = subprocess.run([MEMORY_BIN] + args, capture_output=True, text=True)
    if r.returncode != 0:
        print(f"  ERROR: {r.stderr.strip()[:200]}", file=sys.stderr)
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

print("Loading DataModels..."); datamodels = load_objects("DataModel"); print(f"  {len(datamodels)}")
print("Loading Modules..."); modules = load_objects("Module"); print(f"  {len(modules)}")
print("Loading existing stores_model...")
existing = load_relationships("stores_model")
existing_pairs = set((r.get("src_id") or r.get("from"), r.get("dst_id") or r.get("to")) for r in existing)
print(f"  {len(existing_pairs)} already wired")

# Module key: mod-<domain>
mod_by_domain = {m["key"][len("mod-"):]: m for m in modules if m.get("key","").startswith("mod-") and not m["key"].startswith("mod-cli-") and not m["key"].startswith("mod-pkg-")}

to_create = []
unmatched = []
for dm in datamodels:
    key = dm.get("key", "")
    parts = key.split("-")
    # table-<domain>-... → domain = parts[1]
    if len(parts) >= 2 and parts[0] == "table":
        domain = parts[1]
        mod = mod_by_domain.get(domain)
        if mod:
            pair = (mod["id"], dm["id"])
            if pair not in existing_pairs:
                to_create.append({"type": "stores_model", "from": mod["id"], "to": dm["id"]})
        else:
            unmatched.append((key, domain))

if unmatched:
    print(f"\nUnmatched ({len(unmatched)}):")
    for k, d in unmatched: print(f"  {k} → {d}")

print(f"\nTo create: {len(to_create)}")
if not to_create: sys.exit(0)

created = 0
for i in range(0, len(to_create), 50):
    batch = to_create[i:i+50]
    print(f"  Batch {i//50+1}: {len(batch)}...")
    with tempfile.NamedTemporaryFile(mode="w", suffix=".json", delete=False) as f:
        json.dump(batch, f); fname = f.name
    try:
        out = run(["graph", "relationships", "create-batch", "--upsert", "--file", fname])
        if out: print("    ✓"); created += len(batch)
        else: print("    ✗")
    finally:
        os.unlink(fname)

print(f"\nDone. Created {created} stores_model relationships.")
