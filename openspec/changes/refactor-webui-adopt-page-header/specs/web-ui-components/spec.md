## MODIFIED Requirements

### Requirement: Detail-page headers render through nav.PageHeading

The detail-page header (breadcrumb trail, optional kicker eyebrow, h1, optional subtitle, right-side actions) SHALL render through `nav.PageHeading`, and the gateway's `detailHeader` SHALL be a thin adapter delegating to it. The list-page `pageHeader` SHALL also render through `nav.PageHeading` as a thin adapter (Flat + Dashboard + no-top-margin variant), which now reproduces it byte-for-byte.

#### Scenario: detailHeader delegates to PageHeading

- **WHEN** a detail header is rendered in its default, dashboard, bare, kicker, subtitle-full, or title-adornment variant
- **THEN** it emits the same breadcrumbs, title column, subtitle, and actions markup, with the header's children forwarded into `PageHeading`'s `Actions` slot

#### Scenario: Leading variant stays local

- **WHEN** a detail header uses the `Leading` option (an agent icon tile before the title column)
- **THEN** the gateway has no local `detailHeaderLeading`, and the leading component is forwarded into `PageHeading`'s `Leading` slot (rendered before the breadcrumbs and the h1), with `Leading` ignored when `Bare`

#### Scenario: pageHeader stays local

- **WHEN** the list-page `pageHeader` is rendered
- **THEN** the gateway has no local `pageHeader` markup — `pageHeader` is a thin adapter over `nav.PageHeading` (Flat + Dashboard + no-top-margin variant) that emits the same single `<div class="mb-6 flex flex-wrap items-end justify-between gap-4">` wrapper, the kicker eyebrow and `lg:text-3xl` title, no breadcrumbs region, no `mt-2`, and the subtitle rendered full-width (`text-base-content/55 mt-1 text-sm`, no `max-w-2xl`)
