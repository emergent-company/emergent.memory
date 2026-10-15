<!-- openspec:archive-hold: the predecessor change `agent-public-share` is still unarchived on main, so the
     requirement headers this change MODIFIES (`agent-public-share-runtime`, `web-agent-public-share`) still
     live only in that unarchived delta and are not yet in the archived main specs, and an archive would be
     refused. Land this PR as-is and archive after `agent-public-share` is archived (which itself waits on
     its deferred Phase 2/3 tasks). -->

## 1. Server: share session deletion reclaims the backing session (#1891)

- [x] 1.1 Rework `DeleteShareSession` in `apps/server/domain/agents/share_store.go` to run in a transaction: resolve the backing `kb.sessions` row for the join row, delete the join row, and delete the session's runs + the session itself only when no other `kb.agent_share_sessions` row references that session.
- [x] 1.2 Add `apps/server/domain/agents/share_delete_store_test.go` (sqlmock) covering: join-row-only delete when another share still references the session, and full cascade (runs + session) when the last reference is removed.
- [x] 1.3 Take a per-conversation `pg_advisory_xact_lock` before the reclaim decision (same pattern as the cap/run guards), so two concurrent last-reference deletes cannot both miss the reclaim under READ COMMITTED; the sqlmock regexes assert the delete/count predicates, not just the table names.

## 2. Server: share-session cancel duplication (#1889)

- [x] 2.1 Extract a single `cancelQuestion(ctx, p RespondParams)` in `apps/server/domain/agents/handler.go`; make `CancelQuestion` and `HandleCancelQuestion` thin delegators.
- [x] 2.2 `go build ./...` + `domain/agents` tests pass.

## 3. Server: userprofile logging + test (#1887, #1888)

- [x] 3.1 Downgrade the duplicate 5xx `log.Error` calls in `apps/server/domain/userprofile/repository.go` to `log.Debug` (the logging boundary is `apperror.HTTPErrorHandler`).
- [x] 3.2 Add `apps/server/domain/userprofile/repository_test.go` (sqlmock) pinning `GetEmail`: `sql.ErrNoRows` → `("", nil)`; other error → `database_error`.

## 4. Tooling: lint ratchet headroom (#1886)

- [x] 4.1 Raise `BASELINE_APPERROR_STYLEA` in `apps/server/scripts/lint-ratchet.sh` to the live count plus headroom; `scripts/lint-ratchet.sh` passes.

## 5. Web UI: share rail failure state (#1890)

- [x] 5.1 Delete the dead `loadSessions`/`renderSessions`/`sessionRow` JSON fallback in `apps/web-ui/gateway/webui/static/js/share-agent.js`.
- [x] 5.2 Render an explicit recoverable state (`share-sessions-error` + `share-retry-rail`) when the server-rendered rail partial fails, and wire the retry action to `refreshRail()`.
- [x] 5.3 `node --check` the script.
- [x] 5.4 Treat a non-partial 2xx body as a failure too: `isSessionsPartial` requires a `share-session-row` or `share-sessions-empty` marker before the body is injected into the rail.

## 6. Web UI: read-only cards when approvals are disabled (#1892)

- [x] 6.1 In `share-agent.js`, record `config.allowEndUserApprovals` as `data-allow-approvals` on the page root and expose `approvalsEnabled()` on the shared engine context.
- [x] 6.2 In `chat-stream.js`, `renderQuestion` and `renderApproval` omit decision controls (option rows, Submit, Approve/Reject/Cancel) and their handlers when the host reports decisions disabled, keeping the question body / tool call visible. The reject-message input is only emitted when decisions are allowed.
- [x] 6.3 `node --check` both scripts.
- [x] 6.4 Gate the decision-bearing A2UI cards too: `share-agent.js` puts `approvalsEnabled` on the `badgeCtx` it passes to `renderA2UISurface`, and `chat-components.js` renders `proposal`/`approval`/`question` read-only (bodies visible, `Approvals are disabled for this chat.` footer) when the hook reports decisions disabled. The `ui` frame is the second renderer of approval controls, so the `approval`/`question` frames alone were not sufficient.
- [x] 6.5 Add `tests/e2e/specs/js/a2ui-approvals-gate.spec.ts` (hermetic js-dom gate, run by CI) pinning both directions: hook → no `chat-approval-*`/`chat-question-option` controls with the disabled note; hook absent → the full controls still render. Verified non-vacuous (fails when the gate is removed).

## 7. Web UI: remove the dead share endpoints (#1893)

- [x] 7.1 Remove `share.GET("/api/questions", ...)` from `apps/web-ui/gateway/main.go`.
- [x] 7.2 Remove `shareQuestions`, `mapShareQuestion`, and `shareQuestionJSON` from `apps/web-ui/gateway/share.go`.
- [x] 7.3 Remove the `GET /share/api/sessions` route and `shareListSessions` handler the client-fallback removal orphaned, plus the orphaned `ShareQuestion` type, `MemoryClient.ShareListQuestions`, the `MemoryBackend` method, and the `fakeMemory` stub — the change's own "only consumed endpoints" rule.

## 8. Verification

- [x] 8.1 `go build ./...` in `apps/server` and `apps/web-ui/gateway`.
- [x] 8.2 `go test ./...` for `apps/server/domain/agents`, `apps/server/domain/userprofile`, and the gateway.
- [x] 8.3 `apps/server/scripts/lint-ratchet.sh` passes with the new baseline.
- [x] 8.4 `node --check` on `share-agent.js`, `chat-stream.js`, and `chat-components.js`.
- [x] 8.5 Repo lint (`task lint`) green.
- [x] 8.6 `npx playwright test --config=js-dom.config.ts` green (hermetic CI JS gate).
- [x] 8.7 `npx playwright test --config=share.config.ts` green (share mock harness).

## 9. Lint: gitleaks false positives (#1905)

- [x] 9.1 Add a `.gitleaks.toml` path allowlist entry for `domain/sandbox/integration_mock_test.go` (fabricated `ghp_secret123` fixture token), extending the existing "Go test fixtures" allowlist.
- [x] 9.2 Add a precise regex allowlist for the bare `E2E_SCENARIO_LLM_API_KEY` env-var *name* used in an operator-facing message string.
- [x] 9.3 Add a precise regex allowlist for templ's generated identifier (`templ_[0-9a-f]{8}_Err`) rather than a `*_templ.go` path allowlist — the path form would also mask the three committed `*_templ.go` files under `apps/cli/internal/graphexplore/`, none of which is gitignored.
- [x] 9.4 `gitleaks dir . --config .gitleaks.toml` reports no leaks; no `.gitleaksignore` entries added.

## 10. E2E coverage (share mock harness)

- [x] 10.1 In `tests/e2e/specs/share/agent-public-share.spec.ts`, add `a failed rail refresh shows an explicit error with a working retry`: intercept `share/api/partial/sessions`, assert `share-sessions-error` + retry, then retry and assert the server-rendered row and its "Actions for …" menu return.
- [x] 10.2 Add `a link with end-user approvals disabled renders question and approval cards read-only` against a new well-known mock key (`no-approvals-key` → `allowEndUserApprovals:false`): no option rows, no Submit, no Approve/Reject/Cancel.
- [x] 10.3 `mock-memory.mjs`: add the `no-approvals-key` exchange branch and make `publicConfigForLink` derive `allowEndUserApprovals` from the link config instead of hardcoding true.
- [x] 10.4 `npx playwright test --config=share.config.ts` passes.

## 11. Independent review (pre-merge gate)

- [x] 11.1 Reviewer 1 (server correctness): PASS — no BLOCKER/MAJOR; cap-counting wording, TOCTOU lock, and sqlmock predicate depth actioned in 1.3 and the proposal/spec reword.
- [x] 11.2 Reviewer 2 (web UI / JS): FAIL initially — the A2UI `ui`-frame renderer bypassed the read-only gate (6.4/6.5) and the fallback removal orphaned `GET /share/api/sessions` (7.3); both fixed.
- [x] 11.3 Reviewer 3 (tooling / spec / hygiene): FAIL initially — the `*_templ.go` path allowlist masked committed files and the delta MODIFIED header was misnamed so an archive would still be refused; both fixed (9.3, header renamed to the predecessor's `Session list and approval surface`).
- [x] 11.4 Re-review after the fixes: all three lenses green.

