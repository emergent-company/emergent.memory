## Purpose

Adds a measured legacy-contrast bar for the small accent-on-tint labels and captions the gateway renders in the chat session rail and pending-work dock, whose effective background depends on row state rather than being a fixed surface.

## ADDED Requirements

### Requirement: Chat rail badge labels clear WCAG AA in every row state

The label text of the chat session-rail status badge SHALL render at a contrast ratio of at least 4.5:1 against the badge's rendered background in every rail row state (base, hover, active) and for every bucket, without changing the badge geometry, the bucket tint and border alphas, or the token pair used by the badge's count pill.

#### Scenario: Failed label is legible in every row state
- **WHEN** a conversation has bucket `failed` and its rail row is in the base, hover, or active state
- **THEN** the label renders at ≥4.5:1 (measured 6.53 / 5.88 / 5.22), the accent still drives the tint and the border, and the count pill keeps its `--color-error` / `--color-error-content` pair

#### Scenario: Done label is legible in every row state
- **WHEN** a conversation has bucket `done` (or the empty fallback) and its rail row is in the base, hover, or active state
- **THEN** the label renders at ≥4.5:1 (measured 5.97 / 5.50 / 5.03)

#### Scenario: Already-passing buckets are unchanged
- **WHEN** a conversation has bucket `needs_input` or `running`
- **THEN** its label keeps its accent token and its measured ratio is unchanged (`needs_input` 7.07 / 6.30 / 5.56; `running` 6.02 / 5.37 / 4.75)

#### Scenario: The count pill is not part of this change
- **WHEN** the badge renders a pending count
- **THEN** `.memory-rail-count` keeps its per-bucket solid token pairs and geometry, and `background: currentColor` does not reappear

### Requirement: Dock pending-count caption clears WCAG AA

The dock's pending-count caption SHALL render at a contrast ratio of at least 4.5:1 against the dock background.

#### Scenario: Dock caption is legible
- **WHEN** the pending-work dock shows its "N pending" header
- **THEN** `.dock-count-label` renders at ≥4.5:1 (measured 6.79), while the `.dock-count` pill keeps `--color-warning` / `--color-warning-content` (9.91:1)

### Requirement: Badge stylesheet copies stay in sync

The rail-badge and dock-caption rules SHALL be identical in the source stylesheet and the runtime-injected stylesheet, since the injected copy is appended to `<head>` and wins at equal specificity.

#### Scenario: Both copies declare the same label colours
- **WHEN** `webui/css/app.css` and the injected `<style>` string in `webui/static/js/chat-components.js` are compared
- **THEN** the `failed` label mix, the `done` label alpha, and the dock caption alpha are the same in both
