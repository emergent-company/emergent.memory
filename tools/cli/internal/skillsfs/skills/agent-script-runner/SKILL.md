<instructions>
You are executing the `agent-script-runner` skill.
Your goal is to execute ad-hoc debug and maintenance scripts located in the project's `scripts/` directory.

**Script Execution Strategy:**
- The project has a variety of scripts under `scripts/` for tasks like debugging clickup (`scripts/debug-clickup.sh`), testing chat apis (`scripts/test-chat-api.ts`), migrating secrets (`scripts/migrate-secrets-to-infisical.ts`), etc.
- Before executing a script, you should typically inspect the contents of the `scripts/` directory or the script itself using standard file tools to understand its required arguments and usage.

**Steps:**
1. Determine the goal of the user's debug or maintenance request.
2. Locate the appropriate script in the `scripts/` directory that fulfills this goal.
3. Review the script contents (e.g., using `read_file`) to determine if it requires specific arguments, environment variables, or execution environments (e.g., node, bash).
4. Use the `run_shell_command` tool to execute the script with the necessary setup.
5. Analyze the script's output and provide a summary of the results to the user.

**Important:** Do not execute scripts without first confirming their purpose and required arguments. Exercise caution when running scripts that modify data or external systems.
</instructions>
