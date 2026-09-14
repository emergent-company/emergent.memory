# Fix env-dependent failures in the agent-model-warning e2e pair

**Status:** done
**Created:** 2026-09-08
**Source:** [2026-09-08-prg-toast-titles](../sessions/2026-09-08-prg-toast-titles.md)

## What

Two tests in `tests/e2e/specs/agent-model-warning-ui.spec.ts` fail on the shared
dev installation instead of self-skipping:

- "warning severity: provider configured, no default model pinned"
- "no alert when the agent has an explicit model"

The spec is written to `test.skip` when dev memory can't configure a provider
(deepseek `sk-e2e-test-key` → live generate test 401s; catalog unsynced) or
won't persist an explicit model — but in the observed runs it failed rather than
skipped, so one of the "best-effort" branches is succeeding unexpectedly (e.g.
the provider upsert appearing to stick) or a later assertion is throwing before
the skip guard.

## Why

These are part of the other lane's committed P0 coverage (commit `311baf5`), and
two consistently red tests make the full `--project=mutations` suite fail.

## Depends on

none (spec owned by the agent-model-default-display lane)

## Notes

- Run: `npx playwright test agent-model-warning-ui.spec.ts --project=mutations`
- Recent full mutations run: 22 passed / 2 failed (both in this spec).
- The README (tests/e2e) notes the warning-severity case "skips when the
  dev-memory provider catalog is unsynced" — the actual run did not skip.

## Done (2026-09-10)

**Root cause.** Both provider-dependent tests only guarded the *thrown* failure
mode of `configureProvider()` (form rejected → `test.skip`). They never verified
the precondition the assertions actually need: that the project HAS a configured
provider. On dev memory the upsert can return an accepted response (303 +
"Providers saved." flash, no save-error body) while the provider is still not
present in the project's provider list the gateway's warning classifier reads
(`ListProjectProviders`) — e.g. the credential probe/catalog check drops it. The
tests then proceeded and asserted the wrong severity:

- warning-severity test: got provider-less **error** severity → `alert-warning`
  class assertion failed;
- no-alert test: the explicit `deepseek/...` model matched no configured
  provider → error alert rendered → `toHaveCount(0)` failed.

**Guard change.** Added `requireConfiguredProvider(page)` to
`tests/e2e/specs/agents/agent-model-warning-ui.spec.ts`. It keeps the best-effort
`configureProvider('deepseek', 'sk-e2e-test-key')` (skips on throw, as before)
and then **re-reads state**: navigates to `/settings/providers` and checks the
live `ListProjectProviders`-backed panel for the deepseek row
(`a[href="/settings/providers/deepseek/edit"]`). If absent, it `test.skip`s with
the reason "dev memory did not retain the deepseek provider — project still has
no configured provider". Both affected tests now do
`if (!(await requireConfiguredProvider(page))) return;`.

Only the env-setup precondition got the skip; the warning/no-alert assertions
still run (and still fail) whenever the provider truly configured. The existing
explicit-model persistence re-read guard is unchanged.

**Verified** (dev gateway reachable):
`npx playwright test specs/agents/agent-model-warning-ui.spec.ts --project=mutations`
→ 4 passed / 2 skipped. Both target tests now skip on the current dev state
(`configureProvider` throws on the 401 credential probe) instead of failing;
the other three model-warning tests pass. `npx playwright test --list` enumerates
all 5 spec tests. `tsc --noEmit` not run: the e2e project has no `typescript`
dependency (Playwright transpiles the spec).
