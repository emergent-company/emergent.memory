## Why

Three defects were found while building the agent **Work** settings subpage
(`web-agent-settings`): the form, the copy, and the server resolver disagree
about what a `0` budget means; the retry backoff fields are editable but only
`maxAttempts` is ever consumed; and two smaller value-hygiene problems (an
undeduped comma list, and a `step` constraint that rejects valid stored values)
plus a decorative `Task.priority` property in the task-board blueprint.

## What Changes

- **#1849 — zero budgets are "use the default".** The Work form's `failureLimit`
  and `revisionLimit` inputs accept `0` (the platform-default sentinel) instead
  of rejecting it, matching the server resolver
  (`FailureLimitValue()`/`RevisionLimitValue()` treat `≤ 0` as inherit) and the
  page copy ("zero limits inherit the platform defaults"). A **negative** value
  is still rejected. The generic `workIntField` minimum check now rejects any
  value below the supplied minimum (so `min: 0` means "non-negative") instead of
  skipping the check when the minimum is 0.
- **#1848 — retry backoff gap disclosed honestly.** Only `retryPolicy.maxAttempts`
  is consumed by the work scheduler today. The Work subpage now annotates the
  Retry policy section to say the initial interval, backoff coefficient, and max
  interval are stored but not yet applied, and the capability spec records the
  gap so it is not mistaken for shipped behaviour. (Wiring a real exponential
  schedule was rejected for this batch: it changes requeue semantics and the
  fixed, test-bounded requeue backoff, a larger change than this cleanup.)
- **#1852a — remove decorative `Task.priority`.** The task-board blueprint's
  `Task` type declares a `priority` integer property with no ordering consumer
  anywhere in the server. Removed it (and its seed value and the `task-worker`
  prompt reference) rather than wiring it, because board pickup is driven by
  status/triggers, not priority, and an ordering consumer is a cross-cutting
  change out of scope for this cleanup.
- **#1852b — dedupe comma-separated list input.** `splitCSV` now collapses
  duplicate entries (first-occurrence order preserved), so `requiredDeliverableType`
  and `triggerStatus` cannot carry duplicates.
- **#1852c — backoff coefficient accepts arbitrary precision.** The
  `backoffCoefficient` input switches to `step="any"`, so a value set through the
  API or a blueprint that is not a multiple of `0.1` no longer fails native step
  validation on re-save.

## Capabilities

### New Capabilities

<!-- none -->

### Modified Capabilities

- `web-agent-settings`: budgets accept `0` as "use the platform default" and
  reject negatives; retry policy fields disclose that only max attempts is
  applied and the backoff coefficient accepts arbitrary precision;
  comma-separated Work list fields collapse duplicates.

## Impact

- `apps/web-ui/gateway/agent.go` — `workIntField` minimum semantics;
  `failureLimit`/`revisionLimit` minimum.
- `apps/web-ui/gateway/agent.templ` — budget `min`, backoff-coefficient `step`,
  retry-policy gap note.
- `apps/web-ui/gateway/config.go` — `splitCSV` dedupe.
- `apps/web-ui/gateway/agent_work_settings_test.go`, `config_test.go` — tests.
- `blueprints/task-board/` and
  `apps/server/domain/blueprints/builtin_packs/task-board/` — drop `Task.priority`
  from the schema, seed, and `task-worker` prompt.
