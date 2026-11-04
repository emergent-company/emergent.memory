"""Transport layer: headers, parameters, status handling, and streaming."""

from __future__ import annotations

from typing import Any, Dict, List

import httpx
import pytest

from emergent import APIError, APIKeyProvider
from emergent._base import BaseClient
from emergent.sse import DoneEvent, TokenEvent, iter_sse_events


class DummyClient(BaseClient):
    """Minimal concrete subclass exposing the protected helpers."""


def make_base(
    responses: List[httpx.Response],
    *,
    org_id: str = "",
    project_id: str = "",
) -> tuple[DummyClient, List[httpx.Request]]:
    seen: List[httpx.Request] = []

    def handler(request: httpx.Request) -> httpx.Response:
        seen.append(request)
        if not responses:
            raise AssertionError(f"unexpected request: {request.url}")
        return responses.pop(0)

    http = httpx.Client(transport=httpx.MockTransport(handler))
    base = DummyClient(http, "https://api.test", APIKeyProvider("key"), org_id, project_id)
    return base, seen


class TestHeaders:
    def test_injects_auth_and_context_headers(self) -> None:
        base, seen = make_base([httpx.Response(200, json={})], org_id="org_1", project_id="proj_1")
        base._get("/api/x")
        headers = seen[0].headers
        assert headers["x-api-key"] == "key"
        assert headers["x-org-id"] == "org_1"
        assert headers["x-project-id"] == "proj_1"

    def test_omits_empty_context_headers(self) -> None:
        base, seen = make_base([httpx.Response(200, json={})])
        base._get("/api/x")
        assert "x-org-id" not in seen[0].headers
        assert "x-project-id" not in seen[0].headers

    def test_set_context_updates_headers(self) -> None:
        base, seen = make_base([httpx.Response(200, json={}), httpx.Response(200, json={})])
        base.set_context("org_2", "proj_2")
        base._get("/api/x")
        assert seen[0].headers["x-org-id"] == "org_2"
        assert seen[0].headers["x-project-id"] == "proj_2"


class TestParameters:
    def test_none_parameters_are_dropped(self) -> None:
        base, seen = make_base([httpx.Response(200, json={})])
        base._get("/api/x", params={"a": 1, "b": None, "c": ""})
        query = seen[0].url.params
        assert query["a"] == "1"
        assert "b" not in query
        assert query["c"] == ""


class TestStatusHandling:
    def test_error_body_is_parsed(self) -> None:
        base, _ = make_base(
            [
                httpx.Response(
                    400,
                    json={
                        "error": {"message": "bad thing", "code": "INVALID", "details": {"f": 1}}
                    },
                )
            ]
        )
        with pytest.raises(APIError) as excinfo:
            base._get("/api/x")
        err = excinfo.value
        assert err.status_code == 400
        assert err.code == "INVALID"
        assert err.message == "bad thing"
        assert err.details == {"f": 1}
        assert err.is_bad_request
        assert "[400] INVALID: bad thing" in str(err)

    def test_plain_text_error_body_is_used_as_message(self) -> None:
        base, _ = make_base([httpx.Response(502, content=b"upstream down")])
        with pytest.raises(APIError) as excinfo:
            base._get("/api/x")
        assert excinfo.value.status_code == 502
        assert excinfo.value.message == "upstream down"
        assert excinfo.value.code == ""

    def test_not_found_helper(self) -> None:
        base, _ = make_base([httpx.Response(404, json={})])
        with pytest.raises(APIError) as excinfo:
            base._get("/api/x")
        assert excinfo.value.is_not_found


class TestEmptyBodies:
    def test_post_204_returns_none(self) -> None:
        base, _ = make_base([httpx.Response(204)])
        assert base._post("/api/x", json={"a": 1}) is None

    @pytest.mark.parametrize("method", ["_patch", "_put"])
    def test_patch_and_put_without_body_return_none(self, method: str) -> None:
        base, _ = make_base([httpx.Response(200, content=b"")])
        assert getattr(base, method)("/api/x", json={}) is None

    def test_delete_returns_none(self) -> None:
        base, _ = make_base([httpx.Response(204)])
        base._delete("/api/x")


class TestStreaming:
    def test_stream_sets_accept_header_and_parses_events(self) -> None:
        body = b'data: {"type":"token","token":"hi"}\n\ndata: {"type":"done"}\n\n'
        base, seen = make_base([httpx.Response(200, content=body)])
        with base._stream("/api/stream", json={"message": "x"}) as chunks:
            events = list(iter_sse_events(chunks))
        assert [type(e) for e in events] == [TokenEvent, DoneEvent]
        assert seen[0].headers["accept"] == "text/event-stream"
        assert seen[0].method == "POST"

    def test_stream_raises_on_error_status(self) -> None:
        base, _ = make_base(
            [httpx.Response(401, json={"error": {"message": "nope", "code": "UNAUTHORIZED"}})]
        )
        with pytest.raises(APIError) as excinfo:
            with base._stream("/api/stream"):
                pass
        assert excinfo.value.is_unauthorized


class TestJSONDecoding:
    def test_post_returns_decoded_json(self) -> None:
        base, seen = make_base([httpx.Response(200, json={"ok": True})])
        data: Dict[str, Any] = base._post("/api/x", json={"name": "n"})
        assert data == {"ok": True}
        assert seen[0].method == "POST"
