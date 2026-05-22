#!/usr/bin/env python3
"""
POC: directly exercise classify-document + finalize-discovery MCP tools
without going through the agent. Lets us see exact server errors.

Usage:
  python3 bench/domain-test/poc_finalize_debug.py

Set SERVER / TOKEN / ORG_ID at top if needed.
"""

import requests, json, time, sys
from pathlib import Path

SERVER  = "https://memory.emergent-company.ai"
TOKEN   = "emt_90e466b66031ef242148336a85152d30f78ba3e723fb81dc7ebed0fefc9156de"
ORG_ID  = "256508f5-6cbf-46bb-8c29-d8f839dd4ba8"
DEEPSEEK_KEY = "sk-bfa5b8465aad4a1e907474714936b0ff"

FIXTURES = Path(__file__).parent / "fixtures"

# ── helpers ──────────────────────────────────────────────────────────────────

def jprint(label, r):
    print(f"  [{r.status_code}] {label}: {r.text[:300]}")

def post(url, **kw):
    return requests.post(url, **kw)

def get(url, **kw):
    return requests.get(url, **kw)

# ── setup ────────────────────────────────────────────────────────────────────

print("=== POC: FinalizeDiscovery debug ===\n")

# 1. create project
r = post(f"{SERVER}/api/projects",
    headers={"Authorization": f"Bearer {TOKEN}", "Content-Type": "application/json"},
    json={"name": f"poc-finalize-debug-{int(time.time())}", "orgId": ORG_ID})
assert r.status_code in (200, 201), f"create project failed: {r.text}"
proj = r.json()
pid = proj["id"]
org_id = proj["orgId"]
print(f"Project: {pid}")

# 2. project-scoped token
r = post(f"{SERVER}/api/projects/{pid}/tokens",
    headers={"Authorization": f"Bearer {TOKEN}", "Content-Type": "application/json"},
    json={"name": "poc-debug-token",
          "scopes": ["agents:read", "agents:write", "data:read", "data:write", "schema:read", "schema:write"]})
assert r.status_code in (200, 201), f"token failed: {r.text}"
ptok = r.json()["token"]
ph = {"Authorization": f"Bearer {ptok}", "x-project-id": pid}

# 3. configure DeepSeek provider
r = requests.put(f"{SERVER}/api/v1/projects/{pid}/providers/openai-compatible",
    headers={**ph, "Content-Type": "application/json"},
    json={"apiKey": DEEPSEEK_KEY,
          "baseUrl": "https://api.deepseek.com/v1",
          "generativeModel": "deepseek-chat"})
jprint("configure provider", r)

def cleanup():
    r = requests.delete(f"{SERVER}/api/projects/{pid}",
        headers={"Authorization": f"Bearer {TOKEN}"})
    print(f"\nCleanup: {r.status_code} {r.text[:80]}")

def upload(name):
    path = FIXTURES / name
    with open(path, "rb") as f:
        r = post(f"{SERVER}/api/documents/upload",
            headers={"Authorization": f"Bearer {ptok}", "x-project-id": pid},
            files={"file": (name, f, "text/plain")})
    assert r.status_code in (200, 201), f"upload failed: {r.text}"
    body = r.json()
    doc_id = (body.get("document") or body).get("id") or body.get("existingDocumentId")
    print(f"  Uploaded {name} → doc_id={doc_id}")
    return doc_id

_mcp_session = {}  # keyed by token

def mcp_init(token, project_id, org_id_val):
    """Call MCP initialize to establish session, return session token."""
    r = post(f"{SERVER}/api/mcp/rpc",
        headers={"Authorization": f"Bearer {token}", "x-project-id": project_id,
                 "Content-Type": "application/json"},
        json={"jsonrpc": "2.0", "id": 1, "method": "initialize",
              "params": {"protocolVersion": "2024-11-05",
                         "clientInfo": {"name": "poc-debug", "version": "1.0"},
                         "project_id": project_id, "org_id": org_id_val}})
    print(f"    initialize → {r.status_code}: {r.text[:200]}")
    return token  # token IS the session key

def call_mcp_tool(tool_name, args, token, project_id, org_id_val):
    """Call MCP tool via JSON-RPC 2.0."""
    if token not in _mcp_session:
        mcp_init(token, project_id, org_id_val)
        _mcp_session[token] = True
    r = post(f"{SERVER}/api/mcp/rpc",
        headers={"Authorization": f"Bearer {token}", "x-project-id": project_id,
                 "Content-Type": "application/json"},
        json={"jsonrpc": "2.0", "id": 2, "method": "tools/call",
              "params": {"name": tool_name, "arguments": args}})
    return r

# ── test cases ────────────────────────────────────────────────────────────────

test_cases = [
    ("ai-chat-1.txt",         "AI Assistant Session", "AI Chat (first)"),
    ("supplier-agreement.txt","Supplier Agreement",   "Supplier Agreement (first)"),
]

for fixture, suggested_pack, label in test_cases:
    print(f"\n{'='*60}")
    print(f"TEST: {label}")
    print(f"{'='*60}")

    doc_id = upload(fixture)
    time.sleep(10)  # wait for extraction worker

    # Check doc state after extraction
    r = get(f"{SERVER}/api/documents/{doc_id}", headers=ph)
    doc = r.json()
    print(f"  After extraction: stage={doc.get('stage')} domain={doc.get('domainName')} signals={json.dumps(doc.get('classificationSignals'))[:100]}")

    # Try calling classify-document directly
    print(f"\n  Calling classify-document ...")
    args_classify = {
        "document_id": doc_id,
        "project_id": pid,
        "org_id": org_id,
    }
    r = call_mcp_tool("classify-document", args_classify, ptok, pid, org_id)
    print(f"    classify-document → {r.status_code}: {r.text[:400]}")

    # Check doc signals after classify
    r = get(f"{SERVER}/api/documents/{doc_id}", headers=ph)
    doc = r.json()
    signals = doc.get("classificationSignals") or {}
    print(f"  After classify: domain={doc.get('domainName')} signals={json.dumps(signals)[:150]}")
    suggested = signals.get("suggestedPackName", "<none>")
    print(f"  suggestedPackName in signals: {suggested!r}")

    # Try finalize-discovery with forbidden name first (simulating agent bug)
    print(f"\n  Calling finalize-discovery with pack_name='new_domain' (forbidden) ...")
    args_bad = {
        "document_id": doc_id,
        "project_id": pid,
        "org_id": org_id,
        "mode": "create",
        "pack_name": "new_domain",
        "included_types": [
            {"type_name": "TestType", "description": "Test", "properties": {"name": {"type": "string"}}}
        ],
        "included_relationships": [],
    }
    r = call_mcp_tool("finalize-discovery", args_bad, ptok, pid, org_id)
    print(f"    forbidden name → {r.status_code}: {r.text[:500]}")

    # Check schema count
    r = get(f"{SERVER}/api/schemas", headers=ph)
    schemas_data = r.json() if r.status_code == 200 else []
    schemas = schemas_data if isinstance(schemas_data, list) else schemas_data.get("schemas", schemas_data.get("items", []))
    print(f"  Schemas after forbidden call: {len(schemas)} — {[s.get('name') for s in schemas]}")

    # Try finalize-discovery with good name
    print(f"\n  Calling finalize-discovery with pack_name={suggested_pack!r} ...")
    args_good = {**args_bad, "pack_name": suggested_pack}
    r = call_mcp_tool("finalize-discovery", args_good, ptok, pid, org_id)
    print(f"    good name → {r.status_code}: {r.text[:500]}")

    # Final schema count
    r = get(f"{SERVER}/api/schemas", headers=ph)
    schemas_data = r.json() if r.status_code == 200 else []
    schemas = schemas_data if isinstance(schemas_data, list) else schemas_data.get("schemas", schemas_data.get("items", []))
    print(f"  Schemas after good call: {len(schemas)} — {[s.get('name') for s in schemas]}")

    # Check doc domain_name
    r = get(f"{SERVER}/api/documents/{doc_id}", headers=ph)
    doc = r.json()
    print(f"  Doc after finalize: domain={doc.get('domainName')} confidence={doc.get('domainConfidence')}")

cleanup()
print("\nDone.")
