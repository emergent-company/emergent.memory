"""Streaming client for Emergent Memory's chat API (the agent brain).

The bridge does NOT run an LLM — it streams text turns through memory's
`POST /api/chat/stream`, which drives the agent loop (prompt, model, MCP tools).
"""

import json
from urllib.parse import quote

import httpx


class MemoryChatError(RuntimeError):
    """Non-2xx response from the memory chat endpoint."""


class MemoryChatClient:
    def __init__(
        self,
        base_url: str,
        token: str,
        project_id: str | None = None,
        transport: httpx.AsyncBaseTransport | None = None,
    ):
        self._base_url = base_url.rstrip("/")
        self._token = token
        self._project_id = project_id
        self._transport = transport

    async def stream(self, message: str, agent_definition_id: str, conversation_id: str | None = None):
        """Stream one chat turn.

        Yields decoded SSE event dicts with a `type` key:
        ``meta`` | ``token`` | ``mcp_tool`` | ``thinking`` | ``approval`` |
        ``error`` | ``done``.
        """
        url = f"{self._base_url}/api/chat/stream"
        body: dict = {"message": message, "agentDefinitionId": agent_definition_id}
        if conversation_id:
            body["conversationId"] = conversation_id
        headers = {
            "Authorization": f"Bearer {self._token}",
            "Content-Type": "application/json",
        }
        timeout = httpx.Timeout(120.0, connect=10.0)
        async with httpx.AsyncClient(timeout=timeout, transport=self._transport) as client:
            async with client.stream("POST", url, json=body, headers=headers) as resp:
                if resp.status_code >= 400:
                    raw = (await resp.aread()).decode()
                    raise MemoryChatError(f"memory chat {resp.status_code}: {raw}")
                async for line in resp.aiter_lines():
                    if not line.startswith("data: "):
                        continue
                    payload = line[len("data: "):].strip()
                    if not payload:
                        continue
                    try:
                        event = json.loads(payload)
                    except json.JSONDecodeError:
                        continue
                    yield event

    async def respond(
        self, question_id: str, response: str, message: str = ""
    ) -> dict:
        """Answer a pending agent question and resume its paused run.

        ``response`` is the raw value memory expects: ``"approve"``/``"reject"``
        for tool-policy confirmations, or the free-text/option answer for
        ``ask_user`` questions. ``message`` is an optional human reason attached
        to a tool-policy rejection.

        Memory resumes the run in the background and answers with JSON (202),
        not an SSE stream (see gateway/memory.go RespondQuestion).
        """
        body: dict = {"response": response}
        if message:
            body["message"] = message
        return await self._question_action(question_id, "respond", body)

    async def cancel(self, question_id: str) -> dict:
        """Revoke a pending tool-policy confirmation.

        The paused run resumes with the tool call treated as not taken
        (gateway/memory.go CancelQuestion). No request body is sent.
        """
        return await self._question_action(question_id, "cancel", None)

    async def _question_action(self, question_id: str, action: str, body: dict | None) -> dict:
        if not self._project_id:
            raise MemoryChatError(
                "agent-question respond/cancel requires project_id (from voice binding)"
            )
        url = (
            f"{self._base_url}/api/projects/{quote(self._project_id, safe='')}"
            f"/agent-questions/{quote(question_id, safe='')}/{action}"
        )
        headers = {
            "Authorization": f"Bearer {self._token}",
            "Content-Type": "application/json",
        }
        timeout = httpx.Timeout(60.0, connect=10.0)
        async with httpx.AsyncClient(timeout=timeout, transport=self._transport) as client:
            resp = await client.post(url, json=body, headers=headers)
            if resp.status_code >= 400:
                raw = resp.text
                raise MemoryChatError(f"memory {action} {resp.status_code}: {raw}")
            try:
                return resp.json()
            except ValueError:
                return {}
