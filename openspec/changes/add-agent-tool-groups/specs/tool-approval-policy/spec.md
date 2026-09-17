## MODIFIED Requirements

### Requirement: Per-tool approval policy

Each tool an agent may call SHALL carry one of three policies: `allow` (run without asking), `deny` (unavailable), or `ask` (intercept and require approval). An agent SHALL have a default policy that applies to any tool without an explicit entry. A tool MAY additionally be governed by a policy set on the tool group it belongs to, which applies when the tool has no explicit entry.

#### Scenario: Default policy applies to unlisted tools

- **WHEN** an agent calls a tool that has no explicit policy entry and belongs to no group with a stored policy
- **THEN** the agent's default policy governs that call

#### Scenario: Explicit policy overrides default

- **WHEN** an agent calls a tool that has an explicit policy entry
- **THEN** the explicit entry governs the call, regardless of the default or any group policy

#### Scenario: Explicit policy overrides group policy

- **WHEN** a tool has an explicit policy entry and its group also has a stored policy
- **THEN** the tool's explicit entry governs the call

## ADDED Requirements

### Requirement: Tool groups

Every tool an agent can call SHALL belong to exactly one tool group identified by a stable slug, derived from the tool's required scope where the tool has one and from a static mapping otherwise. Groups SHALL carry a stable identity independent of their display label. A tool that matches no group SHALL belong to a fallback group.

#### Scenario: Scope-derived membership

- **WHEN** a tool declares a required scope that maps to a group
- **THEN** the tool belongs to that group

#### Scenario: Unscoped tool with a static mapping

- **WHEN** a tool declares no required scope and is listed in the static tool-to-group mapping
- **THEN** the tool belongs to the mapped group

#### Scenario: Unmatched tool falls back

- **WHEN** a tool matches no group by scope or static mapping
- **THEN** the tool belongs to the fallback group and remains controllable

#### Scenario: Fallback group is display-only

- **WHEN** a tool belongs to the fallback group (unmatched, external MCP, or relay tool)
- **THEN** the fallback group is used for display and membership only and never carries a group policy; its effective policy resolves to the tool's explicit entry or the default

#### Scenario: Group ids are not display labels

- **WHEN** a group's display label changes
- **THEN** its stored policy entries remain attached to the same tools

### Requirement: Group approval policy

A tool group SHALL be able to carry an approval policy of `allow`, `ask`, or `deny`, stored on the agent. The group policy SHALL apply to every member tool that has no explicit policy entry, without writing a policy entry per member tool. A newly available tool that joins a group SHALL be governed by that group's policy without the agent being re-saved. The fallback group SHALL NOT carry a group policy, and external MCP-server, relay-node, and native (Google `Model.NativeTools`) tools are out of scope for group policy: they fall through to their explicit entry or the default.

#### Scenario: Group policy applies to members without explicit entries

- **WHEN** an agent calls a member tool of a group whose policy is `ask` and the tool has no explicit entry
- **THEN** the call is intercepted and requires approval

#### Scenario: Group policy covers a newly available member

- **WHEN** a group has a stored policy and a tool is added to the agent that belongs to that group with no explicit entry
- **THEN** that tool is governed by the group policy on the next run without re-saving the group

#### Scenario: No group policy stored

- **WHEN** a member group has no stored policy
- **THEN** resolution falls through to the agent's default policy

#### Scenario: Group policy denied

- **WHEN** a group's policy is `deny` and a member tool with no explicit entry is called
- **THEN** the tool is unavailable and does not execute

### Requirement: Group disable removes and bans members

Disabling a tool group SHALL remove every member tool from the agent's allowed tools and SHALL record every member in the agent's banned tools, so that a later change to the available tool catalog cannot silently re-enable a member. Enabling a group SHALL be the inverse. Group disable SHALL be distinct from a group `deny` policy: disable removes the member from the agent's tool set, while `deny` keeps the tool visible to the model and fails the call.

#### Scenario: Disabling a group removes and bans its members

- **WHEN** a group is disabled
- **THEN** its member tools are absent from the agent's allowed tools and present in the agent's banned tools

#### Scenario: Enabling a group restores members

- **WHEN** a disabled group is enabled
- **THEN** its member tools are present in the agent's allowed tools and absent from the agent's banned tools

#### Scenario: Banned member stays unavailable

- **WHEN** a disabled group's member becomes available in the project's tool catalog again
- **THEN** the member remains unavailable to the agent until the group is enabled

#### Scenario: Disable does not alter policies

- **WHEN** a group is disabled
- **THEN** stored group and per-tool policies are unchanged

### Requirement: Group resolution is the single authorization point

Group-aware resolution SHALL be applied by the same policy resolver used for per-tool and default policies, so that every enforcement path that consults tool policy observes the same effective value. The group id SHALL be derived from the tool's required scope at enforcement time, so a tool governed by a group at read time is governed by the same group when it is called.

#### Scenario: Enforcement paths agree

- **WHEN** a tool's effective policy is computed
- **THEN** the explicit-entry, group, and default layers are resolved in that order regardless of which enforcement path asks

#### Scenario: Enforcement uses the scope-derived group

- **WHEN** an agent calls a tool whose required scope derives its group, including a dynamic tool whose scope is known only from the tool catalog
- **THEN** the enforcement resolver assigns the same group id the read DTO reports, so a stored group policy governs the call
