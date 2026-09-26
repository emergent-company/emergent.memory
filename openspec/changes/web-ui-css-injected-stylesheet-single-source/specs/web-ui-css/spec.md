## REMOVED Requirements

### Requirement: Badge stylesheet copies stay in sync

The rail-badge and dock-caption rules SHALL be identical in the source stylesheet and the runtime-injected stylesheet, since the injected copy is appended to `<head>` and wins at equal specificity.

#### Scenario: Both copies declare the same label colours
- **WHEN** `webui/css/app.css` and the injected `<style>` string in `webui/static/js/chat-components.js` are compared
- **THEN** the `failed` label mix, the `done` label alpha, and the dock caption alpha are the same in both

## ADDED Requirements

### Requirement: Runtime-injected styles are not a second stylesheet

The runtime-injected `<style>` in `webui/static/js/chat-components.js` SHALL NOT re-declare a rule that `webui/css/app.css` already owns. `app.css` is the single source of truth for the chat run-control surface (live run status, typed run markers, turn footers, copy affordances, the composer queue, the pending-work dock, the session todo card) and the session-rail badge. The injected sheet SHALL contain only the badge/thinking shell that `app.css` deliberately does not define, plus the documented `.memory-tool-chip[data-status]` border override that keeps the tool-chip row borderless.

#### Scenario: The injected sheet does not duplicate app.css selectors

- **WHEN** the injected `<style>` in `chat-components.js` and `webui/css/app.css` are compared
- **THEN** no selector from the app.css-owned chat-control surface or the rail badge reappears as an injected rule

#### Scenario: The runtime-only badge shell stays injected

- **WHEN** `chat-components.js` runs and the badge shell is rendered
- **THEN** the injected sheet still defines the `.memory-badge` expandable shell and shimmer that `app.css` does not own, so tool-call and thinking badges render

#### Scenario: A guard test fails on a re-introduced duplicate

- **WHEN** a change re-adds an app.css-owned selector to the injected sheet
- **THEN** the injected-stylesheet guard test fails, so the two sources cannot drift apart by convention again
