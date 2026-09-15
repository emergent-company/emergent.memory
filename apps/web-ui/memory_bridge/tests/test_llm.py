"""Unit tests for the Memory bridge LLM (MemoryLLM) — no iOS app or LiveKit needed.

Simulates conversation turns through the full LLM path (message extraction +
streaming) using a fake chat client, so the agent "brain" can be verified
without any device.
"""
import asyncio

from livekit.agents.llm import ChatContext

from memory_bridge.llm import MemoryLLM, _content_to_text, _latest_user_text


class _FakeChatClient:
    """Stand-in for MemoryChatClient: streams canned SSE event dicts."""

    def __init__(self, events):
        self._events = events
        self.last_message = None
        self.last_agent = None

    async def stream(self, message, agent_definition_id, conversation_id=None):
        self.last_message = message
        self.last_agent = agent_definition_id
        for event in self._events:
            yield event


def _ctx(*messages):
    """Build a ChatContext from (role, content) pairs."""
    ctx = ChatContext()
    for role, content in messages:
        ctx.add_message(role=role, content=content)
    return ctx


def test_latest_user_text_picks_latest():
    ctx = _ctx(
        ("system", "You are a helper"),
        ("user", "Hello"),
        ("assistant", "Hi!"),
        ("user", "How are you?"),
    )
    assert _latest_user_text(ctx) == "How are you?"


def test_latest_user_text_empty():
    assert _latest_user_text(_ctx(("system", "prompt"))) == ""


def test_content_to_text():
    assert _content_to_text("plain") == "plain"
    assert _content_to_text([{"type": "text", "text": "hi"}]) == "hi"
    assert _content_to_text(["a", "b"]) == "a b"
    assert _content_to_text([{"type": "image", "url": "x"}]) == ""


def test_full_stream():
    async def run():
        events = [
            {"type": "meta", "conversationId": "conv-test-123"},
            {"type": "token", "token": "Hello"},
            {"type": "token", "token": " there!"},
            {"type": "done"},
        ]
        fake = _FakeChatClient(events)
        llm = MemoryLLM(chat_client=fake, agent_definition_id="agent-abc")
        ctx = _ctx(("user", "Hello there"))

        collected = []
        async with llm.chat(chat_ctx=ctx) as stream:
            async for chunk in stream:
                collected.append(getattr(chunk.delta, "content", "") or "")

        assert fake.last_message == "Hello there"
        assert "".join(collected) == "Hello there!"
        assert llm.conversation_id == "conv-test-123"

    asyncio.run(run())


if __name__ == "__main__":
    # Standalone runner — no pytest dependency. Each test is a plain function
    # that asserts directly; any failure raises and stops the run.
    test_latest_user_text_picks_latest()
    test_latest_user_text_empty()
    test_content_to_text()
    test_full_stream()
    print("ALL TESTS PASSED")
