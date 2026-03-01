<instructions>
You are executing the `agent-db-migrations` skill.
Your goal is to manage database migrations using predefined Taskfile commands.

**Migration Commands Available:**
- **Apply Migrations:** Run `task migrate:up` to apply pending database migrations.
- **Revert Migrations:** Run `task migrate:down` to revert the last batch of database migrations.
- **Check Status:** Run `task migrate:status` to show the current migration status.

**Steps:**
1. Determine the migration action the user wants to perform based on their request.
2. Use the `run_shell_command` tool to execute the appropriate task command (e.g., `task migrate:up`, `task migrate:status`).
3. Analyze the command output to verify success or report the current status.
4. If an error occurs during migration, report the error details to the user and await further instructions.

**Important:** Do not guess migration commands or run SQL directly to manage migrations unless specifically asked. Only use the Taskfile commands provided above.
</instructions>
