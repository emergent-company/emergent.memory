# Tasks — refactor-webui-component-adoption

Pure markup refactor: every item preserves rendered output. Run commands from `apps/web-ui/gateway` unless stated otherwise. Generated `*_templ.go` are gitignored — run `templ generate`, never stage the output.

## 1 — EmptyDash dedup

- [x] 1.1 Delete the local `emptyDash` in `ui.templ` (~1034-1037) and repoint its four callers (`ui.templ` ×2, `sessions.templ`, `documents.templ`) at `components.EmptyDash`. Verify: no `emptyDash` symbol remains; `go test ./...` green.

## 2 — ConfirmIcon

- [x] 2.1 Add `components.ConfirmIcon(icon string)` in `components/confirm.templ` (trash glyph default via `cmp.Or`, matching `components/secret.go`). Verify: render test asserting the icon circle + default glyph.
- [x] 2.2 Replace the six inline icon circles (`ui.templ` confirmDeleteDialog, `mcp_shares.templ` revoke, `agent_mcp_endpoint.templ` revoke ×2, `mcp_nodes.templ` remove, `project_settings.templ` save-error) with `components.ConfirmIcon`, passing `lucide--alert-triangle` where the original used it. Verify: all six render the identical markup.

## 3 — MetaGrid

- [x] 3.1 Add `components.MetaGrid(opts ...MetaGridOpts)` (with `GapY` and `ColsClass`) to `components/meta.templ`, preserving the exact class order `grid grid-cols-1 gap-x-6 {gap-y} sm:grid-cols-2 {cols}`. Verify: `go test ./...` green.
- [x] 3.2 Replace the four definition-list grids: `backups.templ` (`gap-y-4` + `lg:grid-cols-3`), `org_members_ui.templ` (`gap-y-4`), `project_settings.templ` (`gap-y-3`), `schema_packs.templ` (`gap-y-3`). The second `<dl>` in `backups.templ` (the bordered settings grid) stays local — it carries `mt-4 border-t pt-4`, a different treatment.

## 4 — Raw modal shells → modalShell

- [x] 4.1 Convert `deriveVersionDialog` (`blueprints.templ`), `deriveWarningDialog` and `deriveBlueprintDialog` (`schema.templ`) to `ui.templ` `modalShell`, passing the `aria-labelledby`/`aria-describedby` via attrs. Preserve every id, `data-dialog-open`/`data-dialog-close` wiring, and the `data-requires-confirm` opener. Verify: `schema_ui_test.go` assertions (ids, `data-dialog-open`, `action=`) green.

## 5 — Raw bordered table shells → TableCard

- [x] 5.1 Convert the `agent_mcp_endpoint.templ` key-list and session-list shells (`rounded-box border-base-200 border`) to `components.TableCard`, preserving `data-testid` (via attrs) and the `mt-4`/`mt-3` margin (via a wrapper). Keep the raw `<table class="table table-sm">` markup — the go-daisy `table.*` primitives would add a `w-full` and a nesting level, changing output. Verify: `agent_mcp_endpoint_handlers_test.go` list assertions green.

## 6 — ToggleField adoption (skipped where shape doesn't fit)

- [x] 6.1 Audit the four audited toggle rows. **All four stay local** — none fits `ToggleField`'s (name, label, tip, checked, attrs) shape:
  - `mcp_servers.templ` `mcpServerRowActions`: a `toggle toggle-sm` checkbox inside a custom `<label title=…>` with `aria-label` + `data-mcp-*` attrs and a custom `text-base-content/60 text-xs` caption — no id/name, no tip.
  - `mcp_servers.templ` `mcpServerToolRow`: a bare `toggle toggle-sm shrink-0` checkbox with `data-mcp-*` attrs and no label at all (name is an adjacent mono span).
  - `schedules.templ`: a bare form-submit toggle (`onchange="this.form.submit()"`, `toggle-sm`, `aria-label` only) inside a `<form>` — a row action, not a labelled settings field.
  - `objects.templ`: a bare boolean property toggle paired with a hidden `value="false"` input and `value="true"` — no label, no tip.
  Converting any would change rendered markup. `agent.templ` is owned by another lane and is untouched.

## 7 — Raw optgroup loops → SelectOptionGroups

- [x] 7.1 Add `schemaSelectGroups(history, selectedID) []components.SelectOptionGroup` as the call-site adapter in `migrations.templ`, and replace the two `from`/`to` optgroup loops with `components.SelectOptionGroups(schemaSelectGroups(…))`. Verify: rendered optgroup/option markup identical.

## 8 — Raw badge → StatusBadge

- [x] 8.1 Convert the `@ui.Badge` "Revoked" in `api_tokens.templ` to `components.StatusBadge("Revoked", ui.BadgeError, "lucide--ban", …)`, preserving the XS size and `data-testid`. Verify: `api_tokens_ui_test.go` "Revoked" assertions green.

## 9 — Stale comment

- [x] 9.1 Replace the stale `toast.go` comment claiming `alpine.ToastQueueState` marshals nil as `null` (upstream now seeds a non-nil slice) with an accurate note.

## 10 — Verification + PR

- [ ] 10.1 Gate: `templ generate ./...` && `go build ./...` && `go test ./...` && `task lint` (from `apps/web-ui/gateway`) — all clean.
- [ ] 10.2 Commit the change artifacts + implementation on `refactor/webui-components-adoption`, push, open one PR to `main` linking `openspec/changes/refactor-webui-component-adoption/`. Author does not self-merge.
- [ ] 10.3 Post-merge: `openspec archive refactor-webui-component-adoption`.
