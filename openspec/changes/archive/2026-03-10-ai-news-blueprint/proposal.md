## Why

AI is evolving faster than any single person can track. This blueprint creates an automated system that continuously gathers fresh information across 5-8 AI topic categories, stores it in the Emergent knowledge graph, and delivers a curated morning digest — so users wake up to a concise, interesting summary of what's exciting in AI without having to hunt for it.

## What Changes

- New `ai-news-feed` blueprint directory providing template packs, agent definitions, and seed data
- 6 specialized AI topic categories as distinct object types in the knowledge graph
- 7 research agents (one per category + one orchestrator) with web search tool access
- 1 daily digest agent that synthesizes top items across all categories and delivers a formatted morning summary
- Agent definitions wired to run on a daily schedule (morning)
- Blueprint seed data for initial topic categories and search query templates

## Capabilities

### New Capabilities

- `ai-news-template-pack`: Template pack defining AI News object types (AINewsItem, AIResearchPaper, AIToolRelease, AIModelUpdate, AIIndustryEvent, AIEthicsUpdate) with appropriate relationship types (COVERS_TOPIC, AUTHORED_BY, RELATES_TO, SUPERSEDES)
- `ai-news-research-agents`: Six category-focused research agents (research-papers, model-releases, ai-tools, industry-news, ethics-safety, applications-demos) each equipped with web-search MCP tools to discover and store fresh AI news items daily
- `ai-news-digest-agent`: Orchestrator agent that reads the day's gathered items from the knowledge graph, selects the most interesting/significant ones across categories, and renders a concise morning digest in an engaging format
- `ai-news-blueprint`: Top-level blueprint directory that installs the template pack, registers all agent definitions, and provides seed data for initial configuration

### Modified Capabilities

## Impact

- New blueprint directory: `blueprints/ai-news/` (template packs, agent definitions YAML, seed data)
- Uses existing `agent-definitions` and `agent-execution` infrastructure
- Uses existing `mcp-server-hosting` and `external-mcp-connections` for web search tools
- Uses existing `template-packs` infrastructure for object type definitions
- Uses existing `graph-api` and `graph-search` for storing and querying news items
- Requires a web search MCP server (e.g., Brave Search, Tavily, or Exa) to be configured
- Daily schedule via existing agent scheduler infrastructure
