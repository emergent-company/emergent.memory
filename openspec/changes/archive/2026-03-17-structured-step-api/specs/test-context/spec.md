## ADDED Requirements

### Requirement: TestContext type bundles test lifecycle
The framework SHALL expose a `TestContext` struct that holds a `*testing.T`, a `*RunLog`, an isolated home directory path, server URL, auth token, and an optional project ID. All fields SHALL be exported for direct access.

#### Scenario: TestContext fields are accessible
- **WHEN** `tc := NewTest(t, opts)` is called
- **THEN** `tc.T`, `tc.RunLog`, `tc.Home`, `tc.Server`, `tc.Token`, and `tc.ProjectID` are all accessible as exported fields

### Requirement: NewTest creates TestContext with automatic preamble
The framework SHALL expose a `NewTest(t *testing.T, opts TestOpts) *TestContext` constructor that performs all preamble steps automatically: creates a RunLog, registers `rl.Close` via `t.Cleanup`, creates an isolated temp dir as Home, sets up CLI auth, and checks server readiness.

#### Scenario: NewTest without project
- **WHEN** `NewTest(t, TestOpts{Describe: "test"})` is called with empty `Project` field
- **THEN** a TestContext is returned with RunLog initialized, Home set to a temp dir, CLI auth configured, server readiness checked, and ProjectID set to empty string

#### Scenario: NewTest with project
- **WHEN** `NewTest(t, TestOpts{Describe: "test", Project: "e2e-foo"})` is called
- **THEN** a TestContext is returned with a unique project created (name prefixed with "e2e-foo"), ProjectID populated, and project deletion registered via `t.Cleanup`

#### Scenario: NewTest sets description
- **WHEN** `NewTest(t, TestOpts{Describe: "Verify X", Bullets: []string{"step 1", "step 2"}})` is called
- **THEN** `rl.Describe` is called with the summary and bullets

#### Scenario: NewTest skips when server is down
- **WHEN** `NewTest` is called and the server health endpoint is unreachable
- **THEN** the test is skipped via `t.Skip` (not failed)

### Requirement: TestOpts configures test behavior
The framework SHALL expose a `TestOpts` struct with fields: `Describe string`, `Bullets []string`, `Project string`, `Tags []string`, and `Binary string`. The `Binary` field SHALL default to `"memory"` when empty.

#### Scenario: Binary defaults to memory
- **WHEN** `TestOpts{Binary: ""}` is used
- **THEN** CLI steps execute the `memory` binary

#### Scenario: Binary override
- **WHEN** `TestOpts{Binary: "mycli"}` is used
- **THEN** CLI steps execute the `mycli` binary instead of `memory`

#### Scenario: Tags applied at creation
- **WHEN** `TestOpts{Tags: []string{"model:gemini"}}` is used
- **THEN** `rl.Tag("model:gemini")` is called during NewTest

### Requirement: Done finalizes TestContext
The framework SHALL expose a `Done()` method on TestContext. Tests SHALL call `defer tc.Done()` after `NewTest`. Done SHALL be safe to call multiple times.

#### Scenario: Done is idempotent
- **WHEN** `tc.Done()` is called twice
- **THEN** no panic or error occurs

### Requirement: TestContext exposes Log, Tag, and Skip
The framework SHALL expose `tc.Log(format, args...)`, `tc.Tag(tags...)`, and `tc.Skip(reason)` methods that delegate to the underlying RunLog.

#### Scenario: tc.Log writes to RunLog
- **WHEN** `tc.Log("hello %s", "world")` is called
- **THEN** a `log` event with message "hello world" is recorded in RunLog
