## Purpose

Defines the gateway's page-level navigation cues: which header idiom a page of a given class must use,
the vocabulary of page kickers, and how completely the command palette covers the navigation.

The settings sub-navigation rail is **not** defined here — it is owned by the `settings-navigation`
capability, which this change modifies.

## ADDED Requirements

### Requirement: Pages use one header idiom per page class

List pages SHALL supply the list-page set of header parts, and detail pages SHALL supply the detail-page set. Two pages at the same level of the same section SHALL NOT supply different idiom sets.

The header component itself — and its optional kicker, breadcrumbs, leading content, and actions slot — is owned by `web-ui-components`. This requirement owns only which optional parts a page of a given class supplies, so the two requirements do not double-own the component.

#### Scenario: Sibling pages agree

- **WHEN** a section's index page and its sub-pages are compared
- **THEN** either all supply the list-page idiom (when they are peer lists) or the index supplies the list-page idiom and its detail pages supply the detail idiom, with no unexplained mix

### Requirement: Page kickers match the sidebar navigation vocabulary

A page kicker SHALL name the sidebar group the page belongs to, so the kicker reinforces the navigation
rather than introducing a second, unrelated taxonomy. Because kickers are currently free-form strings, the
relationship SHALL be expressed as a shared source of truth — a kicker-to-sidebar-group mapping used by
both the header and the test — so the rule is deterministic rather than a matter of convention.

#### Scenario: Kicker equals the sidebar group

- **WHEN** a page renders a kicker in its header
- **THEN** the kicker text names the sidebar group that contains the page, as resolved through the shared mapping

#### Scenario: No orphan kicker vocabulary

- **WHEN** every kicker string used across the gateway is resolved through the shared mapping
- **THEN** each resolves to a sidebar group, and no page invents a category the navigation does not show

### Requirement: The command palette covers every navigation destination

The command palette SHALL offer every destination the sidebar exposes, derived from the same source as
the sidebar rather than a hand-maintained list.

#### Scenario: Palette covers the nav

- **WHEN** the command palette is opened
- **THEN** it lists every sidebar destination, including objects, schema, embeddings, backups, schedules, skills, settings, blueprints, MCP, and approvals

#### Scenario: Coverage is derived, not duplicated

- **WHEN** a navigation destination is added or removed
- **THEN** the palette reflects it without a separate edit, because both read the same group definition
