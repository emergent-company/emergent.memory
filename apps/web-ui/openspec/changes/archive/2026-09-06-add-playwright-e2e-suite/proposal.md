## Why

The Memory web console (gateway) has a large UI surface — agents, chat, documents, objects, schema/blueprints, skills, schedules, backups, usage, settings (providers/voice/devices/approvals/tokens), org tenancy, members/invites, and profile — but almost no browser-level coverage. The only suite is two stale specs (`chat`, `agents`) that run against a mock-memory backend in dev mode, which never exercises session auth, tenancy, or the real dev stack. We want an end-to-end Playwright suite that runs against the full dev installation (dev memory + dev Zitadel + session mode) and walks the real lifecycle: create org → project → provider → run the surface → delete org.

## What Changes

- Add a Playwright e2e suite under `tests/e2e/` targeting the real dev stack (`https://api.dev.emergent-company.ai` + `https://zitadel.dev.emergent-company.ai`), reusing the gateway already running in session mode on this host.
- Authenticate via real Zitadel OIDC using a dedicated test user; credentials come from a gitignored `.env.e2e`.
- Implement the tenant lifecycle as `setup` (login + create org/project/provider) → parallel specs → `globalTeardown` (delete org).
- Deliver in two phases: **Phase 1** bootstraps via API (`page.request` + session) and covers the full read/use surface; **Phase 2** adds UI-driven specs for the org-create, project-create, and provider-config forms themselves.
- No gateway or product code changes — this is test infrastructure only (`skip_specs: true`).

## Capabilities

### New Capabilities

None — no spec-level behavior change. Test infrastructure only; the change opts out of specs via `skip_specs: true` in `.openspec.yaml`.

### Modified Capabilities

None.

## Impact

- `tests/e2e/*` — new Playwright config, helpers, fixtures, specs, and `.env.e2e` (gitignored). Replaces the current mock-memory harness (`mock-memory.mjs`, `run-e2e.sh`) as the primary e2e path.
- `tests/e2e/package.json` — add `@playwright/test` (already present) plus any helper deps (`dotenv`, `@types/node`).
- `.gitignore` — ignore `.auth/`, `test-results/`, `tests/e2e/.env.e2e`.
- Docs — a short `tests/e2e/README.md` documenting how to run the suite and where test creds live.
- No changes to `gateway/`, `memory_bridge/`, or iOS client.

### Out of scope (separate changes)

Voice/LiveKit/STT/TTS (native path, not browser-testable); the iOS client; any gateway behavior change or test-only login shortcut (real OIDC is used).
