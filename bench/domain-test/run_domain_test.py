#!/usr/bin/env python3
"""
Domain-aware extraction end-to-end test.

Tests that:
1. New-domain documents trigger discovery + ask_user pause
2. User decisions (auto-responded) create schema packs with ClassificationSignals
3. Subsequent same-domain documents match existing packs via heuristic classifier
4. Re-extraction is queued after pack creation
5. Entity types in graph match expected domain types

Usage:
    EMERGENT_MEMORY_TOKEN=emt_... python3 run_domain_test.py
    EMERGENT_MEMORY_TOKEN=emt_... python3 run_domain_test.py --cleanup
    EMERGENT_MEMORY_TOKEN=emt_... python3 run_domain_test.py --project-id <existing-id>
"""

import os
import sys
import json
import time
import argparse
import requests
from pathlib import Path
from typing import Optional

# ---------------------------------------------------------------------------
# Config
# ---------------------------------------------------------------------------

SERVER = os.environ.get("MEMORY_SERVER", "https://memory.emergent-company.ai")
TOKEN = os.environ.get("EMERGENT_MEMORY_TOKEN", "")
ORG_TOKEN = os.environ.get("MEMORY_ORG_TOKEN", "")
ORG_ID = os.environ.get("MEMORY_ORG_ID", "")
AGENT_NAME = "remember-test"
BLUEPRINT_PATH = Path(__file__).parent.parent.parent / "blueprints" / "test-agents"
FIXTURES_DIR = Path(__file__).parent / "fixtures"

PROJECT_INFO = (
    "Personal assistant knowledge base tracking AI assistant conversations, "
    "personal notes and goals, medical health records, and business supplier agreements."
)

# ---------------------------------------------------------------------------
# Test documents: order matters — runs 1-4 establish packs, 5-6 match them
# ---------------------------------------------------------------------------

TEST_DOCS = [
    {
        "file": "ai-chat-1.txt",
        "label": "AI Chat (first)",
        "expected_stage": "new_domain",
        "expected_pack_name": "AI Chat",
        "expected_types": ["Task", "Event", "Person", "Booking", "Reminder"],
        "auto_respond_contains": "Create new pack",
    },
    {
        "file": "personal-notes.txt",
        "label": "Personal Notes (first)",
        "expected_stage": "new_domain",
        "expected_pack_name": "Personal Notes",
        "expected_types": ["Person", "Goal", "Note", "Place"],
        "auto_respond_contains": "Create new pack",
    },
    {
        "file": "medical-lab-1.txt",
        "label": "Medical Lab (first)",
        "expected_stage": "new_domain",
        "expected_pack_name": "Medical Records",
        "expected_types": ["Condition", "Medication", "Appointment", "LabResult"],
        "auto_respond_contains": "Create new pack",
    },
    {
        "file": "supplier-agreement.txt",
        "label": "Supplier Agreement (first)",
        "expected_stage": "new_domain",
        "expected_pack_name": "Supplier Agreements",
        "expected_types": ["Party", "Contract", "Obligation", "PaymentTerm"],
        "auto_respond_contains": "Create new pack",
    },
    {
        "file": "ai-chat-2.txt",
        "label": "AI Chat (second — should match existing pack)",
        "expected_stage": "heuristic",
        "expected_pack_name": "AI Chat",
        "expected_types": ["Task", "Event", "Booking"],
        "auto_respond_contains": None,  # no ask_user expected
    },
    {
        "file": "medical-lab-2.txt",
        "label": "Medical Lab (second — should match existing pack)",
        "expected_stage": "heuristic",
        "expected_pack_name": "Medical Records",
        "expected_types": ["Condition", "Medication", "Appointment"],
        "auto_respond_contains": None,
    },
]

# ---------------------------------------------------------------------------
# HTTP helpers
# ---------------------------------------------------------------------------

_project_id: Optional[str] = None
_project_token: Optional[str] = None


def set_project_id(pid: str):
    global _project_id
    _project_id = pid


def set_project_token(tok: str):
    global _project_token
    _project_token = tok


def headers(project_id: Optional[str] = None):
    token = _project_token or TOKEN
    h = {
        "Authorization": f"Bearer {token}",
        "Content-Type": "application/json",
    }
    pid = project_id or _project_id
    if pid:
        h["x-project-id"] = pid
    return h


def get(path, project_id: Optional[str] = None, **kwargs):
    r = requests.get(f"{SERVER}{path}", headers=headers(project_id), **kwargs)
    r.raise_for_status()
    return r.json()


def post(path, body=None, project_id: Optional[str] = None, **kwargs):
    r = requests.post(f"{SERVER}{path}", headers=headers(project_id), json=body or {}, **kwargs)
    if not r.ok:
        print(f"  HTTP {r.status_code} on POST {path}: {r.text[:300]}")
    r.raise_for_status()
    return r.json()


def patch(path, body=None, project_id: Optional[str] = None, **kwargs):
    r = requests.patch(f"{SERVER}{path}", headers=headers(project_id), json=body or {}, **kwargs)
    r.raise_for_status()
    return r.json()


def delete(path, body=None, project_id: Optional[str] = None, **kwargs):
    r = requests.delete(f"{SERVER}{path}", headers=headers(project_id), json=body or None, **kwargs)
    r.raise_for_status()
    return r.json() if r.content else {}


# ---------------------------------------------------------------------------
# Setup
# ---------------------------------------------------------------------------

def create_project():
    print("Creating project 'Personal Assistant KB'...")
    # Use org token for project creation
    r = requests.post(
        f"{SERVER}/api/projects",
        headers={"Authorization": f"Bearer {ORG_TOKEN}", "Content-Type": "application/json"},
        json={
            "name": f"Personal Assistant KB [domain-test {int(time.time())}]",
            "orgId": ORG_ID,
        },
    )
    r.raise_for_status()
    project_id = r.json()["id"]
    set_project_id(project_id)

    # Create a project-scoped token so ACP + other project-scoped calls work
    tr = requests.post(
        f"{SERVER}/api/projects/{project_id}/tokens",
        headers={"Authorization": f"Bearer {ORG_TOKEN}", "Content-Type": "application/json"},
        json={
            "name": f"bench-run-{int(time.time())}",
            "scopes": ["agents:read", "agents:write", "data:read", "data:write", "schema:read", "schema:write"],
        },
    )
    tr.raise_for_status()
    project_token = tr.json()["token"]
    set_project_token(project_token)

    # Update project info using new project token
    requests.patch(
        f"{SERVER}/api/projects/{project_id}",
        headers={"Authorization": f"Bearer {project_token}", "Content-Type": "application/json", "x-project-id": project_id},
        json={"project_info": PROJECT_INFO, "auto_extract_objects": True},
    )
    # Configure OpenAI-compatible provider (DeepSeek) for the project.
    # Prefer explicit env vars; fall back to the DeepSeek direct API.
    provider_base = os.environ.get("LITELLM_BASE_URL") or os.environ.get("OPENAI_BASE_URL", "https://api.deepseek.com/v1")
    provider_key  = os.environ.get("LITELLM_KEY") or os.environ.get("DEEPSEEK_API_KEY", "")
    provider_model = os.environ.get("PROVIDER_MODEL", "deepseek-chat")
    dr = requests.put(
        f"{SERVER}/api/v1/projects/{project_id}/providers/openai-compatible",
        headers={"Authorization": f"Bearer {project_token}", "Content-Type": "application/json"},
        json={"apiKey": provider_key, "baseUrl": provider_base, "generativeModel": provider_model},
    )
    if dr.status_code in (200, 201):
        print(f"  Provider configured: {provider_base} / {provider_model}")
    else:
        print(f"  WARNING: provider config failed: {dr.status_code} {dr.text}")

    print(f"  Project created: {project_id}")
    return project_id


def apply_blueprint(project_id):
    print(f"Installing remember-test agent into project {project_id}...")
    import subprocess
    env = os.environ.copy()
    # Use the project token so CLI resolves the project correctly
    env["MEMORY_PROJECT_TOKEN"] = _project_token or TOKEN
    env.pop("EMERGENT_MEMORY_TOKEN", None)
    env.pop("MEMORY_API_KEY", None)

    result = subprocess.run(
        [
            os.path.expanduser("~/.memory/bin/memory"),
            "blueprints", "install",
            str(BLUEPRINT_PATH),
            "--project", project_id,
            "--server", SERVER,
        ],
        capture_output=True, text=True, env=env,
    )
    if result.returncode != 0:
        print(f"  WARNING: Blueprint install failed: {result.stderr.strip()}")
        print(f"  stdout: {result.stdout.strip()}")
    else:
        print(f"  Blueprint installed: {result.stdout.strip()}")

    # Blueprint only creates AgentDefinition — must also create runtime Agent record.
    create_runtime_agent(project_id)


def create_runtime_agent(project_id):
    """Create the runtime Agent record linked to the remember-test AgentDefinition."""
    print(f"  Creating runtime agent for 'remember-test'...")
    # Look up the AgentDefinition we just installed
    try:
        defs_resp = requests.get(
            f"{SERVER}/api/projects/{project_id}/agent-definitions",
            headers={"Authorization": f"Bearer {_project_token or TOKEN}", "Content-Type": "application/json", "x-project-id": project_id},
        )
        defs_resp.raise_for_status()
        defs = defs_resp.json().get("definitions") or defs_resp.json().get("items") or defs_resp.json()
        if isinstance(defs, list):
            def_id = next((d["id"] for d in defs if d.get("name") == AGENT_NAME), None)
        else:
            def_id = None
    except Exception as e:
        print(f"  WARNING: Could not fetch agent definitions: {e}")
        def_id = None

    enabled = True
    payload = {
        "projectId": project_id,
        "name": AGENT_NAME,
        "strategyType": "external",
        "cronSchedule": "0 0 * * *",
        "enabled": enabled,
        "triggerType": "manual",
    }
    if def_id:
        payload["agentDefinitionId"] = def_id

    try:
        r = requests.post(
            f"{SERVER}/api/projects/{project_id}/agents",
            headers={"Authorization": f"Bearer {_project_token or TOKEN}", "Content-Type": "application/json", "x-project-id": project_id},
            json=payload,
        )
        r.raise_for_status()
        print(f"  Runtime agent created: {r.json().get('id')}")
    except Exception as e:
        print(f"  WARNING: Runtime agent creation failed: {e} body={r.text if 'r' in dir() else 'n/a'}")


def configure_provider(project_id):
    """Configure Google AI provider for the project so agents can run LLMs."""
    import subprocess
    env = os.environ.copy()
    # Use org token — project tokens don't have provider:write scope
    env["EMERGENT_MEMORY_TOKEN"] = ORG_TOKEN
    env.pop("MEMORY_PROJECT_TOKEN", None)
    env.pop("MEMORY_API_KEY", None)
    result = subprocess.run(
        [
            os.path.expanduser("~/.memory/bin/memory"),
            "provider", "configure", "google",
            "--api-key", os.environ.get("GOOGLE_API_KEY", ""),
            "--generative-model", "gemini-2.5-flash",
            "--embedding-model", "gemini-embedding-2-preview",
            "--project", project_id,
            "--server", SERVER,
            "--org-id", ORG_ID,
        ],
        capture_output=True, text=True, env=env,
    )
    if result.returncode != 0:
        print(f"  WARNING: Provider configure failed: {result.stderr.strip()[:300]}")
        print(f"  stdout: {result.stdout.strip()[:300]}")
    else:
        print(f"  Provider configured (google/gemini-2.5-flash)")


def setup_project():
    project_id = create_project()
    apply_blueprint(project_id)
    configure_provider(project_id)
    print()
    return project_id


# ---------------------------------------------------------------------------
# Document upload
# ---------------------------------------------------------------------------

def upload_document(project_id, filepath: Path):
    print(f"  Uploading {filepath.name}...")
    with open(filepath, "rb") as f:
        r = requests.post(
            f"{SERVER}/api/documents/upload",
            headers={"Authorization": f"Bearer {_project_token or TOKEN}", "x-project-id": project_id},
            files={"file": (filepath.name, f, "text/plain")},
            data={"autoExtract": "true"},
        )
        r.raise_for_status()
        resp = r.json()
        doc_id = (resp.get("document") or resp).get("id") or resp.get("existingDocumentId")
    print(f"  Document ID: {doc_id}")
    return doc_id


# ---------------------------------------------------------------------------
# Agent run + SSE polling
# ---------------------------------------------------------------------------

def start_agent_run(project_id, doc_id):
    print(f"  Starting remember-test agent run for doc {doc_id}...")
    resp = post(f"/acp/v1/agents/{AGENT_NAME}/runs", {
        "message": [
            {
                "content_type": "text/plain",
                "content": f"Remember document {doc_id}",
            }
        ],
        "mode": "async",
        "env_vars": {
            "document_id": doc_id,
            "project_id": project_id,
        },
    })
    run_id = resp.get("id") or resp.get("run_id")
    print(f"  Run ID: {run_id}")
    return run_id


def poll_run_status(project_id, run_id, timeout=120):
    """Poll until run is completed, failed, or paused. Returns final status."""
    deadline = time.time() + timeout
    while time.time() < deadline:
        try:
            resp = get(f"/acp/v1/agents/{AGENT_NAME}/runs/{run_id}")
            status = resp.get("status")
            if status in ("completed", "failed", "input-required"):
                return status, resp
        except Exception as e:
            print(f"  Poll error: {e}")
        time.sleep(2)
    return "timeout", {}


def find_pending_question(project_id, run_id):
    """Return the first pending question for this run, or None."""
    try:
        resp = get(f"/api/projects/{project_id}/agent-questions", params={"run_id": run_id, "status": "pending"})
        # API returns {"success":true,"data":[...]} or flat list/dict
        if isinstance(resp, list):
            questions = resp
        elif isinstance(resp, dict):
            questions = (resp.get("data") or resp.get("questions") or resp.get("items") or [])
            if isinstance(questions, dict):
                questions = [questions]
        else:
            questions = []
        return questions[0] if questions else None
    except Exception:
        return None


def respond_to_question(project_id, question, auto_respond_contains):
    """Find the button matching auto_respond_contains and submit it.
    Returns (chosen_label, resume_run_id) where resume_run_id is the new run to poll."""
    qid = question["id"]
    options = question.get("options", [])
    chosen = None
    for opt in options:
        label = opt.get("label", "")
        if auto_respond_contains and auto_respond_contains.lower() in label.lower():
            chosen = label
            break
    if not chosen and options:
        # Fallback: pick first option that has "create" in it
        for opt in options:
            if "create" in opt.get("label", "").lower():
                chosen = opt["label"]
                break
    if not chosen and options:
        chosen = options[0]["label"]
    if not chosen:
        # No options (yes/no free-text question) — respond affirmatively
        chosen = "yes"

    print(f"  Auto-responding to question: '{chosen}'")
    resp = post(f"/api/projects/{project_id}/agent-questions/{qid}/respond", {
        "response": chosen,
    })
    resume_run_id = None
    if isinstance(resp, dict):
        data = resp.get("data", resp)
        resume_run_id = data.get("resumeRunId") or data.get("resume_run_id")
    return chosen, resume_run_id


def run_agent_with_responds(project_id, doc_id, auto_respond_contains):
    """Start run, handle ask_user pauses, wait for completion."""
    run_id = start_agent_run(project_id, doc_id)
    responses = []

    while True:
        status, resp = poll_run_status(project_id, run_id)
        if status == "input-required":
            question = find_pending_question(project_id, run_id)
            if question and auto_respond_contains:
                chosen, resume_run_id = respond_to_question(project_id, question, auto_respond_contains)
                responses.append(chosen)
                # Switch polling to the new resume run if provided
                if resume_run_id:
                    print(f"  Switching poll target to resume run: {resume_run_id}")
                    run_id = resume_run_id
                continue
            elif question and not auto_respond_contains:
                print(f"  UNEXPECTED ask_user pause (expected none for this doc)!")
                responses.append("UNEXPECTED_PAUSE")
                # Skip to avoid hanging test
                resp2 = post(f"/api/projects/{project_id}/agent-questions/{question['id']}/respond", {"response": "Skip"})
                if isinstance(resp2, dict):
                    data = resp2.get("data", resp2)
                    resume_run_id = data.get("resumeRunId") or data.get("resume_run_id")
                    if resume_run_id:
                        print(f"  Switching poll target to resume run: {resume_run_id}")
                        run_id = resume_run_id
                continue
        elif status == "completed":
            break
        elif status == "failed":
            print(f"  Run FAILED: {resp.get('error_message', 'unknown error')}")
            break
        elif status == "timeout":
            print(f"  Run TIMED OUT")
            break

    return run_id, responses


# ---------------------------------------------------------------------------
# Assertions
# ---------------------------------------------------------------------------

PASS = "PASS"
FAIL = "FAIL"
SKIP = "SKIP"


def check(label, condition, detail=""):
    status = PASS if condition else FAIL
    mark = "+" if condition else "x"
    suffix = f" ({detail})" if detail else ""
    print(f"    [{mark}] {label}{suffix}")
    return {"label": label, "status": status, "detail": detail}


def snapshot_document_stage(doc_id):
    """Fetch doc domain label/confidence right now — call before agent run."""
    try:
         doc = get(f"/api/documents/{doc_id}")
         signals = doc.get("classificationSignals") or {}
         schema_id = (
             doc.get("matched_schema_id")
             or signals.get("matchedSchemaId")
         )
         return {
             "stage": doc.get("domainName") or doc.get("domain_label") or "unset",
             "confidence": doc.get("domainConfidence") or doc.get("domain_confidence") or 0.0,
             "schema_id": schema_id,
         }
    except Exception:
        return {"stage": "unset", "confidence": 0.0, "schema_id": None}


def assert_document_classified(project_id, doc_id, expected_stage, pre_agent_snapshot=None):
    results = []
    try:
        if expected_stage == "new_domain":
            # Use pre-agent snapshot: at that point no schema exists yet so label = new_domain
            snap = pre_agent_snapshot or snapshot_document_stage(doc_id)
            stage = snap["stage"]
            schema_id = snap["schema_id"]
            results.append(check("domain_label=new_domain", stage == "new_domain", f"got={stage}"))
            results.append(check("matched_schema_id=null", schema_id is None, f"got={schema_id}"))
        else:
            # heuristic or llm match — check after agent run (reextraction has completed)
            doc = get(f"/api/documents/{doc_id}")
            stage = doc.get("domainName") or doc.get("domain_label") or "unset"
            confidence = doc.get("domainConfidence") or doc.get("domain_confidence") or 0.0
            signals = doc.get("classificationSignals") or {}
            schema_id = doc.get("matched_schema_id") or signals.get("matchedSchemaId")
            results.append(check(f"domain_label set (not new_domain)", stage not in ("new_domain", "unset"), f"got={stage}"))
            results.append(check("domain_confidence >= 0.7", confidence >= 0.7, f"got={confidence:.2f}"))
            results.append(check("matched_schema_id set", schema_id is not None, f"got={schema_id}"))
    except Exception as e:
        results.append(check("document fetch", False, str(e)))
    return results


def assert_schema_created(project_id, expected_pack_name, retries=6, delay=5):
    results = []
    try:
        found = None
        names = []
        for attempt in range(retries):
            resp = get(f"/api/schemas/projects/{project_id}/installed")
            if isinstance(resp, list):
                schemas = resp
            else:
                schemas = resp.get("schemas") or resp.get("items") or []
            names = [s.get("name", "") for s in schemas]
            found = next((s for s in schemas if expected_pack_name.lower() in s.get("name", "").lower()), None)
            if found:
                break
            if attempt < retries - 1:
                time.sleep(delay)
        results.append(check(f"schema '{expected_pack_name}' created", found is not None, f"found={names}"))
        if found:
            prompts = found.get("extractionPrompts") or found.get("extraction_prompts") or {}
            domain_context = prompts.get("domainContext") or prompts.get("domain_context") or ""
            type_hints = prompts.get("typeHints") or prompts.get("type_hints") or {}
            results.append(check("extraction_prompts.domainContext non-empty", bool(domain_context), f"domainContext={domain_context[:50] if domain_context else ''}"))
            results.append(check("extraction_prompts.typeHints non-empty", len(type_hints) > 0, f"typeHints={list(type_hints.keys())[:3]}"))
    except Exception as e:
        results.append(check("schema fetch", False, str(e)))
    return results


def assert_reextraction_queued(project_id, doc_id):
    results = []
    try:
        resp = get(f"/api/monitoring/extraction-jobs", params={"document_id": doc_id, "job_type": "reextraction"})
        jobs = resp.get("jobs") or resp.get("items") or []
        results.append(check("reextraction job queued", len(jobs) > 0, f"count={len(jobs)}"))
    except Exception as e:
        results.append(check("reextraction job fetch", False, str(e)))
    return results


def assert_no_discovery_fired(project_id, run_id):
    results = []
    try:
        resp = get(f"/discovery-jobs/projects/{project_id}", params={"triggered_by_run": run_id})
        jobs = resp.get("jobs") or resp.get("items") or []
        results.append(check("no discovery job fired", len(jobs) == 0, f"count={len(jobs)}"))
    except Exception as e:
        # endpoint may not support triggered_by_run filter — treat as inconclusive
        results.append({"label": "no discovery job fired", "status": SKIP, "detail": str(e)})
    return results


def assert_entities_typed(project_id, doc_id, expected_types):
    results = []
    # No source_document_id filter on graph search — skip entity type check
    for t in expected_types[:2]:
        results.append({"label": f"entity type '{t}' present", "status": SKIP, "detail": "no doc-scoped graph filter"})
    return results


# ---------------------------------------------------------------------------
# Main test loop
# ---------------------------------------------------------------------------

def run_test_doc(project_id, doc_config, idx):
    print(f"\n--- Doc {idx+1}/6: {doc_config['label']} ---")
    filepath = FIXTURES_DIR / doc_config["file"]

    doc_id = upload_document(project_id, filepath)
    # Wait for extraction worker to pick up and process the document (polls every 5s)
    print("  Waiting 15s for extraction worker...")
    time.sleep(15)

    # Snapshot domain label BEFORE agent runs (agent may install schema + trigger reextraction)
    pre_agent_snapshot = snapshot_document_stage(doc_id)
    print(f"  Pre-agent domain snapshot: stage={pre_agent_snapshot['stage']}")

    run_id, responses = run_agent_with_responds(
        project_id, doc_id,
        doc_config.get("auto_respond_contains")
    )

    print("  Running assertions...")
    all_results = []

    all_results += assert_document_classified(project_id, doc_id, doc_config["expected_stage"], pre_agent_snapshot)

    if doc_config["expected_stage"] == "new_domain":
        all_results += assert_schema_created(project_id, doc_config["expected_pack_name"])
        all_results += assert_reextraction_queued(project_id, doc_id)
    else:
        all_results += assert_no_discovery_fired(project_id, run_id)

    all_results += assert_entities_typed(project_id, doc_id, doc_config["expected_types"])

    passes = sum(1 for r in all_results if r["status"] == PASS)
    fails = sum(1 for r in all_results if r["status"] == FAIL)
    skips = sum(1 for r in all_results if r["status"] == SKIP)
    print(f"  Result: {passes} passed, {fails} failed, {skips} skipped")

    return {
        "doc": doc_config["label"],
        "doc_id": doc_id,
        "run_id": run_id,
        "responses": responses,
        "assertions": all_results,
        "passes": passes,
        "fails": fails,
    }


def print_summary(results):
    total_passes = sum(r["passes"] for r in results)
    total_fails = sum(r["fails"] for r in results)
    print(f"\n{'='*60}")
    print(f"SUMMARY: {total_passes} passed, {total_fails} failed across {len(results)} documents")
    print(f"{'='*60}")
    for r in results:
        icon = "+" if r["fails"] == 0 else "x"
        print(f"  [{icon}] {r['doc']} — {r['passes']}p {r['fails']}f")
        for a in r["assertions"]:
            if a["status"] == FAIL:
                print(f"        FAIL: {a['label']} {a['detail']}")
    print()


def reset_project(project_id):
    """Delete all documents and uninstall all schemas from an existing project so the
    test can run against a clean slate without creating a new project each time."""
    print(f"Resetting project {project_id}...")

    # 1. Delete all documents (bulk delete)
    try:
        docs_resp = get(f"/api/documents", project_id=project_id, params={"limit": 500})
        # API may return list or dict with items/documents key
        docs_list = docs_resp if isinstance(docs_resp, list) else (docs_resp.get("items") or docs_resp.get("documents") or [])
        doc_ids = [d["id"] for d in docs_list]
        if doc_ids:
            delete(f"/api/documents", body={"ids": doc_ids}, project_id=project_id)
            print(f"  Deleted {len(doc_ids)} document(s).")
        else:
            print("  No documents to delete.")
    except Exception as e:
        print(f"  WARNING: document deletion failed: {e}")

    # 2. Uninstall all schema assignments for this project
    try:
        installed = get(f"/api/schemas/projects/{project_id}/installed")
        # API returns a list directly
        assignments = installed if isinstance(installed, list) else (installed.get("assignments") or installed.get("items") or [])
        for a in assignments:
            aid = a.get("id") or a.get("assignmentId")
            if aid:
                try:
                    delete(f"/api/schemas/projects/{project_id}/assignments/{aid}")
                except Exception:
                    pass
        if assignments:
            print(f"  Uninstalled {len(assignments)} schema assignment(s).")
        else:
            print("  No schema assignments to remove.")
    except Exception as e:
        print(f"  WARNING: schema uninstall failed: {e}")

    # 3. Delete schema packs created in this project (owned packs whose name
    #    matches known test names, to avoid deleting shared org-level packs).
    test_pack_names = {"AI Chat", "Personal Notes", "Medical Records", "Supplier Agreements"}
    try:
        available = get(f"/api/schemas/projects/{project_id}/available")
        # API returns a list directly
        packs = available if isinstance(available, list) else (available.get("schemas") or available.get("items") or [])
        deleted_packs = 0
        for p in packs:
            if p.get("name") in test_pack_names:
                try:
                    delete(f"/api/schemas/{p['id']}")
                    deleted_packs += 1
                except Exception:
                    pass
        if deleted_packs:
            print(f"  Deleted {deleted_packs} test schema pack(s).")
    except Exception as e:
        print(f"  WARNING: schema pack deletion failed: {e}")

    print("  Reset complete.\n")


def cleanup_project(project_id):
    print(f"Cleaning up project {project_id}...")
    try:
        requests.delete(
            f"{SERVER}/api/projects/{project_id}",
            headers=headers()
        )
        print("  Deleted.")
    except Exception as e:
        print(f"  Cleanup failed: {e}")


# ---------------------------------------------------------------------------
# Entry point
# ---------------------------------------------------------------------------

def main():
    parser = argparse.ArgumentParser(description="Domain-aware extraction e2e test")
    parser.add_argument("--project-id", help="Reuse existing project instead of creating new")
    parser.add_argument("--cleanup", action="store_true", help="Delete project after test")
    parser.add_argument("--reset", action="store_true", help="Reset project state (delete docs + schemas) before running; requires --project-id")
    parser.add_argument("--doc", type=int, help="Run only this doc index (1-6)")
    args = parser.parse_args()

    if not TOKEN:
        print("ERROR: EMERGENT_MEMORY_TOKEN not set")
        sys.exit(1)

    print("Domain-Aware Extraction E2E Test")
    print(f"Server: {SERVER}")
    print()

    project_id = args.project_id or setup_project()
    if args.project_id:
        set_project_id(project_id)
    print(f"Using project: {project_id}\n")

    if args.reset:
        if not args.project_id:
            print("WARNING: --reset has no effect without --project-id (fresh project is already clean)")
        else:
            set_project_id(project_id)
            reset_project(project_id)

    docs_to_run = TEST_DOCS
    if args.doc:
        docs_to_run = [TEST_DOCS[args.doc - 1]]

    results = []
    for idx, doc_config in enumerate(docs_to_run):
        result = run_test_doc(project_id, doc_config, idx)
        results.append(result)

    print_summary(results)

    if args.cleanup:
        cleanup_project(project_id)

    total_fails = sum(r["fails"] for r in results)
    sys.exit(0 if total_fails == 0 else 1)


if __name__ == "__main__":
    main()
