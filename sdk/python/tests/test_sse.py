"""SSE parser: event mapping, buffering, and terminal events."""

from __future__ import annotations

from typing import Any, Iterator

from emergent import (
    DoneEvent,
    ErrorEvent,
    MCPToolEvent,
    MetaEvent,
    TokenEvent,
    UnknownEvent,
    iter_sse_events,
)


def chunks(payload: bytes, size: int = 1024) -> Iterator[bytes]:
    for i in range(0, len(payload), size):
        yield payload[i : i + size]


def parse(payload: bytes, size: int = 1024) -> list[Any]:
    return list(iter_sse_events(chunks(payload, size)))


def test_meta_event_fields_are_mapped_from_camel_case() -> None:
    events = parse(
        b'data: {"type":"meta","conversationId":"conv_1","citations":[1],'
        b'"graphObjects":[{"id":"o"}],"graphNeighbors":{"n":1}}\n\n'
    )
    assert isinstance(events[0], MetaEvent)
    assert events[0].conversation_id == "conv_1"
    assert events[0].citations == [1]
    assert events[0].graph_objects == [{"id": "o"}]
    assert events[0].graph_neighbors == {"n": 1}


def test_token_events_accumulate_across_chunks() -> None:
    payload = (
        b'data: {"type":"token","token":"Hello "}\n\ndata: {"type":"token","token":"world"}\n\n'
    )
    events = parse(payload, size=7)
    assert [e.token for e in events] == ["Hello ", "world"]


def test_tool_event_lifecycle() -> None:
    events = parse(
        b'data: {"type":"mcp_tool","tool":"search","status":"completed","result":{"n":2}}\n\n'
    )
    event = events[0]
    assert isinstance(event, MCPToolEvent)
    assert (event.tool, event.status, event.result) == ("search", "completed", {"n": 2})


def test_error_event_terminates_stream() -> None:
    payload = (
        b'data: {"type":"error","error":"boom"}\n\ndata: {"type":"token","token":"ignored"}\n\n'
    )
    events = parse(payload)
    assert len(events) == 1
    assert isinstance(events[0], ErrorEvent)
    assert events[0].error == "boom"


def test_done_event_terminates_stream() -> None:
    payload = b'data: {"type":"done"}\n\ndata: {"type":"token","token":"ignored"}\n\n'
    events = parse(payload)
    assert [type(e) for e in events] == [DoneEvent]


def test_unknown_type_is_preserved() -> None:
    events = parse(b'data: {"type":"future","x":1}\n\n')
    assert isinstance(events[0], UnknownEvent)
    assert events[0].type == "future"
    assert events[0].raw == {"type": "future", "x": 1}


def test_malformed_json_becomes_parse_error() -> None:
    events = parse(b"data: {not json}\n\n")
    assert isinstance(events[0], UnknownEvent)
    assert events[0].type == "parse_error"


def test_non_data_lines_are_ignored() -> None:
    payload = b': comment\nevent: message\n\ndata: {"type":"done"}\n\n'
    events = parse(payload)
    assert [type(e) for e in events] == [DoneEvent]


def test_crlf_line_endings_are_accepted() -> None:
    events = parse(b'data: {"type":"token","token":"x"}\r\n\r\n')
    assert isinstance(events[0], TokenEvent)
    assert events[0].token == "x"


def test_partial_trailing_line_is_not_emitted() -> None:
    # No trailing newline: the parser must not yield a half-written event.
    events: list[Any] = parse(b'data: {"type":"token","token":"x"}')
    assert events == []
