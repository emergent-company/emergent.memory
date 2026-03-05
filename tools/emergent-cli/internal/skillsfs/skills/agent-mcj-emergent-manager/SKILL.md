<instructions>
You are executing the `agent-mcj-emergent-manager` skill.
Your goal is to manage the `mcj-emergent` semi-production server remotely via SSH and the `emergent` CLI.

**SSH Access:**
- SSH keys are already installed on this machine, so you can connect to `mcj-emergent` directly without a password.
- All commands to be executed on the remote server MUST be wrapped in `ssh mcj-emergent '<command>'`.

**Key Commands (via `emergent` CLI on remote host):**
- **Upgrade Application:** To upgrade the application to the latest version, run: `ssh mcj-emergent 'emergent upgrade'`.
- **Check Service Status:** To check the status of all `mcj-emergent` services, run: `ssh mcj-emergent 'emergent status'`.
- **Restart Services:** To restart all `mcj-emergent` services, run: `ssh mcj-emergent 'emergent restart'`.
- **Stop Services:** To stop all `mcj-emergent` services, run: `ssh mcj-emergent 'emergent stop'`.
- **Start Services:** To start all `mcj-emergent` services, run: `ssh mcj-emergent 'emergent start'`.

**Steps:**
1. Identify the requested management action (upgrade, status, restart, stop, start).
2. Construct the appropriate `ssh mcj-emergent '<command>'` based on the action and the `emergent` CLI usage.
3. Execute the command using the `run_shell_command` tool.
4. Analyze the output from the remote server and report relevant information back to the user.

**Important Considerations:**
- **CLI Changes:** If the `emergent` CLI commands or flags change, run `ssh mcj-emergent 'emergent --help'` to get dynamic help.
- **Network Errors:** Report any SSH connection errors clearly to the user and do not retry without explicit instruction.
- **No Raw Commands:** Do NOT attempt to run raw Docker commands, systemd commands, or other low-level system commands directly on the `mcj-emergent` server. Always use the `emergent` CLI for consistency and safety.
</instructions>
