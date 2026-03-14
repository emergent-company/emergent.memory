---
name: improve-ask-agent
description: Analyze ask agent traces and conversation patterns to surface optimizations for the agent prompt, CLI, server, and documentation. Use when investigating ask agent quality or planning prompt improvements.
license: MIT
metadata:
  author: emergent
  version: "1.0"
---

Analyze recent `memory ask` agent traces to identify inefficiencies and surface actionable
improvements across the agent prompt, CLI UX, server behavior, and documentation gaps.

**Input**: Optional time window (e.g. "last 6h", "last 24h"). Defaults to last 24h.

---

## Step 1 — Collect traces

```bash
# List all POST /api/ask traces in the window
memory traces search --route "/api/ask" --since 24h --limit 50

# Also catch agent.run spans not wrapped in ask.run (direct invocations)
memory traces list --since 24h --limit 100
```

Pull the full span tree for every POST trace found:

```bash
memory traces get <traceId>
```

For each trace record:
- Total duration
- Whether it has an `ask.run` wrapper (newer) or bare `agent.run`
- All `call_llm` spans and their durations
- All `execute_tool` spans and **which tool** was called
- Success (`✓`) vs failure (`✗`) on the root span

---

## Step 2 — Count and categorize

Build a tally across all traces:

| Metric | What to count |
|---|---|
| Total `call_llm` calls | Sum across all traces |
| Total `execute_tool` calls | Sum and break down by tool name |
| Duplicate tool calls | Same tool name called 2+ times in one trace |
| Duplicate URL fetches | `web-fetch` calls with the same URL in one trace |
| Tool calls per trace | Distribution: min / median / max |
| LLM calls per trace | Distribution: min / median / max |
| Duration distribution | Buckets: <2s / 2–5s / 5–10s / >10s |
| Failed traces | Root span marked `✗` |
| Traces with no tools | Pure `call_llm` only — likely task questions answered without data |

---

## Step 3 — Identify the questions asked

Look up recent ask runs to retrieve the actual messages users sent:

```bash
# Get agent runs from the cli-assistant-agent
memory agents list --output json | jq '.[] | select(.name == "cli-assistant-agent")'

# Then check recent runs
memory agents runs <agent-id> --limit 20 --output json
```

For each run, retrieve the conversation to see the user message:

```bash
memory agents get-run <run-id> --output json
```

Categorize each message as:
- **DOCS_QUESTION** — "how do I...", "what is...", "show me the commands for..."
- **TASK** — "list my agents", "create a project called X", "delete object Y"
- **MIXED** — question requiring both docs and live data
- **GREETING/SHORT** — single word, test message, "hi", "help"
- **UNANSWERABLE** — asked for something the agent has no tools or docs for

---

## Step 4 — Identify prompt gaps and anti-patterns

For each anti-pattern found, note the trace ID(s) as evidence.

### A. Tool discipline violations

- **web-fetch on a TASK** — agent fetched docs when the user asked to do something
- **graph/agent tools on a DOCS_QUESTION** — agent called `agent-list`, `skill-list`, `document-list` when user just asked a question
- **skill-list for orientation** — `skill-list` called without subsequently calling `get_skill`

### B. Redundancy

- **Duplicate web-fetch** — same URL fetched 2+ times in one trace
- **Duplicate tool call** — any tool (not just web-fetch) called twice in one trace with same effective arguments
- **Extra planning LLM calls** — `call_llm` count exceeds number of tool calls + 1 by more than 2 (agent is reasoning excessively before acting)

### C. Coverage gaps

- **Question with no tool calls, long duration** — agent answered from memory but took >3s; may indicate the prompt forced unnecessary reasoning
- **Failed trace** — root span `✗`; investigate whether it was a prompt issue, tool error, or infra issue
- **Question asked but unanswerable** — agent hit a dead end; note what was missing

### D. UX / response quality issues

- **Unsolicited curl examples** — agent returned curl snippets when user asked a plain CLI question
- **Wrong command suggested** — agent suggested a relocated command (e.g. `memory mcp-servers` instead of `memory agents mcp-servers`)
- **Hallucinated flags** — agent described flags that do not exist (cross-check against `memory-cli-reference` skill)

---

## Step 5 — Identify platform gaps

Beyond the agent prompt, look for signals that the CLI, server, or docs need improvement.

### CLI gaps

- If users repeatedly ask "how do I do X" and X has a natural CLI command, consider:
  - Is the command name intuitive?
  - Is the `--help` output clear enough?
  - Is the command missing a shorthand or alias?

### Server / API gaps

- If the agent repeatedly calls multiple tools to answer one question (e.g. needs `list` then `get` to find basic info), consider:
  - Could a single endpoint return more complete data?
  - Is pagination forcing unnecessary round-trips?

### Documentation gaps

- If the agent consistently fetches the same doc pages for similar questions, those pages are likely incomplete or poorly structured.
- If the agent falls back to hallucination for a topic, that topic is missing from the docs.
- Note the exact URLs that returned insufficient content.

---

## Step 6 — Produce the improvement report

Structure the output as:

```
## Ask Agent Trace Analysis — <date/window>

### Summary
- N traces analyzed (N successful, N failed)
- Duration: min Xs / median Xs / max Xs
- Average LLM calls per trace: N
- Average tool calls per trace: N
- Most-used tools: web-fetch (N), agent-list (N), skill-list (N), ...

### Agent Prompt Improvements

1. **[Critical/High/Medium/Low]** <title>
   Evidence: trace IDs <id1>, <id2>
   Observation: <what was observed>
   Recommendation: <specific change to the system prompt, with exact wording if possible>

### CLI Improvements

1. **[Priority]** <title>
   Evidence: <question pattern or trace>
   Observation: <what users are struggling with>
   Recommendation: <specific CLI/UX change>

### Server / API Improvements

1. **[Priority]** <title>
   Evidence: <trace or pattern>
   Observation: <inefficiency or gap>
   Recommendation: <API or server-side change>

### Documentation Improvements

1. **[Priority]** <title>
   Evidence: <question pattern, doc URL that was insufficient>
   Observation: <what information was missing or unclear>
   Recommendation: <specific doc change or new page to add>
```

---

## Step 7 — Implement prompt fixes (optional)

If the user wants to apply prompt improvements immediately:

1. Open `apps/server/domain/agents/repository.go`
2. Locate `cliAssistantAgentSystemPrompt` (search for `const cliAssistantAgentSystemPrompt`)
3. Edit the relevant section based on the recommendations
4. Verify the file compiles: `cd apps/server && GOWORK=off go build ./domain/agents/...`
5. Note: `EnsureCliAssistantAgent` auto-propagates the updated prompt on the next `memory ask` call — no migration needed

---

## Guardrails

- Always pull the actual trace spans before drawing conclusions — do not infer tool usage from duration alone
- When classifying questions, read the `memory.ask.message_preview` attribute on the `ask.run` span if available; otherwise use `agents get-run`
- Do NOT suggest increasing `maxSteps` (currently 20) unless there is clear evidence that agents are hitting the limit — the current limit is appropriate
- Do NOT suggest switching the model unless benchmarking shows a quality problem — `gemini-3.1-flash-lite-preview` is intentionally chosen for low latency
- Prompt changes should be minimal and surgical — avoid restructuring sections that are working correctly
- Cross-check any "wrong command" findings against the actual CLI `--help` output before reporting them
