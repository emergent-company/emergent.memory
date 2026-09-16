## 1. Remove agent-configuration surface

- [x] 1.1 Delete `client/ios/VoiceAgent/Agents/AddAgentView.swift` and `client/ios/VoiceAgent/Agents/AgentSettingsView.swift`; verify no remaining references with `grep -rn "AddAgentView\|AgentSettingsView" client/ios/VoiceAgent/`
- [x] 1.2 Remove the Settings tab from `client/ios/VoiceAgent/App/AgentLevelView.swift` (drop `settingsTab` and the `missingAgentView` settings path), leaving Conversation + Sessions; verify with `tools/ios-build-mac.sh`
- [x] 1.3 Remove the `addPresented` sheet, `addButton()`, and the add-agent `emptyView()` CTA from `client/ios/VoiceAgent/App/AppShellView.swift`; replace the empty state with a read-only `ContentUnavailableView` directing to web/desktop; verify with `tools/ios-build-mac.sh`

## 2. Native list rendering

- [x] 2.1 Replace the `ScrollView` + `VStack` + `AgentPanelRow` picker in `AppShellView.swift` with a `List` (`.insetGrouped`) whose rows are `Button`s that call `select(agent)`; verify the list renders with system insets via simulator screenshot in DevTools
- [x] 2.2 Row content: show agent name (title) + `flowType` (subtitle); remove the enabled badge, selection checkmark, staggered entrance animation, and tool-count subtitle segment; verify with `tools/ios-build-mac.sh`

## 3. Prune dead model and client code

- [x] 3.1 Remove `add`, `remove`, `setEnabled`, and `fetchDetail` from `client/ios/VoiceAgent/Agents/AgentStore.swift`; verify `grep -rn "agentStore.add\|agentStore.remove\|setEnabled\|fetchDetail" client/ios/VoiceAgent/` returns nothing
- [x] 3.2 Remove `createAgent`, `updateAgent`, `deleteAgent`, `activateAgent`, `deactivateAgent`, `getAgent`, and `AgentPayload`/`payload(for:)` from `client/ios/VoiceAgent/Agents/ControlPlaneClient.swift`; verify `tools/ios-build-mac.sh`
- [x] 3.3 Remove the `AgentDraft` and `AgentModel` structs and the detail-only fields `systemPrompt`, `model`, `tools` from `client/ios/VoiceAgent/Agents/Agent.swift`; verify `tools/ios-build-mac.sh`

## 4. Localization

- [x] 4.1 Remove unused keys (`agents.add*`, `agents.addFirst`, `agents.settings*`, `agents.enabled`, `agents.disabled`, `agents.tools.count`) from `client/ios/VoiceAgent/Localizable.xcstrings` and add a read-only empty-state string; verify `tools/ios-build-mac.sh`

## 5. Tests

- [x] 5.1 Update `client/ios/VoiceAgentTests/AgentStoreTests.swift` to drop add/remove/setEnabled/fetchDetail coverage and keep load/select coverage; verify `tools/ios-build-mac.sh --test`
- [x] 5.2 Update `client/ios/VoiceAgentTests/ControlPlaneClientTests.swift` to drop mutation-endpoint coverage and keep list/decode/error coverage; verify `tools/ios-build-mac.sh --test`

## 6. Final verification

- [x] 6.1 Run the full simulator build and unit-test suite with `tools/ios-build-mac.sh --test` and confirm all tests pass
- [x] 6.2 Manual check in the simulator: agent list renders as a native list with standard spacing, no "Add Agent" button, empty/error states behave, and selecting an agent opens Conversation + Sessions only
