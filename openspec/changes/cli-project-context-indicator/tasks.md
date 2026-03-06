## 1. Audit & Preparation

- [x] 1.1 Verify no subcommand defines `PersistentPreRunE` or `PersistentPreRun` (grep check)
- [x] 1.2 Verify `golang.org/x/term` or equivalent isatty check is available in the module (check `go.mod`)

## 2. Source Detection Helper

- [x] 2.1 Add `detectProjectTokenSource(cmd *cobra.Command) string` function in `helpers.go` that returns the source label (`--project-token flag`, `EMERGENT_PROJECT_TOKEN`, `EMERGENT_PROJECT`, `config file`) using the priority order from the design
- [x] 2.2 Add `printProjectIndicator(cmd *cobra.Command, cfg *config.Config)` function in `helpers.go` that: checks `cfg.ProjectToken == ""` and returns early; checks if stderr is a TTY and returns early if not; determines the display name (`cfg.ProjectName` or masked token); determines the source label via `detectProjectTokenSource`; prints the formatted line to stderr respecting `--no-color` / `NO_COLOR`

## 3. Root Command Hook

- [x] 3.1 Add `PersistentPreRunE` to `rootCmd` in `root.go` that calls `config.LoadWithEnv` then `printProjectIndicator`
- [x] 3.2 Ensure the hook returns `nil` (not an error) when no project token is set — it must never block command execution

## 4. Cleanup

- [x] 4.1 Remove the existing ad-hoc stderr banner from `resolveProjectContext()` in `helpers.go` (lines 44–52) to avoid double-printing

## 5. Build & Verify

- [x] 5.1 Run `go build ./...` from `tools/emergent-cli` — confirm zero errors
- [x] 5.2 Install binary: `go build -o /root/.local/bin/emergent ./cmd/main.go`
- [x] 5.3 Run `emergent status` from `tools/emergent-cli/` (where `.env.local` has `EMERGENT_PROJECT`) — confirm indicator appears on stderr before output
- [x] 5.4 Run `emergent version` — confirm no indicator is printed (no token active for that command path)
- [x] 5.5 Run `emergent status 2>/dev/null` — confirm stdout is clean (indicator only on stderr)
- [x] 5.6 Run `emergent status | cat` — confirm indicator is suppressed in non-TTY
