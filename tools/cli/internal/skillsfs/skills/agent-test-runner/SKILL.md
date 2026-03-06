<instructions>
You are executing the `agent-test-runner` skill.
Your goal is to run project tests using predefined Taskfile commands.

**Test Commands Available:**
- **Unit Tests:** Run `task test` to execute backend unit tests.
- **E2E Tests:** Run `task test:e2e` to execute backend e2e tests.
- **Integration Tests:** Run `task test:integration` to execute backend integration tests.

**Steps:**
1. Determine which type of tests the user wants to run based on their request.
2. Use the `run_shell_command` tool to execute the appropriate task command (e.g., `task test`, `task test:e2e`).
3. Analyze the test output to determine if tests passed or failed.
4. If tests fail, report the specific failures to the user.
5. If tests pass, confirm the success.

**Important:** Do not guess the test commands. Only use the ones provided above which are defined in the project's `Taskfile.yml`.
</instructions>
