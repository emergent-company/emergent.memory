## Context

The agent picker (`AppShellView`) renders agents as custom `AgentPanelRow` cards inside a `ScrollView` + `VStack`: large corner-radius backgrounds, a staggered entrance animation, an enabled badge, a selection checkmark, and custom `N×grid` padding with an oversized `minHeight`. A bottom safe-area `addButton()` and an empty-state `emptyView()` both open the `AddAgentView` sheet. The level-2 `AgentLevelView` exposes a Settings tab (`AgentSettingsView`) for editing, removing, and enabling/disabling agents. Motivation lives in proposal.md — Why.

## Goals / Non-Goals

**Goals:**

- Render the agent picker as a native SwiftUI `List` with system-standard insets, row height, and fonts.
- Remove all agent-configuration affordances from the phone (add, edit, remove, enable/disable) and make the list read-only.
- Delete now-dead code and its tests so the client no longer calls the mutation endpoints.

**Non-Goals:**

- No server/control-plane API changes; the mutation endpoints stay and web/desktop keeps using them.
- No full navigation refactor of level 2 (header chrome, Conversation/Sessions internals) — those stay as-is except dropping the Settings tab.
- No change to QR-based app-level authentication; it is backend reachability, not agent config.

## Decisions

### 1. Use a native `List`, not a custom `ScrollView` + cards

Replace the `ScrollView { VStack { panels() } }` with a `List` and drop `AgentPanelRow` entirely.

- **Chosen**: `List` with `.listStyle(.insetGrouped)` for the picker. System-provided grouped insets and separators deliver the "standard paddings, margins, and font sizes" the user asked for, with no hand-rolled metrics.
- **Alternative**: `.listStyle(.plain)` (closer to Messages' flat list). Rejected for the picker because `.insetGrouped` gives automatic standard margins and is the canonical native selection-list look with zero custom padding.
- **Alternative**: Keep `ScrollView` and just reduce padding. Rejected — it would still be hand-styled and would not satisfy "native iOS elements as much as possible."

Each row is a `Button` that calls the existing `select(agent)` (persist selection + append `.agent` route). Using a `Button` keeps the current path-append flow and avoids reworking selection persistence into a `NavigationLink(value:)`; `List` supplies the native row chrome and tappable area. Row content: agent name (title) + `flowType` (subtitle, when present). The enabled badge, selection checkmark, and staggered animation are removed.

### 2. Remove the agent-configuration surface

- Delete `AddAgentView.swift` and `AgentSettingsView.swift`.
- Remove the `addPresented` sheet and `addButton()` from `AppShellView`; replace `emptyView()` with a read-only `ContentUnavailableView` directing users to web/desktop.
- Remove the Settings tab from `AgentLevelView`, leaving Conversation and Sessions.

### 3. Prune dead model and client code

Remove what only the add/settings flows consumed:

- `AgentStore`: drop `add`, `remove`, `setEnabled`, `fetchDetail`.
- `ControlPlaneClient`: drop `createAgent`, `updateAgent`, `deleteAgent`, `activateAgent`, `deactivateAgent`, `getAgent`, and `AgentPayload`/`payload(for:)`.
- `Agent`: drop the detail-only fields `systemPrompt`, `model`, `tools`, and delete the `AgentModel` and `AgentDraft` structs. Keep the list-summary fields (`id`, `name`, `flowType`, `toolCount`, `isDefault`, `enabled`) — they are defensively decoded from `GET /api/agents` and harmless even if a couple are no longer surfaced.

### 4. Subtitle content

Show agent name + `flowType` only; drop the tool-count segment and the enabled/disabled badge. Tool count and enabled state are configuration details that have no place in a read-only chat-style list.

### 5. Localizable strings

Remove unused keys (`agents.add*`, `agents.addFirst`, `agents.settings*`, `agents.enabled`, `agents.disabled`, `agents.tools.count`) and add one string for the read-only empty state. Keep failure/retry/QR strings — the load-failure path is unchanged.

### 6. Tests

Update `ControlPlaneClientTests.swift` and `AgentStoreTests.swift` to remove coverage of the deleted mutation methods. The read-only list behavior is a view concern; its verifiable behavior (fetch + display + read-only empty state) remains covered by the existing list/load tests.

## Risks / Trade-offs

- **[Removing config entirely may strand a phone user with no way to fix an empty or disabled list]** → Mitigation: the read-only empty state explicitly directs users to web/desktop; the app remains a consumer, not a manager, of agents.
- **[Native `List` row tap vs the current path-append flow]** → Mitigation: keep the `select(agent)` path-append call inside a `Button` row, so navigation and persistence behavior is unchanged; no new navigation abstraction.
- **[Deleting detail fields from `Agent` could break decoding if the server changes shape]** → Mitigation: decoding is already defensive (`decodeIfPresent` + fallback); summary fields are preserved and detail fields were already only surfaced on the settings screen.
- **[Over-pruning could remove a method still referenced elsewhere]** → Mitigation: grep for each symbol before deletion (see the pre-implementation grep in tasks.md); verify with `go`-free Swift build via `tools/ios-build-mac.sh`.

## Migration Plan

- Client-only change; no data or API migration. Deploy = rebuild the iOS app (`tools/ios-build-mac.sh`).
- Rollback = revert the commit. The control-plane API is untouched, so server state and web/desktop behavior are unaffected.
