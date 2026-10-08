## Purpose

Composition of project-level context (`kb.projects.project_info`) into an
agent's system instruction. The source is opt-in per agent definition and, when
enabled, contributes the same block, in the same position, on every execution
surface (trusted HTTP trigger, A2A `message:send`, chat sessions, and the
OpenAI-compatible `/v1/chat/completions` surface) so the composed system prompt
is byte-identical for a given agent definition and flag value.

## ADDED Requirements

### Requirement: Project context injection is opt-in per agent definition

`project_info` SHALL be injected into an agent's composed system instruction only
when the agent definition opts in via its `includeProjectInfo` flag. The default
SHALL be `false`. When the flag is `false`, no execution surface SHALL inject
`project_info`. When the flag is `true`, every execution surface SHALL inject it
as the `## Knowledge base context` block.

#### Scenario: Default (flag off) injects nothing on any surface

- **WHEN** an agent definition with `includeProjectInfo` unset or `false` runs on the HTTP trigger, A2A, chat, or `/v1/chat/completions` surface
- **THEN** the composed system instruction contains no `## Knowledge base context` block

#### Scenario: Opt-in (flag on) injects on every surface

- **WHEN** an agent definition with `includeProjectInfo` set to `true` runs on the HTTP trigger, A2A, chat, or `/v1/chat/completions` surface, and the project has a non-empty `project_info`
- **THEN** the composed system instruction contains the `## Knowledge base context` block exactly once, containing the project's `project_info`

#### Scenario: The OpenAI-compatible surface no longer injects unconditionally

- **WHEN** a `/v1/chat/completions` request runs against an agent definition that does not opt in
- **THEN** `project_info` is absent, removing the previous unconditional injection

### Requirement: Cross-surface composition is identical

For a given agent definition and `includeProjectInfo` value, the contribution of
`project_info` to the composed system instruction SHALL be identical across
surfaces — same block text, injected at the same position relative to the agent
`systemPrompt`, language directive, skills block, and caller-supplied
`SystemPromptAppendix`.

#### Scenario: Same agent and flag produce byte-identical project context

- **WHEN** the HTTP trigger, A2A, and `/v1/chat/completions` surfaces compose the instruction for the same agent definition, the same flag value, and the same project
- **THEN** each composed instruction places the identical `## Knowledge base context` block in the identical position, and the position is independent of the entry point

#### Scenario: No surface injects project context itself

- **WHEN** any surface composes the instruction
- **THEN** `project_info` is added only by the shared instruction resolver, and no surface prepends it independently (so it can never be duplicated)
