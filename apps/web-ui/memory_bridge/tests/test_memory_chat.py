"""Unit tests for MemoryChatClient.respond / cancel (task 2.4).

Verifies the exact memory endpoints, request method/path/auth and body fields
against gateway/memory.go RespondQuestion / CancelQuestion, using httpx
MockTransport so no network is touched.

Plain functions + standalone __main__ (pytest optional, per test_llm.py).
"""
import asyncio
import json

import httpx

from memory_bridge.memory_chat import MemoryChatClient, MemoryChatError

BASE = "https://memory.test"
TOKEN = "tok-123"
PROJECT = "proj-uuid-1"


def _make_client(handler, project_id=PROJECT):
    transport = httpx.MockTransport(handler)
    return MemoryChatClient(BASE, TOKEN, project_id=project_id, transport=transport)


def _json_handler(record, status=202):
    def handler(request: httpx.Request) -> httpx.Response:
        record.append(request)
        return httpx.Response(status, json={"success": True,
                                            "data": {"id": "q-1",
                                                     "resumeRunId": "run-9",
                                                     "status": "resumed"}})
    return handler


def test_respond_approve_path_and_body():
    async def run():
        record = []
        client = _make_client(_json_handler(record))
        result = await client.respond("q-1", "approve")
        req = record[0]
        assert req.method == "POST"
        assert req.url.path == (
            "/api/projects/proj-uuid-1/agent-questions/q-1/respond"
        )
        assert json.loads(req.content) == {"response": "approve"}
        assert req.headers["Authorization"] == f"Bearer {TOKEN}"
        assert result["data"]["resumeRunId"] == "run-9"
    asyncio.run(run())


def test_respond_reject_with_message():
    async def run():
        record = []
        client = _make_client(_json_handler(record))
        await client.respond("q-1", "reject", message="not now")
        body = json.loads(record[0].content)
        assert body == {"response": "reject", "message": "not now"}
    asyncio.run(run())


def test_respond_question_answer_uses_text_as_response():
    async def run():
        record = []
        client = _make_client(_json_handler(record))
        await client.respond("q-2", "the red one")
        body = json.loads(record[0].content)
        assert body == {"response": "the red one"}
    asyncio.run(run())


def test_cancel_path_and_no_body():
    async def run():
        record = []
        client = _make_client(_json_handler(record))
        await client.cancel("q-3")
        req = record[0]
        assert req.method == "POST"
        assert req.url.path == (
            "/api/projects/proj-uuid-1/agent-questions/q-3/cancel"
        )
        assert req.content in (b"", b"null")
    asyncio.run(run())


def test_ids_are_url_escaped():
    async def run():
        record = []
        client = _make_client(_json_handler(record))
        await client.respond("q/../x", "approve")
        assert "/agent-questions/q%2F..%2Fx/respond" in str(record[0].url)
    asyncio.run(run())


def test_missing_project_id_raises():
    async def run():
        record = []
        client = _make_client(_json_handler(record), project_id="")
        try:
            await client.respond("q-1", "approve")
            raise AssertionError("expected MemoryChatError")
        except MemoryChatError:
            pass
    asyncio.run(run())


def test_non_2xx_raises_memory_chat_error():
    async def run():
        def handler(request):
            return httpx.Response(409, text="question already answered")

        client = _make_client(handler)
        try:
            await client.cancel("q-1")
            raise AssertionError("expected MemoryChatError")
        except MemoryChatError as e:
            assert "409" in str(e)
    asyncio.run(run())


if __name__ == "__main__":
    test_respond_approve_path_and_body()
    test_respond_reject_with_message()
    test_respond_question_answer_uses_text_as_response()
    test_cancel_path_and_no_body()
    test_ids_are_url_escaped()
    test_missing_project_id_raises()
    test_non_2xx_raises_memory_chat_error()
    print("ALL TESTS PASSED")
