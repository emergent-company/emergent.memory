"""
Shared configuration for benchmark harnesses.
Loads from .env.benchmark in the benchmarks/ directory, then environment variables.
Environment variables take precedence over .env.benchmark values.
"""

from __future__ import annotations

import os
from dataclasses import dataclass
from pathlib import Path


def _load_dotenv(path: Path) -> None:
    """Minimal dotenv loader — no external deps required."""
    if not path.exists():
        return
    with open(path) as f:
        for line in f:
            line = line.strip()
            if not line or line.startswith("#") or "=" not in line:
                continue
            key, _, val = line.partition("=")
            key = key.strip()
            val = val.strip().strip('"').strip("'")
            # Only set if not already in environment
            if key not in os.environ:
                os.environ[key] = val


# Load .env.benchmark from benchmarks/ dir (two levels up from this file)
_BENCHMARKS_DIR = Path(__file__).parent.parent
_load_dotenv(_BENCHMARKS_DIR / ".env.benchmark")


@dataclass
class Config:
    api_url: str
    api_key: str
    project_id: str
    eval_llm_base_url: str
    eval_llm_api_key: str
    eval_llm_model: str

    def auth_headers(self, project_id: str | None = None) -> dict[str, str]:
        headers: dict[str, str] = {}
        if self.api_key:
            headers["Authorization"] = f"Bearer {self.api_key}"
        pid = project_id or self.project_id
        if pid:
            headers["X-Project-ID"] = pid
        return headers


_cfg: Config | None = None


def get_config() -> Config:
    global _cfg
    if _cfg is None:
        _cfg = Config(
            api_url=os.environ.get("MEMORY_API_URL", "http://localhost:3012"),
            api_key=os.environ.get("MEMORY_API_KEY", ""),
            project_id=os.environ.get("MEMORY_PROJECT_ID", ""),
            eval_llm_base_url=os.environ.get("EVAL_LLM_BASE_URL", "https://api.openai.com/v1"),
            eval_llm_api_key=os.environ.get("EVAL_LLM_API_KEY") or os.environ.get("OPENAI_API_KEY", ""),
            eval_llm_model=os.environ.get("EVAL_LLM_MODEL", "gpt-4o"),
        )
    return _cfg
