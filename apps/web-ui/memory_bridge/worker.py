"""Bridge worker: AgentServer + entrypoint.

Defined in a module (not `__main__`) so livekit-agents' multiprocessing spawn
can pickle the `entrypoint` by import path.
"""

import asyncio
import contextlib
import json
import logging
import re
import traceback

from livekit.agents import Agent, AgentServer, AgentSession, JobContext, inference
from livekit.agents.voice.room_io import RoomOptions, TextInputEvent, TextInputOptions
from livekit.plugins import deepgram, silero

from . import config, trace
from .binding import VoiceBinding, fetch_binding
from .chat_events import (
    TOPIC_DECISION,
    TOPIC_EVENTS,
    TOPIC_INTERRUPT,
    is_interrupt_message,
    parse_decision,
)
from .llm import MemoryLLM
from .memory_chat import MemoryChatClient, MemoryChatError

logger = logging.getLogger("memory.bridge")

_background_tasks: set[asyncio.Task] = set()


def _track(coro) -> asyncio.Task:
    """create_task with a strong ref so the event loop cannot GC it mid-flight."""
    task = asyncio.create_task(coro)
    _background_tasks.add(task)
    task.add_done_callback(_background_tasks.discard)
    return task


_EXIT_RE = re.compile(
    r"\b(?:" + "|".join(re.escape(k) for k in config.EXIT_KEYWORDS) + r")\b"
)

# Sent as a fresh text turn after a decision resumes the paused memory run.
# Memory's own resume prompt ends with this phrase (gateway web ui treats it as
# an internal resume marker), so the model continues from where it left off.
_RESUME_PROMPT = "Continue from where you left off."


def _build_tts(cartesia_language: str = "en"):
    """Build the TTS strategy from TTS_PROVIDER.

    ``cartesia`` -> server-side Cartesia TTS (audio streamed to the client).
    ``none`` / ``client`` -> no server TTS; the reply is text-only and the
    client does the synthesis locally (e.g. AVSpeechSynthesizer on iOS).
    """
    provider = config.TTS_PROVIDER.strip().lower()
    if provider in ("none", "client", ""):
        return None
    if provider == "cartesia":
        from livekit.plugins import cartesia

        return cartesia.TTS(
            model=config.CARTESIA_MODEL,
            voice=config.CARTESIA_VOICE,
            language=cartesia_language,
            api_key=config.CARTESIA_API_KEY,
        )
    raise ValueError(f"unknown TTS_PROVIDER: {provider!r}")


def _build_session(
    conversation_id: str | None, binding: VoiceBinding
) -> AgentSession:
    chat = MemoryChatClient(config.MEMORY_URL, binding.token, binding.project_id)
    llm = MemoryLLM(chat, binding.agent_definition_id, conversation_id)
    lang = config.language_config_for(binding.language)
    return AgentSession(
        vad=silero.VAD.load(),
        stt=deepgram.STT(
            model=config.DEEPGRAM_MODEL,
            language=lang["deepgram"],
            api_key=config.DEEPGRAM_API_KEY,
        ),
        llm=llm,
        tts=_build_tts(lang["cartesia"]),
        turn_handling={
            "turn_detection": inference.TurnDetector(version="v1-mini"),
            "endpointing": {
                "min_delay": config.ENDPOINT_MIN_DELAY,
                "max_delay": config.ENDPOINT_MAX_DELAY,
            },
            "interruption": {
                "enabled": config.ALLOW_INTERRUPTIONS,
                "discard_audio_if_uninterruptible": False,
            },
            "preemptive_generation": {"enabled": config.PREEMPTIVE_GENERATION},
        },
        user_away_timeout=config.USER_AWAY_TIMEOUT,
    )


async def _run_text_turn(sess: AgentSession, text: str) -> None:
    """Run one text-modality turn: claim the user slot, mute TTS audio, reply.

    Shared by the `lk.chat` typed-input callback and the decision-driven resume
    (both are "the user answered in text", so the reply must not speak).
    """
    async with sess._claim_user_turn():
        await sess.interrupt()
        sess.output.set_audio_enabled(False)
        try:
            handle = sess.generate_reply(user_input=text, input_modality="text")
            await handle.wait_for_playout()
        finally:
            sess.output.set_audio_enabled(True)


async def _text_input_cb(sess: AgentSession, ev: TextInputEvent) -> None:
    """Typed (`lk.chat`) turns reply text-only: the transcript is delivered to
    the client over `lk.transcription`, with no TTS audio. Mirrors the
    framework's default callback, plus audio gating so the reply doesn't speak."""
    trace.trace(trace.get_room(), "text_input_received", text=ev.text[:200])
    # A new typed turn supersedes any card left open by a paused run: a late
    # decision on that card must not trigger a resume anymore.
    tracker = getattr(sess, "_memory_pause_tracker", None)
    if tracker is not None:
        tracker.reset()
    await _run_text_turn(sess, ev.text)


class _PauseTracker:
    """Tracks the questionIds a paused memory run surfaced to the client.

    Memory pauses once per run (ask_user / tool-policy gate) and resumes only
    after every decision of a parallel batch landed. So the worker fires its
    resume turn only when the last open question of the current pause is
    decided.
    """

    def __init__(self) -> None:
        self._pending: set[str] = set()

    def note_open(self, question_id: str) -> None:
        self._pending.add(question_id)

    def note_decided(self, question_id: str) -> bool:
        """Record a decision; True when it closed the last open question.

        Returns False for unknown/stale question ids (never resumes then).
        """
        if question_id not in self._pending:
            return False
        self._pending.discard(question_id)
        return not self._pending

    def reset(self) -> None:
        self._pending.clear()


def _attach_lifecycle(session: AgentSession, room: str) -> None:
    """Exit keywords + goodbye-on-away. The session is stateless; memory owns the
    conversation, so teardown here is transport-level only."""
    closing = False

    def _begin_close(reason: str) -> None:
        nonlocal closing
        if closing:
            return
        closing = True
        logger.info("closing session (%s): %s", reason, room)
        trace.trace(room, "closing_session", reason=reason)

        async def _bye() -> None:
            try:
                handle = session.say(config.GOODBYE_TEXT, allow_interruptions=False)
                await asyncio.wait_for(handle.wait_for_playout(), timeout=4)
            except Exception:
                logger.exception("goodbye failed")
            finally:
                await session.shutdown()

        _track(_bye())

    @session.on("conversation_item_added")
    def _on_item(ev) -> None:
        item = getattr(ev, "item", None)
        if item is None:
            return
        trace.trace(
            room,
            "conversation_item_added",
            role=getattr(item, "role", None),
            text=(getattr(item, "text_content", None) or "")[:200],
        )
        if getattr(item, "role", None) != "user":
            return
        text = (getattr(item, "text_content", None) or "").lower()
        if text and _EXIT_RE.search(text):
            _begin_close(f"exit keyword in user text: {text[:60]!r}")

    @session.on("user_state_changed")
    def _on_user_state(ev) -> None:
        trace.trace(room, "user_state_changed", new_state=getattr(ev, "new_state", None))
        if getattr(ev, "new_state", None) == "away":
            _begin_close("user away")


def _attach_user_transcription(session: AgentSession, room) -> None:
    """Forward the user's STT transcript to the client over `lk.transcription`
    (agent replies already flow via the framework's RoomOutput). The web client
    tells user vs agent apart by sender identity."""
    async def _forward(text: str, is_final: bool) -> None:
        try:
            if not room.isconnected():
                return
            # 1:1 room: the user is the single remote participant (the worker is
            # the local agent participant).
            identity = next(
                (p.identity for p in room.remote_participants.values()), None
            )
            if identity is None:
                return
            writer = await room.local_participant.stream_text(
                topic="lk.transcription",
                sender_identity=identity,
                attributes={"lk.transcription_final": "true" if is_final else "false"},
            )
            try:
                await writer.write(text)
            finally:
                with contextlib.suppress(Exception):
                    await writer.aclose(
                        attributes={"lk.transcription_final": "true"} if is_final else {}
                    )
        except Exception:
            logger.exception("failed to forward user transcription")

    @session.on("user_input_transcribed")
    def _on_user_input(ev) -> None:
        text = (getattr(ev, "transcript", None) or "").strip()
        if not text:
            return
        _track(_forward(text, bool(getattr(ev, "is_final", False))))


# --- rich chat events: worker -> client -------------------------------------


async def _publish_chat_event(room, payload: dict) -> None:
    """Publish one rich chat event as a JSON object over `lk.chat.events`."""
    if not room.isconnected():
        return
    text = json.dumps(payload, ensure_ascii=False)
    writer = await room.local_participant.stream_text(topic=TOPIC_EVENTS)
    try:
        await writer.write(text)
    finally:
        with contextlib.suppress(Exception):
            await writer.aclose()


async def _handle_decision_text(session, chat, tracker: _PauseTracker, text: str) -> str:
    """Handle one client decision from `lk.chat.decision`.

    Forwards it to memory (respond/cancel), then — when it was the last open
    question of the paused run — resumes the turn with a fresh text reply.
    Returns an outcome label for tests: malformed | memory_error | answered |
    resumed | resume_failed.
    """
    decision = parse_decision(text)
    if decision is None:
        logger.info("ignoring malformed lk.chat.decision payload: %.160s", (text or "").strip())
        return "malformed"
    question_id = decision["questionId"]
    kind = decision["type"]
    try:
        if kind == "approval":
            action = decision["action"]
            if action == "cancel":
                await chat.cancel(question_id)
            else:
                await chat.respond(question_id, action, decision.get("message") or "")
        else:  # "question"
            await chat.respond(question_id, decision["answer"], "")
    except MemoryChatError:
        logger.exception("memory rejected decision for question %s", question_id)
        return "memory_error"
    trace.trace(trace.get_room(), "question_decided", question_id=question_id, type=kind)
    if tracker.note_decided(question_id):
        logger.info("last pending question %s decided; resuming turn", question_id)
        try:
            await _run_text_turn(session, _RESUME_PROMPT)
        except Exception:
            logger.exception("resume turn after decision failed")
            return "resume_failed"
        return "resumed"
    return "answered"


def _attach_chat_io(session: AgentSession, room, binding: VoiceBinding) -> None:
    """Stream rich chat events over `lk.chat.events` and accept the client's
    approval/question decisions (`lk.chat.decision`) and interrupts
    (`lk.chat.interrupt`).

    The event sink is attached to the session's MemoryLLM, which reads it lazily
    on every stream, so attaching here (before session.start) is fine. Every
    ``approval``/``question`` payload is also recorded in the pause tracker so a
    decision can resume the paused turn exactly once.
    """
    chat = MemoryChatClient(config.MEMORY_URL, binding.token, binding.project_id)
    tracker = _PauseTracker()
    # Read by `_text_input_cb` so a fresh typed turn invalidates old cards.
    session._memory_pause_tracker = tracker

    async def _event_sink(payload: dict) -> None:
        if payload.get("type") in ("approval", "question"):
            question_id = payload.get("questionId")
            if question_id:
                tracker.note_open(question_id)
        await _publish_chat_event(room, payload)

    llm = getattr(session, "llm", None)
    if llm is None:
        logger.warning("session has no LLM; lk.chat.events forwarding disabled")
    else:
        llm.chat_event_sink = _event_sink

    # Text-stream handlers are sync; drain the reader in a tracked task so the
    # stream is fully consumed and a mid-read GC can never drop a decision.
    _tasks: set[asyncio.Task] = set()

    def _spawn(coro) -> None:
        task = asyncio.create_task(coro)
        _tasks.add(task)
        task.add_done_callback(_tasks.discard)

    async def _drain_decision(reader) -> None:
        try:
            text = await reader.read_all()
        except Exception:
            logger.exception("failed to read lk.chat.decision stream")
            return
        try:
            await _handle_decision_text(session, chat, tracker, text)
        except Exception:
            logger.exception("decision handling failed")

    def _on_decision(reader, participant_identity) -> None:
        trace.trace(trace.get_room(), "decision_received")
        _spawn(_drain_decision(reader))

    async def _drain_interrupt(reader) -> None:
        try:
            text = await reader.read_all()
        except Exception:
            logger.exception("failed to read lk.chat.interrupt stream")
            return
        if not is_interrupt_message(text):
            logger.info("ignoring non-interrupt lk.chat.interrupt payload")
            return
        trace.trace(trace.get_room(), "interrupt_requested")
        try:
            await session.interrupt()
        except Exception:
            logger.exception("interrupt failed")

    def _on_interrupt(reader, participant_identity) -> None:
        _spawn(_drain_interrupt(reader))

    room.register_text_stream_handler(TOPIC_DECISION, _on_decision)
    room.register_text_stream_handler(TOPIC_INTERRUPT, _on_interrupt)
    logger.info("attached chat io: events=%s decision=%s interrupt=%s",
                TOPIC_EVENTS, TOPIC_DECISION, TOPIC_INTERRUPT)


# port=0 → ephemeral HTTP port per worker (prod default 8081 collides across
# multiple workers and with the legacy control plane).
server = AgentServer(port=0)


@server.rtc_session(agent_name=config.AGENT_NAME)
async def entrypoint(ctx: JobContext) -> None:
    room = ctx.room.name
    trace.set_room(room)
    trace.trace(room, "bridge_starting", agent=config.AGENT_NAME)
    logger.info("bridge starting: room=%s agent=%s", room, config.AGENT_NAME)

    try:
        # Credentials are per-session now: resolve the room's voice binding from
        # the gateway before building anything. No env fallback exists — on
        # failure the entrypoint aborts (supervisor retries the job).
        binding = await fetch_binding(
            room, config.VOICE_BINDING_URL, config.WORKER_INTERNAL_KEY
        )
        trace.trace(
            room,
            "voice_binding_resolved",
            project_id=binding.project_id,
            agent_definition_id=binding.agent_definition_id,
            language=binding.language or None,
        )
        conversation_id = (getattr(ctx.job, "metadata", None) or "").strip() or None
        session = _build_session(conversation_id, binding)
        _attach_lifecycle(session, room)
        _attach_user_transcription(session, ctx.room)
        _attach_chat_io(session, ctx.room, binding)
        agent = Agent(instructions="", tools=[])

        await session.start(
            agent=agent,
            room=ctx.room,
            room_options=RoomOptions(
                text_input=TextInputOptions(text_input_cb=_text_input_cb),
            ),
        )
        await ctx.connect()

        # Signal the client that the mic is subscribed, so the cue-to-speak chime
        # plays at the right moment (not before audio is actually flowing).
        try:
            await ctx.room.local_participant.send_text("ready", topic="lk.agent.ready")
        except Exception:
            logger.exception("failed to send ready signal")

        trace.trace(room, "bridge_ready")
        logger.info("bridge ready: %s", room)
    except Exception as e:
        trace.trace(
            room,
            "error",
            error=str(e),
            traceback=traceback.format_exc(),
        )
        raise
