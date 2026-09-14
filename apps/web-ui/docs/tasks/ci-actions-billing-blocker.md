# Fix GitHub Actions billing block (CI jobs never start)

**Status:** proposed
**Created:** 2026-09-10
**Source:** [2026-09-10-agents-responsive-table](sessions/2026-09-10-agents-responsive-table.md)

## What
Restore the org's GitHub Actions billing so CI jobs actually run. On PR #37
every job (`build`, `lint`, `templ`, `test`, `vet`) failed in ~2s without
starting, with the annotation:

> The job was not started because recent account payments have failed or your
> spending limit needs to be increased. Please check the 'Billing & plans'
> section in your settings

## Why
The PR merge gate depends on CI checks passing. With jobs unable to start, no
PR can earn a green check, forcing either a wait or an out-of-band approval to
merge. PR #37 was merged on local evidence + explicit user approval.

## Depends on
- none

## Notes
- Fix is org/billing-side (payment method or spending limit), not a repo change.
- To confirm recovery: open any PR and verify jobs transition past
  "queued"/"not started" — `gh pr checks <n>`.
- Related merge-gate work: [pr-review-bot-wiring](pr-review-bot-wiring.md).
- Interim local verification while CI is down: `go build ./...`,
  `templ generate -check`, `go test ./...`, `golangci-lint run ./...` from
  `gateway/`.
