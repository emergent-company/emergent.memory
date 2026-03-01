## 1. Create Skill File

- [x] 1.1 Create directory `.agents/skills/agent-go-migrations/`
- [x] 1.2 Write `.agents/skills/agent-go-migrations/SKILL.md` with full migration workflow instructions

## 2. Verify Skill Content

- [x] 2.1 Confirm `SKILL.md` opens with `<instructions>` tag and closes with `</instructions>`
- [x] 2.2 Confirm all Taskfile commands (`task migrate:up`, `task migrate:down`, `task migrate:status`) are documented
- [x] 2.3 Confirm file creation command (`go run ./cmd/migrate -c create <name>`) is documented with correct working directory (`apps/server-go/`)
- [x] 2.4 Confirm Goose SQL directives (`Up`, `Down`, `StatementBegin`/`StatementEnd`, `NO TRANSACTION`) are documented with examples
- [x] 2.5 Confirm naming convention (`{version}_{snake_case}.sql`, schema prefixes `kb.`/`core.`) is documented
- [x] 2.6 Confirm troubleshooting section covers: failed migration, existing-database onboarding, embed issues
- [x] 2.7 Confirm the prohibition on editing already-applied migrations is stated explicitly
