## Why

Two adoption lanes already moved most gateway-local components onto go-daisy. Two non-catalog local components were deliberately deferred because the library lacked the required slots: `agentToolDisclosure` (the tool-picker `<details>` shell) had no summary-attributes slot, hardcoded `bg-base-200/40` on the details, and hardcoded `gap-2 border-base-content/10 p-3` on the body; and `detailHeaderLeading` (the agent-dashboard header with an icon tile before the title) had no PageHeading equivalent for its pre-title leading element.

Upstream go-daisy (`f64017d`) now closes both gaps: `ui.Disclosure` gains `DetailsBase` / `SummaryBase` / `BodyBase` (each non-empty value REPLACES the hardcoded base list) plus `SummaryAttrs`, and `nav.PageHeading` gains a `Leading` slot plus `HideBreadcrumbs`. This final lane adopts those two remaining components and deletes the local helpers.

## What Changes

Bump the go-daisy pin to `f64017d5da6ce212f3d94c417a881a2a240c36e7` and repoint the two remaining call sites:

1. `agentToolDisclosure` (2 call sites: the capability group and the source/relay group) → `ui.Disclosure(ui.DisclosureProps{...})`. The capability group maps `GroupClass: "group/cap"`, `ItemsStart: true`, `Attrs` (data-testid/data-tool-group), and `SummaryAttrs` (data-testid="tool-group-header-*"); the source/relay group maps `DetailsBase: "rounded-box border "+BorderClass` and `BodyBase: "flex flex-col border-t "+BodyBorderClass` (no unwanted gap-2/p-3). The local `agentToolDisclosure` helper is deleted.
2. `detailHeaderLeading` (1 call site: the agent-dashboard header) → `nav.PageHeading`'s `Leading` slot. `detailHeader` forwards `Leading` directly (ignored when `Bare`, matching prior behaviour); the local `detailHeaderLeading` and its `detailHeaderTitleRow` wrapper are deleted.

`pageHeader` stays local (PageHeading always adds `mt-2` and its only `lg:text-3xl` variant — Dashboard — drops the kicker eyebrow); the type icon catalog (`type_icons.go`) and its adapters stay local by design.

## Capabilities

### Modified Capabilities

- `web-ui-components`: the detail-page-header requirement is updated so the `Leading` variant now delegates to `PageHeading.Leading` instead of staying local; a new requirement covers the tool-group disclosure rendering through `ui.Disclosure`.

## Impact

- `apps/web-ui/gateway/go.mod`, `go.sum` — go-daisy pin `8ce69ca4cdd1` → `f64017d5da6c`.
- `apps/web-ui/gateway/agent.templ` — delete `agentToolDisclosure`; `agentToolCapabilityGroup` and `agentToolGroup` repoint to `ui.Disclosure`.
- `apps/web-ui/gateway/ui.templ` — delete `detailHeaderLeading` and `detailHeaderTitleRow`; `detailHeader` forwards `Leading` into `PageHeading`; `pageHeader` comment updated.
- No route, handler, API, schema, or user-visible behavior change (rendered markup preserved; the dashboard leading tile now renders via PageHeading's Leading slot before the breadcrumbs).
