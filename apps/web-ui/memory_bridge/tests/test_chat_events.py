"""Unit tests for the bridge -> iOS chat-event contract (tasks 2.1-2.3).

Covers:
- 2.1 mapping memory SSE events (mcp_tool / thinking / approval) into the
  lk.chat.events JSON payloads (tool_call/tool_result/thinking/approval) and
  forwarding them through a MemoryLLMStream event sink;
- 2.2 synthesizing a `question` event from an ask_user mcp_tool call;
- 2.3 decoding lk.chat.decision messages (and interrupt payloads), ignoring
  malformed input.

Plain functions + standalone __main__ (pytest optional, per test_llm.py).
"""
import asyncio
import json
import logging

from livekit.agents.llm import ChatContext

from memory_bridge.chat_events import (
    ChatEventMapper,
    is_interrupt_message,
    parse_decision,
)
from memory_bridge.llm import MemoryLLM

# Keep expected warn-log noise (dropped ask_user events) out of test output.
logging.getLogger("memory.bridge.chat_events").addHandler(logging.NullHandler())


class _FakeChatClient:
    def __init__(self, events):
        self._events = events

    async def stream(self, message, agent_definition_id, conversation_id=None):
        for event in self._events:
            yield event


def _ctx(*messages):
    ctx = ChatContext()
    for role, content in messages:
        ctx.add_message(role=role, content=content)
    return ctx


# --- 2.1 forward events: tool_call / tool_result / thinking / approval -------

def test_tool_call_then_result_share_id():
    mapper = ChatEventMapper()
    events = [
        {"type": "mcp_tool", "tool": "search_entities", "status": "started",
         "result": {"query": "memory", "limit": 5}},
        {"type": "mcp_tool", "tool": "search_entities", "status": "completed",
         "result": {"hits": 3}},
    ]
    out = [p for ev in events for p in mapper.handle(ev)]
    assert out == [
        {"type": "tool_call", "id": "t1", "tool": "search_entities",
         "arguments": '{"query": "memory", "limit": 5}'},
        {"type": "tool_result", "id": "t1", "tool": "search_entities",
         "result": '{"hits": 3}', "isError": False},
    ]


def test_tool_error_maps_to_error_result():
    mapper = ChatEventMapper()
    out = [p for ev in [
        {"type": "mcp_tool", "tool": "search", "status": "started", "result": {"q": "x"}},
        {"type": "mcp_tool", "tool": "search", "status": "error", "result": None,
         "error": "rate limited"},
    ] for p in mapper.handle(ev)]
    assert out[1]["type"] == "tool_result"
    assert out[1]["isError"] is True
    assert out[1]["result"] == "rate limited"
    assert out[1]["id"] == out[0]["id"]


def test_parallel_same_tool_lifo_correlation():
    mapper = ChatEventMapper()
    out = [p for ev in [
        {"type": "mcp_tool", "tool": "web_search", "status": "started", "result": {"q": "a"}},
        {"type": "mcp_tool", "tool": "web_search", "status": "started", "result": {"q": "b"}},
        # second call completes first
        {"type": "mcp_tool", "tool": "web_search", "status": "completed", "result": {"q": "b"}},
        {"type": "mcp_tool", "tool": "web_search", "status": "completed", "result": {"q": "a"}},
    ] for p in mapper.handle(ev)]
    starts = [p for p in out if p["type"] == "tool_call"]
    ends = [p for p in out if p["type"] == "tool_result"]
    assert [p["id"] for p in starts] == ["t1", "t2"]
    # LIFO: the last started call is the first one to finish.
    assert ends[0]["id"] == "t2"
    assert ends[1]["id"] == "t1"


def test_thinking_normalized_to_delta():
    mapper = ChatEventMapper()
    out = mapper.handle({"type": "thinking", "id": "3", "role": "reasoning",
                         "text": "Let me reason", "done": True})
    assert out == [{"type": "thinking", "delta": "Let me reason"}]
    # fallback field names accepted
    assert mapper.handle({"type": "thinking", "delta": "d"})[0]["delta"] == "d"
    assert mapper.handle({"type": "thinking", "content": "c"})[0]["delta"] == "c"
    # no text payload at all -> dropped
    assert mapper.handle({"type": "thinking", "role": "operator"}) == []


def test_approval_mapped_with_input_summary():
    mapper = ChatEventMapper()
    out = mapper.handle({
        "type": "approval", "tool": "send_email", "questionId": "q-9",
        "input": {"to": "a@b.c", "subject": "hi"},
    })
    assert out == [{
        "type": "approval", "questionId": "q-9", "tool": "send_email",
        "arguments": '{"to": "a@b.c", "subject": "hi"}',
    }]
    # no input field -> forward the whole raw event as the arguments summary
    raw = {"type": "approval", "tool": "run_shell", "questionId": "q-2"}
    out = mapper.handle(dict(raw))
    assert out[0]["questionId"] == "q-2"
    assert json.loads(out[0]["arguments"])["tool"] == "run_shell"


def test_unknown_and_meta_events_ignored():
    mapper = ChatEventMapper()
    assert mapper.handle({"type": "meta", "conversationId": "c"}) == []
    assert mapper.handle({"type": "token", "token": "hi"}) == []
    assert mapper.handle({"type": "error", "error": "boom"}) == []
    assert mapper.handle({"type": "bogus"}) == []


def test_forwarding_through_llm_stream_sink():
    """2.1 end-to-end: a MemoryLLMStream with an event sink publishes the rich
    events in memory's original order alongside the token stream."""
    events = [
        {"type": "meta", "conversationId": "conv-1"},
        {"type": "token", "token": "Let me "},
        {"type": "thinking", "id": "1", "role": "reasoning", "text": "search first", "done": True},
        {"type": "mcp_tool", "tool": "search_entities", "status": "started",
         "result": {"query": "memory"}},
        {"type": "mcp_tool", "tool": "search_entities", "status": "completed",
         "result": {"found": True}},
        {"type": "token", "token": "done."},
        {"type": "approval", "tool": "send_email", "questionId": "q-1",
         "input": {"to": "x@y.z"}},
        {"type": "done"},
    ]

    async def run():
        fake = _FakeChatClient(events)
        llm = MemoryLLM(chat_client=fake, agent_definition_id="agent-1")
        sink_payloads = []

        async def sink(payload):
            sink_payloads.append(payload)

        llm.chat_event_sink = sink
        ctx = _ctx(("user", "please look that up"))
        chunks = []
        async with llm.chat(chat_ctx=ctx) as stream:
            async for chunk in stream:
                chunks.append(getattr(chunk.delta, "content", "") or "")

        assert "".join(chunks) == "Let me done."
        kinds = [p["type"] for p in sink_payloads]
        assert kinds == ["thinking", "tool_call", "tool_result", "approval"]
        assert sink_payloads[1] == {"type": "tool_call", "id": "t1",
                                    "tool": "search_entities",
                                    "arguments": '{"query": "memory"}'}
        assert sink_payloads[2] == {"type": "tool_result", "id": "t1",
                                    "tool": "search_entities",
                                    "result": '{"found": true}', "isError": False}
        assert sink_payloads[3] == {"type": "approval", "questionId": "q-1",
                                    "tool": "send_email",
                                    "arguments": '{"to": "x@y.z"}'}
        assert llm.conversation_id == "conv-1"

    asyncio.run(run())


# --- 2.2 ask_user -> question -----------------------------------------------

def test_ask_user_synthesizes_question():
    mapper = ChatEventMapper()
    events = [
        {"type": "mcp_tool", "tool": "ask_user", "status": "started", "result": {
            "question": "Pick a color?",
            "options": [{"label": "Red", "value": "red", "description": "warm"}],
            "interaction_type": "buttons", "placeholder": "", "max_length": 0,
        }},
        {"type": "mcp_tool", "tool": "ask_user", "status": "completed",
         "result": {"question_id": "q-42", "status": "pausing"}},
    ]
    out = [p for ev in events for p in mapper.handle(ev)]
    assert out == [{
        "type": "question", "questionId": "q-42", "question": "Pick a color?",
        "interactionType": "buttons",
        "options": [{"label": "Red", "value": "red", "description": "warm"}],
        "placeholder": "", "maxLength": 0,
    }]
    # no tool_call/tool_result chips are emitted for ask_user


def test_ask_user_interaction_type_normalization():
    cases = [
        ("text", "free_text"),
        ("multi_select", "multi_select"),
        ("select", "buttons"),
        ("buttons", "buttons"),
        ("", "buttons"),          # default when empty
        ("weird", "buttons"),     # unknown -> buttons fallback
    ]
    for raw, expected in cases:
        mapper = ChatEventMapper()
        events = [
            {"type": "mcp_tool", "tool": "ask_user", "status": "started",
             "result": {"question": "Q", "interaction_type": raw}},
            {"type": "mcp_tool", "tool": "ask_user", "status": "completed",
             "result": {"question_id": "q-1"}},
        ]
        out = [p for ev in events for p in mapper.handle(ev)]
        assert out[0]["interactionType"] == expected, raw
        assert out[0]["maxLength"] == 0


def test_ask_user_message_alias_and_free_text_fields():
    mapper = ChatEventMapper()
    events = [
        {"type": "mcp_tool", "tool": "ask_user", "status": "running",
         "result": {"message": "Type your name", "interaction_type": "text",
                    "placeholder": "Jane Doe", "max_length": 120}},
        {"type": "mcp_tool", "tool": "ask_user", "status": "completed",
         "result": {"question_id": "q-7"}},
    ]
    out = [p for ev in events for p in mapper.handle(ev)]
    assert out == [{
        "type": "question", "questionId": "q-7", "question": "Type your name",
        "interactionType": "free_text", "options": [],
        "placeholder": "Jane Doe", "maxLength": 120,
    }]


def test_ask_user_without_question_id_dropped():
    mapper = ChatEventMapper()
    events = [
        {"type": "mcp_tool", "tool": "ask_user", "status": "started",
         "result": {"question": "Q", "options": []}},
        {"type": "mcp_tool", "tool": "ask_user", "status": "completed",
         "result": {"error": "interaction_type requires options"}},
    ]
    out = [p for ev in events for p in mapper.handle(ev)]
    assert out == []


# --- 2.3 decision + interrupt decoding ---------------------------------------

def test_parse_decision_approval():
    d = parse_decision('{"type":"approval","questionId":"q-1","action":"approve"}')
    assert d == {"type": "approval", "questionId": "q-1", "action": "approve", "message": ""}
    d = parse_decision('{"type":"approval","questionId":"q-1","action":"reject","message":"no"}')
    assert d["action"] == "reject" and d["message"] == "no"
    d = parse_decision('{"type":"approval","questionId":"q-1","action":"cancel"}')
    assert d["action"] == "cancel"


def test_parse_decision_question():
    d = parse_decision('{"type":"question","questionId":"q-2","answer":"red"}')
    assert d == {"type": "question", "questionId": "q-2", "answer": "red"}
    d = parse_decision('{"type":"question","questionId":"q-2","answer":"multi word"}')
    assert d["answer"] == "multi word"


def test_parse_decision_ignores_malformed():
    bad = [
        "", "   ", "not json", "{", "[]", "42", '"str"',
        '{"type":"approval","action":"approve"}',            # no questionId
        '{"type":"approval","questionId":"q","action":"nope"}',  # bad action
        '{"type":"question","questionId":"q"}',               # no answer
        '{"type":"question","questionId":"q","answer":""}',   # empty answer
        '{"type":"question","questionId":"q","answer":5}',    # non-string
        '{"type":"bogus","questionId":"q","action":"approve"}',
        '{"questionId":"q"}',                                  # no type
    ]
    for text in bad:
        assert parse_decision(text) is None, text


def test_parse_decision_extra_fields_ignored():
    d = parse_decision('{"type":"question","questionId":"q","answer":"a","extra":1}')
    assert d == {"type": "question", "questionId": "q", "answer": "a"}


def test_interrupt_message():
    assert is_interrupt_message("") is True
    assert is_interrupt_message("   ") is True
    assert is_interrupt_message("{}") is True
    assert is_interrupt_message("{\"type\":\"interrupt\"}") is False
    assert is_interrupt_message("garbage") is False
    assert is_interrupt_message(None) is False


if __name__ == "__main__":
    test_tool_call_then_result_share_id()
    test_tool_error_maps_to_error_result()
    test_parallel_same_tool_lifo_correlation()
    test_thinking_normalized_to_delta()
    test_approval_mapped_with_input_summary()
    test_unknown_and_meta_events_ignored()
    test_forwarding_through_llm_stream_sink()
    test_ask_user_synthesizes_question()
    test_ask_user_interaction_type_normalization()
    test_ask_user_message_alias_and_free_text_fields()
    test_ask_user_without_question_id_dropped()
    test_parse_decision_approval()
    test_parse_decision_question()
    test_parse_decision_ignores_malformed()
    test_parse_decision_extra_fields_ignored()
    test_interrupt_message()
    print("ALL TESTS PASSED")
