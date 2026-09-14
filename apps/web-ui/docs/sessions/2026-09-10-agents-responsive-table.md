# 2026-09-10 — Agents page: desktop table, mobile cards

## Goal
The Agents list (`GET /agents`) had been changed to a card grid at every
viewport. The cards were only ever meant for mobile. Restore the previous data
table on desktop (md and up) while keeping the cards on mobile (below md).

## Outcome
Done. Merged to `master` via PR #37 (squash `d070081`).

- `AgentsPage` now renders both surfaces: `agentsTable` behind a
  `hidden md:block` wrapper (desktop) and `agentCardGrid` behind `md:hidden`
  (mobile). Empty/error branches unchanged.
- Restored `agentsTable`/`agentRow` from the pre-removal revision
  (`960be51^`): columns Agent / Model / Tools / Flow / Visibility / Updated /
  actions, in the same `ui.CardRaw(card-border …)` + go-daisy `table` shell.
- Model column reads `AgentDefinitionSummary.EffectiveModel`; empty renders the
  dash. No `(default)` suffix (signal dropped with the summary-only payload).
- Row actions restored: edit (`/agents/<id>/settings`) + delete
  (`openDeleteConfirm`).
- `TestAgentModelDisplay` case 5 rewritten to assert the responsive contract
  (both wrappers, mobile card affordances, desktop headers + row actions, model
  text present, no `(default)`).

## Decisions
- **Desktop table, mobile cards** — user: the card change should apply to
  mobile only; desktop keeps the table it had before. Supersedes D31 for the
  viewport split (D31 card *design* still governs mobile).
- **Reuse the historical table verbatim** — minimal risk, restores the exact
  prior desktop UX rather than re-designing.
- **Model from `EffectiveModel`, not `agentDefs`/`defaultModel`** — those params
  were removed in `0959346` (N+1 drop); the list summary now carries the
  resolved model, so no lookup is reintroduced.
- **Drop the `(default)` tag** — no explicit-vs-resolved signal remains in the
  summary payload; showing it would guess.
- **Merged PR #37 with red CI** — all GitHub Actions jobs failed to *start*
  (org billing: payments failed / spending limit). Local build/templ/test/lint
  green; user explicitly approved merging anyway.

## Changes
- `gateway/ui.templ` — added `table` import; `AgentsPage` responsive split;
  restored `agentsTable` + `agentRow` (summary-based); card grid scoped to
  mobile (comment only).
- `gateway/agent_ui_test.go` — `TestAgentModelDisplay` case 5 asserts both
  breakpoints, table headers/actions, model rendering, and absence of
  `(default)`.
- `gateway/ui_templ.go` — regenerated (gitignored).

## Verification
- `cd gateway && go build ./...` — ✓
- `cd gateway && templ generate -check` — ✓ (0 updates)
- `cd gateway && go test -count=1 ./...` — ✓ (`memory.web-ui`, `webui`)
- `cd gateway && golangci-lint run ./...` — ✓ 0 issues
- `task lint` — ✗ could not run: `lefthook: executable file not found in $PATH`
  (lefthook not installed on this host); ran gofmt/vet/golangci-lint/templ
  directly instead.
- CI on PR #37 — ✗ all jobs not started (billing); see follow-up.
- Browser md-breakpoint check — not run (browser host is the user's machine).

## Open questions / follow-ups
- Verify the responsive split in a signed-in browser at/below/above the `md`
  breakpoint — folded into the existing
  [verify-agent-model-ui-browser](../tasks/verify-agent-model-ui-browser.md)
  task (its item 1 covers the Agents list model surface).
- GitHub Actions cannot run until the org billing/spending-limit issue is
  resolved — tracked in
  [ci-actions-billing-blocker](../tasks/ci-actions-billing-blocker.md).
- `task lint` is unusable on hosts without `lefthook`; consider a documented
  fallback (gofmt + go vet + golangci-lint + `templ generate -check`).

## Tasks
- [ci-actions-billing-blocker](../tasks/ci-actions-billing-blocker.md) — resolve org billing so CI jobs start
- [verify-agent-model-ui-browser](../tasks/verify-agent-model-ui-browser.md) — updated to cover the responsive Agents surface (no `(default)` tag)
