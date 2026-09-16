## Why

The iOS agent list is over-styled: agents render as large custom cards with heavy padding, staggered entrance animations, enabled badges, and a prominent "Add Agent" button, and each agent has a Settings tab that lets the user edit, remove, and enable/disable agents. That does not feel like a native chat app, and agent configuration is moving off the phone to web/desktop. The app should instead present agents as a plain, native, read-only list using standard iOS styling.

## What Changes

- Remove the "Add Agent" button, the add-agent sheet (`AddAgentView`), and the empty-state "add first agent" call-to-action. Agents can no longer be created from the phone.
- Remove per-agent configuration from the phone: drop the second-level Settings tab (`AgentSettingsView`), so editing, removing, and enabling/disabling agents are no longer available on iOS. The agent second level becomes Conversation + Sessions only.
- Replace the custom `AgentPanelRow` panels (large corner-radius cards, staggered entrance animation, enabled badge, selection checkmark, custom grid padding, oversized min-height) with a native SwiftUI `List` using standard row styling, standard insets, and system fonts — like a typical chat app's conversation list.
- Make the empty state read-only: when no agents exist, show a message directing the user to configure agents from web/desktop instead of an add-agent CTA.
- Update localizable strings: remove "Add Agent" and other add/settings strings that are no longer referenced.

## Capabilities

### New Capabilities

(none)

### Modified Capabilities

- `ios-agent-management`: agent listing becomes read-only on iOS. Remove the "Add an agent", "Remove an agent", and "Enable and disable agents" requirements; replace the "Empty-agent call to action" requirement with a read-only empty state that directs users to web/desktop; render the agent list as native `List` rows with standard iOS spacing, margins, and font sizes.

## Impact

- `client/ios/VoiceAgent/App/AppShellView.swift`: remove the `addPresented` sheet, the `addButton()`, the add-agent empty-state CTA, and replace the custom `AgentPanelRow` list with a native `List`.
- `client/ios/VoiceAgent/App/AgentLevelView.swift`: remove the Settings tab, leaving Conversation and Sessions.
- `client/ios/VoiceAgent/Agents/AddAgentView.swift`: delete.
- `client/ios/VoiceAgent/Agents/AgentSettingsView.swift`: delete.
- `client/ios/VoiceAgent/Agents/AgentStore.swift`: remove `add`, `remove`, `setEnabled`, and `fetchDetail` (and the now-unused `AgentDraft`/detail model wiring).
- `client/ios/VoiceAgent/Agents/ControlPlaneClient.swift`: remove `createAgent`, `updateAgent`, `deleteAgent`, `activateAgent`, `deactivateAgent`, and `getAgent` (and `AgentDraft` payload encoding) once no longer referenced.
- `client/ios/VoiceAgent/Localizable.xcstrings`: remove now-unused add-agent and agent-settings strings.
- Tests: update `AgentStoreTests.swift` and `ControlPlaneClientTests.swift` to drop add/remove/enable-disable coverage.
