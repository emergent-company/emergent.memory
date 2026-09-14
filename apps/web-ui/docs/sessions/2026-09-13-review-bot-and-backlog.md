# 2026-09-13 — Review bot + backlog hardening sweep

## Goal

Take the accumulated session-archive/backlog and drive the highest-priority work to
completion: triage priorities, execute self-serve items, and — separately requested — wire a
real GitHub-app review bot so PRs are gated by a distinct reviewer identity instead of author
self-merge.

## Outcome

**Done.** ~25 PRs shipped across two repos, plus a live review bot.

Gateway (`emergent-company/memory.web-ui`):
- **#35** provider-rate panel strips vendor prefix in the model-only rate fallback.
- **#38** archived 8 fully-complete OpenSpec changes + synced specs; reconciled `documents-delete-ui` (already shipped in `f214011`).
- **#39** registry-sourced blueprint upgrades surface + "Update available" CTA on installed rows.
- **#43** Zitadel RP-initiated logout (`end_session`).
- **#44** agent-model-warning e2e self-skips when the env precondition isn't met.
- **#45** org-level (`org_admin`) invite from `/orgs`.
- **#48** member role change via remove + re-invite.
- **#49** per-object schema-drift detail + pack-aware resync (Preview→Execute preserved).
- **#52** provider model seeding = strict generative superset (interim for classification parity).
- **#54** org rename UI; **#75** review-bot wiring; **#87** embedding-provider guidance callout.
- Docs reconcile PRs: **#36, #40, #50, #76, #91, #94**.
- Throwaway **#74** proved the bot identity (approve + merge), then deleted.

Memory (`emergent-company/emergent.memory`):
- **#413** embedded static pricing is canonical; dead remote fetch removed.
- **#414** genai embedding usage reporting (`usageReportingClient`).
- **#415** migration archive keys are human versions; rollback stamp fixed; `schema_migration_runs` INSERT fixed.
- **#416** `PATCH /api/orgs/:id` (name) + ratchet paydown.
- **#418** implemented `RestoreTypeRegistry` per design D5 (removed dead `_history` stub).
- **#422** ownership-scoped registry reconcile on rollback (type-rename case) + `to_version` semantics.
- **#425** server-side avatar normalize (crop/re-encode/flatten).

Review bot:
- `tools/gh-app-token.sh` mints the `emergent-code-reviewer` GitHub App installation token.
- `docs/pr-review-policy.md` + `pr-review` skill updated to the App identity.
- Paseo schedule `pr-review-bot` (`*/10 * * * *`) reviews/merges open `memory.web-ui` PRs.
- `/root/.config/opencode/AGENTS.md` merge rule flipped: authors no longer self-merge; the bot is the merge actor.

## Decisions

- **Static pricing list is canonical** — `emergent-company/model-pricing` 404s; option 2 (drop the dead remote fetch) chosen over republishing.
- **`RestoreTypeRegistry` implemented via design D5** (re-install from-pack types + delete owned to-only rows) — no new `_history` table; the stub had diverged from its own design.
- **Rollback archive keys are human versions**, matching `ValidateObjects` and the gateway's `to_version`; `to_version` documented as "the migration target being undone".
- **Org rename is name-only** — memory `Org` has no description column; description/logo deferred.
- **Member role change = remove + re-invite** — memory exposes no member-role PATCH.
- **Provider classification option 3 (strict superset)** as the cheap interim bound; option 2 (memory-side ephemeral catalog resolve) remains the durable fix.
- **Review bot uses the existing GitHub App**, not the unprovisioned `emergent-alfred-bot` PAT route (App perms were already set).
- **lint-ratchet baselines are never raised**; the observed `origin/main` failure was transient and self-resolved.
- **Gateway CI is infra-false-red** (Actions billing) — the bot verifies locally at the reviewed head and merges on green.

## Changes

- `gateway/settings_providers.go` — `stripVendorModelName` fallback; strict-superset seeding; embedding-provider capability helpers.
- `gateway/blueprints.{go,templ}` — registry upgrade collapse + update CTA.
- `gateway/oidc.go`, `session.go`, `account.go` — `end_session` logout.
- `gateway/org_context.{go,templ}`, `org_members_*` — org invite, rename, member role editing.
- `gateway/migrations.{go,templ}` — drift detail + pack-aware resync.
- `gateway/project_settings.templ` — embedding-provider guidance callout.
- `tools/gh-app-token.sh`, `docs/pr-review-policy.md`, `.opencode/skills/pr-review/SKILL.md` — review bot.
- `docs/spec/09-security.md` — SSO logout now wired.
- Memory: `domain/provider/pricing_sync.go`, `domain/schemas/{service,repository}.go`, `domain/userprofile/avatar_normalize.go`, `domain/orgs/*`, `pkg/embeddings/genai/client.go`.
- Docs/tasks: status reconciles + new tasks (below).

## Verification

- Every gateway PR: `templ generate && go build ./... && go test ./... && golangci-lint run ./...` — green (local).
- Every memory PR: `go build ./... && go vet ./... && go test ./...` + `scripts/lint-ratchet.sh` — green; required `ci` check green in Actions.
- Review bot: throwaway PR #74 approved + merged by `app/emergent-code-reviewer`; #75/#76/#87/#91/#94 merged by the bot thereafter.
- Gateway Actions jobs do **not** run (org billing), so gateway PRs merge on local verification via the bot.

## Open questions / follow-ups

- **`deploy-embedding-pricing-usage` blocked** — `emergent-memory` host doesn't resolve from this server; needs the `mcj-one` host or a human to roll out + verify.
- **`verify-public-zitadel-auth`** — live Zitadel/HTTPS posture check still outstanding.
- **Rollback archive backfill** — objects migrated before #415 keep UUID-keyed archive entries and still won't roll back.
- **Org description/logo** — needs a `kb.orgs` column + migration.
- **Provider catalog server-side resolve** — durable alternative to the mirrored classification.
- **Review-bot schedule reliability** — runs are long (4–30 min) and one was cancelled; consider cadence/timeout tuning.
- **CI billing** — until fixed, the gateway's formal CI gate is unusable.

## Tasks

- [deploy-embedding-pricing-usage](../tasks/deploy-embedding-pricing-usage.md) — blocked on host reachability.
- [verify-public-zitadel-auth](../tasks/verify-public-zitadel-auth.md) — live auth posture.
- [ci-actions-billing-blocker](../tasks/ci-actions-billing-blocker.md) — gateway Actions not starting.
- [rollback-archive-backfill](../tasks/rollback-archive-backfill.md) — new.
- [org-description-logo](../tasks/org-description-logo.md) — new.
- [provider-catalog-server-resolve](../tasks/provider-catalog-server-resolve.md) — new.
- [pr-review-bot-reliability](../tasks/pr-review-bot-reliability.md) — schedule slow/canceled runs (already tracked).
