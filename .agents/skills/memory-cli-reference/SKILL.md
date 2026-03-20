---
name: memory-cli-reference
description: Command index for Memory CLI setup and config groups: provider, config, login, server, projects, documents. Use as a last resort when no dedicated skill covers the task.
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

## memory agents

Manage runtime agents (create, trigger, list runs, respond to questions). **Use the `memory-agents` skill** for the full workflow. Key subcommands: `agents list`, `agents trigger <id>`, `agents runs <id>`, `agents get-run <run-id>`, `agents questions list-project`. Run `memory agents --help` for flags.

## memory ask

Natural language interface to the Memory CLI assistant — can answer questions and execute tasks. Usage: `memory ask "<question>"`. Flags: `--show-tools`, `--json`, `--v2`. Useful as a fallback when you're unsure which command to use.

## memory blueprints

Apply declarative seed data (objects, relationships, agents, schemas) from a directory or GitHub URL. **Use the `memory-blueprints` skill** for the full workflow. Key flags: `--dry-run`, `--upgrade`. Subcommand: `blueprints dump <dir>` to export. Run `memory blueprints --help` for flags.

## memory config

Manage CLI configuration (server URL, credentials). Key subcommands: `config show`, `config set <key> <value>` (keys: server_url, api_key, email, project_id). Run `memory config --help` for flags.

## memory documents

Upload and manage documents for extraction. Key subcommands: `documents upload <file> [--auto-extract]`, `documents list`, `documents get <id>`, `documents delete <id>`. Run `memory documents --help` for flags.

## memory graph

Create, update, and delete graph objects and relationships. **Use the `memory-graph` skill** for the full workflow, batch patterns, and examples.

## memory init

Interactive wizard to connect a directory to a Memory project and install skills. Use the `memory-onboard` skill for agent-driven setup instead.

## memory login / logout

Authenticate via OAuth device flow (`memory login`) or clear credentials (`memory logout`). Not needed in agent contexts where a project token or API key is already configured.

## memory projects

Manage Memory projects. Key subcommands: `projects list`, `projects create --name <name>`, `projects get <id>`, `projects set-info --file <md>`. Run `memory projects --help` for flags.

## memory provider

Configure LLM provider credentials at the org level. Key subcommands: `provider list`, `provider configure google-ai --api-key <key>`, `provider configure vertex-ai --key-file <path> --gcp-project <id> --location <region>`, `provider test <provider>`. Run `memory provider --help` for flags.

## memory query

Natural language and hybrid search over the knowledge graph. **Use the `memory-query` skill** for modes, flags, and examples.

## memory schemas

Install, list, and manage object/relationship type schemas in a project. **Use the `memory-schemas` skill** for the full workflow. Key subcommands: `schemas list`, `schemas installed`, `schemas install --file pack.json`, `schemas compiled-types`, `schemas uninstall <id>`. Run `memory schemas --help` for flags.

## memory server

Install and manage a self-hosted Memory server. Not relevant in cloud/managed deployments. Run `memory server --help` for subcommands.
