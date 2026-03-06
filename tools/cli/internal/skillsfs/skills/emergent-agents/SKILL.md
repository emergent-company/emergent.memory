---
name: emergent-agents
description: Manage Emergent runtime agents and agent definitions — create, trigger, monitor runs, respond to questions, and manage agent configurations. Use when the user wants to run, inspect, or configure AI agents in an Emergent project.
metadata:
  author: emergent
  version: "1.0"
---

Manage runtime agents and agent definitions in an Emergent project using `emergent agents` and `emergent agent-definitions`.

## Concepts

- **Agent definition** (`agent-definitions`) — the blueprint: system prompt, model config, tools, flow type, visibility. Created once, reused.
- **Agent** (`agents`) — a live instance of a definition in a project, with scheduling, triggers, and run history.

---

## Agent Definitions

### List definitions
```bash
emergent agent-definitions list
emergent agent-defs list --output json
```

### Get definition details
```bash
emergent agent-definitions get <definition-id>
```

### Create a definition
```bash
emergent agent-definitions create --name "My Agent" --description "Does X"
```

### Update a definition
```bash
emergent agent-definitions update <definition-id> --name "New Name"
```

### Delete a definition
```bash
emergent agent-definitions delete <definition-id>
```

---

## Runtime Agents

### List agents in a project
```bash
emergent agents list
emergent agents list --project-id <id> --output json
```

### Get agent details
```bash
emergent agents get <agent-id>
```

### Create an agent (instantiate a definition in a project)
```bash
emergent agents create --name "My Agent" --definition-id <def-id>
```

### Trigger an agent run
```bash
emergent agents trigger <agent-id>
```

### View recent runs
```bash
emergent agents runs <agent-id>
emergent agents runs <agent-id> --limit 20
```

### Update an agent
```bash
emergent agents update <agent-id> --name "New Name"
```

### Delete an agent
```bash
emergent agents delete <agent-id>
```

---

## Agent Questions

Agents can pause and ask clarifying questions. Use these commands to list and respond.

### List questions for a specific run
```bash
emergent agents questions list <run-id>
```

### List all pending questions for a project
```bash
emergent agents questions list-project
emergent agents questions list-project --project-id <id>
```

### Respond to a question
```bash
emergent agents questions respond <question-id> --answer "Yes, proceed"
```

---

## Webhook Hooks

### Manage hooks on an agent
```bash
emergent agents hooks list <agent-id>
```

---

## Workflow

1. **First-time setup**: create an agent definition, then create an agent in your project referencing that definition
2. **Running an agent**: `trigger` kicks off an immediate run; check `runs` to see status
3. **Monitoring**: use `runs --limit 50 --output json` for scripted monitoring
4. **Blocked agent**: if a run is waiting for input, use `questions list-project` to find pending questions, then `questions respond` to unblock it
5. **Finding IDs**: use `list --output json` and look up by name

## Notes

- `agent-definitions` has aliases: `agent-defs`, `defs`
- `--project-id` global flag selects the project; falls back to config default
- Agent and definition IDs are UUIDs
