# Wake word retrain — hey_alfred → hey_memory

**Status:** proposed
**Created:** 2026-09-04
**Source:** [2026-09-04-rename-alfred-to-memory](../sessions/2026-09-04-rename-alfred-to-memory.md)

## What

Retrain the Porcupine/PV wake-word model so the wake phrase becomes `hey memory` instead of `hey alfred`, then update all references:
- `.env.example` (`WAKE_WORD_MODEL=hey_alfred`, `WAKE_WORD_MODEL_PATH=./models/hey_alfred.onnx`)
- `client/wakeword_client.py`, `client/diag_scores.py`, `tools/record_audio.py`
- the `.onnx` model file in `models/` (gitignored)

## Why

Deferred from the rename: the wake word requires ONNX model retraining, which is a product decision (phrase choice) + training effort, not a code rename.

## Depends on

none

## Notes

- Decide the actual phrase first (`hey memory` vs a different brand-appropriate trigger).
- The ONNX model is gitignored; retraining output must be shipped out-of-band (deploy step).
