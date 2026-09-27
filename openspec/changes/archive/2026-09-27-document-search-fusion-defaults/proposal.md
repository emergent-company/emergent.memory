## Why

PR #1002 (`24e6a5bb8`) changed the default weighted-fusion ranking: the relationship leg is no longer promoted to `graphWeight` when its weight is unset. That fix removed a real defect — 72,814 near-identical `has_paragraph` candidates displaced the correct statute, with the graph leg already returning the correct item (issue #996) — but it shipped with **no spec documenting the resulting default**. Repo policy is that a behaviour change ships with its spec; the *new* default is now undocumented behaviour an operator or agent cannot discover from the specs.

## What Changes

Documents, as a new `search` capability spec, the default fusion semantics introduced by #1002:

- **Weighted fusion (default)**: when the relationship weight is unset or `0`, the relationship leg is excluded from weighting — relationship candidates score `0` and sort last, remain present in `Results`, and are truncated only past `limit`.
- **Explicit per-request weight**: `relationshipWeight > 0` re-enables the relationship leg as a first-class contributor via three-way normalisation.
- **`SEARCH_RELATIONSHIP_WEIGHT`** env var (default `0`, read in `NewService` via `envFloatDefault`) restores relationship contributions with no code change.
- **`rrf` strategy** still rank-fuses relationships positively; `interleave` / `graph_first` / `text_first` are weightless and unaffected.

## Capabilities

### New Capabilities

- `search`: the default fusion-strategy semantics and the relationship leg's treatment under each strategy.

### Modified Capabilities

<!-- None: this introduces a new capability spec; no existing spec documented the old promotion. -->

## Impact

Spec-only. No code, schema, or API change.

Refs #996 (root cause), #1002 (the change).
