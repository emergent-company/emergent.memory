## Why

The web chat (`/chat`) renders a full chat experience — rich markdown, live tool-call chips, thinking/reasoning blocks, interactive approval and `ask_user` cards, a stop button, and suggested prompts — because its text path runs over HTTP `/api/chat` SSE. The iOS chat runs over LiveKit and only receives plain `.agentTranscript` text, so it renders inline-only markdown and shows none of the tool, thinking, or approval activity. This change closes that gap so iOS text chat matches the web experience.

## What Changes

- Add a full markdown renderer to the iOS chat: fenced code blocks with syntax highlighting, tables, inline images, task lists, strikethrough, and tappable links — replacing the current `AttributedString(markdown:)` inline-only fallback.
- Add a typing indicator while the agent is working and a stop/interrupt affordance in the composer.
- Add suggested-prompt chips to the empty chat state.
- Stream rich chat events from the bridge worker to the iOS client over LiveKit (tool calls, thinking blocks, approval requests, `ask_user` questions), and render them as expandable chips and interactive cards in the live chat.
- Let the iOS client answer approvals and questions and send that decision back to the worker so the run resumes.
- Reuse the existing tool-call detail components from the recorded-session timeline so live and historical tool evidence share one rendering path.

## Capabilities

### New Capabilities

- `ios-chat`: the iOS text-chat experience — rich markdown rendering, message typing/stop affordances, suggested prompts, and live tool/thinking/approval/question cards.

### Modified Capabilities

- `ios-agent-client`: extend the LiveKit text-stream protocol the client consumes so the worker can emit rich chat events (tool calls, thinking, approvals, questions) and receive the client's decisions.

## Impact

- `client/ios/VoiceAgent/Chat/ChatView.swift` and `ChatInputView.swift`: markdown renderer, message model, typing indicator, stop button, suggested prompts, tool/thinking/approval cards.
- `client/ios/VoiceAgent/Alfred/AlfredSessionController.swift`: consume rich chat events and send approval/question decisions back to the worker.
- `client/ios/VoiceAgent/Sessions/SessionDetailView.swift`: shared tool-call detail components reused by the live chat.
- `alfred_bridge/worker.py`: stream tool/thinking/approval/question events over a new LiveKit text-stream topic and subscribe to the client's decision stream.
- New Swift markdown rendering dependency or a hand-rolled renderer (resolved in design).
