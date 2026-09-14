## 1. CLI Binary Abstraction

- [x] 1.1 Add `MustRunBinaryInDirWithHome(t, binary, dir, home string, args ...string) string` to `framework/cli.go` — identical to `MustRunCLIInDirWithHome` but takes a binary name parameter instead of hardcoding `"memory"`
- [x] 1.2 Add `RunBinaryInDirWithHome(t, binary, dir, home string, args ...string) (string, error)` to `framework/cli.go` — non-fatal variant
- [x] 1.3 Refactor existing `MustRunCLIInDirWithHome` and `RunCLIInDirWithHome` to delegate to the new binary-parameterized functions with `"memory"` as the binary — verify all existing tests still pass

## 2. CLIResult Type

- [x] 2.1 Create `framework/step_result.go` with `CLIResult` struct holding stdout, stderr, exit code, error, and a `*RunLog` reference
- [x] 2.2 Implement `.Contains(substrs ...string) *CLIResult` — asserts all substrings present, logs assertion to RunLog, calls `rl.Failf` on failure
- [x] 2.3 Implement `.ContainsAny(substrs ...string) *CLIResult` — asserts at least one substring present
- [x] 2.4 Implement `.NotContains(substrs ...string) *CLIResult` — asserts no substrings present
- [x] 2.5 Implement `.Matches(pattern string) *CLIResult` — regex assertion
- [x] 2.6 Implement `.Empty() *CLIResult` — asserts output is whitespace-only
- [x] 2.7 Implement `.ParseID(dst *string) *CLIResult` — extracts UUID from output, `rl.Failf` if not found
- [x] 2.8 Implement `.JSONField(field string, dst *string) *CLIResult` — parses JSON, extracts field
- [x] 2.9 Implement `.JSON(dst any) *CLIResult` — full JSON unmarshal
- [x] 2.10 Implement `.ExitCode(expected int) *CLIResult` — asserts exit code
- [x] 2.11 Implement `.Output() string` and `.Stderr() string` — raw access escape hatches

## 3. HTTPResult Type

- [x] 3.1 Add `HTTPResult` struct to `framework/step_result.go` holding status code, headers, response body, and `*RunLog` reference
- [x] 3.2 Implement `.Status(expected int) *HTTPResult` — asserts status code
- [x] 3.3 Implement `.JSONField(field string, dst *string) *HTTPResult` — parse response body JSON
- [x] 3.4 Implement `.JSONContains(field, expected string) *HTTPResult` — assert JSON field value
- [x] 3.5 Implement `.BodyContains(substr string) *HTTPResult` — substring assertion on body
- [x] 3.6 Implement `.JSON(dst any) *HTTPResult` and `.Body() string` — raw access

## 4. Step Type

- [x] 4.1 Create `framework/step.go` with `Step` struct holding `*TestContext`, step name, and start time
- [x] 4.2 Implement `s.CLI(args ...string) *CLIResult` — executes `tc.opts.Binary` with TestContext's home/env, auto-logs to RunLog as a `cli` event, returns `*CLIResult`
- [x] 4.3 Implement `s.CLIExpectError(args ...string) *CLIResult` — same as CLI but does not fail on non-zero exit
- [x] 4.4 Implement `s.HTTP(method, path string, body ...[]byte) *HTTPResult` — makes authenticated request to `tc.Server + path`, auto-logs to RunLog, returns `*HTTPResult`
- [x] 4.5 Implement `s.Log(format string, args ...any)` — delegates to RunLog.Printf
- [x] 4.6 Implement `s.WriteFile(path, content string)` — writes file under `tc.Home`, logs action

## 5. TestContext and TestOpts

- [x] 5.1 Create `framework/test_context.go` with `TestOpts` struct (Describe, Bullets, Project, Tags, Binary fields)
- [x] 5.2 Implement `TestContext` struct with exported fields: T, RunLog, Home, Server, Token, ProjectID
- [x] 5.3 Implement `NewTest(t *testing.T, opts TestOpts) *TestContext` — performs full preamble: NewRunLog, t.Cleanup(rl.Close), TempDir, SetupCLIAuth, server readiness check (skip if down), Describe, Tags, optional project creation + cleanup
- [x] 5.4 Implement `tc.Step(name string, fn func(s *Step))` — creates RunLog Section, constructs Step, calls fn, records step duration
- [x] 5.5 Implement `tc.Done()` — idempotent finalizer (safe to call multiple times)
- [x] 5.6 Implement `tc.Log()`, `tc.Tag()`, `tc.Skip()` — delegates to RunLog

## 6. Verification

- [x] 6.1 Write a sample test using the new API (e.g., rewrite `TestCLIInstalled_ProjectCreateGetDelete` using `NewTest` + `tc.Step` + `s.CLI().Contains().ParseID()`) and verify it passes against the test server
- [x] 6.2 Verify all existing tests still compile and pass unchanged (`go build ./...` and `go vet ./...`)
- [x] 6.3 Update the `create-e2e-test` skill reference (`reference/framework-api.md`) to document the new types and their methods
