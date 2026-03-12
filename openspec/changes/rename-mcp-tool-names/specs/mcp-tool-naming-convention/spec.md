## ADDED Requirements

### Requirement: MCP tool names follow area-action kebab-case format
All MCP tool names exposed by the Memory server SHALL use the `area[-noun]-action` kebab-case format. The area segment identifies the primary resource domain. An optional noun segment identifies a sub-resource. The action segment is the last segment and identifies the operation (e.g., `list`, `get`, `create`, `update`, `delete`).

#### Scenario: Tool name uses hyphens not underscores
- **WHEN** a tool definition is registered with the MCP server
- **THEN** the tool name SHALL contain only lowercase letters, digits, and hyphens
- **THEN** the tool name SHALL NOT contain underscores

#### Scenario: Tool name starts with a resource area
- **WHEN** a tool definition is registered
- **THEN** the first segment of the tool name SHALL identify the resource area (e.g., `entity`, `agent`, `schema`, `document`, `search`)

#### Scenario: Tool name ends with an action verb
- **WHEN** a tool definition is registered
- **THEN** the last segment of the tool name SHALL be an action verb (`list`, `get`, `create`, `update`, `delete`, `search`, `query`, `fetch`, `install`, `configure`, `assign`, `uninstall`, `revoke`, `pause`, `resume`, `restore`, `traverse`, `inspect`, `respond`, `preview`, `version`)

### Requirement: Complete rename mapping is applied to all 97 tools
The server SHALL rename all existing tool names according to the mapping defined in design.md. No tool SHALL retain its old `verb_noun` snake_case name after this change is deployed.

#### Scenario: Old tool name is no longer available
- **WHEN** an MCP client requests the tool list
- **THEN** no tool with a `verb_noun` snake_case name (e.g., `list_agents`, `assign_schema`) SHALL appear in the response

#### Scenario: New tool name is available for every renamed tool
- **WHEN** an MCP client requests the tool list
- **THEN** every new `area[-noun]-action` name from the mapping (e.g., `agent-list`, `schema-assign`) SHALL appear in the response

### Requirement: Dispatch routing is updated to match new names
For every renamed tool, the server's internal dispatch switch block SHALL use the new tool name as the case key.

#### Scenario: Calling a tool by its new name succeeds
- **WHEN** an MCP client calls a tool using its new `area[-noun]-action` name
- **THEN** the server SHALL route the call to the correct handler and return the expected result

#### Scenario: Calling a tool by its old name returns an error
- **WHEN** an MCP client calls a tool using its old `verb_noun` name
- **THEN** the server SHALL return a tool-not-found error (no silent fallback)

### Requirement: Tool parameters and behavior are unchanged
Renaming a tool SHALL NOT alter its input parameters, output format, or handler logic.

#### Scenario: Parameters are identical before and after rename
- **WHEN** comparing the tool definition before and after this change
- **THEN** the `InputSchema` (parameter names, types, required fields) SHALL be identical
- **THEN** the tool description MAY be updated only to reflect the new name; all other description content SHALL remain the same
