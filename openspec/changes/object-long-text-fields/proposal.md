## Why

An object whose string property holds a very long value (a `Law` with ~41,594
characters of `content`) renders as a cramped single-line box. The field is
actually an auto-growing `<textarea data-autogrow>`, but `app.js` only runs
`autogrowAll()` on `DOMContentLoaded` — and the normal navigation flow clicks a
link from `/objects`, where htmx (with `hx-boost="true"` on `#main-content`)
swaps the main content region without firing `DOMContentLoaded`. The freshly
swapped textarea therefore stays at `rows="1"` and its long value is hidden
behind a one-row scroller.

Two more gaps make long free-form text hard to work with even on a direct load:
the field caps at `max-h-60` with `resize-none` (no way to grow it to a
comfortable reading size), and there is no character count for a value that can
run to tens of thousands of characters.

## What Changes

- **Autogrow after HTMX swaps**: re-run auto-grow (and the new counter
  initialization) on `htmx:after:swap`, so textareas swapped into `#main-content`
  by boosted client-side navigation grow on arrival exactly as on a full load.
- **Usable long-text fields**: the multi-line string field becomes taller by
  default (`min-h-20`), capped higher (`max-h-96`), and vertically resizable
  (`resize-y`); a manual resize is preserved across the next keystroke's
  auto-grow.
- **Live character counter**: every multi-line text field shows a muted,
  digit-grouped count at its bottom-right ("41,594 characters"), seeded
  server-side and kept current on input. Single-line inputs, selects, numbers,
  dates, booleans, enums, and TagLists show no counter.

Schema widget handling is unchanged: `widget: "input"` still renders a
single-line input, `widget: "textarea"` (or the Auto default) the multi-line
field.

## Capabilities

### Modified Capabilities

- `object-creation`: the schema-driven create form's "Type-aware property inputs"
  requirement says a `string` property "uses a plain text input"; it now renders
  an auto-growing, vertically resizable multi-line field with a live character
  count, with the single-line/enum/other-widget exclusions made explicit.
- `object-browser`: the "View an object's details" requirement gains the
  contract that long free-form string properties present as usable multi-line
  fields and that client-side (htmx-boosted) navigation into the detail view
  initializes them the same way a direct page load does.

## Impact

- `apps/web-ui/gateway/objects.templ` — new `longTextarea` helper (single source
  for the multi-line field + counter), used by `propertyInput`'s string path and
  the un-schema'd-property fallback in `objectPropertiesCard`.
- `apps/web-ui/gateway/objects.go` — `charCountLabel` / `groupDigits` helpers for
  the server-rendered initial count.
- `apps/web-ui/gateway/webui/static/js/app.js` — `htmx:after:swap` autogrow +
  counter initialization, delegated counter updates, and resize preservation.
- `apps/web-ui/gateway/objects_test.go` — render/no-render assertions for the
  counter and the count formatting.
- No server-side, schema, API, or migration change.
