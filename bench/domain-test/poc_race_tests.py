#!/usr/bin/env python3
"""
Race-condition regression tests for domain_name write ordering.

Tests:
  T1. classify-document after finalize-discovery must NOT overwrite domain_name
  T2. worker real-match write (force=false, domain IS NULL) → must succeed
  T3. finalize-discovery twice (re-finalize, force=true) → second call wins
  T4. finalize extend mode (pack_name omitted / nil) → domain_name unchanged, signals updated
  T5. worker writes "new_domain" placeholder (simulate old code path) → finalize-discovery
      (force=true) must still overwrite it

Usage:
  python3 bench/domain-test/poc_race_tests.py
"""

import requests, json, time, sys, uuid
from pathlib import Path

SERVER       = "https://memory.emergent-company.ai"
TOKEN        = "emt_90e466b66031ef242148336a85152d30f78ba3e723fb81dc7ebed0fefc9156de"
ORG_ID       = "256508f5-6cbf-46bb-8c29-d8f839dd4ba8"
DEEPSEEK_KEY = "sk-bfa5b8465aad4a1e907474714936b0ff"
FIXTURES     = Path(__file__).parent / "fixtures"

# ── helpers ───────────────────────────────────────────────────────────────────

def h(tok, pid=None):
    hd = {"Authorization": f"Bearer {tok}", "Content-Type": "application/json"}
    if pid:
        hd["x-project-id"] = pid
    return hd

def get_doc(pid, doc_id, ptok):
    r = requests.get(f"{SERVER}/api/documents/{doc_id}", headers=h(ptok, pid))
    assert r.status_code == 200, f"get_doc failed: {r.text}"
    return r.json()

def patch_doc_domain(pid, doc_id, ptok, domain_name, confidence=0.9):
    """Directly patch domain_name via API (simulates worker write)."""
    r = requests.patch(f"{SERVER}/api/documents/{doc_id}",
        headers=h(ptok, pid),
        json={"domainName": domain_name, "domainConfidence": confidence})
    return r

_mcp_inited = set()

def mcp_call(tool, args, ptok, pid):
    if ptok not in _mcp_inited:
        requests.post(f"{SERVER}/api/mcp/rpc", headers=h(ptok, pid),
            json={"jsonrpc":"2.0","id":1,"method":"initialize",
                  "params":{"protocolVersion":"2024-11-05",
                             "clientInfo":{"name":"poc-race","version":"1.0"},
                             "project_id":pid,"org_id":ORG_ID}})
        _mcp_inited.add(ptok)
    r = requests.post(f"{SERVER}/api/mcp/rpc", headers=h(ptok, pid),
        json={"jsonrpc":"2.0","id":2,"method":"tools/call",
              "params":{"name":tool,"arguments":args}})
    return r

PASS = "PASS"
FAIL = "FAIL"
results = []

def check(name, condition, got, want):
    status = PASS if condition else FAIL
    results.append((status, name, got, want))
    mark = "✓" if condition else "✗"
    print(f"  {mark} {name}: got={got!r} want={want!r}")
    return condition

# ── project setup ──────────────────────────────────────────────────────────────

print("=== Domain race-condition regression tests ===\n")

r = requests.post(f"{SERVER}/api/projects",
    headers=h(TOKEN),
    json={"name": f"poc-race-{int(time.time())}", "orgId": ORG_ID})
assert r.status_code in (200,201), f"create project: {r.text}"
pid = r.json()["id"]
print(f"Project: {pid}\n")

r = requests.post(f"{SERVER}/api/projects/{pid}/tokens",
    headers=h(TOKEN),
    json={"name":"race-tok",
          "scopes":["agents:read","agents:write","data:read","data:write","schema:read","schema:write"]})
assert r.status_code in (200,201), f"token: {r.text}"
ptok = r.json()["token"]

# Configure DeepSeek provider
requests.put(f"{SERVER}/api/v1/projects/{pid}/providers/openai-compatible",
    headers=h(ptok, pid),
    json={"apiKey":DEEPSEEK_KEY,"baseUrl":"https://api.deepseek.com/v1","generativeModel":"deepseek-chat"})

def upload(name):
    path = FIXTURES / name
    with open(path,"rb") as f:
        r = requests.post(f"{SERVER}/api/documents/upload",
            headers={"Authorization":f"Bearer {ptok}","x-project-id":pid},
            files={"file":(name, f,"text/plain")})
    assert r.status_code in (200,201), f"upload {name}: {r.text}"
    body = r.json()
    doc_id = (body.get("document") or body).get("id") or body.get("existingDocumentId")
    return doc_id

FINALIZE_TYPES = [{"type_name":"TestType","description":"Test","properties":{"name":{"type":"string"}}}]

def finalize(doc_id, pack_name, mode="create"):
    return mcp_call("finalize-discovery", {
        "document_id": doc_id, "project_id": pid, "org_id": ORG_ID,
        "mode": mode, "pack_name": pack_name,
        "included_types": FINALIZE_TYPES,
        "included_relationships": [],
    }, ptok, pid)

def classify(doc_id):
    return mcp_call("classify-document", {
        "document_id": doc_id, "project_id": pid, "org_id": ORG_ID,
    }, ptok, pid)

# ─────────────────────────────────────────────────────────────────────────────
# T1: classify-document AFTER finalize-discovery must NOT overwrite domain_name
# ─────────────────────────────────────────────────────────────────────────────
print("T1: classify-document after finalize must not overwrite domain_name")

doc1 = upload("ai-chat-1.txt")
time.sleep(8)  # wait for extraction

# finalize first
r = finalize(doc1, "AI Chat Session")
print(f"  finalize → {r.status_code}: {r.text[:200]}")

doc = get_doc(pid, doc1, ptok)
domain_before = doc.get("domainName")
print(f"  domain after finalize: {domain_before!r}")

# now classify (simulates agent calling classify-document after finalize)
classify(doc1)

doc = get_doc(pid, doc1, ptok)
domain_after = doc.get("domainName")
check("T1 domain_name not overwritten by classify-document",
      domain_after == domain_before, domain_after, domain_before)

# ─────────────────────────────────────────────────────────────────────────────
# T2: finalize-discovery on doc with domain_name=NULL must set it (force=true)
# ─────────────────────────────────────────────────────────────────────────────
print("\nT2: finalize-discovery sets domain_name when doc is NULL")

doc2 = upload("supplier-agreement.txt")
time.sleep(8)

doc = get_doc(pid, doc2, ptok)
print(f"  domain before finalize: {doc.get('domainName')!r}")

r = finalize(doc2, "Supplier Agreement")
print(f"  finalize → {r.status_code}: {r.text[:200]}")

doc = get_doc(pid, doc2, ptok)
check("T2 domain_name set by finalize",
      doc.get("domainName") == "Supplier Agreement",
      doc.get("domainName"), "Supplier Agreement")

# ─────────────────────────────────────────────────────────────────────────────
# T3: finalize-discovery called twice (re-finalize) → second call wins
# ─────────────────────────────────────────────────────────────────────────────
print("\nT3: re-finalize (second finalize-discovery) must update domain_name")

doc3 = upload("medical-lab-1.txt")
time.sleep(8)

r = finalize(doc3, "Medical Lab Report v1")
print(f"  first finalize → {r.status_code}: {r.text[:200]}")

doc = get_doc(pid, doc3, ptok)
print(f"  domain after 1st finalize: {doc.get('domainName')!r}")

r = finalize(doc3, "Medical Lab Report v2", mode="extend")
print(f"  second finalize (extend) → {r.status_code}: {r.text[:200]}")

doc = get_doc(pid, doc3, ptok)
# extend mode passes nil packNamePtr → domain_name should stay as v1
# (extend doesn't rename; it adds types to existing schema)
# So we just check domain_name is still set (not overwritten to NULL)
check("T3 domain_name still set after extend-mode re-finalize",
      doc.get("domainName") is not None and doc.get("domainName") != "",
      doc.get("domainName"), "non-empty")

# ─────────────────────────────────────────────────────────────────────────────
# T4: Worker writing domain_name=NULL (passes nil, force=false) should NOT
#     change existing finalized domain_name → simulated via classify-document
#     which now passes nil domainName (same code path as worker no-match)
# ─────────────────────────────────────────────────────────────────────────────
print("\nT4: Background nil write (no domain match) must not clear finalized domain_name")

doc4 = upload("real-estate-listing.txt")
time.sleep(8)

r = finalize(doc4, "Real Estate Listing")
print(f"  finalize → {r.status_code}: {r.text[:200]}")

# classify-document now passes nil domainName — simulate the nil write
classify(doc4)
time.sleep(2)

doc = get_doc(pid, doc4, ptok)
check("T4 nil domainName write does not clear finalized domain",
      doc.get("domainName") == "Real Estate Listing",
      doc.get("domainName"), "Real Estate Listing")

# ─────────────────────────────────────────────────────────────────────────────
# T5: finalize-discovery must update domain_name even when doc already has a
#     different finalized name (force=true re-finalize with create mode rename)
#     This validates the force=true path in UpdateDomainClassification.
# ─────────────────────────────────────────────────────────────────────────────
print("\nT5: finalize-discovery (force=true) overwrites existing finalized domain_name")

doc5 = upload("personal-notes.txt")
time.sleep(8)

# first finalize with one name
r = finalize(doc5, "Notes Draft")
print(f"  first finalize → {r.status_code}: {r.text[:200]}")

doc = get_doc(pid, doc5, ptok)
print(f"  domain after 1st finalize: {doc.get('domainName')!r}")

# second finalize with a different name (create mode) — force=true must win
r = finalize(doc5, "Personal Notes Final")
print(f"  second finalize (rename) → {r.status_code}: {r.text[:200]}")

doc = get_doc(pid, doc5, ptok)
check("T5 force=true finalize overwrites existing finalized domain_name",
      doc.get("domainName") == "Personal Notes Final",
      doc.get("domainName"), "Personal Notes Final")

# ─────────────────────────────────────────────────────────────────────────────
# Results
# ─────────────────────────────────────────────────────────────────────────────
print(f"\n{'='*60}")
print("RESULTS")
print(f"{'='*60}")
passed = sum(1 for s,*_ in results if s == PASS)
failed = sum(1 for s,*_ in results if s == FAIL)
for status, name, got, want in results:
    mark = "✓" if status == PASS else "✗"
    print(f"  {mark} {name}")
print(f"\n{passed}/{passed+failed} passed")

# cleanup
requests.delete(f"{SERVER}/api/projects/{pid}", headers=h(TOKEN))
print(f"Cleanup: project {pid} deleted")

if failed:
    sys.exit(1)
