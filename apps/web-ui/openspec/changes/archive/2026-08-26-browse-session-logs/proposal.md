## Why

The agents log every conversation — turns, tool calls, and usage — to a session log, but users can only review it on an internal admin page, not from the iOS app. This adds a read-only session browser so users can replay past conversations as a chat, including the tool calls each turn made.

## What Changes

- Backend "session log" endpoints that expose the session list and a per-session timeline (turns, tool calls, usage) read-only and authenticated.
- Native SwiftUI session browser in the iOS app: a session list, then a chat-style view of a session with expandable tool-call details (tool name, arguments, result, error flag).

## Capabilities

### New Capabilities

- `session-log-api`: HTTP API exposing session logs (list + timeline with turns and tool calls), read-only and authenticated.
- `ios-session-browser`: native SwiftUI chat-style browser for reviewing recorded sessions.

### Modified Capabilities

None.

## Impact

- Backend: `agent/admin.py` — authenticated session list + timeline endpoints (reusing the existing session-log reader).
- iOS: `client/ios/` — session list + chat view + tool-call detail, an API client and models.
- No breaking changes.
