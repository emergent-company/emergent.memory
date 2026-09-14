# 2026-09-10 — Fix duplicate chat message

## Goal

Explain why a chat message the user sent once appeared **twice** after reopening conversation
`da387171-1308-4c4e-ad57-0e582c0b0d91`, then fix it.

## Outcome

**Done.** Root cause: the transcript was merged twice — memory's `/history` already merges
stored `kb.chat_messages` with run-history items, and the gateway re-merged the same rows with a
dedupe key that required exact nanosecond timestamp equality. The stored row (written at request
time) and the run row (written at run start) never share a timestamp, so the stored user copy was
re-added and rendered twice. Fix in the gateway; PR
[#46](https://github.com/emergent-company/memory-web-ui/pull/46) merged (squash `2b30d5a`).
User confirmed in the browser that the duplicate is gone.

## Decisions

- **Memory owns the transcript merge; the gateway must not re-merge** — the gateway's
  exact-timestamp dedupe can never match across the two stores, and memory v0.76+ already
  suppresses the duplicate stored copy (text + nearest-preceding-time match). → D38.
- **Ship via an isolated worktree + PR** — the shared `/root/alfred` checkout held unrelated
  dirty files from other sessions; never commit them.
- **Merge despite red CI** — GitHub Actions is org-billing blocked (jobs fail before any step;
  master and every recent PR are red). Already tracked as `ci-actions-billing-blocker`.

## Changes

- `gateway/extras.go` — `conversationTimeline` returns memory's timeline as-is when it has items;
  the stored-message synthesis fallback now only runs for an empty-item history. Removed the dead
  gateway-side merge helpers (`mergeConversationTimeline`, `parseMergedEntry`, `mergedTimelineEntry`,
  `messageTimelineItemWithTime`, `timelineCreated`) and their now-unused imports (−142/+18).
- `gateway/extras_merge_regression_test.go` (new) — `TestHistoryNoDuplicateUserMessage`: memory
  history item + stored row with differing timestamps → exactly one user item.

## Verification

- `journalctl` — exactly one `POST /api/chat` from the user's IP (`100.121.213.124`) at 17:17:07,
  and the conversation URL opened immediately after → single send, not a client retry.
- Pre-fix repro (throwaway test, removed): `mergeConversationTimeline` produced `2 [hello hello]`.
- `go build ./...` — PASS; `go test ./...` — PASS; `task lint` — 0 issues (isolated worktree,
  `templ generate` run first).
- Dev gateway (`task dev`, air) auto-rebuilt at 17:31:22 → fix live on `:8095`.
- User reloaded the conversation URL and confirmed one copy.

## Open questions / follow-ups

- CI is red org-wide (GitHub Actions billing) — tracked as `ci-actions-billing-blocker`.
- The shared `/root/emergent.memory` checkout still carries an **older exact-time** version of
  `mergeConversationUserMessages` uncommitted. Do not ship that version — deployed `v0.76.0` has the
  good text + nearest-preceding-time merge.

## Tasks

- none new (CI already tracked as `ci-actions-billing-blocker`)
