# Add CLI `--icon`/`--color` and blueprint `ui` passthrough for agent appearance

**Status:** proposed
**Created:** 2026-09-17
**Source:** PR #541 (`feat/agent-appearance`) — archived change `openspec/changes/archive/2026-09-17-agent-appearance`

## What

Finish the CLI half of agent appearance (the Web UI + server halves shipped in PR #541):

1. `memory agent-definitions create` / `update` gain `--icon` and `--color`, building the opaque `uiConfig` blob into the create/update request.
2. The CLI blueprint applier carries a blueprint agent's `ui: {icon, color}` block through to the agent-definition request, so `agents/*.yaml → CLI apply → definition` round-trips (the server manifest and the Web UI gateway already do this).

## Why

The CLI slice was deliberately left out of PR #541: it consumes the new `uiConfig` field on `apps/server/pkg/sdk/agentdefinitions`, but `apps/cli` is built against the **published** SDK module — `.github/workflows/cli.yml` runs with `GOWORK: off` and `apps/cli/go.mod` pins `github.com/emergent-company/emergent.memory/apps/server/pkg/sdk v0.82.0`. SDK module tags are only created on release tags (`server-sdk.yml` → `apps/server/pkg/sdk/<tag>`), so a CLI slice referencing an unreleased SDK field cannot pass `cli.yml` in the same PR. CI failed with:

```
internal/blueprints/applier.go:860:6: req.UIConfig undefined (type *agentdefinitions.CreateAgentDefinitionRequest has no field or method UIConfig)
```

Do **not** work around this with a relative `replace` in `apps/cli/go.mod` — the pinned-SDK check in `cli.yml` is deliberate release hygiene (relative replaces exist only for `tools/*` modules).

## Depends on

- An SDK module tag **newer than `v0.82.0`** that contains `UIConfig` on the `agentdefinitions` agent-definition types (added in PR #541, `apps/server/pkg/sdk/agentdefinitions/client.go`). Confirm with:
  ```bash
  LATEST=$(git tag -l "apps/server/pkg/sdk/*" --sort=-v:refname | head -1)
  git show "$LATEST:agentdefinitions/client.go" | grep -n UIConfig
  ```
- The archived capability spec `openspec/specs/agent-appearance/spec.md` (no CLI requirement was carried into the main spec — re-add one in the follow-up's delta spec).

## Notes

### Recoverable reference implementation (PR #541, deliberately reverted)

Both halves were implemented, CI-tested locally, then reverted in `8dd6c180b` because of the SDK pin. Retrieve the exact code with:

```bash
git fetch origin refs/pull/541/head
git show e73812fd1 -- apps/cli/internal/cmd/agent_definitions.go   # --icon/--color flags
git show ca38c8529 -- apps/cli                                     # blueprint ui passthrough + unit test
```

### 1. Bump the SDK pin first

```bash
cd apps/cli
go get github.com/emergent-company/emergent.memory/apps/server/pkg/sdk@v<next-tag>
go mod tidy
```

### 2. `agent_definitions.go` — flags + request wiring

Add `defIcon`/`defColor` package vars, register `--icon`/`--color` on both `create` and `update`, and build the blob (create: only when a value is set; update: only when the flags changed, so an omitted flag preserves an existing appearance):

```go
func buildUIConfig(icon, color string) (json.RawMessage, error) {
	m := map[string]string{}
	if icon != "" {
		m["icon"] = icon
	}
	if color != "" {
		m["color"] = color
	}
	return json.Marshal(m)
}
```

```go
// update path
if cmd.Flags().Changed("icon") || cmd.Flags().Changed("color") {
	ui, err := buildUIConfig(defIcon, defColor)
	if err != nil {
		return fmt.Errorf("failed to build uiConfig: %w", err)
	}
	updateReq.UIConfig = ui
	hasUpdate = true
}
```

### 3. `internal/blueprints/` — blueprint `ui` passthrough

`types.go`:

```go
type AgentFile struct {
	// …
	UI *AgentUI `json:"ui" yaml:"ui"`
}

// AgentUI mirrors the server-side manifest's ui block.
type AgentUI struct {
	Icon  string `json:"icon"  yaml:"icon"`
	Color string `json:"color" yaml:"color"`
}
```

`applier.go` — apply in both `agentFileToCreateRequest` and `agentFileToUpdateRequest`, returning `nil` for an absent/empty block so an update preserves rather than clears:

```go
func agentUIConfig(ui *AgentUI) json.RawMessage {
	if ui == nil || (ui.Icon == "" && ui.Color == "") {
		return nil
	}
	m := map[string]string{}
	if ui.Icon != "" {
		m["icon"] = ui.Icon
	}
	if ui.Color != "" {
		m["color"] = ui.Color
	}
	raw, err := json.Marshal(m)
	if err != nil {
		return nil
	}
	return raw
}
```

`loader.go` `loadAgents` decodes the whole `AgentFile` from YAML/JSON, so no change is needed for the new block.

### 4. Tests

`internal/blueprints/applier_ui_test.go` (package `blueprints` — the exported-func tests in `applier_test.go` are `blueprints_test` and cannot reach the unexported converters): table-driven assertions that nil/empty → `nil` and icon/color (singly and together) → the expected JSON object, for **both** the create and update request builders. Keep `go test ./internal/blueprints/...` green.

### 5. Verify

```bash
cd apps/cli
PATH="/root/go/bin:$PATH" go build ./... && PATH="/root/go/bin:$PATH" go test ./...
PATH="/root/go/bin:$PATH" gofmt -l internal/cmd internal/blueprints   # must be empty
```

`cli.yml` is path-filtered to `apps/cli/**`, so a CLI-only PR runs the Lint/Test/Build matrix against the bumped SDK — that is the real gate for this work.

### Spec

Add a `cli-agent-appearance`-appropriate delta (or a MODIFIED requirement on `agent-appearance`) covering `--icon`/`--color` and the blueprint `ui` passthrough, in the same PR as the implementation.
