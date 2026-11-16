package agents

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestValidateInvocation(t *testing.T) {
	require.NoError(t, ValidateInvocation(nil))
	require.NoError(t, ValidateInvocation(&AgentInvocation{
		EnvVars: map[string]string{"CI": "true", "TZ": "UTC"},
		Model:   "provider/model",
	}))
	require.ErrorContains(t, ValidateInvocation(&AgentInvocation{
		EnvVars: map[string]string{"PATH": "/tmp"},
	}), "not allowed")
	require.ErrorContains(t, ValidateInvocation(&AgentInvocation{
		Credential: &AgentCredentialRef{Kind: "github_app", Permissions: map[string]string{"admin": "write"}},
	}), "not allowed")
	require.ErrorContains(t, ValidateInvocation(&AgentInvocation{
		Credential: &AgentCredentialRef{Kind: "github_app", Permissions: map[string]string{"contents": "admin"}},
	}), "read or write")
	require.ErrorContains(t, ValidateInvocation(&AgentInvocation{
		Credential: &AgentCredentialRef{Kind: "github_app", SourceID: "not-a-uuid"},
	}), "UUID")
}

func TestNormalizeInvocationDefaultsCredential(t *testing.T) {
	inv := NormalizeInvocation(&AgentInvocation{Credential: &AgentCredentialRef{}})
	require.NotNil(t, inv.Credential)
	require.Equal(t, CredentialKindGitHubApp, inv.Credential.Kind)
	require.Equal(t, "read", inv.Credential.Permissions[PermissionContents])
}
