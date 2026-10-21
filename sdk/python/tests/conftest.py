"""Shared fixtures for the Emergent Python SDK test suite.

Every test talks to an ``httpx.MockTransport``: no network access, no live
server.  The transport records requests so tests can assert on the URL,
headers, and JSON body that the SDK produced.
"""

from __future__ import annotations

import json
from typing import Any, Callable, Iterable, List, Optional, Sequence

import httpx
import pytest

from emergent import Client, Config

Handler = Callable[[httpx.Request], httpx.Response]


class Recorder:
    """Mock transport that records requests and replays queued responses."""

    def __init__(self, responses: Optional[Iterable[httpx.Response]] = None) -> None:
        self.requests: List[httpx.Request] = []
        self.responses: List[httpx.Response] = list(responses or [])

    def __call__(self, request: httpx.Request) -> httpx.Response:
        self.requests.append(request)
        if not self.responses:
            raise AssertionError(f"unexpected request: {request.method} {request.url}")
        return self.responses.pop(0)

    @property
    def last(self) -> httpx.Request:
        assert self.requests, "no request was made"
        return self.requests[-1]

    def body(self, index: int = -1) -> Any:
        """Decoded JSON body of the request at *index* (default: last)."""
        return json.loads(self.requests[index].content)


@pytest.fixture
def make_client() -> Callable[..., tuple[Client, Recorder]]:
    """Factory returning ``(client, recorder)`` with a mocked transport."""

    def _make(
        responses: Sequence[httpx.Response] = (),
        *,
        handler: Optional[Handler] = None,
        api_key: str = "test-key",
        mode: str = "apikey",
        access_token: str = "",
        org_id: str = "",
        project_id: str = "",
        server_url: str = "https://api.test",
    ) -> tuple[Client, Recorder]:
        recorder = Recorder(responses)
        http = httpx.Client(transport=httpx.MockTransport(handler or recorder))
        client = Client(
            Config(
                server_url=server_url,
                api_key=api_key,
                mode=mode,
                access_token=access_token,
                org_id=org_id,
                project_id=project_id,
                http_client=http,
            )
        )
        return client, recorder

    return _make
