# Task Coordinator Design: Who Orchestrates the Agents?

_Design Date: February 14, 2026_
_Updated: February 15, 2026 — Aligned with emergent reality, product layer design, and sub-agent safety mechanisms_

## The Question

Given a task DAG in the knowledge graph (SpecTask objects with `blocks` relationships), **who coordinates which tasks get dispatched to which agents, when, and in parallel?** And what does it mean to store sessions in the graph?

---

## What Already Exists in Emergent

The knowledge graph and agent infrastructure provide the building blocks:

| Component                    | What it does                                            |
| ---------------------------- | ------------------------------------------------------- |
| `graph.Service.CreateObject` | Creates SpecTask entities in the knowledge graph        |
| `graph.Service.GetRelated`   | Traverses `blocks` relationships to find dependencies   |
| `graph.Service.QueryObjects` | Finds tasks by status (pending, unblocked, unassigned)  |
| `graph.Service.UpdateObject` | Sets status to `in_progress`, `completed`, etc.         |
| `graph.Service.TraverseBFS`  | Walks the full dependency chain for context gathering   |
| `AgentRun` entity            | Tracks run status, duration, summary, errors            |
| `AgentDefinition` (planned)  | Product-defined agents with system prompt, model, tools |
| `AgentExecutor` (planned)    | Builds ADK-Go pipeline from agent definition, runs it   |

The Agent entity already has `name`, `strategy_type`, `prompt`, `capabilities`, `config`. The `AgentRun` entity tracks execution lifecycle with status, duration, summary, and error messages.

**The gap**: Nothing connects these. No component queries for available tasks, selects agents, dispatches via ADK-Go, monitors completion, and marks tasks done to unlock the next wave.

---

## Approach A: Code Coordinator (Go Loop)

A `TaskDispatcher` goroutine that mechanically drives the task DAG.

### How It Works

```go
type TaskDispatcher struct {
    graphService  *graph.Service     // Knowledge graph operations
    executor      *AgentExecutor     // ADK-Go pipeline builder + runner
    maxParallel   int                // Max concurrent agent goroutines
    agentSelector AgentSelector      // Strategy for picking agents
    activeRuns    map[string]*ActiveRun
    taskTimeout   time.Duration      // Default timeout per task (overridable per agent definition)
    mu            sync.Mutex
}

func (d *TaskDispatcher) Run(ctx context.Context, projectID string) error {
    for {
        // 1. Get available tasks (pending + unblocked + unassigned)
        available := d.queryAvailableTasks(ctx, projectID)
        if len(available) == 0 {
            if d.allTasksCompleted(ctx, projectID) {
                return nil // Done
            }
            // Tasks exist but all blocked or in-progress — wait
            time.Sleep(5 * time.Second)
            continue
        }

        // 2. How many slots do we have?
        d.mu.Lock()
        slots := d.maxParallel - len(d.activeRuns)
        d.mu.Unlock()
        if slots <= 0 {
            time.Sleep(5 * time.Second)
            continue
        }

        // 3. Dispatch available tasks up to slot limit
        batch := available[:min(slots, len(available))]
        for _, task := range batch {
            agent := d.agentSelector.Pick(task)
            d.graphService.UpdateObject(ctx, task.ID, map[string]any{
                "status":         "in_progress",
                "assigned_agent": agent.Name,
            })

            go d.executeTask(ctx, task, agent)
        }
    }
}

func (d *TaskDispatcher) executeTask(ctx context.Context, task GraphObject, agent AgentDefinition) {
    // Build prompt with full context from graph
    prompt := d.buildPrompt(ctx, task)

    // Apply timeout: agent definition's default_timeout, overridden by dispatcher's taskTimeout
    timeout := d.taskTimeout
    if agent.DefaultTimeout != nil {
        timeout = *agent.DefaultTimeout
    }
    ctx, cancel := context.WithTimeout(ctx, timeout)
    defer cancel()

    // Create Session object in graph
    session := d.graphService.CreateObject(ctx, "Session", map[string]any{
        "status":       "active",
        "agent_name":   agent.Name,
        "task_context":  task.Properties["title"],
        "started_at":   time.Now(),
    })
    d.graphService.CreateRelationship(ctx, task.ID, session.ID, "has_session")

    // Execute via ADK-Go pipeline
    // AgentExecutor enforces step limits (agent.MaxSteps, default 50 for sub-agents)
    // and doom loop detection (3 identical consecutive tool calls → error injection)
    // Full message history + tool calls persisted in kb.agent_run_messages / kb.agent_run_tool_calls
    run, err := d.executor.Execute(ctx, agent, prompt)
    // AgentExecutor internally:
    //   - Creates Gemini model via Vertex AI
    //   - Resolves tools via ToolPool.ResolveTools(agentDef) — filtered by agent's tools whitelist
    //   - Runs pipeline as goroutine with step limit enforcement
    //   - Tracks via AgentRun (including step_count, max_steps)
    //   - Persists all messages + tool calls for observability and resumption

    // Update session with results
    d.graphService.UpdateObject(ctx, session.ID, map[string]any{
        "status":        "completed",
        "completed_at":  time.Now(),
        "message_count": run.MessageCount,
    })

    // Record completion or handle pause/failure
    if err == nil && run.Status == "completed" {
        d.graphService.UpdateObject(ctx, task.ID, map[string]any{
            "status": "completed",
            "metrics": map[string]any{
                "completion_time":  time.Now(),
                "duration_seconds": run.Duration.Seconds(),
                "step_count":      run.StepCount,
            },
        })
    } else if run.Status == "paused" {
        // Agent hit step limit or timeout — partial work done
        // The run can be resumed later via spawn_agents with resume_run_id
        d.graphService.UpdateObject(ctx, task.ID, map[string]any{
            "status":   "paused",
            "run_id":   run.ID,  // store for potential resumption
            "summary":  run.Summary,
        })
    } else {
        d.handleFailure(ctx, task, run, err)
    }

    // Remove from active runs
    d.mu.Lock()
    delete(d.activeRuns, task.ID)
    d.mu.Unlock()

    // Newly-unblocked tasks are picked up on next poll iteration
}
```

### What the Code Coordinator Decides

| Decision                    | How                                                                                           |
| --------------------------- | --------------------------------------------------------------------------------------------- |
| **Which tasks to run**      | Graph query for pending + unblocked + unassigned — pure query, no judgment                    |
| **How many in parallel**    | `maxParallel` config (e.g., 3)                                                                |
| **Which agent per task**    | `AgentSelector` strategy — tag matching, specialization field match                           |
| **What context to include** | `buildPrompt` — deterministic template that pulls design, specs, related artifacts from graph |
| **When to retry**           | On execution error — re-queue task as pending, increment retry count                          |
| **When to stop**            | All tasks completed or max retries exceeded                                                   |

### What the Code Coordinator Does NOT Decide

- Whether the task breakdown is correct
- Whether the agent's output is good enough
- Whether to modify the plan mid-execution
- Whether two tasks actually conflict even though they don't have a `blocks` relationship

### Constraints

```
Constraint 1: DETERMINISTIC DISPATCH
  Available tasks are dispatched in order. No judgment about
  "this task would benefit from running after that other one
  even though there's no formal dependency."

Constraint 2: STATIC AGENT SELECTION
  Agent selection is based on metadata (tags, specialization)
  not on understanding the task content. "This research task
  should go to the agent that just finished the other research
  task and has context" — the code coordinator can't reason
  about this unless you explicitly encode it.

Constraint 3: NO MID-FLIGHT ADAPTATION
  If task 3.1 reveals that the design was wrong and task 3.2
  should be modified, the code coordinator has no mechanism
  to pause 3.2 and revise the plan. It just executes the DAG
  as given.

Constraint 4: FIXED CONTEXT WINDOW
  The prompt template is static. It pulls the same categories
  of context for every task. Can't reason about "this particular
  task needs the test output from task 2.3 but not the design
  decisions from 1.1."
```

### Cost

Zero LLM tokens for coordination. All tokens go to the actual work.

---

## Approach B: LLM Coordinator

An LLM agent that has graph tools + agent dispatch tools. It reads the task DAG, reasons about what to do, and dispatches agents.

### How It Works

The coordinator is itself an ADK-Go agent with a system prompt like:

```
You are a task coordinator for the emergent knowledge graph. You have access to:

- query_tasks(project_id) — get tasks ready to work on (pending + unblocked)
- get_task_dependencies(task_id) — understand the dependency structure
- assign_task(task_id, agent_name) — assign a task to an agent
- complete_task(task_id, summary) — mark task done
- dispatch_agent(agent_name, task_id, prompt) — execute an agent via ADK-Go pipeline
- get_agent_run(run_id) — check if an agent finished
- get_session(session_id) — read a previous session's output

Your job:
1. Check what tasks are available
2. Decide which agents should handle which tasks
3. Dispatch them (in parallel when appropriate)
4. Monitor completion
5. When tasks complete, check what's newly available
6. Repeat until all tasks are done

You may also:
- Modify the plan if you discover issues
- Skip tasks that become unnecessary
- Re-assign tasks if an agent fails
- Inject context from completed tasks into new ones
```

### What the LLM Coordinator Decides

Everything the code coordinator does, PLUS:

| Decision               | How                                                                                                                               |
| ---------------------- | --------------------------------------------------------------------------------------------------------------------------------- |
| **Context selection**  | Reads completed task outputs and selects relevant context per task                                                                |
| **Agent affinity**     | "Agent X just finished the data model — give it the related API task too"                                                         |
| **Plan adaptation**    | "Task 3.1 output shows the design had wrong assumptions. I need to update task 3.2's prompt to account for the actual findings."  |
| **Conflict detection** | "Tasks 2.1 and 2.3 both touch the same graph entities. I'll run 2.1 first even though they're technically independent."           |
| **Quality gating**     | Reads agent output before marking complete — "This implementation is missing edge case handling, I'll re-dispatch with feedback." |

### Constraints

```
Constraint 1: TOKEN COST
  Every coordination decision costs tokens. For a 20-task
  workflow, the coordinator might make 40+ LLM calls just for
  coordination (not counting the actual work). With a smart
  model this could be $5-15 in coordination overhead alone.

Constraint 2: LATENCY
  Each coordination decision takes 2-10 seconds for the LLM
  to reason. For parallel dispatch of 4 tasks, that's 4
  sequential decisions before any work starts.

Constraint 3: UNPREDICTABILITY
  The LLM might make different decisions on the same input.
  "Why did it assign the testing task to the implementation
  agent?" Hard to debug, hard to reproduce.

Constraint 4: CONTEXT WINDOW
  As tasks complete and the coordinator accumulates context,
  it may hit token limits. Needs compaction strategy.

Constraint 5: FAILURE MODES
  If the coordinator LLM hallucinates a task ID or makes a
  bad tool call, the whole pipeline stalls. The code coordinator
  can't hallucinate.
```

### Cost

Significant. Roughly 10-30% additional token cost on top of the actual work, depending on task count and how much reasoning is needed per dispatch.

---

## Approach C: The Hybrid (Recommended)

Code handles the mechanical loop. LLM handles the judgment calls.

### Architecture

```
┌─────────────────────────────────────────────────────────────┐
│                    TASK DISPATCHER (Go)                      │
│                                                             │
│  Loop:                                                      │
│    1. graphService.QueryObjects("SpecTask", pending+unblocked)│
│    2. For each available task:                               │
│       a. LLM call: "Given this task + context,              │
│          which agent? what context to include?"              │
│       b. graphService.UpdateObject(task, in_progress)        │
│       c. executor.Execute(agentDef, prompt)  ← goroutine    │
│    3. Monitor active goroutines for completion               │
│    4. On completion:                                         │
│       a. LLM call: "Is this output acceptable?              │
│          Mark complete or retry with feedback?"              │
│       b. graphService.UpdateObject(task, completed) or retry │
│    5. Repeat                                                │
│                                                             │
│  The loop itself is Go code — deterministic, testable.       │
│  The DECISIONS within the loop are LLM calls — flexible.     │
└─────────────────────────────────────────────────────────────┘
```

### What's Code vs What's LLM

| Aspect                            | Code (Go) | LLM |
| --------------------------------- | --------- | --- |
| Available task query              | x         |     |
| Parallel slot management          | x         |     |
| ADK-Go pipeline dispatch          | x         |     |
| AgentRun monitoring               | x         |     |
| Session lifecycle                 | x         |     |
| Graph entity updates              | x         |     |
| Which agent for this task         |           | x   |
| What context to include in prompt |           | x   |
| Is this output good enough        |           | x   |
| Should we retry or escalate       |           | x   |
| Should we modify the plan         |           | x   |

### LLM Calls Per Task

| When              | LLM Call                           | Cost                           |
| ----------------- | ---------------------------------- | ------------------------------ |
| Before dispatch   | "Pick agent + build prompt"        | ~1K tokens in, ~500 tokens out |
| After completion  | "Evaluate output quality"          | ~2K tokens in, ~200 tokens out |
| On failure (rare) | "Diagnose + decide retry strategy" | ~1K tokens in, ~500 tokens out |

For a 15-task workflow: ~15 dispatch calls + ~15 evaluation calls = ~30 lightweight LLM calls. At ~$0.01-0.05 each, total coordination cost is $0.30-1.50. Compared to the actual agent work (likely $5-50), this is negligible.

---

## Sessions in the Knowledge Graph

### Why

Without persistent sessions, agent execution is ephemeral — once an ADK-Go pipeline finishes, the conversation is gone. This means:

- No record of what an agent tried, what it saw, what it produced
- No ability to resume a failed task from where it left off
- No cross-task learning ("last time we assigned a research task to agent X, it took 45 seconds and used 3 tool calls")
- No audit trail for debugging ("why did the test fail after task 3.2?")

### Entity Model

Using the emergent-coordination template pack (defined in the architecture doc):

```
Object types:
  Session         — An agent work session for a task
  SessionMessage  — A single message in a session

Relationships:
  has_session     — SpecTask → Session
  has_message     — Session → SessionMessage (with sequence property)
  follows_up      — Session → Session (retry chain)
  informed_by     — Session → Session (context dependency)
```

```go
// Session stored as a graph object with these properties:
{
    "status":       "active|completed|failed|cancelled",
    "agent_name":   "research-assistant",  // Which agent definition
    "task_context":  "Research tagging patterns",
    "started_at":   "2026-02-15T10:00:00Z",
    "completed_at": "2026-02-15T10:00:15Z",
    "message_count": 8,
    "tokens_in":    1500,
    "tokens_out":   800,
    "retry_of":     "session-abc",         // Previous session ID if retry
    "retry_reason": "Missing cycle detection"
}

// SessionMessage stored as a graph object with these properties:
{
    "role":      "system|user|assistant|tool",
    "content":   "Based on this research...",
    "sequence":  1,
    "timestamp": "2026-02-15T10:00:01Z"
}
```

### Graph Shape

```
SpecTask("research-tagging")  status: completed
  │
  └── has_session ──► Session(agent: "research-assistant", status: "completed")
                       │
                       ├── has_message ──► SessionMessage(role: system, seq: 1)
                       ├── has_message ──► SessionMessage(role: user, seq: 2)
                       ├── has_message ──► SessionMessage(role: assistant, seq: 3)
                       └── has_message ──► SessionMessage(role: tool, seq: 4)

SpecTask("implement-tagging")  status: completed
  │
  ├── has_session ──► Session(agent: "implement-agent", status: "failed")
  │                    │    retry_reason: "missing edge case handling"
  │                    │
  │                    └── follows_up ──►
  │
  └── has_session ──► Session(agent: "implement-agent", status: "completed")
                       │
                       └── informed_by ──► Session (from research task)
```

### What This Enables

**1. Resumable sessions**
If a task fails and we retry, the new session has `follows_up` pointing to the failed one. The coordinator includes the failure output in the retry prompt: "Previous attempt failed with: [error]. Fix this specific issue."

**2. Context threading**
When task B depends on task A, the coordinator follows `blocks` → finds A's completed session → reads its output → injects relevant parts into B's prompt. The `informed_by` relationship explicitly tracks which sessions informed which.

**3. Agent performance tracking**
Query: "For agent definition X, what's the average session duration, retry rate, and token usage?" — all answerable from the graph.

```
Graph traversal: AgentDefinition("research-assistant")
  → find all Sessions where agent_name matches
  → avg(duration), count(where status="failed") / count(*), sum(tokens_used)
```

**4. Audit trail**
"Why did task 3.2 produce incorrect output?" → Follow `has_session` → read SessionMessages → see exactly what prompt was sent and what the agent did.

**5. Cross-workflow learning**
"Last time we had a research task, which agent definition was fastest?" — query across all workflows.

---

## How the Full System Fits Together

```
USER REQUEST / TRIGGER / SCHEDULE
    │
    ▼
┌──────────────────────────────────────────────────────────┐
│  TASK DAG CREATION                                       │
│                                                          │
│  Task planner agent creates SpecTask objects in graph     │
│  with blocks relationships, complexity estimates, and     │
│  priority levels.                                        │
│                                                          │
│  At this point the task DAG exists with dependency        │
│  relationships and task descriptions.                    │
└──────────────────────────────┬───────────────────────────┘
                               │
                               ▼
┌──────────────────────────────────────────────────────────┐
│  TASK DISPATCHER (new domain/coordination/ module)        │
│                                                          │
│  Go loop:                                                │
│    ┌──────────────────────────────────────────────────┐   │
│    │  1. graphService.QueryObjects("SpecTask",        │   │
│    │     {status: "pending", unblocked: true})         │   │
│    │     → [Task 1.1, Task 1.2, Task 2.1]            │   │
│    │                                                  │   │
│    │  2. For each (up to maxParallel):                │   │
│    │     a. LLM: pick agent + build prompt            │   │
│    │     b. Create Session in graph                   │   │
│    │     c. graphService.UpdateObject(task, in_progress)│  │
│    │     d. go executor.Execute(agentDef, prompt)      │   │
│    │                                                  │   │
│    │  3. Monitor active goroutines:                   │   │
│    │     for each active run:                         │   │
│    │       if AgentRun.Status == completed:            │   │
│    │         LLM: evaluate output quality             │   │
│    │         if good:                                 │   │
│    │           graphService.UpdateObject(task, done)   │   │
│    │           → newly-unblocked tasks picked up next │   │
│    │         else:                                    │   │
│    │           create retry Session (follows_up)      │   │
│    │           re-dispatch                            │   │
│    │       if AgentRun.Status == error:                │   │
│    │         record failure in Session                │   │
│    │         retry or escalate                        │   │
│    │                                                  │   │
│    │  4. Sleep(pollInterval)                          │   │
│    │  5. Loop until all tasks completed or max retries│   │
│    └──────────────────────────────────────────────────┘   │
│                                                          │
└──────────────────────────────────────────────────────────┘
                               │
            ┌──────────────────┼──────────────────┐
            ▼                  ▼                  ▼
     ┌─────────────┐   ┌─────────────┐   ┌─────────────┐
     │  ADK-Go     │   │  ADK-Go     │   │  ADK-Go     │
     │  goroutine  │   │  goroutine  │   │  goroutine  │
     │             │   │             │   │             │
     │ research-   │   │ spec-       │   │ implement-  │
     │ assistant   │   │ writer      │   │ agent       │
     │ Task 1.1    │   │ Task 1.2    │   │ Task 2.1    │
     │ Session A   │   │ Session B   │   │ Session C   │
     └──────┬──────┘   └──────┬──────┘   └──────┬──────┘
            │                 │                  │
            └─────────────────┼──────────────────┘
                              │
                              ▼
                    ┌──────────────────┐
                    │  Knowledge Graph │
                    │                  │
                    │  SpecTask DAG    │
                    │  ├── Sessions    │
                    │  ├── Messages    │
                    │  ├── Discussions │
                    │  └── Artifacts   │
                    └──────────────────┘
```

### Concrete Example: Task 1.1 Completes, Unlocking 2.1 and 2.3

```
TIME 0:00 — Initial state
  Available: [1.1, 1.2, 1.3]    (no blockers)
  Blocked:   [2.1, 2.3, 3.1]    (blocked by 1.x tasks)

  Dispatcher creates 3 Sessions, selects agents, dispatches goroutines

TIME 0:15 — Task 1.2 completes first
  graphService.UpdateObject("1.2", {status: "completed"})
  → unblocked: [2.1]           (2.1 was only blocked by 1.2)

  Dispatcher sees 2.1 is now available on next poll
  LLM call: "Task 2.1 needs the findings from 1.2's output"
    → reads Session B's output from graph
    → includes relevant parts in 2.1's prompt
  Creates Session D, dispatches goroutine

TIME 0:30 — Tasks 1.1 and 1.3 complete
  graphService.UpdateObject("1.1", {status: "completed"})
    → unblocked: [2.3]           (2.3 was blocked by 1.1 + 1.2, both now done)
  graphService.UpdateObject("1.3", {status: "completed"})
    → unblocked: []              (nothing was only blocked by 1.3)

  Dispatcher sees 2.3 is now available
  Creates Session E, dispatches goroutine

TIME 1:00 — Task 2.1 fails
  AgentRun error: "graph object creation failed — duplicate name"
  LLM evaluation: "Fixable error, retry with context"

  Creates Session F (follows_up → Session D)
  Prompt includes: "Previous attempt failed with: 'duplicate name for Tag entity'.
                    Check existing entities before creating. See: [output from Session A]"
  Re-dispatches goroutine

TIME 1:15 — Task 2.1 retry succeeds
  graphService.UpdateObject("2.1", {status: "completed"})
    → unblocked: [3.1]

  And so on...
```

---

## Comparison Table

| Dimension                       | Code Coordinator          | LLM Coordinator           | Hybrid                                 |
| ------------------------------- | ------------------------- | ------------------------- | -------------------------------------- |
| **Token cost for coordination** | $0                        | $5-15 per workflow        | $0.30-1.50 per workflow                |
| **Latency per dispatch**        | <10ms                     | 2-10s                     | 2-5s (one LLM call)                    |
| **Agent selection quality**     | Tag matching (simple)     | Contextual reasoning      | Contextual reasoning                   |
| **Context building quality**    | Template-based (rigid)    | Selective (intelligent)   | Selective (intelligent)                |
| **Plan adaptation**             | None                      | Full                      | On-failure only                        |
| **Output quality gating**       | None (trust agent)        | Full review               | Quick evaluation                       |
| **Debuggability**               | Excellent (deterministic) | Poor (non-deterministic)  | Good (code loop, LLM decisions logged) |
| **Failure modes**               | Predictable               | Can hallucinate           | LLM failures isolated to decisions     |
| **Testability**                 | Unit testable             | Hard to test              | Code loop testable, LLM calls mockable |
| **Complexity**                  | Low (~200 LOC)            | High (~500 LOC + prompts) | Medium (~350 LOC + 2 prompts)          |

---

## Constraints Summary

Regardless of approach, these constraints apply:

### Hard Constraints (from task DAG design)

1. **Task DAG is authoritative** — `blocks` relationships determine execution order. The coordinator cannot violate these.
2. **One agent per task** — `assigned_agent` is singular. No splitting a task across agents (though tasks can spawn discussions with multiple agents).
3. **Status state machine** — pending → in_progress → completed/failed. No skipping states.
4. **Task completion unlocks** — marking a task complete is the only way to unblock downstream tasks.

### Soft Constraints (design decisions)

5. **Max parallelism** — How many agent goroutines run simultaneously? Resource and cost limit.
6. **Retry budget** — How many retries per task before escalating to user?
7. **Session depth** — How many messages in a session before compacting?
8. **Cross-task context** — How much output from task A goes into task B's prompt?

### Open Questions

9. **Graph conflict detection** — Two parallel tasks creating/modifying the same graph objects? The DAG doesn't encode this. Options: (a) trust the task generator to add `blocks` for conflicting entities, (b) coordinator detects at dispatch time by checking task descriptions, (c) use PostgreSQL transactions to handle conflicts.

10. **Agent pool management** — Ephemeral goroutines (spawned per task, clean state) vs persistent workers (take tasks from a queue, maintain context). **Recommendation**: Ephemeral goroutines — follows the extraction pipeline pattern, simpler lifecycle.

11. **User visibility** — How does the user see progress? Options: (a) notifications per task completion, (b) live admin dashboard showing the DAG with color-coded status, (c) periodic summary.

12. **Scope** — Does the dispatcher handle one workflow at a time, or multiple in parallel? If multiple, what about cross-workflow conflicts?

### Relationship to State Persistence & Safety Mechanisms

The TaskDispatcher leverages the same safety and persistence mechanisms described in `multi-agent-architecture-design.md` Sections 11-13:

| Mechanism                        | How TaskDispatcher Uses It                                                                                                                                                                                                                                                              |
| -------------------------------- | --------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| **Step limits** (`max_steps`)    | Each dispatched agent respects its definition's `max_steps`. If reached, the agent produces a summary and the run status becomes `paused`. The dispatcher can re-dispatch with a fresh step budget.                                                                                     |
| **Timeouts** (`default_timeout`) | Configured per agent definition or overridden by `TaskDispatcher.taskTimeout`. Uses Go `context.WithDeadline`. On timeout: soft stop (30s grace for summary), then hard cancel.                                                                                                         |
| **Doom loop detection**          | `DoomLoopDetector` runs inside `AgentExecutor`. If an agent makes 3 identical consecutive tool calls, an error message is injected instead of executing the tool. If the agent loops again, hard-stop.                                                                                  |
| **Full state persistence**       | Every agent run dispatched by TaskDispatcher has its full message history persisted in `kb.agent_run_messages` and tool calls in `kb.agent_run_tool_calls`. This enables: (1) retry with full context from prior attempt, (2) audit trail for debugging, (3) resumption of paused runs. |
| **Resumption**                   | When a dispatched task is paused (step limit or timeout), the dispatcher stores the `run_id`. On retry, it can use `resume_run_id` to continue from where the agent left off, with cumulative step counting.                                                                            |
| **MaxTotalStepsPerRun**          | Global safety cap of 500 steps across all resumes. Prevents infinite resume loops even if the dispatcher keeps retrying.                                                                                                                                                                |

The TaskDispatcher adds its own layer on top: the graph-level Session tracking. Each dispatch creates a Session object linked to the SpecTask, providing a graph-queryable audit trail alongside the relational `kb.agent_run_*` tables.

### Relationship to Agent-Initiated Coordination

The TaskDispatcher is one of **two coordination patterns** in the architecture. The other is **agent-initiated coordination** via `spawn_agents`, where a parent agent dynamically discovers and spawns sub-agents at runtime. Both patterns share the same `AgentExecutor` and `ToolPool` infrastructure.

| Aspect              | TaskDispatcher                                                                                                  | spawn_agents                                                                                     |
| ------------------- | --------------------------------------------------------------------------------------------------------------- | ------------------------------------------------------------------------------------------------ |
| **Who decides**     | Go code loop + LLM judgment calls                                                                               | Parent agent's LLM                                                                               |
| **Task structure**  | Explicit DAG (SpecTask + blocks)                                                                                | Ad-hoc, decided at runtime                                                                       |
| **Agent selection** | AgentSelector strategy                                                                                          | LLM picks from `list_available_agents` catalog                                                   |
| **Best for**        | Structured multi-step workflows                                                                                 | Dynamic delegation, research fan-out                                                             |
| **State**           | Full graph persistence (sessions) + relational persistence (`kb.agent_run_messages`, `kb.agent_run_tool_calls`) | `kb.agent_run_messages` + `kb.agent_run_tool_calls` (same persistence, no graph Session objects) |

See [research-agent-scenario-walkthrough.md](./research-agent-scenario-walkthrough.md) for the agent-initiated pattern.

---

## Recommendation

**Start with the Hybrid.** Here's why:

1. The Go loop gives you a solid, debuggable, testable foundation. You can run it without any LLM coordinator at all (just use tag-based agent selection) and it works.

2. Add LLM calls as upgrades, not dependencies:

   - **v1**: Code loop + tag-based agent selection + template prompts. Zero LLM coordination cost.
   - **v2**: Add LLM agent selection. "Given this task and available agent definitions, which agent?" — one cheap call using Gemini Flash.
   - **v3**: Add LLM output evaluation. "Is this output complete?" — one cheap call after each task.
   - **v4**: Add LLM context threading. "What from session A is relevant for task B?" — one call per dependent task.
   - **v5**: Add LLM plan adaptation. "Task 3.1 revealed X, should we modify 3.2?" — only on failure/surprise.

3. Sessions in the knowledge graph are the foundation that makes all of this work. Without persistent sessions, the LLM calls have no memory to work with. With them, even the pure code coordinator can do "retry with failure context from previous session."

The progression from v1 to v5 can happen incrementally. Each version works standalone. Each LLM call is optional and has a code fallback.

### Implementation in Emergent

The hybrid coordinator becomes a new `domain/coordination/` module:

```
apps/server-go/domain/coordination/
├── module.go           # fx module registration
├── dispatcher.go       # TaskDispatcher — the Go polling loop
├── selector.go         # AgentSelector — code + LLM hybrid
├── executor_bridge.go  # Bridges to domain/agents/executor.go
├── session_tracker.go  # Creates/updates Session objects in graph
└── handler.go          # HTTP endpoints for triggering workflows
```

This follows the same fx module pattern as every other domain package (`domain/agents/`, `domain/graph/`, `domain/extraction/`), wiring into the server via `cmd/server/main.go`.
