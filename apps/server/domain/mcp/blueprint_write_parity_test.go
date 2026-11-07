package mcp_test

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// TestCatalogParityWriteToolsRegistered pins the write tools added for the
// blueprint/schema catalog parity work (#18): each must be discoverable in the
// served catalog under the schema:write scope. A definition with no dispatch
// case is a tool a client can discover and never run, so dispatch is covered
// separately by TestBlueprintWriteToolsReachTheHandler.
func TestCatalogParityWriteToolsRegistered(t *testing.T) {
	svc := fullCatalog(t)

	cases := map[string]string{
		"blueprint-delete":                 "schema:write",
		"blueprint-deprecate":              "schema:write",
		"blueprint-import":                 "schema:write",
		"blueprint-updates-check":          "schema:write",
		"blueprint-updates-apply":          "schema:write",
		"blueprint-model-overrides-put":    "schema:write",
		"blueprint-github-credentials-put": "schema:write",
	}
	for name, wantScope := range cases {
		def := svc.GetToolByName(name)
		require.NotNil(t, def, "tool %q must be registered in the catalog", name)
		require.Equal(t, wantScope, def.RequiredScope,
			"tool %q must declare the write scope it was documented under", name)
		require.NotEmpty(t, def.Description, "tool %q needs a description an agent can act on", name)
	}

	// The GitHub-credentials tool must never advertise a named output key that
	// could echo the PAT; it uses the generic object envelope with no declared
	// properties, so the response contract cannot promise to return the token.
	creds := svc.GetToolByName("blueprint-github-credentials-put")
	require.Contains(t, creds.InputSchema.Properties, "token")
	require.Nil(t, creds.OutputSchema.Properties,
		"blueprint-github-credentials-put must not declare output keys that could echo the token")
}
