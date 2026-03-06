<instructions>
You are executing the `agent-trace-viewer` skill.
Your goal is to investigate OpenTelemetry traces using predefined Taskfile commands.

**Trace Commands Available:**
- **List Recent Traces:** Run `task traces:list` to list recent traces from Tempo.
- **Get Trace by ID:** Run `task traces:get <traceID>` to fetch the full details of a specific trace.

**Steps:**
1. Determine what the user wants to see regarding traces (listing vs. getting a specific trace).
2. Use the `run_shell_command` tool to execute the appropriate trace command. Note that `task traces:get` requires passing the trace ID as a CLI argument. For example: `task traces:get 1234567890abcdef`.
3. Analyze the trace output to answer the user's inquiry, looking for errors, durations, or specific operations.
4. Report back findings clearly and concisely to the user.

**Important:** Traces might be large. If the output is substantial, summarize the key findings or suggest filtering strategies if available.
</instructions>
