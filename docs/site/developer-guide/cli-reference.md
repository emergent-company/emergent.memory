# CLI Reference

The `memory` CLI provides commands for setting up projects, querying the knowledge graph, and managing AI agent integrations. This page documents commands that are not covered elsewhere in the developer guide.

See `memory --help` for the full list of available commands and global flags.

---

## `memory init`

Interactive wizard that configures a Memory project in the current directory.

**What it does**

Walks through three steps:

1. **Project** — select an existing project or create a new one
2. **Provider** — configure an LLM provider at the org level (e.g., Google AI, Vertex AI)
3. **Skills** — install the built-in Memory skills into `.agents/skills/`

On completion it writes the following variables to `.env.local` and automatically adds `.env.local` to `.gitignore`:

| Variable | Description |
|---|---|
| `MEMORY_PROJECT_ID` | UUID of the configured project |
| `MEMORY_PROJECT_NAME` | Human-readable project name |
| `MEMORY_PROJECT_TOKEN` | Project-scoped API token |

Running `memory init` again in an already-configured directory detects the existing configuration and offers to verify or reconfigure each step.

**Flags**

| Flag | Description |
|---|---|
| `--skip-provider` | Skip the LLM provider configuration step |
| `--skip-skills` | Skip the Memory skills installation step |

**Example**

```bash
cd my-project
memory init
```

---

## `memory ask`

Ask the Memory CLI assistant a question or request a task. The assistant is context-aware and adapts based on your authentication state and whether a project is configured.

**Context modes**

| State | Behaviour |
|---|---|
| Not authenticated | Documentation answers; explains how to log in |
| Authenticated, no project | Account-level tasks + documentation answers |
| Authenticated + project active | Full task execution + documentation answers |

The assistant fetches live documentation from the Memory docs site to answer questions about the CLI, SDK, REST API, agents, and knowledge graph features. It can also execute tasks on your behalf.

**Examples**

```bash
memory ask "what are native tools?"
memory ask "what agents do I have configured?"
memory ask "how do I create a schema?"
memory ask --project abc123 "list all agent runs from today"
memory ask "what commands are available for managing API tokens?"
```

**Flags**

| Flag | Description |
|---|---|
| `--project <id>` | Project ID (uses default project if omitted) |
| `--json` | Output result as JSON `{question, response, tools, elapsedMs}` |
| `--show-tools` | Show tool calls made by the assistant during reasoning |
| `--show-time` | Show elapsed time at the end of the response |
| `--runtime <python\|go>` | Sandbox runtime for scripting tasks (default: `python`) |
| `--v2` | Use the v2 code-generation agent (fewer round-trips, faster) |

---

## `memory adk-sessions`

Alias: `memory sessions`

Manage and inspect Google ADK (Agent Development Kit) sessions. ADK sessions represent individual agent conversation threads, including the full event history of messages and tool calls.

### `memory adk-sessions list`

List all ADK sessions for the active project.

```bash
memory adk-sessions list
memory adk-sessions list --project <id>
memory adk-sessions list --json
```

Output per session: `ID: <id> | App: <app> | User: <user> | Updated: <timestamp>`

### `memory adk-sessions get`

Get full details and the complete event history for a specific session. Outputs the full session record as indented JSON including all events (user messages, agent responses, tool calls).

```bash
memory adk-sessions get <session-id>
memory sessions get <session-id> --project <id>
```

---

## `memory mcp-guide`

Print ready-to-use MCP server configuration snippets for connecting AI agents to Memory.

Outputs JSON configuration blocks for **Claude Desktop**, **Cursor**, and other MCP-compatible clients. Snippets use the active server URL and API key (project token takes precedence over account key).

```bash
memory mcp-guide
```

Copy the relevant block into your AI client's MCP configuration to enable Memory tools. For Claude Desktop the output corresponds to the entry in `~/Library/Application Support/Claude/claude_desktop_config.json`.

---

## `memory install-memory-skills`

Install the built-in Memory skills from the embedded catalog into `.agents/skills/` in the current directory (or a custom directory).

Memory skills are instruction files that teach AI agents (Claude, GPT-4, etc.) how to use the Memory CLI and platform. By default, only skills with the `memory-` prefix are installed.

**Flags**

| Flag | Description |
|---|---|
| `--dir <path>` | Target directory (default: `.agents/skills` relative to cwd) |
| `--force` | Overwrite existing skill directories |

**Examples**

```bash
# Install into the default .agents/skills/ directory
memory install-memory-skills

# Install into a custom directory
memory install-memory-skills --dir ./my-agent/skills

# Re-install / update all skills, overwriting existing ones
memory install-memory-skills --force
```

Existing skill directories are skipped by default. Use `--force` to update them to the latest embedded versions.
