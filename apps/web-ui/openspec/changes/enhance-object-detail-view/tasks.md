## 1. Data layer — graph writes, search, and chat link

- [x] 1.1 Test-first: write a unit test asserting `GraphObject` parses `labels` from object JSON (currently dropped); verify it fails, then add `Labels []string` to `GraphObject` and verify the test passes
- [x] 1.2 Add `UpdateObject(ctx, id, *UpdateObjectRequest) (*GraphObject, error)` to `MemoryBackend` + `MemoryClient`; unit-test the PATCH `/api/graph/objects/{id}` method/path/delta payload and update `fakeMemory` so the suite compiles
- [x] 1.3 Add `CreateRelationship(ctx, *CreateRelationshipRequest) error` to `MemoryBackend` + `MemoryClient`; unit-test the POST `/api/graph/relationships` payload (`type`, `src_id`, `dst_id`) and update `fakeMemory`
- [x] 1.4 Add `SearchObjectsFTS(ctx, query, typeFilter) ([]GraphObject, error)` to `MemoryBackend` + `MemoryClient`; unit-test `/api/graph/objects/fts` query building and response parsing and update `fakeMemory`
- [x] 1.5 Add `CanonicalID` to `ChatRequest` and the conversation types (`Conversation`, `ConversationDetail`); unit-test JSON marshal/unmarshal includes `canonicalId` and existing chat tests still pass

## 2. Editor agent setting

- [x] 2.1 Add `editor` category / `agent_id` key constants mirroring the assistant-agent pattern; test-first a `resolveEditorAgentID` helper (default → first enabled → first → empty) via unit tests
- [x] 2.2 Wire an editor-agent selector into the settings page reusing the existing assistant selector; unit-test the save/load round-trip through `SetProjectSetting`/`GetProjectSetting`

## 3. Handlers and routes

- [x] 3.1 Add the object property PATCH handler (parse delta, call `UpdateObject`, redirect to the returned new object ID); unit-test success returns/redirects to the new ID and invalid input errors
- [x] 3.2 Add the relationship-create handler (validate `type`, `src_id`, `dst_id`, call `CreateRelationship`); unit-test validation and success
- [x] 3.3 Add an object-search handler returning JSON for the modal autocomplete; unit-test query/type passthrough and empty-query fallback
- [x] 3.4 Add the chat-about-object handler (resolve editor agent, build seeded prompt, deep-link to chat carrying `canonicalId`); unit-test prompt content, fallback agent resolution, and `canonicalId` carry
- [x] 3.5 Verify object-chat resume semantics against the memory stream handler: confirm the seeded message persists on a `canonicalId` resume; if it is dropped, switch the deep-link to resume by `conversationId` (gateway-side get-or-create) and unit-test that path

## 4. UI — object detail view

- [x] 4.1 Rework the properties card to a single-column editable form (label row, content input beneath); unit-test the rendered markup (labels above inputs, no multi-column grid)
- [x] 4.2 Render non-scalar property values (arrays/objects) as JSON text inputs; unit-test a map/array value renders a JSON input and a scalar renders a plain input
- [x] 4.3 Add a relationship "Connect" call-to-action on the empty state opening a modal (schema-constrained type selector, source pre-selected and searchable, target searchable); unit-test the CTA is present and the modal exposes the three fields
- [x] 4.4 Add the "Chat about object" button wired to the chat-about-object handler; unit-test the button/link is present

## 5. Build and verify

- [x] 5.1 Run `templ generate` then `go build ./...` from `gateway/` and verify it compiles
- [x] 5.2 Run `task lint` and verify it passes
- [x] 5.3 Run `go test ./...` from `gateway/` and verify the full suite passes
- [ ] 5.4 Restart the dev server and manually verify in the browser: edit a property, create a relationship from the empty state, and start/resume an object chat
