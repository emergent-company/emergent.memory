package sandbox

import (
	"errors"
	"os"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newHooksTestCheckoutProvider(results map[string]*ExecResult, errs map[string]error) *checkoutProvider {
	return &checkoutProvider{
		mockProvider: mockProvider{name: "gv", providerType: ProviderGVisor, healthy: true},
		execResults:  results,
		execErrors:   errs,
	}
}

// A fresh clone has no hooks: installing them is what makes an agent's `git
// commit` run the repository's pre-commit gate instead of reaching CI ungated
// (#1164, #1166, #1191).
func TestInstallGitHooks_InstallsTheSandboxSubsetInTheClone(t *testing.T) {
	cp := newHooksTestCheckoutProvider(map[string]*ExecResult{
		"lefthook install": {ExitCode: 0, Stdout: "sync hooks: pre-commit, pre-push"},
	}, nil)

	cs := NewCheckoutService(&mockCredentialProvider{}, testLogger())

	require.NoError(t, cs.InstallGitHooks(t.Context(), cp, "container-1", "/workspace"))

	cp.mu.Lock()
	defer cp.mu.Unlock()
	require.Len(t, cp.execCalls, 1)
	cmd := cp.execCalls[0]
	assert.Contains(t, cmd, "command -v lefthook")
	assert.Contains(t, cmd, "lefthook install")
	// The installed pre-commit hook must run the tagged subset, not the whole
	// set: the image has no templ/golangci-lint/ruff/swiftlint and a whole-tree
	// go vet/build does not fit a git operation's budget, so an untagged run
	// would fail every commit instead of catching a mistake.
	assert.Contains(t, cmd, "--tag "+sandboxHooksTag)
	assert.Contains(t, cmd, "chmod 0755 .git/hooks/pre-commit")
	// A safety valve for an image without gitleaks; the job still scans whenever
	// the binary is present.
	assert.Contains(t, cmd, "export GITLEAKS_OPTIONAL=1")
}

// `lefthook install` CREATES a lefthook.yml when the repository has none, which
// the agent's `git add -A` would then commit into the customer's repository. A
// repository without the config is a no-op, not an install.
func TestInstallGitHooks_NoLefthookConfigIsANoOp(t *testing.T) {
	cp := newHooksTestCheckoutProvider(nil, nil)

	cs := NewCheckoutService(&mockCredentialProvider{}, testLogger())

	require.NoError(t, cs.InstallGitHooks(t.Context(), cp, "container-1", "/workspace"))

	cp.mu.Lock()
	defer cp.mu.Unlock()
	require.Len(t, cp.execCalls, 1)
	assert.Contains(t, cp.execCalls[0], "[ ! -f lefthook.yml ]")
	// The guard has to run before the install, not after it.
	assert.Less(t,
		strings.Index(cp.execCalls[0], "[ ! -f lefthook.yml ]"),
		strings.Index(cp.execCalls[0], "lefthook install"),
	)
}

// Provisioning treats a hook failure as a warning, but the error must still say
// which of the two fixes applies: a missing binary is an image problem, a
// non-zero exit from an installed binary is a repository problem.
func TestInstallGitHooks_ReportsAMissingBinaryDistinctly(t *testing.T) {
	cp := newHooksTestCheckoutProvider(map[string]*ExecResult{
		"lefthook install": {
			ExitCode: 127,
			Stderr:   "lefthook is not installed in the workspace image; commits in this sandbox run no pre-commit hooks",
		},
	}, nil)

	cs := NewCheckoutService(&mockCredentialProvider{}, testLogger())

	err := cs.InstallGitHooks(t.Context(), cp, "container-1", "")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "exit 127")
	assert.Contains(t, err.Error(), "not installed in the workspace image")
}

// The failure message must carry both streams: `lefthook install` reports on
// stderr, a shell-level failure in the shim reports on stdout, and an empty
// message reads as a silent failure.
func TestInstallGitHooks_ReportsBothStreams(t *testing.T) {
	cp := newHooksTestCheckoutProvider(map[string]*ExecResult{
		"lefthook install": {ExitCode: 1, Stderr: "  ", Stdout: "cat: .git/hooks/pre-commit: No such file or directory"},
	}, nil)

	cs := NewCheckoutService(&mockCredentialProvider{}, testLogger())

	err := cs.InstallGitHooks(t.Context(), cp, "container-1", "/workspace")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "No such file or directory")
}

func TestInstallGitHooks_WrapsAnExecFailure(t *testing.T) {
	cp := newHooksTestCheckoutProvider(nil, map[string]error{
		"lefthook install": errors.New("container gone"),
	})

	cs := NewCheckoutService(&mockCredentialProvider{}, testLogger())

	err := cs.InstallGitHooks(t.Context(), cp, "container-1", "/workspace")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "install git hooks")
	assert.Contains(t, err.Error(), "container gone")
}

// The tag is the seam between this code and lefthook.yml: the hook runs `--tag
// sandbox`, and the cheap gates opt in by carrying that tag. If the tag is
// dropped from every job the hook silently runs nothing and the workspace is
// ungated again, with no error anywhere — so pin both directions here.
func TestLefthookYAMLTagsExactlyTheSandboxRunnableGates(t *testing.T) {
	raw, err := os.ReadFile("../../../../lefthook.yml")
	if err != nil {
		t.Skipf("lefthook.yml not reachable from this build (%v)", err)
	}
	tagged := lefthookJobsTagged(string(raw), sandboxHooksTag)

	// The two gates whose absence produced the red PRs this change exists for.
	for _, must := range []string{"server-gofmt", "server-lint-ratchet"} {
		assert.Contains(t, tagged, must, "the sandbox hook must run %s", must)
	}
	// Jobs the workspace image cannot run, or cannot run in a commit's time
	// budget. Tagging one of these blocks every agent commit instead of
	// catching a mistake.
	for _, mustNot := range []string{
		"server-go-vet", "server-go-build", "server-golangci-config",
		"webui-templ", "webui-go-build", "webui-ruff",
		"connector-mac-swift", "ios-swift",
	} {
		assert.NotContains(t, tagged, mustNot, "%s cannot run in a workspace and must not be tagged", mustNot)
	}
}

// lefthookJobsTagged returns the names of pre-commit jobs carrying tag. It reads
// the `- name:` line and the following `tags:` line(s), which is the shape
// lefthook itself parses for this key.
func lefthookJobsTagged(yaml, tag string) []string {
	var (
		names   []string
		current string
	)
	for _, line := range strings.Split(yaml, "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "- name:") {
			current = strings.TrimSpace(strings.TrimPrefix(trimmed, "- name:"))
			continue
		}
		if current == "" || !strings.HasPrefix(trimmed, "tags:") {
			continue
		}
		if strings.Contains(trimmed, tag) {
			names = append(names, current)
		}
	}
	return names
}
