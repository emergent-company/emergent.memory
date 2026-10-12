## Why

The graph type-scope allowlists (which object types and relationship types an
agent may see and query) were added to the agent **General** settings form
(#1695). General is a grab-bag of every agent property — name, description,
system prompt, language, project context, appearance, visibility — and the two
type pickers made it long and hard to scan. They are a distinct concern (the
agent's graph scope), so they belong on their own settings subpage.

Feedback on the live page (issue #1747) asked to move the graph-related agent
settings (object types and relationships) to a separate settings page called
**Graph**.

## What Changes

- Add a **Graph** subpage to the agent Settings group at
  `/agents/:id/settings/graph`, alongside General / Model / Tools / Skills /
  Delegation / MCP sharing. The Settings group now has seven children.
- Move the **Object types** and **Relationship types** allowlist pickers off the
  General form onto the Graph subpage, with a short helper line. They keep the
  PRG save pattern the other non-auto-saving subpages use.
- Move the persistence to a new `applyAgentGraphSection` applier and the POST
  handler `uiAgentUpdateGraph`; `applyAgentGeneralSection` no longer touches
  `Config["objectTypes"]` / `Config["relationshipTypes"]`, so a General save
  cannot wipe the graph scope.
- **Preserve the "cleared all" vs "untouched" contract.** Each picker keeps its
  hidden companion `<input>`: a submitted Graph form always carries
  `objectType` / `relationshipType` (present, possibly blank), so clearing every
  box reads as an explicit "allow all types" (key removed) while a partial body
  that omits the field leaves the stored allowlist untouched. Dropping the
  companion inputs would silently turn "cleared every box" into "leave the
  stored scope as-is".
- The `/agents/:id/settings/graph` getter loads the project's compiled graph
  types (moved from the General loader branch); General now loads no catalog.

No server/API change: the allowlists still persist to the same agent
`config.objectTypes` / `config.relationshipTypes`.

## Impact

- Affected specs: `agent-dashboard-ui` (Settings nav group), `web-agent-settings`
  (type-scope control location).
- Affected code: `apps/web-ui/gateway/agent.templ`, `agent.go`, `main.go`, and
  the agent settings unit tests.
