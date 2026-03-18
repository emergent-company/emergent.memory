## MODIFIED Requirements

### Requirement: MCP tool names follow area-action kebab-case format
All MCP tool names exposed by the Memory server SHALL use the `area[-noun]-action` kebab-case format. The area segment identifies the primary resource domain. An optional noun segment identifies a sub-resource. The action segment is the last segment and identifies the operation (e.g., `list`, `get`, `create`, `update`, `delete`).

#### Scenario: Tool name uses hyphens not underscores
- **WHEN** a tool definition is registered with the MCP server
- **THEN** the tool name SHALL contain only lowercase letters, digits, and hyphens
- **THEN** the tool name SHALL NOT contain underscores

#### Scenario: Tool name starts with a resource area
- **WHEN** a tool definition is registered
- **THEN** the first segment of the tool name SHALL identify the resource area (e.g., `entity`, `agent`, `schema`, `document`, `search`, `embedding`, `provider`, `skill`, `token`, `trace`)

#### Scenario: Tool name ends with an action verb
- **WHEN** a tool definition is registered
- **THEN** the last segment of the tool name SHALL be an action verb (`list`, `get`, `create`, `update`, `delete`, `search`, `query`, `fetch`, `install`, `configure`, `assign`, `uninstall`, `revoke`, `pause`, `resume`, `restore`, `traverse`, `inspect`, `respond`, `preview`, `version`, `sync`, `trigger`, `test`)

### Requirement: Complete rename mapping is applied to all tools
The server SHALL expose all tools under their current `area[-noun]-action` kebab-case names. The MCP README SHALL document the current canonical tool names, not legacy names from a prior implementation phase.

#### Scenario: Old legacy names are not in documentation
- **WHEN** the MCP README tools table is rendered
- **THEN** legacy snake_case names (`hybrid_search`, `semantic_search`, `find_similar`, `traverse_graph`, `list_relationships`, `batch_create_entities`, `batch_create_relationships`, `restore_entity`, `list_tags`) SHALL NOT appear as current tool names

#### Scenario: Current tool names appear in README
- **WHEN** an MCP client requests the tool list
- **THEN** the tool names returned SHALL match those documented in the MCP README (e.g., `search-hybrid`, `search-semantic`, `search-similar`, `graph-traverse`, `relationship-list`, `entity-create`, `entity-restore`, `tag-list`)

### Requirement: MCP README tool count reflects current implementation
The MCP README SHALL accurately report the approximate number of tools currently available, covering tools across all tool files (service.go, agent_ext_tools.go, documents_tools.go, token_tools.go, query_tools.go, provider_tools.go, skills_tools.go, trace_tools.go, embeddings_tools.go).

#### Scenario: README tool count is not outdated
- **WHEN** a developer reads the MCP README
- **THEN** the stated tool count SHALL NOT reference the obsolete "18" or "29" counts from earlier implementation phases

### Requirement: MCP README authentication examples use Authorization: Bearer
The MCP README SHALL show `Authorization: Bearer <api-token>` as the authentication header in all Quick Start and example code blocks, consistent with the developer guide and the actual server handler behaviour.

#### Scenario: README Quick Start uses Bearer auth
- **WHEN** the MCP README Quick Start section is rendered
- **THEN** HTTP examples SHALL use `Authorization: Bearer <token>` rather than `X-API-Key: <key>`

#### Scenario: README notes both accepted auth methods
- **WHEN** the Authentication section is rendered
- **THEN** the README SHALL note that both `Authorization: Bearer` and `X-API-Key` headers are accepted, with `Bearer` as the recommended method

### Requirement: Dispatch routing is updated to match new names
For every tool, the server's internal dispatch switch block SHALL use the current `area[-noun]-action` name as the case key.

#### Scenario: Calling a tool by its current name succeeds
- **WHEN** an MCP client calls a tool using its `area[-noun]-action` name
- **THEN** the server SHALL route the call to the correct handler and return the expected result

### Requirement: Tool parameters and behavior are unchanged
Renaming legacy references in documentation SHALL NOT alter input parameters, output format, or handler logic.

#### Scenario: Parameters are identical before and after documentation update
- **WHEN** comparing tool behaviour before and after this documentation change
- **THEN** the `InputSchema` and handler logic SHALL be identical — only documentation references change
