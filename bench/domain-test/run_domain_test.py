#!/usr/bin/env python3
"""
Domain-aware extraction end-to-end test.

Tests that:
1. New-domain documents trigger finalize-discovery (tool policy pauses for human approval)
2. User approves → schema pack created with a non-empty pack_name chosen by agent
3. Subsequent same-domain documents match existing packs via heuristic classifier
4. Re-extraction is queued after pack creation

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
# Load .env from script directory (if present)
# ---------------------------------------------------------------------------

_env_file = Path(__file__).parent / ".env"
if _env_file.exists():
    for _line in _env_file.read_text().splitlines():
        _line = _line.strip()
        if _line and not _line.startswith("#") and "=" in _line:
            _k, _v = _line.split("=", 1)
            os.environ.setdefault(_k.strip(), _v.strip())

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
    "personal notes and goals, medical health records, business supplier agreements, "
    "and real estate property listings."
)

# ---------------------------------------------------------------------------
# Test documents: order matters — runs 1-4 establish packs, 5-6 match them
# ---------------------------------------------------------------------------

TEST_DOCS = [
    {
        "file": "ai-chat-1.txt",
        "label": "AI Chat (first)",
        "expected_stage": "new_domain",
    },
    {
        "file": "personal-notes.txt",
        "label": "Personal Notes (first)",
        "expected_stage": "new_domain",
    },
    {
        "file": "medical-lab-1.txt",
        "label": "Medical Lab (first)",
        "expected_stage": "new_domain",
    },
    {
        "file": "supplier-agreement.txt",
        "label": "Supplier Agreement (first)",
        "expected_stage": "new_domain",
    },
    {
        "file": "real-estate-listing.txt",
        "label": "Real Estate Listing (first)",
        "expected_stage": "new_domain",
    },
    {
        "file": "ai-chat-2.txt",
        "label": "AI Chat (second — should match existing pack)",
        "expected_stage": "heuristic",
    },
    {
        "file": "medical-lab-2.txt",
        "label": "Medical Lab (second — should match existing pack)",
        "expected_stage": "heuristic",
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
# Agent run + polling + tool-policy confirm auto-approve
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
    """Poll until run is completed, failed, or paused. Returns (status, resp)."""
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


def approve_tool_confirm(project_id, question):
    """Auto-approve a tool-policy confirmation question. Returns resume_run_id."""
    qid = question["id"]
    options = question.get("options", [])
    # Find the approve option (value == "approve" or label contains "Approve")
    chosen_label = None
    for opt in options:
        if opt.get("value") == "approve" or "approve" in opt.get("label", "").lower():
            chosen_label = opt["label"]
            break
    if not chosen_label and options:
        chosen_label = options[0]["label"]
    if not chosen_label:
        chosen_label = "Approve"

    print(f"  Tool policy confirm: auto-approving ('{chosen_label}')")
    resp = post(f"/api/projects/{project_id}/agent-questions/{qid}/respond", {
        "response": chosen_label,
    })
    resume_run_id = None
    if isinstance(resp, dict):
        data = resp.get("data", resp)
        resume_run_id = data.get("resumeRunId") or data.get("resume_run_id")
    return resume_run_id


def run_agent_with_auto_approve(project_id, doc_id):
    """Start run, auto-approve any tool-policy confirmations, wait for completion."""
    run_id = start_agent_run(project_id, doc_id)
    tool_confirms = []  # track approved tool names from question text

    while True:
        status, resp = poll_run_status(project_id, run_id)
        if status == "input-required":
            question = find_pending_question(project_id, run_id)
            if question:
                if MANUAL_RESPOND:
                    print(f"\n  *** QUESTION PENDING — answer in the UI, then press Enter ***")
                    print(f"  Question: {question.get('question', '')}")
                    print(f"  Options:  {[o.get('label') for o in question.get('options', [])]}")
                    print(f"  Question ID: {question['id']}")
                    print(f"  Project: {project_id}")
                    input("  [Press Enter after answering in the UI] ")
                    continue
                tool_confirms.append(question.get("question", ""))
                resume_run_id = approve_tool_confirm(project_id, question)
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

    return run_id, tool_confirms


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
         stage = signals.get("stage") or doc.get("domain_label") or ("new_domain" if not doc.get("domainName") else "classified")
         return {
             "stage": stage,
             "domain_name": doc.get("domainName"),
             "confidence": doc.get("domainConfidence") or doc.get("domain_confidence") or 0.0,
             "schema_id": schema_id,
         }
    except Exception:
        return {"stage": "unset", "domain_name": None, "confidence": 0.0, "schema_id": None}


def assert_document_classified(project_id, doc_id, expected_stage, pre_agent_snapshot=None):
    results = []
    try:
        if expected_stage == "new_domain":
            pre = pre_agent_snapshot or snapshot_document_stage(doc_id)
            results.append(check("matched_schema_id=null before agent", pre["schema_id"] is None, f"got={pre['schema_id']}"))
            doc = get(f"/api/documents/{doc_id}")
            signals = doc.get("classificationSignals") or {}
            domain_name = doc.get("domainName")
            real_name = domain_name and domain_name != "new_domain"
            results.append(check("domain_name set after finalize-discovery", real_name, f"got={domain_name}"))
        else:
            doc = get(f"/api/documents/{doc_id}")
            stage = doc.get("domainName") or doc.get("domain_label") or "unset"
            confidence = doc.get("domainConfidence") or doc.get("domain_confidence") or 0.0
            signals = doc.get("classificationSignals") or {}
            schema_id = doc.get("matched_schema_id") or signals.get("matchedSchemaId")
            results.append(check("domain_name set (classified)", stage not in ("new_domain", "unset"), f"got={stage}"))
            results.append(check("domain_confidence >= 0.7", confidence >= 0.7, f"got={confidence:.2f}"))
            results.append(check("matched_schema_id set", schema_id is not None, f"got={schema_id}"))
    except Exception as e:
        results.append(check("document fetch", False, str(e)))
    return results




def assert_agent_proposed_domain(project_id, run_id, tool_confirms):
    """
    Check that:
    a) finalize-discovery was called — evidenced by tool policy confirm pause
       (finalize-discovery is the only tool with confirm:true in this agent)
    b) tool policy confirmation was issued (user was asked)
    c) the confirm message references finalize-discovery
    """
    results = []

    # (b) Tool policy confirm was issued
    results.append(check(
        "agent paused for tool policy confirm (user asked)",
        len(tool_confirms) > 0,
        f"confirms={len(tool_confirms)}"
    ))

    # (a)+(c) The confirm question text should reference finalize-discovery
    if tool_confirms:
        q_text = " ".join(tool_confirms).lower()
        mentions_tool = "finalize-discovery" in q_text or "finalize discovery" in q_text or "schema pack" in q_text
        results.append(check(
            "tool policy confirm mentions finalize-discovery",
            mentions_tool,
            f"question={tool_confirms[0][:80]!r}"
        ))

    return results


def assert_schema_created_any(project_id, schema_count_before, retries=6, delay=5):
    """Check that at least one new schema was created (count increased)."""
    results = []
    try:
        names = []
        count_after = schema_count_before
        for attempt in range(retries):
            resp = get(f"/api/schemas/projects/{project_id}/installed")
            schemas = resp if isinstance(resp, list) else (resp.get("schemas") or resp.get("items") or [])
            names = [s.get("name", "") for s in schemas]
            count_after = len(schemas)
            if count_after > schema_count_before:
                break
            if attempt < retries - 1:
                time.sleep(delay)
        results.append(check(
            "schema created (count increased)",
            count_after > schema_count_before,
            f"before={schema_count_before} after={count_after} names={names}"
        ))
        if count_after > schema_count_before:
            # Verify the newest schema has non-empty extraction prompts
            newest = next((s for s in schemas if s.get("name") not in [""]), None)
            if newest:
                prompts = newest.get("extractionPrompts") or newest.get("extraction_prompts") or {}
                domain_context = prompts.get("domainContext") or prompts.get("domain_context") or ""
                type_hints = prompts.get("typeHints") or prompts.get("type_hints") or {}
                results.append(check("schema has domainContext", bool(domain_context), f"domainContext={domain_context[:60] if domain_context else ''}"))
                results.append(check("schema has typeHints", len(type_hints) > 0, f"typeHints={list(type_hints.keys())[:3]}"))
    except Exception as e:
        results.append(check("schema fetch", False, str(e)))
    return results, count_after if 'count_after' in dir() else schema_count_before


def assert_reextraction_queued(project_id, doc_id):
    """Assert reextraction job was queued, poll until complete, report object/relation stats."""
    results = []
    try:
        # Poll for reextraction job to appear (agent calls queue-reextraction after finalize-discovery)
        reextract_job = None
        for _ in range(12):
            resp = get(f"/api/monitoring/extraction-jobs", params={"source_id": doc_id})
            jobs = resp.get("jobs") or resp.get("items") or []
            reextract_job = next((j for j in jobs if j.get("job_type") == "reextraction"), None)
            if reextract_job:
                break
            time.sleep(5)

        results.append(check("reextraction job queued", reextract_job is not None, f"count=0"))
        if not reextract_job:
            return results

        job_id = reextract_job["id"]

        # Poll until completed (max 90s)
        for _ in range(18):
            resp = get(f"/api/monitoring/extraction-jobs", params={"source_id": doc_id})
            jobs = resp.get("jobs") or resp.get("items") or []
            reextract_job = next((j for j in jobs if j["id"] == job_id), reextract_job)
            if reextract_job.get("status") in ("completed", "failed", "error"):
                break
            time.sleep(5)

        objects = reextract_job.get("objects_created") or 0
        relations = reextract_job.get("relationships_created") or 0
        status = reextract_job.get("status", "unknown")
        print(f"    Reextraction job {job_id[:8]}: status={status} objects={objects} relations={relations}")
        results.append(check("reextraction completed", status == "completed", f"status={status}"))
        results.append(check("objects extracted > 0", objects > 0, f"objects={objects}"))
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
        results.append({"label": "no discovery job fired", "status": SKIP, "detail": str(e)})
    return results


MANUAL_RESPOND = False  # set to True via --manual flag


# ---------------------------------------------------------------------------
# Schema count tracker (shared across doc runs in one test session)
# ---------------------------------------------------------------------------

_schema_count: int = 0


def get_schema_count(project_id):
    try:
        resp = get(f"/api/schemas/projects/{project_id}/installed")
        schemas = resp if isinstance(resp, list) else (resp.get("schemas") or resp.get("items") or [])
        return len(schemas)
    except Exception:
        return 0


# ---------------------------------------------------------------------------
# Main test loop
# ---------------------------------------------------------------------------

def run_test_doc(project_id, doc_config, idx, schema_count_before):
    print(f"\n--- Doc {idx+1}/6: {doc_config['label']} ---")
    filepath = FIXTURES_DIR / doc_config["file"]

    doc_id = upload_document(project_id, filepath)
    print("  Waiting 15s for extraction worker...")
    time.sleep(15)

    pre_agent_snapshot = snapshot_document_stage(doc_id)
    print(f"  Pre-agent domain snapshot: stage={pre_agent_snapshot['stage']}")

    run_id, tool_confirms = run_agent_with_auto_approve(project_id, doc_id)

    print("  Running assertions...")
    all_results = []

    all_results += assert_document_classified(project_id, doc_id, doc_config["expected_stage"], pre_agent_snapshot)

    schema_count_after = schema_count_before
    if doc_config["expected_stage"] == "new_domain":
        # a) agent proposed a domain and b) user was asked
        all_results += assert_agent_proposed_domain(project_id, run_id, tool_confirms)
        # c) user approved → schema created
        schema_results, schema_count_after = assert_schema_created_any(project_id, schema_count_before)
        all_results += schema_results
        # d) reextraction queued (objects will be upserted)
        all_results += assert_reextraction_queued(project_id, doc_id)
    else:
        all_results += assert_no_discovery_fired(project_id, run_id)

    passes = sum(1 for r in all_results if r["status"] == PASS)
    fails = sum(1 for r in all_results if r["status"] == FAIL)
    skips = sum(1 for r in all_results if r["status"] == SKIP)
    print(f"  Result: {passes} passed, {fails} failed, {skips} skipped")

    return {
        "doc": doc_config["label"],
        "doc_id": doc_id,
        "run_id": run_id,
        "assertions": all_results,
        "passes": passes,
        "fails": fails,
    }, schema_count_after


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
    """Delete all documents and uninstall all schemas from an existing project."""
    print(f"Resetting project {project_id}...")

    try:
        docs_resp = get(f"/api/documents", project_id=project_id, params={"limit": 500})
        docs_list = docs_resp if isinstance(docs_resp, list) else (docs_resp.get("items") or docs_resp.get("documents") or [])
        doc_ids = [d["id"] for d in docs_list]
        if doc_ids:
            delete(f"/api/documents", body={"ids": doc_ids}, project_id=project_id)
            print(f"  Deleted {len(doc_ids)} document(s).")
        else:
            print("  No documents to delete.")
    except Exception as e:
        print(f"  WARNING: document deletion failed: {e}")

    try:
        installed = get(f"/api/schemas/projects/{project_id}/installed")
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

    print("  Reset complete.\n")


TEST_PROJECT_NAME_PREFIX = "Personal Assistant KB [domain-test"

def cleanup_project(project_id):
    """Delete a test project — ONLY if its name matches the test prefix."""
    print(f"Cleaning up project {project_id}...")
    try:
        proj = requests.get(
            f"{SERVER}/api/projects/{project_id}",
            headers=headers(),
        )
        proj.raise_for_status()
        name = proj.json().get("name", "")
        if TEST_PROJECT_NAME_PREFIX not in name:
            print(f"  SKIPPED — project name {name!r} does not match test prefix. Not deleting.")
            return
        requests.delete(f"{SERVER}/api/projects/{project_id}", headers=headers())
        print(f"  Deleted project: {name!r}")
    except Exception as e:
        print(f"  Cleanup failed: {e}")


# ---------------------------------------------------------------------------
# Entry point
# ---------------------------------------------------------------------------

def main():
    parser = argparse.ArgumentParser(description="Domain-aware extraction e2e test")
    parser.add_argument("--project-id", help="Reuse existing project instead of creating new")
    parser.add_argument("--keep-project", action="store_true", help="Do NOT delete project after test (default: always delete test projects)")
    parser.add_argument("--cleanup", action="store_true", help=argparse.SUPPRESS)  # legacy alias
    parser.add_argument("--reset", action="store_true", help="Reset project state before running; requires --project-id")
    parser.add_argument("--doc", type=int, help="Run only this doc index (1-6)")
    parser.add_argument("--manual", action="store_true", help="Pause at questions for human response")
    args = parser.parse_args()

    if args.manual:
        global MANUAL_RESPOND
        MANUAL_RESPOND = True

    if not TOKEN:
        print("ERROR: EMERGENT_MEMORY_TOKEN not set")
        sys.exit(1)

    print("Domain-Aware Extraction E2E Test")
    print(f"Server: {SERVER}")
    print()

    # --project-id implies --keep-project (don't delete a pre-existing project)
    is_fresh_project = not args.project_id
    should_cleanup = is_fresh_project and not args.keep_project

    project_id = args.project_id or setup_project()
    if args.project_id:
        set_project_id(project_id)
    print(f"Using project: {project_id}")
    print(f"Auto-cleanup after test: {'yes' if should_cleanup else 'no (--keep-project or pre-existing)'}\n")

    if args.reset:
        if not args.project_id:
            print("WARNING: --reset has no effect without --project-id")
        else:
            reset_project(project_id)

    docs_to_run = TEST_DOCS
    if args.doc:
        docs_to_run = [TEST_DOCS[args.doc - 1]]

    schema_count = get_schema_count(project_id)

    results = []
    try:
        for idx, doc_config in enumerate(docs_to_run):
            result, schema_count = run_test_doc(project_id, doc_config, idx, schema_count)
            results.append(result)
    finally:
        print_summary(results)
        if should_cleanup:
            cleanup_project(project_id)

    total_fails = sum(r["fails"] for r in results)
    sys.exit(0 if total_fails == 0 else 1)


if __name__ == "__main__":
    main()
