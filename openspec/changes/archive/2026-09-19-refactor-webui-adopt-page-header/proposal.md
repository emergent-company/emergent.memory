## Why

Earlier adoption lanes migrated nearly every gateway-local component onto go-daisy. One component stayed behind on purpose: the list-page `pageHeader`. `nav.PageHeading` could not reproduce it byte-for-byte — it always emitted a breadcrumbs region, always added `mt-2` to its flex row, and its only `lg:text-3xl` variant (`Dashboard`) dropped the kicker eyebrow. The list-page header needs none of those: no breadcrumbs, no `mt-2`, and both the eyebrow and `lg:text-3xl`.

Upstream go-daisy PR #18 closed those gaps by adding `HideBreadcrumbs`, `NoTopMargin`, `Flat`, `SubtitleFull`, and kicker hoisting into the `Dashboard` branch. This is the last adoption lane: migrate `pageHeader` onto `nav.PageHeading` and drop the last hand-rolled header markup.

## What Changes

Bump the go-daisy pin to `062842a29737b686ba2cbaeda169bf81bff2cd3c` and repoint `pageHeader` (25 call sites across 13 page templates) at `nav.PageHeading`:

- `pageHeader` becomes a plain Go function (not a templ component) returning `templ.Component`, so its children forward into `PageHeading`'s `Actions` slot via `templ.GetChildren(ctx)` — the same established pattern as `detailHeader`.
- It maps to `PageHeading` with `Title`, `Kicker`, `Subtitle`, `Actions` (children), `Margin: "mb-6"`, `Dashboard: true`, `SubtitleFull: true`, `HideBreadcrumbs: true`, `NoTopMargin: true`, `Flat: true` — the `Flat` + `Dashboard` + no-top-margin combination collapses the wrapper and flex row into the single `<div class="mb-6 flex flex-wrap items-end justify-between gap-4">` element the local header already emitted, with the kicker eyebrow and `lg:text-3xl` title preserved.
- `detailHeader`, the type icon catalog (`type_icons.go`) and its adapters, and every other local component are untouched — they are already in their intended final state.

Rendered output is byte-identical: same single wrapper carrying both margin and flex classes, same class order, no extra `div`, no `mt-2`, kicker present, subtitle with `text-base-content/55 mt-1 text-sm` and no `max-w-2xl`, and the actions slot in the same position.

## Capabilities

### Modified Capabilities

- `web-ui-components`: the detail-page-header requirement is updated so the list-page `pageHeader` now delegates to `PageHeading` (Flat + Dashboard + no-top-margin variant) instead of staying local.

## Impact

- `apps/web-ui/gateway/go.mod`, `go.sum` — go-daisy pin `f64017d5da6c` → `062842a29737`.
- `apps/web-ui/gateway/ui.templ` — `pageHeader` becomes a thin Go-function adapter over `nav.PageHeading`; the local markup (and its stale "stays local" comment) is removed.
- No route, handler, API, schema, or user-visible behavior change (rendered markup preserved byte-for-byte).
