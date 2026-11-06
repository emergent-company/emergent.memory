# Security Scopes

Every API request is authorized by checking the scopes granted to the bearer token (user session token or API token). This page is the complete reference for the permission scopes.

## Scope types

There are two kinds of scopes:

- **Fine-grained scopes** — directly map to specific API routes
- **Umbrella scopes** — shorthand that implicitly grants a set of fine-grained scopes

---

## All scopes

### Organization management

| Scope | Grants access to |
|---|---|
| `org:read` | Read org details, list members |
| `org:project:create` | Create new projects in an org |
| `org:project:delete` | Delete projects |
| `org:invite:create` | Send org invitations |

### Project management

| Scope | Grants access to |
|---|---|
| `project:read` | Read project details |
| `project:invite:create` | Invite users to a specific project |
| `projects:read` | List and read all projects (broader) |
| `projects:write` | Create and update projects |

### Documents

| Scope | Grants access to |
|---|---|
| `documents:read` | List, get, and download documents |
| `documents:write` | Upload and update documents |
| `documents:delete` | Delete documents |
| `ingest:write` | Trigger document ingestion/parsing |

### Chunks

| Scope | Grants access to |
|---|---|
| `chunks:read` | Read document chunks |
| `chunks:write` | Create and update chunks |

### Search

| Scope | Grants access to |
|---|---|
| `search:read` | Unified search (vector + keyword) |
| `search:debug` | Search debug information (scores, query plans) |

### Knowledge graph

| Scope | Grants access to |
|---|---|
| `graph:read` | Read objects, relationships, branches, embedding policies |
| `graph:write` | Create and update objects, relationships, branches, embedding policies |
| `graph:search:read` | Graph-specific vector search |
| `graph:search:debug` | Graph search debug information |

### Chat

| Scope | Grants access to |
|---|---|
| `chat:use` | Create sessions, send messages, use the chat API |
| `chat:admin` | Admin operations on chat sessions (delete, bulk operations) |

### Agents

| Scope | Grants access to |
|---|---|
| `agents:read` | List agents, definitions, runs; view questions |
| `agents:write` | Create, update, delete agents and definitions; trigger runs; respond to questions |

### Extraction & schema

| Scope | Grants access to |
|---|---|
| `extraction:read` | View extraction jobs, logs, statistics |
| `extraction:write` | Create, cancel, retry extraction jobs |
| `schema:read` | Read type registry, template packs, compiled types |

### Data sources & discovery

| Scope | Grants access to |
|---|---|
| `discovery:read` | List datasources, sync jobs |
| `discovery:write` | Create, update, delete datasources; trigger syncs |

### Tasks & notifications

| Scope | Grants access to |
|---|---|
| `tasks:read` | List and read tasks |
| `tasks:write` | Resolve, cancel tasks |
| `notifications:read` | List and read notifications |
| `notifications:write` | Mark notifications as read |

### User activity

| Scope | Grants access to |
|---|---|
| `user-activity:read` | View user activity log |
| `user-activity:write` | Write user activity entries |

### MCP

| Scope | Grants access to |
|---|---|
| `mcp:admin` | Manage MCP server registry, hosted servers, workspace images |

### Admin

| Scope | Grants access to |
|---|---|
| `admin:read` | Read workspace details, extraction admin, MCP registry, GitHub App, monitoring |
| `admin:write` | Write to workspace, extraction admin, MCP registry, GitHub App |

---

## Umbrella scopes

These scopes implicitly grant all listed fine-grained scopes.

!!! note "`data:read` / `data:write` are retired from token creation"
    Both umbrellas remain valid **stored** scopes: a token, device credential or
    agent share minted before the change keeps exactly the access below, and the
    server still resolves them. They are no longer **mintable** — the create/edit
    APIs, the web UI picker and the CLI reject `data:read` and `data:write` on any
    user-facing token. Request the fine-grained scopes instead; the CLI presets
    `--scopes read` and `--scopes write` expand to them.

| Umbrella scope | Implies |
|---|---|
| `data:read` | `documents:read`, `chunks:read`, `search:read`, `graph:read`, `graph:search:read`, `extraction:read`, `schema:read`, `tasks:read`, `user-activity:read`, `notifications:read` |
| `data:write` | `documents:write`, `documents:delete`, `chunks:write`, `graph:write`, `ingest:write`, `extraction:write`, `tasks:write`, `user-activity:write`, `notifications:write` |
| `agents:read` | includes `chat:use` |
| `agents:write` | includes `chat:admin` |
| `projects:write` | includes `projects:read` |

!!! note "Every umbrella implication is mintable directly"
    The fine-grained scopes `data:read` and `data:write` imply are all in the
    mintable catalogue below, so a token created after the retirement can reach
    the same REST surfaces by requesting them explicitly (`chunks:read`,
    `search:read`, `graph:search:read`, `extraction:read`, `tasks:read`,
    `user-activity:read`, `notifications:read`, and the matching writes). The
    `read` and `write` presets include them.

---

## API token scopes

When creating API tokens via the web UI, CLI or API, only the following scopes can be assigned:

| Scope | Use case |
|---|---|
| `documents:read` | Read documents and their content |
| `documents:write` | Create and update documents |
| `documents:delete` | Delete documents |
| `ingest:write` | Trigger document ingestion/parsing |
| `chunks:read` | Read document chunks |
| `chunks:write` | Create and update document chunks |
| `graph:read` | Read graph objects |
| `graph:write` | Create and update graph objects |
| `graph:search:read` | Graph-specific vector search |
| `search` | Run hybrid and semantic search (MCP) |
| `search:read` | Unified REST search (vector + keyword) |
| `journal:read` | Read the project journal |
| `journal:write` | Append to the project journal |
| `extraction:read` | View extraction jobs, logs and statistics |
| `extraction:write` | Create, cancel and retry extraction jobs |
| `tasks:read` | List and read tasks |
| `tasks:write` | Resolve and cancel tasks |
| `notifications:read` | List and read notifications |
| `notifications:write` | Mark notifications as read |
| `user-activity:read` | View the user activity log |
| `user-activity:write` | Write user activity entries |
| `schema:read` | Read-only access to type registry and template packs |
| `schema:write` | Install and modify schemas (also grants `schema:migrate`) |
| `branches:read` | List and inspect graph branches |
| `branches:write` | Create, merge and delete graph branches |
| `agents:read` | Use chat and view agent runs |
| `agents:write` | Create and manage agents, trigger runs |
| `skills:read` | Read project skills |
| `skills:write` | Create and manage project skills |
| `projects:read` | List and read projects |
| `projects:write` | Create and manage projects |
| `project:admin` | Full project admin for one project (project-bound tokens only, minted by a project admin) |
| `admin`, `admin:all` | Platform admin (requires an active `superadmin_full`) |

The retired `data:read` / `data:write` umbrellas are **not** assignable. Fine-grained internal scopes (e.g. `org:read`, `admin:write`) and the reserved marker scopes (`device:api`, `webhook:trigger`, `mcp:agent-call`, `share:agent-chat`) cannot be assigned to user-created API tokens either.

---

## Scope enforcement by feature area

| Feature area | Read scope | Write scope |
|---|---|---|
| Knowledge graph objects + relationships | `graph:read` | `graph:write` |
| Branches | `graph:read` | `graph:write` |
| Embedding policies | `graph:read` | `graph:write` |
| Documents | `documents:read` | `documents:write` |
| Document deletion | — | `documents:delete` |
| Search | `search:read` | — |
| Chat | `chat:use` | `chat:admin` |
| Agents + definitions | `agents:read` | `agents:write` |
| Datasources + discovery | `discovery:read` | `discovery:write` |
| Tasks | `tasks:read` | `tasks:write` |
| Notifications | `notifications:read` | `notifications:write` |
| Extraction jobs | `extraction:read` | `extraction:write` |
| Document chunks | `chunks:read` | `chunks:write` |
| MCP server registry | `admin:read` | `admin:write` |
| Workspace management | `admin:read` | `admin:write` |
| Workspace images | `admin:read` | `admin:write` |
