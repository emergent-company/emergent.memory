## 1. Scaffolding & SKILL.md Types

- [x] 1.1 Create `tools/emergent-cli/internal/cmd/install_skills.go` with package declaration and `init()` stub that registers `installSkillsCmd` on `rootCmd`
- [x] 1.2 Define `SkillMeta` struct (Name, Description, Version, License, Path string) for parsed frontmatter
- [x] 1.3 Define `SkillFrontmatter` struct with YAML tags matching agentskills.io fields (`name`, `description`, `license`, `metadata`, `compatibility`, `allowed-tools`)
- [x] 1.4 Define persistent `--dir` flag on `installSkillsCmd`; default to `.agents/skills` relative to CWD

## 2. SKILL.md Frontmatter Parser

- [x] 2.1 Implement `parseSkillFrontmatter(path string) (*SkillFrontmatter, error)` — read file, split on `---` delimiters, decode YAML block using `gopkg.in/yaml.v3`
- [x] 2.2 Implement `validateSkillFrontmatter(fm *SkillFrontmatter, dirName string) []string` — return slice of error strings for all violated rules:
  - `name` present and non-empty
  - `name` ≤ 64 chars
  - `name` matches `^[a-z0-9]([a-z0-9-]*[a-z0-9])?$` (single char allowed)
  - `name` does not contain `--`
  - `description` present and 1–1024 chars
  - `name` matches `dirName` (when `dirName` is non-empty)
- [x] 2.3 Write unit tests for `validateSkillFrontmatter` covering all spec scenarios (valid, uppercase, leading hyphen, consecutive hyphens, >64 chars, missing description)

## 3. `validate` Subcommand

- [x] 3.1 Implement `runValidateSkill(cmd, args)` — call `parseSkillFrontmatter` + `validateSkillFrontmatter`; print all errors or success message; exit non-zero on any error
- [x] 3.2 Register `validate [path]` subcommand with `cobra.ExactArgs(1)`; default path arg to `.` if omitted via `cobra.MaximumNArgs(1)` + default logic
- [x] 3.3 Write unit tests for `validate` command using a temp directory fixture

## 4. Local Path Install

- [x] 4.1 Implement `installFromLocalPath(src, targetDir string, force bool) error` — validate SKILL.md, check target exists, prompt user interactively if target exists and `--force` not set, copy directory tree, rename target to `name` field
- [x] 4.2 Implement `copyDirTree(src, dst string) error` — recursive copy preserving file modes; skip `.git/`
- [x] 4.3 Handle already-exists cases:
  - `--force`: remove existing target dir before copy; print `✓ Reinstalled skill '<name>'`
  - interactive terminal (no `--force`): prompt `Skill '<name>' is already installed. Update? [y/N]:` — proceed on `y`/`Y`, abort on anything else
  - non-interactive (no `--force`): exit with error `skill '<name>' is already installed; use --force to overwrite`
- [x] 4.4 Use `golang.org/x/term` to detect interactive terminal for the update prompt
- [x] 4.5 Create target directory (and parents) with `os.MkdirAll` if it does not exist
- [x] 4.6 Write unit tests for local path install scenarios (happy path, already-exists + force, missing SKILL.md)

## 5. `install` Subcommand

- [x] 5.1 Implement `runInstallSkill(cmd, args)` — resolve local path, call `installFromLocalPath`, pass `--force` and `--skip-validate` flags
- [x] 5.2 Register `install <source>` subcommand with `cobra.ExactArgs(1)`; add `--force` and `--skip-validate` flags
- [x] 5.3 Print `⚠ Skipping SKILL.md validation` when `--skip-validate` is set

## 6. `list` Subcommand

- [x] 6.1 Implement `runListSkills(cmd, args)` — read subdirectories of `--dir`, parse each `SKILL.md` frontmatter, collect `SkillMeta` slice
- [x] 6.2 Table output (default): use `tablewriter` with columns Name, Description, Version, License; truncate Description at 60 chars with `…`
- [x] 6.3 JSON output (`--output json`): marshal `[]SkillMeta` and print
- [x] 6.4 Print `No skills installed in <dir>` when no subdirectories contain `SKILL.md`
- [x] 6.5 Register `list` subcommand with `--output` flag (default `table`); propagate `--dir` from parent
- [x] 6.6 Write unit tests for list (table, JSON, empty dir)

## 7. `remove` Subcommand

- [x] 7.1 Implement `runRemoveSkill(cmd, args)` — check `<dir>/<skill-name>/` exists, prompt confirmation if interactive terminal and `--force` not set, then `os.RemoveAll`
- [x] 7.2 Use `golang.org/x/term` (already in `go.mod`) to detect interactive terminal for the confirmation prompt
- [x] 7.3 Register `remove <skill-name>` subcommand with `cobra.ExactArgs(1)` and `--force` flag
- [x] 7.4 Write unit tests for remove (happy path, not-found, force flag)

## 8. Integration & Verification

- [x] 8.1 Run `go build ./...` in `tools/emergent-cli/` — confirm zero compile errors
- [x] 8.2 Run `go test ./internal/cmd/...` — all new and existing tests pass
- [x] 8.3 Smoke test `emergent install-skills --help` and `emergent install-skills install --help` show correct usage
- [x] 8.4 Smoke test `emergent install-skills install ./.agents/skills/commit` (self-install from repo) — installs correctly, `list` shows it, `remove` removes it
- [x] 8.5 Verify `emergent --help` lists `install-skills` in command inventory
