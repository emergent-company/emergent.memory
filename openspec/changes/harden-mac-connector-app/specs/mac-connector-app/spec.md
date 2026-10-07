## ADDED Requirements

### Requirement: Engine configuration is persisted without corruption

Whenever the app writes the engine configuration, every value it writes (server URL, project or organisation identifier, token, paths) SHALL be encoded so that a value containing YAML-significant characters — a colon, a leading indicator character, a quote, or a newline — round-trips unchanged and never corrupts the document or changes the meaning of another key. The app SHALL parse back the configuration it wrote to the same values it wrote.

#### Scenario: Value containing a colon round-trips

- **WHEN** the configuration is written with a value containing `:`, for example a `host:port` server URL
- **THEN** reading the configuration returns that value unchanged and no other key is altered

#### Scenario: Value containing a newline does not corrupt the document

- **WHEN** the configuration is written with a multi-line or newline-containing value
- **THEN** the document remains parseable and every other key retains its value

#### Scenario: Existing configurations still load

- **WHEN** a configuration written before this change is read
- **THEN** it parses to the same values as before

#### Scenario: Failed write does not leave a partial document

- **WHEN** writing the configuration fails partway
- **THEN** the previously valid configuration is left intact rather than replaced by a partial document

### Requirement: Engine and app log writes are serialized and lossless

Log output written by the app — the engine's forwarded stdout and stderr and the app's own lifecycle lines — SHALL be serialized so concurrent writers cannot interleave or truncate lines, and SHALL not rely on unsynchronized shared mutable state. Each logged line SHALL be written atomically with respect to other lines.

#### Scenario: Concurrent engine output stays line-atomic

- **WHEN** the engine writes to stdout and stderr at the same time
- **THEN** each logged line appears whole and no line is interleaved with another

#### Scenario: App lifecycle lines stay distinguishable under load

- **WHEN** the app logs an account-scope or engine-lifecycle transition while the engine is also writing
- **THEN** the app's line is whole and remains distinguishable from the engine's own output, as required by "Engine lifecycle is observable in the log"

#### Scenario: No unsynchronized shared log state

- **WHEN** the app's logging implementation is inspected
- **THEN** log/terminal handles and buffers are not shared as unsafely-mutable state across concurrent callbacks
