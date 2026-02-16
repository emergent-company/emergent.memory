# OpenCode vs Diane: Agent Orchestration Comparison

*Analysis Date: February 14, 2026*

## Executive Summary

OpenCode and Diane's proposed multi-agent system represent two fundamentally different philosophies for agent orchestration. **OpenCode is LLM-driven** — the model decides when and how to spawn subagents, with no explicit phase management. **Diane proposes programmatic orchestration** — a Go-coded orchestrator that walks through explicit phases, manages retries, and coordinates agent discussions. This document analyzes both approaches in depth and recommends a hybrid architecture for Diane.

---

## Part 1: How OpenCode Actually Orchestrates Agents

### Architecture Overview

OpenCode is a TypeScript project (Bun runtime) with a SQLite-backed session system. Its agent orchestration is built on three primitives:

1. **SessionPrompt.loop()** — a `while(true)` loop that streams LLM responses, executes tool calls, and repeats until the model stops
2. **Task tool** — spawns a child session with its own loop, returns the result to the parent
3. **Sessions plugin** (community) — adds 4 structured collaboration patterns on top

### The Core Loop (`packages/opencode/src/session/prompt.ts`)

```
while (true) {
    1. Check abort signal
    2. Fetch all messages for this session
    3. EXIT if last assistant message has terminal finish reason
    4. Increment step counter
    5. Check for context overflow → compact if needed
    6. Check maxSteps → inject "summarize and stop" if exceeded
    7. Resolve available tools
    8. Call LLM.stream() → get model response
    9. If model made tool calls → continue loop (tools auto-executed)
    10. If model stopped → break
}
```

**Key property**: The model decides everything — what tools to call, when to spawn subagents, when to stop. There is no programmatic phase management. The loop simply keeps going until the LLM says it's done.

**Stopping conditions**: abort signal, model says stop, max steps reached (configurable per agent), context overflow, permission rejection, or error.

**Doom loop detection**: If the same tool is called with identical arguments 3 times consecutively, the system asks the user to confirm continuation.

### The Task Tool (`packages/opencode/src/tool/task.ts`)

This is the key mechanism for multi-agent orchestration:

```
Parent Agent (Build)
    │
    ├── Model decides to call Task tool
    │   Parameters: { subagent_type: "explore", prompt: "Find all auth files" }
    │
    ├── Task tool creates new Session(parentID = current session)
    │   Child gets: ONLY the prompt text (no parent history)
    │   Child gets: Its own agent system prompt + tool permissions
    │   Child denied: todowrite, todoread, task (by default)
    │
    ├── Task tool calls SessionPrompt.prompt() for child session
    │   This runs the FULL agent loop in the child session
    │   Same process, same event loop, isolated message history
    │
    ├── Child loop completes → returns last text part
    │   Wrapped in <task_result> tags + session ID for resumption
    │
    └── Parent receives tool result, continues its own loop
```

**Critical design decisions**:

| Decision | OpenCode's Choice | Implication |
|----------|------------------|-------------|
| **Context isolation** | Child gets ONLY the prompt, no parent history | Parent must put everything relevant in the prompt string |
| **Process model** | Same process, different session | Lightweight, but shares memory/event loop |
| **Tool restrictions** | Child can't manage parent's todos or spawn sub-subagents | Prevents recursive agent sprawl |
| **Resumption** | Pass `task_id` to continue a previous child session | Enables multi-turn subagent interactions |
| **Concurrency** | Multiple Task calls run in parallel | Model can spawn many subagents in one response |
| **Abort propagation** | Parent abort → all children abort | Clean shutdown chain |

### The Sessions Plugin (4 Modes)

The community `opencode-sessions` plugin (~560 lines TypeScript) adds structured collaboration on top of OpenCode's primitives:

#### Mode 1: `message` — Agent Relay Pattern

```
Build Agent working in Session X
    │
    ├── Calls session({ mode: "message", agent: "review", text: "Review auth" })
    │   Tool stores {agent, text} in pendingMessages Map
    │   Returns immediately — Build finishes its turn
    │
    ├── Session becomes idle → fires session.idle event
    │   Plugin picks up pending message
    │   Calls session.prompt() with agent="review" in SAME session
    │
    └── Review Agent receives message in same conversation
        Sees full history of Build's work
        Can call session() again to pass back to Build
```

**Key insight**: Both agents share the same session and conversation history. This is the opposite of the Task tool (which isolates). The deferred delivery via `session.idle` prevents deadlocks from concurrent session writes.

#### Mode 2: `new` — Clean Phase Transitions

Creates a brand new session, sends the prompt as the first message. **Zero context carry-over** — only what's explicitly in the prompt crosses the boundary. Useful for preventing "context bleed" between phases.

#### Mode 3: `compact` — Conversation Compression

Three-phase state machine: inject context marker → wait for session idle → trigger compaction → wait for compaction complete → send new message with compressed history. Handles long conversations nearing token limits.

#### Mode 4: `fork` — Parallel Exploration

Copies entire message history to a new session, sends a divergent prompt. No automatic reconciliation — user manually compares forks. Designed for architectural exploration, not parallel implementation.

---

## Part 2: Diane's Proposed 3-Loop Architecture

### Architecture Overview

Diane proposes a Go-coded orchestrator that explicitly manages phases, agent discussions, and retries:

```
ORCHESTRATOR LOOP (outer)
    │ Walks phases: Research → Design → Implement → Test → Review
    │ Handles retries when test/review fails
    │
    ├── DISCUSSION LOOP (middle)
    │   Agents exchange proposals/feedback until consensus (max 3 rounds)
    │   Uses ACP RunSync calls to get each agent's response
    │
    └── AGENT EXECUTION LOOP (inner)
        Polls async ACP agents for completion every 5 seconds
        Handles status: in_progress, completed, failed, awaiting
```

### Key Design Decisions

| Decision | Diane's Choice | Implication |
|----------|---------------|-------------|
| **Orchestration** | Programmatic phase sequencing in Go | Predictable, testable, but rigid |
| **Agent spawning** | ACP protocol (HTTP REST to external processes) | Real process isolation, any CLI agent |
| **State management** | Emergent knowledge graph | Persistent, queryable, survives restarts |
| **Context passing** | Orchestrator builds prompts from Emergent data | Explicit, but verbose |
| **Discussion** | Structured proposal/feedback rounds | Controlled, but artificial |
| **Retry logic** | Phase result → GoToPhase with context injection | Clean feedback loops |
| **Concurrency** | Go goroutines + sync.WaitGroup | Native, efficient |
| **Trigger modes** | User, cron, webhook, agent-initiated | Much broader than OpenCode |

---

## Part 3: Head-to-Head Comparison

### 3.1 Orchestration Philosophy

```
OpenCode:  LLM decides    → "I think I should research first, then implement"
Diane:     Code decides   → phases.Next() // always Research → Design → Implement → Test → Review
```

| Dimension | OpenCode (LLM-driven) | Diane (Programmatic) |
|-----------|----------------------|---------------------|
| **Flexibility** | High — model adapts to task complexity | Low — fixed phase order |
| **Predictability** | Low — model may skip steps or loop | High — deterministic phase progression |
| **Debuggability** | Hard — "why did it call 3 subagents?" | Easy — logs show phase transitions |
| **Cost control** | Harder — model decides how many agents | Easier — fixed agent count per phase |
| **Correctness** | Model may forget to test or review | Always tests and reviews (if configured) |
| **Simple tasks** | Efficient — model skips unnecessary phases | Wasteful — runs all phases regardless |
| **Complex tasks** | May not be thorough enough | Guaranteed coverage |

**Verdict**: Neither is strictly better. LLM-driven is better for variable-complexity tasks. Programmatic is better for tasks where you want guaranteed process coverage (CI/CD-like workflows).

### 3.2 Context Passing

| Aspect | OpenCode | Diane |
|--------|----------|-------|
| **Between phases** | Sessions plugin `message` mode: shared conversation | Orchestrator queries Emergent, builds new prompt |
| **Parent → child** | Only the prompt string | Full context from Emergent graph |
| **Child → parent** | Last text part of final message | Parsed structured output stored in Emergent |
| **Persistence** | SQLite messages table (conversation log) | Emergent knowledge graph (structured entities) |
| **Cross-restart** | Messages survive, but no structured extraction | Full entity graph survives with relationships |

**Key difference**: OpenCode passes **unstructured text** between agents. Diane passes **structured entities** via Emergent. This means:

- OpenCode agents can communicate nuance and reasoning naturally in prose
- Diane agents get precise, queryable data but lose conversational nuance
- OpenCode's approach breaks down when context exceeds token limits
- Diane's approach scales to arbitrarily complex tasks (query only what's needed)

### 3.3 Agent Communication

| Pattern | OpenCode | Diane |
|---------|----------|-------|
| **Request/Response** | Task tool (parent → child → parent) | ACP RunSync (orchestrator → agent → orchestrator) |
| **Turn-based collaboration** | Sessions plugin `message` mode | Discussion loop with proposal/feedback |
| **Broadcast** | Not supported | NATS message bus (proposed) |
| **Parallel exploration** | Sessions plugin `fork` mode | Go goroutines + WaitGroup |
| **Agent-to-agent direct** | Only via parent relay | Direct via NATS queues |
| **Discussion/consensus** | Not formalized — agents just talk | Structured rounds with APPROVED/SUGGEST_CHANGES |

**Key difference**: OpenCode's agent communication is **conversational** — agents talk to each other like humans in a chat. Diane's is **protocol-driven** — agents exchange structured messages with explicit status codes.

OpenCode's approach is more natural and allows for emergent collaboration patterns. Diane's is more reliable and auditable but less flexible.

### 3.4 State Management

| Aspect | OpenCode | Diane |
|--------|----------|-------|
| **Primary store** | SQLite messages table | Emergent knowledge graph |
| **Data model** | Flat message log per session | Rich entity-relationship graph |
| **Queryability** | List messages by session | Graph queries (find all tasks with failed tests) |
| **Cross-task knowledge** | None — each session is isolated | Full — entities persist across tasks |
| **Learning/patterns** | No built-in mechanism | Learning Agent + pattern recognition (proposed) |
| **Auditability** | Read conversation log | Query decision history with relationships |

**Diane has a significant advantage here.** The Emergent graph means:
- "Why did we implement dark mode this way?" → query the decision chain
- "What tests have failed most often?" → aggregate TestResult entities
- "Which agents collaborate best?" → query COLLABORATED_WITH relationships
- Research from one task informs future tasks

OpenCode has no cross-session knowledge. Each task starts from scratch.

### 3.5 Process Model

| Aspect | OpenCode | Diane |
|--------|----------|-------|
| **Agent runtime** | Same process (in-process function call) | Separate process (ACP over HTTP/stdio) |
| **Isolation** | Session-level (shared memory) | Process-level (full isolation) |
| **Agent diversity** | Same LLM with different system prompts | Different CLI tools (OpenCode, Claude Code, Gemini CLI) |
| **Crash impact** | Child crash could affect parent | Agent crash is isolated |
| **Resource usage** | Lightweight (one process) | Heavier (multiple processes) |
| **Agent availability** | Always available (same process) | Must be installed + spawnable |

**Diane has stronger isolation** but at a higher resource cost. The ability to use genuinely different agents (not just different prompts for the same LLM) is a real advantage — a Gemini reviewer may catch issues a Claude implementer misses.

### 3.6 Trigger & Lifecycle

| Aspect | OpenCode | Diane |
|--------|----------|-------|
| **Trigger** | User types in TUI | User, cron, webhook, agent-initiated |
| **Always-running** | No — agents exist only during sessions | Yes — agents run as persistent goroutines |
| **Background tasks** | Not supported | Cron + event triggers |
| **Proactive behavior** | Not supported | Agent alerts → automatic task creation |
| **Lifecycle** | Session start → finish | Continuous with health monitoring |

**This is the biggest architectural gap.** OpenCode is a human-in-the-loop tool — you sit at a terminal and interact. Diane aims to be an autonomous system that works while you sleep. These are fundamentally different use cases, and Diane's trigger system is essential for its vision.

---

## Part 4: What Diane Should Learn from OpenCode

### 4.1 Adopt: LLM-Driven Flexibility Within Phases

**Problem with Diane's current design**: Fixed 5-phase sequence is wasteful for simple tasks. "Fix this typo" doesn't need Research → Design → Implement → Test → Review.

**OpenCode's lesson**: Let the LLM decide what's needed.

**Recommendation**: Hybrid approach — the Orchestrator uses an LLM call to determine which phases are needed:

```go
func (o *Orchestrator) planPhases(task *DevTask) []Phase {
    // LLM analyzes the task and selects relevant phases
    plan := o.llm.Call(`
        Given this task: "%s"
        Which phases are needed? Choose from: research, design, implement, test, review
        For simple fixes, you may skip research and design.
        Always include test for code changes.
        Return a JSON array of phase names.
    `, task.Description)
    
    return o.parsePhasePlan(plan) // e.g., ["implement", "test"] for a typo fix
}
```

This keeps the programmatic orchestrator (predictable, testable) while gaining LLM flexibility in phase selection.

### 4.2 Adopt: Lightweight In-Process Subagents for Simple Tasks

**Problem**: Spawning an ACP agent (external process) for a 10-second research question is heavy.

**OpenCode's lesson**: The Task tool runs subagents as in-process function calls — extremely lightweight.

**Recommendation**: Diane should support both modes:
- **Lightweight mode**: In-process LLM calls for simple subtasks (research queries, feedback evaluation, phase planning)
- **Heavy mode**: ACP external agents for real implementation work (code writing, testing)

```go
type AgentMode int
const (
    AgentModeLight AgentMode = iota  // Direct LLM API call, no external process
    AgentModeHeavy                    // Full ACP agent with process isolation
)
```

### 4.3 Adopt: Session Resumption

**OpenCode's lesson**: The `task_id` parameter allows resuming a previous subagent session, preserving all context.

**Recommendation**: Diane should support resumable agent interactions. If an implementation agent is interrupted (user cancellation, crash), the orchestrator should be able to resume it with its previous context rather than starting from scratch.

This maps naturally to Emergent: store the agent's session state as an entity, and restore it on resume.

### 4.4 Adopt: The Agent Relay Pattern (from Sessions Plugin)

**OpenCode's lesson**: The `message` mode's deferred delivery pattern (store pending message → wait for idle → deliver) is elegant and prevents deadlocks.

**Recommendation**: Diane's Discussion Loop should use a similar pattern rather than blocking synchronous calls. Instead of:

```go
// Current design: blocking synchronous exchange
proposal := runAgent("designer", designPrompt)
feedback := runAgent("researcher", feedbackPrompt)  // blocks until done
```

Consider:

```go
// Relay pattern: non-blocking with state machine
o.postMessage("designer", designPrompt)
// Designer completes → triggers onAgentIdle
// onAgentIdle delivers proposal to researcher
// Researcher completes → triggers onAgentIdle
// onAgentIdle delivers feedback back to designer or marks consensus
```

This is more complex but handles agent failures more gracefully and enables true asynchronous collaboration.

### 4.5 Learn From (But Don't Adopt): Shared Conversation History

**OpenCode's `message` mode** puts both agents in the same conversation. This is powerful for natural collaboration but has severe limitations:

- Token usage grows linearly with conversation length
- No structured data extraction — everything is prose
- Can't selectively share context (all or nothing)

**Diane's Emergent approach is better for its use case.** Structured entities with selective querying scales better for complex, multi-phase tasks. Keep this.

---

## Part 5: What OpenCode Could Learn from Diane

(Included for completeness — these are areas where Diane's design is ahead)

1. **Persistent knowledge graph**: OpenCode has no cross-session memory. Every task starts cold.
2. **Trigger diversity**: OpenCode is human-initiated only. No cron, no webhooks, no proactive behavior.
3. **Multi-agent diversity**: OpenCode uses one LLM with different prompts. Diane uses genuinely different tools.
4. **Structured discussions**: OpenCode has no formalized consensus mechanism.
5. **Retry with context**: OpenCode has no built-in "test failed → retry implementation with failure context" loop.
6. **Audit trail**: OpenCode's conversation logs are unstructured. Diane's Emergent graph is queryable.
7. **Resource management**: OpenCode has no budget tracking or resource pooling across agents.

---

## Part 6: Recommended Hybrid Architecture for Diane

Based on this analysis, here's the recommended evolution of Diane's multi-agent design:

### Layer 1: Smart Phase Planner (LLM-Driven)

Replace the fixed phase sequence with an LLM-driven planner that selects phases based on task complexity:

```
User Request
    │
    ▼
┌────────────────────────────────┐
│  Phase Planner (LLM call)      │
│                                │
│  Input: task description       │
│  Output: ordered phase list    │
│                                │
│  "Fix typo" → [implement, test]│
│  "Add feature" → [research,   │
│    design, implement, test,    │
│    review]                     │
│  "Investigate bug" → [research]│
└────────────────────────────────┘
    │
    ▼
  Orchestrator Loop (unchanged)
```

### Layer 2: Dual Agent Modes

```
┌─────────────────────────────────────────────────┐
│              Agent Dispatcher                    │
│                                                  │
│  Task complexity < threshold?                    │
│    YES → LightAgent (in-process LLM call)        │
│    NO  → HeavyAgent (ACP external process)       │
│                                                  │
│  Examples:                                       │
│    "What SwiftUI pattern for X?" → Light (2s)    │
│    "Implement dark mode toggle"  → Heavy (3min)  │
│    "Is this feedback approving?" → Light (1s)    │
│    "Write and run tests"         → Heavy (2min)  │
└─────────────────────────────────────────────────┘
```

### Layer 3: Async Discussion with Relay Pattern

Replace blocking synchronous discussion with an event-driven relay:

```
┌──────────────────────────────────────────────────┐
│          Discussion Coordinator                   │
│                                                   │
│  State Machine:                                   │
│    PROPOSE → agent produces proposal              │
│    REVIEW  → reviewer evaluates (async)           │
│    REVISE  → agent refines (if needed)            │
│    DECIDE  → consensus reached or max rounds      │
│                                                   │
│  Each transition fires via Go channels:           │
│    proposalChan <- proposal                       │
│    <-feedbackChan // non-blocking with timeout    │
│                                                   │
│  Emergent updated at each transition              │
└──────────────────────────────────────────────────┘
```

### Layer 4: Emergent as System of Record (Keep As-Is)

The Emergent knowledge graph remains the backbone. This is Diane's strongest differentiator over OpenCode. All agent state, decisions, artifacts, and discussions are persisted as queryable entities.

### Layer 5: Multi-Trigger Orchestration (Keep As-Is)

The 4 trigger modes (user, cron, webhook, agent-initiated) are essential for Diane's always-on vision and don't exist in OpenCode.

---

## Part 7: Summary Comparison Matrix

| Dimension | OpenCode | Diane (Current Design) | Diane (Recommended) |
|-----------|----------|----------------------|---------------------|
| **Orchestration** | LLM-driven | Fixed phases | LLM-selected phases, programmatic execution |
| **Agent spawning** | In-process (lightweight) | External process (ACP) | Dual mode (light + heavy) |
| **Context passing** | Unstructured text | Emergent entities | Emergent entities (keep) |
| **Agent communication** | Conversational | Protocol-driven | Protocol-driven with relay pattern |
| **State persistence** | SQLite messages | Emergent graph | Emergent graph (keep) |
| **Cross-task knowledge** | None | Full graph | Full graph (keep) |
| **Trigger modes** | User only | User, cron, webhook, agent | All four (keep) |
| **Discussion** | Ad-hoc in conversation | Structured rounds | Async relay with state machine |
| **Retry logic** | None (model decides) | Phase retry with context | Phase retry with context (keep) |
| **Agent diversity** | One LLM, different prompts | Multiple CLI tools | Multiple CLI tools (keep) |
| **Cost tracking** | None | Per-phase budgets | Per-phase budgets (keep) |
| **Fault tolerance** | Doom loop detection | Supervision tree | Supervision tree (keep) |

---

## Part 8: Implementation Priority

Based on the analysis, here's the recommended order for implementing the hybrid architecture:

### Phase 1: Smart Phase Planner (1-2 days)
- Add LLM call before orchestrator loop to select relevant phases
- Simple: just a JSON-returning prompt that picks from available phases
- Huge ROI: prevents wasting 5 agent calls on a typo fix

### Phase 2: Dual Agent Modes (2-3 days)
- Add `LightAgent` that makes direct LLM API calls (no ACP overhead)
- Use for: phase planning, feedback evaluation, simple research queries
- Keep `HeavyAgent` (ACP) for: code implementation, testing, review

### Phase 3: Async Discussion Relay (3-5 days)
- Replace blocking discussion loop with channel-based state machine
- Add timeout handling (agent doesn't respond in 60s → auto-approve or escalate)
- Better error recovery (agent crashes mid-discussion → restart from last state)

### Phase 4: Session Resumption (2-3 days)
- Store agent session state in Emergent before each phase
- On retry/resume, restore agent context from Emergent
- Prevents re-doing work after crashes or interruptions

### Phase 5: Everything Else (existing design)
- NATS message bus, supervision tree, health monitoring, resource management
- These are solid as designed and don't need changes from the OpenCode comparison

---

## Conclusion

OpenCode and Diane solve different problems at different scales. OpenCode is an interactive coding assistant where one human directs one AI through tool calls. Diane aims to be an autonomous agent ecosystem that coordinates multiple AI tools to accomplish goals independently.

The three most valuable lessons from OpenCode for Diane are:

1. **LLM flexibility in phase selection** — don't run 5 phases for every task
2. **Lightweight in-process agents** — not everything needs a full ACP process
3. **Relay pattern for agent handoffs** — non-blocking, deadlock-free collaboration

The three areas where Diane's design is already superior:

1. **Emergent knowledge graph** — persistent, structured, queryable state
2. **Multi-trigger orchestration** — cron, webhooks, and proactive behavior
3. **Agent diversity** — genuinely different tools, not just different prompts

The recommended hybrid architecture combines OpenCode's LLM-driven flexibility with Diane's programmatic reliability and rich state management.
