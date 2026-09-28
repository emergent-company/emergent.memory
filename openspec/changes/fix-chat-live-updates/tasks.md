## 1. Fingerprint progress signal (#1203)

- [ ] 1.1 DONE: add `progressCount` + `progressMark` to `conversationRunState`, extract `runStateFingerprint`, and include both in the fingerprint.
- [ ] 1.2 DONE: derive them from the poller's existing history fetch via a shared `runStateFromTimeline` (conversation + run scopes).
- [ ] 1.3 DONE: unit test `TestRunStateFingerprintChangesOnStepProgress` (changes on a step, stable otherwise, still moves on pending question) and poller test `TestHubPollerRebroadcastsOnStepProgressWithinRunningRun`; both were RED before the fix.
- [ ] 1.4 DONE: existing approval/question refresh tests remain green.

## 2. Run-scoped live channel (#1204)

- [ ] 2.1 DONE: add `GET /api/runs/:runId/events` (run-scoped hub key `run:<id>`, `runState` fingerprinting the run timeline).
- [ ] 2.2 DONE: client `openLiveStream`/`openRunStream` subscribe on `resumeRun`; re-render the run transcript on refresh.

## 3. New-conversation live channel (#1204)

- [ ] 3.1 DONE: client `openConversationStream` on the `meta` event when a new conversation gets its id.
- [ ] 3.2 DONE: single-source teardown — `openLiveStream` dedupes/closes; `resetConversation` and run abandonment close the stream.

## 4. Verify

- [ ] 4.1 `templ generate` + `go build ./...` + `go test ./...` in `apps/web-ui/gateway`.
- [ ] 4.2 `task lint` in `apps/web-ui`.
- [ ] 4.3 `openspec validate --all --strict`.
- [ ] 4.4 e2e: `chat-run-control.spec.ts` asserts the new-conversation events subscription (live-LLM gated; not runnable in this lane).
