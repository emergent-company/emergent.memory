## MODIFIED Requirements

### Requirement: Bounded umbrella expansion of role scope sets
When evaluating authorization the system SHALL expand a role's scope set through the umbrella scope implication relation, so a role-derived write scope also satisfies the fine-grained scopes it implies: `schema:write` additionally grants `schema:migrate`, `agents:write` additionally grants `chat:admin`, and `data:write` additionally grants the related write scopes and `journal:write`. This expansion is deliberate and accepted. The umbrella scope implication relation SHALL be defined in exactly one place in the codebase; every consumer SHALL derive its expansion from that single definition rather than maintaining a parallel implication table. Expansion SHALL be single-level: an implied scope is not itself expanded again, so `data:write` reaches `schema:write` but not `schema:migrate`. The system SHALL pin the exact expanded result for each canonical role with an explicit set-equality test, and the expansion SHALL NOT reach any scope excluded from the role sets (`admin*`, `mcp:admin`, `org:*`, `project:invite:create`, `account:*`).

#### Scenario: Expanded viewer set is pinned
- **WHEN** the `project_viewer` role scopes are expanded
- **THEN** the result is exactly the viewer read-only scopes plus the read scopes they imply (`search`, `journal:read`, `chat:use`, `skills:read`, and the implied `*:read` scopes), with no write scope

#### Scenario: Expanded user set is pinned
- **WHEN** the `project_user` role scopes are expanded
- **THEN** the result is exactly the expanded viewer set plus `data:write` and the write scopes it implies, including `schema:write` and `journal:write`

#### Scenario: Expanded admin set is pinned and reaches no excluded scope
- **WHEN** the `project_admin` role scopes are expanded
- **THEN** the result is exactly the expanded user set plus `agents:write`, `chat:admin`, `skills:write`, and `schema:migrate`, and contains no `admin*`, `mcp:admin`, `org:*`, `project:invite:create`, or `account:*` scope

#### Scenario: Expansion is single-level
- **WHEN** a scope set containing only `data:write` is expanded
- **THEN** the result contains `schema:write` but does not contain `schema:migrate`

## ADDED Requirements

### Requirement: Single source of truth for umbrella-scope implications
The umbrella scope implication relation SHALL be defined exactly once. The system SHALL expose one canonical expansion operation, and every package that needs to know which scopes an umbrella scope implies SHALL derive its answer from that operation. No package SHALL maintain a second, hand-maintained implication table. A consumer whose vocabulary is narrower than the canonical relation SHALL derive its view by projecting the canonical expansion, never by re-listing implications.

#### Scenario: Canonical relation is the only definition
- **WHEN** a package must resolve whether scope B is implied by scope A
- **THEN** it consults the canonical expansion operation rather than a package-local table

#### Scenario: A canonical change propagates to every consumer
- **WHEN** the canonical relation gains or loses an implication
- **THEN** every consumer's view changes accordingly, with no second implication table to update

### Requirement: MCP tool-surface projection of umbrella implications
The MCP tool surface SHALL honor an implied umbrella scope only when the implied scope can gate an MCP tool, i.e. it is the required scope of some tool in the MCP catalog. The MCP view SHALL be the canonical expansion projected onto the tool-scope vocabulary, with explicitly held scopes always retained. The tool-scope vocabulary SHALL be derived from the tool catalog rather than hand-listed. The account/organisation/global-admin scope families (`admin:read`, `admin:write`, `mcp:admin`, `org:*`, `project:invite:create`, `account:*`) SHALL NOT be reachable through any expansion on the MCP surface. The system SHALL assert that the MCP view equals the canonical projection, and SHALL pin the tool-visible result for each umbrella scope.

#### Scenario: Data read reconciles to schema read
- **WHEN** an API token holding `data:read` is used against the MCP surface
- **THEN** it satisfies `schema:read` and can list and call the `schema:read` tools

#### Scenario: Data write reconciles to schema write
- **WHEN** an API token holding `data:write` is used against the MCP surface
- **THEN** it satisfies `schema:write` and can list and call the `schema:write` tools

#### Scenario: Data write does not reach schema migrate
- **WHEN** an API token holding `data:write` is used against the MCP surface
- **THEN** it does not satisfy `schema:migrate`

#### Scenario: Admin-all tool-visible set is unchanged
- **WHEN** an API token holding `admin:all` is used against the MCP surface
- **THEN** the set of tools it can see and call is unchanged from before the projection

#### Scenario: No expansion reaches an excluded family
- **WHEN** any umbrella scope (including `admin:all`) is expanded for the MCP surface
- **THEN** the result contains no `admin:read`, `admin:write`, `mcp:admin`, `org:*`, `project:invite:create`, or `account:*` scope

#### Scenario: MCP view equals the canonical projection
- **WHEN** the MCP effective scope set is computed for any scope set
- **THEN** it equals the explicit scopes unioned with the canonical expansion restricted to the tool-scope vocabulary

#### Scenario: Vocabulary covers the tool catalog
- **WHEN** every tool in the MCP catalog is inspected
- **THEN** each tool's required scope is present in the derived tool-scope vocabulary
