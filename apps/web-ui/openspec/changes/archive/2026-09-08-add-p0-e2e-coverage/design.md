## Context

E2E harness (archived changes `add-playwright-e2e-suite`, `e2e-full-surface-coverage`) already established: real Zitadel OIDC login via `auth.setup.ts` → API bootstrap of org+project+provider → `storageState`; `chromium` read project (parallel) + serial `mutations` project (`*-ui.spec.ts`); `globalTeardown` deletes the bootstrap org (only on `E2E_RESET=1`). 29 specs green. Existing specs follow API-seed → UI-act → API-verify → `finally`-cleanup (`project-transfer-ui.spec.ts` is the model for the transfer dialog, `object-create-ui.spec.ts` for labels/TagList).

Target behaviors all shipped (see proposal.md): agent model warning (`9c573b7`), account menu identity + email backfill, org projects table bulk delete, org row-click activate, org Settings hub + 301, mobile spotlight, go-daisy `@source` CSS scan fix.

## Goals / Non-Goals

**Goals:**
- Deterministic e2e pins for the seven P0 behaviors, green against the running dev gateway (tailnet URL, session mode).
- Reuse existing fixtures/helpers/projects — no new harness projects unless a parallel/serial conflict forces one.
- Prefer API-side state setup so each spec is independent of UI preconditions already covered elsewhere.

**Non-Goals:**
- No product behavior change; test-only `data-testid` only where a semantic locator is proven unstable.
- P1/P2 coverage (cookie Max-Age, provider model dropdown, model catalog, hx-boost title, switcher layout) — separate change.
- No live-LLM chat assertions, no visual screenshot regression testing.

## Decisions

### Fixture strategy — each spec owns its state via API
- Follow `project-transfer-ui.spec.ts`: `test.beforeAll`/per-test API setup (create org/project/agent via `page.request`), teardown in `finally`, tolerate already-deleted (idempotent).
- **Agent model warning needs a provider-less project.** Bootstrap org already configures a provider (best-effort in setup). Spec creates a *fresh project without provider* → error severity; adds provider + project default → warning/absent transitions. If provider config is best-effort-flaky on dev memory (catalog not synced), gate severity assertions on whether the API provider upsert succeeded (skip sub-asserts otherwise) — matches archived change's "best-effort" precedent.
- **Projects table bulk delete** is async (memory returns 202): assert row removal via `expect.poll` with generous timeout rather than strict immediate absence; keep flash-message assertion loose ("deleted"/"started") per session-log note.
- **Account menu email backfill**: read expected email from the profile API (`/api/profile`) rather than hardcoding; assert dropdown text equals it and profile card line 2 (email) ≠ line 1 (name). If test user's Memory profile has no email, assert the known fallback rendering instead — record actual behavior in the spec.

### Mobile viewport + CSS regression guard
- `spotlight-mobile.spec.ts` + `css-go-daisy-scan.spec.ts` set `page.setViewportSize({width: 390, height: 844})` at test start (no device-descriptor project needed).
- CSS assertions use computed styles via `locator.evaluate` (`mask-image !== "none"`, checkbox `offsetWidth/offsetHeight === 0` / `opacity === "0"`), not screenshots — deterministic, mirrors the manual DOM checks the session log used.
- Palette full-screen check: bounding box ≈ viewport; X close: `#spotlight-toggle` unchecked after click → palette gone. Esc-close regression on desktop viewport kept in the same spec (both viewports in one file).

### Selectors — prefer existing hooks, add minimal testids
- Use existing: `page-*` roots, `data-row-menu-trigger`, `project-switcher`, `account-menu`, `chat-input`, semantic roles/`name=` forms.
- Account-menu trigger: if topbar avatar lacks a stable hook (post-chevron-removal it's a bare rounded button), add one `data-testid` (`account-avatar-trigger`) — sole permitted DOM addition; document in `tests/e2e/README.md` convention section if added.
- Agent warning alert: assert via role `alert` + text ("no provider"/"default model") rather than a new testid, since severity differs by content.

### Project placement
- `css-go-daisy-scan.spec.ts` + mobile spotlight are **read/visual** — but spotlight toggles state and both need viewport control; mutations project is serial and self-cleaning. Spot-check current config: specs asserting only rendering/computed CSS (no org mutation) go to `chromium`; anything creating/deleting orgs/projects/agents goes to `mutations`. Decide per file when writing; default to `mutations` when in doubt (serial = no cross-spec interference).
- Self-clean every org/project created outside the bootstrap org (mirror `project-transfer-ui` finally pattern); specs that only read the bootstrap tenant need no cleanup.

## Risks / Trade-offs

- **Provider-less fresh project may inherit org/provider context** (session is scoped org+project; a project under an org with configured providers may still see them). → Verify scoping against the dev gateway when writing the spec; if provider resolution is org-level, invert the test (create a fresh *org* + project with no provider) and accept slower setup.
- **Alert severity depends on model-catalog state** (memory 400 "no models in catalog" precedents). → Keep provider-config assertions API-verified first; mark affected asserts skip-if-unsynced.
- **Bulk-delete 202 latency** → `expect.poll` + tolerant messaging; no `page.waitForTimeout` sleeps.
- **CSS regression guard couples to Tailwind/go-daisy compile output** (class-starved sheet would fail mask-image assert — that's the point). Risk is false-fail on legit icon swap → assert a `lucide--*` mask utility present AND checkbox hidden, not a specific icon class, to reduce brittleness.
- **Mutation-project serialization** — 6 UI specs added serial lengthens runtime. → Keep each spec's API setup lean; reuse bootstrap project where the target doesn't require isolation.

## Open Questions
- None blocking. Exact dev-memory provider-resolution scope (org vs project) and test-user profile email presence confirmed empirically while writing the two affected specs — answers adjust assertions only, not scope.
