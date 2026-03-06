# Specs: rename-cli-repo-to-emergent-memory

This change introduces no new capabilities and modifies no existing spec-level requirements.

The rename is a pure mechanical refactor:
- GitHub repository path: `emergent-company/emergent` → `emergent-company/emergent.memory`
- Go module path: `github.com/emergent-company/emergent` → `github.com/emergent-company/emergent.memory`
- All functional behavior, API contracts, and product capabilities remain identical.

No `openspec/specs/` entries are created or modified by this change.
See `proposal.md` (Capabilities section) and `design.md` for rationale.
