## Why

The `emergent-onboard` skill jumps straight to designing a template pack without first asking which Emergent project to use or offering to create a new one. This leaves the project context undefined — the user may already have an Emergent project they want to target, or they may need a new one created, but the skill silently assumes the CLI's currently-configured project. After onboarding there is also no record in the repo of which Emergent project was used, so running the skill again or using other emergent skills in CI loses the context.

## What Changes

- The onboarding skill (Step 3) now runs **before** template pack design: it lists existing Emergent projects, lets the user pick one or create a new one, and writes the chosen project ID to `.env.local` as `EMERGENT_PROJECT=<id>` so every subsequent CLI call picks it up automatically.
- The `.env.local` write happens immediately after the user confirms the project — not at the end — so the rest of onboarding already operates in the correct project context.
- The skill guidance for "already onboarded" projects is updated: if `.env.local` already contains `EMERGENT_PROJECT`, the skill reads it and skips project selection, confirming the project with the user instead.
- The pack-id save step (`pack-id.txt`) is kept as-is; no change to template pack design or population steps.

## Capabilities

### New Capabilities

- `onboard-project-selection`: User-facing workflow step in the `emergent-onboard` skill that lists available Emergent projects, lets the user select or create one, and persists the chosen project ID to `.env.local`.

### Modified Capabilities

_(none — this adds a new step to the skill; no existing spec-level requirements change)_

## Impact

- **`tools/emergent-cli/internal/skillsfs/skills/emergent-onboard/SKILL.md`** — the only file that changes; the new step is inserted between "understand the project" and "design the template pack".
- No Go code changes required — `emergent projects list`, `emergent projects create`, and `.env.local` writing are all handled by the skill's instructions to the AI agent.
- No new CLI commands needed; `emergent projects list` and `emergent projects create` already exist.
