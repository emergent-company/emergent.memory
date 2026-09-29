## ADDED Requirements

### Requirement: Inherited policy value is inlined into the policy control

The per-tool and group policy selects SHALL show the inherited value in the `Inherit`
option itself, formatted `Inherit (<value>)`, where `<value>` is the owning group's
policy when set, otherwise the agent's default policy, otherwise `Default`. The
picker SHALL NOT render a separate non-interactive inheritance hint element on tool
rows.

#### Scenario: Tool inherits its group policy

- **WHEN** a tool row has no explicit policy entry and its group has a stored policy of `Ask`
- **THEN** the row's policy select shows `Inherit (Ask)`

#### Scenario: Tool inherits the default

- **WHEN** a tool row has no explicit policy entry and its group has no stored policy
- **THEN** the row's policy select shows `Inherit (<agent default>)`, or `Inherit (Default)` when no default is configured

#### Scenario: Group select inherits the default

- **WHEN** a capability group has no stored policy
- **THEN** the group's policy select shows the agent default's value in its `Inherit` option

#### Scenario: No separate hint element

- **WHEN** the Tools panel renders a tool row
- **THEN** no separate inheritance hint element is rendered beside the policy select

### Requirement: Ban-managed tool rows

A tool row whose enable state is managed by the agent's banned tools rather than its
allowed tools SHALL render a checkbox reflecting ban state (checked = not banned) and
SHALL NOT render a per-tool policy select. Toggling it SHALL add or remove the tool
from the agent's banned tools on save.

#### Scenario: Ban-managed row has no policy select

- **WHEN** the picker renders `set_session_title`
- **THEN** its row shows a checkbox and no per-tool policy select

#### Scenario: Unchecking bans the tool

- **WHEN** the user unchecks `set_session_title` and saves the Tools panel
- **THEN** the saved agent lists it in banned tools

#### Scenario: Checking un-bans the tool

- **WHEN** the user checks a previously banned `set_session_title` and saves the Tools panel
- **THEN** the saved agent no longer lists it in banned tools
