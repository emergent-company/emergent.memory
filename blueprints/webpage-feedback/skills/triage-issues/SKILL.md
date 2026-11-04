---
name: triage-issues
description: How captured webpage feedback is classified and grouped into recurring IssueGroups.
---
# Triage issues

An `Issue` is one report from emergent.feedback. The capture source creates it,
including the page, description, app version, and environment. The
`issue-triager` agent classifies it and groups recurring problems.

## Ownership

- **Capture source (emergent.feedback):** `capture_status` (`open`, `resolved`,
  `dismissed`), `page_url`, `repo`, `description`, `source_id`, `source_url`,
  `created_at`.
- **issue-triager:** `status` (`new`, `triaged`, `needs-info`, `wont-fix`),
  `severity`, `category`, `confidence`, `triage_notes`, `group_key`.
  `Issue.status` is the object's board field: set it with entity-update's
  top-level `status` argument, never as a property. `IssueGroup` is not
  board-enabled, so its `status` is an ordinary property.
- **Humans:** may override either, but should not rewrite the other side's
  fields by accident. `entity-update` merges, so always send only the fields you
  own.

## Status flow

`new` → `triaged` (normal), `needs-info` (the report is ambiguous), or
`wont-fix` (clearly out of scope / already fixed). A resolved report is not a
status change here: the capture source writes `capture_status = resolved` via a
patch, preserving everything the agent wrote.

## Grouping rule

- **One report is only triaged.** Create no `IssueGroup`.
- On the **second** similar report, create exactly one `IssueGroup` (stable key
  `group-<slug>`) and link both Issues with `BELONGS_TO_GROUP`.
- Later similar reports join the existing group; increment `issue_count` and
  update `last_seen`. Do not create a second group for the same problem.
- `Issue.group_key` mirrors the group key for fast filtering. The relationship
  is the source of truth.

## Triage fields

- `category`: `bug`, `ux`, `performance`, `content`, `feature`, `question`,
  `other`.
- `severity`: `blocker`, `high`, `medium`, `low`, `unknown`.
- `confidence`: 0.0–1.0; low confidence is a reason for `needs-info`.
- `triage_notes`: a short paragraph, including what is missing when the report
  cannot be classified confidently.

## Search before grouping

Use `search-hybrid` on the issue text and the classified category/severity.
Exclude the current Issue. Similarity is about the underlying problem, not just
shared words: two reports on different pages can still be the same component or
API failure.

## Idempotency

Reaction agents are at-least-once. Before creating a group, check whether the
similar Issue already has a `group_key`; if so, join it instead of creating
another. The run ends when triage is complete.
