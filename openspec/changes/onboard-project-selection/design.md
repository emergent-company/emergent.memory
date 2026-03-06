## Context

The `emergent-onboard` skill is a SKILL.md file embedded in the CLI binary at `tools/emergent-cli/internal/skillsfs/skills/emergent-onboard/SKILL.md`. It is loaded by OpenCode as an AI agent skill and instructs the agent how to walk a user through onboarding a project into Emergent.

Current state: the skill assumes a project is already configured (via `emergent projects set` or `~/.emergent/config.yaml`). Step 3 mentions `emergent projects list` only as an afterthought, and does not write anything to `.env.local`. The user must manually ensure they're in the right project context before onboarding.

The fix is entirely in the SKILL.md — no Go code, no CLI commands, no migrations.

## Goals / Non-Goals

**Goals:**
- Add a "Step 2 — Choose or create an Emergent project" between "understand the project" and "design the template pack"
- The step lists projects via `emergent projects list`, lets the user pick or says "create new", then writes `EMERGENT_PROJECT=<id>` to `.env.local`
- If `.env.local` already contains `EMERGENT_PROJECT`, skip selection and confirm the existing project with the user
- The rest of the skill (pack design, install, populate) already benefits from the set context because the CLI reads `EMERGENT_PROJECT` from the environment

**Non-Goals:**
- No change to the template pack design step
- No new CLI commands
- No change to how `.env.local` is read by the CLI (already supported)
- No support for multiple projects per repo

## Decisions

**D1: Write to `.env.local`, not `~/.emergent/config.yaml`**  
`.env.local` is repo-local and not committed (it should be in `.gitignore`), which is appropriate for per-project credentials and config. The global config (`~/.emergent/config.yaml`) is machine-global. Writing there would break other projects. `.env.local` is the established pattern — the `meta-project` already uses `EMERGENT_PROJECT=meta-project` there.

Alternative considered: `emergent projects set <id>` which writes to global config — rejected because it's global, not scoped to the repo.

**D2: Insert project selection as Step 2, before template pack design**  
The pack must be installed into the correct project. Doing project selection first means every subsequent CLI call in the skill already has the right context, avoiding a class of "I installed the pack in the wrong project" errors.

Alternative: ask at the end — rejected because the pack installation step needs the project ID anyway.

**D3: Check `.env.local` for existing `EMERGENT_PROJECT` to short-circuit**  
Re-running the skill on an already-onboarded project should not re-prompt for project selection. Reading `.env.local` first and confirming with the user is the least surprising behavior.

## Risks / Trade-offs

- **`.env.local` may not be gitignored** → The skill should remind the user to add `.env.local` to `.gitignore` if it isn't already. Not a blocker.
- **User picks wrong project by mistake** → They can re-run the skill; it will detect the existing `EMERGENT_PROJECT` and ask them to confirm or change it.
- **`emergent projects list` returns an empty list** → The skill must handle this gracefully by going straight to "create new project".

## Migration Plan

No migration needed. The change is a SKILL.md text update. After `emergent skills install --force` in the target repo, the new behavior is immediately active.
