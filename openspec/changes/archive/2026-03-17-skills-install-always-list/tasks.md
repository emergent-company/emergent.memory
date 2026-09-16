# Tasks: skills-install-always-list

## Task 1: Add `allToolIDs()` helper
- [x] In `cmd/runlog/skills.go`, add a helper `allToolIDs() []string` that returns all keys of `toolRegistry` sorted alphabetically.

## Task 2: Add `provisionMarker(projectRoot, toolID string, dryRun bool) error`
- [x] If `dryRun` is true, print `"would provision: <markerPath>"` and return nil.
- [x] Resolve the full marker path: `filepath.Join(projectRoot, filepath.FromSlash(entry.MarkerPath))`.
- [x] If the marker already exists (`os.Stat` succeeds), return nil (no-op).
- [x] If the marker path's base name has an extension that isn't the full base name (e.g. `.md` but not `.claude`), treat it as a file marker:
  - `os.MkdirAll` the parent directory.
  - Create the file with `os.WriteFile(..., []byte{}, 0644)` if it doesn't exist.
- [x] Otherwise treat as a dir marker: `os.MkdirAll(markerPath, 0755)`.

## Task 3: Rewrite `cmdSkillsList` to show the tool registry table
- [x] Change signature to `cmdSkillsList(projectRoot string) error`.
- [x] Compute detected set via `detectTools(projectRoot)`.
- [x] Print header: `"Supported tools (N):"` where N is `len(toolRegistry)`.
- [x] For each ID in `allToolIDs()`, print: `"  <id>  marker: <MarkerPath>  install: <InstallDir>  detected: yes|no"`.
- [x] Update the caller in `cmdSkills` to pass `projectRoot` (obtain via `os.Getwd()`).

## Task 4: Rewrite `selectTools` to show all tools
- [x] Change signature to `selectTools(allIDs []string, detectedSet map[string]bool) ([]string, error)`.
- [x] Print `"Supported agent tools:"` (no longer "Detected agent tools:").
- [x] For each ID in `allIDs`, print `"  N) <id>  [detected]"` if detected, else `"  N) <id>"`.
- [x] Keep the rest of the selection logic (all/none/comma-separated numbers) unchanged, operating on `allIDs` instead of `detected`.

## Task 5: Update `cmdSkillsInstall` — `--all` targets all tools
- [x] In the `else` branch (no `--tools` flag), change `if *all { targetTools = detected }` to `if *all { targetTools = allToolIDs() }`.
- [x] Pass `allToolIDs()` and a `detectedSet` to `selectTools` instead of just `detected`.
- [x] Remove the early-exit when `len(detected) == 0` (detection no longer gates anything).

## Task 6: Call `provisionMarker` before installing each tool
- [x] In the per-tool install loop, before the inner skill loop, call `provisionMarker(projectRoot, toolID, *dryRun)`.
- [x] If it returns an error, return the error (wrapping with tool name for context).

## Task 7: Update `skillsInstallUsage` help text
- [x] Change `--all` description from `"install for all detected agent tools without prompting"` to `"install for all supported agent tools without prompting"`.
