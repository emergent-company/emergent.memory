## ADDED Requirements

### Requirement: Session tool group

The capability taxonomy SHALL include a `session` group (display label "Session")
that owns the session-scoped tools: `session-todo-list`, `session-todo-update`, and
the always-injected hidden builtin `set_session_title`. The session todo tools SHALL
NOT be reported in the display-only fallback group.

#### Scenario: Session todo tools belong to Session

- **WHEN** the agent definition's tool-group catalog is computed with a tool catalog
- **THEN** `session-todo-list` and `session-todo-update` appear in the `session` group and not in the fallback group

#### Scenario: Session title builtin belongs to Session

- **WHEN** the tool-group catalog is computed with a tool catalog
- **THEN** `set_session_title` appears as a member of the `session` group

#### Scenario: Session group is absent without a catalog

- **WHEN** the tool-group catalog is computed from agent-referenced tools only (no catalog)
- **THEN** the `session` group is not injected, so the nil-catalog membership semantics are unchanged

### Requirement: Session title builtin is ban-managed

The server SHALL enable `set_session_title` by default, because it injects the tool
regardless of the agent's allowed-tools whitelist. The agent's banned tools SHALL be
its only disable mechanism: disabling it SHALL record it in banned tools, and
enabling it SHALL remove it from banned tools. Its Session group SHALL report enabled
when it is not banned, even though it is never listed in the agent's allowed tools.

#### Scenario: Enabled unless banned

- **WHEN** `set_session_title` is not in the agent's banned tools
- **THEN** the Session group reports enabled and the tool renders checked

#### Scenario: Banned disables it

- **WHEN** `set_session_title` is in the agent's banned tools
- **THEN** the Session group reports not enabled and the tool renders unchecked

#### Scenario: Group disable bans it

- **WHEN** the Session group's enable control is turned off
- **THEN** `set_session_title` is recorded in the agent's banned tools
