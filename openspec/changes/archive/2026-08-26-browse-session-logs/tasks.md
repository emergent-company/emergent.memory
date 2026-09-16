## 1. Backend — session-log API in agent/admin.py

- [ ] 1.1 Add `X-API-Key` gating to `/api/sessions` and `/api/session` (reuse the `_authorized()` helper)
- [ ] 1.2 Elide `session_end.history` from the timeline response to keep payloads small
- [ ] 1.3 Inject `TOKEN_API_KEY` into the admin page JS so the internal sessions viewer keeps working under auth
- [ ] 1.4 Verify with curl on home2: list + timeline return records, no key → 401, admin sessions page still loads

## 2. iOS — session client + models

- [ ] 2.1 Add a `SessionLogClient` that derives the base URL from the token endpoint host and sends `X-API-Key`
- [ ] 2.2 Add `Codable` models for a session summary (room, recency) and session records (kind, role, text, calls, outputs)

## 3. iOS — session browser UI

- [ ] 3.1 Add a `SessionsView` using `List`, listing sessions most recent first and fetching on open
- [ ] 3.2 Add a `SessionChatView` that renders `turn` records as chat bubbles (user right-aligned, assistant left)
- [ ] 3.3 Add expandable tool-call details (`tools_executed`: tool name, arguments, result, error flag) attached to the preceding turn
- [ ] 3.4 Add empty-state and error-state presentations
- [ ] 3.5 Add the sessions entry point to `StartView` (consistent with the memories entry point)
- [ ] 3.6 Add new localization keys to `Localizable.xcstrings`

## 4. Verification

- [ ] 4.1 Rebuild the iOS app in Xcode and confirm: session list → chat view → tool-call details all work
- [ ] 4.2 Confirm no regression to `/api/token`, the prompt editor, or the memories API on `admin.py`
