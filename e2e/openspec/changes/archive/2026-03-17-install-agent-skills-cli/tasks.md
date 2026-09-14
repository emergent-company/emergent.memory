## 1. Subcommand scaffolding (in `github.com/emergent-company/runlog`)

- [x] 1.1 Create `cmd/runlog/skills.go` defining the `skills` subcommand and routing `skills install` to the install handler
- [x] 1.2 Register the `skills` subcommand in `cmd/runlog/main.go` alongside existing subcommands
- [x] 1.3 Add usage/help text for `runlog skills install` (flags: `--all`, `--tools`, `--force`, `--dry-run`)

## 2. Tool registry

- [x] 2.1 Define `toolRegistry` — a static map of tool ID → marker path (for detection) and install path template (e.g., `.opencode/skills/<name>/`)
- [x] 2.2 Add entries for the primary tools used in this project: `opencode`, `claude`, `cursor`, `agents` (generic fallback), and at minimum 5 other tools from the OpenSpec supported-tools table
- [x] 2.3 Write `detectTools(projectRoot string) []string` — iterates registry, checks marker paths with `os.Stat`, returns detected IDs

## 3. Skill source discovery

- [x] 3.1 Write `discoverSkills(projectRoot string) ([]skillEntry, error)` — scans `.agents/skills/` then `.opencode/skills/`, deduplicates by name (first-found wins)
- [x] 3.2 Return an error if no source directories exist
- [x] 3.3 Each `skillEntry` holds: `Name string`, `SrcPath string`

## 4. Interactive prompt

- [x] 4.1 Write `selectTools(detected []string) ([]string, error)` — prints numbered list, reads stdin, parses comma-separated numbers, supports `all` and `none`/empty shortcuts
- [x] 4.2 Handle non-TTY / `--all` / `--tools` flags by bypassing the prompt
- [x] 4.3 Validate `--tools` values against registry; return error for unknown IDs

## 5. Install logic

- [x] 5.1 Write `installSkill(srcDir, dstDir string, force bool, dryRun bool) error` — recursive copy using `os.MkdirAll` + `io.Copy`
- [x] 5.2 Implement skip-if-exists behavior: stat `dstDir`; if exists and `!force`, print skip message and continue
- [x] 5.3 Implement `--force`: remove existing `dstDir` before copy
- [x] 5.4 Implement `--dry-run`: print planned actions, return without writing
- [x] 5.5 After all installs, print summary: "installed N skills for M tools" (or dry-run equivalent)

## 6. Wire up and test

- [x] 6.1 Connect all pieces in the `skills install` handler: detect → prompt/flag → discover → install loop
- [x] 6.2 Add a `TestSkillsInstall` unit test in `cmd/runlog/skills_test.go` covering: happy path, skip-existing, force overwrite, dry-run, unknown tool error
- [x] 6.3 Build the binary and manually smoke-test against this repo: `go run ./cmd/runlog skills install --dry-run --all`

## 7. Documentation and integration

- [x] 7.1 Update `AGENTS.md` in this e2e repo with a "Install skills" section documenting `runlog skills install`
- [x] 7.2 Update `go.mod` in this e2e repo to require the new `runlog` release that includes the subcommand
- [x] 7.3 Add a `runlog-install-skills` skill in `.agents/skills/` (a SKILL.md describing how to use `runlog skills install`) so agents can trigger the install workflow
