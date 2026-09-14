## 1. Core Fixture Types (`/root/runlog/fixture.go`)

- [ ] 1.1 Create `/root/runlog/fixture.go` with unexported `fixture` builder struct (fields: `t`, `home`, `server`, `token`, `projectID`, `binary`, `rl`, `hasProject`)
- [ ] 1.2 Define exported `Fixture` struct with fields: `T`, `Home`, `Server`, `Token`, `ProjectID`, `Binary`, `RunLog`
- [ ] 1.3 Define exported `type Option func(*fixture)` type
- [ ] 1.4 Implement `Use(t *testing.T, opts ...Option) *Fixture`: create RunLog, TempDir, RequireServerReady, SetupCLIAuth, apply options, return Fixture

## 2. Option Functions

- [ ] 2.1 Implement `WithProject(prefix string) Option`: call `CreateProject`, register `DeleteProjectOnCleanup` via `t.Cleanup`, set `hasProject = true`
- [ ] 2.2 Implement `WithSchema(filePath string) Option`: panic if `!hasProject`, run `fx.CLI("schemas", "upload", filePath)` in setup
- [ ] 2.3 Implement `WithDocument(filePath string) Option`: panic if `!hasProject`, run `fx.CLI("documents", "upload", filePath)` in setup
- [ ] 2.4 Implement `WithBinary(bin string) Option`: set `fixture.binary = bin`

## 3. `Fixture` Methods

- [ ] 3.1 Implement `Fixture.CLI(args ...string) *CLIResult`: run `RunBinaryInDirWithHome` with `fx.Home`, log to RunLog, fatal on error, return `*CLIResult`
- [ ] 3.2 Implement `Fixture.CLIExpectError(args ...string) *CLIResult`: same as CLI but does NOT fatal on non-zero exit
- [ ] 3.3 Implement `Fixture.TempFile(name, content string) string`: write file to `t.TempDir()`, fatal on error, return abs path

## 4. Verification

- [ ] 4.1 Run `go build github.com/emergent-company/runlog` — must compile clean
- [ ] 4.2 Write a minimal demo test in `/root/emergent.memory.e2e/tests/cli/` using `framework.Use(t, framework.WithProject(...))` + `fx.CLI(...)` and verify it compiles (`go build ./tests/cli/...`)
- [ ] 4.3 Run `go vet github.com/emergent-company/runlog` — must pass with no warnings
