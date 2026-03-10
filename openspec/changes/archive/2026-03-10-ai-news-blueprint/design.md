## Context

The Emergent platform has mature infrastructure for agents, template packs, blueprints, and graph storage. This design uses those building blocks to create a zero-maintenance AI news system: specialized agents scrape and classify the latest AI news into category-specific knowledge objects each day, and a digest agent synthesizes the best items into a clean morning summary.

The system is delivered entirely as a blueprint — a directory of YAML agent definitions, a template pack JSON, and seed JSONL data. Users apply it with `memory blueprints` and it self-configures. No new backend code is needed.

## Goals / Non-Goals

**Goals:**
- Define 6 AI topic categories as object types in a dedicated template pack
- Define 7 agents (6 category researchers + 1 digest writer) in blueprint YAML
- Specify the graph schema (object types, relationships) to hold news items
- Specify the digest agent's prompt structure and output format
- Specify how agents are triggered (daily cron)
- Provide seed data: initial search queries per category

**Non-Goals:**
- Building a new web crawler or scraping backend — agents use MCP web-search tools (Brave/Exa/Tavily must be externally configured)
- Modifying the existing agent execution engine or scheduler — use as-is
- A UI for browsing news — the morning summary is the interface (chat or notification)
- Storing full article text — only summaries + metadata + URLs

## Decisions

### Decision: Blueprint-only delivery (no backend code changes)

The entire feature is a blueprint directory: `blueprints/ai-news/`. This means:
- Template pack JSON declares 6 AI News object types
- Agent definitions YAML declares 7 agents
- Seed JSONL provides default search queries as graph objects

**Why**: The platform already supports all required primitives. Adding backend code would increase scope and maintenance; a blueprint is portable and self-contained.

**Alternative considered**: New Go domain module for news fetching. Rejected — overkill when agents + MCP tools cover the use case.

### Decision: 6 topic categories

| Category | Object Type | Focus |
|---|---|---|
| Research Papers | `AIResearchPaper` | arxiv, preprints, academic releases |
| Model Releases | `AIModelRelease` | new model drops, benchmarks, availability |
| AI Tools & Products | `AIToolRelease` | new tools, APIs, products, integrations |
| Industry & Business | `AIIndustryEvent` | funding, acquisitions, partnerships, policy |
| Ethics & Safety | `AIEthicsUpdate` | alignment research, incidents, regulation |
| Applications & Demos | `AIApplicationDemo` | impressive demos, novel use-cases, creative AI |

**Why 6**: Broad enough to cover the field, narrow enough for focused agents and digestible summaries. A 7th "misc" category is omitted — borderline items are assigned to best-fit.

**Alternative considered**: 3 broad categories. Rejected — too coarse for daily agent focus and too much noise per digest section.

### Decision: One agent per category + one digest agent

Each category gets its own agent with a category-specific system prompt and search query list. The digest agent runs after, reads the day's items from the graph, and outputs the summary.

**Why**: Isolated agents are easier to tune, debug, and replace. Category focus improves search quality. Running them independently means one failed search doesn't block others.

**Alternative considered**: Single multi-category agent. Rejected — harder to tune per category, larger context window needed, harder to parallelize.

### Decision: Digest format — short prose sections, not a list dump

The digest agent produces ~300-500 word output: a one-line "headline" intro, then 6 short sections (one per category), each with 1-3 highlighted items and 1-2 sentences per item. Total: readable in under 3 minutes.

**Why**: A raw bullet list of 30+ links is overwhelming. Curated, opinionated prose with just the most interesting items is more valuable and engaging.

### Decision: Agents use graph search to avoid duplicates

Before creating a new news item object, each research agent SHALL query the graph for objects with matching URL or title. Deduplication is key to keeping the graph clean across daily runs.

### Decision: Web search via MCP tool (Brave/Exa/Tavily)

Agents use a registered MCP server with a `web_search` or `search` tool. The specific provider is user-configured. Blueprint README documents this prerequisite.

**Why**: Avoids coupling the blueprint to a specific search provider. Brave, Exa, and Tavily all expose compatible MCP interfaces.

## Risks / Trade-offs

- **Risk: MCP web search not configured** → Mitigation: agents fail gracefully with a clear error message; blueprint README includes setup instructions
- **Risk: Duplicate news items across days** → Mitigation: agents check for existing objects by URL before creating; deduplication is a hard requirement
- **Risk: Digest agent produces low-quality summaries** → Mitigation: system prompt is carefully crafted; iterative tuning is expected post-deployment
- **Risk: Rate limits on search API** → Mitigation: agents run sequentially (not simultaneously); each makes ~5-10 searches per run
- **Risk: Stale/irrelevant results** → Mitigation: search queries include `after:YYYY-MM-DD` date filters based on current date

## Migration Plan

1. Configure a web-search MCP server in the target project (Brave/Exa/Tavily)
2. Run `memory blueprints blueprints/ai-news/` — installs pack, agents, seed queries
3. Trigger the category agents manually for first run to verify
4. Enable daily schedule via agent trigger configuration
5. Review digest output, tune agent system prompts as needed

Rollback: delete installed template pack and agent definitions via admin UI or CLI.

## Open Questions

- Which MCP web search provider should be the "recommended default" in the README? (Exa has the best semantic search quality; Brave is cheapest)
- Should the digest be delivered as a project notification, a chat message, or both?
- Should the digest agent also store the digest itself as a graph object (for history)?
