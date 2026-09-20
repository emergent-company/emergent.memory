## ADDED Requirements

### Requirement: Read tools bound emitted property value size

Read/search MCP tools that emit entity or relationship `properties` SHALL bound the size of each emitted property value so a single oversized field (e.g. a full document `content` blob) cannot inflate a tool result and overflow the caller's context. Any string property value longer than a server-side cap SHALL be truncated in the emitted result with an explicit truncation marker, and the truncation SHALL apply equally under the `full` field strategy and inside nested maps/arrays. Property values under the cap SHALL be emitted unchanged.

#### Scenario: Oversized string property is truncated
- **WHEN** a read/search tool returns an entity whose `properties` contains a string value longer than the cap
- **THEN** the emitted value SHALL be truncated to the cap
- **THEN** a truncation marker SHALL indicate how many characters were omitted

#### Scenario: Truncation applies under the full strategy
- **WHEN** a caller requests `field_strategy: "full"` and a result has an oversized string property
- **THEN** the emitted `properties` value SHALL still be truncated

#### Scenario: Short and non-string properties are unchanged
- **WHEN** every property value is under the cap
- **THEN** the emitted properties SHALL be unchanged
- **AND** non-string property values SHALL be passed through unchanged
