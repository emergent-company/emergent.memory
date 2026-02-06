# AGENT.md Documentation Changelog

This file tracks updates to the AGENT.md documentation files across the codebase.

## Files Tracked

| File                                                     | Domain              | Lines |
| -------------------------------------------------------- | ------------------- | ----- |
| `apps/admin/src/components/AGENT.md`                     | Frontend Components | ~350  |
| `apps/admin/src/components/organisms/DataTable/AGENT.md` | DataTable           | 488   |
| `apps/admin/src/contexts/AGENT.md`                       | React Contexts      | 772   |
| `apps/admin/src/hooks/AGENT.md`                          | React Hooks         | ~365  |
| `apps/admin/src/pages/AGENT.md`                          | Page Components     | 1150  |
| `apps/server-go/AGENT.md`                                | Go Server           | ~450  |

**Note**: NestJS server (`apps/server/`) AGENT.md files are deprecated. The Go server (`apps/server-go/`) is the current backend.

**Total: 6 active files**

---

## 2026-01-17

### Updated Files

- `apps/server-go/AGENT.md` - Complete rewrite for production-ready Go server
- `AGENTS.md` - Updated to reference Go server instead of NestJS
- `.opencode/instructions.md` - Updated backend references for Go server

### Changes

- **Go Server AGENT.md**: Fully rewritten to document all 17 domain modules, 455 E2E tests, fx patterns, Bun ORM, job queues, ADK-Go extraction, Goose migrations
- **Root AGENTS.md**: Changed backend references from `apps/server/` to `apps/server-go/`
- **Instructions**: Updated hot reload info (Go ~30s vs NestJS ~1.5min), test commands, backend patterns

### Context

Go server migration completed. NestJS server deprecated. All 455 E2E tests passing with full feature parity.

---

## 2026-01-02 (Update #2)

### Updated Files

- `apps/server/src/entities/AGENT.md` - Added 15+ missing entities
- `apps/server/src/modules/AGENT.md` - Documented all 45 modules
- `apps/admin/src/components/AGENT.md` - Added missing molecules and organisms
- `apps/admin/src/hooks/AGENT.md` - Added hook count header

### Changes

- **Entities**: Added documentation for Agent, AgentRun, Invite, Integration, LlmCallLog, MergeProvenance, ClickUpImportLog, ClickUpSyncState, ProductVersion, ProductVersionMember, Tag, AuditLog, AuthIntrospectionCache. Total: 47 entities
- **Modules**: Reorganized into categorized sections (Core Domain, Auth, Multi-tenancy, Chat & AI, Extraction, Email, Notifications, Agents, Integrations, User Management, Infrastructure, Template Management). Total: 45 modules
- **Components**: Added comprehensive molecule documentation (29 total) with categories. Added full organism documentation (41 total). Updated counts in header
- **Hooks**: Verified all 33 hooks documented, added count header

### Context

Systematic audit to sync AGENT.md files with current codebase state. Executed via `openspec/changes/update-agent-documentation/` proposal.

---

## 2026-01-02

### Initial Setup

- Created `/update-agent-docs` command in `.opencode/command/update-agent-docs.md`
- Created this changelog file
- Updated `.opencode/instructions.md` to reference all 10 AGENT.md files

### Baseline Audit

All 10 AGENT.md files inventoried. Line counts recorded above.

---

## Template for Future Entries

```markdown
## YYYY-MM-DD

### Updated Files

- `path/to/AGENT.md` - Brief description of changes

### Changes

- Added: New sections/components/patterns documented
- Updated: Existing sections modified
- Removed: Deleted/deprecated content removed

### Context

Why these updates were needed (feature addition, refactor, bug fix, maintenance, etc.)

### Session Reference (optional)

Link or ID to the AI session that made changes, for continuity.
```
