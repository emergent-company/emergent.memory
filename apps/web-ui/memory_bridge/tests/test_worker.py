"""Unit tests for the worker's decision round-trip (task 2.5).

Verifies that a client decision on `lk.chat.decision` is forwarded to memory
(respond/cancel) BEFORE the paused turn is resumed with a fresh text
generate_reply, that the resume fires only once per paused batch, and that
malformed input is ignored. The memory client and AgentSession are fakes, so no
network or LiveKit is touched.

Plain functions + standalone __main__ (pytest optional, per test_llm.py).
"""
import asyncio
import logging

from memory_bridge.memory_chat import MemoryChatError
from memory_bridge.worker import (
    _PauseTracker,
    _RESUME_PROMPT,
    _handle_decision_text,
    _run_text_turn,
)

# Keep expected error-log noise (e.g. the memory_error test path) out of output.
logging.getLogger("memory.bridge").addHandler(logging.NullHandler())


class _FakeOutput:
    def __init__(self, log):
        self._log = log

    def set_audio_enabled(self, enabled):
        self._log.append(f"audio {'on' if enabled else 'off'}")


class _FakeSession:
    """Minimal AgentSession stand-in exposing the surface _run_text_turn uses."""

    def __init__(self, log):
        self._log = log
        self.output = _FakeOutput(log)
        self.generated = []

    def _claim_user_turn(self):
        return _FakeTurn(self._log)

    async def interrupt(self):
        self._log.append("interrupt")

    def generate_reply(self, *, user_input, input_modality="text"):
        self._log.append(f"generate:{user_input}:{input_modality}")
        self.generated.append(user_input)
        return _FakeHandle()

    @property
    def llm(self):
        return None


class _FakeTurn:
    def __init__(self, log):
        self._log = log

    async def __aenter__(self):
        self._log.append("claim enter")
        return self

    async def __aexit__(self, *exc):
        self._log.append("claim exit")


class _FakeHandle:
    async def wait_for_playout(self):
        pass


class _FakeChat:
    def __init__(self, log):
        self._log = log
        self.responded = []
        self.cancelled = []

    async def respond(self, question_id, response, message=""):
        self._log.append(f"respond {question_id} {response!r} {message!r}")
        self.responded.append((question_id, response, message))

    async def cancel(self, question_id):
        self._log.append(f"cancel {question_id}")
        self.cancelled.append(question_id)


class _ErrorChat(_FakeChat):
    async def respond(self, question_id, response, message=""):
        self._log.append(f"respond {question_id} {response!r} {message!r}")
        raise MemoryChatError("409 conflict")


def test_run_text_turn_text_modality_with_audio_gating():
    async def run():
        log = []
        sess = _FakeSession(log)
        await _run_text_turn(sess, "hello there")
        assert log == [
            "claim enter",
            "interrupt",
            "audio off",
            "generate:hello there:text",
            "audio on",
            "claim exit",
        ]
        assert sess.generated == ["hello there"]
    asyncio.run(run())


def test_approval_decision_responds_then_resumes():
    async def run():
        log = []
        sess = _FakeSession(log)
        chat = _FakeChat(log)
        tracker = _PauseTracker()
        tracker.note_open("q-1")
        outcome = await _handle_decision_text(
            sess, chat, tracker, '{"type":"approval","questionId":"q-1","action":"approve"}'
        )
        assert outcome == "resumed"
        # Ordering: memory respond lands strictly before the resume turn fires.
        assert chat.responded == [("q-1", "approve", "")]
        assert log.index("respond q-1 'approve' ''") < log.index("claim enter")
        assert log[-1] == "claim exit"
    asyncio.run(run())


def test_reject_with_reason_passes_message():
    async def run():
        log = []
        sess = _FakeSession(log)
        chat = _FakeChat(log)
        tracker = _PauseTracker()
        tracker.note_open("q-1")
        outcome = await _handle_decision_text(
            sess, chat, tracker,
            '{"type":"approval","questionId":"q-1","action":"reject","message":"not now"}',
        )
        assert outcome == "resumed"
        assert chat.responded == [("q-1", "reject", "not now")]
    asyncio.run(run())


def test_question_answer_responds_with_answer():
    async def run():
        log = []
        sess = _FakeSession(log)
        chat = _FakeChat(log)
        tracker = _PauseTracker()
        tracker.note_open("q-2")
        outcome = await _handle_decision_text(
            sess, chat, tracker, '{"type":"question","questionId":"q-2","answer":"the red one"}'
        )
        assert outcome == "resumed"
        assert chat.responded == [("q-2", "the red one", "")]
        assert sess.generated == [_RESUME_PROMPT]
    asyncio.run(run())


def test_cancel_action_hits_cancel_endpoint():
    async def run():
        log = []
        sess = _FakeSession(log)
        chat = _FakeChat(log)
        tracker = _PauseTracker()
        tracker.note_open("q-1")
        outcome = await _handle_decision_text(
            sess, chat, tracker, '{"type":"approval","questionId":"q-1","action":"cancel"}'
        )
        assert outcome == "resumed"
        assert chat.cancelled == ["q-1"]
        assert chat.responded == []
    asyncio.run(run())


def test_parallel_batch_resumes_only_after_last_decision():
    async def run():
        log = []
        sess = _FakeSession(log)
        chat = _FakeChat(log)
        tracker = _PauseTracker()
        tracker.note_open("q-1")
        tracker.note_open("q-2")

        first = await _handle_decision_text(
            sess, chat, tracker, '{"type":"approval","questionId":"q-1","action":"approve"}'
        )
        assert first == "answered"          # q-2 still open: no resume yet
        assert sess.generated == []

        second = await _handle_decision_text(
            sess, chat, tracker, '{"type":"approval","questionId":"q-2","action":"reject"}'
        )
        assert second == "resumed"          # last decision: resume exactly once
        assert sess.generated == [_RESUME_PROMPT]
        assert len(chat.responded) == 2
    asyncio.run(run())


def test_stale_decision_responds_but_does_not_resume():
    """Decision on a card from an older (superseded) pause: still forwarded so
    memory's background run does not stay paused, but no new turn is fired."""
    async def run():
        log = []
        sess = _FakeSession(log)
        chat = _FakeChat(log)
        tracker = _PauseTracker()          # tracker was reset by a newer turn
        outcome = await _handle_decision_text(
            sess, chat, tracker, '{"type":"approval","questionId":"q-old","action":"approve"}'
        )
        assert outcome == "answered"
        assert chat.responded == [("q-old", "approve", "")]
        assert sess.generated == []
    asyncio.run(run())


def test_malformed_decision_ignored():
    async def run():
        log = []
        sess = _FakeSession(log)
        chat = _FakeChat(log)
        tracker = _PauseTracker()
        tracker.note_open("q-1")
        for text in ["not json", "", "{}", '{"type":"question","questionId":"q-1"}']:
            outcome = await _handle_decision_text(sess, chat, tracker, text)
            assert outcome == "malformed", text
        assert chat.responded == [] and chat.cancelled == []
        assert sess.generated == []
    asyncio.run(run())


def test_memory_error_no_resume():
    async def run():
        log = []
        sess = _FakeSession(log)
        chat = _ErrorChat(log)
        tracker = _PauseTracker()
        tracker.note_open("q-1")
        outcome = await _handle_decision_text(
            sess, chat, tracker, '{"type":"approval","questionId":"q-1","action":"approve"}'
        )
        assert outcome == "memory_error"
        assert sess.generated == []        # never resumed on a failed decision
    asyncio.run(run())


if __name__ == "__main__":
    test_run_text_turn_text_modality_with_audio_gating()
    test_approval_decision_responds_then_resumes()
    test_reject_with_reason_passes_message()
    test_question_answer_responds_with_answer()
    test_cancel_action_hits_cancel_endpoint()
    test_parallel_batch_resumes_only_after_last_decision()
    test_stale_decision_responds_but_does_not_resume()
    test_malformed_decision_ignored()
    test_memory_error_no_resume()
    print("ALL TESTS PASSED")
