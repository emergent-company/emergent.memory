## Why

An agent's `visibility` — `external`, `project`, or `internal` — is the single field that decides where an agent shows up and who can invoke it. Today it can only be set through the API, the CLI, or a blueprint; the admin UI renders the value as a read-only badge, so a user cannot change it from the surface they actually use to manage an agent. At the same time, the A2A message flow resolves a skill by slug with an "external first, then any-visibility" fallback, which means an `internal` agent — whose whole point is to be a hidden, system-only agent — is still callable over A2A by anyone who guesses its slug. That contradicts the intent of `internal` and makes the visibility field a weak guarantee.

## What Changes

- The admin UI's General agent-settings form gains a **Visibility** control: a native dropdown with the three levels, each labelled `Name — description`, plus a server-rendered helper line, an `external` warning, and an `internal` note.
- The gateway validates and persists visibility through the existing general-settings update path: an empty/missing value normalises to `project` (the server default) and an unknown value is rejected.
- The A2A skill resolver is tightened so `internal` definitions are **never** resolvable via A2A: `external` is preferred, `project` remains resolvable by slug (but not advertised), and `internal`-only matches resolve as not-found (`SKILL_NOT_FOUND`), never falling back to the CLI assistant.

## Capabilities

### New Capabilities

- `web-agent-settings`: the admin UI exposes a Visibility control on the agent General settings form that renders the three levels with per-option descriptions, a default of `project`, conditional `external`/`internal` guidance, and server-side validation, persisting through the existing general-settings update path.

### Modified Capabilities

- `a2a-message-flow`: the skill-resolution contract for `message:send` is tightened so `internal` definitions are never resolvable via A2A, `project` definitions remain resolvable by slug but not advertised, and `external` definitions are preferred.

## Impact

- Server (`apps/server/domain/agents/`): the A2A skill resolver (`resolveA2AAgentBySkillID`) and its routing-contract doc comment; visibility doc comments in `entity.go`. Comment-only for the visibility enum; the resolver change is code + unit tests.
- Gateway (`apps/web-ui/gateway/`): `agent.templ` (Visibility section + dropdown), `agent.go` (validate + persist), `ui.go` (visibility option/warning/note helpers), and `agent_visibility_test.go`.
- No database migration: visibility already exists as a `kb.agent_definitions.visibility` column with a `project` default.
