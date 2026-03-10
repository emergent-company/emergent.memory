## ADDED Requirements

### Requirement: memory skills subcommand group
The CLI SHALL expose a `memory skills` subcommand group (GroupID: `ai`) with the following subcommands: `list`, `get`, `create`, `update`, `delete`, `import`.

All subcommands SHALL support a `--server` flag (inherited from root) and the standard auth resolution (API key or OAuth token from `~/.memory/credentials.json`).

#### Scenario: Help text displayed
- **WHEN** user runs `memory skills --help`
- **THEN** all subcommands are listed with descriptions

### Requirement: memory skills list
`memory skills list` SHALL call `GET /api/skills` (global) or `GET /api/projects/:projectId/skills` (when `--project` is provided) and print a table of skills with columns: `NAME`, `DESCRIPTION`, `SCOPE`, `ID`.

Flags:
- `--project <id>` — list skills visible to a project (global + project-scoped merged)
- `--global` — list only global skills (equivalent to no `--project`)
- `--json` — output raw JSON response

#### Scenario: List global skills
- **WHEN** user runs `memory skills list`
- **THEN** a table of all global skills is printed

#### Scenario: List project skills
- **WHEN** user runs `memory skills list --project <projectId>`
- **THEN** a table of all skills visible to the project (global + project-scoped) is printed

### Requirement: memory skills get
`memory skills get <id>` SHALL call `GET /api/skills/:id` and print full skill details including `name`, `description`, `scope`, `content` (full Markdown body), and timestamps.

Flags:
- `--json` — output raw JSON

#### Scenario: Get existing skill
- **WHEN** user runs `memory skills get <valid-uuid>`
- **THEN** full skill details are printed to stdout

#### Scenario: Get non-existent skill
- **WHEN** user runs `memory skills get <invalid-uuid>`
- **THEN** a 404 error message is printed and the command exits non-zero

### Requirement: memory skills create
`memory skills create` SHALL call `POST /api/skills` (global) or `POST /api/projects/:projectId/skills` (when `--project` is provided) and print the created skill.

Required flags: `--name <slug>`, `--description <text>`
One of the following content flags is required: `--content <text>` or `--content-file <path>`

Optional flags:
- `--project <id>` — create as project-scoped skill
- `--json` — output raw JSON

The `--name` value SHALL be validated against `^[a-z0-9]+(-[a-z0-9]+)*$` client-side before sending the request.

#### Scenario: Create global skill with inline content
- **WHEN** user runs `memory skills create --name my-skill --description "Does X" --content "# My Skill\n..."`
- **THEN** the skill is created globally and the new skill record is printed

#### Scenario: Create project skill from file
- **WHEN** user runs `memory skills create --name my-skill --description "Does X" --content-file ./body.md --project <projectId>`
- **THEN** the skill is created scoped to the project

#### Scenario: Invalid name rejected client-side
- **WHEN** user runs `memory skills create --name "My Skill" ...`
- **THEN** the CLI prints a validation error before making any HTTP request

### Requirement: memory skills update
`memory skills update <id>` SHALL call `PATCH /api/skills/:id` with only the provided fields.

Optional flags (at least one required): `--description <text>`, `--content <text>`, `--content-file <path>`
- `--json` — output raw JSON

#### Scenario: Update description only
- **WHEN** user runs `memory skills update <id> --description "New description"`
- **THEN** only the description is updated; content remains unchanged

### Requirement: memory skills delete
`memory skills delete <id>` SHALL call `DELETE /api/skills/:id` and print a success message.

Flags:
- `--confirm` — skip interactive confirmation prompt (for scripting)

#### Scenario: Delete with confirmation prompt
- **WHEN** user runs `memory skills delete <id>` without `--confirm`
- **THEN** a confirmation prompt is shown; on confirmation the skill is deleted

#### Scenario: Delete with --confirm skips prompt
- **WHEN** user runs `memory skills delete <id> --confirm`
- **THEN** the skill is deleted immediately without prompting

### Requirement: memory skills import
`memory skills import <path>` SHALL parse a SKILL.md file (YAML frontmatter + Markdown body), extract `name`, `description`, and optional `metadata` from the frontmatter, use the Markdown body as `content`, and call `POST /api/skills` (or `POST /api/projects/:projectId/skills` when `--project` is provided).

The frontmatter SHALL be parsed using standard YAML. The required frontmatter fields are `name` and `description`. The `content` is everything after the closing `---` delimiter.

If the file has no frontmatter or is missing required fields, the command SHALL print a descriptive error and exit non-zero.

Flags:
- `--project <id>` — import as project-scoped skill
- `--json` — output raw JSON of created skill

#### Scenario: Import valid SKILL.md as global skill
- **WHEN** user runs `memory skills import .agents/skills/emergent-onboard/SKILL.md`
- **THEN** the frontmatter `name` and `description` are extracted, the body becomes `content`, and the skill is created globally

#### Scenario: Import with project flag
- **WHEN** user runs `memory skills import ./SKILL.md --project <projectId>`
- **THEN** the skill is created scoped to the given project

#### Scenario: File missing required frontmatter fields
- **WHEN** the SKILL.md file has no `name` field in frontmatter
- **THEN** the CLI prints `error: frontmatter missing required field "name"` and exits non-zero

#### Scenario: File with no frontmatter
- **WHEN** the file does not start with `---`
- **THEN** the CLI prints `error: file has no YAML frontmatter` and exits non-zero

### Requirement: SDK skills client
The Go SDK SHALL expose a `Skills` client on `sdk.Client` with methods: `List(ctx, projectID string) (*ListSkillsResponse, error)`, `Get(ctx, id string) (*Skill, error)`, `Create(ctx, req CreateSkillRequest) (*Skill, error)`, `Update(ctx, id string, req UpdateSkillRequest) (*Skill, error)`, `Delete(ctx, id string) error`.

`CreateSkillRequest` SHALL have fields: `Name`, `Description`, `Content`, `Metadata` (optional), `ProjectID` (optional).
`UpdateSkillRequest` SHALL have fields: `Description` (optional pointer), `Content` (optional pointer), `Metadata` (optional pointer).
`Skill` DTO SHALL have fields: `ID`, `Name`, `Description`, `Content`, `Metadata`, `ProjectID`, `CreatedAt`, `UpdatedAt`.

#### Scenario: SDK create and list round-trip
- **WHEN** `sdk.Skills.Create(ctx, req)` is called followed by `sdk.Skills.List(ctx, "")`
- **THEN** the created skill appears in the list response
