## Why

The share-session transcript renders only the end-user turns: every agent reply is silently dropped, and the replies that do render are mis-styled. Three defects combine:

1. **The server drops agent replies.** `ShareService.sessionTranscript` filtered the run-message history on `role == "user" || role == "assistant"`, but the executor persists assistant turns under the ADK event Author — the sanitized agent name (`coder`, `Norwegian_Law_Assistant`, …) — never the literal `assistant` (see `persistEventContent` and `isAgentReplyRole`). A real share session therefore shows the user's questions and nothing else.
2. **The shared chat renders markdown with whitespace preserved.** `share-agent.js` wrapped the assistant content node in `whitespace-pre-wrap`, which inherits into the server-rendered markdown HTML, so goldmark soft line breaks become visible breaks. Models hard-wrap mid-sentence, so the user saw a break after every wrapped line. The admin chat (`chat-stream.js`) correctly uses `memory-md break-words` with no pre-wrap.
3. **A dead CSS class left markdown unstyled.** The markdown wrapper was renamed `alfred-md` → `memory-md` long ago; two surfaces still emitted `alfred-md`, so markdown rendered without the shared markdown styles.

This change corrects the behavior the unarchived `agent-public-share` change intended: the transcript endpoints must return the agent's replies, the shared chat must render assistant markdown the same way the admin chat does, and the owner transcript page must render markdown through the live `.memory-md` wrapper.

## What Changes

- **Server (`apps/server/domain/agents/share_service.go`):** `sessionTranscript` keeps `Kind == "message"` and a non-empty `Content["text"]`; maps `user` → `user`, `isAgentReplyRole(role)` → `assistant`, and skips everything else (`system`, `tool`, `tool_result`, `""`). The wire DTO contract stays `role: "user" | "assistant"` (see `ShareTranscriptMessage` in `share_entity.go`); ordering and project/ownership scoping are unchanged.
- **Web UI (`apps/web-ui/gateway/webui/static/js/share-agent.js`):** the assistant content node drops `whitespace-pre-wrap` (now `memory-md break-words`, matching `chat-stream.js`); the end-user plain-text bubble keeps `whitespace-pre-wrap`.
- **Web UI templ (`share_session_transcript.templ`, `sessions.templ`):** rename the dead `alfred-md` class → `memory-md` (6 occurrences), then `templ generate`.

### Non-goals

No change to the DTO shape or ordering, no change to project/ownership scoping, no new endpoints, and no change to the admin chat's markdown rendering (already correct).

## Capabilities

### New Capabilities

None. This change is a correction to the unarchived `agent-public-share` change.

### Modified Capabilities

The delta specs add requirements to the three capabilities introduced by the unarchived `agent-public-share` change (none exist in `openspec/specs/` yet, so the deltas use `## ADDED Requirements`):

- `agent-public-share`: the owner transcript endpoint returns the agent's replies whatever author role the runtime persisted, and the owner transcript page renders markdown through `.memory-md`.
- `agent-public-share-runtime`: the end-user session transcript returns the agent's replies whatever author role the runtime persisted.
- `web-agent-public-share`: the shared chat renders assistant markdown through `.memory-md` with soft line breaks collapsed, while end-user plain-text bubbles keep preserving line breaks.

## Impact

- `apps/server/domain/agents/share_service.go`: `sessionTranscript` author-role mapping.
- `apps/server/domain/agents/share_test.go`: `TestGetSessionTranscriptByID_FiltersMessages` extended with an agent-name role, `tool`/`tool_result` roles, and empty text (TDD).
- `apps/web-ui/gateway/webui/static/js/share-agent.js`: assistant wrapper class.
- `apps/web-ui/gateway/share_session_transcript.templ`, `apps/web-ui/gateway/sessions.templ`: `alfred-md` → `memory-md`.
