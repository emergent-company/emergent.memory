"""MemoryLLM — a livekit-agents LLM that streams from memory's chat loop.

The AgentSession uses this as its ``llm``, so all the turn-taking / barge-in
machinery works unchanged while the "brain" is memory's chat endpoint.

Rich chat events (``mcp_tool`` -> tool chips, ``thinking``, ``approval``,
``ask_user`` -> question cards) are forwarded onto the iOS ``lk.chat.events``
stream via an injected async event sink (wired by the worker once the room
exists). The sink receives one JSON-ready payload dict per client event, in
memory's original stream order.
"""

import inspect
import logging
from collections.abc import Awaitable, Callable

from livekit.agents import llm
from livekit.agents.llm import ChatChunk, ChoiceDelta, LLMStream
from livekit.agents.types import DEFAULT_API_CONNECT_OPTIONS

from . import trace
from .chat_events import ChatEventMapper
from .memory_chat import MemoryChatClient

logger = logging.getLogger("memory.bridge.llm")

# An async (or sync) callable that publishes one rich chat event payload to the
# client over LiveKit. ``None`` disables forwarding (unit tests / plain use).
ChatEventSink = Callable[[dict], Awaitable[None] | None]


class MemoryLLM(llm.LLM):
    def __init__(
        self,
        chat_client: MemoryChatClient,
        agent_definition_id: str,
        conversation_id: str | None = None,
    ):
        super().__init__()
        self._chat = chat_client
        self._agent_def_id = agent_definition_id
        # Continue the browser's conversation when provided, else a fresh one is
        # created by memory and set after the first turn.
        self.conversation_id: str | None = conversation_id or None
        # Wired by the worker once the LiveKit room is available. Read lazily on
        # every chat() call, so attaching it after session.start() is fine.
        self.chat_event_sink: ChatEventSink | None = None
        # Shared across streams/turns of this LLM so tool_call ids never collide
        # (memory emits no tool-call id of its own).
        self._chat_event_seq = 0

    def _next_chat_event_id(self) -> str:
        self._chat_event_seq += 1
        return f"t{self._chat_event_seq}"

    @property
    def model(self) -> str:
        return "memory-chat"

    @property
    def provider(self) -> str:
        return "memory"

    def chat(self, *, chat_ctx, tools=None, conn_options=DEFAULT_API_CONNECT_OPTIONS, parallel_tool_calls=None, tool_choice=None, extra_kwargs=None) -> LLMStream:
        return MemoryLLMStream(
            self,
            chat_ctx=chat_ctx,
            tools=tools or [],
            conn_options=conn_options,
            chat_client=self._chat,
            agent_def_id=self._agent_def_id,
            conversation_holder=self,
            chat_event_sink=self.chat_event_sink,
            event_id_source=self._next_chat_event_id,
        )


class MemoryLLMStream(LLMStream):
    def __init__(self, llm, *, chat_ctx, tools, conn_options, chat_client, agent_def_id, conversation_holder, chat_event_sink=None, event_id_source=None):
        super().__init__(llm, chat_ctx=chat_ctx, tools=tools, conn_options=conn_options)
        self._chat = chat_client
        self._agent_def_id = agent_def_id
        self._holder = conversation_holder
        self._message = _latest_user_text(chat_ctx)
        self._chat_event_sink = chat_event_sink
        self._event_id_source = event_id_source

    async def _run(self) -> None:
        request_id = f"mem-{id(self)}"
        trace.trace(trace.get_room(), "llm_stream_start")
        # Per-turn rich-event state (tool ids, pending ask_user input). Fresh on
        # every memory stream so ids restart per memory turn.
        mapper = ChatEventMapper(id_source=self._event_id_source)
        try:
            async for event in self._chat.stream(
                self._message, self._agent_def_id, self._holder.conversation_id
            ):
                kind = event.get("type")
                if kind == "meta":
                    cid = event.get("conversationId")
                    if cid and not self._holder.conversation_id:
                        self._holder.conversation_id = cid
                elif kind == "token":
                    self._event_ch.send_nowait(
                        ChatChunk(id=request_id, delta=ChoiceDelta(content=event.get("token")))
                    )
                elif kind == "error":
                    raise RuntimeError(event.get("error") or "memory chat error")
                elif kind == "done":
                    break
                elif kind in ("mcp_tool", "thinking", "approval"):
                    # Rich events memory streams for tool chips / thinking
                    # blocks / approval + question cards. Forwarded verbatim in
                    # stream order; a publish failure must not kill the turn.
                    for payload in mapper.handle(event):
                        await self._emit_chat_event(payload)
                else:
                    logger.debug("memory stream: ignoring unknown event type %r", kind)
        except Exception as e:
            trace.trace(trace.get_room(), "llm_error", error=str(e))
            raise
        else:
            trace.trace(trace.get_room(), "llm_done")
        finally:
            self._event_ch.close()

    async def _emit_chat_event(self, payload: dict) -> None:
        sink = self._chat_event_sink
        if sink is None:
            return
        try:
            result = sink(payload)
            if inspect.isawaitable(result):
                await result
        except Exception:
            logger.exception("failed to forward rich chat event: %s", payload.get("type"))


def _latest_user_text(chat_ctx) -> str:
    for m in reversed(chat_ctx.messages() or []):
        if getattr(m, "role", None) == "user":
            return _content_to_text(getattr(m, "content", ""))
    return ""


def _content_to_text(content) -> str:
    if isinstance(content, str):
        return content
    if isinstance(content, list):
        parts = []
        for item in content:
            if isinstance(item, str):
                parts.append(item)
            elif isinstance(item, dict) and item.get("type") == "text":
                parts.append(item.get("text", ""))
        return " ".join(p for p in parts if p)
    return ""
