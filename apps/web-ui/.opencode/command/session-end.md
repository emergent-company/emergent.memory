---
description: Wrap up a coding session — write a session log, sync docs/spec, and capture remaining work.
agent: build
---

You are finishing a coding session. Produce an accurate, durable handoff so a future session (or future you) can resume cleanly. Do not change product behavior or refactor code in this step — only document, update docs, and capture remaining work.

## 1. Reconstruct what happened this session

Reconstruct the session's work from git and the working tree:
- `git status --short`, `git diff`, `git diff --cached`, `git log --oneline -10`.
- Summarize what was built / changed / fixed, and why.

Optional extra context from the user (append it to the log; do not let it replace your own reconstruction):

$ARGUMENTS

## 2. Write the session log

Create `docs/sessions/YYYY-MM-DD-<short-slug>.md` (today's date; slug = 2–4 lowercase hyphen-separated words). Use the template in `docs/sessions/README.md`. Include:
- **Goal** — what this session set out to do.
- **Outcome** — done / partially done / blocked, with specifics.
- **Decisions** — choices made and a one-line rationale each.
- **Changes** — key files touched and what changed in each.
- **Verification** — build/test/lint commands actually run and their results.
- **Open questions / follow-ups** — unresolved items and next steps.

If `docs/sessions/README.md` is missing, create it first with the naming and template convention.

## 3. Sync the docs

Check the guides and specs for staleness and update them to match reality. Review at minimum:
- `docs/spec/` — the system specification (read `docs/spec/README.md` for the index). If behavior, API, or architecture changed this session, update the relevant numbered file in the same change. Record new decisions in `docs/spec/00-vision.md` (decision log / open items).
- `INFRASTRUCTURE.md` and `MEMORY_GUIDE.md` (and any other top-level `*.md` guides) — update if the session changed how anything is deployed, run, or configured.

Fix any drift you find; keep edits minimal and factual. Do not leave a spec/guide knowingly wrong.

## 4. Capture remaining work

Maintain the future-work repository under `docs/tasks/`:
- For each follow-up, future idea, or deferred suggestion that emerged this session, write a task file `docs/tasks/<slug>.md` using the template in `docs/tasks/README.md`, and add/update its entry in `docs/tasks/BACKLOG.md`.
- Default status is `proposed`. Link the originating session log.
- Do NOT duplicate items already tracked elsewhere (e.g. the spec roadmap `docs/spec/13-roadmap.md`); link to them instead.

## 5. Report

Return a terse summary: session log path, docs updated (or "no docs changes"), and tasks added (or "none"). List any decisions or follow-ups you were unsure how to classify so the human can confirm.
