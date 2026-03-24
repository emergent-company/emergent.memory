# emergent -- Python SDK

[![Python SDK CI](https://github.com/emergent-company/emergent.memory/actions/workflows/python-sdk.yml/badge.svg)](https://github.com/emergent-company/emergent.memory/actions/workflows/python-sdk.yml)

Python client library for the [Emergent Memory](https://emergent-company.ai) API.

Mirrors the Go SDK at `apps/server/pkg/sdk/` and supports all three auth modes, SSE streaming, and the full range of API sub-clients.

---

## Installation

```bash
pip install emergent-memory-sdk        # when published to PyPI
# or, from source:
pip install -e sdk/python
```

**Runtime requirements:** `httpx >= 0.24`

---

## Authentication

Three auth modes are supported, matching the Go SDK.

| Mode | When to use | Header sent |
|------|-------------|-------------|
| `apikey` | Standalone / self-hosted server key | `X-API-Key: <key>` |
| `apitoken` | Project-scoped token (`emt_*`) | `Authorization: Bearer <token>` |
| `oauth` | OAuth 2.0 (caller manages token) | `Authorization: Bearer <token>` |

`emt_*` prefix is auto-detected in `apikey` mode and transparently upgraded to Bearer auth.

### Auto-discovery (`from_env`)

`Client.from_env()` discovers configuration automatically — no arguments needed:

```python
client = Client.from_env()
```

Resolution order (highest priority wins):

| Source | Example |
|--------|---------|
| `MEMORY_*` env vars | `MEMORY_SERVER_URL=http://...` |
| `.env.local` (walked up from cwd) | `MEMORY_API_KEY=emt_abc123` |
| `.env` (walked up from cwd) | `MEMORY_PROJECT_ID=proj_1` |
| `~/.memory/config.yaml` | `api_key: emt_abc123` |

Recognised keys: `MEMORY_SERVER_URL` (or `MEMORY_API_URL`), `MEMORY_API_KEY`, `MEMORY_PROJECT_TOKEN`, `MEMORY_ORG_ID`, `MEMORY_PROJECT_ID`. If `MEMORY_PROJECT_TOKEN` is set it takes precedence over `MEMORY_API_KEY` as the credential.

---

## Quick start

```python
from emergent import Client

# --- API key (standalone server) ---
client = Client.from_api_key("http://localhost:3012", "my-server-api-key")

# --- Project API token (emt_* -> Bearer auto-detected) ---
client = Client.from_api_key("https://api.emergent-company.ai", "emt_abc123...")

# --- OAuth Bearer token ---
client = Client.from_oauth_token("https://api.emergent-company.ai", access_token="eyJh...")

# --- Full config ---
from emergent import Config
client = Client(Config(
    server_url="https://api.emergent-company.ai",
    api_key="emt_abc123",
    org_id="org_1",
    project_id="proj_1",
))

# --- Context manager (auto-closes) ---
with Client.from_api_key("http://localhost:3012", "key") as client:
    client.set_context(org_id="org_1", project_id="proj_1")
    ...
```

---

## Setting context

Most sub-clients need an organisation and project context. Set them once; all sub-clients update atomically.

```python
client.set_context(org_id="org_abc", project_id="proj_xyz")
```

---

## Sub-clients

### Projects & Orgs

```python
projects = client.projects.list()
project  = client.projects.get("proj_1")
orgs     = client.orgs.list()
org      = client.orgs.get("org_1")

# Create / delete orgs
new_org = client.orgs.create({"name": "My Org"})
client.orgs.delete("org_1")
```

### Chat & Streaming

```python
client.set_context("org_1", "proj_1")

# Create a conversation
conv = client.chat.create_conversation({"title": "My chat"})

# Stream tokens
for event in client.chat.stream(conversation_id=conv["id"], message="Hello!"):
    if event.type == "token":
        print(event.token, end="", flush=True)
    elif event.type == "meta":
        print("\ncitations:", event.citations)
    elif event.type == "done":
        break

# Collect full response (blocks until done)
result = client.chat.ask_collect(conversation_id=conv["id"], message="Summarise the graph")
print(result["text"])
print("citations:", result["citations"])
```

### Agents

```python
agents = client.agents.list()
agent  = client.agents.get("agent_id")

# Trigger an agent run
run = client.agents.trigger("agent_id", {"input": "analyse latest data"})

# Poll run status
run = client.agents.get_project_run(run["id"])
print(run["status"])

# Respond to a pending question
client.agents.respond_to_question("question_id", {"response": "yes"})
```

### Graph

```python
# Create / upsert objects
obj = client.graph.create_object({
    "name": "Alice",
    "type": "Person",
    "properties": {"role": "engineer"},
})

# Upsert (create-or-update by external id)
client.graph.upsert_object({
    "externalId": "user-alice",
    "name": "Alice",
    "type": "Person",
})

# Fetch and search
obj      = client.graph.get_object("obj_id")
results  = client.graph.fts_search("Alice")
similar  = client.graph.similar("obj_id", limit=10)

# Hybrid search with neighbours
res = client.graph.search_with_neighbors("machine learning", limit=5)

# Relationships
client.graph.create_relationship("obj_1", "obj_2", "KNOWS", {"since": "2024"})

# Move object to another branch
client.graph.move_object("obj_id", target_branch_id="branch_2")

# Relationship history & restore
history = client.graph.get_relationship_history("rel_id")
client.graph.restore_relationship("rel_id")
count   = client.graph.count_relationships(type="KNOWS")

# Bulk update objects
client.graph.bulk_update([{"id": "obj_1", "name": "Updated"}])

# Extract a subgraph
sub = client.graph.subgraph({"object_ids": ["obj_1", "obj_2"], "depth": 2})

# Upsert a relationship
client.graph.upsert_relationship({"src_id": "a", "dst_id": "b", "type": "LINKS_TO"})
```

### Branches

```python
branches = client.branches.list()
branch   = client.branches.get("branch_id")
new      = client.branches.create({"name": "feature-x", "parentBranchId": "main"})
client.branches.update("branch_id", {"name": "renamed"})
client.branches.delete("branch_id")

# Fork a branch
forked = client.branches.fork("branch_id", {"name": "experiment-fork"})
```

### Documents

```python
docs = client.documents.list()
doc  = client.documents.get("doc_id")

# Upload a file
doc = client.documents.upload("/path/to/file.pdf", auto_extract=True)

# Download URL (follows 307 redirect)
url = client.documents.download_url("doc_id")

# Bulk operations
client.documents.bulk_delete(["doc_1", "doc_2"])
impact = client.documents.bulk_deletion_impact(["doc_1", "doc_2"])

# Extraction info
summary = client.documents.get_extraction_summary("doc_id")
types   = client.documents.get_source_types()
```

### API Tokens

```python
# Project-scoped tokens (requires project context or explicit project_id)
client.set_context("org_1", "proj_1")
token = client.api_tokens.create_project_token({"name": "CI token"})
tokens = client.api_tokens.list_project_tokens()
client.api_tokens.revoke_project_token("token_id")

# Account-scoped tokens
acct_token = client.api_tokens.create_account_token({"name": "Personal"})
acct_tokens = client.api_tokens.list_account_tokens()
client.api_tokens.revoke_account_token("token_id")
```

### Tasks

```python
# Project tasks (uses current project context)
tasks  = client.tasks.list(status="pending", limit=20)
counts = client.tasks.counts()

# Cross-project tasks
all_tasks  = client.tasks.list_all(status="running")
all_counts = client.tasks.counts_all()

# Single task operations
task = client.tasks.get("task_id")
client.tasks.resolve("task_id", notes="Done")
client.tasks.cancel("task_id")
```

### MCP (Model Context Protocol)

```python
client.set_context("org_1", "proj_1")

tools = client.mcp.list_tools()
for t in tools.get("tools", []):
    print(t["name"], "-", t.get("description", ""))

result = client.mcp.call_tool("graph_read_object", {"object_id": "abc123"})
```

### Search

```python
results = client.search.search("machine learning", limit=10)
results = client.search.text_search("neural networks")
results = client.search.graph_search("AI research", limit=5)
```

### Schemas & Skills

```python
# Schema packs — list available/installed packs for a project
client.set_context("org_1", "proj_1")
available = client.schemas.list_available()
installed = client.schemas.list_installed()
types     = client.schemas.get_compiled_types()

# Assign a schema pack to the project
client.schemas.assign({"packId": "pack_id"})

# Skills (project-scoped)
skills = client.skills.list()
skill  = client.skills.create({"name": "Summarise", "prompt": "..."})
client.skills.update("skill_id", {"name": "Summarise v2"})
client.skills.delete("skill_id")
```

### Agent Definitions

```python
defs = client.agent_definitions.list()
defn = client.agent_definitions.create({
    "name": "ResearchAgent",
    "description": "Searches and summarises documents",
    "skills": ["skill_id_1"],
})
```

---

## SSE Event types

All streaming methods yield typed event objects.

| Class | `type` field | Key attributes |
|-------|-------------|----------------|
| `MetaEvent` | `"meta"` | `conversation_id`, `citations`, `graph_objects` |
| `TokenEvent` | `"token"` | `token` (text delta) |
| `MCPToolEvent` | `"mcp_tool"` | `tool`, `status`, `result`, `error` |
| `ErrorEvent` | `"error"` | `error` (message) |
| `DoneEvent` | `"done"` | -- |
| `UnknownEvent` | *(anything else)* | `raw` (raw dict) |

```python
from emergent import MetaEvent, TokenEvent, DoneEvent

for event in client.chat.stream(conversation_id="conv_1", message="Hi"):
    if isinstance(event, TokenEvent):
        print(event.token, end="")
    elif isinstance(event, MetaEvent):
        print("\nCitations:", event.citations)
    elif isinstance(event, DoneEvent):
        break
```

---

## Manual auth (custom HTTP transport)

```python
import httpx

headers = client.authenticate_request({})
with httpx.stream("POST", url, headers=headers, json=body) as resp:
    for line in resp.iter_lines():
        ...
```

---

## Error handling

```python
from emergent import APIError

try:
    obj = client.graph.get_object("nonexistent")
except APIError as e:
    if e.is_not_found:
        print("Object not found")
    else:
        print(f"API error {e.status_code}: {e.message}")
```

---

## Closing the client

```python
client.close()          # releases connection pool

# Or use the context manager:
with Client.from_api_key(url, key) as client:
    ...
```

---

## Development

### Setup

```bash
cd sdk/python
pip install -e '.[dev]'
```

### Testing

```bash
pytest                          # run all tests
pytest -v --tb=short            # verbose with short tracebacks
pytest --cov=emergent           # with coverage report
pytest tests/test_graph.py      # run a single test file
pytest -k "test_create_object"  # run tests matching a pattern
```

Tests use [respx](https://lundberg.github.io/respx/) for HTTP mocking -- no live server required.

### Linting & Type checking

```bash
ruff check .                    # lint
ruff format --check .           # format check
ruff format .                   # auto-format
mypy emergent/                  # type check
```

### Releasing

Releases are triggered by pushing a tag matching `python-sdk/v*`:

```bash
git tag python-sdk/v0.2.0
git push origin python-sdk/v0.2.0
```

This runs the CI validation, builds the package, publishes to PyPI, and creates a GitHub release.

---

## File layout

```
sdk/python/
├── pyproject.toml
├── tests/
│   ├── conftest.py              # Shared fixtures and helpers
│   ├── test_auth.py             # Auth providers
│   ├── test_base.py             # BaseClient HTTP machinery
│   ├── test_client.py           # Root Client + Config
│   ├── test_exceptions.py       # Error classes
│   ├── test_sse.py              # SSE parser + events
│   ├── test_graph.py            # GraphClient
│   ├── test_chat.py             # ChatClient
│   ├── test_agents.py           # AgentsClient
│   └── test_subclient_misc.py   # MCP, Search, Documents, Projects, Orgs,
│                                  Schemas, Skills, AgentDefs, Branches,
│                                  APITokens, Tasks
└── emergent/
    ├── __init__.py              # Public API surface
    ├── client.py                # Root Client + Config
    ├── auth.py                  # AuthProvider, APIKeyProvider, APITokenProvider, OAuthProvider
    ├── exceptions.py            # EmergentError, APIError, AuthError, StreamError
    ├── sse.py                   # SSE parser + typed event dataclasses
    ├── _base.py                 # BaseClient (shared HTTP machinery)
    ├── chat.py                  # ChatClient
    ├── agents.py                # AgentsClient
    ├── agent_definitions.py     # AgentDefinitionsClient
    ├── mcp.py                   # MCPClient
    ├── graph.py                 # GraphClient
    ├── search.py                # SearchClient
    ├── documents.py             # DocumentsClient
    ├── projects.py              # ProjectsClient
    ├── orgs.py                  # OrgsClient
    ├── schemas.py               # SchemasClient
    ├── skills.py                # SkillsClient
    ├── branches.py              # BranchesClient
    ├── api_tokens.py            # APITokenClient
    └── tasks.py                 # TasksClient
```
