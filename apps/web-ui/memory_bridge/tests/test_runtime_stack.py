"""Runtime-stack smoke test for the voice agent (#772).

#742 moved the deployed voice stack by a major version inside a lockfile-only
dependabot PR: `openai 2.54 -> 3.51` and `livekit-agents 1.8.0 -> 1.8.12`
(forced by `opentelemetry-api 1.45.2`, which `livekit-agents` caps). Nothing in
CI imported the stack, so the move was reviewed by hand. These tests make it
visible: they import the stack, pin the majors that were accepted on purpose,
and build the objects the entrypoint builds.

WHAT IS COVERED (offline, sub-second, no DB, no audio, no network):

  * the locked distributions install and import (`openai`, `livekit-agents`,
    `livekit-api`, the four LiveKit plugins declared in pyproject.toml);
  * `_build_session()` — the exact AgentSession `entrypoint` starts — builds:
    silero VAD loaded from the wheel, Deepgram STT, Cartesia TTS (plus the
    text-only `TTS_PROVIDER=none` path the iOS client uses), with `MemoryLLM`
    as its LLM;
  * `MemoryLLM.chat()` still satisfies livekit-agents' `LLMStream` subclass
    contract — the signature a `livekit-agents` bump is likeliest to break,
    because it is the one place this repo subclasses the framework;
  * `livekit.api.AccessToken` mints a room-join JWT (livekit-api's own
    jwt/crypto path, no network);
  * importing `memory_bridge.worker` constructs the module-level `AgentServer`.

WHAT IS NOT COVERED — the honesty this file exists for:

  * **no audio ever flows.** VAD, STT and TTS are constructed and never
    invoked, so a behaviour change in the *inference* path — an `openai` 3.x
    streaming or tool-call change reaching a plugin, a Deepgram/Cartesia
    protocol change pushed through a plugin bump — would NOT fail here;
  * **no memory chat turn.** `MemoryLLMStream._run()` is never driven, so the
    memory <-> brain wire protocol is covered only by test_llm's unit tests;
  * **no room join, no dispatch, no gateway voice-binding round trip**, and no
    real API keys: `fetch_binding` is exercised against MockTransport in
    test_binding, never against the gateway;
  * the **deployed image** itself (cp314 wheels, `uv sync --locked --no-dev`)
    is proved by the CI workflow that runs this file, not by this file.

A live end-to-end voice turn against a deployed agent on dev is #772's own
follow-up and stays unverified by CI: it needs a LiveKit server, a real room and
paid STT/TTS, which this rig does not have. Run it by hand before a release tag
builds the image.

Plain functions + standalone __main__ (pytest optional), per test_llm.py.
"""

import asyncio
import base64
import importlib
import importlib.metadata
import json
import warnings
from contextlib import contextmanager

from memory_bridge import config
from memory_bridge.binding import VoiceBinding
from memory_bridge.llm import MemoryLLM, MemoryLLMStream
from memory_bridge.memory_chat import MemoryChatClient
from memory_bridge.worker import _build_session, _build_tts

# The stack #772/#742 accepted, on purpose. Deliberately not "whatever is
# installed": the point is that the next major move shows up as a red check with
# a one-line diff to justify, instead of a lockfile diff nobody reads. When the
# stack moves intentionally, change these numbers in that PR and say so there.
EXPECTED_OPENAI_MAJOR = "3"
MIN_LIVEKIT_AGENTS = (1, 8, 12)

# Modules `memory_bridge` imports, or that the deployed image must be able to
# import. The worker's own imports are the first group; the rest are the plugin
# and client libraries pyproject.toml declares.
IMPORT_SURFACE = (
    # worker.py / llm.py
    "livekit.agents",
    "livekit.agents.llm",
    "livekit.agents.inference",
    "livekit.agents.voice.room_io",
    "livekit.plugins.deepgram",
    "livekit.plugins.silero",
    "livekit.plugins.cartesia",
    # not imported by the worker itself, but inside the locked runtime stack:
    # `livekit.api` mints the client token, `openai` is what livekit-agents'
    # plugins stream through (the package #742 moved to 3.x), and the
    # turn-detector plugin is declared in pyproject.toml.
    "livekit.api",
    "openai",
    "livekit.plugins.turn_detector",
)

_BINDING = VoiceBinding(
    project_id="proj-1",
    org_id="org-1",
    agent_definition_id="agent-def-1",
    language="pl",
    token="emt-not-a-real-token",
)


@contextmanager
def _config(**overrides):
    """Temporarily override memory_bridge.config attributes.

    The bridge reads its provider keys off the imported module, so a test can
    build real provider objects with placeholder credentials and touch nothing
    global. Restores every value even when the assertion fails.
    """
    saved = {k: getattr(config, k) for k in overrides}
    try:
        for k, v in overrides.items():
            setattr(config, k, v)
        yield
    finally:
        for k, v in saved.items():
            setattr(config, k, v)


def _version_tuple(dist: str) -> tuple[int, ...]:
    raw = importlib.metadata.version(dist)
    parts = []
    for piece in raw.split("."):
        digits = "".join(c for c in piece if c.isdigit())
        if not digits:
            break
        parts.append(int(digits))
    return tuple(parts)


def test_locked_stack_is_the_accepted_one():
    """`uv.lock` moved openai by a major and livekit-agents by a patch in a
    lockfile-only PR (#742). A silent second major is what #772 filed."""
    openai_version = importlib.metadata.version("openai")
    assert openai_version.split(".")[0] == EXPECTED_OPENAI_MAJOR, (
        f"openai {openai_version} is installed, but the voice stack was accepted "
        f"on openai {EXPECTED_OPENAI_MAJOR}.x (#772). A major bump changes the "
        "streaming/tool-call surface livekit-agents' plugins and MemoryLLM sit on. "
        "If this move is intentional, change EXPECTED_OPENAI_MAJOR here and "
        "document it (and the live voice turn it needs) in the PR."
    )
    agents_version = _version_tuple("livekit-agents")
    assert agents_version >= MIN_LIVEKIT_AGENTS, (
        f"livekit-agents {importlib.metadata.version('livekit-agents')} is older "
        f"than the {'.'.join(map(str, MIN_LIVEKIT_AGENTS))} that #742 accepted; "
        "this repo subclasses its LLMStream/AgentSession APIs, so a downgrade "
        "past the accepted floor needs the smoke test re-checked by hand."
    )


def test_locked_stack_imports():
    """Every module in the deployed stack imports under the locked wheels."""
    for name in IMPORT_SURFACE:
        # python -W error would otherwise fail this module on the turn-detector
        # plugin's own DeprecationWarning: the plugin is still declared in
        # pyproject.toml (unused by worker.py, which uses the hosted
        # inference.TurnDetector), and this test's job is only to prove the
        # wheel still installs and imports.
        with warnings.catch_warnings():
            warnings.simplefilter("ignore", DeprecationWarning)
            module = importlib.import_module(name)
        assert module.__name__ == name


def test_bridge_session_builds_under_the_lock():
    """The AgentSession `entrypoint` starts must construct offline.

    This is the whole wiring in one object: silero VAD (model loaded from the
    plugin wheel), Deepgram STT, Cartesia TTS, MemoryLLM, and the turn-handling
    block. It is constructed, not started: nothing connects or plays audio.
    """
    from livekit.agents import AgentSession
    from livekit.plugins import cartesia, deepgram

    with _config(DEEPGRAM_API_KEY="dg-not-a-real-key", CARTESIA_API_KEY="ct-not-a-real-key"):
        session = asyncio.run(_build_session_now(None, _BINDING))

        assert isinstance(session, AgentSession)
        assert isinstance(session.llm, MemoryLLM)
        # MemoryLLM's required `model`/`provider` properties are what
        # livekit-agents binds a session's LLM by; a core bump that renames or
        # re-types them breaks this line before it breaks a live call.
        assert (session.llm.model, session.llm.provider) == ("memory-chat", "memory")
        assert isinstance(session.stt, deepgram.STT)
        assert isinstance(session.tts, cartesia.TTS)

        # A conversation id from the job metadata is carried into the LLM, so
        # the agent continues the browser's chat instead of starting a new one.
        continued = asyncio.run(_build_session_now("conv-42", _BINDING))
        assert continued.llm.conversation_id == "conv-42"
        assert session.llm.conversation_id is None


async def _build_session_now(conversation_id, binding):
    # AgentSession (like llm.LLMStream) reaches for asyncio.get_event_loop() at
    # construction time, and Python 3.14 no longer creates one implicitly — so
    # build it inside a running loop, exactly like the worker does.
    return _build_session(conversation_id, binding)


def test_tts_provider_modes():
    """Server TTS vs the client-side synthesis path (`TTS_PROVIDER=none`)."""
    from livekit.plugins import cartesia

    with _config(CARTESIA_API_KEY="ct-not-a-real-key"):
        with _config(TTS_PROVIDER="cartesia"):
            assert isinstance(_build_tts("en"), cartesia.TTS)
        for provider in ("none", "client", ""):
            with _config(TTS_PROVIDER=provider):
                assert _build_tts("en") is None


def test_memory_llm_stream_contract():
    """`MemoryLLM.chat()` still returns a livekit-agents LLMStream.

    The subclass constructor is the fragile seam: livekit-agents 1.8 owns
    `LLMStream.__init__`'s keyword contract (`chat_ctx`, `tools`, `conn_options`)
    and the event channel it builds. Constructing a stream proves the seam
    without streaming a token.
    """
    from livekit.agents.llm import LLMStream

    llm = MemoryLLM(MemoryChatClient("http://127.0.0.1:1", "tok", "proj"), "agent-def-1")

    async def build():
        from livekit.agents.llm import ChatContext

        return llm.chat(chat_ctx=ChatContext.empty())

    stream = asyncio.run(build())
    assert isinstance(stream, MemoryLLMStream)
    assert isinstance(stream, LLMStream)


def test_livekit_token_mints():
    """livekit-api's JWT path (jwt + crypto deps) works on the locked version."""
    import livekit.api

    token = livekit.api.AccessToken("not-a-real-key", "x" * 32)
    jwt = (
        token.with_identity("user-1")
        .with_name("user-1")
        .with_grants(livekit.api.VideoGrants(room_join=True, room="room-1"))
        .to_jwt()
    )

    assert jwt.count(".") == 2, "not a compact JWS"
    body = jwt.split(".")[1]
    payload = json.loads(base64.urlsafe_b64decode(body + "=" * (-len(body) % 4)))
    assert payload["video"]["room"] == "room-1"
    assert payload["video"]["roomJoin"] is True


def test_worker_process_objects_exist():
    """Importing the worker (the deployed entrypoint module) builds its server.

    `agent.py`/`__main__` hands this to `server.run()`; if a core bump moved
    `AgentServer` or the `rtc_session` decorator, the import would fail here.
    """
    from livekit.agents import AgentServer

    from memory_bridge import worker

    assert isinstance(worker.server, AgentServer)
    assert callable(worker.entrypoint)


if __name__ == "__main__":
    test_locked_stack_is_the_accepted_one()
    test_locked_stack_imports()
    test_bridge_session_builds_under_the_lock()
    test_tts_provider_modes()
    test_memory_llm_stream_contract()
    test_livekit_token_mints()
    test_worker_process_objects_exist()
    print("ALL TESTS PASSED")
