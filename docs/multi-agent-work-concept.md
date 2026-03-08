Conceptual Spec: Dynamic Multi-Agent Orchestration System
Overview
A self-improving, event-driven multi-agent system organized around a shared knowledge graph. Agents operate at different levels of abstraction, each with defined capabilities, tools, and accountability. The system continuously learns from feedback and experiments to optimize itself over time.

Core Abstractions
Tasks & Work Packages

Every unit of work is a task
Tasks can have subtasks, forming a tree structure
A work package is a top-level task that owns a full execution tree
Tasks have:

Type (work package, research, enrichment, coding, review, etc.)
Status (created, in-progress, blocked, complete, rejected)
Dependencies
Acceptance criteria — a checklist defining what "good" looks like
Assigned agent
Metadata (time spent, tokens used, rejection count, feedback)



Agents

Agents have a type, a set of skills, and access to specific tools
Some agents can create subtasks and delegate (coordinators); others only execute (leaf agents)
Agents are grouped into pools managed by pool managers
Agent types are defined as templates that can be instantiated dynamically

Knowledge Graph
Two layers:
Domain Layer — actual product knowledge: bugs, code, features, relationships, research findings
System Layer — meta-knowledge about the organization itself: agent performance metrics, experiment results, task trails, rejection patterns, KPIs

Agent Hierarchy
Leaf Agents

Execute a specific task; no delegation capability
Examples: enrichment agent, researcher, coder, reviewer
Always work against acceptance criteria
Return structured results + self-assessment against criteria

Pool Managers

Own a pool of agent variants (e.g. researcher manager)
Responsible for agent selection, A/B testing within the pool, performance monitoring
Add new agent variants when new models become available
Report pool health upward

Orchestrator

Receives a work package and breaks it into subtasks
Understands available agent pools and assigns tasks accordingly
Can create subtasks for itself (to gather information before deciding next steps)
Waits for blockers to resolve before continuing its own work
Has access to A/B testing framework — can decide to run multiple agent variants in parallel
Presents plan summary to human before execution; presents results at key checkpoints
Proposes three options when asking for human input

Janitor Orchestrator

Works like a regular orchestrator but its domain is the system itself
Triggered periodically (e.g. after every N closed work packages)
Analyzes closed work package trails: time, tokens, rejections, feedback, experiment results
Proposes structural changes: new agent types, model swaps, template updates, tool additions
All proposals require human approval before execution
Has exclusive capability to create/modify agent templates
Human acceptance/rejection of janitor proposals is itself a performance signal


Feedback & Quality System
Acceptance Criteria

Every task assignment includes a checklist of success criteria
Receiving agent checks criteria before submitting
Reviewing agent/orchestrator evaluates against criteria on receipt
Rejection must reference which criteria were not met

Feedback Signals

Accept — task output meets criteria and delivers real value
Reject with explanation — criteria missed or quality insufficient; explanation is optional but strongly encouraged
Human feedback — same signal format; highest weight in performance scoring
Rejection patterns per agent are tracked in the system layer of the knowledge graph

Performance Metrics (KPIs)

Rejection rate per agent type
Average task completion time
Token cost per task type
Reopening rate (tasks sent back after initial completion)
Human approval rate for orchestrator plans
Human approval rate for janitor proposals


A/B Testing Framework

Orchestrators have access to an A/B testing tool as a standard capability
Default strategy: start with cheapest/simplest model; escalate on rejection
Rejection triggers experiment: spin up 2–3 variants of the same task with different agents/models
Experiments are bound to a specific task branch, not the whole work package
Results scored against acceptance criteria + quality feedback
Experiment metadata stored in system layer for janitor analysis
Configurable testing rate (e.g. always test on rejection, or test X% of tasks proactively)
Pool managers use experiment results to update agent selection logic


Human-in-the-Loop
Touchpoints

Plan review — orchestrator presents summary + three options before starting significant work
Checkpoint reviews — at key milestones within a work package
Janitor proposals — all structural system changes require human approval

Principles

Higher-level decisions get more human oversight; leaf-level work runs autonomously
Agent work is cheap and reversible; human time is the scarce resource
Human rejection is always a signal — logged, weighted, fed back into system learning
Humans don't micromanage; they validate direction and make strategic calls


Model & Agent Evolution

New models are registered as available resources in the system
Janitor detects new models, assesses fit for existing agent types, proposes new variants to relevant pool managers
Pool managers run A/B tests to validate new variants before promoting to standard pool
Underperforming agents are flagged, retrained (via prompt/instruction updates), or retired
All changes are logged so before/after performance is always comparable


Recommended Starting Point (Lab Phase)
Before production deployment, validate the architecture with a contained test:

Define a simple task (e.g. "build a to-do list app")
Run it through the full system: enrichment → research → design → coding
Observe task tree structure, rejection patterns, agent handoffs
Use this to calibrate acceptance criteria templates, orchestrator instructions, and KPI baselines
Iterate on abstractions before connecting to real production workloads


Key Design Principles

Minimal rigid structure — define the core abstractions, keep everything else flexible
Feedback loops at every level — no agent works without a reviewer
Cheap to experiment, cheap to scrap — agent work is disposable; direction changes are cheap
Progressive escalation — start simple, escalate on failure
Self-improving — the system gets better with each work package through logging, testing, and janitor analysis
Human as strategic decision maker — not a bottleneck, but the final authority on direction and structure
