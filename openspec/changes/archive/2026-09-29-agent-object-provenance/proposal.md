## Why

The product still names a special class of knowledge as "agent memories" — a dedicated memories API, a memories subpage in the agent dashboard, and an iOS "Memories" browser — even though the underlying graph stores everything in one place. That naming forces an artificial split: an object written by an agent is "a memory" only if some memory-specific pipeline or type says so, while the same object browsed through the objects page is "just an object".

We want to retire the "agent memories" concept. The agent Memories page/browser becomes the project OBJECT browser, filtered by provenance: "objects created by / updated by this agent". There is no object-type hardcoding and no memory-specific naming. Provenance is recorded for BOTH objects and relationships, so the object browser can answer "what did this agent touch" uniformly.

## What Changes

- Record actor provenance on every object version write (already stored on `kb.graph_objects`, now populated consistently from a stamped actor context instead of hardcoding `"user"`).
- Record actor provenance on relationships for the first time: new `actor_type`/`actor_id` columns on `kb.graph_relationships` via a Goose migration, attributed on create/update/bulk relationship mutators.
- Propagate agent identity by stamping the actor on the context at the agent run boundary (`auth.WithActor`), so nested/delegated runs attribute to the ACTUAL writing agent (a sub-agent re-stamps its own id). Per-agent MCP endpoints attribute to their `endpoint.AgentID`.
- Expose a provenance filter on object listing with three modes — `created` | `updated` | `any` — that always keys on the `(actor_type, actor_id)` pair, never `actor_id` alone.
- Populate actor fields on the object and relationship API responses and on the SDK list options, so the CLI, web UI, and iOS can build agent-scoped views.
- Fix branch-merge clones (both `applyMerge` and the fast-forward clone path) so cloned rows carry the source row's actor rather than building fresh actor-less rows.
- Replace the agent dashboard's "memories browser" link with a link to objects created/updated by that agent, and replace the iOS "Memories" browser with a native object browser scoped to an agent's provenance.
- Add actor provenance filters to `memory graph objects list` (`--actor-type`, `--actor-id`, `--provenance`).

## Capabilities

### New Capabilities

- `object-provenance`: cross-cutting provenance model — actor recorded on every object version write and on relationship writes, actor exposed in object + relationship responses, object provenance filter (`created`/`updated`/`any` keyed on the `(actor_type, actor_id)` pair), agent-identity propagation (nested/delegated runs, per-agent MCP endpoint), branch-merge provenance preservation, and the documented limitations (sandbox writes, delete/restore, hard-delete drift).
- `ios-agent-object-browser`: net-new native SwiftUI object browser scoped to an agent's provenance (list objects created/updated by the agent, search, view object detail, gate on agent availability, fresh data), replacing the memory browser.
- `cli-object-provenance`: `memory graph objects list` gains actor provenance filters (`--actor-type`, `--actor-id`, `--provenance created|updated|any`).

### Modified Capabilities

- `object-browser`: add an agent-provenance filter (Created by / Updated by modes) to the object list, and reuse the same browse/detail components for an agent-scoped view.
- `agent-dashboard-ui`: replace the "link to the memories browser" requirement with a "link to objects created/updated by this agent" requirement, and drop the three memory-object requirements (list / search / show full content).

### Removed Capabilities

- `agent-memory-api`: the read-only memories proxy API is retired.
- `ios-memory-browser`: the memories-specific SwiftUI browser is retired, superseded by `ios-agent-object-browser`.

## Impact

- Server (`apps/server/domain/graph`, `apps/server/pkg/auth`, `apps/server/domain/mcp`, `apps/server/pkg/sdk`): actor stamping, relationship migration + attribution, actor filtering, and DTO/SDK field additions.
- CLI (`apps/cli`): new actor provenance flags on `graph objects list`.
- Web UI (`apps/web-ui/gateway`): agent dashboard link swap and agent-scoped object browse view (reusing the object browser).
- iOS (`apps/ios`): new SwiftUI agent-object browser; remove the memories browser.
- Database: one new Goose migration adding `actor_type`/`actor_id` to `kb.graph_relationships` (objects already carry these columns).
- Breaking change: the `agent-memory-api` and `ios-memory-browser` capabilities are removed outright.
