## Why

`memory blueprints install <source>` applies packs, then agents, then skills, then seed data without running the offline validator first. A blueprint whose agent `model.name` is not in `provider/model` form (e.g. the bare `gemini-2.5-flash`) is rejected by the server at agent-create time — after packs are already created and assigned, leaving a partial install behind. The web-UI import path was made preflight-validated and atomic in #1585; the CLI path still applies partially (issue #1586).

## What Changes

- Run the offline blueprint validator (`blueprints.Preflight`) before any API call in the install path, and abort with a non-zero exit and an actionable error when it reports errors — so a malformed blueprint applies nothing.
- Validate and normalize every agent `model` reference through the shared `modelref` parser (`apps/server/pkg/sdk/modelref`) before applying: a structured `model.provider` is folded into the canonical `provider/model` name (mirroring the server's `NormalizeAgentModelConfig`), and a malformed reference is rejected with the agent name, the offending value, and the expected form.
- Add an optional structured `model.provider` field to the CLI blueprint agent model so the prefixed and structured shapes agree with the server.
- Cover the new behavior with unit tests, including a mutation-tested guard proving install makes zero API calls when the model is invalid.

No **BREAKING** changes; a blueprint that passes offline validation installs as before, and blueprints with a valid prefixed model are unaffected.

## Capabilities

### New Capabilities

None.

### Modified Capabilities

- `blueprint-api`: the CLI blueprint install path gains a pre-apply offline validation + model-normalization contract (previously only `validate` performed any offline check).

## Impact

- `apps/cli/internal/blueprints/types.go` — add `AgentModel.Provider`.
- `apps/cli/internal/blueprints/validator.go` — shared modelref validation, `NormalizeAgentModels`, `Preflight`.
- `apps/cli/internal/cmd/blueprints.go` — run `Preflight` in `runBlueprintsInstall` before the applier.
- `apps/cli/internal/blueprints/validator_test.go`, `apps/cli/internal/blueprints/preinstall_test.go` — unit + guard coverage.
- No server, SDK, or blueprint-format breaking change; the server already accepts the canonical `provider/model` name produced by normalization.
