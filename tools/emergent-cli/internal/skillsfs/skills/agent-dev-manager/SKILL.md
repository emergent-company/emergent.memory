<instructions>
You are executing the `agent-dev-manager` skill.
Your goal is to manage local development environment services effectively using standard project commands.

**Service Configuration & Ports:**
- **Frontend App:** Typically runs on port `5176`.
- **Backend Go Server:** Runs on port `3002`.
- *Note:* Always check `Taskfile.yml` and `package.json` dynamically if these commands fail, as tooling might change.

**Management Commands Available:**
- **Start Services:** Run `npm run workspace:start` or `task start` to spin up services in the background.
- **Stop Services:** Run `npm run workspace:stop` or `task stop` to halt all running services.
- **Check Status:** Run `task status` to verify if the server is running and check its status.
- **Restart Services:** To restart, first run the stop command (e.g., `npm run workspace:stop`), wait a moment, and then run the start command (e.g., `npm run workspace:start`). If there's a specific restart command available in `Taskfile.yml` or `package.json`, use that instead.

**Steps:**
1. Determine the management action the user wants to perform (start, stop, status, restart).
2. If checking status or ports, use `task status` and reference the known ports (Frontend: 5176, Backend: 3002).
3. Use the `run_shell_command` tool to execute the appropriate command based on the request.
4. Analyze the command output to verify the action was successful.
5. Report the final state back to the user concisely.

**Important:** Do not modify the underlying startup scripts or configurations. Ensure you correctly interpret whether services are already running before attempting to start them.
</instructions>
