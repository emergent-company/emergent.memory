## ADDED Requirements

### Requirement: `Use` is the single entry point for declarative test setup
The framework SHALL export a `Use(t *testing.T, opts ...Option) *Fixture` function in package `runlog`. Calling `Use` SHALL:
1. Create a `RunLog` and register `rl.Close` via `t.Cleanup`
2. Create an isolated temp dir as `Fixture.Home`
3. Assert server readiness (skip test if server is down)
4. Set up CLI auth in `Fixture.Home`
5. Apply each `Option` in order
6. Return a fully-initialized `*Fixture`

#### Scenario: Basic Use call produces ready Fixture
- **WHEN** `framework.Use(t)` is called with no options
- **THEN** `fx.Home` is a non-empty path to a writable temp directory
- **AND** `fx.Server` is the value of `ServerURL()`
- **AND** `fx.Token` is the value of `E2ETestToken()`
- **AND** `fx.Binary` defaults to `"memory"`

#### Scenario: Cleanup is automatic
- **WHEN** `framework.Use(t)` is called
- **THEN** no explicit `t.Cleanup` or `defer` calls are required in the test body for standard teardown

### Requirement: `Option` is a functional-options type
The framework SHALL export `type Option func(*fixture)` where `fixture` is the unexported builder struct. Option functions may mutate any field of `fixture` and may register `t.Cleanup` callbacks for teardown.

#### Scenario: Options compose via variadic args
- **WHEN** `framework.Use(t, framework.WithProject("e2e-docs"), framework.WithBinary("memory"))` is called
- **THEN** both options are applied and the resulting `*Fixture` reflects both settings

### Requirement: `WithProject` creates an ephemeral project and registers cleanup
`WithProject(prefix string) Option` SHALL create a new project via `CreateProject(t, home, srv, prefix)` and register deletion via `DeleteProjectOnCleanup(t, home, projectID)` using `t.Cleanup`.

#### Scenario: Project is created and available on Fixture
- **WHEN** `framework.Use(t, framework.WithProject("e2e-docs"))` is called
- **THEN** `fx.ProjectID` is a non-empty string containing the created project's ID

#### Scenario: Project is deleted after test completes
- **WHEN** a test using `WithProject` completes (pass or fail)
- **THEN** `t.Cleanup` deletes the project via the CLI (via `DeleteProjectOnCleanup`)

#### Scenario: Project config is set in isolated home
- **WHEN** `WithProject` runs
- **THEN** `memory config set project_id <id>` is run in `fx.Home` so project-unaware commands find the right project

### Requirement: `WithSchema` uploads a schema file after project creation
`WithSchema(filePath string) Option` SHALL run `memory schemas upload <filePath>` using `fx.CLI` after the project is available. It SHALL `t.Fatal` if the upload fails.

#### Scenario: Schema is uploaded during setup
- **WHEN** `framework.Use(t, framework.WithProject("e2e-schema"), framework.WithSchema("testdata/schema.json"))` is called
- **THEN** the schema is uploaded to the project before the test body executes

#### Scenario: WithSchema without WithProject panics with clear message
- **WHEN** `framework.Use(t, framework.WithSchema("testdata/schema.json"))` is called without `WithProject`
- **THEN** `Use` panics with a message indicating `WithProject` must precede `WithSchema`

### Requirement: `WithDocument` uploads a document file after project creation
`WithDocument(filePath string) Option` SHALL run `memory documents upload <filePath>` using `fx.CLI` after the project is available. It SHALL `t.Fatal` if the upload fails.

#### Scenario: Document is uploaded during setup
- **WHEN** `framework.Use(t, framework.WithProject("e2e-doc"), framework.WithDocument("testdata/doc.txt"))` is called
- **THEN** the document is uploaded to the project before the test body executes

#### Scenario: WithDocument without WithProject panics with clear message
- **WHEN** `framework.Use(t, framework.WithDocument("testdata/doc.txt"))` is called without `WithProject`
- **THEN** `Use` panics with a message indicating `WithProject` must precede `WithDocument`

### Requirement: `WithBinary` overrides the CLI binary name
`WithBinary(bin string) Option` SHALL set `Fixture.Binary` to the given value. If not provided, the binary defaults to `"memory"`.

#### Scenario: Binary override is reflected in CLI calls
- **WHEN** `framework.Use(t, framework.WithBinary("memory-dev"))` is called
- **THEN** `fx.Binary` equals `"memory-dev"`

### Requirement: `Fixture` is the exported test context type
The framework SHALL export a `Fixture` struct with fields:
- `T *testing.T`
- `Home string`
- `Server string`
- `Token string`
- `ProjectID string`
- `Binary string`
- `RunLog *RunLog`

#### Scenario: All fields are populated after Use
- **WHEN** `framework.Use(t, framework.WithProject("demo"))` is called
- **THEN** all `Fixture` fields are non-zero/non-nil

### Requirement: `Fixture.CLI` runs the binary scoped to the fixture home
`Fixture.CLI(args ...string) *CLIResult` SHALL:
1. Run `fx.Binary` with the given args using `fx.Home` as the `HOME` environment variable
2. Log the invocation and output to `fx.RunLog`
3. Fail the test via `t.Fatal` on non-zero exit
4. Return a `*CLIResult` for chainable assertions

No `--project`, `--server`, or token flags need to be passed — `WithProject` already wrote `project_id`, `server_url`, and credentials into `fx.Home/.memory/config.yaml` during setup.

#### Scenario: Project-scoped commands work without `--project` flag
- **WHEN** `fx.CLI("documents", "list")` is called after `WithProject` setup
- **THEN** the CLI reads `project_id` from the config file in `fx.Home` and lists the correct project's documents

#### Scenario: CLI failure fails the test
- **WHEN** `fx.CLI("documents", "get", "nonexistent-id")` exits non-zero
- **THEN** the test is failed via `t.Fatal`

### Requirement: `Fixture.CLIExpectError` allows non-zero exit
`Fixture.CLIExpectError(args ...string) *CLIResult` SHALL behave identically to `Fixture.CLI` except it SHALL NOT fail the test on non-zero exit. The exit code is captured in the returned `*CLIResult`.

#### Scenario: Non-zero exit does not fail the test
- **WHEN** `fx.CLIExpectError("documents", "get", "nonexistent-id")` is called and the CLI exits non-zero
- **THEN** the test is NOT failed and the `*CLIResult` captures the error

### Requirement: `Fixture.TempFile` creates a named file in a temp directory
`Fixture.TempFile(name, content string) string` SHALL write `content` to a file named `name` inside `t.TempDir()` and return the absolute path. It SHALL `t.Fatal` if the write fails.

#### Scenario: TempFile returns a writable path
- **WHEN** `fx.TempFile("input.txt", "hello world")` is called
- **THEN** a file at the returned path exists with content `"hello world"`

#### Scenario: Multiple TempFile calls return distinct paths
- **WHEN** `fx.TempFile("a.txt", "foo")` and `fx.TempFile("b.txt", "bar")` are called
- **THEN** the two returned paths are different files with their respective contents
