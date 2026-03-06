---
name: openspec-apply-change
description: Implement tasks from an OpenSpec change. Use when the user wants to start implementing, continue implementation, or work through tasks.
license: MIT
compatibility: Requires openspec CLI.
metadata:
  author: openspec
  version: "1.0"
  generatedBy: "1.1.1"
---

Implement tasks from an OpenSpec change.

**Input**: Optionally specify a change name. If omitted, check if it can be inferred from conversation context. If vague or ambiguous you MUST prompt for available changes.

**Steps**

1. **Select the change**

   If a name is provided, use it. Otherwise:
   - Infer from conversation context if the user mentioned a change
   - Auto-select if only one active change exists
   - If ambiguous, run `openspec list --json` to get available changes and use the **AskUserQuestion tool** to let the user select

   Always announce: "Using change: <name>" and how to override (e.g., `/opsx:apply <other>`).

2. **Check status to understand the schema**

   **Skip this step on resumption** (some tasks already marked `[x]` in tasks.md).
   On resumption, go directly to step 4.

   ```bash
   openspec status --change "<name>" --json
   ```
   Parse the JSON to understand:
   - `schemaName`: The workflow being used (e.g., "spec-driven")
   - Which artifact contains the tasks (typically "tasks" for spec-driven, check status for others)

3. **Get apply instructions**

   **Skip this step on resumption** (some tasks already marked `[x]` in tasks.md).
   On resumption, go directly to step 4.

   ```bash
   openspec instructions apply --change "<name>" --json
   ```

   This returns:
   - Context file paths (varies by schema - could be proposal/specs/design/tasks or spec/tests/implementation/docs)
   - Progress (total, complete, remaining)
   - Task list with status
   - Dynamic instruction based on current state

   **Handle states:**
   - If `state: "blocked"` (missing artifacts): show message, suggest using openspec-continue-change
   - If `state: "all_done"`: congratulate, suggest archive
   - Otherwise: proceed to implementation

4. **Read context files**

   **On first invocation** (no tasks marked complete yet): Read all files listed in
   `contextFiles` from the apply instructions output. The files depend on the schema:
   - **spec-driven**: proposal, specs, design, tasks
   - Other schemas: follow the contextFiles from CLI output

   **On resumption** (some tasks already marked `[x]`): Read the tasks file ONLY.
   Do NOT re-read spec files (proposal, specs, design docs), source code files, or
   any other context already covered in prior turns — even if those files are listed
   in `contextFiles`. The design decisions and discoveries from prior sessions are
   already reflected in the tasks list; re-reading specs produces zero new information
   and wastes context budget.
   Look for a `<!-- RESUME HERE: ... -->` marker — if present, start at that exact
   task immediately without reading anything else. If no marker, find the first
   unchecked `- [ ]` task and start there.
   Trust the tasks file as the authoritative record of what is done — do not
   re-verify completed tasks by inspecting source files.
   Remove the `<!-- RESUME HERE -->` marker once you have read it.

5. **Show current progress**

   Display:
   - Schema being used
   - Progress: "N/M tasks complete"
   - Remaining tasks overview
   - Dynamic instruction from CLI

6. **On first invocation: record baseline failures (skip on resumption)**

   If no tasks are yet marked complete, run the project's build or test command
   (e.g., `go build ./...`, `npm run build`, `cargo check`) and note any
   pre-existing failures. Record them as a comment at the top of the tasks file:

   ```
   <!-- Baseline failures (pre-existing, not introduced by this change):
   - path/to/file_test.go: compile error — <description>
   -->
   ```

   This prevents wasting time investigating failures that existed before the change.
   Skip this step on resumption (tasks already partially complete).

7. **Implement tasks (loop until done or blocked)**

   For each pending task:
   - Show which task is being worked on
   - Make the code changes required
   - Keep changes minimal and focused
   - **Verify before marking complete**: after writing code, run the minimal check
     that confirms the task actually works — at minimum `go build ./...` / `npm run build`
     to catch compile errors, or a targeted test / `curl` smoke test if the task is
     an API endpoint or user-visible behaviour. Do NOT mark `[x]` based on code
     being written alone.
   - **Immediately** mark task complete in the tasks file: `- [ ]` → `- [x]` — do this right after verification, not in batches at the end
   - Continue to next task

   **Pause if:**
   - Task is unclear → ask for clarification
   - Implementation reveals a design issue → suggest updating artifacts
   - Error or blocker encountered → report and wait for guidance
   - User interrupts

   **Before pausing for any reason**, write a resume marker into the tasks file
   immediately above the next pending `- [ ]` task:

   ```
   <!-- RESUME HERE: <one-line description of what to do next, e.g. "add skip call to members_test.go line 45"> -->
   - [ ] next task...
   ```

   This lets the next "Continue" proceed without re-reading any source files.

8. **On completion or pause, show status**

   Display:
   - Tasks completed this session
   - Overall progress: "N/M tasks complete"
   - If all done: suggest archive
   - If paused: explain why and wait for guidance

**Output During Implementation**

```
## Implementing: <change-name> (schema: <schema-name>)

Working on task 3/7: <task description>
[...implementation happening...]
✓ Task complete

Working on task 4/7: <task description>
[...implementation happening...]
✓ Task complete
```

**Output On Completion**

```
## Implementation Complete

**Change:** <change-name>
**Schema:** <schema-name>
**Progress:** 7/7 tasks complete ✓

### Completed This Session
- [x] Task 1
- [x] Task 2
...

All tasks complete! Ready to archive this change.
```

**Output On Pause (Issue Encountered)**

```
## Implementation Paused

**Change:** <change-name>
**Schema:** <schema-name>
**Progress:** 4/7 tasks complete

### Issue Encountered
<description of the issue>

**Options:**
1. <option 1>
2. <option 2>
3. Other approach

What would you like to do?
```

**Guardrails**
- Keep going through tasks until done or blocked
- On resumption (tasks already partially done): skip CLI status/instructions calls (steps 2–3) and go directly to reading tasks.md
- On resumption: look for `<!-- RESUME HERE: ... -->` marker first — if present, start at that task immediately and remove the marker; if absent, find first `- [ ]`
- On resumption read tasks file only — do not re-read source files, spec files (proposal/specs/design), or design docs. These were already processed; re-reading them wastes context without providing new information.
- If task is ambiguous, pause and ask before implementing
- If implementation reveals issues, pause and suggest artifact updates
- Keep code changes minimal and scoped to each task
- Update task checkbox **immediately** after completing each task — not in batches
- **Never mark `[x]` based on code written alone** — always verify with a build, test, or curl smoke test first
- Pause on errors, blockers, or unclear requirements - don't guess
- Use contextFiles from CLI output, don't assume specific file names
- Trust tasks.md as authoritative on resumption — do not re-verify completed tasks by inspecting files
- **When using subagents (Task/explore tool) to gather information, write targeted prompts**: ask for exactly what you need (e.g., "Does this directory contain Go files or docs infrastructure?") — not a broad codebase survey. Overly broad subagent prompts produce hundreds of lines of output for questions answerable in 2 sentences.
- **Before any code change that will trigger hot-reload (e.g., Air in Go projects)**: stop any background jobs (uploads, processing workers, long-running scripts) first. Restart them only after the rebuild completes. Editing code while background jobs run against a hot-reloading server causes orphaned DB records and in-flight failures.
- **In multi-tenant/multi-project platforms**: never add per-resource configuration as a global env var or global config field. Prefer database-stored per-resource config (e.g., a `config` column on the resource table). Propose this pattern before implementing if the design asks for a new env var that is logically per-resource.

**Fluid Workflow Integration**

This skill supports the "actions on a change" model:- **Can be invoked anytime**: Before all artifacts are done (if tasks exist), after partial implementation, interleaved with other actions
- **Allows artifact updates**: If implementation reveals design issues, suggest updating artifacts - not phase-locked, work fluidly
