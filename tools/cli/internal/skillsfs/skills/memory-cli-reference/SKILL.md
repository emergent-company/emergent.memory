---
name: memory-cli-reference
description: Full Memory CLI command reference with all subcommands and flags. Use when you need exact command syntax, flag names, or usage examples for any `memory` CLI command.
metadata:
  author: emergent
  version: "1.0"
---

This skill contains the complete `memory` CLI command reference, auto-generated from the binary.

Use this when you need to look up:
- Exact subcommand names (e.g. `memory agents get-run`, `memory provider configure-project`)
- Available flags and their types for any command
- Usage examples embedded in the help text
- Which subcommands exist under a parent command

# Memory CLI Reference

Full command reference auto-generated from `memory --help`. Each section covers one command or subcommand with its synopsis, usage, and flags.

---

## memory

CLI tool for Memory platform

### Synopsis

Command-line interface for the Memory knowledge base platform.

Manage projects, documents, graph objects, AI agents, and MCP integrations.

For self-hosted deployments, use 'memory server' to install and manage your server.

### Options

```
      --compact                use compact output layout
      --config string          config file (default is $HOME/.memory/config.yaml)
      --debug                  enable debug logging
  -h, --help                   help for memory
      --json                   shorthand for --output json
      --no-color               disable colored output
      --output string          output format (table, json, yaml, csv) (default "table")
      --project string         project ID (overrides config and environment)
      --project-token string   project token (overrides config and environment)
      --server string          Memory server URL
```

## memory acp

Agent Communication Protocol (ACP) operations

### Synopsis

Commands for interacting with agents via the Agent Communication Protocol (ACP) v1 API.

### Options

```
  -h, --help   help for acp
```

## memory acp agents

Manage ACP agents

### Synopsis

Discover and inspect agents exposed via the Agent Communication Protocol.

### Options

```
  -h, --help   help for agents
```

## memory acp agents get

Get an ACP agent manifest

### Synopsis

Get the full manifest for a specific ACP agent by its slug name.

Displays the agent's name, description, capabilities, input/output modes,
and status metrics. Use --json for the raw JSON response.

```
memory acp agents get <name> [flags]
```

### Options

```
  -h, --help   help for get
```

## memory acp agents list

List externally-visible ACP agents

### Synopsis

List all agents with visibility='external' that are exposed via ACP.

Displays a table with NAME, DESCRIPTION, VERSION, and SUCCESS RATE columns.
Use --json to output the raw JSON response.

```
memory acp agents list [flags]
```

### Options

```
  -h, --help   help for list
```

## memory acp ping

Check that the ACP endpoint is reachable

### Synopsis

Ping the ACP v1 endpoint on the configured Memory server.

This command does not require authentication.

```
memory acp ping [flags]
```

### Options

```
  -h, --help   help for ping
```

## memory acp runs

Manage ACP agent runs

### Synopsis

Create, inspect, cancel, and resume agent runs via ACP.

### Options

```
  -h, --help   help for runs
```

## memory acp runs cancel

Cancel an ACP run

### Synopsis

Cancel a running or queued ACP run.

Requests cancellation of the specified run. The run transitions to
'cancelling' and eventually 'cancelled'.

```
memory acp runs cancel <agent-name> <run-id> [flags]
```

### Options

```
  -h, --help   help for cancel
```

## memory acp runs create

Create a new ACP agent run

### Synopsis

Create a new run for an ACP agent.

Requires --message with the user's input text. The --mode flag controls
execution mode:
  sync   (default) — blocks until the run completes, prints output
  async  — returns immediately with the run ID and status
  stream — streams the agent's output to stdout in real-time

If a sync run pauses for human input (status: input-required), the CLI
will prompt you interactively and automatically resume the run.

Use --session to link the run to an existing ACP session.

```
memory acp runs create <agent-name> [flags]
```

### Options

```
  -h, --help             help for create
      --message string   Input message for the agent (required)
      --mode string      Execution mode: sync, async, stream (default "sync")
      --session string   Link run to an existing ACP session ID
```

## memory acp runs get

Get an ACP run by ID

### Synopsis

Get the state of a specific ACP run.

Displays run status, output messages, and timing. If the run is paused
(input-required), shows the pending question. Use --json for raw JSON output.

```
memory acp runs get <agent-name> <run-id> [flags]
```

### Options

```
  -h, --help   help for get
```

## memory acp runs resume

Resume a paused ACP run

### Synopsis

Resume an ACP run that is waiting for human input (status: input-required).

Requires --message with the response to the agent's question.
Use --mode to control execution mode (sync, async, stream).

```
memory acp runs resume <agent-name> <run-id> [flags]
```

### Options

```
  -h, --help             help for resume
      --message string   Response message (required)
      --mode string      Execution mode: sync, async, stream (default "sync")
```

## memory acp sessions

Manage ACP sessions

### Synopsis

Create and inspect ACP sessions for grouping related agent runs.

### Options

```
  -h, --help   help for sessions
```

## memory acp sessions create

Create a new ACP session

### Synopsis

Create a new ACP session.

Sessions group related agent runs together. Use --agent to scope the
session to a specific agent.

```
memory acp sessions create [flags]
```

### Options

```
      --agent string   Scope session to a specific agent
  -h, --help           help for create
```

## memory acp sessions get

Get an ACP session

### Synopsis

Get details of an ACP session including its run history.

Use --json for raw JSON output.

```
memory acp sessions get <session-id> [flags]
```

### Options

```
  -h, --help   help for get
```

## memory adk-sessions

Manage and inspect ADK sessions

### Synopsis

Manage and inspect Google ADK (Agent Development Kit) sessions.

ADK sessions represent individual agent conversation threads, including the full
event history of messages and tool calls. Use the list subcommand to browse
sessions for a project, and the get subcommand to inspect a specific session in
detail.

### Options

```
  -h, --help             help for adk-sessions
      --project string   Project name or ID
```

## memory adk-sessions get

Get details and event history for a specific ADK session

### Synopsis

Get full details and the complete event history for a specific ADK session.

Outputs the entire session record as indented JSON, including all events (user
messages, agent responses, and tool calls) in the session history.

```
memory adk-sessions get [id] [flags]
```

### Options

```
  -h, --help   help for get
```

## memory adk-sessions list

List ADK sessions for the active project

### Synopsis

List all ADK sessions for the active (or specified) project.

Each session is printed on one line with its session ID, App name, User ID, and
last Updated timestamp in the format:
  ID: <id> | App: <app> | User: <user> | Updated: <timestamp>

```
memory adk-sessions list [flags]
```

### Options

```
  -h, --help   help for list
```

## memory agent-definitions

Manage agent definitions

### Synopsis

Commands for managing agent definitions (system prompts, tools, model config, flow type, visibility)

### Options

```
  -h, --help   help for agent-definitions
```

## memory agent-definitions create

Create a new agent definition

### Synopsis

Create a new agent definition.

Examples:
  memory agent-definitions create --name "my-def" --system-prompt "You are a helpful agent"
  memory defs create --name "extractor" --flow-type single --tools "search,graph_query" --visibility project

```
memory agent-definitions create [flags]
```

### Options

```
      --default-timeout int    Default timeout in seconds
      --description string     Description
      --flow-type string       Flow type (single, multi, coordinator)
  -h, --help                   help for create
      --is-default string      Set as default definition (true/false)
      --max-steps int          Maximum steps per run
      --model string           Model name (e.g., gemini-2.0-flash)
      --name string            Definition name (required)
      --skills string          Comma-separated skill names (e.g. "code-review,*")
      --system-prompt string   System prompt
      --tools string           Comma-separated tool names
      --visibility string      Visibility (external, project, internal)
```

## memory agent-definitions delete

Delete an agent definition

### Synopsis

Delete an agent definition by ID

```
memory agent-definitions delete [id] [flags]
```

### Options

```
  -h, --help   help for delete
```

## memory agent-definitions get

Get agent definition details

### Synopsis

Get full details for a specific agent definition by ID.

Prints Name, ID, ProjectID, FlowType, Visibility, IsDefault, Description (if
set), System Prompt (truncated to 200 characters), Model configuration (Name,
Temperature, MaxTokens), Tools list, MaxSteps, DefaultTimeout, ACP Config
(DisplayName, Description, Capabilities), CreatedAt and UpdatedAt timestamps,
and any extra Config JSON.

```
memory agent-definitions get [id] [flags]
```

### Options

```
  -h, --help   help for get
      --json   Output as JSON
```

## memory agent-definitions list

List all agent definitions

### Synopsis

List all agent definitions for the current project.

Prints a numbered list with each definition's Name, ID, FlowType, Visibility,
IsDefault flag, Tool count, and Description (if set).

```
memory agent-definitions list [flags]
```

### Options

```
  -h, --help        help for list
      --json        Output as JSON
      --limit int   Maximum number of definitions to show (0 = all)
      --page int    Page number (1-based, used with --limit) (default 1)
```

## memory agent-definitions override

View or set per-project agent overrides

### Synopsis

View or set per-project configuration overrides for an agent definition.

Without flags, shows the current override for the agent. With flags, sets
or updates the override. Overrides are merged on top of canonical defaults
each time the agent runs — non-overridden fields always get the latest code defaults.

Examples:
  memory defs override graph-query-agent                          # view current override
  memory defs override graph-query-agent --model gemini-2.5-pro   # override model
  memory defs override cli-assistant-agent --max-steps 30         # override max steps
  memory defs override graph-query-agent --model gemini-2.5-pro --temperature 0.2 --max-steps 20
  memory defs override graph-query-agent --system-prompt-file prompt.txt
  memory defs override graph-query-agent --sandbox-enabled false  # disable sandbox
  memory defs override graph-query-agent --clear                  # remove override

```
memory agent-definitions override [agentName] [flags]
```

### Options

```
      --clear                       Remove override — revert to canonical defaults
  -h, --help                        help for override
      --max-steps int               Override max steps
      --model string                Override model name (e.g., gemini-2.5-pro)
      --sandbox-enabled string      Override sandbox enabled state (true/false)
      --system-prompt string        Override system prompt
      --system-prompt-file string   Read system prompt from file
      --temperature float32         Override temperature (0.0-2.0) (default -1)
      --tools string                Override tools (comma-separated)
```

## memory agent-definitions overrides

List all agent overrides for the project

### Synopsis

List all per-project agent configuration overrides.

```
memory agent-definitions overrides [flags]
```

### Options

```
  -h, --help   help for overrides
```

## memory agent-definitions update

Update an agent definition

### Synopsis

Update an existing agent definition (partial update)

```
memory agent-definitions update [id] [flags]
```

### Options

```
      --default-timeout int    New default timeout
      --description string     New description
      --flow-type string       New flow type
  -h, --help                   help for update
      --is-default string      Set as default (true/false)
      --max-steps int          New max steps
      --model string           New model name
      --name string            New name
      --skills string          New comma-separated skill names
      --system-prompt string   New system prompt
      --tools string           New comma-separated tool names
      --visibility string      New visibility
```

## memory agents

Manage runtime agents

### Synopsis

Commands for managing runtime agents (scheduling, triggers, execution state)

### Options

```
  -h, --help             help for agents
      --project string   Project name or ID (auto-detected from config/env if not specified)
```

## memory agents builtin-tools

Manage built-in tools

### Synopsis

Commands for managing built-in (Go-native) tools in the Memory platform.

Built-in tools are implemented directly in the server and are available to all
agents without requiring an external MCP server connection. Examples include
query_entities, brave_web_search, webfetch, and create_document.

Use 'memory agents mcp-servers' to manage externally-connected MCP servers.

### Options

```
  -h, --help   help for builtin-tools
```

## memory agents builtin-tools configure

Set runtime config for a built-in tool

### Synopsis

Set runtime configuration key/value pairs for a named built-in tool.

Looks up the tool by name and patches its config. Only the provided keys are
updated; existing keys not mentioned are left unchanged.

Examples:
  memory agents builtin-tools configure brave_web_search api_key=YOUR_KEY
  memory agents builtin-tools configure reddit_search client_id=ID client_secret=SECRET

```
memory agents builtin-tools configure [tool-name] [key=value ...] [flags]
```

### Options

```
  -h, --help   help for configure
```

## memory agents builtin-tools list

List all built-in tools

### Synopsis

List all built-in tools registered for the current project.

Prints each tool's enabled/disabled state, name, and description. Tools that
require runtime configuration (e.g. API keys) are shown with their config status.
The 'Source' column shows where the effective settings come from: project, org,
or global (server default).

```
memory agents builtin-tools list [flags]
```

### Options

```
  -h, --help        help for list
      --limit int   Maximum number of tools to show (0 = all)
      --page int    Page number (1-based, used with --limit) (default 1)
```

## memory agents builtin-tools toggle

Enable or disable a built-in tool

### Synopsis

Enable or disable a built-in tool for the current project.

The tool-id is the UUID shown in 'memory agents builtin-tools list'.

Examples:
  memory agents builtin-tools toggle <tool-id> off
  memory agents builtin-tools toggle <tool-id> on

```
memory agents builtin-tools toggle [tool-id] [on|off] [flags]
```

### Options

```
  -h, --help   help for toggle
```

## memory agents create

Create a new agent

### Synopsis

Create a new runtime agent for the current project.

Examples:
  memory agents create --name "my-agent" --project <id>
  memory agents create --name "cron-agent" --trigger-type schedule --cron "0 */5 * * * *"
  memory agents create --name "reaction-agent" --trigger-type reaction --reaction-events created,updated --reaction-object-types document

```
memory agents create [flags]
```

### Options

```
      --cron string                    Cron schedule (e.g., '0 */5 * * * *')
      --description string             Agent description
      --enabled string                 Enable agent (true/false)
      --execution-mode string          Execution mode
  -h, --help                           help for create
      --name string                    Agent name (required)
      --prompt string                  Agent prompt
      --reaction-events string         Comma-separated reaction event types (e.g., created,updated)
      --reaction-object-types string   Comma-separated reaction object types (e.g., document,chunk)
      --strategy-type string           Strategy type (e.g., graph_object_processor)
      --trigger-type string            Trigger type (manual, schedule, reaction, webhook)
```

## memory agents delete

Delete an agent

### Synopsis

Delete an agent by ID.

Prints "Agent <id> deleted successfully." on success.

```
memory agents delete [id] [flags]
```

### Options

```
  -h, --help   help for delete
```

## memory agents get-run

Get details for a specific run

### Synopsis

Get full details for a specific agent run by its run ID. Output includes:
  - Run ID, agent ID, status, start/end times
  - Token usage: total input tokens, total output tokens
  - Estimated cost in USD
  - Root run ID (for sub-runs triggered by a parent run)
  - Any output or error message from the run

No --project flag is required — run IDs are globally unique.

This is the primary command to check the cost of a specific agent run.

```
memory agents get-run [run-id] [flags]
```

### Options

```
  -h, --help   help for get-run
      --json   Output result as JSON
```

## memory agents get

Get agent details

### Synopsis

Get full details for a specific agent by its ID.

Prints Name, ID, Project ID, Strategy Type, Enabled status, Trigger Type,
Execution Mode, Cron Schedule (if set), Description (if set), Prompt (if set),
Reaction Config (Object Types and Events), Last Run At, Last Run Status,
Created At, Updated At, and any extra Config JSON.

```
memory agents get [id] [flags]
```

### Options

```
  -h, --help   help for get
      --json   Output as JSON
```

## memory agents hooks

Manage agent webhook hooks

### Synopsis

Commands for managing webhook hooks on agents (create, list, delete)

### Options

```
  -h, --help   help for hooks
```

## memory agents hooks create

Create a webhook hook

### Synopsis

Create a new webhook hook for an agent. The plaintext token is only shown once.

Examples:
  memory agents hooks create <agent-id> --label "CI/CD Pipeline"
  memory agents hooks create <agent-id> --label "Staging" --rate-limit 30 --burst-size 5

```
memory agents hooks create [agent-id] [flags]
```

### Options

```
      --burst-size int   Burst size for rate limiting (0 = server default)
  -h, --help             help for create
      --label string     Hook label (required)
      --rate-limit int   Rate limit in requests per minute (0 = server default)
```

## memory agents hooks delete

Delete a webhook hook

### Synopsis

Delete a webhook hook from an agent

```
memory agents hooks delete [agent-id] [hook-id] [flags]
```

### Options

```
  -h, --help   help for delete
```

## memory agents hooks list

List webhook hooks

### Synopsis

List all webhook hooks configured for an agent.

Prints a numbered list with each hook's Label, ID, Enabled status, Rate Limit
configuration (requests/minute and burst size, if set), and Created timestamp.

```
memory agents hooks list [agent-id] [flags]
```

### Options

```
  -h, --help   help for list
```

## memory agents list

List all agents

### Synopsis

List all agents configured for the current project.

Prints a numbered list with each agent's Name, ID, Enabled status, Trigger
Type, Cron schedule (if any), Description (if set), Last Run timestamp, and
Last Run Status. Use --project to specify a project other than the active one.

```
memory agents list [flags]
```

### Options

```
  -h, --help        help for list
      --json        Output as JSON
      --limit int   Maximum number of agents to show (0 = all)
      --page int    Page number (1-based, used with --limit) (default 1)
```

## memory agents mcp-servers

Manage MCP servers

### Synopsis

Commands for managing Model Context Protocol (MCP) servers in the Memory platform

### Options

```
  -h, --help   help for mcp-servers
```

## memory agents mcp-servers configure

Configure a tool's runtime settings

### Synopsis

Set runtime configuration key/value pairs for a named MCP tool.

The command searches all MCP servers in the current project to find the tool
by name, then patches its config with the provided key=value pairs.

Examples:
  memory agents mcp-servers configure brave_web_search api_key=YOUR_KEY --project <id>
  memory agents mcp-servers configure reddit_search client_id=YOUR_ID client_secret=YOUR_SECRET --project <id>

```
memory agents mcp-servers configure [tool-name] [key=value ...] [flags]
```

### Options

```
  -h, --help   help for configure
```

## memory agents mcp-servers create

Create a new MCP server

### Synopsis

Register a new MCP server with the specified configuration.

Examples:
 memory agents mcp-servers create --name "my-server" --type sse --url "http://localhost:8080/sse"
 memory agents mcp-servers create --name "stdio-server" --type stdio --command "npx" --args "-y,@modelcontextprotocol/server-github"
 memory agents mcp-servers create --name "my-server" --type http --url "http://localhost:8080/mcp" --env "API_KEY=abc123"

```
memory agents mcp-servers create [flags]
```

### Options

```
      --args string          Comma-separated arguments (for stdio type)
      --command string       Command to run (for stdio type)
      --description string   Server description
      --enabled string       Enable server (true/false, default: true)
      --env strings          Environment variables (KEY=VALUE format, can be specified multiple times)
  -h, --help                 help for create
      --name string          Server name (required)
      --type string          Server type: 'sse', 'stdio', or 'http' (required)
      --url string           Server URL (for sse/http types)
```

## memory agents mcp-servers delete

Delete an MCP server

### Synopsis

Remove an MCP server and all its tools from your project configuration

```
memory agents mcp-servers delete [server-id] [flags]
```

### Options

```
  -h, --help   help for delete
```

## memory agents mcp-servers get

Get MCP server details

### Synopsis

Get full details for a specific MCP server, including its registered tools.

Prints Name (enabled/disabled), ID, Project ID, Description (if set), Type,
URL (for sse/http), Command and Args (for stdio), Env Vars count, Headers count,
Created, and Updated timestamps. Also lists all registered tools with their
enabled/disabled state and description (truncated to 60 characters).

```
memory agents mcp-servers get [server-id] [flags]
```

### Options

```
  -h, --help   help for get
```

## memory agents mcp-servers inspect

Inspect an MCP server

### Synopsis

Test connection to an MCP server and display its capabilities, tools, prompts, and resources

```
memory agents mcp-servers inspect [server-id] [flags]
```

### Options

```
  -h, --help   help for inspect
```

## memory agents mcp-servers list

List all MCP servers

### Synopsis

List all MCP servers configured for the current project.

Prints a numbered list with each server's Name (enabled/disabled), Description
(if set), ID, Type (sse/http/stdio), URL or Command, Tool count, and Created
timestamp.

```
memory agents mcp-servers list [flags]
```

### Options

```
  -h, --help   help for list
```

## memory agents mcp-servers sync

Sync tools from an MCP server

### Synopsis

Connect to the MCP server and refresh the list of available tools

```
memory agents mcp-servers sync [server-id] [flags]
```

### Options

```
  -h, --help   help for sync
```

## memory agents mcp-servers tools

List tools for an MCP server

### Synopsis

List all tools registered for a specific MCP server.

Each tool entry shows its enabled/disabled state and tool name. Use
'memory agents mcp-servers sync <id>' first to discover available tools if the list
is empty.

```
memory agents mcp-servers tools [server-id] [flags]
```

### Options

```
  -h, --help   help for tools
```

## memory agents questions

Manage agent questions

### Synopsis

Commands for listing and responding to agent questions

### Options

```
  -h, --help   help for questions
```

## memory agents questions list-project

List questions for a project

### Synopsis

List all agent questions for the current project.

Outputs the full question list as indented JSON. Use --status to filter by
question status (e.g. pending, answered).

```
memory agents questions list-project [flags]
```

### Options

```
  -h, --help            help for list-project
      --status string   Filter by status (pending, answered, cancelled, expired)
```

## memory agents questions list

List questions for a run

### Synopsis

List all questions asked by the agent during a specific run.

Outputs the full question list as indented JSON, including each question's ID,
status, prompt text, and response (if already answered).

```
memory agents questions list [run-id] [flags]
```

### Options

```
  -h, --help   help for list
```

## memory agents questions respond

Respond to a question

### Synopsis

Respond to a pending agent question and resume the paused agent run.

Sends the response text as the answer to the specified question. Outputs the
updated question record as indented JSON on success.

```
memory agents questions respond [question-id] [response] [flags]
```

### Options

```
  -h, --help   help for respond
```

## memory agents runs

List agent runs

### Synopsis

List recent runs for an agent. Each run entry shows:
  - Run ID and status (running, completed, failed)
  - Start time and duration
  - Token usage: input tokens / output tokens
  - Estimated cost in USD (e.g. "Cost: $0.001234")

Use --limit to control how many runs are returned (default 10).
Use "memory agents get-run [run-id]" to get the full breakdown for a specific run.

```
memory agents runs [id] [flags]
```

### Options

```
  -h, --help        help for runs
      --json        Output as JSON
      --limit int   Maximum number of runs to return (default 10)
```

## memory agents trigger

Trigger an agent run

### Synopsis

Trigger an immediate run of an agent.

Prints "Agent triggered successfully!" with an optional message on success, or
"Agent trigger failed." with an error message on failure.

```
memory agents trigger [id] [flags]
```

### Options

```
      --env stringArray   Environment variable to inject into sandbox (KEY=VALUE, repeatable)
  -h, --help              help for trigger
      --input string      Initial message to pass to the agent at trigger time
      --model string      Override the model for this single run (e.g. claude-sonnet-4.7)
```

## memory agents update

Update an agent

### Synopsis

Update an existing agent (partial update)

```
memory agents update [id] [flags]
```

### Options

```
      --cron string             New cron schedule
      --description string      New description
      --enabled string          Enable/disable (true/false)
      --execution-mode string   New execution mode
  -h, --help                    help for update
      --name string             New agent name
      --prompt string           New agent prompt
      --trigger-type string     New trigger type
```

## memory ask

Ask the Memory CLI assistant a question or request a task

### Synopsis

Ask the Memory CLI assistant a question or request a task.

The assistant is context-aware — it adapts its responses based on whether you
are authenticated and whether a project is configured:

  • Not authenticated     → documentation answers; explains how to log in
  • Auth, no project      → account-level tasks + documentation answers
  • Auth + project active → full task execution + documentation answers

The assistant fetches live documentation from the Memory docs site to answer
questions about the CLI, SDK, REST API, agents, and knowledge graph features.
It can also execute tasks on your behalf (list agents, query the graph, etc.).

Words after "ask" are joined into the question — quotes are optional.

Examples:
  memory ask what are native tools?
  memory ask what agents do I have configured?
  memory ask how do I create a schema?
  memory ask --project abc123 list all agent runs from today
  memory ask what commands are available for managing API tokens?

```
memory ask <question...> [flags]
```

### Options

```
  -h, --help             help for ask
      --json             Output result as JSON {question, response, tools, elapsedMs}
      --project string   Project ID (optional — uses default project if configured)
      --runtime string   Sandbox runtime for scripting tasks: python (default) or go
      --show-time        Show elapsed time at the end of the response
      --show-tools       Show tool calls made by the assistant during reasoning
      --v2               Use the v2 code-generation agent (fewer round-trips, faster)
```

## memory blueprints

Install or export Blueprints (schemas, agents, skills, seed data)

### Synopsis

Blueprints are structured directories (or GitHub repos) containing schemas,
agent definitions, skills, and seed data to apply to a project.

Subcommands:

  memory blueprints install <source>    Apply blueprints from a directory or GitHub URL
  memory blueprints inspect <source>    List contents of a blueprint without installing
  memory blueprints validate <source>   Validate a blueprint offline (no API calls)
  memory blueprints dump <output-dir>   Export project graph data as JSONL seed files

Examples:

  memory blueprints install ./my-config
  memory blueprints install https://github.com/acme/memory-blueprints
  memory blueprints install https://github.com/acme/memory-blueprints#v1.2.0 --upgrade
  memory blueprints inspect https://github.com/acme/memory-blueprints
  memory blueprints install ./my-config --dry-run
  memory blueprints dump ./exported

```
memory blueprints [flags]
```

### Options

```
      --dry-run          Preview actions without making any API calls
  -h, --help             help for blueprints
      --project string   Project ID or name (overrides config/env)
      --token string     GitHub personal access token (for private repos); also read from MEMORY_GITHUB_TOKEN
      --upgrade          Update existing resources instead of skipping them
```

## memory blueprints dump

Export project graph objects and relationships as JSONL seed files

### Synopsis

Export the current project's graph objects and relationships as per-type JSONL
seed files that can be re-applied with "memory blueprints install <dir>".

Output layout:
  <output-dir>/seed/objects/<Type>.jsonl
  <output-dir>/seed/relationships/<Type>.jsonl

Files exceeding 50 MB are automatically split:
  <Type>.001.jsonl, <Type>.002.jsonl, …

Examples:

  memory blueprints dump ./exported
  memory blueprints dump ./exported --types Document,Person
  memory blueprints dump ./exported --project my-project

```
memory blueprints dump <output-dir> [flags]
```

### Options

```
  -h, --help           help for dump
      --types string   Comma-separated list of object/relationship types to export (default: all types)
```

## memory blueprints inspect

List the contents of a Blueprint directory or GitHub URL

### Synopsis

Inspect a Blueprint directory (or GitHub repository) and display a detailed
listing of its contents without making any API calls or modifying the project.

Shows all discovered packs (with object and relationship type listings), agents,
skills, and seed data counts. Useful for reviewing what a blueprint contains
before installing it.

Examples:

  memory blueprints inspect ./my-config
  memory blueprints inspect https://github.com/acme/memory-blueprints
  memory blueprints inspect https://github.com/acme/memory-blueprints#v1.2.0

```
memory blueprints inspect <source> [flags]
```

### Options

```
  -h, --help           help for inspect
      --token string   GitHub personal access token (for private repos); also read from MEMORY_GITHUB_TOKEN
```

## memory blueprints install

Apply Blueprints from a directory or GitHub URL

### Synopsis

Apply Blueprints — schemas, agent definitions, skills, and seed data — to the
current project from a structured directory or a GitHub repository URL.

The source directory (or GitHub repo root) may contain:
  schemas/            — one file per memory schema  (.json, .yaml, .yml)
  agents/             — one file per agent definition (.json, .yaml, .yml)
  skills/             — one subdirectory per skill, each containing a SKILL.md file
  seed/objects/       — per-type JSONL files with graph objects to seed
  seed/relationships/ — per-type JSONL files with graph relationships to seed

Skills follow the agentskills.io open standard: each skill is a directory with a
SKILL.md file containing YAML frontmatter (name, description) and Markdown content.

By default the command is additive-only: existing resources are skipped.
Use --upgrade to update resources that already exist.

Examples:

  memory blueprints install ./my-config
  memory blueprints install https://github.com/acme/memory-blueprints
  memory blueprints install https://github.com/acme/memory-blueprints#v1.2.0 --upgrade
  memory blueprints install ./my-config --dry-run

```
memory blueprints install <source> [flags]
```

### Options

```
      --dry-run        Preview actions without making any API calls
  -h, --help           help for install
      --token string   GitHub personal access token (for private repos); also read from MEMORY_GITHUB_TOKEN
      --upgrade        Update existing resources instead of skipping them
```

## memory blueprints validate

Validate a Blueprint directory or GitHub URL without applying it

### Synopsis

Validate a Blueprint directory (or GitHub repository) without making any API
calls or modifying the project.

Checks performed:
  Packs      — required fields, object/relationship type definitions, internal
               cross-references (sourceType/targetType must name an objectType
               in the same pack), duplicate names within and across files.
  Agents     — required fields, enum values (flowType, visibility, dispatchMode),
               model.name required when model block is present, duplicate names.
  Skills     — required fields, non-empty content body, duplicate names.
  Seed data  — required fields, key presence (warning when missing), type
               cross-referenced against loaded pack definitions, srcKey/dstKey
               cross-referenced against seed object keys.

Exits 0 when only warnings are found, exits 1 when any error is found.

Examples:

  memory blueprints validate ./my-config
  memory blueprints validate https://github.com/acme/memory-blueprints
  memory blueprints validate https://github.com/acme/memory-blueprints#v1.2.0

```
memory blueprints validate <source> [flags]
```

### Options

```
  -h, --help           help for validate
      --token string   GitHub personal access token (for private repos); also read from MEMORY_GITHUB_TOKEN
```

## memory browse

Interactive TUI for browsing projects and documents

### Synopsis

Launch an interactive terminal UI (TUI) for browsing projects, documents, and extractions.

The TUI provides:
- Tab-based navigation (Projects, Documents, Worker Stats, Template Packs, Query, Extractions, Traces)
- Natural language query (Ctrl+Q) to ask questions about your project
- Vim-style keybindings (j/k for up/down, Enter to select)
- Search functionality (press / to search)
- Help panel (press ? to toggle)

Minimum terminal size: 80x24

The Traces tab connects to the Grafana Tempo instance that runs alongside the configured
server. The Tempo URL is derived automatically from the server URL (same host, port 3200).
Override with --tempo-url or MEMORY_TEMPO_URL if Tempo runs elsewhere.

```
memory browse [flags]
```

### Options

```
  -h, --help               help for browse
      --tempo-url string   Override Tempo URL (auto-derived from server URL by default)
```

## memory changelog

Show what's new since your current version

### Synopsis

Display the aggregated changelog between your installed version and the latest release.

For dev builds, shows the latest release changelog only.
If you are already on the latest version, a message is shown instead.

```
memory changelog [flags]
```

### Options

```
  -h, --help   help for changelog
```

## memory completion

Generate shell completion scripts

### Synopsis

Generate shell completion scripts for Memory CLI.

The completion script provides:
- Command and subcommand completion
- Flag name completion
- Flag value completion for enum flags (e.g., --output)
- Dynamic resource completion (project names, document IDs)

To load completions:

Bash:
  $ source <(memory completion bash)
  
  # To load completions for each session, execute once:
  # Linux:
  $ memory completion bash > /etc/bash_completion.d/memory
  # macOS:
  $ memory completion bash > $(brew --prefix)/etc/bash_completion.d/memory

Zsh:
  # If shell completion is not already enabled in your environment,
  # you will need to enable it. You can execute the following once:
  $ echo "autoload -U compinit; compinit" >> ~/.zshrc

  # To load completions for each session, execute once:
  $ memory completion zsh > "${fpath[1]}/_memory"

  # You will need to start a new shell for this setup to take effect.

Fish:
  $ memory completion fish | source

  # To load completions for each session, execute once:
  $ memory completion fish > ~/.config/fish/completions/memory.fish

PowerShell:
  PS> memory completion powershell | Out-String | Invoke-Expression

  # To load completions for every new session, run:
  PS> memory completion powershell > memory.ps1
  # and source this file from your PowerShell profile.

Notes:
- Dynamic completions (project names, document IDs) are cached locally for 5 minutes
- Cache location: ~/.memory/cache/
- Completion timeout: 2 seconds (configurable via ~/.memory/config.yaml)

```
memory completion [bash|zsh|fish|powershell]
```

### Options

```
  -h, --help   help for completion
```

## memory config

Manage CLI configuration

### Synopsis

Configure server URL, credentials, and other settings for the Memory CLI

### Options

```
  -h, --help   help for config
```

## memory config set-credentials

Set the email for authentication

### Synopsis

Set the email address used for authentication in the CLI configuration file.

Prints the email that was set and the path to the configuration file where
the setting was saved.

```
memory config set-credentials [email] [flags]
```

### Options

```
      --config string   config file path
  -h, --help            help for set-credentials
```

## memory config set-server

Set the Memory server URL

### Synopsis

Set the Memory server URL in the CLI configuration file.

Prints the new server URL and the path to the configuration file where the
setting was saved. Use this to point the CLI at a different server environment
(e.g. local dev vs production).

```
memory config set-server [url] [flags]
```

### Options

```
      --config string   config file path
  -h, --help            help for set-server
```

## memory config set

Set a configuration value

### Synopsis

Set a configuration value by key.

Supported keys:
  server_url                  Server URL (e.g., http://localhost:3002)
  api_key                     API key for authentication
  email                       Email for authentication
  org_id                      Organization ID
  project_id                  Project ID
  auto_update_enabled         Enable/disable automatic update checks (true/false)
  auto_update_mode            Update mode: notify (default) or auto
  auto_update_check_interval  How often to check for updates (e.g., 24h, 12h)
  google_api_key              Google AI API key (for LLM features)
  openai_base_url             OpenAI-compatible base URL (e.g., http://localhost:11434/v1)
  openai_api_key              OpenAI-compatible API key (optional for local servers)
  llm_model                   LLM model name (e.g., llama3, mistral)

For standalone installations, google_api_key is saved to .env.local.
All other keys are saved to config.yaml.

```
memory config set <key> <value> [flags]
```

### Options

```
      --config string   config file path
  -h, --help            help for set
```

## memory config show

Display current configuration

### Synopsis

Display the current CLI configuration as a table.

Prints a Setting/Value table with: Server URL, API Key (masked, showing only
the first 8 and last 4 characters), Email, Organization ID, Project ID, Debug
mode, and the Config File path. Values are merged from the config file and any
overriding environment variables.

```
memory config show [flags]
```

### Options

```
      --config string   config file path
  -h, --help            help for show
```

## memory documents

Manage project documents

### Synopsis

Commands for managing documents in the Memory platform

### Options

```
  -h, --help             help for documents
      --output string    Output format: table or json (default "table")
      --project string   Project ID (overrides config/env)
```

## memory documents delete

Delete a document

### Synopsis

Delete a document and all related entities.

Prints the deletion status and a summary of removed entities: Chunks,
Extraction jobs, Graph objects, and Graph relationships. Use --output json
for a machine-readable response.

```
memory documents delete <id> [flags]
```

### Options

```
  -h, --help   help for delete
```

## memory documents extraction-summary

Show extraction summary for a document

### Synopsis

Show the extraction summary for the most recently completed extraction
job on a document.

Displays counts of objects created (broken down by type), relationships
created, chunks processed, and the completion timestamp. Use --output json
for a machine-readable response.

```
memory documents extraction-summary <id> [flags]
```

### Options

```
  -h, --help   help for extraction-summary
```

## memory documents get

Get a document by ID

### Synopsis

Get details for a specific document by its ID.

Prints ID, Filename, MIME Type, Size (bytes), Conversion Status, total Chunks,
Embedded Chunks, and Created/Updated timestamps. Use --output json to receive
the full document record as JSON instead.

```
memory documents get <id> [flags]
```

### Options

```
  -h, --help   help for get
```

## memory documents list

List documents

### Synopsis

List documents in the current project.

Output is a table with columns: ID, Filename, MIME Type, Size (bytes), and
Created date. Use --limit to control how many records are returned. Use
--output json to receive the full document list as JSON.

```
memory documents list [flags]
```

### Options

```
  -h, --help        help for list
      --limit int   Maximum number of results (default 50)
```

## memory documents upload

Upload a file as a document

### Synopsis

Upload a local file and create a document record. Use --auto-extract to trigger extraction after upload.

```
memory documents upload <file> [flags]
```

### Options

```
      --auto-extract   Trigger extraction after upload
  -h, --help           help for upload
```

## memory embeddings

Manage embedding workers

### Synopsis

Inspect and control the embedding workers running in the Memory server.

Useful for benchmarking: pause all workers before a bench run so embeddings
don't interfere with write throughput, then resume afterwards.

Examples:
  memory embeddings status            Show current worker state
  memory embeddings pause             Pause all embedding workers
  memory embeddings resume            Resume all embedding workers
  memory embeddings pause --server http://your-server:3002

### Options

```
      --config-path string   path to Memory config.yaml
  -h, --help                 help for embeddings
      --server string        Memory server URL (overrides config)
```

## memory embeddings clear

Delete all pending and processing embedding jobs from both queues

### Synopsis

Delete all pending and processing jobs from the object and relationship
embedding queues. Useful when the queue is stuck or polluted.

Examples:
  memory embeddings clear
  memory embeddings clear --server http://your-server:3002

```
memory embeddings clear [flags]
```

### Options

```
  -h, --help   help for clear
```

## memory embeddings config

Get or set embedding worker config (batch, concurrency, stale-minutes)

### Synopsis

Get or update embedding worker configuration at runtime without restarting.

All flags are optional — omit a flag to leave that value unchanged.
With no flags, shows the current configuration.

Examples:
  memory embeddings config                                  Show current config
  memory embeddings config --batch 200 --concurrency 200   Max throughput
  memory embeddings config --stale-minutes 60              Raise stale threshold
  memory embeddings config --batch 10 --concurrency 10     Throttle down

```
memory embeddings config [flags]
```

### Options

```
      --batch int           Number of jobs to dequeue per poll (0 = no change)
      --concurrency int     Number of jobs processed concurrently per poll (0 = no change)
  -h, --help                help for config
      --interval-ms int     Polling interval in milliseconds (0 = no change)
      --stale-minutes int   Minutes before a processing job is marked stale (0 = no change)
```

## memory embeddings pause

Pause all embedding workers (object, relationship, sweep)

### Synopsis

Pause all embedding workers (objects, relationships, and sweep).

Prints a confirmation message from the server, then displays the updated worker
state table showing each worker's status symbol (running ●, paused ⏸, stopped ○)
and the current Config (batch_size, concurrency, interval_ms, stale_minutes).

```
memory embeddings pause [flags]
```

### Options

```
  -h, --help   help for pause
```

## memory embeddings progress

Show embedding job queue progress (pending, processing, completed, failed)

### Synopsis

Show embedding job queue statistics for all queues.

Displays counts of pending, processing, completed, failed, and dead-letter jobs
for both the graph object and graph relationship embedding queues.

Examples:
  memory embeddings progress
  memory embeddings progress --server http://your-server:3002

```
memory embeddings progress [flags]
```

### Options

```
  -h, --help   help for progress
```

## memory embeddings resume

Resume all embedding workers

### Synopsis

Resume all paused embedding workers (objects, relationships, and sweep).

Prints a confirmation message from the server, then displays the updated worker
state table showing each worker's status symbol (running ●, paused ⏸, stopped ○)
and the current Config (batch_size, concurrency, interval_ms, stale_minutes).

```
memory embeddings resume [flags]
```

### Options

```
  -h, --help   help for resume
```

## memory embeddings status

Show pause/run state of all embedding workers

### Synopsis

Show the current state of all embedding workers.

Prints a worker state table for the objects, relationships, and sweep workers.
Each worker is shown with a symbol indicating its state: running (●), paused (⏸),
or stopped (○). Also displays the current worker Config: batch_size, concurrency,
interval_ms, and stale_minutes.

```
memory embeddings status [flags]
```

### Options

```
  -h, --help   help for status
```

## memory extraction

Manage extraction operations

### Synopsis

Commands for managing extraction jobs and related operations in the Memory platform

### Options

```
  -h, --help             help for extraction
      --output string    Output format: table or json (default "table")
      --project string   Project ID (overrides config/env)
```

## memory extraction jobs

Manage extraction jobs

### Synopsis

Create, monitor, and manage extraction jobs that extract structured entities from documents

### Options

```
  -h, --help   help for jobs
```

## memory extraction jobs cancel

Cancel an extraction job

### Synopsis

Cancel a running or queued extraction job.

Completed or failed jobs cannot be cancelled.

Requires an API key with admin:write scope.

```
memory extraction jobs cancel <job-id> [flags]
```

### Options

```
  -h, --help   help for cancel
```

## memory extraction jobs create

Create a new extraction job

### Synopsis

Create a new extraction job for a document.

Requires --project and --document flags. The extraction job will process the
document and extract structured entities based on any installed schemas.

Requires an API key with admin:write scope.

```
memory extraction jobs create [flags]
```

### Options

```
      --document string   Document ID to extract from (required)
  -h, --help              help for create
```

## memory extraction jobs get

Get extraction job details

### Synopsis

Get detailed information about an extraction job by its ID.

Shows the job status, progress, created/updated timestamps, and any error messages.

Requires an API key with admin:read scope.

```
memory extraction jobs get <job-id> [flags]
```

### Options

```
  -h, --help                help for get
      --interval duration   Poll interval (minimum 1s) (default 3s)
      --quiet               Suppress progress updates
      --timeout duration    Maximum watch duration (default 10m0s)
      --watch               Poll until terminal state (completed/failed/cancelled)
```

## memory extraction jobs list

List extraction jobs

### Synopsis

List extraction jobs for a project.

Use --status to filter by job status (queued, running, completed, failed, cancelled).
Use --document to filter by source document ID.
Use --limit to control the number of results (default 50).

Requires an API key with admin:read scope.

```
memory extraction jobs list [flags]
```

### Options

```
      --document string   Filter by document ID
  -h, --help              help for list
      --limit int         Maximum number of results (default 50)
      --status string     Filter by status (queued, running, completed, failed, cancelled)
```

## memory extraction jobs logs

Get logs for an extraction job

### Synopsis

Get execution logs for an extraction job.

Shows processing steps, entity discoveries, and any errors encountered during extraction.

Requires an API key with admin:read scope.

```
memory extraction jobs logs <job-id> [flags]
```

### Options

```
  -h, --help   help for logs
```

## memory extraction jobs retry

Retry a failed extraction job

### Synopsis

Retry a failed extraction job.

The job will be re-queued for processing with the same configuration.

Requires an API key with admin:write scope.

```
memory extraction jobs retry <job-id> [flags]
```

### Options

```
  -h, --help   help for retry
```

## memory extraction jobs stats

Get extraction job statistics for a project

### Synopsis

Get aggregated statistics for extraction jobs in a project.

Shows counts by status (queued, running, completed, failed, cancelled) and other metrics.

Requires an API key with admin:read scope.

```
memory extraction jobs stats [flags]
```

### Options

```
  -h, --help   help for stats
```

## memory graph

Manage graph objects and relationships

### Synopsis

Commands for managing graph objects and relationships in the Memory knowledge graph

### Options

```
  -h, --help             help for graph
      --output string    Output format: table or json (default "table")
      --project string   Project ID (overrides config/env)
```

## memory graph branches

Manage graph branches

### Synopsis

Manage isolated workspaces (branches) for the knowledge graph.

A branch is a copy of the graph where you can create, update, and delete
objects and relationships without affecting the main graph. Changes stay
isolated until you explicitly merge them.

The main graph has no branch ID. All graph write commands (objects create,
relationships create, etc.) accept --branch <id> to target a branch instead
of the main graph. Without --branch, writes go to the main graph.

Typical workflow:
  1. Create a branch
  2. Write objects/relationships with --branch <id>
  3. Preview the merge (dry run)
  4. Execute the merge into a target branch
  5. Delete the branch

### Options

```
  -h, --help   help for branches
```

## memory graph branches create

Create a new branch

### Synopsis

Create a new branch — an isolated workspace for the knowledge graph.

Objects and relationships written with --branch <id> are visible only on
that branch until merged. The main graph is unaffected until you run
"memory graph branches merge".

--parent is optional metadata recording which branch this was forked from.
It does not affect merge behavior — it is lineage information only.

After creating a branch, capture its ID immediately:

  BRANCH_ID=$(memory graph branches create --name "my-branch" --output json \
    | python3 -c "import json,sys; print(json.load(sys.stdin)['id'])")

Then use it on all graph writes:

  memory graph objects create --type Service --key "svc-x" --branch "$BRANCH_ID"
  memory graph relationships create --type depends_on --from <id> --to <id> --branch "$BRANCH_ID"

Examples:
  memory graph branches create --name "scenario/what-if"
  memory graph branches create --name "feature-x" --project <project-id>
  memory graph branches create --name "child" --parent <parent-branch-id>

```
memory graph branches create [flags]
```

### Options

```
      --description string   Branch description (optional)
  -h, --help                 help for create
      --name string          Branch name (required)
      --parent string        Parent branch ID
```

## memory graph branches delete

Delete a branch

### Synopsis

Delete a branch and all objects that exist only on that branch.

Objects that have already been merged into another branch are unaffected.
This operation is irreversible.

Examples:
  memory graph branches delete <branch-id>

```
memory graph branches delete <id> [flags]
```

### Options

```
  -h, --help   help for delete
```

## memory graph branches fork

Fork a branch with object copies

### Synopsis

Fork a branch — create a new branch and copy all HEAD objects and
relationships from the source into it.

SOURCE: use a branch UUID from "memory graph branches list", or the special
keyword "main" to fork from the main graph (branch_id IS NULL).

Copied objects preserve their canonical IDs so a subsequent merge back
into the source is aware of shared identity. Only HEAD versions are
copied (not full version history).

Use --filter-type to selectively copy only certain object types. When
a filter is applied, relationships where one endpoint was excluded are
silently skipped (the response reports the skipped count).

Examples:
  memory graph branches fork main --name "what-if-scenario"
  memory graph branches fork main --name "subset" --filter-type Service --filter-type API
  memory graph branches fork <source-branch-id> --name "child" --description "child branch"

```
memory graph branches fork <source-branch-id|main> [flags]
```

### Options

```
      --description string    Branch description (optional)
      --filter-type strings   Only copy objects of these types (repeatable)
  -h, --help                  help for fork
      --name string           New branch name (required)
```

## memory graph branches get

Get a branch by ID

### Synopsis

Get details for a branch by its ID.

Use --output json to receive the full object as JSON.

Examples:
  memory graph branches get <branch-id>
  memory graph branches get <branch-id> --output json

```
memory graph branches get <id> [flags]
```

### Options

```
  -h, --help   help for get
```

## memory graph branches list

List graph branches

### Synopsis

List all branches for the current project.

The main graph is not a branch and does not appear in this list. Every
branch shown here has an ID you can pass to --branch on graph write commands,
or as the target/source of a merge.

Use --output json to get full branch details including parent_branch_id and
created_at, which is useful for scripting.

Examples:
  memory graph branches list
  memory graph branches list --project <project-id>
  memory graph branches list --output json

```
memory graph branches list [flags]
```

### Options

```
  -h, --help   help for list
```

## memory graph branches merge

Merge a source branch into a target branch (or main)

### Synopsis

Merge changes from a source branch into a target branch.

DIRECTION: source → target. The source branch is read; the target branch
receives the changes.

TARGET: use a branch UUID from "memory graph branches list", or the special
keyword "main" to merge into the main graph (branch_id IS NULL).

By default this is a DRY RUN — no changes are made. Pass --execute only
when you are ready to apply.

MERGE CLASSIFICATIONS:
  added        — object exists on source only; will be created on target
  fast_forward — object changed on source only; target will be updated
  conflict     — object changed on BOTH branches; merge is BLOCKED
  unchanged    — identical on both branches; nothing to do

If any conflicts exist, --execute is blocked. Resolve conflicts manually
(update the source or target so they agree) then re-run.

The merge runs in a single database transaction — all-or-nothing.

WORKFLOW:
  # 1. Dry run first — always
  memory graph branches merge main --source <source-id>

  # 2. Inspect conflicts in detail
  memory graph branches merge main --source <source-id> --output json

  # 3. Execute when clean
  memory graph branches merge main --source <source-id> --execute

Examples:
  memory graph branches merge main --source <source-id>
  memory graph branches merge main --source <source-id> --execute
  memory graph branches merge <target-id> --source <source-id> --execute
  memory graph branches merge <target-id> --source <source-id> --output json

```
memory graph branches merge <target-branch-id|main> [flags]
```

### Options

```
      --execute         Execute the merge (default is dry run)
  -h, --help            help for merge
      --source string   Source branch ID to merge from (required)
```

## memory graph branches update

Update a branch

### Synopsis

Update a branch's name or description.

Use --name to rename the branch. Use --description to set or update the
description. At least one flag is required.

Examples:
  memory graph branches update <branch-id> --name "new-name"
  memory graph branches update <branch-id> --description "staging area for Q4 planning"
  memory graph branches update <branch-id> --name "new-name" --description "updated purpose"

```
memory graph branches update <id> [flags]
```

### Options

```
      --description string   New branch description
  -h, --help                 help for update
      --name string          New branch name
```

## memory graph events

Stream real-time graph object events via SSE

### Synopsis

Opens a Server-Sent Events stream and prints graph object create/update/delete events as they occur.

```
memory graph events [flags]
```

### Options

```
  -h, --help   help for events
```

## memory graph explore

Explore the knowledge graph visually in the browser

### Synopsis

Start a local web server and open an interactive graph visualizer in your browser.

The visualizer lets you search for nodes, click to expand neighbors, and follow
paths through the knowledge graph. All API calls are proxied through the CLI
process — no credentials are ever sent to the browser.

Examples:
  memory graph explore
  memory graph explore --port 7734
  memory graph explore --host 0.0.0.0 --port 7734
  memory graph explore --project my-project
  memory graph explore --branch my-feature-branch

```
memory graph explore [flags]
```

### Options

```
      --branch string    Branch name or ID to explore (default: main graph)
  -h, --help             help for explore
      --host string      Host/IP to bind (use 0.0.0.0 to listen on all interfaces) (default "127.0.0.1")
      --port int         Local port to listen on (default 7734)
      --project string   Project ID (overrides config/env)
```

## memory graph objects

Manage graph objects

### Options

```
  -h, --help   help for objects
```

## memory graph objects bulk-delete

Bulk delete graph objects matching a filter

### Synopsis

Permanently delete all graph objects matching the given filter.

This is a shorthand for bulk-update --action hard_delete. Use --dry-run to
preview the number of matching objects without deleting any.

Examples:
  memory graph objects bulk-delete --type Message --dry-run
  memory graph objects bulk-delete --type Log --filter "created_at=90d" --filter-op lt --limit 5000

```
memory graph objects bulk-delete [flags]
```

### Options

```
      --dry-run              Preview matched count without deleting
      --filter stringArray   Property filter key=value (repeatable)
      --filter-op string     Operator for --filter: eq, neq, gt, gte, lt, lte, contains, in, exists (default "eq")
  -h, --help                 help for bulk-delete
      --limit int            Maximum number of objects to delete (default 1000, max 100000; 0 = server default)
      --type string          Filter by object type (comma-separated)
```

## memory graph objects bulk-update

Bulk update graph objects matching a filter

### Synopsis

Execute a bulk action on all graph objects matching the given filter.

The --action flag controls what operation is performed. Use --dry-run to preview
the number of matching objects without making any changes.

Supported actions:
  update_status        Set status field (requires --value)
  soft_delete          Set deleted_at timestamp
  hard_delete          Permanently remove objects
  merge_properties     Deep-merge --properties into existing JSONB properties
  replace_properties   Replace the entire properties object
  add_labels           Add labels (--labels)
  remove_labels        Remove labels (--labels)
  set_labels           Replace all labels (--labels)

Examples:
  memory graph objects bulk-update --type Message --action update_status --value archived
  memory graph objects bulk-update --type Document --filter "days_since_access>365" --action soft_delete --dry-run
  memory graph objects bulk-update --action merge_properties --properties '{"verified":true}' --limit 500

```
memory graph objects bulk-update [flags]
```

### Options

```
      --action string        Action to perform: update_status, soft_delete, hard_delete, merge_properties, replace_properties, add_labels, remove_labels, set_labels (required)
      --dry-run              Preview matched count without making changes
      --filter stringArray   Property filter key=value (repeatable)
      --filter-op string     Operator for --filter: eq, neq, gt, gte, lt, lte, contains, in, exists (default "eq")
  -h, --help                 help for bulk-update
      --labels stringArray   Labels for add_labels/remove_labels/set_labels actions
      --limit int            Maximum number of objects to affect (default 1000, max 100000; 0 = server default)
      --properties string    JSON properties for merge_properties/replace_properties actions
      --type string          Filter by object type (comma-separated)
      --value string         Value for update_status action
```

## memory graph objects create-batch

Batch-create graph objects (and optionally relationships) from a JSON file

### Synopsis

Create multiple graph objects in one API call. Accepts two input formats:

FLAT ARRAY FORMAT (objects only):
  A JSON array of objects, each with:
    type        (string, required)
    name        (string, optional) — placed in properties.name
    description (string, optional) — placed in properties.description
    properties  (object, optional) — arbitrary additional properties

  Example:
    [
      {"type": "Person", "name": "Alice"},
      {"type": "Project", "name": "Acme", "properties": {"status": "active"}}
    ]

  Output: one line per object: <entity-id>  <type>  <name>

SUBGRAPH FORMAT (objects + relationships, preferred when wiring is needed):
  A JSON object with "objects" and "relationships" arrays. Objects carry an
  optional "_ref" placeholder; relationships reference objects via "src_ref"/"dst_ref"
  for new objects in the same call, or "src_id"/"dst_id" for pre-existing UUIDs.
  Both can be mixed freely. Max 500 objects, 500 relationships per call — larger
  inputs are auto-chunked with a warning.

  Example (new objects only):
    {
      "objects": [
        {"_ref": "alice", "type": "Person", "key": "person-alice", "name": "Alice"},
        {"_ref": "acme",  "type": "Project", "key": "proj-acme",  "name": "Acme"}
      ],
      "relationships": [
        {"type": "member_of", "src_ref": "alice", "dst_ref": "acme"}
      ]
    }

  Example (mixed: new objects wired to existing UUIDs):
    {
      "objects": [
        {"_ref": "svc", "type": "Service", "name": "auth-service"}
      ],
      "relationships": [
        {"type": "calls_service", "src_id": "<existing-module-uuid>", "dst_ref": "svc"}
      ]
    }

  Output (text): one line per object, then "Created N objects, M relationships"
  Output (--output json): full response including ref_map (placeholder → UUID)

```
memory graph objects create-batch [flags]
```

### Options

```
      --file string   Path to JSON file containing array of objects (required)
  -h, --help          help for create-batch
```

## memory graph objects create

Create a graph object

### Synopsis

Create a new graph object with the given type and optional properties.

When --key is given, the object is keyed for idempotent operations:
  - By default (skip): if an object with that key already exists, the command
    exits successfully without modifying it.
  - With --upsert: if an object with that key already exists, it is updated
    (create-or-update semantics matching blueprint behavior).

```
memory graph objects create [flags]
```

### Options

```
      --branch string        Branch ID to create the object on (omit for main branch)
      --description string   Shorthand for --properties '{"description": "..."}' (subject to schema validation)
  -h, --help                 help for create
      --key string           Stable key for idempotent operations
      --name string          Shorthand for --properties '{"name": "..."}' (subject to schema validation)
      --properties string    JSON properties object
      --status string        Object status (e.g. active, planned, deprecated)
      --type string          Object type (required)
      --upsert               Update existing object if key already exists (requires --key)
```

## memory graph objects delete

Delete a graph object

### Synopsis

Soft-delete a graph object by ID

```
memory graph objects delete <id> [flags]
```

### Options

```
      --branch string   Branch name or ID to scope deletion to (omit for main branch)
  -f, --force           Skip confirmation prompt (accepted for scripting compatibility)
  -h, --help            help for delete
```

## memory graph objects edges

Show edges (relationships) for an object

### Synopsis

Show all incoming and outgoing relationships for a graph object.

Prints two sections: Outgoing (format: [Type] → DstID (entity: EntityID)) and
Incoming (format: [Type] ← SrcID (entity: EntityID)) with counts for each.
Use --output json to receive the full edges response as JSON.

```
memory graph objects edges <id> [flags]
```

### Options

```
  -h, --help   help for edges
```

## memory graph objects get

Get a graph object by ID or key

### Synopsis

Get details for a graph object (entity) by its ID or key.

When a UUID is provided it is fetched directly. When a non-UUID string is
provided it is treated as a key and resolved against the specified branch
(or the main branch when --branch is omitted).

Use --branch to scope key resolution to a specific branch — accepts either
a branch UUID or a human-readable branch name.

Prints Entity ID, Version ID, Type, Version number, Key (if set), Status (if
set), Labels (if any), Created timestamp, and Properties as formatted JSON.
Use --output json to receive the full object as JSON instead.

Examples:
  memory graph objects get <uuid>
  memory graph objects get my-object-key
  memory graph objects get my-object-key --branch plan/next-gen
  memory graph objects get my-object-key --branch <branch-uuid>

```
memory graph objects get <id|key> [flags]
```

### Options

```
      --branch string   Branch ID or name to resolve the key against (omit for main branch)
  -h, --help            help for get
      --json            Output as JSON (shorthand for --output json)
      --output string   Output format: table or json (default "table")
```

## memory graph objects list

List graph objects

### Synopsis

List graph objects (entities) in the current project.

Output is a table with columns: Entity ID, Type, Version, Status, and Created
date. Use --type to filter by object type, --limit to control result count, and
--output json to receive the full list as JSON.

Use --key to filter by the object's stable key field directly. This is the most
efficient way to look up a single object by key without fetching all objects.

Use --filter key=value to filter by object properties (repeatable). All filters
are combined with AND. The --filter-op flag sets the comparison operator for
every --filter in the same invocation (default: eq).

  --filter-op operators: eq, neq, gt, gte, lt, lte, contains, in, exists

Examples:
  memory graph objects list --key sq-soft-delete-employee
  memory graph objects list --branch <branch-id> --key ep-employees-delete
  memory graph objects list --filter status=active
  memory graph objects list --type Feature --filter status=active --filter inertia_tier=1
  memory graph objects list --filter status=active,draft --filter-op in
  memory graph objects list --filter status --filter-op exists

```
memory graph objects list [flags]
```

### Options

```
      --branch string        Branch ID to scope results to (omit for main branch)
      --cursor string        Pagination cursor from a previous response (next_cursor field)
      --filter stringArray   Property filter as key=value (repeatable); see --filter-op
      --filter-op string     Operator for --filter: eq, neq, gt, gte, lt, lte, contains, in, exists (default "eq")
  -h, --help                 help for list
      --ids string           Fetch specific objects by ID (comma-separated: --ids id1,id2,id3)
      --key string           Filter by object key (direct key-based lookup)
      --limit int            Maximum number of results (server default: 1000) (default 1000)
      --status string        Filter by object status
      --type string          Filter by object type
```

## memory graph objects move

Move a graph object to a different branch

### Synopsis

Move a graph object from its current branch to a target branch.

The object's full version chain and any self-referencing relationships are moved.
If the object has relationships connecting to other objects that are not being
moved, the operation fails — move related objects first or delete the relationships.

Use --target-branch to specify the destination branch UUID. Use "main" or omit
the flag to move to the main branch.

Examples:
  memory graph objects move <entity-id> --target-branch <branch-uuid>
  memory graph objects move <entity-id> --target-branch main

```
memory graph objects move <id> [flags]
```

### Options

```
  -h, --help                   help for move
      --output string          Output format: table or json (default "table")
      --target-branch string   Target branch UUID (use 'main' or omit for main branch)
```

## memory graph objects similar

Find objects similar to a given object by embedding

### Synopsis

Find graph objects similar to the given object using cosine similarity on stored embeddings.

Returns a ranked list with similarity scores. Use --limit to control result count,
--type to filter by object type, and --min-score to exclude low-confidence results.
Use --output json to receive the full response as JSON.

Examples:
  memory graph objects similar <entity-id>
  memory graph objects similar <entity-id> --limit 20 --type Feature
  memory graph objects similar <entity-id> --min-score 0.75 --output json

```
memory graph objects similar <id> [flags]
```

### Options

```
  -h, --help              help for similar
      --limit int         Maximum number of similar objects to return (default 10)
      --min-score float   Minimum similarity score (0–1); 0 means no threshold
      --output string     Output format: table or json (default "table")
      --type string       Filter results by object type
```

## memory graph objects update-batch

Batch-update graph objects from a JSON file

### Synopsis

Update multiple graph objects in one API call.

The input file must contain a JSON array of objects, each with:
  id         (string, required) — object entity ID to update
  key        (string, optional)
  properties (object, optional) — merged with existing properties
  labels     ([]string, optional) — appended (or replaced with replaceLabels)
  replaceLabels (bool, optional) — replace labels instead of appending
  status     (string, optional)
  branch_id  (string, optional)

Example updates.json:
  [
    {"id": "<entity-id-1>", "properties": {"priority": "high"}},
    {"id": "<entity-id-2>", "status": "archived", "labels": ["deprecated"]}
  ]

Output (one line per object): <entity-id>  <type>  <version>

```
memory graph objects update-batch [flags]
```

### Options

```
      --file string     Path to JSON file containing array of object updates (required)
  -h, --help            help for update-batch
      --output string   Output format: table or json (default "table")
```

## memory graph objects update

Update a graph object

### Synopsis

Update a graph object's properties or status (creates a new version)

```
memory graph objects update <id> [flags]
```

### Options

```
      --branch string        Branch ID to update the object on (omit for main branch)
      --description string   Shorthand for --properties '{"description": "..."}' (subject to schema validation)
  -h, --help                 help for update
      --key string           Set a stable key on the object (enables cross-session src_key/dst_key references)
      --name string          Shorthand for --properties '{"name": "..."}' (subject to schema validation)
      --properties string    JSON properties object to merge
      --status string        Set object status (e.g. active, planned, deprecated)
```

## memory graph relationships

Manage graph relationships

### Options

```
  -h, --help   help for relationships
```

## memory graph relationships create-batch

Batch-create graph relationships from a JSON file

### Synopsis

Create multiple graph relationships in one API call.

The input file must contain a JSON array of objects, each with:
  type  (string, required) — relationship type
  from  (string, required) — source entity ID
  to    (string, required) — destination entity ID
  properties (object, optional)

Use --upsert to make the operation idempotent: existing relationships (same
type, from, to) are returned as-is instead of causing an error. Safe to retry.

Example relationships.json:
  [
    {"type": "knows", "from": "<entity-id-1>", "to": "<entity-id-2>"},
    {"type": "manages", "from": "<entity-id-3>", "to": "<entity-id-4>"}
  ]

Output (one line per relationship): <entity-id>  <type>  <from> -> <to>

```
memory graph relationships create-batch [flags]
```

### Options

```
      --file string   Path to JSON file containing array of relationships (required)
  -h, --help          help for create-batch
      --upsert        Apply upsert semantics to all items: skip existing relationships instead of failing
```

## memory graph relationships create

Create a relationship

### Synopsis

Create a directed relationship between two graph objects.

When --upsert is given, the operation is idempotent: if a relationship with the
same type, source, and destination already exists, it is returned as-is without
creating a duplicate. If the relationship was deleted, it is restored.
Without --upsert, creating a relationship that already exists returns an error.

```
memory graph relationships create [flags]
```

### Options

```
      --branch string       Branch ID to create the relationship on (omit for main branch)
      --from string         Source object ID (required)
  -h, --help                help for create
      --properties string   JSON properties object
      --to string           Destination object ID (required)
      --type string         Relationship type (required)
      --upsert              Return existing relationship if (type, from, to) already exists instead of creating a duplicate
```

## memory graph relationships delete

Delete a relationship

### Synopsis

Soft-delete a graph relationship by ID.

Use --branch to scope the deletion to a specific branch (name or UUID).
Without --branch the relationship is deleted from the main graph.

Examples:
  memory graph relationships delete <id>
  memory graph relationships delete <id> --branch plan/next-gen
  memory graph relationships delete <id> --branch <branch-uuid>

```
memory graph relationships delete <id> [flags]
```

### Options

```
      --branch string   Branch name or ID to scope deletion to (omit for main branch)
  -h, --help            help for delete
```

## memory graph relationships get

Get a relationship by ID

### Synopsis

Get details for a graph relationship by its ID.

Prints Entity ID, Version ID, Type, From (source entity ID), To (destination
entity ID), Version number, Created timestamp, and Properties as formatted
JSON. Use --output json to receive the full relationship as JSON instead.

```
memory graph relationships get <id> [flags]
```

### Options

```
  -h, --help   help for get
```

## memory graph relationships list

List relationships

### Synopsis

List relationships in the current project.

Output is a table with columns: Entity ID, Type, From (source entity ID), To
(destination entity ID), and Created date. Use --type to filter by relationship
type, --from/--to to filter by endpoint, --limit to control result count,
--cursor to paginate through large result sets, and --output json to receive
the full list as JSON.

```
memory graph relationships list [flags]
```

### Options

```
      --branch string   Branch ID to scope results to (omit for main branch)
      --cursor string   Pagination cursor from a previous response (next_cursor field)
      --from string     Filter by source object ID
  -h, --help            help for list
      --limit int       Maximum number of results (server default: 1000) (default 1000)
      --to string       Filter by destination object ID
      --type string     Filter by relationship type
```

## memory init

Initialize a Memory project in the current directory

### Synopsis

Interactive wizard that sets up a Memory project in the current directory.

Walks through:
  1. Project selection or creation
  2. LLM provider configuration (org-level)
  3. Memory skills installation for AI agents

Writes MEMORY_PROJECT_ID, MEMORY_PROJECT_NAME, and MEMORY_PROJECT_TOKEN
to .env.local and auto-adds .env.local to .gitignore.

Running 'memory init' again detects existing configuration and offers
to verify or reconfigure each step.

Use --skip-provider or --skip-skills to skip individual steps.

```
memory init [flags]
```

### Options

```
  -h, --help            help for init
      --skip-provider   skip LLM provider configuration step
      --skip-skills     skip Memory skills installation step
```

## memory install-memory-skills

Install Memory skills to .agents/skills/

### Synopsis

Install the built-in Memory skills from the embedded catalog into
.agents/skills/ in the current directory (or the directory specified by --dir).

Only skills with the "memory-" prefix are installed. This is the set of skills
that teach AI agents how to use the Memory CLI and platform.

By default the command skips skills that are already up to date. Skills whose
content has changed since they were last installed are reported as outdated —
use --force to overwrite them with the latest version.

Use --global to also install into all known global agent skill directories that
already exist on disk (e.g. ~/.opencode/skills, ~/.claude/skills, ~/.gemini/skills).

After installing, any "memory-" prefixed skill directories in the target that
are no longer present in the catalog are considered stale. Use --prune to
remove them automatically, or run interactively to be prompted for each one.

```
memory install-memory-skills [flags]
```

### Options

```
      --dir string   target directory (default: .agents/skills relative to cwd)
      --force        overwrite existing skill directories
      --global       also install into known global agent skill dirs (e.g. ~/.opencode/skills, ~/.claude/skills)
  -h, --help         help for install-memory-skills
      --prune        remove stale memory-* skill directories not present in the catalog
```

## memory journal

View and annotate the project journal

### Options

```
  -h, --help   help for journal
```

## memory journal list

List project journal entries

### Synopsis

List recent graph mutations and notes for the current project.

Output is a log-style feed showing each event with timestamp, actor, event type,
and relevant details. Notes are printed inline with entries or at the end (for
standalone notes).

Use --since to filter by age (e.g. 7d, 24h, 1h). Defaults to last 7 days.
Use --limit to control the maximum number of entries returned.
Use --output json for machine-readable output.

```
memory journal list [flags]
```

### Options

```
      --branch string      Branch name or UUID (omit for main branch)
  -h, --help               help for list
      --include-branches   Include merged branches in the feed alongside the main branch
      --limit int          Maximum number of entries to return (default 100)
      --since string       Show entries from the last duration (e.g. 7d, 24h, 1h) (default "7d")
```

## memory journal note

Add a note to the project journal

### Synopsis

Add a markdown note to the project journal.

The note body can be passed as an argument, piped via stdin, or entered
interactively in your $EDITOR when no argument or stdin is provided.

Use --entry <journal-entry-id> to attach the note to a specific journal entry.

Examples:
  memory journal note "Skipped worker services — need schema clarification first."
  echo "Some context" | memory journal note
  memory journal note --entry <entry-id> "Removed legacy auth service"

```
memory journal note [text] [flags]
```

### Options

```
      --entry string   Journal entry ID to attach the note to
  -h, --help           help for note
```

## memory login

Sign in or create a Memory account

### Synopsis

Authenticate using the OAuth Device Authorization flow.

Opens your browser so you can sign in or create a new account.
Your credentials are saved locally for future CLI use.

If this server is running in standalone mode, use an API key instead:
  memory config set-api-key <key>

```
memory login [flags]
```

### Options

```
  -h, --help   help for login
```

## memory logout

Clear stored credentials

### Synopsis

Remove locally stored OAuth credentials and log out from the Memory platform.

Before deleting local credentials, attempts to revoke tokens server-side via
the OIDC revocation endpoint. Revocation is best-effort — if it fails, local
credentials are still removed.

Use --all to also clear api_key and project_token from your config file,
removing all locally stored authentication state.

```
memory logout [flags]
```

### Options

```
      --all    Also clear api_key and project_token from config
  -h, --help   help for logout
```

## memory mcp-guide

Show MCP configuration for AI agents

### Synopsis

Print ready-to-use MCP server configuration snippets for connecting AI agents to Memory.

Outputs JSON configuration blocks for Claude Desktop, Cursor, and other MCP-
compatible clients. Snippets use the active server URL and API key (project
token takes precedence over account key). Copy the relevant block into your
AI client's MCP configuration to enable Memory tools.

```
memory mcp-guide [flags]
```

### Options

```
  -h, --help   help for mcp-guide
```

## memory mcp-relay

Inspect and interact with connected MCP relay instances

### Options

```
  -h, --help   help for mcp-relay
```

## memory mcp-relay call

Invoke a tool on a connected relay instance

```
memory mcp-relay call <instance-id> <tool-name> [flags]
```

### Options

```
      --args string   Tool arguments as a JSON object, e.g. '{"key":"value"}'
  -h, --help          help for call
```

## memory mcp-relay sessions

List connected MCP relay instances for the current project

```
memory mcp-relay sessions [flags]
```

### Options

```
  -h, --help   help for sessions
```

## memory mcp-relay tools

List tools exposed by a connected relay instance

```
memory mcp-relay tools <instance-id> [flags]
```

### Options

```
  -h, --help   help for tools
```

## memory orgs

Manage organizations

### Synopsis

Commands for managing organizations in the Memory platform

### Options

```
  -h, --help   help for orgs
```

## memory orgs create

Create a new organization

### Synopsis

Create a new organization in the Memory platform.

Prints the new organization's Name and ID on success.

```
memory orgs create [flags]
```

### Options

```
  -h, --help          help for create
      --name string   Organization name (required)
```

## memory orgs delete

Delete an organization

### Synopsis

Permanently delete an organization by ID.

```
memory orgs delete <id> [flags]
```

### Options

```
  -h, --help   help for delete
```

## memory orgs get

Get organization details

### Synopsis

Get details for a specific organization by ID.

```
memory orgs get <id> [flags]
```

### Options

```
  -h, --help   help for get
```

## memory orgs list

List all organizations

### Synopsis

List all organizations you are a member of.

Output prints a numbered list with each organization's Name and ID.

```
memory orgs list [flags]
```

### Options

```
  -h, --help      help for list
      --members   Include members with roles
```

## memory projects

Manage projects

### Synopsis

Commands for managing projects in the Memory platform

### Options

```
  -h, --help   help for projects
```

## memory projects create-token

Create a new API token for a project

### Synopsis

Create a new project-scoped API token (emt_...) and print it.

The token is also written to .env.local in the current directory as
MEMORY_PROJECT_TOKEN so subsequent CLI commands pick it up automatically.

Scopes default to: data:read data:write schema:read agents:read agents:write

Example:
  memory projects create-token my-project --name onboard-token

```
memory projects create-token [project-name-or-id] [flags]
```

### Options

```
  -h, --help             help for create-token
      --name string      Token name (default "cli-token")
      --no-env           Do not write token to .env.local
      --scopes strings   Token scopes (default: data:read,data:write,schema:read,agents:read,agents:write)
```

## memory projects create

Create a new project

### Synopsis

Create a new project in the Memory platform.

Prints the new project's Name and ID on success. If no LLM provider credentials
are configured for the organization, a warning is shown explaining that AI
features (embeddings, search, extraction) will not work until a provider is
added via 'memory provider configure'.

```
memory projects create [flags]
```

### Options

```
      --description string   Project description
  -h, --help                 help for create
      --name string          Project name (required)
      --org-id string        Organization ID (auto-detected if not specified)
```

## memory projects delete

Delete a project

### Synopsis

Permanently delete a project and all its data

```
memory projects delete [project-id] [flags]
```

### Options

```
  -h, --help   help for delete
```

## memory projects get

Get project details

### Synopsis

Get details for a specific project by name or ID.

Prints the project's Name, ID, and Org ID. If a project info document is set
it is shown as well. Use the --stats flag to additionally display counts for
Documents, Graph Objects, Relationships, Extraction jobs, and installed Schemas
with their object and relationship type names.

```
memory projects get [name-or-id] [flags]
```

### Options

```
  -h, --help    help for get
      --stats   Include project statistics (documents, objects, jobs, schemas)
```

## memory projects list

List all projects

### Synopsis

List all projects you have access to.

Output prints a numbered list with each project's Name and ID. If the project
has a project info document set, it is shown beneath the name. Use the --stats
flag to also display per-project counts for Documents, Graph Objects,
Relationships, Extraction jobs (total/running/queued), and installed Schemas
(with their object and relationship type names).

```
memory projects list [flags]
```

### Options

```
      --filter string   Filter results (e.g., 'name=MyProject,status=active')
  -h, --help            help for list
      --limit int       Maximum number of results (default from config)
      --members         Include project members with roles
      --offset int      Number of results to skip
      --search string   Search projects by name or description
      --sort string     Sort results (e.g., 'name:asc' or 'updated_at:desc')
      --stats           Include project statistics (documents, objects, jobs, schemas)
```

## memory projects set-budget

Set a monthly spend budget for a project

### Synopsis

Set or clear the monthly spend budget for a project.

When the project's estimated spend for the current month exceeds
budget_usd * budget_alert_threshold (default 0.8), an in-app notification
is sent to all org members. Set --budget 0 to clear an existing budget.

Examples:
  memory projects set-budget my-project --budget 50
  memory projects set-budget my-project --budget 100 --threshold 0.9
  memory projects set-budget --budget 25

```
memory projects set-budget [project-name-or-id] [flags]
```

### Options

```
      --budget float      Monthly budget in USD (set to 0 to clear)
  -h, --help              help for set-budget
      --threshold float   Alert threshold as a fraction of budget (e.g. 0.8 = 80%) (default 0.8)
```

## memory projects set-info

Set the project info document

### Synopsis

Set the project info document — a Markdown description of this project's
purpose, goals, audience, and context. Agents and MCP clients read this via the
get_project_info tool to orient themselves before working with the project's data.

Provide content via --file (read a .md file) or --text (inline string).
If no project is specified, the active project from config/env is used.

Examples:
  memory projects set-info --file README.md
  memory projects set-info my-project --file docs/project-info.md
  memory projects set-info --text "This project tracks internal HR documents."

```
memory projects set-info [project-name-or-id] [flags]
```

### Options

```
      --file string   Path to a Markdown file to use as project info
  -h, --help          help for set-info
      --text string   Inline project info text
```

## memory projects set-provider

Configure the LLM provider for a project

### Synopsis

Configure the LLM provider credentials for a specific project.

Supported providers: google, google-vertex. Prints the provider name, the
configured generative model, and the embedding model on success. Use flags
such as --api-key, --embedding-model, and --generative-model to specify
credentials and model overrides.

```
memory projects set-provider [project-name-or-id] <provider> [flags]
```

### Options

```
      --api-key string            Google AI API key (for google)
      --embedding-model string    Override embedding model for this project
      --gcp-project string        GCP project ID (for google-vertex)
      --generative-model string   Override generative model for this project
  -h, --help                      help for set-provider
      --location string           GCP region (for google-vertex)
      --sa-file string            Path to Vertex AI service account JSON (for google-vertex)
```

## memory projects set

Set active project

### Synopsis

Set the active project context.

Updates project_id in ~/.memory/config.yaml and writes MEMORY_PROJECT_ID,
MEMORY_PROJECT_NAME, and MEMORY_PROJECT_TOKEN into .env.local in the current
directory so that subsequent CLI commands and application code automatically use
the selected project. If no existing token is found for the project, a new one
is created automatically. Run without arguments to select interactively from a
numbered list of available projects.

Use --clear to remove the active project from the global config.

```
memory projects set [name-or-id] [flags]
```

### Options

```
      --clear   Clear the active project from config
  -h, --help    help for set
```

## memory projects share-mcp

Share read-only MCP access with a teammate or AI agent

### Synopsis

Generate a read-only API token for your project and print ready-to-use
MCP config snippets for Claude Desktop, Cursor, and OpenCode.

The token is scoped to data:read, schema:read, agents:read, and projects:read.
Write operations will be rejected. The token can be revoked at any time with:

  memory tokens revoke <token-id> --project <project>

Examples:
  memory projects share-mcp --project my-project
  memory projects share-mcp --project my-project --name "Alice read-only"
  memory projects share-mcp --project my-project --email alice@example.com --email bob@example.com

```
memory projects share-mcp [flags]
```

### Options

```
      --email stringArray   Email address to invite (can be repeated)
  -h, --help                help for share-mcp
      --json                Output raw JSON response
      --name string         Display name for the token (default: auto-generated)
      --project string      Project name or ID (required if not set in config)
```

## memory projects team

Manage project team members

### Synopsis

Commands for listing, inviting, and removing members of a project

### Options

```
  -h, --help   help for team
```

## memory projects team accept

Accept a pending invitation

### Synopsis

Accept a pending invitation using the token from the invitation email.

```
memory projects team accept <token> [flags]
```

### Options

```
  -h, --help   help for accept
```

## memory projects team invite

Invite someone to the project

### Synopsis

Send an email invitation to join the project.

The invited user will receive an email with CLI install instructions and an
accept link. Use --role to control the access level (default: project_viewer).

Roles:
  project_viewer  Read-only access (cannot modify data)
  project_user    Read-write access
  project_admin   Full admin access

```
memory projects team invite <email> [project-name-or-id] [flags]
```

### Options

```
  -h, --help          help for invite
      --role string   Role to assign (project_viewer, project_user, project_admin) (default "project_viewer")
```

## memory projects team invites

List pending invitations

### Synopsis

List all pending invitations sent to your account.

```
memory projects team invites [flags]
```

### Options

```
  -h, --help   help for invites
      --json   Output as JSON
```

## memory projects team list

List project team members

### Synopsis

List all members of a project with their roles and join dates. Use --stats for last-active info.

```
memory projects team list [project-name-or-id] [flags]
```

### Options

```
  -h, --help    help for list
      --json    Output as JSON
      --stats   Show last-active stats per member
```

## memory projects team remove

Remove a member from the project

### Synopsis

Remove a user from the project by email. Use --yes to skip confirmation.

```
memory projects team remove <email> [project-name-or-id] [flags]
```

### Options

```
  -h, --help   help for remove
  -y, --yes    Skip confirmation prompt
```

## memory provider

Manage LLM provider credentials and models

### Synopsis

Commands for managing LLM provider credentials, model selections, and usage reporting.

### Options

```
  -h, --help   help for provider
```

## memory provider configure-project

Save project-level LLM provider credentials (overrides org config)

### Synopsis

Save project-specific credentials and model selections for the given provider.
This overrides the organization's provider config for this project.

Use --remove to remove the project-level override and fall back to the org config.

Supported providers:
  google            — Google AI (Gemini API); requires --api-key
  google-vertex     — Google Cloud Vertex AI; requires --gcp-project, --location
  openai-compatible — OpenAI-compatible API (Ollama, vLLM, etc.); requires --api-key, --base-url, --generative-model
  deepseek          — DeepSeek AI models; requires --api-key

The project is read from --project or the MEMORY_PROJECT_ID environment variable.

Examples:
  memory provider configure-project google --api-key AIzaSy...
  memory provider configure-project google-vertex --gcp-project my-proj --location us-central1 --key-file sa.json
  memory provider configure-project openai-compatible --api-key sk-... --base-url http://localhost:11434/v1 --generative-model llama3
  memory provider configure-project deepseek --api-key sk-... --generative-model deepseek-v4-flash
  memory provider configure-project google --remove

```
memory provider configure-project <provider> [flags]
```

### Options

```
      --api-key string            API key (required for google)
      --base-url string           OpenAI-compatible base URL (required for openai-compatible)
      --embedding-model string    Embedding model to use (auto-selected from catalog if omitted)
      --gcp-project string        GCP project ID (required for google-vertex)
      --generative-model string   Generative model to use (auto-selected from catalog if omitted)
  -h, --help                      help for configure-project
      --key-file string           Path to service account JSON key file (google-vertex)
      --location string           GCP region, e.g. us-central1 (required for google-vertex)
      --project string            Project ID (auto-detected from MEMORY_PROJECT_ID)
      --remove                    Remove the project-level override and inherit org config
```

## memory provider configure

Save LLM provider credentials and model selections for the organization

### Synopsis

Save LLM provider credentials (and optionally model selections) for the
current organization. Runs a live credential test and syncs the model catalog
on success. Models are auto-selected from the catalog if not specified.

Supported providers:
  google            — Google AI (Gemini API); requires --api-key
  google-vertex     — Google Cloud Vertex AI; requires --gcp-project, --location
  openai-compatible — OpenAI-compatible API (Ollama, vLLM, etc.); requires --api-key, --base-url, --generative-model
  deepseek          — DeepSeek AI models; requires --api-key

Examples:
  memory provider configure google --api-key AIzaSy...
  memory provider configure google-vertex --gcp-project my-project --location us-central1 --key-file sa.json
  memory provider configure openai-compatible --api-key sk-... --base-url http://localhost:11434/v1 --generative-model llama3
  memory provider configure google --api-key AIzaSy... --generative-model gemini-2.5-flash --embedding-model text-embedding-004
  memory provider configure deepseek --api-key sk-...
  memory provider configure deepseek --api-key sk-... --generative-model deepseek-v4-flash

```
memory provider configure <provider> [flags]
```

### Options

```
      --api-key string            API key (required for google)
      --base-url string           OpenAI-compatible base URL (required for openai-compatible)
      --embedding-model string    Embedding model to use (auto-selected from catalog if omitted)
      --gcp-project string        GCP project ID (required for google-vertex)
      --generative-model string   Generative model to use (auto-selected from catalog if omitted)
  -h, --help                      help for configure
      --key-file string           Path to service account JSON key file (google-vertex)
      --location string           GCP region, e.g. us-central1 (required for google-vertex)
      --org-id string             Organization ID (auto-detected from config)
```

## memory provider list

Show current provider configurations

### Synopsis

List all configured LLM providers at the organization level, plus any
project-level overrides across all projects in the organization.

The output is a table with columns: SCOPE, PROVIDER, GENERATIVE MODEL,
EMBEDDING MODEL, GCP PROJECT, LOCATION, and UPDATED.

Use --project to filter results to a single project (name or ID).
If multiple projects share the same name, an error is returned with all matching IDs.

Examples:
  memory provider list
  memory provider list --org-id <id>
  memory provider list --project my-project
  memory provider list --json

```
memory provider list [flags]
```

### Options

```
  -h, --help             help for list
      --json             Output raw JSON
      --org-id string    Organization ID (auto-detected from config)
      --project string   Filter to a specific project (name or ID)
```

## memory provider models

List available models from the provider catalog

### Synopsis

List models available in the cached model catalog.

Without a provider argument, lists models for all configured providers.
Pass a provider name to filter to a single provider.

Use --type to filter by model type (embedding or generative).

Examples:
  memory provider models
  memory provider models openai-compatible
  memory provider models google-vertex
  memory provider models google --type generative
  memory provider models deepseek

```
memory provider models [provider] [flags]
```

### Options

```
  -h, --help            help for models
      --org-id string   Organization ID (auto-detected from config)
      --type string     Filter by model type: embedding or generative
```

## memory provider test

Test LLM provider credentials with a live generate call

### Synopsis

Send a live "say hello" generate call to verify that provider credentials
work end-to-end.

Without a provider argument, tests all configured providers.
Pass a provider name (google, google-vertex, openai-compatible, or deepseek) to test a specific one.

Use --project to test using the project-level credential hierarchy
(project override → org) instead of org credentials only.

Examples:
  memory provider test
  memory provider test openai-compatible
  memory provider test deepseek
  memory provider test google-vertex
  memory provider test google --project <id>

```
memory provider test [provider] [flags]
```

### Options

```
  -h, --help             help for test
      --org-id string    Organization ID (auto-detected from config)
      --project string   Project ID for project-level credential resolution
```

## memory provider timeseries

Show LLM usage over time

### Synopsis

Show LLM token usage and estimated cost broken down by time period.

Without --project, reports org-wide usage. With --project, reports usage for
that specific project. Use --granularity to control bucket size (default: day).

Output is a table with columns: PERIOD, PROVIDER, MODEL, TEXT IN, IMAGE, VIDEO,
AUDIO, OUTPUT, and EST. COST (USD). A running subtotal is shown per period.

Examples:
  memory provider timeseries
  memory provider timeseries --project <id> --granularity week
  memory provider timeseries --since 2024-01-01 --until 2024-03-31 --granularity month

```
memory provider timeseries [flags]
```

### Options

```
      --granularity string   Time bucket size: day, week, or month (default "day")
  -h, --help                 help for timeseries
      --json                 Output raw JSON
      --org-id string        Organization ID (auto-detected from config)
      --project string       Filter to a specific project ID
      --since string         Start date (YYYY-MM-DD)
      --until string         End date (YYYY-MM-DD)
```

## memory provider usage

Show LLM usage and estimated cost

### Synopsis

Show aggregated LLM token usage and estimated cost.

Without --project, reports org-wide usage across all projects.
With --project, reports usage for that specific project.

Use --by-project to break org-wide totals down per project instead of per model.

Output is a table with columns: PROVIDER, MODEL, TEXT IN (tokens), IMAGE
(tokens), VIDEO (tokens), AUDIO (tokens), OUTPUT (tokens), and EST. COST (USD).
A total estimated cost line is printed below the table.

Examples:
  memory provider usage
  memory provider usage --project <id>
  memory provider usage --since 2024-01-01
  memory provider usage --by-project

```
memory provider usage [flags]
```

### Options

```
      --by-project       Break down org usage by project instead of by model
  -h, --help             help for usage
      --json             Output raw JSON
      --org-id string    Organization ID (auto-detected from config)
      --project string   Filter usage to a specific project ID
      --since string     Start date for usage window (YYYY-MM-DD)
      --until string     End date for usage window (YYYY-MM-DD)
```

## memory query

Query a project using natural language

### Synopsis

Query a project using natural language.

By default, uses the graph-query-agent — an AI agent that reasons over the knowledge
graph using search, traversal, and entity tools. The agent is managed server-side;
no agent ID is needed.

Use --mode=search for direct hybrid search without AI reasoning.

--branch is only supported in --mode=search. It scopes the search to a specific
branch of the knowledge graph. Without --branch, the main graph is searched.
To search a branch, first find its ID with "memory graph branches list".

Examples:
  memory query "what are the main services and how do they relate?"
  memory query --mode=search "auth service"
  memory query --mode=search --branch <branch-id> "planned services"
  memory query --project abc123 "list all requirements"

```
memory query <question> [flags]
```

### Options

```
      --access-boost float32        Boost score by access recency (0 = disabled; try 0.5-2.0) (search mode only)
      --branch string               Branch ID to search (search mode only; omit to search the main graph)
      --debug                       Include debug information in output
      --fusion-strategy string      Fusion strategy: weighted, rrf, interleave, graph_first, text_first (search mode only) (default "weighted")
  -h, --help                        help for query
      --json                        Output results as JSON
      --limit int                   Maximum number of results to return (search mode only) (default 10)
      --mode string                 Query mode: agent (default, AI reasoning) or search (direct hybrid search) (default "agent")
      --project string              Project ID to query (uses default project if not specified)
      --recency-boost float32       Boost score by recency (0 = disabled; try 0.5-2.0) (search mode only)
      --recency-half-life float32   Half-life in hours for recency decay (default 168 = 7 days) (search mode only) (default 168)
      --result-types string         Types of results: graph, text, or both (search mode only) (default "both")
      --session string              Continue a previous query session (use the session ID printed after a query)
      --show-scores                 Show relevance scores for each result (search mode only)
      --show-time                   Show elapsed query time
      --show-tools                  Show tool calls made by the agent (agent mode only)
```

## memory remember

Store information in the knowledge graph using natural language

### Synopsis

Store information in the knowledge graph using natural language.

The graph-insert-agent understands your input, checks for existing entities to avoid
duplicates, creates a branch, writes structured data (entities + relationships), and
merges back to the main graph — all automatically.

Schema policy controls what happens when no matching entity type exists:
  auto        Create new schema types as needed (default)
  reuse_only  Never create new types; use the closest existing type
  ask         Prompt before creating any new type (requires interactive terminal)

Examples:
  memory remember "I have to buy toilet paper at Lidl"
  memory remember "Meeting with Sarah tomorrow at 3pm about the Q3 roadmap"
  memory remember "The API server is deployed on aws-eu-west-1, running Go 1.22"
  memory remember --schema-policy reuse_only "Task: fix login bug, priority high"
  memory remember --dry-run "Note: team offsite on 15 June in Berlin"
  memory remember --project abc123 "remember to call dentist next week"

```
memory remember <text> [flags]
```

### Options

```
      --dry-run                Create branch and write data but do not merge to main
  -h, --help                   help for remember
      --json                   Output results as JSON
      --project string         Project ID (uses default project if not specified)
      --schema-policy string   Schema creation policy: auto, reuse_only, ask (default "auto")
      --session string         Continue a previous remember session (use session ID printed after a run)
      --show-time              Show elapsed time
      --show-tools             Show tool calls made by the agent
```

## memory schemas

Manage schemas

### Synopsis

Commands for managing schemas in the Memory platform

### Options

```
  -h, --help             help for schemas
      --output string    Output format: table or json (default "table")
      --project string   Project ID (overrides config/env)
```

## memory schemas compiled-types

Show compiled object and relationship types for the current project

### Synopsis

Show the merged set of type definitions compiled from all installed schemas.

Prints two tables: Object Types (columns: Name, Label, Schema, Description)
and Relationship Types (columns: Name, Label, Source → Target, Schema). Use
--output json to receive the raw compiled types as JSON.

With --verbose, additional columns show the schema version and whether a type
is shadowed (overridden) by a higher-priority schema. Shadowed types also emit
a warning to stderr.

```
memory schemas compiled-types [flags]
```

### Options

```
  -h, --help      help for compiled-types
      --verbose   Include schema version and shadowed status in output
```

## memory schemas create

Create a schema from a JSON or YAML file

### Synopsis

Create a new schema by loading its definition from a JSON or YAML file

```
memory schemas create [flags]
```

### Options

```
      --file string   Path to schema JSON file (required)
  -h, --help          help for create
```

## memory schemas delete

Delete a schema from the registry

### Synopsis

Permanently delete a schema definition from the global registry

```
memory schemas delete <schema-id> [flags]
```

### Options

```
  -h, --help   help for delete
```

## memory schemas diff

Diff two schemas — registry vs registry, or registry vs local file

### Synopsis

Compare two schema definitions and show type-level and property-level differences.

Two modes:

  diff <schema-id> --file <path>
      Compare a schema in the registry against a local JSON/YAML file.
      The local file is treated as the "incoming" (new) version.

  diff <schema-id-a> <schema-id-b>
      Compare two schemas already stored in the registry.
      Schema A is treated as the "before" (base) version and schema B as the "after" version.

Output shows added (+), removed (-) types, and property-level changes for shared
types. A suggested migrations YAML block is printed when removals are detected.
Use --output json for machine-readable output.

```
memory schemas diff <schema-id-a> [<schema-id-b>] [--file <path>] [flags]
```

### Options

```
      --file string   Path to incoming schema file (JSON, YAML, or YML) for registry-vs-file mode
  -h, --help          help for diff
```

## memory schemas get

Get a schema by ID

### Synopsis

Get details for a schema pack by its ID.

Prints ID, Name, Version, Description (if set), Author (if set), Draft status,
and Created timestamp. Use --output json to receive the full schema record as
JSON instead.

```
memory schemas get <schema-id> [flags]
```

### Options

```
  -h, --help   help for get
```

## memory schemas history

Show installation history for schemas on the current project

### Synopsis

Show the full installation history for schemas assigned to the current project,
including schemas that have since been removed (soft-deleted).

Output is a table with columns: Assignment ID, Schema ID, Name, Version,
Installed, Removed. Use --output json to receive the full list as JSON.

```
memory schemas history [flags]
```

### Options

```
  -h, --help   help for history
```

## memory schemas install

Install a schema into the current project

### Synopsis

Install a schema into the current project.

	Two modes:
  install <schema-id>          Install an existing schema from the registry by ID.
  install --file schema.json   Create a new schema from a JSON or YAML file and install it in one step.

```
memory schemas install [<schema-id>] [flags]
```

### Options

```
      --auto-uninstall   Uninstall the from-version schema after successful migration chain
      --dry-run          Preview what would be installed without making changes
      --file string      Create schema from JSON file and install in one step
      --force            Force migration even if risk level is dangerous
  -h, --help             help for install
      --merge            Additively merge incoming type schemas into existing registered types
```

## memory schemas installed

List installed schemas

### Synopsis

List schemas currently installed (assigned) on the current project.

Output is a table with columns: Assignment ID, Schema ID, Name, Version,
Active (yes/no), and Installed date. The Assignment ID is used with
'memory schemas uninstall' to remove a schema from the project. Use
--output json to receive the full list as JSON.

```
memory schemas installed [flags]
```

### Options

```
  -h, --help   help for installed
```

## memory schemas list

List schemas installed on this project

### Synopsis

List schemas installed on the current project (default), or schemas available
in the registry to install (--available).

Schemas installed via 'memory blueprints install' appear here under 'installed'.
Use 'memory schemas compiled-types' to see the full merged set of active types.

Output is a table. Use --output json to receive the full list as JSON.

```
memory schemas list [flags]
```

### Options

```
      --available   Show schemas available in the registry (not yet installed) instead of installed schemas
  -h, --help        help for list
```

## memory schemas migrate

Migrate live graph data by renaming types or properties

### Synopsis

Rename object types or properties across live graph objects and edges.

Use --rename-type OldName:NewName to rename an object or edge type.
Use --rename-property OldType.old_key:OldType.new_key to rename a property within a type.
Both flags are repeatable. At least one rename must be provided.

Use --dry-run to preview how many objects/edges would be affected without making changes.

```
memory schemas migrate [flags]
```

### Options

```
      --dry-run                       Preview migration without making changes
  -h, --help                          help for migrate
      --rename-property stringArray   Rename a property: OldType.old_key:OldType.new_key (repeatable)
      --rename-type stringArray       Rename a type: OldName:NewName (repeatable)
```

## memory schemas migrate commit

Commit (prune) migration archive data through a given schema version

### Synopsis

Remove migration_archive entries from all project objects whose to_version is
<= the given through_version. Once committed, rollback to those versions is no
longer possible.

This is an explicit user action — run after a migration has been stable for
some time. The assignment itself is unaffected.

```
memory schemas migrate commit [flags]
```

### Options

```
  -h, --help                     help for commit
      --through-version string   Prune archive entries at or below this version (required)
```

## memory schemas migrate execute

Execute a schema migration (System A: full field-level migration with archive)

### Synopsis

Migrate live graph objects from one schema version to another using System A
(SchemaMigrator). Applies type renames, property renames, and archives dropped
properties for rollback.

Use --force to bypass the dangerous-risk block. Use --max-objects to limit how
many objects are migrated (useful for staged rollouts).

A confirmation prompt is shown for risky or dangerous migrations unless --force
is set.

```
memory schemas migrate execute [flags]
```

### Options

```
      --force             Force migration even if risk level is dangerous
      --from string       Source schema ID (required)
  -h, --help              help for execute
      --max-objects int   Limit number of objects to migrate (0 = no limit)
      --to string         Target schema ID (required)
```

## memory schemas migrate job

Check the status of an async schema migration job

### Synopsis

Poll the status of a background schema migration job.

Use --job-id to specify the job ID (returned by 'memory schemas install' when
auto-migration is triggered).

Use --wait to block until the job completes, streaming progress updates every
2 seconds.

```
memory schemas migrate job [flags]
```

### Options

```
  -h, --help            help for job
      --job-id string   Migration job ID to poll (required)
      --wait            Block until job completes, streaming progress updates
```

## memory schemas migrate preview

Preview a schema migration (risk assessment, no data changes)

### Synopsis

Preview the risk of migrating live graph objects from one schema version to another.

Returns a risk breakdown per object type (safe/cautious/risky/dangerous) and a
total object count, but makes no data changes.

Use --from and --to to specify the source and target schema IDs.

```
memory schemas migrate preview [flags]
```

### Options

```
      --from string   Source schema ID (required)
  -h, --help          help for preview
      --to string     Target schema ID (required)
```

## memory schemas migrate rollback

Roll back a schema migration by restoring archived property data

### Synopsis

Restore property data archived during a previous migration.

Use --to-version to specify which schema version to roll back to.
Use --restore-registry to also restore the type registry to the pre-migration state
(re-installs the old schema types and removes new additions). This is a transactional
operation — if any step fails, the entire rollback is reverted.

```
memory schemas migrate rollback [flags]
```

### Options

```
  -h, --help                help for rollback
      --restore-registry    Also restore type registry to the pre-migration state
      --to-version string   Schema version to roll back to (required)
```

## memory schemas uninstall

Uninstall (remove) a schema assignment from the current project

### Synopsis

Remove a schema assignment from the current project.

Single mode: provide the assignment ID to remove one schema.
Bulk mode: use --all-except or --keep-latest to remove multiple schemas.

Use 'memory schemas installed' to list assignment IDs.

```
memory schemas uninstall [<assignment-id>] [flags]
```

### Options

```
      --all-except string   Remove all assignments except the comma-separated IDs (assignment IDs or schema IDs)
      --dry-run             Preview what would be removed without making changes
  -h, --help                help for uninstall
      --keep-latest         Remove all but the most-recently installed assignment per unique schema
```

## memory schemas validate

Check project objects for schema version mismatches

### Synopsis

Identify objects whose stored schema version is out of date with the
currently installed schemas in the project.

Returns a list of entity IDs, their types, keys, and the specific issues
(e.g., version mismatch). Exits with code 1 if any stale objects are found.

```
memory schemas validate [flags]
```

### Options

```
  -h, --help   help for validate
```

## memory server

Manage a self-hosted Memory server

### Synopsis

Commands for installing, running, and maintaining a self-hosted Memory server.

### Options

```
  -h, --help   help for server
```

## memory server ctl

Control Memory services

### Synopsis

Control and manage Memory standalone services.

This command provides service management capabilities similar to memory-ctl:
  - start/stop/restart services
  - view service status and logs
  - check server health
  - open shell in server container

Examples:
  memory ctl start
  memory ctl stop
  memory ctl status
  memory ctl logs -f
  memory ctl logs server
  memory ctl health

### Options

```
      --dir string   Installation directory (default "/root/.memory")
  -h, --help         help for ctl
```

## memory server ctl health

Check server health

### Synopsis

Check the health of the local Memory server.

Makes an HTTP GET request to the server's /health endpoint and prints whether
the server is healthy (✓) or not responding (✗). On a healthy response the
full JSON health payload is printed in indented format.

```
memory server ctl health [flags]
```

### Options

```
  -h, --help   help for health
```

## memory server ctl logs

Show service logs

### Synopsis

Show logs from Memory services.

Examples:
  memory ctl logs           # Show recent logs from all services
  memory ctl logs -f        # Follow logs in real-time
  memory ctl logs server    # Show logs from server only
  memory ctl logs -n 50     # Show last 50 lines

```
memory server ctl logs [service] [flags]
```

### Options

```
  -f, --follow      Follow log output
  -h, --help        help for logs
  -n, --lines int   Number of lines to show (default 100)
```

## memory server ctl pull

Pull latest Docker images

```
memory server ctl pull [flags]
```

### Options

```
  -h, --help   help for pull
```

## memory server ctl restart

Restart all services

```
memory server ctl restart [flags]
```

### Options

```
  -h, --help   help for restart
```

## memory server ctl shell

Open shell in server container

```
memory server ctl shell [flags]
```

### Options

```
  -h, --help   help for shell
```

## memory server ctl start

Start all services

```
memory server ctl start [flags]
```

### Options

```
  -h, --help   help for start
```

## memory server ctl status

Show service status

### Synopsis

Show the current status of all Memory Docker services.

Runs 'docker compose ps' for the local installation and prints the container
name, state (running/exited), and port mappings for each service.

```
memory server ctl status [flags]
```

### Options

```
  -h, --help   help for status
```

## memory server ctl stop

Stop all services

```
memory server ctl stop [flags]
```

### Options

```
  -h, --help   help for stop
```

## memory server doctor

Check system health and configuration

### Synopsis

Run diagnostic checks on your Memory CLI installation.

This command verifies:
- Configuration file exists and is valid
- Server connectivity
- Authentication status
- API functionality
- Docker container health (for standalone installations)

Use --fix to automatically repair common issues.

```
memory server doctor [flags]
```

### Options

```
      --debug   Show detailed debug information (copyable for bug reports)
      --fix     Attempt to automatically fix detected issues
  -h, --help    help for doctor
```

## memory server install

Install Memory standalone server

### Synopsis

Install Memory standalone server with all required components.

This command will:
  - Check Docker and Docker Compose are installed
  - Create installation directory (~/.memory by default)
  - Generate secure configuration (API keys, passwords)
  - Write Docker Compose configuration
  - Pull and start Docker containers
  - Configure the CLI to connect to the local server

Example:
  memory install
  memory install --port 8080 --google-api-key YOUR_KEY
  memory install --open-api-base-url http://localhost:11434/v1 --llm-model llama3
  memory install --dir /opt/memory --skip-start

```
memory server install [flags]
```

### Options

```
      --dir string                 Installation directory (default "/root/.memory")
      --force                      Overwrite existing installation
      --google-api-key string      Google API key for embeddings
  -h, --help                       help for install
      --llm-model string           LLM model name (for OpenAI-compatible)
      --open-api-base-url string   OpenAI-compatible base URL
      --port int                   Server port (default 3002)
      --skip-start                 Generate config but don't start services
```

## memory server uninstall

Remove Memory installation

### Synopsis

Remove Memory standalone server installation.

This command will:
  - Stop and remove Docker containers
  - Remove Docker volumes (unless --keep-data is specified)
  - Remove installation directory

Example:
  memory uninstall
  memory uninstall --keep-data
  memory uninstall --force

```
memory server uninstall [flags]
```

### Options

```
      --dir string   Installation directory (default "/root/.memory")
      --force        Skip confirmation prompt
  -h, --help         help for uninstall
      --keep-data    Keep Docker volumes (preserve data)
```

## memory server upgrade

Upgrade the standalone server installation

### Synopsis

Upgrades the Memory standalone server installation.

This will:
  - Pull the latest Docker images
  - Restart services with the new images
  - Preserve all existing configuration and data

Examples:
  memory server upgrade
  memory server upgrade --dir ~/.memory

```
memory server upgrade [flags]
```

### Options

```
      --dir string   Installation directory (default "/root/.memory")
  -f, --force        Force upgrade without confirmation
  -h, --help         help for upgrade
```

## memory sessions

Manage AI agent sessions and messages

### Options

```
  -h, --help             help for sessions
      --project string   Project name or ID
```

## memory sessions create

Create a new session

### Synopsis

Creates a new Session graph object to track an AI agent conversation.

```
memory sessions create [flags]
```

### Options

```
      --agent-version string   Optional agent version
  -h, --help                   help for create
      --summary string         Optional session summary
      --title string           Session title (required)
```

## memory sessions get

Get a session by ID

```
memory sessions get [session-id] [flags]
```

### Options

```
  -h, --help   help for get
```

## memory sessions list

List sessions in the project

```
memory sessions list [flags]
```

### Options

```
      --cursor string   Pagination cursor
  -h, --help            help for list
      --limit int       Max sessions to return (default 20)
```

## memory sessions messages

Manage messages in a session

### Options

```
  -h, --help   help for messages
```

## memory sessions messages add

Append a message to a session

```
memory sessions messages add [session-id] [flags]
```

### Options

```
      --content string   Message content (required)
  -h, --help             help for add
      --role string      Message role: user|assistant|system (required)
      --tokens int       Token count (optional)
```

## memory sessions messages list

List messages in a session

```
memory sessions messages list [session-id] [flags]
```

### Options

```
      --cursor string   Pagination cursor
  -h, --help            help for list
      --limit int       Max messages to return (default 50)
```

## memory sessions recount

Recount message_count and total_tokens for a session

### Synopsis

Scans all messages for a session and updates message_count and total_tokens on the Session object. Useful for backfilling existing sessions.

```
memory sessions recount <session-id> [flags]
```

### Options

```
  -h, --help   help for recount
```

## memory sessions spawn

Spawn a child session from a parent

### Synopsis

Creates a new child session linked to the parent via a spawned_from relationship.

When --fork-context is set, the parent's message history is copied into the child
as a snapshot at spawn time. The child then operates independently — no live sync.

```
memory sessions spawn <parent-session-id> [flags]
```

### Options

```
      --fork-context       Copy parent message history into child session
  -h, --help               help for spawn
      --max-messages int   Max parent messages to copy when --fork-context is set (default 50)
      --summary string     Optional session summary
      --title string       Child session title (required)
```

## memory set-token

Save a static Bearer token as CLI credentials

### Synopsis

Save a static Bearer token to ~/.memory/credentials.json.

Useful in CI, test harnesses, and dev environments where a token is
pre-issued rather than obtained via the OAuth device flow.

Example:
  memory auth set-token e2e-test-user

```
memory set-token <bearer-token> [flags]
```

### Options

```
      --duration string   Token validity duration (default 24h, e.g. 48h, 168h)
  -h, --help              help for set-token
```

## memory skills

Manage skills

### Synopsis

Commands for managing skills — reusable Markdown workflow instructions for agents

### Options

```
      --global           Use global scope (built-in skills only, superadmin)
  -h, --help             help for skills
      --json             Output as JSON
      --org string       Organization ID (creates/lists org-scoped skill)
      --project string   Project ID (creates/lists project-scoped skill)
```

## memory skills create

Create a skill

### Synopsis

Create a new skill. Use --project to create a project-scoped skill, or omit for global.

```
memory skills create [flags]
```

### Options

```
      --content string        Skill content (Markdown)
      --content-file string   Path to a file containing the skill content
      --description string    Skill description (required)
  -h, --help                  help for create
      --name string           Skill name (slug, e.g. 'my-skill') (required)
```

## memory skills delete

Delete a skill by ID

### Synopsis

Permanently delete a skill by its ID.

Prints "Skill <id> deleted." on success. You will be prompted for confirmation
unless the --confirm flag is provided.

```
memory skills delete [id] [flags]
```

### Options

```
      --confirm   Skip confirmation prompt
  -h, --help      help for delete
```

## memory skills get

Get a skill by ID

### Synopsis

Get full details for a skill by its ID.

Prints ID, Name, Description, Scope (global / org / project), Created and
Updated timestamps, and the full skill Content. Use --json to receive the raw
JSON response instead.

```
memory skills get [id] [flags]
```

### Options

```
  -h, --help   help for get
```

## memory skills import

Import skills from a SKILL.md file or directory

### Synopsis

Import one or more skills and register them on the server so agents can use them.

Import a single SKILL.md file:
  memory skills import path/to/SKILL.md

Import all skills found in a directory (scans one level deep for SKILL.md files):
  memory skills import --from-dir .agents/skills/

Auto-discover skills from well-known locations (.agents/skills/, ~/.claude/skills/, etc.):
  memory skills import --discover

Import all discovered skills without prompting:
  memory skills import --discover --all

Import built-in Memory skills from the embedded catalog:
  memory skills import --builtin

Import built-in skills including experimental ones:
  memory skills import --builtin --experimental

```
memory skills import [path] [flags]
```

### Options

```
      --all               Import all found skills without prompting
      --builtin           Import from the built-in embedded Memory skill catalog
      --discover          Auto-discover skills from well-known locations (.agents/skills/, ~/.claude/skills/, etc.)
      --experimental      Include experimental skills when importing from the built-in catalog (--builtin)
      --from-dir string   Scan a directory for SKILL.md files and import all found skills
  -h, --help              help for import
```

## memory skills list

List skills installed on the server

### Synopsis

List skills stored on the server and available to agents.

Output is a table with columns: NAME, DESCRIPTION (truncated to 55 characters),
SCOPE (global/org/project), and ID. Use --project to include project-scoped
skills, or --global for global-only skills. Use --json to receive the full
skill list as JSON.

```
memory skills list [flags]
```

### Options

```
  -h, --help        help for list
      --limit int   Maximum number of skills to show (0 = all)
      --page int    Page number (1-based, used with --limit) (default 1)
```

## memory skills update

Update a skill

### Synopsis

Update the description or content of an existing skill.

Prints "Skill updated." followed by the skill's ID and Name on success. At
least one of --description, --content, or --content-file must be provided.
Use --json to receive the full updated skill as JSON instead.

```
memory skills update [id] [flags]
```

### Options

```
      --content string        New content (Markdown)
      --content-file string   Path to file with new content
      --description string    New description
  -h, --help                  help for update
```

## memory status

Show current authentication status

### Synopsis

Display detailed information about the current authentication session and server health.

Shows authentication Mode (project token, account API key, or OAuth), Server URL,
masked credential key, and connection Status (Connected or unreachable). Also
displays server Health and Version. If authenticated as a user, prints the active
project Name and ID, or a numbered list of all accessible projects.

Additionally prints full Usage Statistics for the active project including:
Documents, Graph Objects, Relationships, Type Registry (Types, Enabled,
TypesWithObjects), Template Packs, and Processing Pipeline job queue depths.

```
memory status [flags]
```

### Options

```
  -h, --help   help for status
```

## memory tokens

Manage API tokens

### Synopsis

Commands for managing API tokens (emt_* keys). Tokens can be account-level (cross-project) or project-scoped.

### Options

```
  -h, --help             help for tokens
      --project string   Project name or ID (omit for account-level tokens)
```

## memory tokens cleanup

Bulk-revoke account tokens by name prefix

### Synopsis

Revoke all account-level tokens whose names start with a given prefix.

Useful for cleaning up stale tokens left by e2e tests or automated tooling.
Prompts for confirmation before revoking unless --force is passed.

Example:
  memory tokens cleanup --name-prefix "e2e-"
  memory tokens cleanup --name-prefix "cli-test-" --force

```
memory tokens cleanup [flags]
```

### Options

```
      --force                Skip confirmation prompt
  -h, --help                 help for cleanup
      --name-prefix string   Revoke tokens whose names start with this prefix (required)
```

## memory tokens create

Create a new API token

### Synopsis

Create a new API token.

Without --project, creates an account-level token usable across all projects.
With --project, creates a project-scoped token.

On success, prints the full plaintext Token value prominently (this is the only
time the full token is shown — save it immediately), followed by ID, Name, Type,
Prefix, Scopes, and Created timestamp.

Valid scopes: schema:read, data:read, data:write, agents:read, agents:write, projects:read, projects:write

```
memory tokens create [flags]
```

### Options

```
  -h, --help            help for create
      --name string     Token name (required)
      --scopes string   Comma-separated scopes (default: data:read). Valid: schema:read, data:read, data:write, agents:read, agents:write, projects:read, projects:write
```

## memory tokens get

Get token details

### Synopsis

Get details for a specific API token by its ID.

Use --project to specify a project-scoped token; without it, looks up an
account-level token.

```
memory tokens get [token-id] [flags]
```

### Options

```
  -h, --help   help for get
```

## memory tokens list

List API tokens

### Synopsis

List API tokens and their details.

Without --project, lists account-level tokens. With --project, lists tokens
for the specified project. Each token entry prints: Name, ID, Prefix, Type
(account or project), Scopes, Created timestamp, and Revoked timestamp (if
applicable). For project tokens, the full plaintext token value is also fetched
and displayed — treat this output as sensitive.

```
memory tokens list [flags]
```

### Options

```
  -h, --help                 help for list
      --limit int            Maximum number of tokens to show (0 = all)
      --name-prefix string   Filter tokens by name prefix (account-level only)
      --page int             Page number (1-based, used with --limit) (default 1)
```

## memory tokens revoke

Revoke an API token

### Synopsis

Permanently revoke an API token, making it unusable. Without --project, revokes an account-level token.

```
memory tokens revoke [token-id] [flags]
```

### Options

```
  -h, --help   help for revoke
```

## memory traces

Query traces

### Synopsis

Query OpenTelemetry traces via the server's built-in Tempo proxy.

Traces are proxied through the configured --server endpoint so no direct
access to Tempo is required.

### Options

```
  -h, --help   help for traces
```

## memory traces get

Fetch a full trace by ID

### Synopsis

Fetch and display a full trace as an indented span tree.

Prints the Trace ID, then each span in a tree structure showing the span name
and duration. If an agent run ID is found in the trace attributes, also prints
a token usage summary (Input Tokens, Output Tokens, Estimated Cost). Use
--debug to include internal ADK bookkeeping spans in the tree.

```
memory traces get <traceID> [flags]
```

### Options

```
      --debug   Show all spans including internal ADK bookkeeping spans (e.g. merged tool responses)
  -h, --help    help for get
```

## memory traces list

List recent traces

### Synopsis

List recent traces (default: last 1 hour, up to 20 results).

Output is a table with columns: TRACE ID, ROOT SPAN, DURATION, and TIMESTAMP.
When the --agent-runs flag is used, the table additionally includes: AGENT,
INPUT TOKENS, OUTPUT TOKENS, and EST. COST columns, and results are filtered
to traces with agent.run root spans only. Use --since (e.g. 30m, 2h, 24h) and
--limit to control the time window and result count.

```
memory traces list [flags]
```

### Options

```
      --agent-runs     Filter to agent.run root spans and show token/cost columns
  -h, --help           help for list
      --limit int      Maximum number of traces to return (default 20)
      --since string   Show traces from the last duration (e.g. 30m, 2h, 24h) (default "1h")
```

## memory traces search

Search traces by criteria

### Synopsis

Search traces using TraceQL filters.

Outputs the same table format as 'memory traces list': TRACE ID, ROOT SPAN,
DURATION, TIMESTAMP. Use --service, --route, --min-duration, --since, and
--limit flags to narrow results. The query is scoped to the active project
automatically.

```
memory traces search [flags]
```

### Options

```
  -h, --help                  help for search
      --limit int             Maximum number of results (default 20)
      --min-duration string   Filter by minimum duration (e.g. 200ms, 1s)
      --route string          Filter by HTTP route (e.g. /api/kb/documents)
      --service string        Filter by service name
      --since string          Search within the last duration (e.g. 30m, 2h, 24h) (default "1h")
```

## memory upgrade

Upgrade the Memory CLI binary

### Synopsis

Upgrades the Memory CLI binary to the latest release.

Downloads the latest CLI binary from GitHub and replaces the current binary
in-place. Does not touch a self-hosted server installation — use
'memory server upgrade' to upgrade the server.

Examples:
  memory upgrade            # Upgrade the CLI binary
  memory upgrade --force    # Upgrade even when running a dev build

```
memory upgrade [flags]
```

### Options

```
      --dir string   Installation directory (unused for CLI-only upgrade) (default "/root/.memory")
  -f, --force        Force upgrade even for dev versions
  -h, --help         help for upgrade
```

## memory version

Show version information

### Synopsis

Display the version, commit hash, and build date of the Memory CLI

```
memory version [flags]
```

### Options

```
  -h, --help   help for version
```

