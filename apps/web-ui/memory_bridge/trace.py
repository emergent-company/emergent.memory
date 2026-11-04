"""Room-keyed JSONL session trace. Direct file append so it works inside the
LiveKit forkserver child (where the `logging` module is not configured)."""
import json
import os
from datetime import datetime, UTC

TRACE_DIR = os.environ.get("MEMORY_TRACE_DIR", "/tmp/memory-trace")

_room: str = ""

def set_room(room: str) -> None:
    global _room
    _room = room

def get_room() -> str:
    return _room

def trace(room: str, event: str, **fields) -> None:
    try:
        os.makedirs(TRACE_DIR, exist_ok=True)
        rec = {"ts": datetime.now(UTC).isoformat(), "room": room, "event": event}
        rec.update(fields)
        with open(os.path.join(TRACE_DIR, f"{room}.jsonl"), "a") as f:
            f.write(json.dumps(rec, default=str) + "\n")
    except Exception:
        pass  # tracing must never break the session
