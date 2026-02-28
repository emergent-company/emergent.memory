## Why

Agents working in this codebase need to create and apply database migrations, but no skill exists that covers the full workflow — generating a file, writing correct Goose SQL, running it, and verifying it. The thin existing `agent-db-migrations` skill only exposes Taskfile shortcuts.

## What Changes

- New agent skill added at `.agents/skills/agent-go-migrations/SKILL.md` that covers the complete migration workflow: create → write → apply → verify → rollback.

## Capabilities

### New Capabilities

- `go-migrations-skill`: Agent skill document that teaches an agent how to create, write, run, and troubleshoot Goose migrations in this Go project.

### Modified Capabilities

<!-- none -->

## Impact

- Adds one file: `.agents/skills/agent-go-migrations/SKILL.md`
- No code changes, no API changes, no dependencies
