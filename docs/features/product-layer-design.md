# Product Layer Design: emergent.memory + Installable Products

**Status:** Proposed
**Created:** 2026-02-14
**Author:** AI Agent + Human

---

## 1. Vision

Rename the core platform to **emergent.memory** and introduce a **product layer** that allows domain-specific configurations to be installed on top of it. Products like `emergent.research`, `emergent.code`, `emergent.docs` are not separate applications — they are **configuration bundles** that customize how emergent.memory behaves for a specific domain.

Products are distributed as **GitHub releases** and installed/upgraded via the platform.

```
┌──────────────────────────────────────────────────────────────┐
│                     emergent.memory (core)                    │
│                                                                │
│   Vector DB + Knowledge Graph + AI Extraction + MCP + Admin   │
│                                                                │
├──────────────────────────────────────────────────────────────┤
│                                                                │
│   ┌──────────────┐  ┌──────────────┐  ┌──────────────┐       │
│   │   emergent   │  │   emergent   │  │   emergent   │  ...  │
│   │   .research  │  │   .code      │  │   .docs      │       │
│   ├──────────────┤  ├──────────────┤  ├──────────────┤       │
│   │ Template     │  │ Template     │  │ Template     │       │
│   │ Packs        │  │ Packs        │  │ Packs        │       │
│   │              │  │              │  │              │       │
│   │ Agent        │  │ Agent        │  │ Agent        │       │
│   │ Definitions  │  │ Definitions  │  │ Definitions  │       │
│   │              │  │              │  │              │       │
│   │ Scheduled    │  │ Scheduled    │  │ Scheduled    │       │
│   │ Jobs         │  │ Jobs         │  │ Jobs         │       │
│   │              │  │              │  │              │       │
│   │ MCP Presets  │  │ MCP Presets  │  │ MCP Presets  │       │
│   └──────────────┘  └──────────────┘  └──────────────┘       │
│                                                                │
│   Products installed from GitHub releases                      │
│   Products bound to projects (1 product per project)           │
│                                                                │
└──────────────────────────────────────────────────────────────┘
```

---

## 2. What a Product Is

A product is a **declarative configuration bundle** — not new backend code. It customizes existing core systems:

| Component         | What It Configures                                      | Core System       | Status         |
| ----------------- | ------------------------------------------------------- | ----------------- | -------------- |
| Template Packs    | Domain schemas, extraction prompts, UI config           | `templatepacks`   | Already exists |
| Agent Definitions | System prompts, model config, tool selection, flow type | `agents` / `chat` | Needs building |
| Scheduled Jobs    | Recurring tasks, sync intervals, cron expressions       | `scheduler`       | Needs registry |
| MCP Config        | External MCP servers, tool filtering, custom prompts    | `mcp`             | Needs config   |
| Onboarding        | Seed data, initial setup steps                          | -                 | Future         |

### What a Product Is NOT

- Not a separate microservice or binary
- Not a plugin with arbitrary code execution
- Not a new API surface — it configures the existing one
- Not a tenant or workspace — it's a configuration layer on projects

---

## 3. Product Manifest Schema

Each product is defined by a `manifest.json` file distributed via GitHub releases.

```json
{
  "name": "emergent.research",
  "version": "1.2.0",
  "description": "Research paper management and analysis",
  "repository": "github.com/emergent-company/emergent.research",
  "min_core_version": "0.5.0",
  "author": "Emergent",
  "license": "MIT",

  "template_packs": [
    {
      "name": "Research Papers",
      "version": "2.0.0",
      "object_type_schemas": {
        "Paper": {
          "type": "object",
          "required": ["title", "abstract"],
          "properties": {
            "title": { "type": "string" },
            "abstract": { "type": "string" },
            "doi": { "type": "string" },
            "publication_date": { "type": "string", "format": "date" },
            "journal": { "type": "string" },
            "citations_count": { "type": "integer" }
          }
        },
        "Author": {
          "type": "object",
          "required": ["name"],
          "properties": {
            "name": { "type": "string" },
            "affiliation": { "type": "string" },
            "h_index": { "type": "integer" }
          }
        },
        "ResearchTopic": {
          "type": "object",
          "required": ["name"],
          "properties": {
            "name": { "type": "string" },
            "field": { "type": "string" },
            "description": { "type": "string" }
          }
        }
      },
      "relationship_type_schemas": {
        "authored_by": {
          "source": "Paper",
          "target": "Author",
          "properties": {
            "order": { "type": "integer" },
            "corresponding": { "type": "boolean" }
          }
        },
        "cites": {
          "source": "Paper",
          "target": "Paper",
          "properties": {
            "context": { "type": "string" }
          }
        },
        "belongs_to_topic": {
          "source": "Paper",
          "target": "ResearchTopic"
        }
      },
      "ui_configs": {
        "Paper": { "icon": "FileText", "color": "#3B82F6", "category": "Core" },
        "Author": { "icon": "User", "color": "#10B981", "category": "Core" },
        "ResearchTopic": {
          "icon": "Tag",
          "color": "#8B5CF6",
          "category": "Classification"
        }
      },
      "extraction_prompts": {
        "Paper": {
          "prompt": "Extract research paper metadata including title, abstract, DOI, publication date, journal name, and citation count.",
          "examples": []
        }
      }
    }
  ],

  "agents": [
    {
      "name": "research-assistant",
      "description": "Helps explore and analyze research papers",
      "system_prompt": "You are a research assistant specialized in academic literature. Help users explore papers, find connections between research topics, and synthesize findings across multiple sources.",
      "model": {
        "provider": "google",
        "name": "gemini-2.0-flash"
      },
      "tools": [
        "search_hybrid",
        "graph_traverse",
        "create_entity",
        "create_relationship",
        "get_entity"
      ],
      "temperature": 0.7,
      "max_tokens": 8192,
      "is_default": true
    },
    {
      "name": "paper-summarizer",
      "description": "Automatically summarizes ingested papers",
      "system_prompt": "You are a paper summarization agent. When a document is ingested, extract key findings, methodology, and conclusions into structured graph entities.",
      "model": {
        "provider": "google",
        "name": "gemini-2.0-flash"
      },
      "tools": ["create_entity", "create_relationship"],
      "trigger": "on_document_ingested",
      "temperature": 0.3
    }
  ],

  "jobs": [
    {
      "name": "arxiv-sync",
      "description": "Sync new papers from arXiv based on tracked topics",
      "cron": "0 */6 * * *",
      "agent": "research-assistant",
      "config": {
        "source": "arxiv",
        "max_papers_per_run": 50
      }
    }
  ],

  "mcp": {
    "servers": [
      {
        "name": "web-tools",
        "description": "Web search and content fetching for research agents",
        "transport": "stdio",
        "command": "npx",
        "args": ["-y", "@anthropic/mcp-web-tools"],
        "env": {
          "SEARCH_API_KEY": "${SEARCH_API_KEY}"
        }
      }
    ],
    "prompts": [
      {
        "name": "literature-review",
        "description": "Conduct a literature review on a topic",
        "arguments": [
          {
            "name": "topic",
            "description": "Research topic to review",
            "required": true
          }
        ],
        "template": "Search for all papers related to {topic}. Analyze the citation graph to identify seminal papers, recent developments, and research gaps. Produce a structured literature review."
      },
      {
        "name": "find-collaborators",
        "description": "Find potential research collaborators",
        "arguments": [
          {
            "name": "research_area",
            "description": "Area of research",
            "required": true
          }
        ],
        "template": "Search for authors who have published in {research_area}. Analyze co-authorship networks and identify potential collaborators based on complementary expertise."
      }
    ],
    "tool_filter": [
      "search_hybrid",
      "search_semantic",
      "search_fts",
      "graph_traverse",
      "entities_*",
      "relationships_*",
      "web_search",
      "web_fetch"
    ]
  }
}
```

---

## 4. Distribution via GitHub Releases

Products are distributed as GitHub release assets. The install/upgrade flow:

```
INSTALL
═══════

  emergent product install github.com/emergent-company/emergent.research
                           ─────────────────────────────────────────────
                                         │
                                         ▼
  1. Resolve repo → GET /repos/emergent-company/emergent.research/releases/latest
  2. Download manifest.json from release assets
  3. Validate manifest:
     - Schema validation
     - min_core_version check
     - No conflicts with existing products on project
   4. Delegate to existing systems:
      ├─ templatepacks module  → install packs (existing API)
      ├─ agent registry        → register agent definitions (NEW)
      ├─ scheduler             → register cron jobs (extend existing)
      ├─ mcp                   → register prompts + tool filter (extend existing)
      └─ mcp                   → register external MCP server connections (NEW)
   5. Record in kb.installed_products


UPGRADE
═══════

  emergent product upgrade emergent.research
                           ─────────────────
                                │
                                ▼
  1. Read current version from kb.installed_products
  2. Fetch latest release from GitHub
  3. Compare versions (semver)
   4. If newer:
      ├─ Template packs changed?
      │    → Use existing pack versioning + data migration
      │    → Pack system already handles schema evolution
      ├─ Agents changed?
      │    → Overwrite definitions (config only, safe)
      ├─ Jobs changed?
      │    → Deregister old, register new
      ├─ MCP servers changed?
      │    → Close removed connections, add new ones
      └─ MCP presets changed?
           → Overwrite (config only, safe)
  5. Update kb.installed_products.installed_version


CHECK FOR UPDATES
═════════════════

  emergent product check
                   ─────
                     │
                     ▼
  For each installed product:
    1. Fetch latest release tag from GitHub
    2. Compare with installed_version
    3. Report: "emergent.research: installed 1.1.0, available 1.2.0"
```

### GitHub Release Structure

```
emergent-company/emergent.research
│
├── releases/
│   └── v1.2.0/
│       └── Assets:
│           └── manifest.json      ← the entire product definition
│
├── README.md                      ← product documentation
├── CHANGELOG.md                   ← version history
└── examples/                      ← usage examples, sample data
```

The manifest.json is the **only required asset**. Everything is self-contained in that single file. No binaries, no code to compile.

---

## 5. Data Model

### New Table: `kb.installed_products`

```sql
CREATE TABLE kb.installed_products (
    id                UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    project_id        UUID NOT NULL REFERENCES kb.projects(id) ON DELETE CASCADE,
    product_name      TEXT NOT NULL,              -- "emergent.research"
    product_repo      TEXT NOT NULL,              -- "github.com/emergent-company/emergent.research"
    installed_version TEXT NOT NULL,              -- "1.2.0"
    manifest          JSONB NOT NULL,             -- full manifest snapshot at install time
    status            TEXT NOT NULL DEFAULT 'active',  -- active | upgrading | error
    installed_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at        TIMESTAMPTZ NOT NULL DEFAULT NOW(),

    UNIQUE(project_id, product_name)
);
```

### New Table: `kb.agent_definitions`

```sql
CREATE TABLE kb.agent_definitions (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    project_id      UUID NOT NULL REFERENCES kb.projects(id) ON DELETE CASCADE,
    product_name    TEXT,                        -- which product registered this (NULL = custom)
    name            TEXT NOT NULL,               -- "research-assistant"
    description     TEXT,
    system_prompt   TEXT NOT NULL,
    model_provider  TEXT NOT NULL DEFAULT 'google',
    model_name      TEXT NOT NULL DEFAULT 'gemini-2.0-flash',
    tools           JSONB NOT NULL DEFAULT '[]', -- ["search_hybrid", "graph_traverse"]
    temperature     REAL DEFAULT 0.7,
    max_tokens      INTEGER DEFAULT 8192,
    trigger         TEXT,                        -- NULL (chat), "on_document_ingested", etc.
    flow_type       TEXT DEFAULT 'react',        -- "react", "sequential", "custom"
    flow_config     JSONB,                       -- additional flow configuration
    is_default      BOOLEAN DEFAULT false,
    config          JSONB DEFAULT '{}',          -- extra product-specific config
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),

    UNIQUE(project_id, name)
);
```

### Relationship to Existing Tables

```
kb.projects
    │
    ├──→ kb.installed_products      (1:N - products installed on project)
    │        │
    │        └──→ references manifest which contains template pack definitions
    │
    ├──→ kb.agent_definitions       (1:N - agents available in project)
    │
    ├──→ kb.project_mcp_servers     (1:N - external MCP server connections)
    │
    ├──→ kb.project_template_packs  (1:N - already exists, installed by product manager)
    │
    └──→ kb.chat_conversations      (1:N - already exists, uses agent_definitions)
```

---

## 6. Upgrade & Data Migration Strategy

### What Changes and How

```
COMPONENT           UPGRADE STRATEGY                    RISK
─────────           ────────────────                    ────

Template Packs      Use existing pack versioning        Medium
                    Pack system handles:
                    - Schema evolution (add/rename/remove fields)
                    - Data re-extraction triggers
                    - Backward-compatible changes
                    Already partially solved.

Agent Definitions   Config overwrite                    Low
                    System prompts, model config,
                    tool lists are pure config.
                    No data to migrate.

Scheduled Jobs      Deregister old, register new        Low
                    Jobs are stateless config.
                    Running jobs finish, new
                    schedule takes effect.

MCP Servers         Close removed, add new              Low
                    External MCP connections are
                    stateless. Close old connections,
                    establish new ones. Secrets
                    persist in project settings.

MCP Presets         Config overwrite                    Low
                    Prompts and tool filters are
                    pure config. No state.
```

### Template Pack Versioning (Existing System)

The template pack system already handles versioning. When a product upgrades and includes a new version of its template pack:

1. The pack's `version` field is compared (semver)
2. Schema changes are detected (new fields, renamed fields, removed types)
3. Existing graph objects are flagged for re-processing if extraction prompts changed
4. The `project_object_type_registry` is updated with new schema definitions

**No new migration engine is needed.** The product manager delegates to the existing template pack upgrade flow.

### Version Compatibility

```json
{
  "min_core_version": "0.5.0"
}
```

The `min_core_version` field in the manifest ensures products aren't installed on incompatible core versions. The product manager checks this during install/upgrade and rejects if the core is too old.

---

## 7. Architecture: Where It Fits

### New Domain Module: `products`

```
apps/server-go/domain/
    products/                    ← NEW module
        entity.go               # ProductManifest, InstalledProduct structs
        store.go                # CRUD for kb.installed_products
        service.go              # Install, upgrade, uninstall, check
        github.go               # Fetch releases from GitHub API
        handler.go              # REST endpoints
        routes.go               # Route registration
        module.go               # fx module
```

### Extended Modules

```
agents/                         ← EXTEND
    Add: agent registry (store/service for kb.agent_definitions)
    Add: wire agent definitions into chat flow
    Add: agent-triggered jobs (on_document_ingested, etc.)

scheduler/                      ← EXTEND
    Add: product-scoped job registration
    Add: deregister jobs by product_name

mcp/                            ← EXTEND
    Add: tool filtering per agent (ResolveTools from ToolPool)
    Add: ToolPool component (built-in + external MCP server connections)
    Add: external MCP client connections (stdio + SSE transports)
    Add: product-defined prompts registration
    Add: kb.project_mcp_servers table

chat/                           ← EXTEND
    Add: select agent definition for conversation
    Add: use agent's system prompt, tools, model config
```

### Module Wiring (fx)

```go
// cmd/server/main.go
fx.Provide(
    products.NewStore,
    products.NewGitHubClient,
    products.NewService,
    products.NewHandler,
),
products.Module,
```

### API Endpoints

```
POST   /api/products/install              # Install from GitHub repo URL
POST   /api/products/:name/upgrade        # Upgrade to latest release
DELETE /api/products/:name                 # Uninstall
GET    /api/products                       # List installed products
GET    /api/products/:name                 # Get product details
GET    /api/products/check-updates         # Check all for updates
GET    /api/products/registry              # Browse available products (future)
```

---

## 8. Agent Registry Design

### Per-Agent Tool Filtering

Each agent definition's `tools` field is a **whitelist** of tool names from the project's combined tool pool. The tool pool consists of:

1. **Built-in graph tools** (30+ tools from `domain/mcp/service.go`) — always available
2. **External MCP server tools** — discovered from servers defined in the product manifest's `mcp.servers` section

When the `AgentExecutor` builds an ADK pipeline for an agent, it calls `ResolveTools(def.Tools)` which returns ONLY the tools in the agent's whitelist. This enables:

- **Read-only agents** — only `search_*`, `get_*`, `graph_traverse` tools
- **Write-only agents** — only `create_entity`, `create_relationship` tools
- **External-only agents** — only tools from external MCP servers (e.g., `web_search`, `web_fetch`)
- **Full-access agents** — `tools: ["*"]` wildcard grants all tools

See `multi-agent-coordination/multi-agent-architecture-design.md` Section 10 for the full `ToolPool` architecture.

### Agent Definition Lifecycle

```
Product Install
    │
    ▼
Parse manifest.agents[]
    │
    ▼
For each agent:
    ├─ Insert into kb.agent_definitions
    ├─ If trigger == "on_document_ingested":
    │    → Register event listener in extraction pipeline
    ├─ If trigger == cron expression:
    │    → Register with scheduler
    └─ If is_default == true:
         → Set as project's default chat agent


Chat Conversation
    │
    ▼
User starts new conversation
    │
    ▼
Select agent (default or user-chosen)
    │
    ▼
Load agent definition from kb.agent_definitions
    │
    ▼
Configure chat session:
    ├─ System prompt from agent definition
    ├─ Model + temperature from agent definition
    ├─ Available tools filtered by agent's tool list
    └─ Flow type determines execution pattern
```

### Agent Flow Types

```
react          Default. LLM decides which tools to call.
               Standard ReAct loop. Most agents use this.

sequential     Steps executed in order. Each step's output
               feeds the next. Good for extraction pipelines.

custom         Flow defined in flow_config JSONB.
               DAG of steps with conditions. Future.
```

### Integration with Existing Chat

The current chat module has hardcoded system prompts and tool configurations. The agent registry replaces these with per-project, per-agent configurations:

```
BEFORE (current):
    Chat → hardcoded system prompt → all MCP tools

AFTER (with agent registry):
    Chat → select agent → agent's system prompt → agent's tool subset
```

This is backward-compatible: if no agent definitions exist (no product installed), the chat falls back to current defaults.

---

## 8b. External MCP Server Connections

Products can connect external MCP servers to a project, extending the tool pool beyond emergent's built-in graph tools. This enables agents to interact with external services (web search, GitHub, Slack, etc.) via the standard MCP protocol.

### How External MCP Servers Work

```
Product manifest defines:
  mcp.servers: [
    { name: "web-tools", transport: "stdio", command: "npx ...", env: {...} },
    { name: "github", transport: "sse", url: "https://...", headers: {...} }
  ]

At product install time:
  1. Server configs stored in project settings
  2. No connections established yet (lazy initialization)

At agent execution time:
  1. AgentExecutor resolves tools for the agent definition
  2. If agent's tool list includes tools from external servers,
     ToolPool connects to those servers on demand
  3. External tools are discovered via MCP tools/list
  4. Tools are wrapped as ADK tool functions
  5. Agent can call them like any other tool

Connection lifecycle:
  - Connections are pooled per project (not per agent execution)
  - stdio transports: process spawned and kept alive
  - SSE transports: HTTP connection maintained
  - Connections are closed on server shutdown or project uninstall
```

### MCP Server Configuration Schema

```json
{
  "mcp": {
    "servers": [
      {
        "name": "web-tools",
        "description": "Web search and content fetching",
        "transport": "stdio",
        "command": "npx",
        "args": ["-y", "@anthropic/mcp-web-tools"],
        "env": {
          "SEARCH_API_KEY": "${SEARCH_API_KEY}"
        }
      },
      {
        "name": "github",
        "description": "GitHub repository access",
        "transport": "sse",
        "url": "https://mcp.github.com/sse",
        "headers": {
          "Authorization": "Bearer ${GITHUB_TOKEN}"
        }
      }
    ]
  }
}
```

**Transport types:**

- `stdio` — MCP server runs as a child process, communicates via stdin/stdout. Emergent spawns and manages the process.
- `sse` — MCP server runs remotely, communicates via HTTP Server-Sent Events. Emergent connects as a client.

**Environment variable interpolation:**

- Values like `${SEARCH_API_KEY}` are resolved from the project's secret store at connection time
- Secrets are NOT stored in the manifest — only variable references
- The admin UI provides a settings page for entering secret values per project

### Relationship to Agent Tool Lists

External MCP server tools are merged into the project's **combined tool pool** alongside built-in graph tools. Agent definitions reference tools by name regardless of source:

```json
{
  "name": "web-browser",
  "tools": ["web_search", "web_fetch"]
}
```

The agent doesn't know (or care) that `web_search` comes from an external MCP server. The `ToolPool` handles resolution:

```
Agent "web-browser" requests tools: ["web_search", "web_fetch"]
  │
  ├── ToolPool checks built-in graph tools: no match
  ├── ToolPool checks external MCP "web-tools": found web_search, web_fetch
  └── Returns 2 ADK tool functions wrapping the external MCP calls
```

### Data Model

External MCP server configurations are stored per project:

```sql
CREATE TABLE kb.project_mcp_servers (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    project_id      UUID NOT NULL REFERENCES kb.projects(id) ON DELETE CASCADE,
    product_name    TEXT,                        -- which product registered this (NULL = custom)
    name            TEXT NOT NULL,               -- "web-tools"
    description     TEXT,
    transport       TEXT NOT NULL,               -- "stdio" | "sse"
    config          JSONB NOT NULL,              -- { command, args, env } or { url, headers }
    status          TEXT DEFAULT 'active',       -- active | error | disabled
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),

    UNIQUE(project_id, name)
);
```

### V1 Scope

For V1, external MCP server connections are **optional** — the system works perfectly with only built-in graph tools. External MCP servers are the path to extending agent capabilities to the broader tool ecosystem (web, code, APIs) without embedding all that functionality in the emergent server.

---

## 9. emergent.memory as the Built-in Product

The current functionality is factored into `emergent.memory` — the built-in product that ships with the platform:

```json
{
  "name": "emergent.memory",
  "version": "1.0.0",
  "description": "Knowledge graph with vector search, AI extraction, and MCP integration",
  "repository": "github.com/emergent-company/emergent.memory",
  "builtin": true,

  "template_packs": [],

  "agents": [
    {
      "name": "memory-assistant",
      "description": "General-purpose knowledge assistant",
      "system_prompt": "...(current default system prompt)...",
      "model": { "provider": "google", "name": "gemini-2.0-flash" },
      "tools": ["*"],
      "is_default": true
    }
  ],

  "jobs": [],

  "mcp": {
    "servers": [],
    "prompts": ["...(current 5 MCP prompts)..."],
    "tool_filter": ["*"]
  }
}
```

The `"builtin": true` flag means this product is embedded in the binary and always available. It doesn't need to be fetched from GitHub. It serves as:

- The default product for projects with no product installed
- A reference implementation / template for building other products
- Dogfooding of the product system from day one

---

## 10. Example Products

### emergent.code

```json
{
  "name": "emergent.code",
  "version": "1.0.0",
  "description": "Codebase knowledge management for engineering teams",

  "template_packs": [
    {
      "name": "Software Engineering",
      "object_type_schemas": {
        "Repository": {
          "properties": { "name": {}, "language": {}, "url": {} }
        },
        "Module": {
          "properties": { "name": {}, "path": {}, "description": {} }
        },
        "Function": {
          "properties": { "name": {}, "signature": {}, "complexity": {} }
        },
        "Bug": { "properties": { "title": {}, "severity": {}, "status": {} } },
        "Decision": {
          "properties": { "title": {}, "context": {}, "outcome": {} }
        }
      }
    }
  ],

  "agents": [
    {
      "name": "code-analyst",
      "system_prompt": "You help engineers understand codebases, find patterns, and track architectural decisions...",
      "tools": ["search_hybrid", "graph_traverse", "create_entity"],
      "is_default": true
    },
    {
      "name": "adr-extractor",
      "system_prompt": "Extract architectural decisions from documents and conversations...",
      "trigger": "on_document_ingested",
      "tools": ["create_entity", "create_relationship"]
    }
  ]
}
```

### emergent.research

(Full manifest shown in Section 3 above)

### emergent.meeting

```json
{
  "name": "emergent.meeting",
  "version": "1.0.0",
  "description": "Meeting intelligence — transcripts, action items, decisions",

  "template_packs": [
    {
      "name": "Meeting Intelligence",
      "object_type_schemas": {
        "Meeting": {
          "properties": { "title": {}, "date": {}, "attendees": {} }
        },
        "ActionItem": {
          "properties": {
            "task": {},
            "assignee": {},
            "due_date": {},
            "status": {}
          }
        },
        "Decision": {
          "properties": { "description": {}, "rationale": {}, "decided_by": {} }
        },
        "Topic": { "properties": { "name": {}, "summary": {} } }
      }
    }
  ],

  "agents": [
    {
      "name": "meeting-analyst",
      "system_prompt": "You analyze meeting transcripts and notes...",
      "is_default": true
    },
    {
      "name": "action-tracker",
      "system_prompt": "Extract action items and track their completion...",
      "trigger": "on_document_ingested"
    }
  ],

  "jobs": [
    {
      "name": "overdue-actions-check",
      "cron": "0 9 * * 1-5",
      "agent": "action-tracker",
      "config": { "action": "check_overdue" }
    }
  ]
}
```

---

## 11. Implementation Plan

### Day 1: Product Manifest + Data Model

- Define `ProductManifest` Go struct with JSON tags
- Define `InstalledProduct` entity
- Define `AgentDefinition` entity
- Create database migration for `kb.installed_products` and `kb.agent_definitions`
- Create `products` domain module skeleton (entity/store/service/handler/routes/module)

### Day 2: GitHub Fetcher + Install Flow

- Implement GitHub release fetcher (GET latest release, download manifest.json asset)
- Implement manifest validation (schema, version checks)
- Implement install flow:
  - Parse manifest
  - Delegate template packs to `templatepacks` module
  - Insert agent definitions into `kb.agent_definitions`
  - Register jobs with scheduler
  - Register MCP prompts
  - Record in `kb.installed_products`
- API endpoints: `POST /api/products/install`, `GET /api/products`

### Day 3: Agent Registry + Chat Integration

- Agent registry CRUD (store/service for `kb.agent_definitions`)
- Wire agent definitions into chat module:
  - Agent selection when starting conversation
  - Load system prompt from agent definition
  - Filter tools per agent configuration
  - Use agent's model/temperature settings
- Event-triggered agents (`on_document_ingested`)
- API endpoints for agent management

### Day 4: emergent.memory as Built-in + First External Product

- Extract current defaults into `emergent.memory` manifest (embedded in binary)
- Create `emergent.research` or `emergent.meeting` as first external product
- Create GitHub repo with manifest.json release
- Test full install/upgrade cycle
- Upgrade flow: `POST /api/products/:name/upgrade`

---

## 12. Open Questions

### Resolved

- **SQL migrations in products?** No. Products don't include SQL. Template pack versioning handles data evolution.
- **Where do products live?** GitHub releases. Single `manifest.json` file per release.
- **How complex are product migrations?** They're not — template pack versioning is the migration system. Everything else is config swaps.

### Open

- **Product scope: per-org or per-project?** Template packs are per-project today. Products should probably follow the same pattern. But should an org be able to say "all new projects use emergent.research by default"?

- **Multi-product projects:** Can a project have both `emergent.research` AND `emergent.code` installed? Template packs can coexist. Agent name collisions would need handling. Probably worth supporting but not in v1.

- **Agent flow engine depth:** v1 is "system prompt + tools + model" (simple). Do we need multi-step DAG flows (ADK-level) in the manifest, or is that a future iteration?

- **Private GitHub repos:** Public repos work without auth. Private repos need a GitHub token. Support this in v1 or later?

- **Product registry / marketplace:** A central registry of available products (like npm registry). Not needed for v1, but the `manifest.json` schema should be designed with this in mind.

- **CLI vs API only:** Should there be a CLI command (`emergent product install ...`) in addition to the REST API? The API is sufficient for the admin UI, but a CLI would help developers.

---

## 13. Risks & Considerations

- **Security:** Product manifests are trusted configuration. They cannot execute arbitrary code. The tool list is filtered against known MCP tools — an agent cannot reference tools that don't exist in the core.

- **Breaking changes in core:** If core renames or removes an MCP tool, products referencing it will break. The `min_core_version` field mitigates this, but we need a deprecation strategy for tools.

- **Template pack conflicts:** Two products could define the same object type name (e.g., both define "Document"). Per-project scoping helps, but multi-product support needs conflict resolution.

- **Rate limiting on GitHub API:** Unauthenticated GitHub API is limited to 60 requests/hour. For checking updates across many products, this could be tight. Consider caching release info.

---

## 14. Success Criteria

1. `emergent.memory` is a real product manifest embedded in the binary
2. At least one external product (`emergent.research` or `emergent.meeting`) can be installed from a GitHub release
3. Installed products configure template packs, agents, and MCP prompts on the target project
4. Chat conversations use agent definitions (system prompt, tools, model) from the installed product
5. Products can be upgraded by fetching a newer release, with template pack versioning handling data changes
6. The entire product definition fits in a single `manifest.json` — no binaries, no code, no SQL
