## MODIFIED Requirements

### Requirement: Detail-page headers render through nav.PageHeading

The detail-page header (breadcrumb trail, optional kicker eyebrow, h1, optional subtitle, right-side actions) SHALL render through `nav.PageHeading`, and the gateway's `detailHeader` SHALL be a thin adapter delegating to it. The list-page `pageHeader` SHALL stay local because `nav.PageHeading` cannot reproduce it byte-for-byte.

#### Scenario: detailHeader delegates to PageHeading

- **WHEN** a detail header is rendered in its default, dashboard, bare, kicker, subtitle-full, or title-adornment variant
- **THEN** it emits the same breadcrumbs, title column, subtitle, and actions markup, with the header's children forwarded into `PageHeading`'s `Actions` slot

#### Scenario: Leading variant stays local

- **WHEN** a detail header uses the `Leading` option (an agent icon tile before the title column)
- **THEN** the gateway has no local `detailHeaderLeading`, and the leading component is forwarded into `PageHeading`'s `Leading` slot (rendered before the breadcrumbs and the h1), with `Leading` ignored when `Bare`

#### Scenario: pageHeader stays local

- **WHEN** the list-page `pageHeader` is rendered
- **THEN** it emits no `mt-2` on its flex row and always renders the kicker eyebrow alongside the `lg:text-3xl` title — which `PageHeading` cannot reproduce (it always adds `mt-2`, and its only `lg:text-3xl` variant, Dashboard, drops the eyebrow)

## ADDED Requirements

### Requirement: Tool-group disclosures render through ui.Disclosure

The collapsible tool-picker group shell (capability groups and source/relay groups) SHALL render through `ui.Disclosure`, and the gateway SHALL NOT carry its own `agentToolDisclosure`.

#### Scenario: Capability group maps to Disclosure props

- **WHEN** a capability tool group is rendered
- **THEN** it calls `ui.Disclosure` with `GroupClass: "group/cap"`, `ItemsStart: true`, `Open` from the group's open state, `Attrs` carrying `data-testid="tool-group"` and `data-tool-group=<id>`, and `SummaryAttrs` carrying `data-testid="tool-group-header-<id>"`, with the capability leading block as the header and the capability controls as the trailing slot

#### Scenario: Source/relay group replaces the details and body bases

- **WHEN** a registry-server or relay-node tool group is rendered
- **THEN** it calls `ui.Disclosure` with `DetailsBase: "rounded-box border "+<BorderClass>` and `BodyBase: "flex flex-col border-t "+<BodyBorderClass>`, so the per-variant border/background tone is preserved and no unwanted `gap-2` or `p-3` is added to the body

#### Scenario: Tool-group markup is preserved

- **WHEN** a tool group is rendered
- **THEN** every `data-testid`, `data-tool-group`, form field `name`, `value`, and `checked` state asserted by `agent_ui_test.go` survives unchanged, and the chevron rotation (`group` vs `group/cap`) still works
