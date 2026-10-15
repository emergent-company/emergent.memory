## Why

A review pass over the public-share surface and two server-side chores turned up
eight defects that are cheap to fix and expensive to leave:

- The share page's session rail silently degraded to a menu-less client-rendered
  fallback whenever the server-rendered partial failed to load, so the
  archive/unarchive/delete actions disappeared with no error shown (#1890).
- `DeleteShareSession` deleted only the join row, stranding the backing
  `kb.sessions` row, its runs, and its messages forever — unbounded retention
  debt that no later cleanup path reclaimed (#1891).
- `allow_end_user_approvals=false` was enforced only server-side; the client
  still rendered Approve/Reject/Submit controls that always 403 (#1892).
- `GET /share/api/questions` was unconsumed and its mapper ignored `Tool`/`Input`
  (#1893); removing the client-side JSON fallback (#1890) likewise orphaned
  `GET /share/api/sessions`.
- `Handler.CancelQuestion` duplicated `HandleCancelQuestion` (~90 lines kept in
  lockstep by convention only) (#1889).
- `userprofile/repository.go` logged 5xx errors that
  `apperror.HTTPErrorHandler` logs again at the single logging boundary (#1887),
  and its `GetEmail` error mapping had no test (#1888).
- The apperror Style A lint ratchet baseline was pinned at the live count, so any
  change anywhere in the tree failed the ratchet (#1886).
- `apps/server/scripts/lint-ratchet.sh` was not the only guard pinned at its
  live value: the repo-wide `gitleaks dir .` lint reported three unwaived
  `generic-api-key` false positives (a fabricated `ghp_secret123` test token, an
  E2E env-var *name*, and templ's own generated identifier), keeping the `lint`
  group red (#1905).

## What Changes

- **#1890** `share-agent.js`: delete the dead `loadSessions`/`renderSessions`/
  `sessionRow` JSON fallback. A failed rail partial now renders an explicit
  `Couldn't load your chats` state with a `Retry` action
  (`share-sessions-error`, `share-retry-rail`) that re-requests the
  server-rendered partial, so the per-row menu can never silently vanish.
- **#1891** `share_store.go`: `DeleteShareSession` resolves the backing session,
  deletes the join row, and — only when no other share link references that
  session — deletes the session's runs and the session itself, transactionally
  under a per-conversation advisory lock so concurrent deletes cannot both miss
  the reclaim. This is retention, not cap accounting: the active-session cap
  counts join rows and the concurrent-run cap counts runs joined through the
  join table, so deleting the join row already removed the session from both.
- **#1892** `share-agent.js` records the sanitized config's
  `allowEndUserApprovals` on the page root (`data-allow-approvals`); the shared
  engine (`chat-stream.js`) renders question and approval cards read-only — no
  option rows, no decide buttons — when the host reports decisions disabled, and
  the same hook gates the decision-bearing A2UI cards (`chat-components.js`, the
  `ui` frame path). The authenticated chat host is unaffected (it omits the
  hook).
- **#1893** Remove the unconsumed `GET /share/api/questions` route, its
  `shareQuestions`/`mapShareQuestion`/`shareQuestionJSON` helpers, and the
  orphaned `ShareQuestion`/`ShareListQuestions` backend plumbing; remove the
  now-unconsumed `GET /share/api/sessions` route and `shareListSessions` handler
  the fallback removal orphaned.
- **#1889** Collapse `Handler.CancelQuestion` and `HandleCancelQuestion` onto a
  single `cancelQuestion(ctx, params)` helper.
- **#1887** Drop the duplicate `log.Error` calls on the `userprofile` 5xx path
  to `log.Debug`.
- **#1888** Add `userprofile/repository_test.go` (sqlmock) pinning `GetEmail`'s
  `sql.ErrNoRows` → `("", nil)` and generic-error → `database_error` mapping.
- **#1886** Raise `BASELINE_APPERROR_STYLEA` in `scripts/lint-ratchet.sh` from
  1206 to 1208 (live count 1203 + headroom) so unrelated changes do not fail the
  ratchet.
- **#1905** Add `.gitleaks.toml` allowlists covering the three false positives
  (path entry for the fabricated-token sandbox mock test, a precise regex for
  the bare `E2E_SCENARIO_LLM_API_KEY` name, and a precise `templ_<hash>_Err`
  regex that clears templ's generated identifier in both gitignored web-ui
  output and the committed CLI `*_templ.go` files without masking their
  contents), so the repo-wide `gitleaks dir .` lint group is green again.
  No `.gitleaksignore` entries are added.

## Capabilities

### Modified Capabilities

- `web-agent-public-share`: the rail no longer hides failures behind a
  menu-less fallback, and the share page renders questions/approvals read-only
  when the link disables end-user approvals.
- `agent-public-share-runtime`: deleting a share session now reclaims the
  backing session and its runs when no other link references them.

## Impact

- `apps/web-ui/gateway/webui/static/js/share-agent.js`
- `apps/web-ui/gateway/webui/static/js/chat-stream.js`
- `apps/web-ui/gateway/webui/static/js/chat-components.js` (A2UI read-only gate)
- `apps/web-ui/gateway/share.go`, `apps/web-ui/gateway/main.go`,
  `apps/web-ui/gateway/memory_share.go`, `apps/web-ui/gateway/backend.go`
- `apps/server/domain/agents/share_store.go`, `handler.go`
- `apps/server/domain/userprofile/repository.go`
- `apps/server/scripts/lint-ratchet.sh`
- Tests: `apps/server/domain/agents/share_delete_store_test.go`,
  `apps/server/domain/userprofile/repository_test.go`

## Out of scope

- #1857 (mutations Playwright lane) is blocked on CI infrastructure: the
  self-hosted runner has no session-mode gateway and no `E2E_TEST_USER_*`
  secrets, so the lane cannot pass in CI. Tracked separately.
