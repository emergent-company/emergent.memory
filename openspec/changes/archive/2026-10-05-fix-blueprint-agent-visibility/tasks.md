# Tasks — fix-blueprint-agent-visibility

## 1. Align the CLI validator with the server enum

- [x] 1.1 Fix `validVisibilities` in `apps/cli/internal/blueprints/validator.go` to `{"", "project", "external", "internal"}` and add a comment pointing at `apps/server/domain/agents/entity.go` (`VisibilityProject|External|Internal`; empty normalizes to `project`). The SDK exposes visibility only as a `string`, so no shared enum source exists.
- [x] 1.2 Fix the validation message to `invalid visibility %q (valid: project, external, internal)`.
- [x] 1.3 Extend `TestValidateAgents_ServerEnums` in `apps/cli/internal/blueprints/validator_test.go`: accepts `project|external|internal` + empty; rejects `public|private|system`; asserts the message lists the canonical set and no longer lists the legacy set.

## 2. OpenSpec delta

- [x] 2.1 Add `specs/blueprint-api/spec.md` delta pinning the blueprint agent visibility enum to the server's canonical levels.
- [x] 2.2 `openspec validate fix-blueprint-agent-visibility --strict` passes.

## 3. Verification

- [x] 3.1 `cd apps/cli && go build ./...`
- [x] 3.2 `cd apps/cli && go test -count=1 ./internal/blueprints/...`
- [x] 3.3 `gofmt -l` on changed Go files reports nothing.
- [x] 3.4 Confirm no server change is required (server already validates the enum).
