# Python SDK

The Python SDK (`emergent-memory`) is a client library for the Memory API. It
mirrors the Go SDK, exposes one root `Client` with a sub-client per resource
area, and supports all three authentication modes plus SSE streaming.

**Source:** [`sdk/python/`](https://github.com/emergent-company/emergent.memory/tree/main/sdk/python)
**Import name:** `emergent`
**Distribution name:** `emergent-memory`

## Installation

!!! note "PyPI publication"
    The package is not published on PyPI yet. The release workflow
    (`python-sdk-release.yml`) publishes to PyPI when a `python-sdk/v*` tag is
    pushed, so `pip install emergent-memory` is the intended install command
    once the first release is out. Until then, install from source.

From a clone of the repository:

```bash
git clone https://github.com/emergent-company/emergent.memory.git
cd emergent.memory
pip install -e sdk/python
```

Runtime requirements, from `sdk/python/pyproject.toml`:

| Requirement | Version |
|---|---|
| Python | `>=3.10` |
| `httpx` | `>=0.24.0` |
| `pydantic` | `>=2.0.0` |

## Quick start

```python
from emergent import Client

client = Client.from_api_key("https://memory.emergent-company.ai", "<your-token>")
client.set_context(org_id="org_abc123", project_id="proj_xyz789")

for project in client.projects.list():
    print(project["id"], project["name"])

client.close()
```

Use the client as a context manager to close the HTTP connection pool
automatically:

```python
from emergent import Client

with Client.from_api_key("https://memory.emergent-company.ai", "<your-token>") as client:
    client.set_context(org_id="org_abc123", project_id="proj_xyz789")
    print(client.documents.list(limit=10))
```

## Authentication

`Client` sends one of two credential headers, depending on the mode:

| Mode | Use it for | Header sent |
|---|---|---|
| `apikey` | Standalone / self-hosted server key | `X-API-Key: <key>` |
| `apitoken` | Project-scoped token (`emt_*`) | `Authorization: Bearer <token>` |
| `oauth` | OAuth 2.0 access token you manage | `Authorization: Bearer <token>` |

`mode="apikey"` is the default. If the key starts with `emt_`, the SDK detects
it and switches to `APITokenProvider` automatically, so the same constructor
works for a server key and a project token.

```python
from emergent import Client

# Standalone server API key (X-API-Key)
client = Client.from_api_key("http://localhost:3012", "<server-api-key>")

# Project API token (emt_ prefix -> Bearer, auto-detected)
client = Client.from_api_key("https://memory.emergent-company.ai", "emt_<your-token>")

# OAuth access token (the caller refreshes it)
client = Client.from_oauth_token(
    "https://memory.emergent-company.ai",
    access_token="<your-access-token>",
)
```

### Automatic discovery with `Client.from_env()`

```python
from emergent import Client

client = Client.from_env()
```

Configuration is merged in this order, lowest priority first:

1. `~/.memory/config.yaml` (`server_url`, `api_key`, `project_token`, `org_id`, `project_id`)
2. `.env` (walked up from the current directory)
3. `.env.local` (walked up from the current directory)
4. `MEMORY_*` environment variables

Recognised environment variables:

| Variable | Meaning |
|---|---|
| `MEMORY_SERVER_URL` | Server URL (`MEMORY_API_URL` is an alias) |
| `MEMORY_ACCOUNT_API_KEY` | Account-level key (`MEMORY_API_KEY` is a deprecated alias) |
| `MEMORY_PROJECT_API_KEY` | Project token (`MEMORY_PROJECT_TOKEN` is a deprecated alias) |
| `MEMORY_ORG_ID` | Default organisation ID |
| `MEMORY_PROJECT_ID` | Default project ID |

If `MEMORY_PROJECT_API_KEY` is set, it is used as the credential instead of the
account key. `from_env()` raises `OSError` when no server URL or credential can
be found.

### Full configuration object

```python
from emergent import Client, Config

client = Client(
    Config(
        server_url="https://memory.emergent-company.ai",
        api_key="emt_<your-token>",
        org_id="org_abc123",
        project_id="proj_xyz789",
    )
)
```

`Config` fields: `server_url` (required), `api_key`, `mode`, `access_token`,
`org_id`, `project_id`, and an optional `http_client`.

## Organisation and project context

Most sub-clients send `X-Org-ID` and `X-Project-ID` on every request. Set both
once; `set_context` propagates the values to every context-scoped sub-client
atomically.

```python
client.set_context(org_id="org_abc123", project_id="proj_xyz789")
```

If you manage the HTTP transport yourself, `authenticate_request` injects the
same auth and context headers into a plain dict:

```python
headers = client.authenticate_request({})
# {"Authorization": "Bearer <token>", "X-Org-ID": "...", "X-Project-ID": "..."}
```

## Core resources

### Projects and organisations

These sub-clients are not project-scoped.

```python
projects = client.projects.list()
project = client.projects.get("proj_xyz789")

created = client.projects.create({"name": "Product Research"})
client.projects.update("proj_xyz789", {"name": "Renamed"})
client.projects.delete("proj_xyz789")

orgs = client.orgs.list()
org = client.orgs.get("org_abc123")
client.orgs.create({"name": "Acme Corp"})
client.orgs.delete("org_abc123")
```

### Knowledge graph

Objects are created from a payload and returned as dictionaries.

```python
obj = client.graph.create_object(
    {
        "type": "Person",
        "name": "Alice",
        "properties": {"role": "engineer"},
    }
)

obj = client.graph.get_object(obj["id"])
client.graph.update_object(obj["id"], {"properties": {"role": "lead"}})
client.graph.delete_object(obj["id"])

page = client.graph.list_objects(type="Person", limit=50)
count = client.graph.count_objects(type="Person")
```

Upserts and search:

```python
client.graph.upsert_object(
    {
        "externalId": "user-alice",
        "type": "Person",
        "name": "Alice",
    }
)

hits = client.graph.fts_search("Alice", limit=20)
similar = client.graph.find_similar(obj["id"], limit=10)

# Hybrid search with graph neighbours, and subgraph extraction, take a payload
res = client.graph.search_with_neighbors({"query": "machine learning", "limit": 5})
sub = client.graph.subgraph({"object_ids": [obj["id"]], "depth": 2})
```

Relationships take a payload with `type`, `src_id`, and `dst_id`. Optional keys
include `properties`, `weight`, and `branch_id`.

```python
rel = client.graph.create_relationship(
    {
        "type": "KNOWS",
        "src_id": "obj_1",
        "dst_id": "obj_2",
        "properties": {"since": "2024"},
    }
)

client.graph.list_relationships(type="KNOWS", limit=50)
client.graph.count_relationships(type="KNOWS")
client.graph.get_relationship_history(rel["id"])
client.graph.restore_relationship(rel["id"])
```

Objects can be moved between branches:

```python
client.graph.move_object("obj_id", target_branch_id="branch_2")
```

### Documents

```python
docs = client.documents.list(limit=50, offset=0)
doc = client.documents.get("doc_id")

# Upload a local file; extraction is enabled by default
result = client.documents.upload("/path/to/report.pdf", auto_extract=True)

# Signed download URL (the server redirects; this returns the target)
url = client.documents.download_url("doc_id")

client.documents.bulk_delete(["doc_1", "doc_2"])
summary = client.documents.get_extraction_summary("doc_id")
```

`upload()` returns `{"document": ..., "isDuplicate": bool, "existingDocumentId": ...}`.
Pass `file_name="..."` to override the uploaded file name.

### Unified search

```python
results = client.search.search(
    "machine learning",
    limit=10,
    strategy="hybrid",
    result_types="both",
)

graph_only = client.search.graph_search("neural networks", limit=5)
text_only = client.search.text_search("neural networks", limit=5)
```

`search()` returns `{"results": [...], "metadata": {...}}`; the two convenience
methods return the result list directly.

### Chat and streaming

`chat.stream()` is a generator of typed SSE events. The message is the first
positional argument.

```python
from emergent import Client, DoneEvent, MetaEvent, TokenEvent

with Client.from_api_key("https://memory.emergent-company.ai", "<your-token>") as client:
    client.set_context(org_id="org_abc123", project_id="proj_xyz789")

    for event in client.chat.stream("What is a knowledge graph?"):
        if isinstance(event, TokenEvent):
            print(event.token, end="", flush=True)
        elif isinstance(event, MetaEvent):
            print("\nCitations:", event.citations)
        elif isinstance(event, DoneEvent):
            break
```

Continue an existing conversation with `conversation_id`:

```python
conv = client.chat.create_conversation(title="My chat", message="Hello")
for event in client.chat.stream("Summarise the graph", conversation_id=conv.id):
    if event.type == "token":
        print(event.token, end="")
```

`chat.ask()` streams an answer through the project ask endpoint, and
`chat.ask_collect()` blocks and returns the accumulated text as a string:

```python
answer = client.chat.ask_collect("Summarise the project goals")
print(answer)

for event in client.chat.ask("Summarise project goals"):
    if event.type == "token":
        print(event.token, end="")
```

SSE event classes:

| Class | `type` | Key attributes |
|---|---|---|
| `MetaEvent` | `"meta"` | `conversation_id`, `citations`, `graph_objects` |
| `TokenEvent` | `"token"` | `token` (text delta) |
| `MCPToolEvent` | `"mcp_tool"` | `tool`, `status`, `result`, `error` |
| `ErrorEvent` | `"error"` | `error` |
| `DoneEvent` | `"done"` | — |
| `UnknownEvent` | any other value | `raw` |

### Agents

```python
agents = client.agents.list()
agent = client.agents.get("agent_id")

run = client.agents.trigger("agent_id")
run = client.agents.get_project_run(run["runId"])

client.agents.respond_to_question("<question-id>", "yes")
```

### Other sub-clients

| Attribute | Purpose |
|---|---|
| `client.branches` | List, create, update, fork, and delete graph branches |
| `client.tasks` | Project and cross-project background tasks |
| `client.schemas` | Schema packs: list available/installed, assign |
| `client.skills` | Project skills CRUD |
| `client.agent_definitions` | Agent definition templates |
| `client.api_tokens` | Project and account API tokens |
| `client.mcp` | MCP JSON-RPC client (`list_tools`, `call_tool`, ...) |

## Error handling

HTTP errors are raised as `APIError`. The exception carries `status_code`,
`code`, `message`, and `details`, plus predicate properties:

```python
from emergent import APIError

try:
    obj = client.graph.get_object("nonexistent")
except APIError as e:
    if e.is_not_found:
        print("Object not found")
    elif e.is_unauthorized:
        print("Check the credential")
    else:
        print(f"API error {e.status_code}: {e.message}")
```

Other exported exceptions are `EmergentError` (base class), `AuthError`, and
`StreamError`.

## Closing the client

```python
client.close()
```

If you supplied `http_client` in `Config`, the SDK does not close it; you own
that client's lifecycle.

## Next steps

- [Go SDK](go-sdk/index.md) — the reference client this SDK mirrors
- [Knowledge Graph](user-guide/knowledge-graph.md) — objects, relationships, and branches
- [Documents](user-guide/documents.md) — ingestion and extraction
- [Memory CLI](cli.md) — the command-line interface for the same API
- [API Reference](api-reference/index.md) — raw REST endpoints
