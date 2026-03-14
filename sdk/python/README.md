# emergent — Python SDK

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

---

## Quick start

```python
from emergent import Client

# --- API key (standalone server) ---
client = Client.from_api_key("http://localhost:3012", "my-server-api-key")

# --- Project API token (emt_* → Bearer auto-detected) ---
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
schemas = client.schemas.list()
schema  = client.schemas.create({"name": "Person", "fields": [...]})

skills = client.skills.list()
skill  = client.skills.create({"name": "Summarise", "prompt": "..."})
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
| `DoneEvent` | `"done"` | — |
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

## File layout

```
sdk/python/
├── pyproject.toml
└── emergent/
    ├── __init__.py          # Public API surface
    ├── client.py            # Root Client + Config
    ├── auth.py              # AuthProvider, APIKeyProvider, APITokenProvider, OAuthProvider
    ├── exceptions.py        # EmergentError, APIError, AuthError, StreamError
    ├── sse.py               # SSE parser + typed event dataclasses
    ├── _base.py             # BaseClient (shared HTTP machinery)
    ├── chat.py              # ChatClient
    ├── agents.py            # AgentsClient
    ├── agent_definitions.py # AgentDefinitionsClient
    ├── mcp.py               # MCPClient
    ├── graph.py             # GraphClient
    ├── search.py            # SearchClient
    ├── projects.py          # ProjectsClient
    ├── orgs.py              # OrgsClient
    ├── schemas.py           # SchemasClient
    └── skills.py            # SkillsClient
```
