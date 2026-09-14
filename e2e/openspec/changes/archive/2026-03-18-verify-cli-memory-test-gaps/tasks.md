## 1. Browse Smoke Tests

- [x] 1.1 Create `tests/cli/browse_test.go` with `package cli_test` header and runlog imports
- [x] 1.2 Implement `TestCLI_Browse_NoTTY` — run `memory browse` with no flags, assert non-zero exit and no panic in output
- [x] 1.3 Implement `TestCLI_Browse_Help` — run `memory browse --help`, assert exit 0, output contains "tempo-url"

## 2. Upgrade Tests

- [x] 2.1 Create `tests/cli/upgrade_test.go` with `package cli_test` header
- [x] 2.2 Implement `TestCLI_Upgrade_Help` — run `memory upgrade --help`, assert exit 0, output contains "--force" and "--dir"
- [x] 2.3 Implement `TestCLI_Upgrade_Force_DevBuild` — run `memory upgrade --force` with temp HOME, assert non-empty output and no panic (skip if network unavailable)
- [x] 2.4 Implement `TestCLI_ServerUpgrade_Help` — run `memory server upgrade --help`, assert exit 0 and "upgrade" in output

## 3. Server CTL Tests

- [x] 3.1 Create `tests/cli/server_ctl_test.go` with `package cli_test` header
- [x] 3.2 Implement `TestCLI_ServerCtl_Help` — run `memory server ctl --help`, assert exit 0 and "status" in output
- [x] 3.3 Implement `TestCLI_ServerCtl_Status_NoPanic` — run `memory server ctl status`, assert output is non-empty and contains no "panic" or "runtime error"
- [x] 3.4 Implement `TestCLI_ServerInstall_Help` — run `memory server install --help`, assert exit 0
- [x] 3.5 Implement `TestCLI_ServerUninstall_Help` — run `memory server uninstall --help`, assert exit 0

## 4. Output Format Tests

- [x] 4.1 Create `tests/cli/output_formats_test.go` with `package cli_test` header
- [x] 4.2 Implement `TestCLI_ProjectsList_CSV` — run `memory projects list --output csv`, assert exit 0, first line contains comma-separated header with "id" and "name"
- [x] 4.3 Implement `TestCLI_Status_JSON` — run `memory status --json`, assert exit 0, output is valid JSON, contains version or auth field
- [x] 4.4 Implement `TestCLI_GraphList_JSON` — create a temp project, run `memory graph list --output json`, assert output is a valid JSON array

## 5. Install-Memory-Skills Coverage Tests

- [x] 5.1 Create `tests/cli/install_skills_coverage_test.go` with `package cli_test` header
- [x] 5.2 Implement `TestInstallMemorySkills_CustomDir` — run `memory install-memory-skills --dir <t.TempDir()>`, assert exit 0 and at least one `memory-*` subdirectory created
- [x] 5.3 Implement `TestInstallMemorySkills_Idempotent` — run install twice without `--force` to same dir, assert second run exits 0 and output indicates skipped
- [x] 5.4 Implement `TestInstallMemorySkills_Force_Overwrites` — place a dummy file in a skill subdir, run with `--force`, assert skill dir is replaced by embedded version
- [x] 5.5 Implement `TestInstallMemorySkills_SkillManifestsValid` — after install, walk each `memory-*` dir, assert YAML manifest exists, is parseable, and references "memory"

## 6. Docs Tests Update

- [x] 6.1 Update `tests/docs/cli_help_test.go` — add `TestDocHelp_Browse` and `TestDocHelp_Upgrade` to verify these commands appear in top-level help and have documented flags
- [x] 6.2 Update `tests/docs/cli_commands_test.go` — add `browse` and `upgrade` to the command coverage inventory if not already present
