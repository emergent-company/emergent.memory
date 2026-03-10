# AI News Blueprint

An Emergent blueprint that automatically gathers fresh AI news across 6 topic categories and delivers a concise morning digest every day.

## What It Does

- **6 research agents** run daily, one per AI topic category, discovering and storing news items in the knowledge graph
- **1 digest agent** synthesizes the day's most interesting items into a 300-500 word morning summary
- All news items are stored as typed graph objects for search, linking, and historical reference

## Categories

| Category | Object Type | Focus |
|---|---|---|
| Research Papers | `AIResearchPaper` | arxiv, preprints, academic AI releases |
| Model Releases | `AIModelRelease` | new model drops, benchmarks, provider announcements |
| AI Tools & Products | `AIToolRelease` | new tools, APIs, products, integrations |
| Industry & Business | `AIIndustryEvent` | funding, acquisitions, partnerships, policy |
| Ethics & Safety | `AIEthicsUpdate` | alignment research, incidents, regulation |
| Applications & Demos | `AIApplicationDemo` | impressive demos, novel use-cases, creative AI |

## Prerequisites

### 1. Web Search MCP Server

The research agents require a web search MCP tool. Configure one of the following providers in your project before applying this blueprint.

#### Option A: Brave Search (recommended — cheapest)

Register a Brave Search MCP server in your project:

```bash
memory mcp-servers create \
  --name "brave-search" \
  --url "https://api.search.brave.com/mcp" \
  --project <your-project-id>
```

Or use the Brave Search MCP server URL with your API key configured as an environment variable (`BRAVE_API_KEY`).

#### Option B: Exa (best semantic quality)

```bash
memory mcp-servers create \
  --name "exa-search" \
  --url "https://mcp.exa.ai/mcp" \
  --project <your-project-id>
```

The built-in `brave_web_search` tool is available if you have configured a Brave API key in your project's provider settings.

### 2. LLM Provider

Ensure your project has an LLM provider configured (Gemini, OpenAI, or Anthropic). The agents use `gemini-2.5-flash` by default — update the YAML files if you want a different model.

## Installation

```bash
memory blueprints blueprints/ai-news/ --project <your-project-id>
```

This will:
1. Install the **AI News** template pack (6 object types, 3 relationship types)
2. Create **7 agent definitions** (6 research agents + 1 digest agent)
3. Create **30 seed search query objects** (5 per category)

## Setting Up Daily Schedule

After applying the blueprint, configure each agent to run on a daily schedule via the admin UI or CLI:

```bash
# List the created agent definitions
memory agent-definitions list --project <your-project-id>

# For each research agent, set a daily cron schedule
# Research agents: run between 06:00–07:00 UTC
memory agents create \
  --definition ai-research-papers \
  --cron "0 6 * * *" \
  --project <your-project-id>

memory agents create \
  --definition ai-model-releases \
  --cron "10 6 * * *" \
  --project <your-project-id>

memory agents create \
  --definition ai-tools-products \
  --cron "20 6 * * *" \
  --project <your-project-id>

memory agents create \
  --definition ai-industry-news \
  --cron "30 6 * * *" \
  --project <your-project-id>

memory agents create \
  --definition ai-ethics-safety \
  --cron "40 6 * * *" \
  --project <your-project-id>

memory agents create \
  --definition ai-applications-demos \
  --cron "50 6 * * *" \
  --project <your-project-id>

# Digest agent: run at 07:30 UTC (after all research agents complete)
memory agents create \
  --definition ai-morning-digest \
  --cron "30 7 * * *" \
  --project <your-project-id>
```

**Recommended run order**: Research agents first (06:00–07:00 UTC), digest agent last (07:30 UTC). This gives the research agents time to populate the graph before the digest reads it.

## First Run

To test without waiting for the scheduled time, trigger agents manually:

```bash
# Trigger a research agent
memory agents trigger <agent-id> --project <your-project-id>

# After research agents finish, trigger the digest
memory agents trigger <digest-agent-id> --project <your-project-id>
```

## Viewing the Morning Digest

Past digests are stored as graph objects labeled `daily-digest`. Query them:

```bash
memory graph objects list --label daily-digest --project <your-project-id>
```

Or ask your project's chat agent: *"Show me today's AI news digest."*

## Tuning

- **Agent system prompts**: Edit the YAML files in `blueprints/ai-news/agents/` and re-apply the blueprint to update
- **Search queries**: Modify the seed JSONL files and re-apply; existing objects with matching keys will be updated
- **Model selection**: Change the `model.name` field in each agent YAML file

## Rollback

To remove the blueprint:

```bash
# Delete agent definitions
memory agent-definitions delete ai-research-papers --project <your-project-id>
# (repeat for each agent)

# Uninstall template pack
memory template-packs uninstall "AI News" --project <your-project-id>
```
