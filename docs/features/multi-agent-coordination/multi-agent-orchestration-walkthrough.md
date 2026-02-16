# Multi-Agent Orchestration: Walkthrough

_Design Date: February 14, 2026_
_Updated: February 15, 2026 — Aligned with emergent reality and product layer design_

## The Scenario

**User triggers a multi-agent workflow**: "Break down and implement a document tagging system for the knowledge base"

This triggers a coordination workflow involving **5 product-defined agents** working together through orchestrated loops, sharing state via the knowledge graph, and "discussing" design decisions before implementing them.

---

## The Agents Involved

All agents are defined by products installed on the project and executed via ADK-Go pipelines as goroutines.

```
┌──────────────────────────────────────────────────────────────────┐
│                    TASK DISPATCHER                                │
│  Role: Walks task DAG, coordinates agents, manages lifecycle     │
│  Runs: Go polling loop inside emergent server                    │
│  Uses: Code rules + LLM fallback for agent selection             │
└──────────────┬───────────────────────────────────────────────────┘
               │ dispatches via ADK-Go
    ┌──────────┼──────────┬──────────────┬──────────────┐
    ▼          ▼          ▼              ▼              ▼
┌────────┐ ┌────────┐ ┌──────────┐ ┌──────────┐ ┌──────────┐
│RESEARCH│ │  SPEC  │ │  TASK    │ │IMPLEMENT │ │ REVIEWER │
│ AGENT  │ │ WRITER │ │ PLANNER  │ │  AGENT   │ │  AGENT   │
│        │ │        │ │          │ │          │ │          │
│Gemini  │ │Gemini  │ │Gemini    │ │Gemini    │ │Gemini    │
│Flash   │ │Pro     │ │Flash     │ │Pro       │ │Flash     │
└────────┘ └────────┘ └──────────┘ └──────────┘ └──────────┘
  (from emergent.research)  (from emergent.code)
```

**Key insight**: Each agent is an `AgentDefinition` from a product's manifest.json, stored in `kb.agent_definitions`. The `AgentExecutor` builds an ADK-Go pipeline for each one — creating a Gemini model via Vertex AI, wiring tools from the project's **ToolPool** (filtered per agent via `ResolveTools()`), and running it as a goroutine.

> **Note**: This walkthrough covers the **TaskDispatcher-driven DAG pattern** — structured workflows where tasks have explicit dependency relationships. For ad-hoc coordination where an agent dynamically discovers and spawns sub-agents at runtime, see [research-agent-scenario-walkthrough.md](./research-agent-scenario-walkthrough.md).

---

## How It Actually Works

### The Core Loop Architecture

There are **three types of loops** operating at different levels:

```
LEVEL 1: DISPATCHER LOOP (outer)
 │  Walks the task DAG
 │  Dispatches agents to available (unblocked) tasks
 │  Checks if all tasks complete
 │
 ├─► LEVEL 2: PHASE LOOP (middle)
 │    Manages one task's execution lifecycle
 │    Handles retries with context injection
 │    Runs discussion loops when collaboration needed
 │
 └──► LEVEL 3: AGENT EXECUTION (inner)
       Individual agent running via ADK-Go pipeline
       LLM calls via Vertex AI, tool calls via MCP
       Results written to graph, tracked via AgentRun
```

---

## Step-by-Step Walkthrough

### PHASE 0: Task DAG Creation (TaskDispatcher, ~5 seconds)

The task planner agent receives the user's request and creates a task DAG in the knowledge graph. This is a single ADK-Go agent call — fast and cheap.

```
TASK DAG CREATED IN GRAPH:
┌─────────────────────────────────────────────────────────┐
│ SpecTask: "Document tagging system"                      │
│                                                         │
│ Task DAG:                                               │
│   Task 1: RESEARCH   → What tagging patterns exist?     │
│   Task 2: DESIGN     → Schema and API design            │
│   Task 3: IMPLEMENT  → Build the feature                │
│   Task 4: TEST       → Verify correctness               │
│   Task 5: REVIEW     → Quality check                    │
│                                                         │
│ Dependencies (blocks relationships):                    │
│   RESEARCH ──blocks──► DESIGN                           │
│   DESIGN ──blocks──► IMPLEMENT                          │
│   IMPLEMENT ──blocks──► TEST                            │
│   TEST ──blocks──► REVIEW                               │
│                              ▲                          │
│                              │                          │
│                    (retry if TEST fails)                 │
└─────────────────────────────────────────────────────────┘
```

**Graph State Created:**

```
TaskDispatcher creates SpecTask objects in the knowledge graph:

  SpecTask("research-tagging")     status: pending
    │ blocks
  SpecTask("design-tagging")       status: pending
    │ blocks
  SpecTask("implement-tagging")    status: pending
    │ blocks
  SpecTask("test-tagging")         status: pending
    │ blocks
  SpecTask("review-tagging")       status: pending
```

---

### TASK 1: Research (~15 seconds)

**TaskDispatcher finds "research-tagging" is unblocked and dispatches the research agent:**

```go
// Inside emergent's TaskDispatcher — uses AgentExecutor with ADK-Go
func (td *TaskDispatcher) executeTask(ctx context.Context, task GraphObject, agentDef AgentDefinition) {
    // 1. Mark task in_progress
    td.graphService.UpdateObject(ctx, task.ID, map[string]any{
        "status":         "in_progress",
        "assigned_agent": agentDef.Name,
    })

    // 2. Build context from task description + predecessors
    taskContext := td.buildTaskContext(task)

    // 3. Execute via AgentExecutor (builds ADK-Go pipeline internally)
    run, err := td.executor.Execute(ctx, agentDef, taskContext)
    // AgentExecutor internally:
    //   - Creates Gemini model via pkg/adk/model.go
    //   - Resolves tools via ToolPool.ResolveTools(agentDef) — filtered by agent's whitelist
    //   - Runs ADK pipeline as goroutine
    //   - Tracks via AgentRun (status, duration, summary)

    // 4. Handle result
    if err != nil {
        td.handleFailure(ctx, task, run, err)
    } else {
        td.graphService.UpdateObject(ctx, task.ID, map[string]any{
            "status": "completed",
        })
    }
}
```

**Research Agent execution** (ADK-Go pipeline with graph tools):

```
Agent: research-assistant (from emergent.research product)
Model: gemini-2.5-flash via Vertex AI
Tools: search_hybrid, graph_traverse, entities_get  (from ToolPool, filtered by agent's tools whitelist)

Prompt: "Research existing tagging patterns in the knowledge base.
         Find: current entity schemas, existing tag-like fields,
         similar features in the codebase."

The agent:
  1. Calls search_hybrid to find existing schemas
  2. Calls graph_traverse to understand entity relationships
  3. Calls create_entity to store a ResearchReport object
  4. Returns structured findings
```

**Research results stored in graph:**

```json
{
  "object_type": "ResearchReport",
  "properties": {
    "findings": {
      "existing_schemas": ["Document", "Note", "Bookmark"],
      "tag_patterns": "No existing tag entity. Documents have flat 'tags' string array.",
      "graph_patterns": "Objects use relationships for categorization"
    },
    "recommended_approach": "Create Tag entity type with TAGGED_WITH relationship",
    "relevant_code": [
      "domain/graph/service.go — object CRUD",
      "domain/templatepacks/ — schema definitions"
    ],
    "risks": [
      "Need migration for existing flat tags",
      "Search index needs to include tag relationships"
    ]
  }
}
```

---

### TASK 2: Design (~30 seconds, with Discussion Loop)

The design task is unblocked after research completes. The TaskDispatcher dispatches the spec-writer agent.

**The "Discussion" Pattern**: Before finalizing the design, the spec-writer and research-assistant have an exchange via the discussion mechanism:

```
DISCUSSION LOOP (2 rounds max):
┌─────────────────────────────────────────────────────────────┐
│                                                             │
│  Round 1: Spec-writer proposes schema                       │
│    ┌──────────┐                ┌──────────────┐             │
│    │  Spec    │──proposal──►   │  Research    │             │
│    │  Writer  │◄──feedback──   │  Assistant   │             │
│    └──────────┘                └──────────────┘             │
│    Spec-writer: "Tag entity with name, color, description.  │
│                  TAGGED_WITH relationship from any object."  │
│    Research:    "Suggest adding tag_type field (user vs      │
│                  auto-generated). Also need hierarchical     │
│                  tags via PARENT_TAG relationship."          │
│                                                             │
│  Round 2: Spec-writer refines based on feedback              │
│    Spec-writer: "Updated. Added tag_type enum and           │
│                  PARENT_TAG relationship for hierarchy.      │
│                  Added auto-tag trigger agent definition."   │
│    Research:    "APPROVED. Looks complete."                  │
│                                                             │
│  Consensus reached → design stored in graph                  │
└─────────────────────────────────────────────────────────────┘
```

**How this discussion works in code:**

```go
func (td *TaskDispatcher) runWithDiscussion(ctx context.Context, task GraphObject, agents []AgentDefinition) {
    // 1. Load predecessor outputs from graph
    predecessors := td.graphService.GetRelated(ctx, task.ID, "blocks", graph.Incoming)
    researchReport := td.getLatestOutput(predecessors)

    // 2. Get initial proposal from primary agent (spec-writer)
    specWriter := agents[0]
    proposal := td.executor.Execute(ctx, specWriter, fmt.Sprintf(
        "Based on this research: %s\nCreate a schema design for document tagging.",
        researchReport,
    ))

    // 3. Create Discussion object in graph
    discussion := td.graphService.CreateObject(ctx, "Discussion", map[string]any{
        "topic":               "Document tagging schema design",
        "status":              "active",
        "discussion_type":     "consensus",
        "participating_agents": []string{specWriter.Name, agents[1].Name},
    })
    td.graphService.CreateRelationship(ctx, task.ID, discussion.ID, "spawned_discussion")

    // 4. Store initial proposal as discussion entry
    proposalEntry := td.graphService.CreateObject(ctx, "DiscussionEntry", map[string]any{
        "agent_name": specWriter.Name,
        "content":    proposal.Summary,
        "entry_type": "proposal",
    })
    td.graphService.CreateRelationship(ctx, discussion.ID, proposalEntry.ID, "has_entry")

    // 5. DISCUSSION LOOP — max 3 rounds
    reviewer := agents[1] // research-assistant
    for round := 1; round <= 3; round++ {
        // Get feedback from reviewing agent
        feedback := td.executor.Execute(ctx, reviewer, fmt.Sprintf(
            "Review this design proposal for technical feasibility:\n\nPROPOSAL: %s\nRESEARCH: %s\n\n"+
            "Respond with either:\n- 'APPROVED' + brief confirmation\n- 'SUGGEST_CHANGES' + specific feedback",
            proposal.Summary, researchReport,
        ))

        // Store feedback in graph
        feedbackEntry := td.graphService.CreateObject(ctx, "DiscussionEntry", map[string]any{
            "agent_name": reviewer.Name,
            "content":    feedback.Summary,
            "entry_type": "argument",
        })
        td.graphService.CreateRelationship(ctx, discussion.ID, feedbackEntry.ID, "has_entry")

        // Check if approved
        if strings.Contains(feedback.Summary, "APPROVED") {
            td.graphService.UpdateObject(ctx, discussion.ID, map[string]any{
                "status":          "resolved",
                "consensus_level": 1.0,
            })
            break
        }

        // Refine proposal based on feedback
        proposal = td.executor.Execute(ctx, specWriter, fmt.Sprintf(
            "Refine your design based on this feedback:\nORIGINAL: %s\nFEEDBACK: %s",
            proposal.Summary, feedback.Summary,
        ))
    }
}
```

**Design stored in graph:**

```
SpecTask("design-tagging")  status: completed
  │
  ├── spawned_discussion ──► Discussion("Document tagging schema design")
  │                           ├── has_entry ──► DiscussionEntry(spec-writer, "proposal", round 1)
  │                           ├── has_entry ──► DiscussionEntry(research-assistant, "SUGGEST_CHANGES")
  │                           ├── has_entry ──► DiscussionEntry(spec-writer, "refined proposal")
  │                           └── has_entry ──► DiscussionEntry(research-assistant, "APPROVED")
  │
  └── has_session ──► Session(spec-writer, completed)
                       ├── has_message ──► SessionMessage(system, prompt)
                       ├── has_message ──► SessionMessage(assistant, proposal)
                       └── has_message ──► SessionMessage(tool, create_entity result)
```

---

### TASK 3: Implementation (~30-60 seconds)

The implement agent gets everything from the graph — research findings, approved design, and the full discussion history.

```go
func (td *TaskDispatcher) executeImplementTask(ctx context.Context, task GraphObject) {
    // 1. Gather ALL context from graph via predecessor chain
    predecessors := td.graphService.GetRelated(ctx, task.ID, "blocks", graph.Incoming)
    design := td.getApprovedDesign(predecessors)
    research := td.getResearchReport(predecessors)

    // 2. Check for retry context (from previous failed attempts)
    retryContext := ""
    if task.Properties["retry_count"].(int) > 0 {
        retryContext = fmt.Sprintf(
            "\n\nPREVIOUS ATTEMPT FAILED:\n%s\nFix the specific issues listed above.",
            task.Properties["failure_context"],
        )
    }

    // 3. Build implementation prompt
    prompt := fmt.Sprintf(`
        TASK: Implement document tagging system

        APPROVED DESIGN:
        %s

        RESEARCH CONTEXT:
        %s

        IMPLEMENTATION INSTRUCTIONS:
        1. Create Tag template pack schema (object type + relationships)
        2. Create the tag management graph tools
        3. Create auto-tagging agent definition for the product manifest
        4. Write the implementation summary as graph objects
        %s

        Write results to the knowledge graph using the provided tools.
    `, design, research, retryContext)

    // 4. Execute via ADK-Go pipeline
    agentDef := td.selector.SelectAgent(ctx, task) // picks implement-agent
    run, err := td.executor.Execute(ctx, agentDef, prompt)
    // The executor:
    //   - Creates ADK agent with Gemini Pro model
    //   - Resolves tools via ToolPool.ResolveTools(agentDef) — filtered by agent's whitelist
    //   - Runs pipeline, agent uses tools to write implementation artifacts to graph
    //   - Tracks duration, token usage, summary via AgentRun

    // 5. Store artifacts
    if err == nil {
        td.graphService.UpdateObject(ctx, task.ID, map[string]any{
            "status": "completed",
            "metrics": map[string]any{
                "duration_seconds": run.Duration.Seconds(),
                "llm_calls":       run.LLMCallCount,
                "tokens_used":     run.TokensUsed,
            },
        })
    }
}
```

**Implementation artifacts stored in graph:**

```
SpecTask("implement-tagging")  status: completed
  │
  ├── has_session ──► Session(implement-agent, completed)
  │                    ├── has_message ──► SessionMessage(system, implementation prompt)
  │                    ├── has_message ──► SessionMessage(assistant, "Creating Tag schema...")
  │                    ├── has_message ──► SessionMessage(tool, create_entity result)
  │                    └── has_message ──► SessionMessage(assistant, "Implementation complete")
  │
  └── (agent created these objects via graph tools during execution):
      ├── Tag("user-created") template pack schema
      ├── Tag("auto-generated") template pack schema
      └── Relationship types: TAGGED_WITH, PARENT_TAG
```

---

### TASK 4: Testing (~15-30 seconds)

The test agent runs independently. It gets the implementation outputs from the graph and verifies correctness.

```go
func (td *TaskDispatcher) executeTestTask(ctx context.Context, task GraphObject) {
    // 1. Get implementation artifacts from graph
    predecessors := td.graphService.GetRelated(ctx, task.ID, "blocks", graph.Incoming)
    implementation := td.getImplementationArtifacts(predecessors)
    design := td.getApprovedDesign(predecessors)

    // 2. Build verification prompt
    prompt := fmt.Sprintf(`
        TESTING TASK: Verify the document tagging implementation

        EXPECTED (from approved design):
        %s

        IMPLEMENTED:
        %s

        VERIFICATION STEPS:
        1. Check that Tag entity schema matches the approved design
        2. Verify TAGGED_WITH relationship type exists and is correct
        3. Verify PARENT_TAG relationship for hierarchical tags
        4. Check that tag_type enum includes user and auto-generated
        5. Verify auto-tagging agent definition is complete
        6. Look for edge cases: duplicate tags, empty names, circular hierarchies

        Report:
        - PASS or FAIL for each verification step
        - If FAIL: specific issue and suggested fix
        - Overall verdict: PASS, FAIL, or NEEDS_CHANGES
    `, design, implementation)

    // 3. Execute test agent
    agentDef := td.selector.SelectAgent(ctx, task) // picks reviewer-agent
    run, err := td.executor.Execute(ctx, agentDef, prompt)

    // 4. Parse verdict from agent output
    verdict := td.parseVerdict(run.Summary)

    // 5. Store test results
    td.graphService.CreateObject(ctx, "TestResult", map[string]any{
        "verdict":   verdict.Overall, // "PASS", "FAIL", "NEEDS_CHANGES"
        "steps":     verdict.Steps,
        "issues":    verdict.Issues,
    })

    // 6. RETRY LOOP — if tests fail, task goes back to pending with context
    if verdict.Overall == "FAIL" || verdict.Overall == "NEEDS_CHANGES" {
        td.handleFailure(ctx, task, run, fmt.Errorf("test verdict: %s — %s",
            verdict.Overall, strings.Join(verdict.Issues, "; ")))
        // handleFailure sets:
        //   status: "pending" (back in the queue)
        //   retry_count: incremented
        //   failure_context: "Previous test found: missing PARENT_TAG validation..."
        //   assigned_agent: nil (allow re-selection)
        //
        // On retry, the implement agent sees the failure_context in its prompt
        // and knows exactly what to fix.
    }
}
```

---

### TASK 5: Review (~15 seconds)

A review agent checks quality and consistency.

```go
func (td *TaskDispatcher) executeReviewTask(ctx context.Context, task GraphObject) {
    // Gather full history from graph
    allPredecessors := td.graphService.TraverseBFS(ctx, task.ID, "blocks", graph.Incoming, maxDepth)

    prompt := fmt.Sprintf(`
        CODE REVIEW: Document tagging implementation

        Review the full implementation history for:
        1. Schema quality and consistency with existing patterns
        2. Relationship design completeness
        3. Edge case handling
        4. Design decision traceability (can we understand WHY?)

        Full context:
        %s

        Provide:
        - Overall assessment: APPROVE, REQUEST_CHANGES, or COMMENT
        - Specific issues (if any)
        - Suggestions for improvement
    `, formatTraversalResults(allPredecessors))

    agentDef := td.selector.SelectAgent(ctx, task) // picks reviewer-agent
    run, err := td.executor.Execute(ctx, agentDef, prompt)

    review := td.parseReviewResult(run.Summary)

    td.graphService.CreateObject(ctx, "CodeReview", map[string]any{
        "verdict":     review.Verdict,
        "comments":    review.Comments,
        "suggestions": review.Suggestions,
    })

    if review.Verdict == "REQUEST_CHANGES" {
        // Same retry pattern — goes back to implement with context
        td.handleFailure(ctx, task, run, fmt.Errorf("review requested changes: %s",
            strings.Join(review.RequiredChanges, "; ")))
    }
}
```

---

## The TaskDispatcher Main Loop

This is the **top-level loop** that ties everything together. It walks the DAG, dispatches agents, and handles the lifecycle:

```go
func (td *TaskDispatcher) Run(ctx context.Context, projectID string) error {
    maxRetries := 3

    for {
        // ═══════════════════════════════════════════════════════
        // QUERY AVAILABLE TASKS
        // ═══════════════════════════════════════════════════════
        // Find SpecTask objects where:
        //   - status = "pending"
        //   - no incoming "blocks" from incomplete tasks
        //   - retry_count < max_retries
        available := td.queryAvailableTasks(ctx, projectID)

        if len(available) == 0 {
            if td.allTasksComplete(ctx, projectID) {
                return nil // All done!
            }
            if td.hasFailedTasks(ctx, projectID) {
                return td.escalateFailures(ctx, projectID)
            }
            time.Sleep(pollInterval) // Wait for running tasks to complete
            continue
        }

        // ═══════════════════════════════════════════════════════
        // DISPATCH AVAILABLE TASKS (parallel where possible)
        // ═══════════════════════════════════════════════════════
        slots := td.maxConcurrent - len(td.activeRuns)
        for _, task := range available[:min(len(available), slots)] {
            // Select agent using hybrid strategy
            agentDef := td.selector.SelectAgent(ctx, task)

            // Check if task needs discussion (requires_collaboration = true)
            if task.Properties["requires_collaboration"].(bool) {
                collaborators := td.selectCollaborators(ctx, task)
                go td.runWithDiscussion(ctx, task, append([]AgentDefinition{agentDef}, collaborators...))
            } else {
                go td.executeTask(ctx, task, agentDef)
            }

            td.mu.Lock()
            td.activeRuns[task.ID] = &ActiveRun{Task: task, Agent: agentDef, StartedAt: time.Now()}
            td.mu.Unlock()
        }
    }
}
```

---

## The Complete Flow Diagram

```
USER: "Break down and implement a document tagging system"
 │
 ▼
╔═══════════════════════════════════════════════════════════════╗
║                    TASK DISPATCHER LOOP                       ║
║                                                              ║
║  Task Planner agent creates DAG in graph                     ║
║                                                              ║
║  ┌─────────────────────────────────────────────────────────┐ ║
║  │ Task 1: RESEARCH                                        │ ║
║  │                                                         │ ║
║  │  Dispatcher ──ADK-Go──► research-assistant goroutine     │ ║
║  │       │                      │                          │ ║
║  │       │              searches graph, finds patterns      │ ║
║  │       │              writes ResearchReport to graph      │ ║
║  │       │                      │                          │ ║
║  │       ◄──── AgentRun complete ───┘                       │ ║
║  │       │                                                 │ ║
║  │  Status: completed ✅                                     │ ║
║  └─────────────────────────────────────────────────────────┘ ║
║                          │                                   ║
║                          ▼  (task 2 unblocked)               ║
║  ┌─────────────────────────────────────────────────────────┐ ║
║  │ Task 2: DESIGN (with Discussion Loop)                   │ ║
║  │                                                         │ ║
║  │  ┌────────────── DISCUSSION LOOP ──────────────────┐    │ ║
║  │  │                                                 │    │ ║
║  │  │  Round 1:                                       │    │ ║
║  │  │    spec-writer ──proposal──► research-assistant  │    │ ║
║  │  │    spec-writer ◄──feedback── research-assistant  │    │ ║
║  │  │    Verdict: SUGGEST_CHANGES                     │    │ ║
║  │  │                                                 │    │ ║
║  │  │  Round 2:                                       │    │ ║
║  │  │    spec-writer ──refined──► research-assistant   │    │ ║
║  │  │    spec-writer ◄──"APPROVED"── research-assistant│    │ ║
║  │  │    Verdict: APPROVED ✅                          │    │ ║
║  │  │                                                 │    │ ║
║  │  └─────────────────────────────────────────────────┘    │ ║
║  │                                                         │ ║
║  │  Graph: Discussion + DiscussionEntry objects stored      │ ║
║  │                                                         │ ║
║  │  Status: completed ✅                                     │ ║
║  └─────────────────────────────────────────────────────────┘ ║
║                          │                                   ║
║                          ▼  (task 3 unblocked)               ║
║  ┌─────────────────────────────────────────────────────────┐ ║
║  │ Task 3: IMPLEMENT                                       │ ║
║  │                                                         │ ║
║  │  Dispatcher ──ADK-Go──► implement-agent goroutine        │ ║
║  │       │                      │                          │ ║
║  │       │   Agent reads design + research from graph       │ ║
║  │       │   Uses graph tools to create Tag schemas         │ ║
║  │       │   Creates TAGGED_WITH relationship type          │ ║
║  │       │   Writes implementation artifacts to graph       │ ║
║  │       │                      │                          │ ║
║  │       ◄──── AgentRun complete ───┘                       │ ║
║  │                                                         │ ║
║  │  Status: completed ✅                                     │ ║
║  └─────────────────────────────────────────────────────────┘ ║
║                          │                                   ║
║                          ▼  (task 4 unblocked)               ║
║  ┌─────────────────────────────────────────────────────────┐ ║
║  │ Task 4: TEST                              attempt 1     │ ║
║  │                                                         │ ║
║  │  Dispatcher ──ADK-Go──► reviewer-agent goroutine         │ ║
║  │       │                      │                          │ ║
║  │       │              reads implementation from graph     │ ║
║  │       │              compares against approved design    │ ║
║  │       │              checks edge cases                  │ ║
║  │       │                      │                          │ ║
║  │       ◄──── VERDICT: NEEDS_CHANGES ─┘                    │ ║
║  │       │     "Missing PARENT_TAG cycle detection.         │ ║
║  │       │      Hierarchical tags could create infinite     │ ║
║  │       │      loops without validation."                  │ ║
║  │       │                                                 │ ║
║  │  ══════════════════════════════════════                  │ ║
║  │  RETRY: task 3 goes back to pending with context         │ ║
║  │  ══════════════════════════════════════                  │ ║
║  └───────────────────────┬─────────────────────────────────┘ ║
║                          │                                   ║
║              ┌───────────┘  (retry loop)                     ║
║              ▼                                               ║
║  ┌─────────────────────────────────────────────────────────┐ ║
║  │ Task 3: IMPLEMENT (retry attempt 2)                     │ ║
║  │                                                         │ ║
║  │  Failure context injected into prompt:                   │ ║
║  │    "Previous attempt failed: Missing PARENT_TAG cycle   │ ║
║  │     detection. Add validation to prevent infinite       │ ║
║  │     loops in tag hierarchies."                          │ ║
║  │                                                         │ ║
║  │  Implement agent fixes the specific issue               │ ║
║  │                                                         │ ║
║  │  Status: completed ✅                                     │ ║
║  └─────────────────────────────────────────────────────────┘ ║
║                          │                                   ║
║                          ▼  (task 4 re-unblocked)            ║
║  ┌─────────────────────────────────────────────────────────┐ ║
║  │ Task 4: TEST (attempt 2)                                │ ║
║  │                                                         │ ║
║  │  All checks pass including cycle detection              │ ║
║  │  VERDICT: PASS ✅                                        │ ║
║  └─────────────────────────────────────────────────────────┘ ║
║                          │                                   ║
║                          ▼  (task 5 unblocked)               ║
║  ┌─────────────────────────────────────────────────────────┐ ║
║  │ Task 5: REVIEW                                          │ ║
║  │                                                         │ ║
║  │  Reviewer agent checks full history via BFS traversal   │ ║
║  │  VERDICT: APPROVE ✅                                     │ ║
║  │  Suggestion: "Consider adding tag usage analytics      │ ║
║  │   as a follow-up task"                                  │ ║
║  └─────────────────────────────────────────────────────────┘ ║
║                                                              ║
╚══════════════════════════════════════════════════════════════╝
 │
 ▼
ALL TASKS COMPLETE — Full history persisted in knowledge graph
```

---

## Final Graph State

After the workflow completes, all knowledge is persisted and queryable:

```
SpecTask("research-tagging")     status: completed
 └── has_session ──► Session(research-assistant)
      └── (agent created) ResearchReport(findings, approach, risks)

SpecTask("design-tagging")       status: completed
 ├── has_session ──► Session(spec-writer)
 └── spawned_discussion ──► Discussion("tagging schema design")
      ├── has_entry ──► DiscussionEntry(spec-writer, proposal, round 1)
      ├── has_entry ──► DiscussionEntry(research-assistant, SUGGEST_CHANGES)
      ├── has_entry ──► DiscussionEntry(spec-writer, refined proposal)
      └── has_entry ──► DiscussionEntry(research-assistant, APPROVED)

SpecTask("implement-tagging")    status: completed
 ├── has_session ──► Session(implement-agent, attempt 1)
 ├── has_session ──► Session(implement-agent, attempt 2)  ← retry
 └── (agent created) Tag schemas, TAGGED_WITH, PARENT_TAG types

SpecTask("test-tagging")         status: completed
 ├── has_session ──► Session(reviewer-agent, attempt 1)
 │    └── TestResult(NEEDS_CHANGES, "missing cycle detection")
 └── has_session ──► Session(reviewer-agent, attempt 2)
      └── TestResult(PASS)

SpecTask("review-tagging")       status: completed
 └── has_session ──► Session(reviewer-agent)
      └── CodeReview(APPROVE, suggestion: "add usage analytics")
```

This graph is **queryable later**. Next time someone asks "why did we design tagging this way?", the full decision history — including the spec-writer/researcher discussion, the test failure and fix, and the review suggestions — is all there via BFS traversal.

---

## Triggering Modes

The same TaskDispatcher can be triggered in multiple ways:

### 1. Manual (Admin UI / API)

```
User ──Admin UI──► POST /api/coordination/dispatch
                   body: { description: "...", project_id: "..." }
```

### 2. Scheduled (Cron via existing scheduler)

```go
// In product manifest:
// "trigger": "0 9 * * MON"
//
// The scheduler creates a job that invokes TaskDispatcher
// with a predefined task template.
```

### 3. Event-Driven (Agent trigger)

```go
// In product manifest:
// "trigger": "on_document_ingested"
//
// When a document is processed by the extraction pipeline,
// the event system dispatches the configured agent.
```

### 4. Agent-Initiated (Proactive)

```go
// An agent discovers something during execution and creates
// a new SpecTask in the graph. The TaskDispatcher picks it up
// on its next polling iteration.
//
// Discussion.resulted_in_task ──► SpecTask("add-cycle-detection")
```

---

## Resource & Cost Management

Each agent run costs tokens. The dispatcher tracks this via AgentRun metrics:

```go
type CostTracker struct {
    Budget     float64            // max cost for this workflow
    Spent      float64            // cost so far
    ByTask     map[string]float64 // cost per task
    ByAgent    map[string]float64 // cost per agent definition
}

// Before each agent dispatch
func (td *TaskDispatcher) checkBudget(agentDef AgentDefinition, estimatedTokens int) error {
    estimatedCost := td.estimateCost(agentDef.Model, estimatedTokens)
    if td.costs.Spent + estimatedCost > td.costs.Budget {
        return fmt.Errorf("budget exceeded: spent $%.2f of $%.2f budget",
            td.costs.Spent, td.costs.Budget)
    }
    return nil
}
```

---

## Parallel Task Execution

The DAG structure naturally enables parallelism. When multiple tasks are unblocked simultaneously, the TaskDispatcher runs them concurrently:

```
Example: Tasks 3a and 3b are both blocked by Task 2, but NOT by each other.

  Task 2 (design)
    │ blocks     │ blocks
    ▼            ▼
  Task 3a      Task 3b        ← dispatched in parallel as goroutines
  (implement   (implement
   schemas)     tools)
    │ blocks     │ blocks
    └────┬───────┘
         ▼
       Task 4 (test)           ← waits for BOTH 3a and 3b
```

The dispatcher naturally handles this — it queries for ALL unblocked tasks and dispatches up to `maxConcurrent` goroutines.

---

## Summary: The Three Loops

| Loop                | Level  | What it does                             | When it repeats                                               |
| ------------------- | ------ | ---------------------------------------- | ------------------------------------------------------------- |
| **Dispatcher Loop** | Outer  | Walks DAG, dispatches to unblocked tasks | Continuously until all tasks complete                         |
| **Discussion Loop** | Middle | Agents exchange proposals and feedback   | Repeats until consensus (max 3 rounds)                        |
| **Agent Execution** | Inner  | ADK-Go pipeline runs LLM + tools         | Single execution per dispatch (retries create new dispatches) |

The key insight: **emergent doesn't spawn external agents** — it builds ADK-Go pipelines from product-defined agent definitions and runs them as goroutines. Each agent gets a filtered tool set from the project's ToolPool via `ResolveTools()`. The knowledge graph is both the shared state and the coordination mechanism.
