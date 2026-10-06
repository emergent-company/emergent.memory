## MODIFIED Requirements

### Requirement: First-class proposal kinds for writable resources

The proposal card SHALL provide first-class renderers for the operator's writable resources, each using the same body shape the corresponding write tool consumes:

- `skill` — `{name, description, prompt, tools, bannedTools}`
- `agent` — `{name, description, model, systemPrompt, tools, skills, bannedTools, visibility}`
- `mcp_server` — `{name, type, url, headers, enabled, enabledTools, disabledTools}` (write-path shape; `headers` intentionally omitted from the rendered card)
- `provider` — `{provider, baseUrl, models}` (secret masked, never echoed)
- `object` — `{entities, relationships}`

#### Scenario: Skill proposal is previewed
- **WHEN** a question carries a `proposal` whose `kind` is `skill`
- **THEN** the card SHALL show the skill's name, description, and prompt, and its tool/banned-tool lists

#### Scenario: Agent proposal is previewed
- **WHEN** a question carries a `proposal` whose `kind` is `agent`
- **THEN** the card SHALL show the agent's name, model, and system prompt, and its tool/skill/banned-tool lists

#### Scenario: MCP server proposal is previewed
- **WHEN** a question carries a `proposal` whose `kind` is `mcp_server`
- **THEN** the card SHALL show the server's name, type, URL, and the tools it enables/disables

#### Scenario: MCP server proposal masks auth headers
- **WHEN** a question carries a `proposal` whose `kind` is `mcp_server` and whose body includes `headers`
- **THEN** the card SHALL NOT render the `headers` field or any auth credentials it carries

#### Scenario: Provider proposal masks the secret
- **WHEN** a question carries a `proposal` whose `kind` is `provider`
- **THEN** the card SHALL show the provider slug, base URL, and models, and SHALL NOT render the API key

#### Scenario: Object proposal is previewed
- **WHEN** a question carries a `proposal` whose `kind` is `object`
- **THEN** the card SHALL list the proposed entities with their type and properties and the proposed relationships with their source and target
