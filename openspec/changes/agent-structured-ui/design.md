## Design

### Catalog (single source of truth)

A JSON Schema catalog (`apps/server/pkg/a2ui/catalog.json`, A2UI v0.9.1 `basic` catalog shape) defines our component set. Each card is one component id:

| component | purpose | key props |
|---|---|---|
| `proposal` | review/accept/reject a proposed change | `kind`, `summary`, `body`, actions |
| `approval` | gate a tool call | `tool`, `input` (mono), actions |
| `question` | structured input | `prompt`, `interactionType` (text/choice/multi/bool), `options` |
| `code` | code block / diff | `lang`, `code` |
| `entity` | graph object preview | `type`, `properties[]`, `relationships[]` |
| `object-form` | schema-driven data entry | `fields[]` (type/widget/enum) |
| `todo` | checkable task list | `items[]` |
| `result` | key/value or table | `rows[]` |

The catalog is additive; unknown components fall back to a summary/text render and never error.

### Envelope

Four A2UI messages, JSONL, `version: "v0.9.1"`:

- `createSurface {surfaceId, catalogId}` — must precede updates; one component has `id: "root"`.
- `updateComponents {surfaceId, components[]}` — flat adjacency list; same `id` = update.
- `updateDataModel {surfaceId, path, value}` — JSON Pointer data binding.
- `deleteSurface {surfaceId}`.

The server assigns a stable `surfaceId` per card (e.g. `proposal-<questionId>`, `approval-<toolCallId>`).

### Emission

The executor recognizes A2UI two ways: (1) fenced ```a2ui JSONL blocks in model output, and (2) a dedicated structured tool output. Extracted messages are validated against the catalog; valid → `StreamEventA2UI{surfaceId, messages[]}`; invalid/absent → the run proceeds as markdown text. Validation failure never fails the run.

### Transport

- **Chat SSE**: a new `{"type":"ui","surfaceId":...,"messages":[...]}` event, emitted by `streamCallback` and passed through `rewriteChatStream` → `makeHandleEvent` → the web component registry.
- **A2A**: `a2aStreamTranslator.translate(StreamEventA2UI)` → `artifactUpdate` with a `data` part, `metadata.mimeType = "application/a2ui+json"`, `data = []a2uiMessage`, `artifactId = "a2ui-<surfaceId>"`. `createSurface` precedes updates; ordering is preserved.

### Action round-trip

A client action (`Button.action.event` or `functionCall`) returns to the server as a follow-up message on the same `contextId` (or `taskId`) carrying `{surfaceId, action}` under `message.metadata["a2uiAction"]`. The server detects it, resolves the context, formats the action as the agent's user message, and starts a **new turn in the existing context** — never the `INPUT_REQUIRED` question-answer resume (`AnswerQuestion`). The agent's reply (possibly a new ```a2ui fence) streams updated surfaces. A surface action does not require a text part.

### Rendering

- **Web**: a Lit `@a2ui/lit` `<a2ui-surface>` island inside the templ gateway, backed by `@a2ui/web_core` `MessageProcessor`; actions POST back to the gateway. Falls back to the existing templ `proposal` cards until the catalog is fully wired.
- **iOS**: a SwiftUI `Surface` renderer consuming the same JSONL; a component registry maps catalog ids to native SwiftUI views. (Use the official `A2UISwiftCore`/`A2UISwiftUI` packages pinned to a commit, or a thin bespoke renderer for our 8 components — decided at implementation time.)

### Security

- Catalog allowlist: the renderer renders ONLY catalog components; unknown ids → fallback.
- No code execution: actions are `event` (→ agent) or `functionCall` (→ pre-registered client fn); no arbitrary JS.
- Secrets never echoed (provider/api-key, mcp headers masked) — same rule as the existing proposal cards.
- `a2uiClientDataModel` (when `sendDataModel: true`) is point-to-point; stripping for multi-agent fan-out is out of scope (leaf server today).

## Implementation notes (deviations from the original design)

- **Catalog**: implemented as a Go-native component registry + structural validator (`pkg/a2ui`), not a JSON Schema file. No new dependency; a JSON-Schema export can be added later if a spec-conformant artifact is needed.
- **Web rendering**: implemented as a dependency-free plain-JS card renderer (no `@a2ui/lit` npm island). The `@a2ui/lit`/`@a2ui/web_core` island remains a follow-up for byte-identical A2UI rendering.
- **Surface-action routing**: implemented as a new turn in the existing context (not a literal resume of the completed run, which is terminal). Documented in the Action round-trip section.
