## 1. Build-input hygiene

- [ ] 1.1 Key `vendor/` generation to the pinned dependency version instead of directory-absence: change `Taskfile.yml:27` so the vendored go-daisy tree is regenerated when the version pinned in `go.mod` differs from the one already vendored (not only when the directory is missing). `vendor/` is untracked (`apps/web-ui/gateway/.gitignore:3`) and is a genuine CSS build input — `webui/css/app.css` `@source`s the vendored components and `@import`s the vendored `components/css/custom.css` (`Taskfile.yml:22-27`).
- [ ] 1.2 Document and enforce a reproducible policy for the tree, since it is gitignored: CI SHALL run the CSS build (which validates/regenerates the tree for the pinned version) rather than depending on a skipped optional test. Add a check that fails when a present `vendor/` tree disagrees with the `go.mod` pin, wired into the CSS build path. Unit-level companion: `apps/web-ui/gateway/vendor_consistency_test.go`, skipping only when `vendor/` is absent, with a comment recording that CI (no `vendor/` present) is covered by the CSS-build regeneration instead.
- [ ] 1.3 Confirm after 1.1 that a fresh `task css` regenerates the tree at the pinned commit. **Do not use `go build -mod=vendor ./...` as the check** — verified it exits 1 ("inconsistent vendoring") even with a freshly regenerated tree, because the gateway module lives in the repo-root `go.work` workspace and workspace mode requires `go work vendor` to cover the whole module graph. The vendored tree is a CSS build input only (`@source`/`@import`); Go compilation resolves from the module cache. Verify the pin instead, via 1.2's test.
- [ ] 1.4 Do **not** edit the vendored `custom.css`. It is generated from go-daisy and any local edit is reverted by the next `go mod vendor`. Its brace nesting and dead daisyUI-v4 variables are upstream defects.
- [ ] 1.5 Keep the `/static/*` mount and verify why: `webui/webui.go:3-5` documents it as intentional ("go-daisy's own static files keep their /static/* mount"), and go-daisy's `layout`, `alpine`, and `stimulus` components emit live `/static/js/*` URLs, so removing the mount would silently break any future adoption of them. Assert only that no page references go-daisy's pre-compiled `/static/css/app.css` (already covered at `css_consolidation_test.go:45`), and record the decision here so the earlier "stop serving the bundle" intent is not mistaken for an omission.
- [ ] 1.6 Delete the dead `.row-actions` / `tr[data-row]:hover .row-actions` rules (`webui/css/app.css:1082-1088`) after re-confirming zero references across `*.templ`, `*.go`, and `*.js`.
- [ ] 1.7 Verification: `templ generate ./...`, `go build ./...`, `go vet ./...`, `go test ./...`, `task lint`, `task css` from `apps/web-ui/gateway` all succeed; compiled CSS is byte-identical across two builds. (`go build -mod=vendor` is deliberately excluded — see 1.3.)

## 2. Radius

- [ ] 2.1 Migrate literal radius in our templates to the semantic utilities: `rounded-box` for containers, cards, and tiles; `rounded-field` for controls, rows, and inline chips. Leave `rounded-full` where it is a deliberate circle (avatars, status dots, pills).

  **Scope corrected by measurement** (the audit's list understated it): the real inventory is **53 sites across 28 templates** — 34 `rounded-lg`, 8 `rounded-md`, 5 `rounded-sm`, 3 `rounded-xl`, 3 `rounded-2xl`. Count with `grep -rE "rounded-(sm|md|lg|xl|2xl|3xl)" --include=*.templ . | grep -v _templ.go`. The audit's enumerated list (20 `rounded-lg` etc.) is a subset; drive the migration from the grep, not from that list.

  **Ordering constraint:** three test files pin these class strings — `refactor_exact_test.go`, `agent_ui_appearance_test.go` (asserts `rounded-lg` is *absent*), and `visual_delta_pinning_test.go`. This task therefore fails until 2.4 narrows those assertions, so 2.1 and 2.4 SHALL land in the same commit and be verified together.

  **Note:** 2.3's bridge already makes these literals follow the theme, so this task is role-clarity cleanup (letting the bridge be deleted once upstream converts), not a functional fix.

  Reference sites from the audit (subset): `api_tokens.templ:423`, `skills.templ:302`, `share_page.templ:397,398,462`, `mcp_servers.templ:417`, `project_settings.templ:40`, `agent.templ:59,497`, `share_manage.templ:512,521,564`, `components/nav.templ:19`, `org_context.templ:845`, `mcp_shares.templ:353`, `approvals.templ:69`, `ui.templ:183,1026`, `schedules.templ:320`, `backups.templ:91,95,436,442,481,505`, `proposal.templ:232`, `migrations.templ:294,305,316,326,335`.
- [ ] 2.2 Convert the card-like radius literals in `app.css` to theme variables: `#chat-rail .list-row:1127`, `.dock-card:663`, `.todo-card:745`, `.memory-queue-row:575`, `.dock-question-option:722`, `.memory-run-marker-failure:448`, `.memory-md pre:975`, blockquote `:1000`, `.memory-copy-btn:508`, queue-input focus `:595`, todo status `:792` → `var(--radius-box)` / `var(--radius-field)` / `var(--radius-selector)`.
- [ ] 2.3 Add the temporary `@theme inline` radius alias mapping Tailwind's radius namespace to the semantic tokens, so the literal radius still present in library markup follows the theme until upstream is converted. Declare it once, mark it as a bridge with a comment naming the upstream work that removes it, and do not rely on it for gateway-owned markup.
- [ ] 2.4 Update the class-only assertions that pinned the old literal radius (`visual_delta_pinning_test.go`, `adoption_contract_test.go`) by **narrowing** them to contract attributes rather than re-pinning the new class strings, per the component conventions.

## 3. Density

- [ ] 3.1 Remove the component-local padding overrides on daisyUI roots: `components/panel.templ:14` `p-5`, `components/table.templ:11` `p-0`, `components/secret.templ:109` `p-5`, `blueprints.templ:308` `p-0`, `ui.templ:306-308` `p-4`, `share_manage.templ:225,416`, `approvals.templ:28,45`.
- [ ] 3.2 Declare density in one documented block in `app.css`, using the component-internal variables (`--card-p`, `--btn-p`) and layered overrides. The block SHALL state that daisyUI provides no padding token, that the levers are component internals / layered overrides, and that it must be re-checked on a daisyUI upgrade.
- [ ] 3.3 Document `html{font-size:15px}` (`app.css:140`) as the deliberate global rem lever that scales all theme geometry, so it is not mistaken for an accident.

## 4. Page background

- [ ] 4.1 Give the page background one source: consolidate `app.css:137` and the three inline FOUC guards (`ui.templ:77`, `auth_ui.templ:59`, `share_page.templ:141`) onto one token-derived declaration.
- [ ] 4.2 Keep the two metadata pairs distinct and co-located with what they mirror: the web manifest `background_color` (`static/manifest.webmanifest:8`) mirrors the page-background token; the manifest `theme_color` (`:9`) and the `theme-color` meta tags (`ui.templ:35`, `auth_ui.templ:25`, `share_page.templ:111`) mirror the surface token. Annotate both as literals that cannot read CSS variables and must be updated together.

## 5. Stylesheet consolidation

- [ ] 5.1 Reduce the JS-injected duplicate rules in `webui/static/js/chat-components.js:172,473` to a documented, tested subset. It cannot simply be deleted: the active `error-contrast-remediation` change adds a `web-ui-css` requirement that the injected badge rules stay in sync with `app.css`, so this task makes the subset explicit and keeps the sync test green.
- [ ] 5.2 Introduce a muted-text scale (~4 tokens replacing the 12 opacities) and a canonical icon size set (`size-3.5/4/5`), then migrate the call sites; unit/style test asserts no opacity outside the scale is used in the gateway templates.
- [ ] 5.3 Token-ize the remaining hardcoded colours: the four `#0E1017` FOUC guards (`ui.templ:77`, `auth_ui.templ:59`, `share_page.templ:141`, `app.css:137`), `.memory-monogram` (`app.css:265-272`), `.chat-bubble-neutral` (`app.css:298`), `.memory-md pre` (`app.css:970`), and the JS `#fff` mix (`chat-components.js:203`). Also reconcile the rendered-markdown/hairline literals (`app.css:950,964,1017` and the `oklch(1 0 0 / x)` hairlines).
- [ ] 5.4 De-triplicate the inline FOUC `<style>` guard into one shared head partial.
- [ ] 5.5 Move the page-local `<style>` blocks that duplicate shared idioms (`chat.templ:270`, `auth_ui.templ:60-129`, `share_page.templ:142-239`) into the shared stylesheet, keeping each page's self-contained-shell intent.
- [ ] 5.6 Replace the hand-rolled badge/button/card/drawer/modal classes the audit listed with daisyUI or the shared components (`.memory-rail-badge`, `.dock-count`, `.todo-item-status`, `.dock-card`, `.memory-queue-send`/`-remove`, `.memory-copy-btn`, `.login-card`, `.share-rail`, `#sidepanel-modal-box`).

## 6. Verification

- [ ] 6.1 Run `task css` **first** — the compiled-CSS assertions in `css_consolidation_test.go` skip when `webui/static/css/app.css` is absent, so `go test ./...` alone does not exercise them.
- [ ] 6.2 `task css` twice → byte-identical output.
- [ ] 6.3 `templ generate ./...`, `go build ./...`, `go vet ./...`, `go test ./...`, `task lint` all pass from `apps/web-ui/gateway`.
- [ ] 6.4 `task e2e:test` passes (this unit changes the compiled CSS every page depends on).
- [ ] 6.5 Manual theme check: change `--radius-field` and `--radius-box` in the theme block, run `task css`, and confirm buttons, fields, cards, dialogs, and badges all change with no template edit.
- [ ] 6.6 Manual density check: change the density block once and confirm component padding changes across pages with no per-component edit.
- [ ] 6.7 Confirm no literal radius remains on controls or containers (grep for the Tailwind radius scale in gateway-owned markup and CSS).
