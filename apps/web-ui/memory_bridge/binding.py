"""Per-room voice binding — resolved from the gateway per LiveKit session.

The Go supervisor no longer injects memory credentials / agent-definition /
language as process env: each voice session (room/job) carries its own identity.
The worker asks the gateway's internal endpoint for the room's binding and builds
every per-session client from it.

Binding JSON shape (field names match gateway/agent.go exactly):
    {"project_id": "<uuid>", "org_id": "<uuid-or-empty>",
     "agent_definition_id": "<uuid>", "language": "<iso-639-1-or-empty>",
     "token": "emt_..."}
"""

from dataclasses import dataclass

import httpx

_BINDING_PATH = "/internal/voice-binding"


class VoiceBindingError(RuntimeError):
    """Binding fetch failed: non-200 response, malformed body or missing token."""


@dataclass(frozen=True)
class VoiceBinding:
    """Per-session credentials + agent identity for one voice room."""

    project_id: str
    org_id: str
    agent_definition_id: str
    language: str
    token: str


async def fetch_binding(
    room: str,
    base_url: str,
    worker_key: str,
    transport: httpx.AsyncBaseTransport | None = None,
) -> VoiceBinding:
    """GET the gateway's binding for ``room`` and parse it.

    Raises :class:`VoiceBindingError` on non-200 responses, malformed JSON and
    missing required fields (no env fallback exists anymore).
    """
    url = f"{base_url.rstrip('/')}{_BINDING_PATH}"
    headers = {"X-Worker-Key": worker_key}
    timeout = httpx.Timeout(10.0, connect=5.0)
    async with httpx.AsyncClient(timeout=timeout, transport=transport) as client:
        resp = await client.get(url, params={"room": room}, headers=headers)

    if resp.status_code != 200:
        raise VoiceBindingError(
            f"voice binding fetch failed: {resp.status_code} for room {room!r} "
            f"({url}): {resp.text[:200]}"
        )
    try:
        data = resp.json()
    except ValueError as e:
        raise VoiceBindingError(
            f"voice binding fetch: malformed JSON for room {room!r}: {e}"
        ) from e

    def _field(name: str) -> str:
        value = data.get(name)
        return value.strip() if isinstance(value, str) else ""

    token = _field("token")
    if not token:
        raise VoiceBindingError(
            f"voice binding fetch: response for room {room!r} missing token"
        )
    return VoiceBinding(
        project_id=_field("project_id"),
        org_id=_field("org_id"),
        agent_definition_id=_field("agent_definition_id"),
        language=_field("language"),
        token=token,
    )
