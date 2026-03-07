## 1. Create memory server parent command

- [ ] 1.1 Create `tools/cli/internal/cmd/server.go` with `serverCmd` (Use: `"server"`, Short: `"Manage a self-hosted Memory server"`)
- [ ] 1.2 In `server.go` `init()`, call `rootCmd.AddCommand(serverCmd)` and add `install`, `upgrade`, `uninstall`, `ctl`, `doctor` as subcommands of `serverCmd`

## 2. Re-register server commands under serverCmd

- [ ] 2.1 In `install.go`: change `rootCmd.AddCommand(installCmd)` → remove, let `server.go` register it
- [ ] 2.2 In `upgrade.go`: change `rootCmd.AddCommand(upgradeCmd)` → remove, let `server.go` register it
- [ ] 2.3 In `uninstall.go`: change `rootCmd.AddCommand(uninstallCmd)` → remove, let `server.go` register it
- [ ] 2.4 In `ctl.go`: change `rootCmd.AddCommand(ctlCmd)` → remove, let `server.go` register it
- [ ] 2.5 In `doctor.go`: change `rootCmd.AddCommand(doctorCmd)` → remove, let `server.go` register it

## 3. Add command groups to root

- [ ] 3.1 In `root.go` `init()`, add three `rootCmd.AddGroup()` calls: `knowledge` ("Knowledge Base"), `ai` ("Agents & AI"), `account` ("Account & Access")

## 4. Assign GroupID to Knowledge Base commands

- [ ] 4.1 `documents.go` — add `GroupID: "knowledge"` to `documentsCmd`
- [ ] 4.2 `graph.go` — add `GroupID: "knowledge"` to `graphCmd`
- [ ] 4.3 `query.go` — add `GroupID: "knowledge"` to `queryCmd`
- [ ] 4.4 `template_packs.go` — add `GroupID: "knowledge"` to `templatePacksCmd`
- [ ] 4.5 `blueprints.go` — add `GroupID: "knowledge"` to `blueprintsCmd`
- [ ] 4.6 `browse.go` — add `GroupID: "knowledge"` to `browseCmd`
- [ ] 4.7 `embeddings.go` — add `GroupID: "knowledge"` to `embeddingsCmd`

## 5. Assign GroupID to Agents & AI commands

- [ ] 5.1 `agents.go` — add `GroupID: "ai"` to `agentsCmd`
- [ ] 5.2 `agent_definitions.go` — add `GroupID: "ai"` to `agentDefsCmd`
- [ ] 5.3 `adksessions.go` — add `GroupID: "ai"` to the command struct in `newADKSessionsCmd()`
- [ ] 5.4 `mcp_servers.go` — add `GroupID: "ai"` to `mcpServersCmd`
- [ ] 5.5 `auth.go` — add `GroupID: "ai"` to `mcpGuideCmd`
- [ ] 5.6 `provider.go` — add `GroupID: "ai"` to `providerCmd`
- [ ] 5.7 `install_skills.go` — add `GroupID: "ai"` to `installSkillsCmd`

## 6. Assign GroupID to Account & Access commands

- [ ] 6.1 `auth.go` — add `GroupID: "account"` to `loginCmd`, `logoutCmd`, `statusCmd`, `setTokenCmd`
- [ ] 6.2 `tokens.go` — add `GroupID: "account"` to `tokensCmd`
- [ ] 6.3 `config.go` — add `GroupID: "account"` to `configCmd`
- [ ] 6.4 `projects.go` — add `GroupID: "account"` to `projectsCmd`

## 7. Hide developer commands

- [ ] 7.1 `traces.go` — add `Hidden: true` to `tracesCmd`
- [ ] 7.2 `db.go` — add `Hidden: true` to `dbCmd`

## 8. Update root description

- [ ] 8.1 Update `rootCmd.Long` in `root.go` to reflect grouped usage and mention `memory server` for self-hosted deployments

## 9. Build and verify

- [ ] 9.1 Run `task cli:install` (or `go build`) to confirm no compile errors
- [ ] 9.2 Run `memory --help` and verify the three group headings appear with correct commands
- [ ] 9.3 Run `memory server --help` and verify all 5 server subcommands are listed
- [ ] 9.4 Verify `memory install` returns "unknown command" error
- [ ] 9.5 Verify `memory traces list --help` still works (hidden but functional)
