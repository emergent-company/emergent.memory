# Tasks

## 1. Gateway initial refresh frame carries current state

- [x] 1.1 Cache the last broadcast refresh frame per conversation in `conversationHub` (`lastFrame`), populated in `broadcast` for conversation scope only.
- [x] 1.2 Replay the cached frame as the connecting subscriber's first frame in `streamEvents`; fall back to the bare `{"type":"refresh"}` when none exists.
- [x] 1.3 Clear the cached frame on unsubscribe when the conversation's subscriber set empties; never populate it for run-scoped keys.
- [x] 1.4 Tests: cached-frame replay on second subscriber (state before `live_replay`); cache/clear behavior; run-scope exclusion.

## 2. Client reflects a mid-run open (chat.js / chat-stream.js)

- [x] 2.1 Derive `liveRunStatus` from the rendered history's newest run and show the header working indicator; leave completed/failed runs untouched.
- [x] 2.2 Add a single idempotent `[data-memory-working]` agent bubble (assistant shell + `.memory-typing`) for an active run not owned by this page.
- [x] 2.3 Split `streaming` (turn-busy) from `liveTurn` (this page owns a live stream); raise `liveTurn` via one engine hook in `openAssistantBubble`; flip the DOM-ownership gates; clear `liveTurn` at every turn-teardown site.
- [x] 2.4 Consume `replayFrames` after flushing so a later re-render cannot duplicate the connect-time in-flight tail.
- [x] 2.5 Plant the placeholder after the replay tail; remove it when the live bubble takes over, the run stops, or a re-render shows no active run.

## 3. Verification

- [x] 3.1 Hermetic js-dom spec `specs/js/chat-midrun-working.spec.ts`: mid-run open shows working bubble + indicator + busy composer; a completed refresh removes the bubble, clears the indicator, and releases the composer.
- [x] 3.2 `go build ./...`, `go test ./...`, `go vet ./...` in `apps/web-ui/gateway`.
- [x] 3.3 `npx playwright test --config=js-dom.config.ts` (a2ui + new spec) green.
- [x] 3.4 `node --check` on the two edited JS files.
