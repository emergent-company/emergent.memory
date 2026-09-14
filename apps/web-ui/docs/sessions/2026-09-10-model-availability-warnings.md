# 2026-09-10 — Agent / blueprint / chat model-availability warnings

## Goal

- Resolve the report that the bundled `operator` blueprint pins `openai/deepseek-v4-pro`,
  yet a project with **no configured providers** shows the model with no warning.
- Decide what a blueprint's pinned model should mean (suggestion vs contract), implement the
  policy, then extend coverage: e2e specs, then the chat workspace (the one surface that still
  failed silently on send).

## Outcome

Done — three worktree lanes, each tested and merged to master (CI green: build/lint/templ/test/vet):

| PR | Commit | What |
|---|---|---|
| #3 | `0ae4069` | `feat(gateway): warn on agents whose provider is unconfigured` — availability warnings on agent dashboard, agent settings, blueprint detail agent rows; model kept, never mutated |
| #12 | `49c617a` | `test(e2e): cover unservable-model warnings on agents and blueprints` — replaced the now-stale "explicit model never warns" test, added provider-configured no-alert case + new blueprint spec |
| #16 | `7048bc0` | `feat(gateway): warn in chat when agent model unservable` — pre-send banner in the chat workspace, reactive to the agent picker |

Each lane was an isolated worktree off `origin/master`, self-cleaned after merge (branch +
worktree removed).

## Decisions

- **Warn, never mutate** — the stored model is kept as-is; an unsatisfiable pin is surfaced as a
  warning, never dropped/overridden at install/apply. Rationale: bundled blueprints are seeded as
  **global, published** records shared across projects, so a per-project model override cannot go
  through `ApplyBlueprint(ctx, id)`; warn-only also avoids surprising users who rely on pinned models.
- **Availability = provider-prefix match** — `providerKey/modelName`'s prefix must equal a configured
  provider key. Catalog membership is deliberately **not** required (custom OpenAI-compatible base
  URLs legitimately serve off-catalog names), and a bare model (no `/`) with providers present is
  treated as satisfied (mirrors how model pickers tolerate custom names).
- **Severity split** — `error` only when chats genuinely cannot run (zero providers, or the pinned
  model's prefix is unconfigured); the legacy "providers exist but no default is pinned" state stays
  `warning`. The chat banner shows `error` only.
- **One classifier, reused** — `agentModelDashboardIssue` / `agentModelSettingsIssue` /
  `agentPinnedModelIssue` own all copy; the chat banner reuses the dashboard helper so the message
  text is identical across surfaces and has a single Go source.
- **Chat warning is server-rendered + client-refreshed** — server fills the banner for the selected
  agent; `chat.js` re-reads each option's `data-warn` on picker change/resume. No copy is duplicated
  in JS.
- **Install-time prompt / drop-pinned-model was considered and deferred** — see
  [`blueprint-model-install-choice`](../tasks/blueprint-model-install-choice.md).

## Changes

PR #3 — availability warning core:
- `gateway/agent.go` — added `projectProviderNames`, `modelProviderOf`, `classifyAgentModelIssue`,
  and the per-context evaluators `agentModelDashboardIssue` / `agentModelSettingsIssue` /
  `agentPinnedModelIssue`; added `ProviderNames` to the dashboard/settings data; removed the old
  `agentModelWarningSeverity`.
- `gateway/agent.templ` — dashboard notice + settings warning render the evaluator's `(sev, msg)`.
- `gateway/blueprints.go` — `BlueprintDetail.ProviderNames`.
- `gateway/blueprints_handlers.go` — `uiBlueprint` fetches configured providers best-effort when the
  detail carries a pinned-model agent.
- `gateway/blueprints.templ` — `agentDetailRow` warns when a pinned model can't be served.
- `gateway/blueprints/operator/project.yaml` — doc drift fixed: model described as
  `deepseek/deepseek-v4-pro`, actual pin is `openai/deepseek-v4-pro`.
- `docs/spec/03-agent-model.md` — new "Explicit (pinned) models without a configured provider" section.
- Tests: `gateway/agent_ui_test.go`, `gateway/blueprints_test.go`.

PR #12 — e2e:
- `tests/e2e/specs/agents/agent-model-warning-ui.spec.ts` — the `no alert when the agent has an
  explicit model` test contradicted the new behavior; replaced with
  `error severity: explicit model on a provider-less project` and
  `no alert when the explicit model's provider is configured`.
- `tests/e2e/specs/schema/blueprint-model-warning-ui.spec.ts` — new: `/blueprints/operator` warns on a
  provider-less project, quiet once `openai` is configured.

PR #16 — chat workspace:
- `gateway/ui.go` — `chatModelWarnings(ctx, agents)` (agentID → error message), called from `uiChat`.
- `gateway/chat.templ` — `modelWarnings` param on `ChatPage`/`chatWorkspace`; `data-warn` on picker
  options; `chatModelWarningBanner` (role=alert, `alert-error`, Configure-a-provider link) between the
  header and the log.
- `gateway/webui/static/js/chat.js` — `updateModelWarning()` on init, picker change, and resume.
- `gateway/chat_model_test.go` — new (mapping + banner render states); `gateway/schedules_ui_test.go`,
  `gateway/project_settings_ui_test.go` — call-site updates.
- `tests/e2e/specs/agents/agent-model-warning-ui.spec.ts` — 5th case: `/chat?agent=<id>` shows the
  banner before send.
- `docs/spec/03-agent-model.md` — chat banner sentence.

## Verification

- PR #3 lane: `templ generate`, `go build ./...`, `go test -count=1 ./...`,
  `golangci-lint run ./...`, `go vet` — all clean.
- PR #12 lane: `npm ci` + `npx playwright test --list --project=mutations` — 4 agent + 2 blueprint
  tests discovered, no load errors. `tsc --noEmit` not usable (TypeScript isn't a devDependency;
  pre-existing unrelated errors in `css-go-daisy-scan.spec.ts`).
- PR #16 lane: same Go suite + `go vet`/`gofmt` + `node --check gateway/webui/static/js/chat.js`;
  `playwright --list` shows the agent spec at 5 tests.
- Reconcile pass per lane: re-ran `playwright --list` independently and byte-matched the e2e copy
  constants against `gateway/agent.go` (ASCII apostrophes; sentence fragments match — full sentences
  are built by Go concatenation, so they never appear contiguously in source).
- GitHub CI for all three PRs: build, lint, templ, test, vet — pass.
- **Not run:** the live e2e suite and a signed-in browser pass — dev stack/`.env.e2e` are absent from
  fresh worktrees and the shared checkout belongs to parallel sessions.

## Open questions / follow-ups

- Install-time choice (ask to change provider/model, or install the agent unpinned) was deliberately
  deferred → task `blueprint-model-install-choice`.
- e2e explicit-model fixtures still persist through the API with skip guards (env fragility) → tracked
  by `e2e-model-warning-fixture-via-ui` (drive through the UI model select) and
  `e2e-agent-model-warning-provider-env`.
- The chat-banner e2e covers the zero-provider variant only; the provider-missing-prefix variant is
  unit-tested but not e2e.
- Signed-in browser pass over the new surfaces → extend `verify-agent-model-ui-browser`.
- `docs/spec/03-agent-model.md` still carries the earlier note that the default-display decision-log
  row is "pending the concurrent docs WIP"; this session added the adjacent model-availability row
  (D35) but did not rewrite that note (a parallel docs lane may own it).
- `gateway/AGENTS.md` + the `use-modern-go` skill were missing from the PR #3/#12 worktrees only
  because they landed in master later (#14); no action.

## Tasks

- [blueprint-model-install-choice](../tasks/blueprint-model-install-choice.md) — new: install-time
  choice when a blueprint agent's pinned model is unservable.
- [verify-agent-model-ui-browser](../tasks/verify-agent-model-ui-browser.md) — scope extended to the
  blueprint rows and chat banner.
- [e2e-model-warning-fixture-via-ui](../tasks/e2e-model-warning-fixture-via-ui.md) — existing; our
  fixtures remain API-driven.
- [e2e-agent-model-warning-provider-env](../tasks/e2e-agent-model-warning-provider-env.md) — existing;
  the new tests inherit the same skip guards.
