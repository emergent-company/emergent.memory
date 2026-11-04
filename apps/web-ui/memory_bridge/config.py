"""Bridge configuration — injected by the Go supervisor as environment."""

import os


def _env_bool(name: str, default: bool) -> bool:
    raw = os.getenv(name)
    if raw is None:
        return default
    return raw.strip().lower() in ("1", "true", "yes", "on")


def _env_float(name: str, default: float) -> float:
    raw = os.getenv(name)
    try:
        return float(raw) if raw else default
    except ValueError:
        return default


# Identity — the 3-way binding (LiveKit dispatch name == agent name == worker).
AGENT_NAME = os.getenv("AGENT_NAME", "")

# Per-session credentials (memory token/project + agent definition + language)
# are no longer global env — each room fetches its own binding from the gateway.
# Only the endpoint + worker key are process-global.
WORKER_INTERNAL_KEY = os.getenv("WORKER_INTERNAL_KEY", "")
VOICE_BINDING_URL = os.getenv("VOICE_BINDING_URL", "http://127.0.0.1:8095").rstrip("/")

# Memory (the brain).
MEMORY_URL = os.getenv("MEMORY_URL", "https://memory.emergent-company.ai").rstrip("/")

# LiveKit transport.
LIVEKIT_URL = os.getenv("LIVEKIT_URL", "ws://localhost:7880")
LIVEKIT_API_KEY = os.getenv("LIVEKIT_API_KEY", "")
LIVEKIT_API_SECRET = os.getenv("LIVEKIT_API_SECRET", "")

# STT (Deepgram).
DEEPGRAM_API_KEY = os.getenv("DEEPGRAM_API_KEY", "")
DEEPGRAM_MODEL = os.getenv("DEEPGRAM_MODEL", "nova-3")
DEEPGRAM_LANG = os.getenv("DEEPGRAM_LANG", "multi")

# TTS strategy: "cartesia" (server-side) | "none"/"client" (text-only, client TTS).
TTS_PROVIDER = os.getenv("TTS_PROVIDER", "cartesia")
CARTESIA_API_KEY = os.getenv("CARTESIA_API_KEY", "")
CARTESIA_MODEL = os.getenv("CARTESIA_MODEL", "sonic-3.5")
CARTESIA_VOICE = os.getenv("CARTESIA_VOICE", "9626c31c-bec5-4cca-baa8-f8ba9e84c8bc")

# Agent language — canonical ISO 639-1 code. The gateway no longer injects
# AGENT_LANGUAGE; per-room bindings carry the authoritative code (see
# `language_config_for`). This remains as a fallback for local/direct runs.
AGENT_LANGUAGE = os.getenv("AGENT_LANGUAGE", "").strip().lower()

# Canonical language → per-provider representation. Today Deepgram and Cartesia
# both accept the same ISO 639-1 code, but this is the seam where a
# provider-specific code (a dialect like "es-419", or a per-language voice id)
# goes if one ever diverges. Keep in sync with gateway/agent.go agentLanguages.
LANGUAGES = {
    "en": {"label": "English",   "deepgram": "en", "cartesia": "en"},
    "pl": {"label": "Polski",    "deepgram": "pl", "cartesia": "pl"},
    "de": {"label": "Deutsch",   "deepgram": "de", "cartesia": "de"},
    "es": {"label": "Español",   "deepgram": "es", "cartesia": "es"},
    "fr": {"label": "Français",  "deepgram": "fr", "cartesia": "fr"},
    "it": {"label": "Italiano",  "deepgram": "it", "cartesia": "it"},
    "ja": {"label": "日本語",     "deepgram": "ja", "cartesia": "ja"},
    "pt": {"label": "Português", "deepgram": "pt", "cartesia": "pt"},
    "nl": {"label": "Nederlands", "deepgram": "nl", "cartesia": "nl"},
}

# Legacy free-form labels from before the dropdown, normalized to codes.
_LEGACY_LANGUAGE_ALIASES = {
    "english": "en", "polish": "pl", "german": "de", "spanish": "es",
    "french": "fr", "italian": "it", "japanese": "ja", "portuguese": "pt",
    "dutch": "nl",
}


def language_config_for(code: str) -> dict:
    """Resolve the STT/TTS language codes for a language code.

    ``code`` is a canonical ISO 639-1 code ("en", "pl", ...) or a legacy
    free-form label ("english", ...) from a voice binding. Returns
    ``{"label", "deepgram", "cartesia"}``. Falls back to the global default
    (DEEPGRAM_LANG for STT, Cartesia "en" for TTS) when empty or unknown.
    """
    norm = (code or "").strip().lower()
    canonical = _LEGACY_LANGUAGE_ALIASES.get(norm, norm)
    entry = LANGUAGES.get(canonical)
    if entry:
        return entry
    return {
        "label": norm or "auto",
        "deepgram": DEEPGRAM_LANG,
        "cartesia": "en",
    }


def language_config() -> dict:
    """Resolve the STT/TTS language codes for this worker (env fallback).

    Reads the canonical AGENT_LANGUAGE env var (legacy labels normalized to
    codes); falls back to `language_config_for("")` defaults when unset.
    """
    return language_config_for(AGENT_LANGUAGE)

# Turn / away tuning.
USER_AWAY_TIMEOUT = _env_float("USER_AWAY_TIMEOUT", 20.0)
ENDPOINT_MIN_DELAY = _env_float("ENDPOINT_MIN_DELAY", 0.4)
ENDPOINT_MAX_DELAY = _env_float("ENDPOINT_MAX_DELAY", 3.0)
ALLOW_INTERRUPTIONS = _env_bool("ALLOW_INTERRUPTIONS", True)
PREEMPTIVE_GENERATION = _env_bool("PREEMPTIVE_GENERATION", True)

EXIT_KEYWORDS = [
    k.strip().lower()
    for k in os.getenv(
        "EXIT_KEYWORDS", "stop,goodbye,end,that's all,do widzenia,koniec"
    ).split(",")
    if k.strip()
]

GOODBYE_TEXT = os.getenv("GOODBYE_TEXT", "Goodbye.")
