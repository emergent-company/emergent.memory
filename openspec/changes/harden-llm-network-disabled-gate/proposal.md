## Why

`LLM_NETWORK_DISABLED` is an operator kill switch that is already read into
`config.LLMConfig.NetworkDisabled`, but it was only consulted by
`IsEnabled()`/module wiring. `ModelFactory.CreateModel` and
`CreateModelWithName` still constructed provider-backed models (env or DB
credentials) when the switch was on, so a request that reached the factory
directly could still make outbound provider calls. The switch must be enforced
at the point of model creation, not only at wiring time.

## What Changes

- Add an exported `ErrNetworkDisabled` sentinel in `pkg/adk`.
- `ModelFactory.CreateModel` and `ModelFactory.CreateModelWithName` return
  `ErrNetworkDisabled` when `LLM_NETWORK_DISABLED` is on, before resolving
  credentials or constructing any provider-backed model.
- The deterministic canned test-LLM keeps precedence over the kill switch: it
  makes no network calls and must remain usable for tests.
- Flag-off behavior is byte-for-byte identical to today; `IsEnabled()` is
  unchanged.
- New unit tests lock the ordering (test-LLM before gate, gate before
  credential/model construction) on both env-var and DB-credential paths.

## Capabilities

### New Capabilities

- `llm-network-disabled`: the operator kill switch hard-gates LLM model
  creation across environment-var and database-credential resolution.

### Modified Capabilities

<!-- none -->

## Impact

- **Server**: `apps/server/pkg/adk/model.go` — new `ErrNetworkDisabled`, new
  `(*ModelFactory).networkDisabled`, gate added to the two creation entry
  points.
- **Tests**: `apps/server/pkg/adk/model_test.go` — ordering and disabled-path
  regression coverage.
- **API/Data**: no wire or schema change. `ErrNetworkDisabled` is a new
  exported package symbol callers may map to a domain error.
- **Config**: `LLM_NETWORK_DISABLED` already exists; no new env var.
