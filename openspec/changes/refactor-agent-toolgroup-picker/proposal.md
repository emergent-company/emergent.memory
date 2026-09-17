## Why

The agent tool-group picker markup in `gateway/agent.templ` repeats several blocks verbatim — the ghost XS wrench count badge, the Inherit/Allow/Ask/Deny policy option set, the dashed "Other" fallback container, the collapsible `<details>`/`<summary>`/chevron shell, and an inline re-implementation of the tool row. Each copy must be kept in sync by hand, and the markup is locked by `agent_ui_test.go`'s exact `data-testid` / `name` / `value` assertions, so drift is easy to introduce and hard to see.

## What Changes

- Extract a shared `agentToolDisclosure` collapsible shell (named-group marker, chevron rotation selector, header/controls slots, body children) used by both the capability-group and source-group headers.
- Extract `agentToolCountBadge` for the ghost XS wrench badge.
- Extract `agentPolicyOptions` for the Inherit/Allow/Ask/Deny option set, preserving both value spaces (group select submits `"inherit"`, per-tool select submits `""`).
- Extract `agentToolOtherContainer` for the dashed "Other" fallback container.
- Make `agentToolOtherGroup` reuse `agentToolRowView` instead of re-implementing the row inline (removing the now-dead `toolPolicySelect` wrapper).
- Convert the two sandbox availability dots to the shared `components.StatusBadge`.

No rendered markup change: every `data-testid`, `name`, `value="on"`/`checked`, and option `value` survives byte-identically.

## Capabilities

### New Capabilities
<!-- none — pure internal refactor, no behavior change -->

### Modified Capabilities
<!-- none -->

## Impact

- `apps/web-ui/gateway/agent.templ` only (plus the generated, untracked `agent_templ.go`). No server, API, schema, or behavior change; sets `skip_specs: true`.
