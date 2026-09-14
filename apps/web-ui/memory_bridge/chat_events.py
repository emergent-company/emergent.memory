"""Mapping between memory's SSE chat stream and the iOS ``lk.chat.*`` JSON contract.

Pure module — no LiveKit / network dependency, so it is unit-testable standalone:

- :class:`ChatEventMapper.handle` turns one memory SSE event dict (``mcp_tool`` /
  ``thinking`` / ``approval``) into zero or more client event dicts — the JSON
  payloads the worker streams over the ``lk.chat.events`` text-stream topic.
- :func:`parse_decision` decodes one client decision message received on the
  ``lk.chat.decision`` topic (approve/reject/answer), returning ``None`` for
  malformed input.
- :func:`is_interrupt_message` decides whether a ``lk.chat.interrupt`` payload
  asks the worker to cancel the current generation.

``meta``/``token``/``error``/``done`` memory events are handled by the LLM stream
itself and never reach this module.

The event payloads follow the iOS contract exactly — field names must NOT be
renamed:

Worker -> client (``lk.chat.events``, one JSON object per message):
    ``tool_call``   {type, id, tool, arguments}          (arguments: json string)
    ``tool_result`` {type, id, tool, result, isError}    (id == its tool_call id)
    ``thinking``    {type, delta}
    ``approval``    {type, questionId, tool, arguments}
    ``question``    {type, questionId, question, interactionType, options,
                     placeholder, maxLength}

Memory's own event shapes (see apps/server/pkg/sse/events.go in the memory repo):

    ``mcp_tool``   {type, tool, status: started|completed|error, result, error?}
                   ask_user tool: started/running carry the question input in
                   ``result``; the terminal event carries ``{question_id}``.
    ``thinking``   {type, id, role, text, done}  (text is the incremental part)
    ``approval``   {type, tool, input, questionId}
"""

from __future__ import annotations

import json
import logging
from collections.abc import Callable
from typing import Any

logger = logging.getLogger("memory.bridge.chat_events")

# LiveKit text-stream topics (worker <-> iOS client).
TOPIC_EVENTS = "lk.chat.events"        # worker -> client: rich chat events
TOPIC_DECISION = "lk.chat.decision"    # client -> worker: approval/question answers
TOPIC_INTERRUPT = "lk.chat.interrupt"  # client -> worker: cancel current generation

# ask_user interaction_type values memory emits -> the iOS contract's values.
# memory: buttons | select | multi_select | text  (see ask_user_tool.go)
# ios:    buttons | multi_select | free_text
_INTERACTION_TYPE_MAP = {
    "": "buttons",
    "buttons": "buttons",
    "select": "buttons",  # single-choice dropdown -> same interaction as buttons
    "multi_select": "multi_select",
    "text": "free_text",
}

# Memory SSE statuses that open a tool call (everything else closes it).
_START_STATUSES = frozenset({"started", "running"})


class _SequenceIdSource:
    """Per-mapper incrementing id source (``t1``, ``t2``, ...)."""

    def __init__(self) -> None:
        self._n = 0

    def __call__(self) -> str:
        self._n += 1
        return f"t{self._n}"


def _first_text(event: dict, *keys: str) -> str | None:
    """First non-empty string among ``keys`` in ``event`` (normalized to str)."""
    for key in keys:
        value = event.get(key)
        if value is None:
            continue
        if isinstance(value, str):
            if value:
                return value
        else:
            text = str(value)
            if text:
                return text
    return None


def _decode_result(result: Any) -> Any:
    """SSE ``result`` may already be an object or a JSON-encoded string."""
    if isinstance(result, str):
        try:
            return json.loads(result)
        except (TypeError, ValueError):
            return result
    return result


def _json_string(value: Any) -> str:
    """Render a tool/approval payload as the contract's JSON-string field."""
    if isinstance(value, str):
        return value
    if value is None:
        return "{}"
    return json.dumps(value, ensure_ascii=False, default=str)


def _result_text(result: Any) -> str:
    """The ``tool_result.result`` text: pass strings through, pretty-render JSON."""
    if isinstance(result, str):
        return result
    if result is None:
        return ""
    return json.dumps(result, ensure_ascii=False, default=str)


def _as_int(value: Any) -> int:
    try:
        if isinstance(value, bool):
            return 0
        return int(value)
    except (TypeError, ValueError):
        return 0


def _question_option(option: Any) -> dict | None:
    if not isinstance(option, dict):
        return None
    label = option.get("label")
    value = option.get("value")
    if not isinstance(label, str) or not isinstance(value, str) or not label or not value:
        return None
    out: dict = {"label": label, "value": value}
    description = option.get("description")
    if isinstance(description, str) and description:
        out["description"] = description
    return out


class ChatEventMapper:
    """Stateful per-turn mapper: memory SSE event dict -> list of client events.

    State kept across events of one memory chat stream:

    - open ``tool_call`` ids per tool name, so each ``tool_result`` reuses the id
      of its ``tool_call`` (memory emits no tool-call id of its own);
    - the pending ``ask_user`` input awaiting that tool's ``question_id``.
    """

    def __init__(self, id_source: Callable[[], str] | None = None) -> None:
        self._id_source = id_source or _SequenceIdSource()
        self._open: dict[str, list[str]] = {}  # tool name -> stack of open ids
        self._ask_input: dict | None = None    # pending ask_user question input

    def reset(self) -> None:
        """Clear per-turn state (new memory stream)."""
        self._open.clear()
        self._ask_input = None

    def handle(self, event: dict) -> list[dict]:
        kind = event.get("type")
        if kind == "mcp_tool":
            return self._handle_tool(event)
        if kind == "thinking":
            delta = _first_text(event, "delta", "text", "content")
            if delta is None:
                logger.debug("thinking event without text payload: %s", event)
                return []
            return [{"type": "thinking", "delta": delta}]
        if kind == "approval":
            return [self._handle_approval(event)]
        logger.debug("chat mapper: ignoring memory event type %r", kind)
        return []

    # --- tool events -------------------------------------------------------

    def _handle_tool(self, event: dict) -> list[dict]:
        tool = event.get("tool")
        if tool == "ask_user":
            return self._handle_ask_user(event)
        if not isinstance(tool, str) or not tool:
            logger.debug("mcp_tool event without a tool name: %s", event)
            return []
        status = event.get("status") or "completed"
        if status in _START_STATUSES:
            tool_id = self._id_source()
            self._open.setdefault(tool, []).append(tool_id)
            return [
                {
                    "type": "tool_call",
                    "id": tool_id,
                    "tool": tool,
                    "arguments": _json_string(_decode_result(event.get("result"))),
                }
            ]
        # Terminal status (completed/error/done/...): correlate with its start.
        tool_id = self._pop_tool_id(tool)
        is_error = status == "error" or bool(event.get("error"))
        error_text = _first_text(event, "error")
        if is_error and error_text is not None:
            result_text = error_text
        else:
            result_text = _result_text(_decode_result(event.get("result")))
        return [
            {
                "type": "tool_result",
                "id": tool_id,
                "tool": tool,
                "result": result_text,
                "isError": is_error,
            }
        ]

    def _pop_tool_id(self, tool: str) -> str:
        """Pop the id of this tool's most recent open call (LIFO — parallel calls
        of the same tool complete in dispatch order). Falls back to a fresh id so
        an orphaned terminal event still yields a well-formed payload."""
        stack = self._open.get(tool)
        if stack:
            return stack.pop()
        return self._id_source()

    # --- ask_user -> question ----------------------------------------------

    def _handle_ask_user(self, event: dict) -> list[dict]:
        """Mirror gateway/sse_markdown.go rewriteChatStream: an ``ask_user``
        mcp_tool is not surfaced as a tool chip — its input is captured on
        started/running and turned into a ``question`` event once the tool's
        terminal event reports its ``question_id``."""
        status = event.get("status") or "completed"
        result = _decode_result(event.get("result"))
        if status in _START_STATUSES:
            if isinstance(result, dict):
                self._ask_input = result
            else:
                self._ask_input = None
            return []
        # Terminal event: carry the question id created by the ask_user tool.
        question_id = result.get("question_id") if isinstance(result, dict) else None
        if isinstance(question_id, str) and question_id and self._ask_input:
            payload = self._build_question(question_id, self._ask_input)
            self._ask_input = None
            return [payload]
        # ask_user finished without pausing (e.g. validation error on bad
        # options) — nothing to render, mirror the gateway's fallback of
        # surfacing the raw event only in the web chat (we have no chip event
        # for it, so we drop it).
        self._ask_input = None
        logger.warning("ask_user completed without a question_id; dropped: %s", event)
        return []

    @staticmethod
    def _build_question(question_id: str, ask_input: dict) -> dict:
        interaction_type = ask_input.get("interaction_type")
        if not isinstance(interaction_type, str):
            interaction_type = ""
        interaction = _INTERACTION_TYPE_MAP.get(interaction_type, "buttons")
        options: list[dict] = []
        raw_options = ask_input.get("options")
        if isinstance(raw_options, list):
            for raw in raw_options:
                option = _question_option(raw)
                if option is not None:
                    options.append(option)
        placeholder = ask_input.get("placeholder")
        question = ask_input.get("question") or ask_input.get("message")
        return {
            "type": "question",
            "questionId": question_id,
            "question": question if isinstance(question, str) else "",
            "interactionType": interaction or "buttons",
            "options": options,
            "placeholder": placeholder if isinstance(placeholder, str) else "",
            "maxLength": _as_int(ask_input.get("max_length")),
        }

    # --- approval ----------------------------------------------------------

    @staticmethod
    def _handle_approval(event: dict) -> dict:
        """``approval`` events carry tool-policy confirmations: questionId + the
        tool + its input. arguments is a JSON string; when the event has no
        clear input field the whole raw event is forwarded as the summary."""
        question_id = event.get("questionId") or event.get("question_id") or ""
        tool = event.get("tool") or ""
        arguments: Any = event.get("input")
        if arguments is None and "arguments" in event:
            arguments = event.get("arguments")
        if arguments is None:
            arguments = event  # no input summary -> forward the raw event
        return {
            "type": "approval",
            "questionId": question_id if isinstance(question_id, str) else str(question_id),
            "tool": tool if isinstance(tool, str) else str(tool),
            "arguments": _json_string(_decode_result(arguments)),
        }


# --- client -> worker parsing ----------------------------------------------

_APPROVAL_ACTIONS = frozenset({"approve", "reject", "cancel"})


def parse_decision(text: str) -> dict | None:
    """Decode one ``lk.chat.decision`` message.

    Returns a canonical dict on success:

    - ``{"type": "approval", "questionId", "action": approve|reject|cancel, "message"}``
    - ``{"type": "question", "questionId", "answer"}``

    Returns ``None`` for anything malformed (non-JSON, wrong shape, missing
    required fields, unknown action/type) — callers must ignore it.
    """
    if not isinstance(text, str) or not text.strip():
        return None
    try:
        payload = json.loads(text)
    except (TypeError, ValueError):
        return None
    if not isinstance(payload, dict):
        return None
    kind = payload.get("type")
    question_id = payload.get("questionId")
    if not isinstance(question_id, str) or not question_id:
        return None
    if kind == "approval":
        action = payload.get("action")
        if action not in _APPROVAL_ACTIONS:
            return None
        message = payload.get("message")
        return {
            "type": "approval",
            "questionId": question_id,
            "action": action,
            "message": message if isinstance(message, str) else "",
        }
    if kind == "question":
        answer = payload.get("answer")
        if not isinstance(answer, str) or not answer:
            return None
        return {"type": "question", "questionId": question_id, "answer": answer}
    return None


def is_interrupt_message(text: str) -> bool:
    """True when an ``lk.chat.interrupt`` payload requests a cancel.

    Per contract: empty text or ``{}`` -> cancel the current generation. Any
    other payload is ignored so a future richer interrupt schema stays safe.
    """
    if not isinstance(text, str):
        return False
    stripped = text.strip()
    if not stripped:
        return True
    try:
        payload = json.loads(stripped)
    except (TypeError, ValueError):
        return False
    return payload == {} or payload is None
