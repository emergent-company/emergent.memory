---
description: Analyze recent paseo/opencode coding sessions and propose reusable improvements (instructions, skills, commands).
agent: build
---

Analyze recent coding sessions (paseo-managed opencode agents) and produce concrete improvement suggestions. Use the `reflect` skill methodology: evidence-driven, smallest-useful-form, propose before changing.

## 1. Discover sessions

Call `paseo_list_agents` (default: recent 48h; add `sinceHours` to widen). Focus on non-archived agents whose title/work represents real coding work. Skip the current session (this one) and any trivial/abandoned placeholders.

Optional filter from the user — target specific sessions, a focus area, or a repo:

$ARGUMENTS

## 2. Pull each session's activity

For each target session, call `paseo_get_agent_activity` with its `agentId`. The response is usually large; opencode saves truncated output to a file under `/root/.local/share/opencode/tool-output/`. Do NOT read those files yourself.

## 3. Analyze (delegate in parallel)

For each session, launch an `explorer` agent to read its saved activity file and return a compact per-session summary:
- goal, and outcome (done / partial / blocked, and where it ended)
- key actions and files touched
- friction: repeated retries, failed commands, wrong approach, re-doing work, repo facts the agent had to rediscover
- patterns worth noting (delegation quality, verification loops, dead ends)

Run these `explorer` agents in parallel.

## 4. Aggregate across sessions

Combine the per-session summaries. Identify repeated friction that appears in **≥2 sessions**:
- the same command/step done repeatedly, or repeatedly rediscovered
- the same repo fact repeatedly re-learned (paths, module layout, build/CLI quirks, auth/git quirks)
- inconsistent delegation or verification behavior
- the same conflict-prone files or merge/rebase rabbit-holes

For each candidate, score frequency / cost / risk / stability, and check whether an existing asset already covers it (read `AGENTS.md`, existing skills, commands, agents).

## 5. Recommend improvements

For strong candidates, choose the smallest useful form:
- `AGENTS.md` line/section (repo facts, workflow rules)
- skill (reusable workflow guidance)
- command (repeatable trigger)
- config/permission change
- no change (weak, one-off, or already covered)

Never manufacture assets. Prefer `AGENTS.md` over a skill, a skill over a command, and "create nothing" when evidence is weak.

## 6. Report — propose before changing

Return a compact report, then propose before editing any config/instructions:

```text
Findings
- <pattern>: evidence (sessions), frequency/confidence, impact.

Recommended changes
- <asset>: one-line purpose and why it's the smallest useful form.

Skipped
- <candidate>: why not worth packaging now.

Needs more evidence
- <candidate>: what would confirm it.
```

Do not silently modify global config, prompts, or permissions. Present the proposal and wait for confirmation before writing files.
