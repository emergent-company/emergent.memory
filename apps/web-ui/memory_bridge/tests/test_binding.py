"""Unit tests for the per-room voice binding fetch (multi-user-voice).

Verifies fetch_binding against the gateway's internal endpoint using httpx
MockTransport — no network touched. Plain functions + standalone __main__
(pytest optional), matching test_memory_chat.py.
"""
import asyncio

import httpx

from memory_bridge.binding import VoiceBinding, VoiceBindingError, fetch_binding

BASE = "http://127.0.0.1:8095"
KEY = "worker-key-1"


def _transport(handler):
    return httpx.MockTransport(handler)


def test_fetch_success_parses_binding():
    async def run():
        seen = {}

        def handler(request: httpx.Request) -> httpx.Response:
            seen["url"] = str(request.url)
            seen["key"] = request.headers.get("X-Worker-Key")
            return httpx.Response(
                200,
                json={
                    "project_id": "p1",
                    "org_id": "org-1",
                    "agent_definition_id": "a1",
                    "language": "pl",
                    "token": "emt-x",
                },
            )

        b = await fetch_binding("room-1", BASE, KEY, transport=_transport(handler))
        assert b == VoiceBinding(
            project_id="p1",
            org_id="org-1",
            agent_definition_id="a1",
            language="pl",
            token="emt-x",
        )
        assert "/internal/voice-binding" in seen["url"]
        assert "room=room-1" in seen["url"]
        assert seen["key"] == KEY

    asyncio.run(run())


def test_fetch_missing_optional_fields_default_empty():
    async def run():
        def handler(request: httpx.Request) -> httpx.Response:
            return httpx.Response(200, json={"project_id": "p1", "token": "emt-x"})

        b = await fetch_binding("r", BASE, KEY, transport=_transport(handler))
        assert b.org_id == ""
        assert b.agent_definition_id == ""
        assert b.language == ""

    asyncio.run(run())


def test_fetch_401_raises():
    async def run():
        def handler(request):
            return httpx.Response(401, json={"error": "unauthorized"})

        try:
            await fetch_binding("r", BASE, KEY, transport=_transport(handler))
            raise AssertionError("expected VoiceBindingError")
        except VoiceBindingError as e:
            assert "401" in str(e)

    asyncio.run(run())


def test_fetch_404_raises():
    async def run():
        def handler(request):
            return httpx.Response(404, json={"error": "unknown or expired room"})

        try:
            await fetch_binding("r", BASE, KEY, transport=_transport(handler))
            raise AssertionError("expected VoiceBindingError")
        except VoiceBindingError as e:
            assert "404" in str(e)

    asyncio.run(run())


def test_fetch_malformed_json_raises():
    async def run():
        def handler(request):
            return httpx.Response(200, content=b"not json", headers={"content-type": "text/plain"})

        try:
            await fetch_binding("r", BASE, KEY, transport=_transport(handler))
            raise AssertionError("expected VoiceBindingError")
        except VoiceBindingError as e:
            assert "malformed" in str(e)

    asyncio.run(run())


def test_fetch_missing_token_raises():
    async def run():
        def handler(request):
            return httpx.Response(200, json={"project_id": "p1", "agent_definition_id": "a1"})

        try:
            await fetch_binding("r", BASE, KEY, transport=_transport(handler))
            raise AssertionError("expected VoiceBindingError")
        except VoiceBindingError as e:
            assert "token" in str(e)

    asyncio.run(run())


if __name__ == "__main__":
    test_fetch_success_parses_binding()
    test_fetch_missing_optional_fields_default_empty()
    test_fetch_401_raises()
    test_fetch_404_raises()
    test_fetch_malformed_json_raises()
    test_fetch_missing_token_raises()
    print("ALL TESTS PASSED")
