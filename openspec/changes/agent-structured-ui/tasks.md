## 1. Catalog (server)

- [ ] 1.1 Add `apps/server/pkg/a2ui/catalog.json` (A2UI v0.9.1 schema) for the 8 card components + Go types for `createSurface`/`updateComponents`/`updateDataModel`/`deleteSurface`
- [ ] 1.2 Add catalog validator (`Validate(messages) error`) using a JSON Schema validator
- [ ] 1.3 Unit tests: each card validates; unknown component id fails validation; malformed envelope fails validation

## 2. Executor emission

- [ ] 2.1 Add `StreamEventA2UI` (`surfaceId`, `messages []a2ui.Message`) to `StreamEvent` in `apps/server/domain/agents/executor.go`
- [ ] 2.2 Add A2UI extraction (fenced ```a2ui block) + catalog validation in the executor; invalid output falls back to text and never errors the run
- [ ] 2.3 Unit tests: valid fenced block → `StreamEventA2UI`; invalid block → no event + text continues; no block → no event

## 3. Chat SSE transport

- [ ] 3.1 Add `ui` event type + payload struct to `apps/server/pkg/sse/events.go`
- [ ] 3.2 Emit `ui` event from `streamCallback` (`apps/server/domain/chat/handler.go`) for `StreamEventA2UI`
- [ ] 3.3 Unit tests: `StreamEventA2UI` → SSE `ui` event with `surfaceId` + messages

## 4. Gateway rewriter + web render

- [ ] 4.1 `rewriteChatStream` (`apps/web-ui/gateway/sse_markdown.go`): pass through `ui` events
- [ ] 4.2 `makeHandleEvent` (`webui/static/js/chat-host.js`): add `ui` case → component-registry builder
- [ ] 4.3 Add Lit `@a2ui/lit` island to `chat.templ` + bundle the `MessageProcessor`; render catalog cards
- [ ] 4.4 Wire button action round-trip (`surfaceId` + action → `/respond`/resume)
- [ ] 4.5 Unit/browser smoke: each card renders; unknown component falls back; action returns an updated surface

## 5. A2A transport

- [ ] 5.1 `a2aStreamTranslator.translate` (`apps/server/domain/agents/a2a_stream.go`): `StreamEventA2UI` → data-part `artifactUpdate` (mimeType `application/a2ui+json`, `data` = array, stable `artifactId`)
- [ ] 5.2 Advertise A2UI extension in the AgentCard (discovery) with `supportedCatalogIds`
- [ ] 5.3 Accept surface-action follow-up messages on the resume path (distinct from question answers)
- [ ] 5.4 Unit tests: translate produces ordered data-part artifacts; extension advertised; action resume produces updated surfaces

## 6. iOS renderer

- [ ] 6.1 Add `ui`/component case to the `ChatEvent` decoder (`apps/ios/VoiceAgent/Chat/ChatEvent.swift`)
- [ ] 6.2 Add SwiftUI component registry + `Surface` renderer in `ChatView.swift` for the 8 cards
- [ ] 6.3 Wire action round-trip (`surfaceId` + action back over `lk.chat.decision`)
- [ ] 6.4 Build via Xcode simulator + manual smoke of one card

## 7. Verify

- [ ] 7.1 `go build ./...` + `go test ./...` in `apps/server` and `apps/web-ui/gateway`
- [ ] 7.2 `templ generate` + gateway `task lint`
- [ ] 7.3 iOS `xcodebuild -scheme VoiceAgent -destination 'generic/platform=iOS Simulator' CODE_SIGNING_ALLOWED=NO`
- [ ] 7.4 `openspec validate --changes` clean
