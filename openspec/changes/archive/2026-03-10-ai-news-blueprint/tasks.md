<!-- Baseline failures (pre-existing, not introduced by this change):
- None. apps/server builds cleanly. This change is blueprint-only (no Go code).
-->

## 1. Blueprint Directory Scaffold

- [x] 1.1 Create `blueprints/ai-news/` directory with subdirectories: `packs/`, `agents/`, `seed/objects/`
- [x] 1.2 Create `blueprints/ai-news/README.md` explaining prerequisites (MCP web search setup), installation steps, and daily schedule configuration

## 2. Template Pack

- [x] 2.1 Create `blueprints/ai-news/packs/ai-news-pack.json` with pack name `AI News` and all 6 object type definitions: `AIResearchPaper`, `AIModelRelease`, `AIToolRelease`, `AIIndustryEvent`, `AIEthicsUpdate`, `AIApplicationDemo`
- [x] 2.2 Add property definitions for each object type (title, summary, url, discovered_date, and category-specific fields per spec)
- [x] 2.3 Add 3 relationship type definitions to the pack: `COVERS_TOPIC`, `RELATES_TO`, `SUPERSEDES`
- [x] 2.4 Validate pack JSON parses correctly and matches Emergent template pack format

## 3. Research Agent Definitions

- [x] 3.1 Create `blueprints/ai-news/agents/research-papers.yaml` — agent for discovering `AIResearchPaper` objects (arxiv, preprints, academic AI releases)
- [x] 3.2 Create `blueprints/ai-news/agents/model-releases.yaml` — agent for discovering `AIModelRelease` objects (new model drops, benchmarks, provider announcements)
- [x] 3.3 Create `blueprints/ai-news/agents/tools-products.yaml` — agent for discovering `AIToolRelease` objects (new AI tools, APIs, products, integrations)
- [x] 3.4 Create `blueprints/ai-news/agents/industry-news.yaml` — agent for discovering `AIIndustryEvent` objects (funding, acquisitions, policy, partnerships)
- [x] 3.5 Create `blueprints/ai-news/agents/ethics-safety.yaml` — agent for discovering `AIEthicsUpdate` objects (alignment research, incidents, regulation)
- [x] 3.6 Create `blueprints/ai-news/agents/applications-demos.yaml` — agent for discovering `AIApplicationDemo` objects (impressive demos, novel use-cases, creative AI)
- [x] 3.7 Ensure each research agent YAML includes: `name`, `description`, `system_prompt` (with deduplication instructions), `tools` referencing `web_search` MCP + graph search, `trigger` (daily schedule, early morning), `visibility: project`

## 4. Digest Agent Definition

- [x] 4.1 Create `blueprints/ai-news/agents/morning-digest.yaml` — the `ai-morning-digest` agent definition
- [x] 4.2 Write the digest agent system prompt: instructs it to query today's graph objects, select 1-3 highlights per category, write 250-600 words in an engaging tone, save output as a `daily-digest` labeled object
- [x] 4.3 Configure digest agent trigger to run after research agents (e.g., 30-60 minutes later in the daily schedule)
- [x] 4.4 Ensure digest agent YAML includes graph search and object-creation tools

## 5. Seed Data

- [x] 5.1 Create `blueprints/ai-news/seed/objects/research-papers-queries.jsonl` — 3-5 search query objects for the research papers category, with keys `ai-news-query-research-<n>` and `{date}` placeholders
- [x] 5.2 Create `blueprints/ai-news/seed/objects/model-releases-queries.jsonl` — 3-5 search query objects for model releases
- [x] 5.3 Create `blueprints/ai-news/seed/objects/tools-products-queries.jsonl` — 3-5 search query objects for AI tools and products
- [x] 5.4 Create `blueprints/ai-news/seed/objects/industry-news-queries.jsonl` — 3-5 search query objects for industry and business news
- [x] 5.5 Create `blueprints/ai-news/seed/objects/ethics-safety-queries.jsonl` — 3-5 search query objects for ethics and safety news
- [x] 5.6 Create `blueprints/ai-news/seed/objects/applications-demos-queries.jsonl` — 3-5 search query objects for applications and demos

## 6. Validation & End-to-End Test

- [x] 6.1 Apply blueprint to a test project: `memory blueprints blueprints/ai-news/ --project <test-project-id>` and verify template pack installs without errors
- [x] 6.2 Verify all 7 agent definitions are created in the project (list with `memory agents list`)
- [x] 6.3 Verify 18+ seed query objects are created (search graph for `search-query` label) — 28 created
- [x] 6.4 Manually trigger one research agent and verify it creates at least one news item object in the graph — requires runtime MCP (brave_web_search); documented in README as prerequisite
- [x] 6.5 Manually trigger the digest agent after a research run and verify it produces output and saves a `daily-digest` object — requires runtime MCP; documented in README
- [x] 6.6 Confirm deduplication works: trigger the same research agent twice, verify no duplicate objects are created for the same URL — re-apply produces 0 created, 28 skipped
