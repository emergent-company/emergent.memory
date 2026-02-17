package workspace

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
// Handles public/private repos, branch/SHA checkout, and retries.
// Returns nil on success or if no repository URL is provided.
func (cs *CheckoutService) CloneRepository(ctx context.Context, provider Provider, providerID string, repoURL string, branch string) error {
	if repoURL == "" {
		return nil // No repo to clone
	}

	// Build clone URL (with credentials for private repos)
	cloneURL, err := cs.buildCloneURL(ctx, repoURL)
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

		cmd := fmt.Sprintf("git clone --depth 1 %q /workspace 2>&1", cloneURL)
		if branch != "" && !isSHA(branch) {
			cmd = fmt.Sprintf("git clone --depth 1 --branch %q %q /workspace 2>&1", branch, cloneURL)
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
				Workdir:   "/workspace",
				TimeoutMs: cloneTimeoutMs,
			})

			checkoutResult, err := provider.Exec(ctx, providerID, &ExecRequest{
				Command:   fmt.Sprintf("git checkout %q 2>&1", branch),
				Workdir:   "/workspace",
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

	// Save current remote URL, inject token, run command, restore original
	script := fmt.Sprintf(`
ORIG_URL=$(git remote get-url origin 2>/dev/null)
if [ -n "$ORIG_URL" ]; then
    # Inject token into URL
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
		name = "Emergent Agent"
		email = "agent@emergent.local"
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

// buildCloneURL creates an authenticated clone URL using GitHub App installation tokens.
func (cs *CheckoutService) buildCloneURL(ctx context.Context, repoURL string) (string, error) {
	if cs.credProvider == nil {
		return repoURL, nil
	}

	token, err := cs.credProvider.GetInstallationToken(ctx)
	if err != nil {
		return "", err
	}

	// Inject token: https://github.com/org/repo -> https://x-access-token:TOKEN@github.com/org/repo
	if strings.HasPrefix(repoURL, "https://") {
		return strings.Replace(repoURL, "https://", fmt.Sprintf("https://x-access-token:%s@", token), 1), nil
	}

	return repoURL, nil
}

// isSHA checks if a string looks like a git commit SHA (40 or 7+ hex chars).
var shaPattern = regexp.MustCompile(`^[0-9a-fA-F]{7,40}$`)

func isSHA(s string) bool {
	return shaPattern.MatchString(s)
}
