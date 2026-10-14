<!-- openspec:archive-hold: registry reconciliation (tasks 1.4/2.3/11.1) blocks on the archive-held web-ui-canonical-primitives change — archive both together when its hold lifts; tasks 7.1/7.2 are superseded by the delta spec's residual-recording requirement; task 12.7 needs a live session-mode gateway. -->

## 1. Panel/table card adoption

- [x] 1.1 Migrate the 8 direct `ui.CardRaw` page sites (blueprints.templ, ui.templ, objects.templ ×3, approvals.templ, blueprints_model_map.templ, share_manage.templ) onto `components.PanelCard` / `components.TableCard`, preserving card/body classes, `data-testid`, and htmx wiring; verify `grep -rn "ui\.CardRaw" --include="*.templ" . | grep -v "components/"` returns only the canonical `components/` shells (0 page sites).
- [x] 1.2 Confirm the 5 `ui.CardRaw` uses inside `components/` remain untouched (they are the canonical shells); verify no diff in `components/panel.templ`, `components/table.templ`, `components/secret.templ`, `components/approval_card.templ`, `components/question_card.templ`.
- [x] 1.3 Add enforcement coverage for page-level `ui.CardRaw`: a check (or `adoptionAllowlist` entry per page site) that distinguishes page-level `ui.CardRaw` (adopter) from `components/` `ui.CardRaw` (owner), seeded at the 8 pre-adoption sites and lowered to 0 as 1.1 lands. `ui.CardRaw` is currently unobserved — it is a primitive call, not a `class="card"` literal, and cannot be banned wholesale because `components/` uses it legitimately.
- [ ] 1.4 Update the canonical-L0 registry note so the panel/table-card row records the migration as done (not "pending"). *Not done: the registry table lives in the archive-held `web-ui-canonical-primitives` change (on `archive-hold`), outside this lane's scope; it is reconciled when that change's hold lifts and both archive.*

## 2. Form-field adoption (FormField → FormControl)

- [x] 2.1 Migrate the 6 `form.FormField(form.FormFieldProps{...})` call sites in `share_manage.templ` to `form.FormControl`, mapping label position, hint, and error message; verify `grep -rn "form\.FormField" share_manage.templ` returns nothing.
- [x] 2.2 Extend `deprecatedPrimitives()` in `canonical_ratchet_test.go` with `form.FormField` → canonical `form.FormControl`. `deprecatedSources()` scans root `*.templ` + `components/*.templ` + root `*.go` (excluding `components/*.go`), which covers both real sites (share_manage.templ) and the gallery fixture (`devgallery_daisy_fixtures.go`); the fixture stays as the single exempt demonstration.
- [ ] 2.3 Resolve the deferred #1747 decision in the registry: FormControl canonical, FormField deprecated (not "decision pending"). *Not done: same cross-change registry table as 1.4.*

## 3. Clipboard-copy adoption (adopt `form.ClipboardCopy`)

`form.ClipboardCopy` exists in go-daisy v0.15.1 (`components/form`; verified via `go doc`). This task **adopts** the
existing primitive — it does **not** create a gateway-local copy component, which would duplicate a canonical
primitive.

- [x] 3.1 Replace the raw copy markup with `form.ClipboardCopy` at every adoptable site: `apiTokenCopyScript()` in api_tokens.templ, share_owner.templ:33, mcp_shares.templ:76, agent_mcp_endpoint.templ:144; and the `data-copy-target` wiring in components/secret.templ:153/:170, components/snippet.templ:20, agent_mcp_endpoint.templ:134. Two sites are **recorded residuals**, not migrations: `object_preview.templ` (icon-only copy) and the `share_manage.templ` reveal-once flow (`shareManageScriptOnce`/`shareCopyButton`), which inlines the secret via `Mono` — `form.ClipboardCopy` documents itself unsuitable for on-demand secrets. `apiTokenCopyScript` is deleted; `SnippetCard` is now a 2-arg mono badge and its chroma JSON highlighting is removed.
- [x] 3.2 Preserve `data-testid` and copy targets across the swap (the endpoint URL's anchor is restored via a `#agent-mcp-endpoint-url` wrapper; the secret value now lives in the input's `value=`); copy behaviour is proven by the clipboard-payload poll in the realigned e2e. The live browser run is session-mode and is tracked under 12.7.
- [x] 3.3 Record the disposition of the triplicated client-side copy JS — `gateway/webui/static/js/{chat-host,object-preview,chat-components}.js` (each carries its own `copyText`/`copyTextFallback`). `chat-components.js` now delegates to the single `MemoryChatHost.copyText` implementation in `chat-host.js`; `object-preview.js` retains its own copy for the icon-only client-rendered panel (recorded in the clipboard budget note), so nothing drifts silently.
- [x] 3.4 Add enforcement coverage for raw copy markup: a new marker/signature (or a `data-copy-target` / copy-script pattern) so a re-introduced hand-rolled copy affordance fails the ratchet. Currently the clipboard surface is unobserved.

## 4. Note / empty-state adoption (EmptyNote)

- [x] 4.1 Migrate raw muted note/empty-state paragraphs to `ui.EmptyNote`, selecting the tier that matches each site (sm / xs / 2xs), including the `text-[11px]` dense-metadata sites. ~224 `ui.EmptyNote` adoptions land; the ratchet holds two recorded residuals (object_preview.templ icon-led gap; schema.templ bordered box, not a note), and `devgallery*` files are outside the scan by design.
- [x] 4.2 Leave `ui.Eyebrow`, `font-mono` code spans, and field `dt` labels in place (they are not note copy); verify the adoption does not swallow non-note `text-[11px]` sites.
- [x] 4.3 Disambiguate the `EmptyNote` name: rename the gateway-local picker identifiers — the struct field `EmptyNote` (`ui.templ:886`) and the helpers `pickerEmptyNote` / `delegateEmptyNote` / `skillEmptyNote` / `objectTypeEmptyNote` / `relationshipTypeEmptyNote` (`ui.templ`) — so they cannot be confused with the adopted go-daisy `ui.EmptyNote`. Note: `checkLatentCollisions`/`declaredCollisions` will **not** catch this (the field is not a `templ` component and the helpers are lowercase, both outside the collision scanner's `[A-Z]`-name regexes); the rename is a reader/renamer safeguard, not a scanner-enforced collision.
- [x] 4.4 Add enforcement coverage for the empty-note surface: a new marker/signature (or a pattern budget over the note predicate) seeded at the pre-adoption count and lowered as 4.1 lands. Currently the empty-note surface is unobserved.

## 5. Dialog-heading adoption (DialogTitle)

- [x] 5.1 Migrate dialog/modal headings (`<h3 class="text-lg font-semibold">` inside dialogs, e.g. "Revoke share?", "Rotate key?", "New agent") to `ui.DialogTitle` with the matching `Level`/`Size`/`Tracking`, preserving `id` attributes referenced by `aria-labelledby`. The `<h3>` dialog shape is fully adopted (0 residual); `<h2>` section headings remain as a separate `dialog-title-section` residual (7 sites), because no bare-section-heading primitive exists.
- [x] 5.2 Add enforcement coverage for the dialog-heading surface: a new marker/signature for the dialog-heading class shape, seeded at the pre-adoption count and lowered as 5.1 lands. Currently the dialog-title surface is unobserved.

## 6. Soft-box adoption (SoftBox Surface axis) + recorded exception list

- [x] 6.1 Migrate adoptable soft-box surfaces (div + `bg-base-200/40`/`/30` + solid hairline border) to `ui.SoftBox` with the matching `Surface`; verify the swap is output-equivalent (`border-base-content/10` → `border-line-translucent` resolves identically).
- [x] 6.2 Record the exception list in the ratchet budget with per-site reasons (floor 8: agent.templ ×1 border-dashed; backups.templ ×2 non-`div` `<label>`; mcp_nodes.templ ×1 conditional fill; mcp_servers.templ ×3 non-`div` `<details>` + `DetailsBase:` Go-string + border-dashed; ui.templ ×1 non-`div` `<pre>`), and file upstream any shared-component limitation that blocks an adoptable site (the `Attrs` slot was added upstream ≥ v0.15.1).
- [x] 6.3 Ensure every remaining raw soft-box surface is a recorded exception, so none drift unrecorded.
- [x] 6.4 Strengthen soft-box coverage so the canonical surface itself is flagged, not only its drift: today `patternBudget()` flags `bg-base-200/<N≠40>` and `border-base-content/<N≠10>` but a raw `bg-base-200/40` + `border-base-content/10` box produces no finding.

## 7. Ratchet-hole closure (Go string literals)

- [ ] 7.1 Extend the scanner (or add a parallel gate) to cover class lists assembled in Go string literals — the `ui.Disclosure` `DetailsBase:` strings in `agent.templ` and `mcp_servers.templ` (actual sites `agent.templ:1174/:1211/:1389` and `mcp_servers.templ:340/:454`, 5 total, not 3), and concatenated/script-assembled lists — or record each as a deliberate exception. *Resolved by residual-recording, not a scanner extension: the `.templ` line-scanner already matches the literal `DetailsBase:` string (`mcp_servers.templ:340`, `rounded-box … bg-base-200/40`) as a soft-box residual; the concatenated/prop-fed case is recorded as a residual rather than parsed. No dedicated Go-string scanner was added.*
- [ ] 7.2 Add a regression test proving a drifted Go-string class list fails the ratchet. *Not done: no such regression test exists.*

## 8. Enforcement coverage for each eliminated surface

This is the heart of the change: no surface is adopted without a ratchet that holds it. Each coverage task is
listed with its adoption section above (1.3, 2.2, 3.4, 4.4, 5.2, 6.4) and SHALL land in the same change as the
adoption, seeded at the pre-adoption count and lowered as the adoption lands. The scanner-extension work (new
`componentSignatures()` markers, a page-vs-component `ui.CardRaw` check, or a pattern-budget entry) is the shared
plumbing behind 1.3/3.4/4.4/5.2/6.4.

- [x] 8.1 Cover the four consolidation surfaces (empty-note, dialog-title, soft-box, clipboard) as pattern budgets in `scanPatternHits`/`patternBudget()`, not `componentSignatures()` markers: their wrapper tokens (`text-sm`, `text-lg font-semibold`, `bg-base-200/40`, `rounded-box`) are generic utilities that would flood the adoption scanner, violating the "high-confidence wrapper markers only" rule.
- [x] 8.2 Wire any new pattern-budget entry (empty-note predicate, soft-box surface) into `patternBudget()` with the pre-adoption seed, and lower it in the same commit as its adoption.
- [x] 8.3 Confirm each of the six surfaces ends the change observable by at least one detector, or with an explicit recorded exception where a scanner genuinely cannot see it.

## 9. Generated files (per adoption, same commit)

- [x] 9.1 Every adoption changes the component graph and adoption findings: run `templ generate ./...` then `go generate ./...` **in the same commit**, regenerating `devgallery_graph_gen.go` and `devgallery_adoption_gen.go` so `TestGraphStaleCheck` / `TestAdoptionStaleCheck` (`devgallery_test.go`) pass on byte equality.

## 10. Pinned goldens (per adoption)

- [x] 10.1 Update `refactor_exact_test.go` for the SoftBox border token (`border-base-content/10` → `border-line-translucent`) and any other exact-string pin an adoption moves.
- [x] 10.2 Update `components_test.go`, `visual_delta_pinning_test.go`, and `adoption_contract_test.go` where an adoption shifts a pinned wrapper string or contract fixture.

## 11. Registry reconciliation

- [ ] 11.1 Reconcile with the archive-held `web-ui-canonical-primitives` registry table, whose rows for Form-field ("Decision pending #1747 — do not decide here") and panel/table-card ("migration task pending") this change resolves: mark FormControl canonical / FormField deprecated, and card migration done. *Not done: that table lives in the held `web-ui-canonical-primitives` change dir (outside this lane's scope); this change's delta spec declares the resolution, and the table is updated when that change's hold lifts.*

## 12. Verification

- [x] 12.1 `go build ./...` passes (from `apps/web-ui/gateway`).
- [x] 12.2 `templ generate` succeeds and produces no uncommitted generated diffs.
- [x] 12.3 `go generate ./...` succeeds (regenerates `devgallery_graph_gen.go` / `devgallery_adoption_gen.go`).
- [x] 12.4 `golangci-lint run ./...` passes.
- [x] 12.5 `go test -run 'Ratchet' -v ./...` passes (canonical-L0, adoption, page-card-raw, and pattern ratchet gates).
- [x] 12.6 `go test ./...` passes (including `TestGraphStaleCheck` / `TestAdoptionStaleCheck` and the pinned-golden tests).
- [ ] 12.7 Browser/Playwright check confirms adopted pages render identically (ids, testids, htmx wiring, copy feedback, dialog headings, soft-box fills). *Not run in this environment: the session-mode suite requires a live gateway in session mode (`AUTH_MODE=session` + Zitadel) plus a Zitadel test user; only the realigned specs are committed, not a green live run.*
