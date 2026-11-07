package mcp

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// TestSuperadminBooleanDefinitionsResolve pins the gap round-1 review found: the
// strict-boolean boundary must be able to find the admin catalogue's
// definitions. Admin tools are deliberately absent from toolIndex, so a
// GetToolByName lookup returns nil and validateBooleanArgs silently validates
// nothing — {"confirm": "true"} would still degrade a mutation to a dry-run.
func TestSuperadminBooleanDefinitionsResolve(t *testing.T) {
	svc := &Service{}
	defs := svc.GetSuperadminToolDefinitions()
	require.NotEmpty(t, defs, "the admin catalogue must not be empty")

	booleans := 0
	for _, def := range defs {
		found := svc.superadminToolDefinition(def.Name)
		require.NotNil(t, found,
			"admin tool %q has no resolvable definition, so its boolean args cannot be validated", def.Name)
		require.Equal(t, def.Name, found.Name)

		for argName, prop := range def.InputSchema.Properties {
			if prop.Type != "boolean" {
				continue
			}
			booleans++
			err := validateBooleanArgs(found, map[string]any{argName: "true"})
			require.Error(t, err,
				"admin tool %q must refuse a string for boolean %q", def.Name, argName)
			require.Contains(t, err.Error(), argName)
		}
	}
	require.NotZero(t, booleans,
		"expected at least one admin tool to declare a boolean argument; otherwise this test proves nothing")
}
