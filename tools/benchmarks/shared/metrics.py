"""
Shared evaluation metrics for benchmark harnesses.

token_f1      — SQuAD-style token-level F1 (LoCoMo primary metric)
exact_match   — normalised exact string match
llm_judge     — OpenAI-compatible LLM-as-judge binary accuracy (LongMemEval primary)
"""

from __future__ import annotations

import re
import string
from typing import Any

from .config import get_config


# ---------------------------------------------------------------------------
# Normalisation helpers
# ---------------------------------------------------------------------------

_ARTICLES = {"a", "an", "the"}
_PUNCT = set(string.punctuation)


def _normalise(text) -> str:
    """Lowercase, strip punctuation, collapse whitespace, remove articles."""
    text = str(text).lower()
    text = "".join(ch if ch not in _PUNCT else " " for ch in text)
    tokens = [t for t in text.split() if t not in _ARTICLES]
    return " ".join(tokens)


# ---------------------------------------------------------------------------
# Token F1 (SQuAD-style)
# ---------------------------------------------------------------------------

def token_f1(predicted: str, gold: str) -> float:
    """
    Compute token-level F1 between predicted and gold answer strings.
    Primary metric for LoCoMo QA.
    """
    pred_tokens = _normalise(predicted).split()
    gold_tokens = _normalise(gold).split()
    if not pred_tokens and not gold_tokens:
        return 1.0
    if not pred_tokens or not gold_tokens:
        return 0.0

    pred_counts: dict[str, int] = {}
    for t in pred_tokens:
        pred_counts[t] = pred_counts.get(t, 0) + 1

    gold_counts: dict[str, int] = {}
    for t in gold_tokens:
        gold_counts[t] = gold_counts.get(t, 0) + 1

    overlap = sum(min(pred_counts.get(t, 0), gold_counts[t]) for t in gold_counts)
    if overlap == 0:
        return 0.0

    precision = overlap / len(pred_tokens)
    recall = overlap / len(gold_tokens)
    return 2 * precision * recall / (precision + recall)


def exact_match(predicted: str, gold: str) -> float:
    """1.0 if normalised strings match exactly, else 0.0."""
    return 1.0 if _normalise(predicted) == _normalise(gold) else 0.0


# ---------------------------------------------------------------------------
# LLM-as-judge (OpenAI-compatible — works with DeepSeek, any gateway)
# ---------------------------------------------------------------------------

_JUDGE_SYSTEM = (
    "You are an answer-correctness judge. "
    "Given a question, a gold answer, and a predicted answer, "
    "output exactly one word: CORRECT or INCORRECT. "
    "Be lenient about phrasing — if the predicted answer conveys the same fact, output CORRECT."
)

_JUDGE_USER_TMPL = """Question: {question}
Gold answer: {gold}
Predicted answer: {predicted}

Is the predicted answer correct? Reply with exactly one word: CORRECT or INCORRECT."""


def llm_judge(
    question: str,
    predicted: str,
    gold: str,
    *,
    base_url: str | None = None,
    api_key: str | None = None,
    model: str | None = None,
) -> float:
    """
    Call an OpenAI-compatible chat endpoint to judge correctness.
    Returns 1.0 (CORRECT) or 0.0 (INCORRECT).
    Compatible with DeepSeek, local Ollama, any OpenAI gateway.
    """
    try:
        from openai import OpenAI  # type: ignore
    except ImportError:
        raise ImportError("pip install openai  (needed for llm_judge)")

    cfg = get_config()
    client = OpenAI(
        base_url=base_url or cfg.eval_llm_base_url,
        api_key=api_key or cfg.eval_llm_api_key,
    )
    m = model or cfg.eval_llm_model

    resp = client.chat.completions.create(
        model=m,
        messages=[
            {"role": "system", "content": _JUDGE_SYSTEM},
            {"role": "user", "content": _JUDGE_USER_TMPL.format(
                question=question, gold=gold, predicted=predicted,
            )},
        ],
        max_tokens=5,
        temperature=0,
    )
    verdict = resp.choices[0].message.content.strip().upper()
    return 1.0 if verdict.startswith("CORRECT") else 0.0


# ---------------------------------------------------------------------------
# Aggregate helpers
# ---------------------------------------------------------------------------

def aggregate(results: list[dict[str, Any]], metric_key: str = "f1") -> dict[str, Any]:
    """
    Compute mean metric across all results and per-category breakdown.
    Each result dict must have metric_key and optionally a 'category' field.
    """
    if not results:
        return {"mean": 0.0, "count": 0, "by_category": {}}

    scores = [r[metric_key] for r in results if metric_key in r]
    mean = sum(scores) / len(scores) if scores else 0.0

    by_cat: dict[str, list[float]] = {}
    for r in results:
        cat = str(r.get("category", "unknown"))
        by_cat.setdefault(cat, []).append(r.get(metric_key, 0.0))

    return {
        "mean": round(mean, 4),
        "count": len(scores),
        "by_category": {
            cat: {"mean": round(sum(vs) / len(vs), 4), "count": len(vs)}
            for cat, vs in by_cat.items()
        },
    }
