# 2026-09-10 — go-daisy component backport + adoption

## Goal
Analyze the gateway's hand-rolled/duplicated UI components, decide which are generic enough to backport into the shared `go-daisy` library, then adopt upstream components and consolidate local duplication. Guiding signal: repeated markup/helpers = extraction candidate.

## Outcome
Done. Analyzed ~30 templ pages; shipped **two go-daisy releases** and **three merged app PRs**. Read-surface e2e green (44/45 → 45/45 after the stale-spec fix).

- go-daisy **v0.11.0** — `layout.Container`, `layout.Rail`, `shared.RelativeTime` (PR #7, merge `f997456`).
- go-daisy **v0.12.0** — `ui.AvatarFull` (size 6/7/14, muted tone, initials/icon fallback priority) (PR #8, merge `7adc200`).
- memory.web-ui **PR #22** (merge `1d20a21`) — Container/Rail/RelativeTime adoption + FormControl + Button + local consolidation.
- memory.web-ui **PR #24** (merge `0a569ab`) — chipRow merge, picker unification, AvatarFull adoption.
- memory.web-ui **PR #29** (merge `4587a5d`) — fixed the stale providers e2e spec.

## Decisions
- **Backport only generic + absent; adopt existing; keep domain local** — `Container`/`Rail`/`RelativeTime`/`AvatarFull` were added upstream; existing `form.FormControl`/`ui.Button` were adopted instead of reinventing; domain-specific duplication (11 status→intent maps, `*CountLabel`, collapsible tool groups, checkbox pickers) stayed app-local rather than pushing app vocabulary into the shared lib.
- **Release real minor tags, not pseudo-versions** — `v0.11.0`/`v0.12.0`. The app already consumed a pre-`v0.10.1` pseudo; tagging `v0.10.1` at a different commit would break the module-proxy invariant.
- **Adopt `AvatarFull` via thin local wrappers with documented deltas** — byte-identical output was impossible; kept `shrink-0` + font-size on the wrapper (font-size inherits into the inner badge), accepted proportional fallback icon (`size-[55%]`) and `img object-cover`/name-only alt.
- **Skip rather than force** — `components/modal`, `layout.Sidebar`, `form.StructuredInput`, and two pickers were left local because upstream could not preserve the current UX/wire format (see follow-ups).
- **Merge past billing-blocked CI with `--admin`** — org GitHub Actions jobs never start ("spending limit"); approved explicitly by the user, unrelated to code.
- **Verify via a throwaway master worktree** — the shared dev server served another branch and e2e is pinned to the tailnet origin (OIDC), so an isolated `origin/master` worktree with symlinked `.env`/`node_modules` + regenerated vendor was used, then removed.

## Changes
**go-daisy (`github.com/emergent-company/go-daisy`)**
- `components/layout/container.templ`, `components/layout/rail.templ`, `components/layout/boundary.go` — new `Container(size)` + `Rail(nav)`.
- `shared/reltime.go` — new `RelativeTime(iso)`.
- `components/ui/avatar.templ`, `components/ui/boundary.go` — `AvatarFull(AvatarProps{Name,Src,Icon,Size,Mask,Tone,Fallback,Attrs})`; legacy `Avatar`/`PlaceholderAvatar` unchanged (delegate, byte-identical).

**memory.web-ui — PR #22** (29 files, +544/−3655; the −3060 is the stale generated file untrack)
- `gateway/*.templ` (20 files) — `mx-auto max-w-{6xl,4xl} p-6 lg:p-8` → `layout.Container` (56 sites); 18 settings rail shells → `layout.Rail`; 27 inline `class="btn …"` → `ui.Button`.
- `gateway/project_settings.templ` — `settingsField` → `form.FormControl` (InfoTip inlined).
- `gateway/ui.go` — deleted local `relTime`/`itoa` (now `shared.RelativeTime` with a thin raw-iso fallback wrapper); collapsed 7 `*CountLabel` helpers into `countLabel(n, singular, plural)`.
- `gateway/agent.templ` — parameterized `agentToolServerGroup`/`agentToolRelayGroup` → `agentToolGroup`.
- `gateway/org_members_ui_templ.go` — untracked (aligns with the `*_templ.go` gitignore rule).
- `gateway/go.mod` — pin `go-daisy v0.11.0`.

**memory.web-ui — PR #24** (10 files, +246/−190)
- `gateway/ui.templ` — `renameChipBlock` merged into `chipRow(label, margin, wrapped)`; new `checkboxPicker` replacing `skillPicker`/`agentDelegatePicker`.
- `gateway/account_menu.templ`, `org_members_ui.templ`, `org_context.templ` — `ui.AvatarFull` adoption (account/member/org-row/profile) via thin wrappers.
- `gateway/go.mod` — pin `go-daisy v0.12.0`.

**memory.web-ui — PR #29** (1 file)
- `tests/e2e/specs/settings/settings-providers.spec.ts` — state-agnostic (asserts `a[href="/settings/providers/new"]` + `Add [your first] provider`, both empty-CTA and populated states).

## Verification
- go-daisy: `templ generate` · `go build ./...` · `go test ./...` — green; `golangci-lint run ./...` reports pre-existing unrelated findings only (new files clean). `go list -m …@v0.11.0` / `@v0.12.0` — resolved via proxy.
- gateway: `templ generate` · `go build ./...` · `go test ./... -count=1` (incl. `refactor_exact_test.go`) · `golangci-lint run ./...` — 0 issues (both lanes).
- Playwright read-surface (`--project=chromium`, real dev tenant, master server): **44 passed / 1 failed**; the failure was the stale providers spec (byte-identical on the prior branch — pre-existing drift from `ed4ef4a`/`61662eb9`). After PR #29: targeted spec **2 passed**.
- Independent post-merge check: `origin/master` pin `v0.12.0`, `AvatarFull` present, 0 stray `*_templ.go`.

## Open questions / follow-ups
- Adoptions skipped for upstream API mismatch: `components/modal`, `layout.Sidebar`, `form.StructuredInput`, api-token scope picker + toggle tool rows → [adopt-remaining-go-daisy-components](../tasks/adopt-remaining-go-daisy-components.md).
- go-daisy polish: `Rail` hardcoded `11rem` column, `RelativeTime` parse-error fallback divergence, avatar visual parity → [godaisy-component-polish](../tasks/godaisy-component-polish.md).
- Org Actions billing still blocks all PR CI → existing [ci-actions-billing-blocker](../tasks/ci-actions-billing-blocker.md).
- Manual/visual QA of the settings rail + page containers beyond the e2e read-surface → existing [emptystate-visual-qa](../tasks/emptystate-visual-qa.md) / [verify-project-settings-browser](../tasks/verify-project-settings-browser.md).

## Tasks
- [adopt-remaining-go-daisy-components](../tasks/adopt-remaining-go-daisy-components.md) — adopt/defer the components skipped for API mismatch.
- [godaisy-component-polish](../tasks/godaisy-component-polish.md) — Rail width, RelativeTime fallback, avatar parity.
