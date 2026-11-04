package apitoken

import (
	"reflect"
	"testing"

	"github.com/stretchr/testify/require"
)

// fieldValidateTag returns the `validate` struct tag of v's named field, or ""
// when the type has no such field.
func fieldValidateTag(v any, field string) string {
	f, ok := reflect.TypeOf(v).FieldByName(field)
	if !ok {
		return ""
	}
	return f.Tag.Get("validate")
}

// TestUserMintableScopes pins the two-catalogue split: the stored vocabulary
// keeps the retired umbrellas and the reserved markers, and the user-mintable
// subset is exactly the stored list minus those six.
func TestUserMintableScopes(t *testing.T) {
	stored := make(map[string]bool, len(ValidApiTokenScopes))
	for _, scope := range ValidApiTokenScopes {
		require.False(t, stored[scope], "ValidApiTokenScopes lists %q twice", scope)
		stored[scope] = true
	}
	for _, retired := range []string{"data:read", "data:write", "device:api", "webhook:trigger", "mcp:agent-call", "share:agent-chat"} {
		require.True(t, stored[retired], "the stored catalogue must keep %q so existing credentials keep their access", retired)
		require.True(t, nonMintableScopes[retired], "%q must be non-mintable", retired)
	}

	mintable := make(map[string]bool, len(UserMintableScopes))
	for _, scope := range UserMintableScopes {
		require.False(t, nonMintableScopes[scope], "UserMintableScopes leaked non-mintable %q", scope)
		require.True(t, stored[scope], "UserMintableScopes contains %q which is not a stored scope", scope)
		mintable[scope] = true
	}

	require.Len(t, UserMintableScopes, len(ValidApiTokenScopes)-len(nonMintableScopes),
		"the mintable catalogue must be derived from the stored one, not hand-listed")
	for _, scope := range []string{"documents:read", "documents:write", "search", "graph:read", "journal:read"} {
		require.True(t, mintable[scope], "%q must be mintable — it is the fine-grained replacement for the retired umbrellas", scope)
	}
}

// TestValidateTokenScopes pins the rejection shape: the first offending scope is
// named and the error is a 400, whichever catalogue is used.
func TestValidateTokenScopes(t *testing.T) {
	require.NoError(t, validateTokenScopes([]string{"documents:read", "search"}, UserMintableScopes))
	require.NoError(t, validateTokenScopes([]string{"schema:read", "data:read"}, ValidApiTokenScopes),
		"the stored catalogue must still accept the retired umbrella")

	for _, tc := range []struct {
		name    string
		scopes  []string
		allowed []string
		want    string
	}{
		{"data:read is not mintable", []string{"documents:read", "data:read"}, UserMintableScopes, "invalid scope: data:read"},
		{"data:write is not mintable", []string{"data:write"}, UserMintableScopes, "invalid scope: data:write"},
		{"a marker is not mintable", []string{"device:api"}, UserMintableScopes, "invalid scope: device:api"},
		{"an unknown scope is rejected against the stored catalogue too", []string{"chunks:read"}, ValidApiTokenScopes, "invalid scope: chunks:read"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := validateTokenScopes(tc.scopes, tc.allowed)
			require.Error(t, err)
			require.Contains(t, err.Error(), tc.want)
		})
	}
}

// TestRequestScopeTagsExcludeRetiredUmbrellas pins the second rejection layer:
// the HTTP bind tags must not list the retired umbrellas (or any marker), so a
// request is rejected at binding time as well as in the service. The tags are
// strings, so this asserts on the DTO types the handlers bind into.
func TestRequestScopeTagsExcludeRetiredUmbrellas(t *testing.T) {
	for _, tag := range []string{
		fieldValidateTag(CreateApiTokenRequest{}, "Scopes"),
		fieldValidateTag(CreateAccountTokenRequest{}, "Scopes"),
		fieldValidateTag(UpdateApiTokenScopesRequest{}, "Scopes"),
	} {
		require.NotEmpty(t, tag, "the Scopes field must carry a validate tag")
		for _, retired := range []string{"data:read", "data:write", "device:api", "webhook:trigger", "mcp:agent-call", "share:agent-chat"} {
			require.NotContains(t, tag, retired, "the oneof tag must not offer %q", retired)
		}
		for _, mintable := range []string{"documents:read", "documents:write", "search", "journal:read"} {
			require.Contains(t, tag, mintable, "the oneof tag must offer %q", mintable)
		}
	}
}
