## 1. Implementation

- [x] Add exported `ErrNetworkDisabled` sentinel in `apps/server/pkg/adk/model.go`.
- [x] Add `(*ModelFactory).networkDisabled` reading `cfg.NetworkDisabled`
      (nil-safe).
- [x] Gate `CreateModel` with the kill switch after the test-LLM check and
      before any resolver/credential work.
- [x] Gate `CreateModelWithName` with the kill switch after the test-LLM check
      and before model-name parsing/credential resolution.
- [x] Leave `IsEnabled()` semantics and all flag-off behavior unchanged.

## 2. Tests

- [x] Unit-test that `CreateModel` returns `ErrNetworkDisabled` when the flag is
      on (env-var path), with no model constructed.
- [x] Unit-test that `CreateModelWithName` returns `ErrNetworkDisabled` when the
      flag is on (DB-credential path), without invoking credential resolution.
- [x] Unit-test that the canned test-LLM still wins over the kill switch.
- [x] Unit-test that flag-off behavior is unchanged (models still resolve).
- [x] Verify: `go build ./...`, `go test -count=1 ./pkg/adk/... ./internal/config/... ./domain/chat/...`,
      `scripts/lint-ratchet.sh`, `openspec validate harden-llm-network-disabled-gate --strict`.
