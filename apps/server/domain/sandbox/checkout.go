package sandbox

import (
	"context"
	"fmt"
	"log/slog"
	"regexp"
	"strings"
	"time"
)

const (
	maxCloneRetries   = 3
	initialRetryDelay = 2 * time.Second
	cloneTimeoutMs    = 300000 // 5 minutes for clone operations
	pushPullTimeoutMs = 120000 // 2 minutes for push/pull
)

// GitCredentialProvider is an interface for getting GitHub installation tokens.
// This breaks the circular dependency between workspace and githubapp packages.
type GitCredentialProvider interface {
	// GetInstallationToken returns a valid short-lived GitHub installation access token.
	GetInstallationToken(ctx context.Context) (string, error)
	// GetBotIdentity returns the git user.name and user.email for commits.
	GetBotIdentity(ctx context.Context) (name string, email string, err error)
}

// CheckoutService handles git clone operations and credential injection for workspaces.
type CheckoutService struct {
	credProvider GitCredentialProvider
	log          *slog.Logger
}

// NewCheckoutService creates a new checkout service.
func NewCheckoutService(credProvider GitCredentialProvider, log *slog.Logger) *CheckoutService {
	return &CheckoutService{
		credProvider: credProvider,
		log:          log.With("component", "workspace-checkout"),
	}
}

// CloneRepository clones a repository into a workspace container.
// destPath is the absolute path inside the container to clone into (e.g. "/workspace").
// token is an optional git token (GH_TOKEN/GITHUB_TOKEN) used to authenticate the
// clone; when non-empty it is injected into https://github.com/ URLs so that private
// repositories can be cloned. The token is never logged or returned in errors.
// Handles public/private repos, branch/SHA checkout, and retries.
// Returns nil on success or if no repository URL is provided.
func (cs *CheckoutService) CloneRepository(ctx context.Context, provider Provider, providerID string, repoURL string, branch string, destPath string, token string) error {
	if repoURL == "" {
		return nil // No repo to clone
	}
	if destPath == "" {
		destPath = "/workspace"
	}

	// Build clone URL (with credentials for private repos)
	cloneURL, err := cs.buildCloneURL(ctx, repoURL, token)
	if err != nil {
		cs.log.Warn("failed to build authenticated clone URL, trying unauthenticated",
			"repo", repoURL,
			"error", err,
		)
		cloneURL = repoURL
	}

	// Clone with retry
	var lastErr error
	for attempt := 0; attempt < maxCloneRetries; attempt++ {
		if attempt > 0 {
			delay := initialRetryDelay * time.Duration(1<<(attempt-1)) // exponential: 2s, 4s, 8s
			cs.log.Info("retrying clone", "attempt", attempt+1, "delay", delay, "repo", repoURL)
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(delay):
			}
		}

		cmd := fmt.Sprintf("git clone --depth 1 %q %q 2>&1", cloneURL, destPath)
		if branch != "" && !isSHA(branch) {
			cmd = fmt.Sprintf("git clone --depth 1 --branch %q %q %q 2>&1", branch, cloneURL, destPath)
		}

		result, err := provider.Exec(ctx, providerID, &ExecRequest{
			Command:   cmd,
			TimeoutMs: cloneTimeoutMs,
		})
		if err != nil {
			lastErr = fmt.Errorf("exec failed: %w", err)
			continue
		}
		if result.ExitCode != 0 {
			lastErr = fmt.Errorf("git clone failed (exit %d): %s", result.ExitCode, sanitizeGitOutput(result.Stderr))
			continue
		}

		// Clone succeeded — now handle SHA checkout if needed
		if branch != "" && isSHA(branch) {
			// Need full history for SHA checkout
			_, _ = provider.Exec(ctx, providerID, &ExecRequest{
				Command:   "git fetch --unshallow 2>&1 || true",
				Workdir:   destPath,
				TimeoutMs: cloneTimeoutMs,
			})

			checkoutResult, err := provider.Exec(ctx, providerID, &ExecRequest{
				Command:   fmt.Sprintf("git checkout %q 2>&1", branch),
				Workdir:   destPath,
				TimeoutMs: 30000,
			})
			if err != nil {
				return fmt.Errorf("SHA checkout failed: %w", err)
			}
			if checkoutResult.ExitCode != 0 {
				return fmt.Errorf("SHA checkout failed (exit %d): %s", checkoutResult.ExitCode, checkoutResult.Stderr)
			}
		}

		// Configure bot identity
		cs.configureGitIdentity(ctx, provider, providerID)

		cs.log.Info("repository cloned successfully",
			"repo", repoURL,
			"branch", branch,
			"dest", destPath,
		)
		return nil
	}

	cs.log.Error("all clone retries exhausted",
		"repo", repoURL,
		"attempts", maxCloneRetries,
		"last_error", lastErr,
	)
	return lastErr
}

// InjectCredentialsForPush temporarily injects credentials into the git remote URL
// for a push/pull operation, then removes them after completion.
func (cs *CheckoutService) InjectCredentialsForPush(ctx context.Context, provider Provider, providerID string, gitCmd string) (*ExecResult, error) {
	// Guard nil credential provider — fall back to unauthenticated
	if cs.credProvider == nil {
		cs.log.Warn("no credentials provider configured for git operation, trying unauthenticated")
		return provider.Exec(ctx, providerID, &ExecRequest{
			Command:   gitCmd,
			Workdir:   "/workspace",
			TimeoutMs: pushPullTimeoutMs,
		})
	}

	// Get token
	token, err := cs.credProvider.GetInstallationToken(ctx)
	if err != nil {
		// Fall back to unauthenticated (will likely fail for private repos)
		cs.log.Warn("no credentials available for git operation, trying unauthenticated", "error", err)
		return provider.Exec(ctx, providerID, &ExecRequest{
			Command:   gitCmd,
			Workdir:   "/workspace",
			TimeoutMs: pushPullTimeoutMs,
		})
	}

	// Inject credentials using git credential helper approach
	// The token is ephemeral and workspace is isolated, so this is safe
	script := fmt.Sprintf(`
ORIG_URL=$(git remote get-url origin 2>/dev/null)
if [ -n "$ORIG_URL" ]; then
    # Inject token into URL (ephemeral token, isolated workspace)
    AUTH_URL=$(echo "$ORIG_URL" | sed "s|https://|https://x-access-token:%s@|")
    git remote set-url origin "$AUTH_URL" 2>/dev/null
    %s 2>&1
    EXIT_CODE=$?
    # Restore original URL
    git remote set-url origin "$ORIG_URL" 2>/dev/null
    exit $EXIT_CODE
else
    %s 2>&1
fi
`, token, gitCmd, gitCmd)

	return provider.Exec(ctx, providerID, &ExecRequest{
		Command:   script,
		Workdir:   "/workspace",
		TimeoutMs: pushPullTimeoutMs,
	})
}

// ConfigureGitAuth writes git and GitHub CLI credentials into a sandbox so the
// agent can push and open pull requests, not just read the repository.
//
// It is a post-assignment setup step rather than container environment: a
// warm-pool hit discards the CreateContainerRequest.Env the provisioner built,
// so env vars reach a cold-booted container only. Writing the config files works
// on both paths and survives for the life of the sandbox.
//
// The token is passed to the shell through ExecRequest.Env, so it never appears
// in the command string (which providers may log or expose as a process
// argument). It is written to files in the workspace container, which is
// isolated and ephemeral — deliberately not to the agent definition, whose
// stored config keeps only the secret *reference* (#1844).
func (cs *CheckoutService) ConfigureGitAuth(ctx context.Context, provider Provider, providerID, token string) error {
	if token == "" {
		return nil
	}

	// git: a credential-helper store entry, so `git push/fetch` authenticates even
	// when the agent re-clones or adds a remote. `gh`: hosts.yml plus config.yml.
	//
	// gh is deliberately configured in the *legacy single-account* shape
	// (`config.yml` version "1", no `users:` map in hosts.yml). With the
	// multi-account shape gh treats the stored login as unknown, runs its
	// multi-account migration, and resolves the login over GraphQL before every
	// command. A token the API rejects — expired, revoked, or a GitHub App
	// installation token — then makes gh refuse *all* commands, `gh --version`
	// included ("cowardly refusing to continue with multi account migration"),
	// instead of reporting a bad credential. Pinning version "1" keeps a rejected
	// token confined to the call that needed it.
	//
	// `: "${GH_TOKEN:?...}"` makes a provider that ignores ExecRequest.Env (e2b)
	// fail loudly here rather than write an empty credential and report success.
	const script = `set -e
: "${GH_TOKEN:?GH_TOKEN must be set}"
umask 077
mkdir -p "$HOME/.config/gh"
printf 'version: "1"\n' > "$HOME/.config/gh/config.yml"
cat > "$HOME/.config/gh/hosts.yml" <<EOF
github.com:
    user: x-access-token
    oauth_token: $GH_TOKEN
    git_protocol: https
EOF
printf 'https://x-access-token:%s@github.com\n' "$GH_TOKEN" > "$HOME/.git-credentials"
git config --global credential.helper store
`

	result, err := provider.Exec(ctx, providerID, &ExecRequest{
		Command: script,
		Workdir: "/workspace",
		Env:     map[string]string{"GH_TOKEN": token},
	})
	if err != nil {
		return fmt.Errorf("configure sandbox git auth: %w", err)
	}
	if result.ExitCode != 0 {
		// Exit code and stderr only — the command carries no token, and the stderr
		// from these builtins cannot echo one.
		return fmt.Errorf("configure sandbox git auth: exit %d: %s", result.ExitCode, sanitizeGitOutput(result.Stderr))
	}
	cs.log.Info("sandbox git auth configured")
	return nil
}

// sandboxHooksTag is the lefthook tag whose jobs a workspace runs on commit.
//
// The full pre-commit set cannot run in a sandbox: the image ships go, gofmt,
// git, grep and gitleaks, but no templ, golangci-lint, ruff or swiftlint, and a
// whole-tree `go vet ./...`/`go build ./...` is minutes on a cold clone inside a
// git operation's 60 s budget. Running everything would fail on a missing binary
// and block every commit, which is worse than the ungated commit this installs
// to fix (product #1191). So lefthook.yml tags the cheap, toolchain-light checks
// (`sandbox`) and this hook runs only those; everything else stays a developer
// machine and CI concern.
const sandboxHooksTag = "sandbox"

const (
	// hooksInstallTimeoutMs bounds `lefthook install` plus writing the shim.
	hooksInstallTimeoutMs = 60000

	// gitHooksShimMarker starts the generated shim, so a re-run can tell its own
	// file from a hook a developer or a previous install wrote.
	gitHooksShimMarker = "# installed-by: memory-sandbox-provisioner"
)

// sandboxPreCommitHook replaces the hook `lefthook install` writes. That hook
// runs the whole pre-commit set; this one runs only the jobs tagged
// sandboxHooksTag, and sets GITLEAKS_OPTIONAL as a safety valve so an image
// without gitleaks skips the scan instead of failing every commit (the scan
// still runs whenever the binary is present, which the image guarantees).
const sandboxPreCommitHook = "#!/bin/sh\n" +
	gitHooksShimMarker + "\n" +
	"# Runs the repository's lefthook pre-commit jobs tagged `sandbox`: the checks\n" +
	"# the workspace image can actually run. Full-tree build/vet, templ, golangci-lint,\n" +
	"# ruff and swiftlint are developer-machine and CI checks (product #1191).\n" +
	"export GITLEAKS_OPTIONAL=1\n" +
	"exec lefthook run pre-commit --tag " + sandboxHooksTag + " --no-auto-install\n"

// gitHooksInstallCommand installs the repository's hooks into the clone and
// narrows the pre-commit hook to the sandbox tag.
//
// It reports a missing binary explicitly (exit 127) instead of letting
// `lefthook: not found` scroll past as a generic failure, because the two cases
// need different fixes: a missing binary is an image problem, a non-zero from an
// installed binary is a repository problem. A repository with no lefthook config
// is a no-op, not a failure — and it must not fall through to `lefthook install`,
// which *creates* a lefthook.yml when none exists, leaving a file the agent's
// `git add -A` would commit into the customer's repository.
const gitHooksInstallCommand = `set -e
if [ ! -f lefthook.yml ]; then
  echo "no lefthook.yml at the repository root; nothing to install"
  exit 0
fi
command -v lefthook >/dev/null 2>&1 || {
  echo "lefthook is not installed in the workspace image; commits in this sandbox run no pre-commit hooks" >&2
  exit 127
}
lefthook install
cat > .git/hooks/pre-commit <<'MEMORY_SANDBOX_HOOK'
` + sandboxPreCommitHook + `MEMORY_SANDBOX_HOOK
chmod 0755 .git/hooks/pre-commit`

// InstallGitHooks installs the repository's committed git hooks (lefthook.yml)
// into a freshly cloned workspace, so `git commit` runs the sandbox subset of
// the pre-commit gate.
//
// Git hooks live in .git/hooks of each clone, so a clone is born without them:
// `git clone` never installs hooks and nothing in the repository sets
// core.hooksPath. Without this call an agent's commit runs no local gate at all,
// which is how an unformatted file or a lint-ratchet increase reaches CI as a red
// PR with nobody to fix it (product #1164, #1166, #1191).
//
// Best effort by design: a repository without a lefthook config, or an older
// workspace image without the binary, must still produce a usable workspace.
// The caller logs the error; this reports it so the caller can.
func (cs *CheckoutService) InstallGitHooks(ctx context.Context, provider Provider, providerID, destPath string) error {
	if destPath == "" {
		destPath = "/workspace"
	}
	result, err := provider.Exec(ctx, providerID, &ExecRequest{
		Command:   gitHooksInstallCommand,
		Workdir:   destPath,
		TimeoutMs: hooksInstallTimeoutMs,
	})
	if err != nil {
		return fmt.Errorf("install git hooks: %w", err)
	}
	if result.ExitCode != 0 {
		// Both streams: `lefthook install` writes its progress and its errors to
		// stderr, but a shell-level failure (the heredoc, chmod) reports on
		// stdout, and an empty message is indistinguishable from a silent failure.
		return fmt.Errorf("install git hooks: exit %d: %s", result.ExitCode,
			sanitizeGitOutput(strings.TrimSpace(result.Stdout+"\n"+result.Stderr)))
	}
	cs.log.Info("sandbox git hooks installed", "path", destPath, "tag", sandboxHooksTag)
	return nil
}

// configureGitIdentity sets the git user.name and user.email in the workspace.
func (cs *CheckoutService) configureGitIdentity(ctx context.Context, provider Provider, providerID string) {
	var name, email string
	if cs.credProvider != nil {
		var err error
		name, email, err = cs.credProvider.GetBotIdentity(ctx)
		if err != nil {
			name = ""
			email = ""
		}
	}
	if name == "" || email == "" {
		name = "Memory Agent"
		email = "agent@memory.local"
		cs.log.Debug("using default git identity (no GitHub App configured)")
	}

	cmd := fmt.Sprintf(
		`git config user.name %q && git config user.email %q`,
		name, email,
	)
	_, execErr := provider.Exec(ctx, providerID, &ExecRequest{
		Command: cmd,
		Workdir: "/workspace",
	})
	if execErr != nil {
		cs.log.Warn("failed to configure git identity", "error", execErr)
	}
}

// buildCloneURL creates an authenticated clone URL. An explicit token (from
// GH_TOKEN/GITHUB_TOKEN) takes precedence: when non-empty and the repo URL is a
// GitHub HTTPS URL, it is injected as `https://x-access-token:<token>@github.com/…`.
// Otherwise it falls back to the configured GitCredentialProvider (GitHub App
// installation tokens), and finally to the repoURL unchanged (public clone).
func (cs *CheckoutService) buildCloneURL(ctx context.Context, repoURL string, token string) (string, error) {
	// Explicit token (GH_TOKEN/GITHUB_TOKEN) wins — this is the load-bearing path
	// for private repositories when no GitHub App credential provider is wired.
	if token != "" && strings.HasPrefix(repoURL, "https://github.com/") {
		return strings.Replace(repoURL, "https://github.com/", fmt.Sprintf("https://x-access-token:%s@github.com/", token), 1), nil
	}

	if cs.credProvider == nil {
		return repoURL, nil
	}

	credToken, err := cs.credProvider.GetInstallationToken(ctx)
	if err != nil {
		return "", err
	}

	// Inject token: https://github.com/org/repo -> https://x-access-token:TOKEN@github.com/org/repo
	if strings.HasPrefix(repoURL, "https://") {
		return strings.Replace(repoURL, "https://", fmt.Sprintf("https://x-access-token:%s@", credToken), 1), nil
	}

	return repoURL, nil
}

// ResolveGitToken returns the first non-empty git token across the given env maps,
// checking GH_TOKEN before GITHUB_TOKEN within each map, in the order the maps are
// provided. Returns "" when no token is configured.
func ResolveGitToken(envs ...map[string]string) string {
	for _, env := range envs {
		for _, key := range []string{"GH_TOKEN", "GITHUB_TOKEN"} {
			if v := env[key]; v != "" {
				return v
			}
		}
	}
	return ""
}

// ResolveGitTokenForRun resolves the effective token for a run: a run-scoped
// source credential (GH_TOKEN minted by the source integration) wins over a
// deployment/agent fallback. This is the path used by provisioning and clone.
func ResolveGitTokenForRun(cfg *AgentSandboxConfig, runtimeEnv ...map[string]string) string {
	if t := ResolveGitToken(runtimeEnv...); t != "" {
		return t
	}
	return ResolveGitTokenForConfig(cfg)
}

// ResolveGitTokenForConfig resolves the effective git token for a sandbox config
// using the documented precedence (first non-empty wins):
//
//  1. cfg.GitTokenSecretRef → resolved from the server environment (allowlisted
//     name only; see ResolveGitTokenSecretRef).
//  2. cfg.GitToken — the legacy plaintext literal backfilled by migration 00244.
//  3. cfg.EnvVars GH_TOKEN / GITHUB_TOKEN — pre-migration stored literals.
//  4. runtimeEnv maps — per-run env vars (GH_TOKEN / GITHUB_TOKEN), never persisted.
//
// The token is never logged.
func ResolveGitTokenForConfig(cfg *AgentSandboxConfig, runtimeEnv ...map[string]string) string {
	if cfg != nil {
		if t := ResolveGitTokenSecretRef(cfg.GitTokenSecretRef); t != "" {
			return t
		}
		if cfg.GitToken != "" {
			return cfg.GitToken
		}
		if t := ResolveGitToken(cfg.EnvVars); t != "" {
			return t
		}
	}
	return ResolveGitToken(runtimeEnv...)
}

// isSHA checks if a string looks like a git commit SHA (40 or 7+ hex chars).
var shaPattern = regexp.MustCompile(`^[0-9a-fA-F]{7,40}$`)

func isSHA(s string) bool {
	return shaPattern.MatchString(s)
}
