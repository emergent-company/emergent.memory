## 1. Catalog (server)

- [x] 1.1 Add `apps/server/pkg/a2ui/` Go types for `createSurface`/`updateComponents`/`updateDataModel`/`deleteSurface` + a component catalog for the 8 cards (Go-native registry, no JSON-Schema file — see design.md)
- [x] 1.2 Add catalog validator (`Validate(messages) error`) using structural validation (no new dependency)
- [x] 1.3 Unit tests: each card validates; unknown component id fails validation; malformed envelope fails validation

## 2. Executor emission

- [x] 2.1 Add `StreamEventA2UI` (`surfaceId`, `messages []a2ui.Message`) to `StreamEvent` in `apps/server/domain/agents/executor.go`
- [x] 2.2 Add A2UI extraction (fenced ```a2ui block) + catalog validation in the executor; invalid output falls back to text and never errors the run
- [x] 2.3 Unit tests: valid fenced block → `StreamEventA2UI`; invalid block → no event + text continues; no block → no event

## 3. Chat SSE transport

- [x] 3.1 Add `ui` event type + payload struct to `apps/server/pkg/sse/events.go`
- [x] 3.2 Emit `ui` event from `streamCallback` (`apps/server/domain/chat/handler.go`) for `StreamEventA2UI`
- [x] 3.3 Unit tests: `StreamEventA2UI` → SSE `ui` event with `surfaceId` + messages

## 4. Gateway rewriter + web render

- [x] 4.1 `rewriteChatStream` (`apps/web-ui/gateway/sse_markdown.go`): pass through `ui` events
- [x] 4.2 `makeHandleEvent` (`webui/static/js/chat-host.js`): add `ui` case → component-registry builder
- [x] 4.3 Dependency-free plain-JS card renderer in `chat-components.js` (deferred `@a2ui/lit` npm island — see design.md)
- [x] 4.4 Wire button action dispatch (`surfaceId` + action → `a2ui:action` CustomEvent; resume/respond wiring is a follow-up)
- [x] 4.5 JS `node --check` on all touched files; renderer uses `textContent`/`escapeHTML` only

## 5. A2A transport

- [x] 5.1 `a2aStreamTranslator.translate` (`apps/server/domain/agents/a2a_stream.go`): `StreamEventA2UI` → data-part `artifactUpdate` (mimeType `application/a2ui+json`, `data` = array, stable `artifactId`)
- [x] 5.2 Advertise A2UI extension in the AgentCard (discovery) with `supportedCatalogIds`
- [x] 5.3 Route surface actions (in `message.metadata.a2uiAction`) as a new turn in the existing context — distinct from question-answer resume
- [x] 5.4 Unit tests: translate produces ordered data-part artifacts; extension advertised; surface-action metadata parse/route

## 6. iOS renderer

- [x] 6.1 Add `ui`/component case to the `ChatEvent` decoder (`apps/ios/VoiceAgent/Chat/ChatEvent.swift` + `ChatUISurface.swift`)
- [x] 6.2 Add SwiftUI component registry + `Surface` renderer (`ChatUISurfaceView.swift`, wired in `ChatView.swift`) for the 8 cards
- [x] 6.3 Wire action round-trip (`surfaceId` + action back over `lk.chat.decision`)
- [x] 6.4 Build via Xcode simulator (`xcodebuild ... generic/platform=iOS Simulator`, on `mcj-mini`)
- [ ] 6.5 Manual smoke of one card on a running agent

## 7. Verify

- [ ] 7.1 `go build ./...` + `go test ./...` in `apps/server` and `apps/web-ui/gateway`
- [ ] 7.2 `templ generate` + gateway `task lint`
- [ ] 7.3 iOS `xcodebuild -scheme VoiceAgent -destination 'generic/platform=iOS Simulator' CODE_SIGNING_ALLOWED=NO`
- [ ] 7.4 `openspec validate --changes` clean
