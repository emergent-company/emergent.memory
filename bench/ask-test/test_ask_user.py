#!/usr/bin/env python3
"""
Single HITL (ask_user) end-to-end test.

Uses the remember-test blueprint agent (already has ask_user tool).
Sends a message that forces the agent to call ask_user, then responds
and verifies the run resumes and completes successfully.

Usage:
    EMERGENT_MEMORY_TOKEN=emt_... MEMORY_ORG_ID=<uuid> python3 test_ask_user.py
    EMERGENT_MEMORY_TOKEN=emt_... MEMORY_ORG_ID=<uuid> python3 test_ask_user.py --cleanup

Environment:
    EMERGENT_MEMORY_TOKEN  required  org-scoped token
    MEMORY_SERVER          optional  default: https://memory.emergent-company.ai
    MEMORY_ORG_ID          required  org UUID
    GOOGLE_API_KEY         optional  for provider config (falls back to existing config)
    MEMORY_PROJECT_ID      optional  reuse existing project
"""

import os
import sys
import time
import subprocess
import argparse
import requests
from typing import Optional

# ---------------------------------------------------------------------------
# Config
# ---------------------------------------------------------------------------

SERVER     = os.environ.get("MEMORY_SERVER", "https://memory.emergent-company.ai")
ORG_TOKEN  = os.environ.get("EMERGENT_MEMORY_TOKEN", "")
ORG_ID     = os.environ.get("MEMORY_ORG_ID", "")
AGENT_NAME = "remember-test"
BLUEPRINT_PATH = os.path.join(os.path.dirname(__file__), "..", "..", "blueprints", "test-agents")

_project_id:    Optional[str] = None
_project_token: Optional[str] = None


def set_project(pid: str, tok: str):
    global _project_id, _project_token
    _project_id, _project_token = pid, tok


def headers(use_project=True):
    tok = (_project_token if use_project and _project_token else None) or ORG_TOKEN
    h = {"Authorization": f"Bearer {tok}", "Content-Type": "application/json"}
    if _project_id:
        h["x-project-id"] = _project_id
    return h


def get(path, params=None):
    r = requests.get(f"{SERVER}{path}", headers=headers(), params=params)
    r.raise_for_status()
    return r.json()


def post(path, body=None, use_org=False):
    tok = ORG_TOKEN if use_org else (_project_token or ORG_TOKEN)
    h = {"Authorization": f"Bearer {tok}", "Content-Type": "application/json"}
    if _project_id and not use_org:
        h["x-project-id"] = _project_id
    r = requests.post(f"{SERVER}{path}", headers=h, json=body or {})
    if not r.ok:
        print(f"  HTTP {r.status_code} on POST {path}: {r.text[:400]}")
    r.raise_for_status()
    return r.json()


def delete_project(project_id: str):
    r = requests.delete(
        f"{SERVER}/api/projects/{project_id}",
        headers={"Authorization": f"Bearer {ORG_TOKEN}", "Content-Type": "application/json"},
    )
    return r.status_code


# ---------------------------------------------------------------------------
# Setup
# ---------------------------------------------------------------------------

def create_project() -> tuple[str, str]:
    print("Creating test project...")
    r = requests.post(
        f"{SERVER}/api/projects",
        headers={"Authorization": f"Bearer {ORG_TOKEN}", "Content-Type": "application/json"},
        json={"name": f"ask-hitl-test [{int(time.time())}]", "orgId": ORG_ID},
    )
    r.raise_for_status()
    project_id = r.json()["id"]

    tr = requests.post(
        f"{SERVER}/api/projects/{project_id}/tokens",
        headers={"Authorization": f"Bearer {ORG_TOKEN}", "Content-Type": "application/json"},
        json={
            "name": f"ask-hitl-bench-{int(time.time())}",
            "scopes": ["agents:read", "agents:write", "data:read", "data:write", "schema:read", "schema:write"],
        },
    )
    tr.raise_for_status()
    project_token = tr.json()["token"]
    set_project(project_id, project_token)
    print(f"  Project: {project_id}")
    return project_id, project_token


def apply_blueprint(project_id: str):
    print(f"  Installing blueprint into project {project_id}...")
    env = os.environ.copy()
    env["MEMORY_PROJECT_TOKEN"] = _project_token or ORG_TOKEN
    env.pop("EMERGENT_MEMORY_TOKEN", None)
    env.pop("MEMORY_API_KEY", None)
    result = subprocess.run(
        [
            os.path.expanduser("~/.memory/bin/memory"),
            "blueprints", "install",
            BLUEPRINT_PATH,
            "--project", project_id,
            "--server", SERVER,
        ],
        capture_output=True, text=True, env=env,
    )
    if result.returncode != 0:
        print(f"  WARNING: blueprint apply failed: {result.stderr.strip()[:300]}")
        print(f"  stdout: {result.stdout.strip()[:300]}")
    else:
        print(f"  Blueprint applied: {result.stdout.strip()}")


def create_runtime_agent(project_id: str) -> Optional[str]:
    print(f"  Creating runtime agent '{AGENT_NAME}'...")
    # Look up agent definition installed by blueprint
    try:
        defs_resp = requests.get(
            f"{SERVER}/api/projects/{project_id}/agent-definitions",
            headers={"Authorization": f"Bearer {_project_token or ORG_TOKEN}", "Content-Type": "application/json", "x-project-id": project_id},
        )
        defs_resp.raise_for_status()
        defs = defs_resp.json().get("definitions") or defs_resp.json().get("items") or defs_resp.json()
        def_id = next((d["id"] for d in (defs if isinstance(defs, list) else []) if d.get("name") == AGENT_NAME), None)
    except Exception as e:
        print(f"  WARNING: could not fetch agent definitions: {e}")
        def_id = None

    payload = {
        "projectId": project_id,
        "name": AGENT_NAME,
        "strategyType": "external",
        "cronSchedule": "0 0 * * *",
        "enabled": True,
        "triggerType": "manual",
    }
    if def_id:
        payload["agentDefinitionId"] = def_id

    try:
        r = requests.post(
            f"{SERVER}/api/projects/{project_id}/agents",
            headers={"Authorization": f"Bearer {_project_token or ORG_TOKEN}", "Content-Type": "application/json", "x-project-id": project_id},
            json=payload,
        )
        r.raise_for_status()
        body = r.json()
        agent_id = body.get("id") or (body.get("data") or {}).get("id")
        print(f"  Runtime agent created: {agent_id}")
        return agent_id
    except Exception as e:
        print(f"  WARNING: runtime agent creation failed: {e}")
        return None


def configure_provider(project_id: str):
    deepseek_key = os.environ.get("DEEPSEEK_API_KEY", "")
    google_key   = os.environ.get("GOOGLE_API_KEY", "")
    if google_key:
        provider, extra = "google", ["--api-key", google_key, "--generative-model", "gemini-2.5-flash", "--embedding-model", "gemini-embedding-2-preview"]
    elif deepseek_key:
        provider, extra = "deepseek", ["--api-key", deepseek_key]
    else:
        print("  WARNING: no LLM provider key set (GOOGLE_API_KEY or DEEPSEEK_API_KEY) — agent may fail")
        return
    env = os.environ.copy()
    env["EMERGENT_MEMORY_TOKEN"] = ORG_TOKEN
    env.pop("MEMORY_PROJECT_TOKEN", None)
    env.pop("MEMORY_API_KEY", None)
    result = subprocess.run(
        [os.path.expanduser("~/.memory/bin/memory"), "provider", "configure", provider] + extra +
        ["--project", project_id, "--server", SERVER, "--org-id", ORG_ID],
        capture_output=True, text=True, env=env,
    )
    if result.returncode != 0:
        print(f"  WARNING: provider configure failed: {result.stderr.strip()[:300]}")
    else:
        print(f"  Provider configured ({provider})")


def setup_project() -> tuple[str, str]:
    project_id, project_token = create_project()
    apply_blueprint(project_id)
    configure_provider(project_id)
    agent_id = create_runtime_agent(project_id)
    print()
    return project_id, agent_id


# ---------------------------------------------------------------------------
# Run helpers
# ---------------------------------------------------------------------------

def start_run(project_id: str, agent_id: str) -> str:
    resp = post(f"/acp/v1/agents/{AGENT_NAME}/runs", {
        "message": [{"content_type": "text/plain", "content": "Ask me a test question using ask_user with options Yes and No."}],
        "mode": "async",
    })
    run_id = resp.get("id") or resp.get("run_id")
    print(f"  Run ID: {run_id}")
    return run_id


def poll_run(run_id: str, timeout=120) -> tuple[str, dict]:
    deadline = time.time() + timeout
    while time.time() < deadline:
        try:
            resp = get(f"/acp/v1/agents/{AGENT_NAME}/runs/{run_id}")
            status = resp.get("status", "")
            if status in ("completed", "failed", "input-required"):
                return status, resp
        except Exception as e:
            print(f"  poll error: {e}")
        time.sleep(2)
    return "timeout", {}


def find_pending_question(project_id: str, run_id: str) -> Optional[dict]:
    for _ in range(15):
        try:
            resp = get(f"/api/projects/{project_id}/agent-questions", params={"run_id": run_id, "status": "pending"})
            questions = resp.get("questions") or resp.get("items") or []
            if questions:
                return questions[0]
        except Exception as e:
            print(f"  find_pending_question error: {e}")
        time.sleep(1)
    return None


def respond_to_question(project_id: str, question: dict, answer: str) -> Optional[str]:
    qid = question["id"]
    print(f"  Responding '{answer}' to question: {question.get('question', '')!r}")
    resp = post(f"/api/projects/{project_id}/agent-questions/{qid}/respond", {"response": answer})
    data = resp.get("data", resp) if isinstance(resp, dict) else {}
    resume_run_id = data.get("resumeRunId") or data.get("resume_run_id")
    if resume_run_id:
        print(f"  Resume run ID: {resume_run_id}")
    return resume_run_id


# ---------------------------------------------------------------------------
# Main test
# ---------------------------------------------------------------------------

def run_test(project_id: str, agent_id: str) -> bool:
    ANSWER = "Yes"

    # 1. Start run
    print("Starting agent run...")
    run_id = start_run(project_id, agent_id)
    if not run_id:
        print("  FAIL: no run_id returned")
        return False

    # 2. Wait for paused
    print("Waiting for run to pause on ask_user...")
    status, run = poll_run(run_id, timeout=90)
    if status != "input-required":
        print(f"  FAIL: expected 'input-required', got '{status}'")
        if run:
            print(f"  Run data: {run}")
        return False
    print(f"  Run paused correctly.")

    # 3. Find pending question
    print("Looking for pending question...")
    question = find_pending_question(project_id, run_id)
    if not question:
        print("  FAIL: no pending question found")
        return False
    print(f"  Question: {question.get('question')!r}")
    opts = [o.get('label') for o in question.get('options', [])]
    print(f"  Options:  {opts}")

    # 4. Respond
    resume_run_id = respond_to_question(project_id, question, ANSWER)
    poll_id = resume_run_id or run_id

    # 5. Wait for completion
    print(f"Waiting for run {poll_id} to complete...")
    status, run = poll_run(poll_id, timeout=90)
    if status not in ("success", "completed"):
        print(f"  FAIL: expected success, got '{status}'")
        print(f"  Run: {run}")
        return False

    print(f"  Run completed successfully.")
    print(f"\n  PASS: HITL ask_user → respond → resume → complete")
    return True


# ---------------------------------------------------------------------------
# Entry point
# ---------------------------------------------------------------------------

def main():
    parser = argparse.ArgumentParser()
    parser.add_argument("--cleanup", action="store_true", help="Delete project after test")
    parser.add_argument("--project-id", help="Reuse existing project (skip creation)")
    parser.add_argument("--agent-id",   help="Reuse existing agent (skip creation)")
    args = parser.parse_args()

    if not ORG_TOKEN:
        print("ERROR: EMERGENT_MEMORY_TOKEN not set")
        sys.exit(1)
    if not ORG_ID and not args.project_id:
        print("ERROR: MEMORY_ORG_ID not set")
        sys.exit(1)

    project_id = args.project_id
    agent_id   = args.agent_id

    if project_id:
        tr = requests.post(
            f"{SERVER}/api/projects/{project_id}/tokens",
            headers={"Authorization": f"Bearer {ORG_TOKEN}", "Content-Type": "application/json"},
            json={"name": f"ask-hitl-bench-{int(time.time())}", "scopes": ["agents:read", "agents:write", "data:read", "data:write"]},
        )
        tr.raise_for_status()
        set_project(project_id, tr.json()["token"])
    else:
        project_id, agent_id = setup_project()

    ok = run_test(project_id, agent_id)

    if args.cleanup:
        print(f"\nCleaning up project {project_id}...")
        sc = delete_project(project_id)
        print(f"  Delete status: {sc}")

    print()
    if ok:
        print("=== TEST PASSED ===")
        sys.exit(0)
    else:
        print("=== TEST FAILED ===")
        sys.exit(1)


if __name__ == "__main__":
    main()
