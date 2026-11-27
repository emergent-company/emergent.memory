package sandbox

import (
	"errors"
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
func TestInstallGitHooks_RunsLefthookInstallInTheClone(t *testing.T) {
	cp := newHooksTestCheckoutProvider(map[string]*ExecResult{
		"lefthook install": {ExitCode: 0, Stdout: "sync hooks: pre-commit, pre-push"},
	}, nil)

	cs := NewCheckoutService(&mockCredentialProvider{}, testLogger())

	require.NoError(t, cs.InstallGitHooks(t.Context(), cp, "container-1", "/workspace"))

	cp.mu.Lock()
	defer cp.mu.Unlock()
	require.Len(t, cp.execCalls, 1)
	assert.Contains(t, cp.execCalls[0], "lefthook install")
	// The binary check must come first: `lefthook install` alone would be a
	// shell error whose message does not say the image is at fault.
	assert.Contains(t, cp.execCalls[0], "command -v lefthook")
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

func TestInstallGitHooks_WrapsAnExecFailure(t *testing.T) {
	cp := newHooksTestCheckoutProvider(nil, map[string]error{
		"lefthook install": errors.New("container gone"),
	})

	cs := NewCheckoutService(&mockCredentialProvider{}, testLogger())

	err := cs.InstallGitHooks(t.Context(), cp, "container-1", "/workspace")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "install git hooks")
	assert.True(t, strings.Contains(err.Error(), "container gone"), "error should carry the cause: %v", err)
}
