## ADDED Requirements

### Requirement: Tools picker inheritance updates live without collapsing the group

On the Settings Tools panel the displayed policy inheritance SHALL stay in step
with the current selections before a save: changing the agent default policy or a
capability group's policy SHALL recompute every policy select's
`Inherit (<value>)` label client-side, using the same resolution order the server
uses (explicit tool policy → owning group policy → agent default). A tool select
outside any capability group SHALL reflect the agent default. Changing a group
policy SHALL NOT fold or unfold the group: a click on a policy select inside a
disclosure header SHALL NOT run the `<summary>` activation behaviour.

#### Scenario: Changing the default relabels every inheriting select

- **WHEN** the owner changes the default approval policy and no group policy is set
- **THEN** every group select and every per-tool select shows `Inherit (<new default>)`

#### Scenario: Changing a group policy relabels that group's tools

- **WHEN** the owner changes a capability group's policy to `Deny`
- **THEN** each tool select in that group shows `Inherit (Deny)` while the group select continues to show `Inherit (<agent default>)`, and tool selects outside the group are unchanged

#### Scenario: Group policy select does not fold the group

- **WHEN** the owner picks an option in a capability group's policy select inside the disclosure header
- **THEN** the group stays open/closed as it was — the disclosure does not toggle
