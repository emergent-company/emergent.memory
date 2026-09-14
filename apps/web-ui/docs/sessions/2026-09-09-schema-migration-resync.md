# 2026-09-09 — Schema-migration resync + PR review process

## Goal

Fix the schema-migrations UX after the blueprint-install work, and stand up a formal,
agent-verified PR review process. Three threads:

1. Pure-agent blueprint installs (e.g. `operator`) were bounced to `/blueprints/migrations`
   over unrelated pre-existing drift, and the page contradicted itself (stale alert, no action).
2. The shipped stale-objects card pointed users at a `/blueprints` upgrade that does not exist
   in that state — research established staleness means objects' stored `schema_version`
   **lags the ACTIVE compiled schema** (stamped only at object write), not a pending newer
   blueprint. The real repair is a same-version from==to "resync" migration.
3. GitHub PRs should be verified by checks *and* a dedicated reviewer agent before merge.

## Outcome

Done for the product changes — three PRs merged to master. Partially done for the review bot.

- **PR #2** (merge `43e1577`, commit `f5e23c7`): install redirect scoped to install-caused drift.
- **PR #4** (merge `6d0dd15`, commit `9db12b3`): formal PR review policy doc + `pr-review` skill.
- **PR #7** (merge `f21b5b9`, commit `028d8ce`): same-version resync migration affordance.
- **Review bot: NOT yet live** — GitHub App `emergent-code-reviewer` created + installed
  org-wide, but carries **zero repository permissions** (Perms still need: Pull requests R/W,
  Contents R/W). No token helper, no Paseo schedule, no merge-gate flip yet.

Working tree clean at session end; branch `omos/schema-resync-stale` == merged `028d8ce`.

## Decisions

- **Redirect to migrations only when the install *increased* the stale count** — a pure-agent
  install applies no schema, so pre-existing drift must not bounce the user off the result page.
  Before/after drift captured around the install; message carries both counts.
- **Staleness is same-version lag, not a missing upgrade** — repair is a from==to migration
  that re-stamps conforming objects to the installed version; the `/blueprints` "upgrade the
  owning blueprint" guidance was categorically wrong and was removed from every reachable state.
- **Resync reachable via the migrate form** — when no upgrade path exists and objects are
  stale, `migrationsData` prefills from==to with the most-recently-installed ACTIVE history row
  (`suggestResync`), so memory's from==to `migrate/execute` path (no from!=to check) renders.
  Guard preserved: non-conforming objects are drops → risky/dangerous → execute still blocks
  without Force.
- **Four-state card model** (upgrade / resync / neutral fallback / all-clear) — pure
  `migrationCardKindFor` drives both the top-of-page card and the health-alert copy; copy never
  promises a blueprint upgrade.
- **Process gate, not branch protection** — private repo on the free GitHub plan has no branch
  protection; enforcement is scripted (merge only after CI green + approving review). GitHub Pro
  remains the hard-gate alternative (deferred).
- **GitHub App identity over a machine-user account** — no second email/signup; a distinct bot
  identity is required because a PR author cannot approve or gate its own PR. App creation is
  web-only; the user provisions it, the agent wires the token.
- **PR-first workflow** — global `AGENTS.md` flipped from "No PRs, push directly" to
  "create + merge PRs whenever possible"; merge gate = checks pass + approving review once the
  bot exists, author merges own green PRs only until then.

## Changes

- `gateway/blueprints_handlers.go` (`f5e23c7`) — `uiInstallBlueprint` captures drift before +
  after the install and redirects to `/blueprints/migrations?migrateMsg=…` only when
  after > before; otherwise `/blueprints?installed=1`.
- `gateway/migrations.go` (`f5e23c7` then `028d8ce`) — card-kind model grew from 3 to 4 states
  (added `migrationCardResync`, `migrationCardFallback`); `suggestResync` picks the latest
  ACTIVE history row; `migrationsData` prefills from==to==active when no upgrade path and stale;
  `staleObjectsMessage` keyed to card kind; dead `/blueprints`-promising helper removed.
- `gateway/migrations.templ` (`f5e23c7` then `028d8ce`) — top-of-page switch over the full kind
  set; migrate form reused for resync with a distinct heading ("Objects lag the installed
  schema") + re-stamp helper; `/blueprints` guidance card and "Go to Blueprints" link deleted;
  neutral `migrationFallbackCard` added; history retitled "Schema install history".
- `gateway/migrations_test.go` — `suggestResync` tests (nil / none-active / single-active /
  latest-active / empty-stamp); classification tests updated to the 4-kind set; copy tests assert
  no blueprint-upgrade wording.
- `gateway/blueprints_test.go` + `gateway/handlers_test.go` (`f5e23c7`) — redirect PRG tests
  (pure-agent ignores pre-existing drift, increase→migrations, equal→installed); `fakeMemory`
  gained a per-call `validateFn` hook.
- `docs/spec/04-go-application.md` — migrations prose updated in-change to describe the
  redirect rule and the resync re-stamp behavior.
- `docs/pr-review-policy.md`, `.opencode/skills/pr-review/SKILL.md` (`9db12b3`) — formal review
  contract (merge gate, required PR-body evidence, checklist, comment standards, identity
  runbook) + operational skill.
- `/root/.config/opencode/AGENTS.md` (outside this repo) — PR/merge workflow + worktree rule
  edits; merge gate staged for the review bot.

## Verification

- `templ generate ./...` + `templ generate -check` — clean each round.
- `go build ./...`, `go vet ./...` from `gateway/` — clean each round.
- `gofmt -l` — empty; `golangci-lint run ./...` — 0 issues.
- `go test -count=1 ./...` from `gateway/` — **853 PASS** (main pkg) + webui ok (resync round);
  earlier rounds 790 PASS. All CI checks green on PRs #2 / #4 / #7 before merge.

## Open questions / follow-ups

- **Review bot is half-provisioned** — finish: set App repo perms (Pull requests R/W, Contents
  R/W), optionally restrict the install to `memory.web-ui`, write the short-lived-token helper
  (JWT via the App private key at `/root/Emergent Code Reviewer Private Key Sept 9 2026.pem`,
  App id `4884315`, installation `160306576`), create the Paseo review schedule, flip the
  AGENTS interim rule to bot-merge, live-test. Tracked in the
  [pr-review-bot-wiring](../tasks/pr-review-bot-wiring.md) task.
- GitHub Pro upgrade would give a hard branch-protection gate (required reviews/checks) instead
  of the scripted process gate — deferred by choice, revisit if enforcement is bypassed.
- Multiple reviewer lanes (role-split reviewers) and/or a human-signoff requirement are
  future variants of the process — noted in the policy, not yet built.

## Tasks

- [pr-review-bot-wiring](../tasks/pr-review-bot-wiring.md) — finish the GitHub App review-bot wiring
