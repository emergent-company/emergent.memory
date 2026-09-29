## Why

The email worker fell back to a generic `generateFallbackHTML/Text` body whenever a template was missing or `Render` failed. That fallback only understands `ctaUrl` and `message`; the `project-invitation` job supplies `acceptUrl`, so the delivered invitation contained **no accept link** — yet the job was marked `sent` with `last_error = NULL`. The invitee received an invitation they could not act on and operators saw a successful send (issue #1214).

## What Changes

- The email worker no longer degrades to the generic fallback for a missing or failed-to-render template. A missing template or render error now fails the job (`failed`/`dead_letter` with `last_error`), so the failure is visible and retried instead of silently sent.
- The worker validates required transactional content before sending: a `project-invitation` job must carry a non-empty, absolute `http(s)` `acceptUrl`; an `mcp-invite` job must carry `mcpUrl`/`apiKey`. A job whose required content is missing is failed, never sent.
- Regression tests: a missing/broken invite template, a missing accept URL, and a relative accept URL must all leave the job unsent; the happy path must deliver the accept link and mark the job sent.

## Capabilities

### New Capabilities

<!-- none -->

### Modified Capabilities

- `project-invitations`: the invitation-email delivery contract now requires that a missing/broken template or a missing/relative accept URL fails the job rather than delivering a degraded email and marking it sent.

## Impact

- `apps/server/domain/email/worker.go`: `processJob` fails on a missing/render-failed template and validates required template content; new `failJob` / `validateJobContent` helpers.
- `apps/server/domain/email/worker_invite_content_test.go`: fail-first + regression coverage.
- No schema, API, or migration change.
