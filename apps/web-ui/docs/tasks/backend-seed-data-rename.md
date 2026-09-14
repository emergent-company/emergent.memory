# Backend seed data rename — persisted "alfred" agent/project → "memory"

**Status:** proposed
**Created:** 2026-09-04
**Source:** [2026-09-04-rename-alfred-to-memory](../sessions/2026-09-04-rename-alfred-to-memory.md)

## What

Rename the persisted seed records in Emergent Memory that still carry the old product name:
- project `alfred` (`558b793e-6f30-4813-9105-44e796ba56eb`)
- agent definition `alfred` (`9101c2e3…`), including the `alfred-google-rt` agent name
- any LiveKit room/agent identifiers (`alfred-persistent`, `alfred-` room prefix) still in use

## Why

Deferred from the rename (design D4): these are *data* values persisted in Memory, not code. Renaming live seed agents/rooms is a data migration and would break running voice sessions until the flag-day flip.

## Depends on

none (but coordinate with `deploy-topology-rename` for a single flag-day)

## Notes

- `docs/spec/02-memory-backend.md` and `13-roadmap.md` document the seed `alfred` records — update those tables in the same change.
- Code defaults already say `memory`/`memory-google-rt`; this task only migrates the persisted rows + any dispatch references (e.g. iOS `defaultAgentName`, `.env` `ROOM_NAME`).
