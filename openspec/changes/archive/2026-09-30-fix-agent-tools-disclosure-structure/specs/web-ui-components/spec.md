## MODIFIED Requirements

### Requirement: Tool-group disclosures render through ui.Disclosure

The collapsible tool-picker group shell (capability groups and source/relay groups) SHALL render through `ui.Disclosure`, and the gateway SHALL NOT carry its own `agentToolDisclosure`.

#### Scenario: Capability group maps to Disclosure props

- **WHEN** a capability tool group is rendered
- **THEN** it calls `ui.Disclosure` with `GroupClass: "group/cap"`, `ItemsStart: true`, `Open` from the group's open state, `Attrs` carrying `data-testid="tool-group"` and `data-tool-group=<id>`, and `SummaryAttrs` carrying `data-testid="tool-group-header-<id>"`, with the capability leading block as the header, the group enable switch as the trailing slot, and the group approval-policy select in the disclosure body so it is not a descendant of the interactive `<summary>`

#### Scenario: Source/relay group replaces the details and body bases

- **WHEN** a registry-server or relay-node tool group is rendered
- **THEN** it calls `ui.Disclosure` with `DetailsBase: "rounded-box border "+<BorderClass>` and `BodyBase: "flex flex-col border-t "+<BodyBorderClass>`, so the per-variant border/background tone is preserved and no unwanted `gap-2` or `p-3` is added to the body

#### Scenario: Tool-group markup is preserved

- **WHEN** a tool group is rendered
- **THEN** every `data-testid`, `data-tool-group`, form field `name`, `value`, and `checked` state asserted by `agent_ui_test.go` survives unchanged, and the chevron rotation (`group` vs `group/cap`) still works
