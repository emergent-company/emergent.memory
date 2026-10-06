# UI recipes — layout & editing patterns

A decision-oriented catalog of the layout and editing affordances shipped in the
web-ui gateway. Use it to pick a pattern by name and to hand an agent an exact
instruction. Each recipe gives **when** to use it, **avoid** when not, a
**ready-to-paste instruction**, a **markup skeleton** built from real components,
the **contract** (ids / `data-testid` / `aria-*`) and **source anchors**.

Companion docs (read those for the *why*, this one for the *which*):

| Doc | Covers |
|---|---|
| `apps/web-ui/docs/ui-patterns.md` | page shell, PRG, htmx ownership, widget inventory, Alpine vs `data-*` |
| `apps/web-ui/gateway/AGENTS.md` | Go/templ style, component layers L0–L4, props, role gating |
| `openspec/specs/web-ui-component-conventions/spec.md` | component-contract law |
| `openspec/specs/inline-form-saving/spec.md` | auto-save behaviour requirements |
| `apps/web-ui/tests/e2e/README.md:119` | `page-<slug>` `data-testid` convention |

Paths are relative to `apps/web-ui/gateway/` unless stated. Line numbers are
verified against `origin/main` when written; **anchors drift — re-grep before
asserting.**

---

## How to use this catalog

1. Find the task in the decision table.
2. Copy the pattern's **Instruction** line into your delegation prompt, naming
   the target entity and file.
3. The agent implements against the **Recipe** + **Contract**.
4. Never invent a fourth variant — if a pattern is *almost* right, extend it
   (documented exception) rather than fork.

### Decision table

| Task | Pattern |
|---|---|
| Collection with filters, sorting, per-row actions | [`layout.list`](#layoutlist--list-page) |
| One entity: header + sections + sub-nav | [`layout.detail`](#layoutdetail--detail-page) |
| Many sibling config sections (a Settings area) | [`layout.settings-rail`](#layoutsettings-rail--settings-area) |
| Summary tiles / label–value facts | [`layout.card-grid`](#layoutcard-grid--cards--meta) |
| Setting auto-persists as the user types/changes it | [`edit.inline-autosave`](#editinline-autosave--form-level-auto-save) |
| One field saves itself on change (own endpoint) | [`edit.inline-htmx`](#editinline-htmx--field-level-inline-save) |
| Multi-field create/edit, fields coupled, explicit commit | [`edit.form-save`](#editform-save--full-page-form--save-prg) |
| Short create/edit without leaving the page | [`edit.modal`](#editmodal--createedit-dialog) |
| Destructive / irreversible confirmation | [`edit.confirm`](#editconfirm--destructive-confirmation) |
| Secondary actions on one row/entity | [`edit.row-menu`](#editrow-menu--row-action-menu) |
| One boolean/state control inline in a row | [`edit.row-action`](#editrow-action--in-row-state-mutation) |
| Report a success/failure without a page change | [`feedback.toast`](#feedbacktoast--toast-feedback) |

---

## Layout patterns

### `layout.list` — list page

**When** a collection needs filters, sorting, an empty state and per-row actions.
**Avoid** when the collection is short and read-only — use a `layout.card-grid`
inside an existing page instead of a dedicated route.

**Instruction:** "Add a `<Entity>` list page using pattern `layout.list`:
`layout.Container(LG)` → `pageHeader` (with a primary action) → a desktop
`components.TableCard` table and a mobile list, with an empty state and
`pageError` on load failure. Follow `AgentsPage`."

**Recipe** (`AgentsPage`, `ui.templ:249`):

```go
templ AgentsPage(agents []AgentDefinitionSummary, models []Model, defaultModel string, loadErr error) {
	@layout.Container(layout.ContainerLG, nil) {
		@pageHeader("Control plane", "Agents", "…subtitle…") {
			// primary create action forwards into nav.PageHeading.Actions
		}
		if loadErr != nil {
			@pageError("Agents unavailable", loadErr)
		} else if len(agents) == 0 {
			@agentsEmpty()
		} else {
			<div class="hidden md:block">@agentsTable(agents)</div>
			<div class="md:hidden">@agentCardGrid(agents)</div>
		}
		@agentFormDialog(agents, models, defaultModel)
		@deleteConfirmDialog()
	}
}
```

Desktop rows: `components.TableCard` → go-daisy `table.*`; mobile rows:
`components.ListRow` (`components/list.templ:10`). Both wrap `PageCard`.

**Contract:** main root carries `data-testid="page-<title-slug>"` (`pageTestID`,
`ui.go:48`); a create button routes to `…/new`; empty state names the collection.

**Source:** `ui.templ:249` (`AgentsPage`), `ui.templ:629` (`pageHeader`),
`ui.templ:224` (`pageError`), `components/table.templ:10`,
`components/list.templ:10`, `ui.templ:385` (`agentsTable`).

---

### `layout.detail` — detail page

**When** one entity with a header, optional sub-navigation rail, and stacked
sections. **Avoid** a hand-rolled header — `detailHeader` is the convention.

**Instruction:** "Add a `<Entity>` detail page using pattern `layout.detail`:
`layout.Container(LG)` → `detailHeader(crumbs, kicker, title, subtitle)` → stacked
`ui.Section`/`components.PanelCard` sections; if it has sub-sections, add a
`layout.Rail` with `components.SettingsRail` + `SubNavItem`."

**Recipe** (synthesised: `detailHeader` from `schedules.templ:213`, rail from
`agent.templ:39`):

```go
@layout.Container(layout.ContainerLG, nil) {
	@detailHeader(crumbsActive(nav.Crumbs("Schedules", "/schedules", a.Name)),
		"", a.Name, "Run this schedule on demand, edit its prompt or cron, or delete it.") {
		// header actions slot
	}
	@layout.Rail(agentSubNav(a.ID, "settings", section), nil) {
		@scheduleEditForm(data.Agent, data.Defs)
	}
}
```

Sub-nav rail: `components.SettingsRail` (`components/nav.templ:34`) drives
`components.SubNav` (`:10`) + `SubNavItem` (`:47`); active item sets
`aria-current="page"`.

**Contract:** `detailHeader` renders breadcrumbs + `h1`; sub-nav items carry
`aria-current="page"` when active; page root keeps the `page-<slug>` testid.

**Known exception:** `objectDetailHeader` (`objects.templ:874`) hand-rolls a
type chip before the `h1` because `detailHeader`'s `Leading` slot renders above
the breadcrumbs. Documented exception — prefer extending `detailHeader` opts.

**Source:** `ui.templ:687` (`detailHeader`), `ui.templ:726`
(`detailHeaderWithTitleLeading`), `components/nav.templ:10-53`,
`schedules.templ:197,212`.

---

### `layout.settings-rail` — settings area

**When** many sibling configuration sections that each deserve a URL and a
shared vertical rail (General / Assistant / Members / Overrides / Providers…).
**Avoid** for fewer than ~3 sections — tabs inside one page are lighter.

**Instruction:** "Add a `<Name>` section to Settings using pattern
`layout.settings-rail`: append a `RailItem` to `settingsSubNav` and render the
page as `layout.Rail(settingsSubNav(active, providersMissing), nil) { … }`."

**Recipe** (`project_settings.templ:35`):

```go
templ settingsSubNav(active string, providersMissing bool) {
	@components.SettingsRail("Settings sections", active, []components.RailItem{
		{Key: "general", Href: "/settings", Icon: "lucide--sliders-horizontal", Label: "General"},
		{Key: "providers", Href: "/settings/providers", Icon: "lucide--plug", Label: "Providers"},
		// Custom: templ.Component for entries with a trailing badge (provider warning)
	})
}
```

A `RailItem` may carry a `Custom templ.Component` for irregular entries (nested
group, trailing warning badge) that `SubNavItem` cannot express
(`components/nav.templ:16`).

**Contract:** one `RailItem` per section; `active` matches `Key`;
role-gated entries are **omitted**, not disabled (see Role gating in
`AGENTS.md`).

**Source:** `project_settings.templ:35` (`settingsSubNav`),
`project_settings.templ:82` (usage), `agent.templ:39` (`agentSubNav`),
`components/nav.templ:34`.

---

### `layout.card-grid` — cards & meta

**When** surfacing summary facts or definition-list metadata. **Avoid** ad-hoc
`<dl>` grids — the label style has one definition.
**Instruction:** "Render the facts with pattern `layout.card-grid`:
`components.MetaGrid` + one `components.MetaRow` per field; use `MetaRowCustom`
for status copy and `components.EmptyDash` for absent values."

**Recipe:**

```go
@components.MetaGrid() {
	@components.MetaRow("Created", createdLabel)
	@components.MetaRow("Owner", ownerLabel, components.MetaRowOpts{Mono: true})
	@components.MetaRowCustom("Status") { @components.StatusBadge(…) }
}
```

For a panel of tiles use `components.PanelCard` (`components/panel.templ:13`) —
`ui.CardRaw` + `memory-density-comfortable`. A one-time accent is passed via
`extraClass` (e.g. `"border-primary/20"`); spacing is owned centrally, never in
the component.

**Contract:** `MetaRow` emits `<dt>` uppercase label + `<dd>` value; empty value
falls back to `EmptyDash` unless `NoEmptyDash`.

**Source:** `components/meta.templ:18,35,55,74`, `components/panel.templ:13`,
`ui.templ:326` (`statRow`), `ui.templ:841` (`cardList`).

---

## Editing patterns

> Cross-cutting: every mutation follows the **PRG contract** (post → 303 /
> `HX-Redirect` → flagged GET → `flashToasts`) and the **htmx-ownership** rule
> (the page owns `hx-target`/`hx-swap`/`hx-trigger`/`hx-include`). See
> `ui-patterns.md` §2–§3. Redirect via `render.RedirectAfterMutation` for
> reachable-by-boost submissions, raw `303` only for provably full-page posts.

### `edit.inline-autosave` — form-level auto-save

**When** a settings form of many *independent*, low-risk fields where the user
expects changes to stick without a Save (the agent General subpage).
**Avoid** for destructive/irreversible actions and for fields that must commit
*atomically as a group* — those keep [`edit.form-save`](#editform-save--full-page-form--save-prg).

**Instruction:** "Make this settings form auto-save using pattern
`edit.inline-autosave`: add `data-agent-autosave` to the `<form>` plus a
`data-autosave-url` POST target (JS falls back to `action`), a
`data-autosave-status` node (icon + `data-autosave-status-label` + retry), keep
each field `name=`. No `hx-*` wiring — the JS owns the request."

**Recipe** (markup; behaviour in `agent-settings.js`):

```html
<form method="post" action="/agents/:id/settings/general"
      data-agent-autosave="general"
      data-autosave-url="/agents/:id/settings/general/autosave">
  <input name="name" class="input w-full"/>
  <select name="model" class="select w-full">…</select>
  <span data-autosave-status data-state="idle" aria-live="polite">
    <i data-autosave-icon class="lucide--cloud-check"></i>
    <span data-autosave-status-label>All changes saved</span>
    <button type="button" data-autosave-retry hidden>Retry</button>
  </span>
</form>
```

Behaviour (`agent-settings.js`): text inputs debounce **700 ms**
(`SAVE_DEBOUNCE_MS`, `:25`); selects/pickers/visibility listbox save on `change`;
an unchanged value never posts (`formSignature`); a failed save keeps edited
values and offers retry; `data-autosave-status` reflects `saving/saved/error`.
Delegated init on load **and** `htmx:after:swap` (`:200,279`), so it survives
swaps.

**Contract:** `data-agent-autosave`, `data-autosave-url` (or `action` fallback),
`data-autosave-status`, `data-autosave-status-label`, `data-autosave-icon`
(`data-autosave-retry` optional); the status node reflects `saving/saved/error`,
not a toast. Debounce is canonical 700 ms, tunable in one place.

**Source:** `webui/static/js/agent-settings.js:25,40,200,279`,
`openspec/specs/inline-form-saving/spec.md`, `agent.templ` General subpage.

---

### `edit.inline-htmx` — field-level inline save

**When** a single field has its own endpoint and should persist on change while
the user stays on the page — project settings name/info/budget, per-model price
overrides. **Avoid** when several fields must validate together.

**Instruction:** "Add inline save to this field using pattern
`edit.inline-htmx`: `hx-post` to the field endpoint, `hx-trigger="change
delay:400ms"` (text) or `change` (select/toggle), `hx-include="this"`,
`hx-swap="none"`. The handler upserts and returns a toast; no navigation."

**Recipe** (`project_settings.templ:256`):

```html
<input id="project-name" name="name" type="text" value={ p.Name } class="input w-full"
       hx-post="/settings/project/name" hx-trigger="change delay:400ms"
       hx-include="this" hx-swap="none"/>
```

Handler returns `toastTrigger(c, "success"|"error", msg)`
(`settings_handlers.go:870`) — an `HX-Trigger` event the shell renders as a
toast. To wrap the field with the shared label/hint/tooltip, use the L3
`settingsField` helper (`project_settings.templ:19`).

**Contract:** stable field `id` == `name` == endpoint last segment; the
response swaps nothing (`hx-swap="none"`); feedback is a toast, not a re-render.

**Source:** `project_settings.templ:19,226,256` (fields),
`project_settings.templ:232,265` (toggle variants),
`settings_handlers.go:870` (`toastTrigger`),
`settings_providers.go:372` (per-model override + `:397` delete).

---

### `edit.form-save` — full-page form + Save (PRG)

**When** a multi-field creator/editor whose fields are coupled, or any action
unsafe to auto-save. **Avoid** using it for a single independent field.

**Instruction:** "Add an edit form for `<entity>` using pattern
`edit.form-save`: `ui.Section` → `<form method="post" action="/…">` with
`form.FormControl` fields, a `components.SaveChangesButton` in
`form.FormActions`, POST → PRG `?updated=1` → `flashToasts`."

**Recipe** (`scheduleEditForm`, `schedules.templ:255`):

```go
templ scheduleEditForm(a *ScheduledAgent, defs []AgentDefinitionSummary) {
	@ui.Section("Edit schedule", ui.SectionProps{Margin: "mb-6", BodyClass: "memory-density-comfortable"}) {
		<form class="flex flex-col gap-4" method="post" action={ templ.URL("/schedules/" + a.ID + "/update") }>
			@form.FormControl("edit-sched-name", "Name", form.LabelAbove, "", "", nil) {
				<input id="edit-sched-name" name="name" type="text" required value={ a.Name } class="input w-full"/>
			}
			@form.FormActions("", "") { @components.SaveChangesButton() }
		</form>
	}
}
```

Page local fields can adopt the Settings look via `settingsField` (label +
`ui.InfoTip`). On success: `render.RedirectAfterMutation(w, r, path)` /
`c.Redirect(303, path+"?updated=1")`; errors: `redirectWithError(c, path, err)`
→ `?err=` → `flashError` → `flashToasts(flashMsg, flashErr)`.

**Contract:** form `action` + `method="post"`; field ids prefixed with the form
(`edit-sched-…`); `components.SaveChangesButton` label "Save changes"; success
flag `?updated=1`/`?created=1`/`?deleted=1`; errors ride `?err=`.

**Source:** `schedules.templ:255` (edit), `schedules.templ:386` (create),
`components/form.templ:37` (`SaveChangesButton`), `ui.go:166,181,188`
(`flashToast`/`redirectWithError`/`flashError`), `ui.templ:1058` (`flashToasts`),
`schedules.go:159`.

---

### `edit.modal` — create/edit dialog

**When** a short create/edit that should preserve page context (agent create).
**Avoid** for long forms (they belong on a page) or nested dialogs.

**Instruction:** "Add a create/edit dialog for `<entity>` using pattern
`edit.modal`: `ui.Dialog` with a stable id, opened by `MemoryApp.openDialog(id)`
from a `data-action` button; use `components.DialogAutoOpen()` when the dialog
must re-open after an htmx swap."

**Recipe** (`agentFormDialog`, `ui.templ:498`, opened at `ui.templ:264`):

```go
templ agentFormDialog(agents []AgentDefinitionSummary, models []Model, defaultModel string) {
	@ui.Dialog(ui.DialogProps{ID: "agent-form-modal", BoxClass: "w-full max-w-2xl"}) {
		<h3 id="agent-form-title" class="text-lg font-semibold">New agent</h3>
		<form id="agent-form" class="flex flex-col gap-4">
			<input type="hidden" id="agent-id" name="id"/>
			@form.FormControl("agent-name", "Name", form.LabelAbove, "", "", nil) {
				<input id="agent-name" name="name" type="text" required class="input w-full"/>
			}
		</form>
	}
}
```

Triggers: `MemoryApp.openAgentForm(id)` (`app.js:180`) for the domain case;
generic `MemoryApp.openDialog(id)` (`app.js:644`) otherwise. Render-time id
(per-row dialogs): `window.openDialogByID("backup-delete-"+id)` (`app.js:656`).
`components.DialogAutoOpen()` (`components/dialog.templ:9`) emits
`data-dialog-autoopen`; `openAutoOpenDialogs` (`app.js:666`) opens those on load
and after every htmx swap.

**Contract:** dialog id stable and passed literally to `openDialog`; title `<h3>`
id; form field ids; auto-open only via `data-dialog-autoopen`.

**Source:** `ui.templ:498` (dialog), `ui.templ:264` (render),
`components/dialog.templ:9`, `webui/static/js/app.js:180,390,644,656,666`.

---

### `edit.confirm` — destructive confirmation

**When** delete or any irreversible action. **Avoid** confirming reversible
actions.

**Instruction:** "Guard the delete with pattern `edit.confirm`: a
`ui.ConfirmDialog`, opened by `MemoryApp.openDeleteConfirm(id, name)`, posting to
the delete route. For an inline form that needs a confirm gate before submit,
add `data-requires-confirm`."

**Recipe** (`deleteConfirmDialog`, `ui.templ:596`; trigger `ui.templ:265`):

```go
templ deleteConfirmDialog() {
	// ui.ConfirmDialog with a stable id; opened via MemoryApp.openDeleteConfirm(id, name)
}
```

Two mechanisms:

- **Dialog confirm** — `MemoryApp.openDeleteConfirm(id, name)` (`app.js:390`)
  opens the shared `deleteConfirmDialog`; confirm performs the DELETE.
- **Inline gate** — `data-requires-confirm` on a `<form>`: `app.js` intercepts
  the submit, shows the confirm dialog, re-submits on confirm
  (`ui-patterns.md` §4, blueprint edit-gate).

**Contract:** destructive control is omitted (role), not disabled; copy names
the object; `components.ConfirmIcon`/`DeleteWarning`
(`components/confirm.templ:14,32`) for the shared warning body.

**Source:** `ui.templ:596` (`deleteConfirmDialog`), `ui.templ:591`
(`deleteAgentDescription`), `components/confirm.templ:14,32`,
`webui/static/js/app.js:390,435,441`.

---

### `edit.row-menu` — row action menu

**When** a row/entity has several secondary actions. **Avoid** for a single
primary action (render a button).

**Instruction:** "Add a row action menu using pattern `edit.row-menu`:
`ui.Popover` with `PopoverTrigger` + `PopoverContent(Placement:
PopoverBottomEnd)`, `role="menuitemradio"`/`aria-checked` for selectable entries,
and `ui.PopoverScript()` once on the page."

**Recipe** (`org_context.templ:458`):

```go
@ui.Popover(ui.PopoverRootProps{}) {
	@ui.PopoverTrigger(ui.PopoverTriggerProps{…})
	@ui.PopoverContent(ui.PopoverContentProps{Placement: ui.PopoverBottomEnd}) {
		// menu items
	}
}
```

`ui.PopoverScript()` (`org_context.templ:93`) initialises once. Runtime close-all:
`window.goDaisy.popover.closeAll()`. For purely CSS menus with no JS state, use
`ui.Dropdown` instead.

**Contract:** trigger has an accessible name; selected items expose
`aria-checked`; positioning via `PopoverContent.Placement`, never inline styles.

**Source:** `org_context.templ:93,458-470`, `ui-patterns.md` §4 (Popover/dropdown
rows).

---

### `edit.row-action` — in-row state mutation

**When** one boolean/state control lives in a list row and should apply
immediately (enable/disable a schedule, "Run now"). **Avoid** for multi-field
edits — link to the detail page.

**Instruction:** "Add an in-row action using pattern `edit.row-action`: a small
`<form method="post" action="/…/toggle">` wrapping the control; a toggle submits
itself via `onchange="this.form.submit()"`. Feedback via PRG."

**Recipe** (`scheduleRow`, `schedules.templ:94`):

```go
<form method="post" action={ templ.URL("/schedules/" + a.ID + "/toggle") }>
	@form.ToggleInput(form.ToggleInputProps{
		Name: "enabled", Checked: a.Enabled, Class: "toggle toggle-sm",
		Attrs: templ.Attributes{"aria-label": "Toggle " + a.Name, "onchange": "this.form.submit()"},
	})
</form>
```

Non-boolean inline actions use a `ui.Button` of `ui.ButtonTypeSubmit` inside a
one-field form (e.g. "Run now", `schedules.templ:120`).

**Contract:** control has an `aria-label` naming the target; one control per
form; server responds through the PRG/flash contract.

**Source:** `schedules.templ:94-140` (`scheduleRow`; the toggle renders go-daisy
`form.ToggleInput`), `ui-patterns.md` §4. Labelled settings rows instead use
`components/form.templ:50` (`ToggleField`) — see `edit.inline-htmx`.

---

## Feedback patterns

### `feedback.toast` — toast feedback

**When** an action (mutation, inline save, async job) should report success or
failure without navigating. **Avoid** for long, actionable, provider-style
messages that need reading room — those use the `memory-alert` modal
(`ui-patterns.md` §4).

**Instruction:** "Report the result with pattern `feedback.toast`: render
`flashToasts(flashMsg, flashErr)` for PRG flows, emit the `memory-toast`
`HX-Trigger` for inline responses, or call `MemoryApp.toast(kind, message)` from
client JS. Severity is load-bearing — pass `error` for failures."

Severity is one rule with one definition per side — never pass an ad-hoc
`duration`:

| Kind | Behaviour |
|---|---|
| `error` | **persistent** — no auto-dismiss; explicit accessible close button; clicking the body does nothing |
| `success` / `info` / `warning` | auto-dismiss after the shared timeout; click-to-dismiss |

The rule lives in `toastDurationMs` (`ui.go:168`, server) and `toastDuration`
(`app.js:139`, client).

**Recipe** (server PRG — `ui.templ:1029`; client — `app.js:135`):

```go
@flashToasts(flashMsg, flashErr) // error-then-success pair; both skipped when unset
```

```js
window.MemoryApp.toast("error", "Save failed: " + err.message); // persists until closed
```

**Contract:** PRG errors ride `?err=<message>` → `flashError` → `flashToasts`;
inline responses use the `memory-toast` `HX-Trigger` with `{kind, message}`;
client code uses `MemoryApp.toast(kind, message)`. Failures pass `kind="error"`.

**Source:** `ui.go:163,168,181,185`
(`flashToast`/`toastDurationMs`/`redirectWithError`/`flashError`), `ui.templ:1029`
(`flashToasts`), `webui/static/js/app.js:135` (`toast`), go-daisy
`components/alpine/lifecycle.go` (`ToastQueueInitPausable`) +
`components/ui/toast-queue.templ`.

---

## Cross-cutting rules (do not duplicate — link)

| Rule | Where |
|---|---|
| PRG contract (`post → 303/HX-Redirect → flagged GET → flashToasts`) | `ui-patterns.md` §2 |
| htmx ownership in components | `ui-patterns.md` §3, `AGENTS.md` |
| Component layers L0–L4 + when a component earns extraction | `AGENTS.md` "UI components" |
| Hide, don't disable (role gating) | `AGENTS.md` "Role gating" |
| Alpine vs `data-*` vs go-daisy policy | `ui-patterns.md` §5 |
| htmx v4 + Alpine same-id restore trap | `ui-patterns.md` §6 |
| Never `innerHTML` untrusted content | `AGENTS.md` "Role gating" |
| Test the contract (ids/`data-testid`/`aria-*`/`hx-*`), not class strings | `AGENTS.md`, `ui-patterns.md` §8 |
| Toast severity (persistent errors + close button) | `ui-patterns.md` §2, `feedback.toast` |

---

## Pattern → source index

| Pattern | Primary source |
|---|---|
| `layout.list` | `ui.templ:249,629,224`; `components/table.templ:10`; `components/list.templ:10` |
| `layout.detail` | `ui.templ:687,726`; `components/nav.templ:10,34,47`; `schedules.templ:197` |
| `layout.settings-rail` | `project_settings.templ:35`; `agent.templ:39`; `components/nav.templ:34` |
| `layout.card-grid` | `components/meta.templ:18,35,55,74`; `components/panel.templ:13` |
| `edit.inline-autosave` | `webui/static/js/agent-settings.js:25,200,279`; `openspec/specs/inline-form-saving/spec.md` |
| `edit.inline-htmx` | `project_settings.templ:19,256`; `settings_handlers.go:870`; `settings_providers.go:372` |
| `edit.form-save` | `schedules.templ:255,386`; `components/form.templ:37`; `ui.go:166,181,188`; `ui.templ:1058` |
| `edit.modal` | `ui.templ:498,264`; `components/dialog.templ:9`; `webui/static/js/app.js:180,644,666` |
| `edit.confirm` | `ui.templ:596`; `components/confirm.templ:14,32`; `webui/static/js/app.js:390` |
| `edit.row-menu` | `org_context.templ:93,458` |
| `edit.row-action` | `schedules.templ:94,120`; `components/form.templ:50` |
| `feedback.toast` | `ui.go:163`; `ui.templ:1029`; `webui/static/js/app.js:135`; go-daisy `components/ui/toast-queue.templ` |

## Adding a pattern (or a variant)

A new recipe earns a place here only if it is a **distinct affordance** with at
least one shipped example — not a one-off page. Before adding: check the decision
table, then the component-conventions layer table, to make sure you are not
describing a fourth variant of an existing pattern. Prefer extending a pattern's
recipe with a documented "variant" over adding a row. Update the decision table,
the `Pattern → source index`, and re-verify anchors.
