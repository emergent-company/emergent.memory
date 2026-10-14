## Why

A chat run inside a gVisor agent sandbox failed with two hard tool failures: `run_python` → `/bin/sh: python3: not found` (exit 127) and `run_go` → `cp: can't stat '/sdk-template': No such file or directory` (exit 1). The default sandbox image `ghcr.io/emergent-company/memory-workspace:latest`, built from `docker/workspace-base.Dockerfile`, is Alpine 3.19 with only git/curl/wget/jq/ripgrep/etc — no python3, no go, no `/sdk-template`, no `pyrunner` daemon. `run_python` needs python3 or the pyrunner daemon; `run_go` needs go + `/sdk-template`.

A second, load-bearing defect compounds this: `CheckoutService` always clones unauthenticated — `apps/server/domain/sandbox/module.go:94` passes a `nil` credential provider, so a configured repository URL never uses credentials and the agent definition's `GH_TOKEN` env var is used for nothing. The memory repo itself is **private**: an anonymous `https://github.com/emergent-company/emergent.memory` clone returns HTTP 404, while `https://x-access-token:$GH_TOKEN@github.com/emergent-company/emergent.memory` succeeds. So without token threading, even a correctly configured `repo_source` cannot clone this repo.

## What Changes

1. **Thread a git token into repository checkout.** `CloneRepository` gains a `token` parameter; `buildCloneURL` injects it as `https://x-access-token:<token>@github.com/…` for GitHub HTTPS URLs when the token is non-empty, falling back to the GitHub App credential provider (when non-nil) and then to unauthenticated clone. The auto-provisioner resolves the token with precedence `cfg.EnvVars` → `runtimeEnvVars` (GH_TOKEN before GITHUB_TOKEN), and the `workspace_git` clone tool resolves it from the per-session env. The token is never logged and never returned in errors (stderr is passed through `sanitizeGitOutput`).

2. **Combined workspace image (Python + Go SDK tooling).** Rewrite `docker/workspace-base.Dockerfile` on `golang:1.25-bookworm` to provide both runtimes: keep the core tools, add python3+pip with the emergent Python SDK, install the `pyrunner` daemon at `/usr/local/bin/pyrunner.py`, and build `/sdk` + `/sdk-template` exactly as `sdk/go/Dockerfile` does. The entrypoint starts the pyrunner daemon so gVisor (which overrides CMD but preserves ENTRYPOINT) still boots the daemon. Mirror the identical content into `GetWorkspaceBaseDockerfile()`.

3. **Build-time credential for the private-repo fetch.** The image's two SDK-fetch steps (`pip install git+…` and the sparse `git clone`) authenticate via an optional BuildKit secret `github_token`. Inside each `RUN --mount=type=secret,id=github_token,required=false` layer, the token is written to a temporary `~/.netrc`, the fetch runs, and the file is removed before the layer ends — so the token never persists in the image. The publish workflow passes `secrets.GITHUB_TOKEN` (its `contents: read` permission already reads the private repo), and the CLI's `BuildSandboxImages` passes `--secret id=github_token,env=GH_TOKEN` (falling back to `GITHUB_TOKEN`) when such a token is present in the environment, omitting it otherwise. With no secret, the fetch proceeds unauthenticated for public forks.

## Impact

- **Sandbox image** grows substantially (Go toolchain + Python SDK + pre-cached Go module deps), replacing the slim Alpine base. This is intended: the default image must support both `run_python` and `run_go` out of the box.
- **Checkout auth** is now credential-aware for private GitHub repos; the unauthenticated path remains only as the no-token fallback for genuinely public repos.
- **Image build** now requires a GitHub token to fetch the private repo at build time; CI supplies it via `secrets.GITHUB_TOKEN` and the CLI via `GH_TOKEN`/`GITHUB_TOKEN`, with an unauthenticated fallback for public forks.
- No API/schema change. Existing tests that encoded the old `CloneRepository` signature are updated (not deleted).

## Capabilities

### New Capabilities

- `agent-sandbox-workspace`: the default workspace image provides Python and Go SDK execution tooling, and repository checkout authenticates with the sandbox's configured git token when present.

### Modified Capabilities

None.
