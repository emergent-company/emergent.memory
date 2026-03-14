"""
Server-Sent Events (SSE) streaming support.

The Emergent server streams responses as newline-delimited SSE lines in
the format::

    data: {"type": "...", ...}\n\n

All events are discriminated by the ``"type"`` field.  This module parses
the raw byte stream into typed Python dataclasses.

Event type hierarchy
--------------------
``MetaEvent``     — first event; carries ``conversation_id``, ``citations``, etc.
``TokenEvent``    — streamed text delta; accumulate ``token`` fields for full text.
``MCPToolEvent``  — tool call lifecycle (``status``: ``started`` | ``completed`` | ``error``).
``ErrorEvent``    — stream-level error; streaming stops after this.
``DoneEvent``     — final event; stream is complete.
``UnknownEvent``  — catch-all for future event types.
"""
from __future__ import annotations

import json
from dataclasses import dataclass, field
from typing import Any, Dict, Generator, Iterator, List, Optional


# ---------------------------------------------------------------------------
# Typed event models
# ---------------------------------------------------------------------------


@dataclass
class MetaEvent:
    type: str = "meta"
    conversation_id: Optional[str] = None
    citations: List[Any] = field(default_factory=list)
    graph_objects: List[Any] = field(default_factory=list)
    graph_neighbors: Optional[Any] = None


@dataclass
class TokenEvent:
    type: str = "token"
    token: str = ""


@dataclass
class MCPToolEvent:
    type: str = "mcp_tool"
    tool: str = ""
    status: str = ""          # "started" | "completed" | "error"
    result: Optional[Any] = None
    error: str = ""


@dataclass
class ErrorEvent:
    type: str = "error"
    error: str = ""


@dataclass
class DoneEvent:
    type: str = "done"


@dataclass
class UnknownEvent:
    type: str = ""
    raw: Dict[str, Any] = field(default_factory=dict)


SSEEvent = MetaEvent | TokenEvent | MCPToolEvent | ErrorEvent | DoneEvent | UnknownEvent


# ---------------------------------------------------------------------------
# Parser
# ---------------------------------------------------------------------------


def _parse_event(data: str) -> SSEEvent:
    """Parse a single ``data:`` JSON payload into a typed event."""
    try:
        obj: Dict[str, Any] = json.loads(data)
    except json.JSONDecodeError:
        return UnknownEvent(type="parse_error", raw={"raw": data})

    event_type = obj.get("type", "")

    if event_type == "meta":
        return MetaEvent(
            conversation_id=obj.get("conversationId"),
            citations=obj.get("citations") or [],
            graph_objects=obj.get("graphObjects") or [],
            graph_neighbors=obj.get("graphNeighbors"),
        )
    elif event_type == "token":
        return TokenEvent(token=obj.get("token", ""))
    elif event_type == "mcp_tool":
        return MCPToolEvent(
            tool=obj.get("tool", ""),
            status=obj.get("status", ""),
            result=obj.get("result"),
            error=obj.get("error", ""),
        )
    elif event_type == "error":
        return ErrorEvent(error=obj.get("error", ""))
    elif event_type == "done":
        return DoneEvent()
    else:
        return UnknownEvent(type=event_type, raw=obj)


def iter_sse_events(response_iter: Iterator[bytes]) -> Generator[SSEEvent, None, None]:
    """
    Parse an iterable of raw bytes chunks from an SSE HTTP response into
    typed :data:`SSEEvent` objects.

    Usage with ``httpx``::

        with client.stream("POST", url, ...) as response:
            for event in iter_sse_events(response.iter_bytes()):
                if isinstance(event, TokenEvent):
                    print(event.token, end="", flush=True)

    The generator stops naturally when the response stream is exhausted or
    when a :class:`DoneEvent` or :class:`ErrorEvent` is encountered.
    """
    buffer = ""
    for chunk in response_iter:
        buffer += chunk.decode(errors="replace")
        while "\n" in buffer:
            line, buffer = buffer.split("\n", 1)
            line = line.rstrip("\r")
            if not line:
                continue
            if line.startswith("data: "):
                data = line[6:]
                event = _parse_event(data)
                yield event
                if isinstance(event, (DoneEvent, ErrorEvent)):
                    return
