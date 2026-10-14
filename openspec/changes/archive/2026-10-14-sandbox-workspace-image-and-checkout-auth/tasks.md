## 1. Spec artifacts

- [x] 1.1 Write `openspec/changes/sandbox-workspace-image-and-checkout-auth/proposal.md`
- [x] 1.2 Write `openspec/changes/sandbox-workspace-image-and-checkout-auth/tasks.md`
- [x] 1.3 Write `openspec/changes/sandbox-workspace-image-and-checkout-auth/specs/agent-sandbox-workspace/spec.md`

## 2. Thread a git token into repository checkout

- [x] 2.1 Add `token string` parameter to `CloneRepository` in `apps/server/domain/sandbox/checkout.go`
- [x] 2.2 Update `buildCloneURL(ctx, repoURL, token)` to inject `https://x-access-token:<token>@github.com/…` when `token != ""` and the URL has prefix `https://github.com/`, otherwise fall back to the credProvider path (non-nil) or return the URL unchanged
- [x] 2.3 Add `ResolveGitToken(envs ...map[string]string)` helper (GH_TOKEN before GITHUB_TOKEN within each map, first map wins)
- [x] 2.4 Never log the token — keep logging `repoURL` only, and keep `sanitizeGitOutput` on stderr
- [x] 2.5 In `attemptProvision`, resolve the token with precedence `cfg.EnvVars` → `runtimeEnvVars` and pass it to `CloneRepository`
- [x] 2.6 Wire the same token into `workspace_git` clone via `deps.SessionEnv` (reuse `CheckoutService` + `ResolveGitToken`, no URL-building duplication)
- [x] 2.7 Update `handler.go` caller (pass empty token — HTTP create path has no token)

## 3. Update tests for the new signature + add coverage

- [x] 3.1 Update `checkout_test.go` callers and add `buildCloneURL` with-token + `ResolveGitToken` precedence tests
- [x] 3.2 Update `integration_mock_test.go` callers; add explicit-token "used" and "not leaked in error" tests
- [x] 3.3 Update `auto_provisioner_test.go` callers

## 4. Combined workspace image (Python + Go SDK tooling)

- [x] 4.1 Rewrite `docker/workspace-base.Dockerfile` on `golang:1.25-bookworm` with a comment stating Go 1.25 is required by the SDK
- [x] 4.2 Keep core tools: bash, git, curl, wget, ca-certificates, jq, ripgrep, grep, sed, gawk, findutils, tree, tar, gzip, unzip, build-essential
- [x] 4.3 Add python3 + pip and install the emergent Python SDK (`git+…#subdirectory=sdk/python`) plus requests/pydantic/python-dateutil
- [x] 4.4 Install the pyrunner daemon at `/usr/local/bin/pyrunner.py` (exact script body from `GetPythonSDKDockerfile`) and `chmod +x`
- [x] 4.5 Build `/sdk` + `/sdk-template` exactly as `sdk/go/Dockerfile` (sparse clone, replace directive, `go mod tidy` + `go build .`, remove stub `main.go`)
- [x] 4.6 `WORKDIR /workspace`, `RUN mkdir -p /workspace`
- [x] 4.7 `ENTRYPOINT ["sh","-c","python3 /usr/local/bin/pyrunner.py & exec \"$@\"","--"]` + `CMD ["sleep","infinity"]`
- [x] 4.8 Mirror identical content into `GetWorkspaceBaseDockerfile()`
- [x] 4.9 Authenticate both SDK-fetch steps (`pip install` + sparse `git clone`) via an optional BuildKit secret `github_token` (`RUN --mount=type=secret,id=github_token,required=false`); write a temporary `~/.netrc`, run the fetch, and `rm -f ~/.netrc` in the SAME layer so the token never persists
- [x] 4.10 Pass `secrets.GITHUB_TOKEN` as the `github_token` secret in `.github/workflows/publish-workspace-image.yml` (keep `contents: read` + `packages: write`)
- [x] 4.11 Extend CLI `BuildSandboxImages` to pass `--secret id=github_token,env=GH_TOKEN` (fall back to `GITHUB_TOKEN`) when a token env var is present; update the call sites for the new signature

## 5. Verification

- [x] 5.1 `go build ./...` (apps/server + apps/cli)
- [x] 5.2 `go test ./apps/server/domain/sandbox/... ./apps/server/domain/agents/...`
- [x] 5.3 `task lint` (report any pre-existing failures distinctly)
- [x] 5.4 `docker build --secret id=github_token,env=GH_TOKEN -f docker/workspace-base.Dockerfile -t emergent-workspace-test:local .` + runtime smoke checks (py-sdk import, go version, /sdk-template, pyrunner FIFO)
- [x] 5.5 `openspec validate`
- [x] 5.6 Prove the token is NOT in the image (`~/.netrc` absent; `docker history --no-trunc | grep -i token` empty)
