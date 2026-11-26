package agents

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/emergent-company/emergent.memory/domain/sandbox"
	"github.com/emergent-company/emergent.memory/internal/config"
)

// fakeSourceCredentialMinter records every mint so a test can assert both the
// call count and the exact permissions requested. It returns a configurable
// token/error and never talks to GitHub.
type fakeSourceCredentialMinter struct {
	calls       int
	projectID   string
	sourceID    string
	repo        string
	permissions map[string]string
	token       string
	err         error
}

func (f *fakeSourceCredentialMinter) MintSourceCredential(_ context.Context, projectID, sourceID, repo string, permissions map[string]string) (string, error) {
	f.calls++
	f.projectID = projectID
	f.sourceID = sourceID
	f.repo = repo
	f.permissions = make(map[string]string, len(permissions))
	for k, v := range permissions {
		f.permissions[k] = v
	}
	if f.err != nil {
		return "", f.err
	}
	if f.token == "" {
		return "ghs_test_token", nil
	}
	return f.token, nil
}

func issueSubject() *SourceSubject {
	return &SourceSubject{SourceID: "src-1", Repo: "acme/repo", Kind: "issue", IssueNumber: 9}
}

func credentialTestExecutor(minter SourceCredentialMinter) *AgentExecutor {
	return &AgentExecutor{sourceCredentials: minter, log: testAgentLogger()}
}

func TestApplySourceGitCredential_IssueSubjectMintsLeastPrivilegeToken(t *testing.T) {
	minter := &fakeSourceCredentialMinter{}
	ae := credentialTestExecutor(minter)
	req := &ExecuteRequest{
		ProjectID:       "proj-1",
		AgentDefinition: sandboxedDefinition(),
		SourceSubject:   issueSubject(),
	}

	ae.applySourceGitCredential(context.Background(), "run-1", req)

	require.Equal(t, 1, minter.calls, "an issue subject provisions a credential")
	assert.Equal(t, "proj-1", minter.projectID)
	assert.Equal(t, "src-1", minter.sourceID)
	assert.Equal(t, "acme/repo", minter.repo)
	assert.Equal(t, map[string]string{
		"contents":      "write",
		"pull_requests": "write",
	}, minter.permissions,
		"the implicit credential must request exactly the issue-coding permissions")
	assert.Equal(t, "ghs_test_token", req.EnvVars["GH_TOKEN"])
}

func TestApplySourceGitCredential_PresetTokenWins(t *testing.T) {
	minter := &fakeSourceCredentialMinter{}
	ae := credentialTestExecutor(minter)
	req := &ExecuteRequest{
		ProjectID:       "proj-1",
		AgentDefinition: sandboxedDefinition(),
		SourceSubject:   issueSubject(),
		EnvVars:         map[string]string{"GH_TOKEN": "caller-token"},
	}

	ae.applySourceGitCredential(context.Background(), "run-1", req)

	assert.Zero(t, minter.calls, "a caller-supplied GH_TOKEN suppresses the implicit mint")
	assert.Equal(t, "caller-token", req.EnvVars["GH_TOKEN"], "the preset token is left in place")
}

func TestApplySourceGitCredential_MetadataOnlySourceDoesNotMint(t *testing.T) {
	minter := &fakeSourceCredentialMinter{}
	ae := credentialTestExecutor(minter)
	req := &ExecuteRequest{
		ProjectID:       "proj-1",
		AgentDefinition: sandboxedDefinition(),
		// Model-settable provenance only: no trusted run column. This must never
		// mint, which is the whole point of using run.SourceSubject.
		TriggerMetadata: map[string]any{
			"sourceId":        "src-1",
			"repo":            "acme/repo",
			"subjectObjectId": "obj-1",
			"issue_number":    float64(9),
			"source_id":       "src-1",
		},
	}

	ae.applySourceGitCredential(context.Background(), "run-1", req)

	assert.Zero(t, minter.calls, "trigger metadata must never mint a write credential")
	assert.NotContains(t, req.EnvVars, "GH_TOKEN")
}

func TestApplySourceGitCredential_PullRequestSubjectDoesNotMint(t *testing.T) {
	minter := &fakeSourceCredentialMinter{}
	ae := credentialTestExecutor(minter)
	req := &ExecuteRequest{
		ProjectID:       "proj-1",
		AgentDefinition: sandboxedDefinition(),
		SourceSubject: &SourceSubject{
			SourceID: "src-1", Repo: "acme/repo", Kind: "pull_request", PRNumber: 7, HeadSHA: "abc",
		},
	}

	ae.applySourceGitCredential(context.Background(), "run-1", req)

	assert.Zero(t, minter.calls, "a review subject keeps its narrower explicit credential path")
	assert.NotContains(t, req.EnvVars, "GH_TOKEN")
}

func TestApplySourceGitCredential_MintErrorIsBestEffort(t *testing.T) {
	minter := &fakeSourceCredentialMinter{err: errors.New("installation deleted")}
	ae := credentialTestExecutor(minter)
	req := &ExecuteRequest{
		ProjectID:       "proj-1",
		AgentDefinition: sandboxedDefinition(),
		SourceSubject:   issueSubject(),
	}

	require.NotPanics(t, func() {
		ae.applySourceGitCredential(context.Background(), "run-1", req)
	})

	assert.Equal(t, 1, minter.calls)
	assert.NotContains(t, req.EnvVars, "GH_TOKEN",
		"a failed implicit mint must leave the run provisioning without a credential")
}

// TestExecuteWithRun_MintsImplicitCredentialFromRunSourceSubject is the
// regression test for the dead queued-run path: the worker builds an
// ExecuteRequest with no trigger metadata, so the only trusted source identity
// is the run row's SourceSubject column. ExecuteWithRun must copy it and reach
// the provisioning branch with a minted credential. Image readiness fails on
// purpose so the run stops before any real container work.
func TestExecuteWithRun_MintsImplicitCredentialFromRunSourceSubject(t *testing.T) {
	repo := newRootCaptureRepository(t)
	minter := &fakeSourceCredentialMinter{}
	ae := &AgentExecutor{
		repo:              repo,
		safeguards:        config.AgentSafeguardsConfig{ExecutionEnabled: true},
		wsEnabled:         true,
		provisioner:       sandbox.NewAutoProvisioner(nil, nil, nil, nil, nil, testAgentLogger(), failingImageResolver{}),
		sourceCredentials: minter,
		log:               testAgentLogger(),
	}

	run := &AgentRun{ID: "run-1", Status: RunStatusRunning, SourceSubject: issueSubject()}
	def := &AgentDefinition{
		ID:            "def-1",
		SandboxConfig: map[string]any{"enabled": true, "base_image": "missing-image"},
	}

	result, err := ae.ExecuteWithRun(context.Background(), run, ExecuteRequest{
		AgentDefinition: def,
		ProjectID:       "proj-1",
		OrgID:           "org-1",
		UserMessage:     "fix the issue",
	})

	require.NoError(t, err)
	require.NotNil(t, result)
	assert.Equal(t, RunStatusError, result.Status, "the failing image stops the run after the mint")
	require.Equal(t, 1, minter.calls,
		"ExecuteWithRun must mint from run.SourceSubject even when the request carries no metadata")
	assert.Equal(t, "src-1", minter.sourceID)
	assert.Equal(t, "acme/repo", minter.repo)
	assert.Equal(t, map[string]string{"contents": "write", "pull_requests": "write"}, minter.permissions)
}
