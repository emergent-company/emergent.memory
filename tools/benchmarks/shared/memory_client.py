"""
Shared Memory API client for benchmark harnesses.
Wraps POST /remember and POST /query with SSE parsing.
"""

from __future__ import annotations

import json
import os
import time
from typing import Iterator

import requests

from .config import get_config


def _sse_lines(resp: requests.Response) -> Iterator[dict]:
    """Parse SSE stream → yield event dicts."""
    for raw in resp.iter_lines():
        if not raw:
            continue
        line = raw.decode("utf-8") if isinstance(raw, bytes) else raw
        if not line.startswith("data: "):
            continue
        data = line[6:]
        if not data:
            continue
        try:
            yield json.loads(data)
        except json.JSONDecodeError:
            continue


def remember(
    text: str,
    *,
    project_id: str | None = None,
    schema_policy: str = "auto",
    dry_run: bool = False,
    session_id: str | None = None,
    namespace: str | None = None,
    timeout: int = 300,
) -> dict:
    """
    POST /api/projects/:projectId/remember
    Returns dict with keys: response, session_id, tools, elapsed_ms, error.
    If namespace is set, it is prepended to the message as a context hint.
    """
    cfg = get_config()
    pid = project_id or cfg.project_id
    if not pid:
        raise ValueError("project_id required (set MEMORY_PROJECT_ID or pass --project)")

    msg = text
    if namespace:
        msg = f"[namespace: {namespace}]\n\n{text}"

    body: dict = {
        "message": msg,
        "schema_policy": schema_policy,
        "dry_run": dry_run,
    }
    if session_id:
        body["conversation_id"] = session_id

    url = f"{cfg.api_url}/api/projects/{pid}/remember"
    headers = {"Content-Type": "application/json", **cfg.auth_headers()}

    start = time.time()
    resp = requests.post(url, json=body, headers=headers, stream=True, timeout=timeout)
    resp.raise_for_status()

    response_text = []
    tools: list[str] = []
    out_session_id: str | None = None
    stream_error: str | None = None

    for event in _sse_lines(resp):
        etype = event.get("type", "")
        if etype == "meta":
            out_session_id = event.get("conversationId")
        elif etype == "token":
            tok = event.get("token", "")
            response_text.append(tok)
        elif etype == "mcp_tool" and event.get("status") == "started":
            tools.append(event.get("tool", ""))
        elif etype == "error":
            stream_error = event.get("error")

    return {
        "response": "".join(response_text),
        "session_id": out_session_id,
        "tools": tools,
        "elapsed_ms": int((time.time() - start) * 1000),
        "error": stream_error,
    }


def query(
    question: str,
    *,
    project_id: str | None = None,
    session_id: str | None = None,
    namespace: str | None = None,
    timeout: int = 120,
) -> dict:
    """
    POST /api/projects/:projectId/query
    Returns dict with keys: answer, session_id, tools, elapsed_ms, error.
    If namespace is set, it is prepended as a scoping hint to the question.
    """
    cfg = get_config()
    pid = project_id or cfg.project_id
    if not pid:
        raise ValueError("project_id required")

    msg = question
    if namespace:
        msg = f"[namespace: {namespace}] {question}"

    body: dict = {"message": msg}
    if session_id:
        body["conversation_id"] = session_id

    url = f"{cfg.api_url}/api/projects/{pid}/query"
    headers = {"Content-Type": "application/json", **cfg.auth_headers()}

    start = time.time()
    resp = requests.post(url, json=body, headers=headers, stream=True, timeout=timeout)
    resp.raise_for_status()

    answer_parts: list[str] = []
    tools: list[str] = []
    out_session_id: str | None = None
    stream_error: str | None = None

    for event in _sse_lines(resp):
        etype = event.get("type", "")
        if etype == "meta":
            out_session_id = event.get("conversationId")
        elif etype == "token":
            answer_parts.append(event.get("token", ""))
        elif etype == "mcp_tool" and event.get("status") == "started":
            tools.append(event.get("tool", ""))
        elif etype == "error":
            stream_error = event.get("error")

    return {
        "answer": "".join(answer_parts).strip(),
        "session_id": out_session_id,
        "tools": tools,
        "elapsed_ms": int((time.time() - start) * 1000),
        "error": stream_error,
    }
