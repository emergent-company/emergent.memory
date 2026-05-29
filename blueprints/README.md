# Example Blueprints

Reference blueprints you can install into any Memory project. Each is a self-contained directory that installs schemas, agents, and seed data.

## Available examples

### [multi-agent](./multi-agent/)

A complete hierarchical multi-agent orchestration system. Installs a schema pack for work lifecycle tracking (WorkPackages, Tasks, AcceptanceCriteria, FeedbackSignals), 11 agent definitions across three tiers (orchestrator, pool managers, leaf agents), and pre-wired agent pools and KPIs.

```bash
memory blueprints install github.com/emergent-company/emergent.memory/blueprints/multi-agent --project <slug>
```

### [code-memory-blueprint](./code-memory-blueprint/)

Installs the `code-structure` schema pack — maps the structural and architectural layer of any software codebase. Works for any language or framework (Go, TypeScript, Python, Swift, Rust, etc.).

```bash
memory blueprints install github.com/emergent-company/emergent.memory/blueprints/code-memory-blueprint --project <slug>
```

## Using blueprints

```bash
# Preview what a blueprint will install (no changes made)
memory blueprints inspect <source>

# Validate a blueprint offline
memory blueprints validate <source>

# Install
memory blueprints install <source> --project <slug>
```

Sources can be a local directory path or a GitHub URL (`github.com/org/repo/path`).
