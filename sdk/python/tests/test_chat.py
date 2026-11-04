"""Chat sub-client: SSE streaming end-to-end through the transport."""

from __future__ import annotations

import httpx
import pytest

from emergent import DoneEvent, ErrorEvent, MetaEvent, TokenEvent

SSE_BODY = (
    b'data: {"type":"meta","conversationId":"conv_1"}\n\n'
    b'data: {"type":"token","token":"Hello "}\n\n'
    b'data: {"type":"token","token":"world"}\n\n'
    b'data: {"type":"done"}\n\n'
)


def stream_response(body: bytes = SSE_BODY) -> httpx.Response:
    return httpx.Response(200, content=body, headers={"content-type": "text/event-stream"})


def test_stream_yields_typed_events(make_client) -> None:
    client, recorder = make_client([stream_response()])
    events = list(client.chat.stream("hi"))
    assert [type(e) for e in events] == [MetaEvent, TokenEvent, TokenEvent, DoneEvent]
    assert events[0].conversation_id == "conv_1"
    assert recorder.body() == {"message": "hi"}


def test_stream_passes_conversation_id(make_client) -> None:
    client, recorder = make_client([stream_response()])
    list(client.chat.stream("hi", conversation_id="conv_9"))
    assert recorder.body() == {"message": "hi", "conversationId": "conv_9"}


def test_ask_uses_project_path_when_context_is_set(make_client) -> None:
    client, recorder = make_client([stream_response()], project_id="proj_1")
    list(client.chat.ask("hi"))
    assert str(recorder.last.url) == "https://api.test/api/projects/proj_1/ask"


def test_ask_falls_back_to_unscoped_path(make_client) -> None:
    client, recorder = make_client([stream_response()])
    list(client.chat.ask("hi"))
    assert str(recorder.last.url) == "https://api.test/api/ask"


def test_ask_collect_joins_tokens(make_client) -> None:
    client, _ = make_client([stream_response()])
    assert client.chat.ask_collect("hi") == "Hello world"


def test_query_requires_a_project(make_client) -> None:
    client, recorder = make_client()
    with pytest.raises(ValueError, match="project_id"):
        list(client.chat.query("hi"))
    assert recorder.requests == []


def test_stream_error_event_is_surfaced(make_client) -> None:
    body = b'data: {"type":"error","error":"model unavailable"}\n\n'
    client, _ = make_client([stream_response(body)])
    events = list(client.chat.stream("hi"))
    assert events == [ErrorEvent(error="model unavailable")]


def test_stream_http_error_raises_api_error(make_client) -> None:
    from emergent import APIError

    client, _ = make_client(
        [httpx.Response(500, json={"error": {"message": "boom", "code": "INTERNAL"}})]
    )
    with pytest.raises(APIError) as excinfo:
        list(client.chat.stream("hi"))
    assert excinfo.value.status_code == 500
    assert excinfo.value.code == "INTERNAL"
