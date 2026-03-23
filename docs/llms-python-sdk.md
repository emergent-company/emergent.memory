# Memory Python SDK Reference for LLMs

Python client library for the Emergent Memory API. Package: `emergent-memory`. Source: `sdk/python/`.

**Install:** `pip install emergent-memory-sdk`

---

## Client setup

```python
from emergent import Client, Config

# From API key (auto-detects emt_* → Bearer)
client = Client.from_api_key("https://api.emergent-company.ai", "emt_abc123")

# From environment variables (EMERGENT_SERVER_URL, EMERGENT_API_KEY)
client = Client.from_env()

# Full config
client = Client(Config(
    server_url="https://api.emergent-company.ai",
    api_key="emt_abc123",
    org_id="org_1",
    project_id="proj_1",
))

# Set/update context (all sub-clients update atomically)
client.set_context(org_id="org_1", project_id="proj_1")

# Context manager (auto-closes HTTP pool)
with Client.from_api_key(url, key) as client:
    ...
```

---

## Sub-clients

### graph — GraphClient

```python
# Objects
obj  = client.graph.create_object({"name": "Alice", "type": "Person", "properties": {}})
obj  = client.graph.get_object("obj_id")
obj  = client.graph.update_object("obj_id", {"name": "Alice B."})
client.graph.delete_object("obj_id")
obj  = client.graph.upsert_object({"externalId": "alice", "name": "Alice", "type": "Person"})
objs = client.graph.bulk_create([{...}, {...}])
client.graph.bulk_update([{"id": "obj_1", "name": "Updated"}])
client.graph.bulk_update_status(["obj_1", "obj_2"], status="archived")
client.graph.move_object("obj_id", target_branch_id="branch_2")
client.graph.restore_object("obj_id")
hist = client.graph.get_object_history("obj_id")
sub  = client.graph.subgraph({"object_ids": ["obj_1"], "depth": 2})

# Search
results = client.graph.fts_search("Alice")
results = client.graph.semantic_search("machine learning", limit=10)
results = client.graph.vector_search({"query": "...", "limit": 5})
results = client.graph.search("query", limit=10)
results = client.graph.search_with_neighbors("query", limit=5)
results = client.graph.traverse({"start_id": "obj_1", "depth": 3})
results = client.graph.expand({"object_id": "obj_1"})
tags    = client.graph.list_tags()
similar = client.graph.similar("obj_id", limit=10)
edges   = client.graph.get_edges("obj_id")
count   = client.graph.count_objects()

# Relationships
rel  = client.graph.create_relationship("src_id", "dst_id", "KNOWS", {"since": "2024"})
rel  = client.graph.get_relationship("rel_id")
rel  = client.graph.update_relationship("rel_id", {"properties": {}})
client.graph.delete_relationship("rel_id")
client.graph.upsert_relationship({"src_id": "a", "dst_id": "b", "type": "LINKS_TO"})
rels = client.graph.bulk_create_relationships([{...}])
hist = client.graph.get_relationship_history("rel_id")
client.graph.restore_relationship("rel_id")
count = client.graph.count_relationships(type="KNOWS")
results = client.graph.search_relationships("query")

# Branches
branches = client.graph.list_branches()
client.graph.merge_branch("target_branch_id", {"sourceBranchId": "src_id", "execute": True})

# Analytics
top  = client.graph.most_accessed(limit=20)
unused = client.graph.unused_objects(limit=20)
```

### chat — ChatClient

```python
convs = client.chat.list_conversations()
conv  = client.chat.create_conversation({"title": "My chat"})
conv  = client.chat.get_conversation("conv_id")
conv  = client.chat.update_conversation("conv_id", {"title": "Renamed"})
client.chat.delete_conversation("conv_id")
msg   = client.chat.send_message("conv_id", "Hello!")

# Streaming (yields SSE events)
for event in client.chat.stream(conversation_id="conv_id", message="Summarise"):
    if event.type == "token":
        print(event.token, end="")
    elif event.type == "done":
        break

# Blocking collect
result = client.chat.ask_collect(conversation_id="conv_id", message="Hello")
# result = {"text": str, "citations": list, "graph_objects": list}
```

### agents — AgentsClient

```python
agents = client.agents.list()
agent  = client.agents.get("agent_id")
agent  = client.agents.create({...})
agent  = client.agents.update("agent_id", {})
client.agents.delete("agent_id")

run    = client.agents.trigger("agent_id", {"input": "..."})
runs   = client.agents.list_runs("agent_id")
runs   = client.agents.list_project_runs(limit=20)
run    = client.agents.get_project_run("run_id")
msgs   = client.agents.get_run_messages("run_id")
calls  = client.agents.get_run_tool_calls("run_id")
client.agents.cancel_run("agent_id", "run_id")

questions = client.agents.list_questions()
questions = client.agents.get_run_questions("run_id")
client.agents.respond_to_question("question_id", "yes")

hooks = client.agents.list_hooks("agent_id")
hook  = client.agents.create_hook("agent_id", {})
client.agents.delete_webhook_hook("agent_id", "hook_id")

events   = client.agents.get_pending_events("agent_id")
sessions = client.agents.list_adk_sessions()
session  = client.agents.get_adk_session("session_id")
```

### agent_definitions — AgentDefinitionsClient

```python
defs = client.agent_definitions.list()
defn = client.agent_definitions.get("def_id")
defn = client.agent_definitions.create({...})
defn = client.agent_definitions.update("def_id", {})
client.agent_definitions.delete("def_id")
```

### documents — DocumentsClient

```python
docs = client.documents.list()
doc  = client.documents.get("doc_id")
doc  = client.documents.create({...})
client.documents.delete("doc_id")

text = client.documents.get_content("doc_id")
doc  = client.documents.upload("/path/to/file.pdf", auto_extract=True)
url  = client.documents.download_url("doc_id")

client.documents.bulk_delete(["doc_1", "doc_2"])
impact = client.documents.get_deletion_impact("doc_id")
impact = client.documents.bulk_deletion_impact(["doc_1", "doc_2"])
summary = client.documents.get_extraction_summary("doc_id")
types   = client.documents.get_source_types()
```

### search — SearchClient

```python
results = client.search.search("query", limit=10)
results = client.search.text_search("query")
results = client.search.graph_search("query", limit=5)
```

### mcp — MCPClient

```python
tools  = client.mcp.list_tools()
result = client.mcp.call_tool("tool_name", {"arg": "value"})
```

### projects — ProjectsClient

```python
projects = client.projects.list()
project  = client.projects.get("proj_id")
project  = client.projects.create({...})
project  = client.projects.update("proj_id", {})
client.projects.delete("proj_id")
```

### orgs — OrgsClient

```python
orgs = client.orgs.list()
org  = client.orgs.get("org_id")
org  = client.orgs.create({"name": "My Org"})
client.orgs.delete("org_id")
```

### schemas — SchemasClient (schema packs)

```python
client.set_context("org_1", "proj_1")
available = client.schemas.list_available()
installed = client.schemas.list_installed()
types     = client.schemas.get_compiled_types()
client.schemas.assign({"packId": "pack_id"})
client.schemas.update_assignment("assignment_id", {"active": True})
client.schemas.delete_assignment("assignment_id")
```

### skills — SkillsClient (project-scoped)

```python
skills = client.skills.list()
skill  = client.skills.create({"name": "Summarise", "prompt": "..."})
skill  = client.skills.update("skill_id", {"name": "Summarise v2"})
client.skills.delete("skill_id")
```

### branches — BranchesClient

```python
branches = client.branches.list()
branch   = client.branches.get("branch_id")
branch   = client.branches.create({"name": "experiment"})
branch   = client.branches.update("branch_id", {"name": "renamed"})
client.branches.delete("branch_id")
forked   = client.branches.fork("branch_id", {"name": "fork-name"})
```

### api_tokens — APITokenClient

```python
# Project-scoped
token  = client.api_tokens.create_project_token({"name": "CI"})
tokens = client.api_tokens.list_project_tokens()
token  = client.api_tokens.get_project_token("token_id")
client.api_tokens.revoke_project_token("token_id")

# Account-scoped
token  = client.api_tokens.create_account_token({"name": "Personal"})
tokens = client.api_tokens.list_account_tokens()
token  = client.api_tokens.get_account_token("token_id")
client.api_tokens.revoke_account_token("token_id")
```

### tasks — TasksClient

```python
tasks  = client.tasks.list(status="pending", limit=20)
counts = client.tasks.counts()
all_tasks  = client.tasks.list_all(status="running")
all_counts = client.tasks.counts_all()
task   = client.tasks.get("task_id")
client.tasks.resolve("task_id", notes="Done")
client.tasks.cancel("task_id")
```

---

## SSE event types

| Class | `type` | Key attributes |
|-------|--------|----------------|
| `MetaEvent` | `"meta"` | `conversation_id`, `citations`, `graph_objects` |
| `TokenEvent` | `"token"` | `token` (text delta) |
| `MCPToolEvent` | `"mcp_tool"` | `tool`, `status`, `result`, `error` |
| `ErrorEvent` | `"error"` | `error` (message) |
| `DoneEvent` | `"done"` | — |
| `UnknownEvent` | *(other)* | `raw` (dict) |

---

## Error handling

```python
from emergent import APIError

try:
    obj = client.graph.get_object("nonexistent")
except APIError as e:
    print(e.status_code)    # int HTTP status
    print(e.message)        # str error message
    print(e.is_not_found)   # bool
    print(e.is_forbidden)   # bool
    print(e.is_unauthorized) # bool
```

---

## Auth providers

```python
from emergent.auth import APIKeyProvider, APITokenProvider, OAuthProvider

# APIKeyProvider: sends X-API-Key header; auto-upgrades emt_* to Bearer
# APITokenProvider: sends Authorization: Bearer for emt_* tokens
# OAuthProvider: sends Authorization: Bearer for OAuth access tokens
```
