# Blueprint Architect

A dedicated authoring agent that designs and emits Memory blueprints for a project.

## What it installs

**Agent** — `blueprint-architect`: given a domain description, it designs object
types, relationship types, agent definitions, and seed data, registers the schema
in the live project, and emits a reusable blueprint directory.

## Install

```bash
memory blueprints install github.com/emergent-company/emergent.memory/blueprints/blueprint-architect --project <your-project-slug>
```

Or from a local clone:

```bash
memory blueprints install ./blueprints/blueprint-architect --project <your-project-slug>
```

## Usage

1. Trigger the `blueprint-architect` agent with a domain description (e.g.
   "a CRM: contacts, companies, deals, and the agents that maintain them").
2. Review the proposed schema at the checkpoint, or pick one of the alternatives.
3. The agent registers the schema and emits the full blueprint as YAML/JSONL.
4. Save the emitted files to a directory and re-install anywhere:
   ```bash
   memory blueprints validate ./my-blueprint
   memory blueprints install ./my-blueprint --project <other-project>
   ```

## Tools

The agent is granted schema, entity, relationship, graph, search, skill, journal,
and memory tools via name globs (`schema-*`, `entity-*`, `relationship-*`, `graph-*`,
`search-*`, `skill-*`, `journal-*`) plus `remember`, `forget`, `project-briefing`,
and `ask_user` for human checkpoints. It uses the project's default model — no
model is pinned.
