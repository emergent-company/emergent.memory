## Purpose

Defines the voice rules for user-facing strings in the gateway: one pinned form for list empty states,
one for load-failure headings, no internal implementation detail in user copy, and consistent names for
the same concept across navigation, page titles, and actions.

## ADDED Requirements

### Requirement: List empty states use the canonical heading form

A list or section that has no content SHALL use the empty-state heading form **"No <thing> yet"**.

The rule applies to genuinely empty collections. It explicitly does **not** apply to search-result and
no-match states (which describe a filtered result, not an empty collection), nor to "not set"/"none
configured" states for a single value, nor to dropdown or select placeholders. Where a capability
already pins a non-canonical empty-state string, that capability's requirement SHALL be updated in this
change rather than left in conflict — the known owners are `external-mcp-nodes` ("no external nodes
connected", "no tools"), `agent-dashboard-ui` ("no tools configured"), `project-settings-ui` ("no
overrides"), and the in-flight `add-api-tokens-ui` ("no tokens") and `add-backups-ui` ("no backups"). The
empty states owned by `mcp-servers-ui` and `mcp-share-management` pin no literal heading and therefore need
no wording change, but SHALL be checked for consistency rather than assumed compliant.

#### Scenario: Empty state uses the canonical form

- **WHEN** an empty state for a genuinely empty collection is rendered
- **THEN** its heading matches "No <thing> yet", so equivalent situations never mix "No X" with "No X yet"

#### Scenario: Non-empty-state states are exempt

- **WHEN** a search returns no matches, or a single value is unset
- **THEN** its message describes that condition in its own terms and is not forced into the empty-state form

### Requirement: Error headings use the canonical form

A page or section that fails to load its data SHALL use the load-failure heading form **"Couldn't load
<thing>"**, and SHALL NOT use a variant such as "<thing> unavailable" or "Failed to load <thing>" for the
same situation. The "unavailable" phrasings pinned by other capabilities SHALL be reconciled in this
change where they are rendered gateway headings.

#### Scenario: Load failure heading uses the canonical form

- **WHEN** a page or section fails to load its data
- **THEN** its heading matches "Couldn't load <thing>", so the gateway does not mix it with "<thing> unavailable" or "Failed to load <thing>"

### Requirement: User copy does not expose internal implementation detail

User-facing copy SHALL describe the product behaviour, and SHALL NOT reference internal packaging,
release timing, backend status text, or implementation jargon.

#### Scenario: No internal detail in copy

- **WHEN** a user-facing string is rendered
- **THEN** it does not describe internal errors verbatim, nor promise behaviour in terms of internal release units, nor use pricing or backend jargon where a plain description applies

### Requirement: Long descriptions do not masquerade as subtitles

Header subtitles SHALL stay short enough to serve as a subtitle, and explanatory prose SHALL render as
body copy or help text instead.

#### Scenario: Subtitle stays a subtitle

- **WHEN** a page renders its header subtitle
- **THEN** the subtitle is a short phrase, and any multi-sentence explanation is rendered as body or help text

### Requirement: The same concept has one name

A concept, entity, or destination SHALL be named the same way across the sidebar, the page title, and its
actions. Because names are free-form strings today, the canonical name for each named concept SHALL be
enumerated as a source of truth in this change, and the assertion SHALL be limited to that enumerated set
rather than to an open-ended judgement.

#### Scenario: Names agree across surfaces for enumerated concepts

- **WHEN** an enumerated concept's sidebar label, page title, and action labels are compared
- **THEN** they use the same name and casing, for example "API tokens" (not "API Tokens") and "MCP sharing" (not "MCP Sharing")

#### Scenario: The canonical set is explicit

- **WHEN** the copy test runs
- **THEN** it asserts against the enumerated canonical-name map, so the rule is deterministic and does not depend on human reading
