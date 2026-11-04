package agents

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/emergent-company/emergent.memory/domain/mcp"
)

// TestConvertMCPSchemaToADK_PreservesArrayItems pins #2374: the converter copied
// Type/Description/Enum/Min/Max/Default and dropped Items, so every array
// parameter reached the model as {"type":"array"} with no element type. The
// model then had to guess the shape, and a wrong guess surfaced as a validation
// error it could not foresee.
func TestConvertMCPSchemaToADK_PreservesArrayItems(t *testing.T) {
	cases := []struct {
		name     string
		items    *mcp.PropertySchema
		wantType string
	}{
		{name: "array of strings", items: &mcp.PropertySchema{Type: "string"}, wantType: "string"},
		{name: "array of objects", items: &mcp.PropertySchema{Type: "object"}, wantType: "object"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := convertMCPSchemaToADK(mcp.InputSchema{
				Type: "object",
				Properties: map[string]mcp.PropertySchema{
					"values": {Type: "array", Description: "the values", Items: tc.items},
				},
			})

			require.NotNil(t, got)
			prop, ok := got.Properties["values"]
			require.True(t, ok, "property missing from the converted schema")
			assert.Equal(t, "array", prop.Type)
			require.NotNil(t, prop.Items,
				"array property lost its element schema: the model sees no element type")
			assert.Equal(t, tc.wantType, prop.Items.Type)
			assert.Equal(t, "the values", prop.Description,
				"the property description must survive too")
		})
	}
}

// TestConvertMCPSchemaToADK_PreservesNestedArrayItems covers the recursion: an
// array of arrays is expressible in the MCP schema, so the conversion has to
// bottom out on its own rather than converting one level and stopping.
func TestConvertMCPSchemaToADK_PreservesNestedArrayItems(t *testing.T) {
	got := convertMCPSchemaToADK(mcp.InputSchema{
		Type: "object",
		Properties: map[string]mcp.PropertySchema{
			"matrix": {
				Type:  "array",
				Items: &mcp.PropertySchema{Type: "array", Items: &mcp.PropertySchema{Type: "number"}},
			},
		},
	})

	require.NotNil(t, got)
	outer := got.Properties["matrix"]
	require.NotNil(t, outer)
	require.NotNil(t, outer.Items, "outer array lost its element schema")
	assert.Equal(t, "array", outer.Items.Type)
	require.NotNil(t, outer.Items.Items, "inner array lost its element schema")
	assert.Equal(t, "number", outer.Items.Items.Type)
}

// TestConvertMCPSchemaToADK_PreservesArrayItemConstraints keeps the recursion
// honest about the parts of an element schema the catalogue can express: an
// array of enum-constrained strings is the shape `graph-branch-fork.filter_types`
// and friends rely on.
func TestConvertMCPSchemaToADK_PreservesArrayItemConstraints(t *testing.T) {
	got := convertMCPSchemaToADK(mcp.InputSchema{
		Type: "object",
		Properties: map[string]mcp.PropertySchema{
			"types": {
				Type:  "array",
				Items: &mcp.PropertySchema{Type: "string", Enum: []string{"Person", "Company"}},
			},
		},
	})

	require.NotNil(t, got)
	items := got.Properties["types"].Items
	require.NotNil(t, items)
	assert.Equal(t, []any{"Person", "Company"}, items.Enum,
		"element enum must survive, not just the element type")
}

// TestConvertMCPSchemaToADK_CatalogArraysKeepItems asserts the invariant over
// the built-in catalogue rather than a hand-built shape: for every tool in it,
// every array parameter must still declare its element type after the
// conversion. It is `&mcp.Service{}` without the handler-contributed tools
// registered — none of those declares an array parameter today, so nothing is
// missed now, and the walk grows cover automatically once the pool is built from
// the full service. The catalogue-side half (each array property *declares*
// items) is TestCatalogArrayPropertiesDeclareItems in domain/mcp, which runs
// against the full catalogue; this is the half that proves the declaration
// survives the path to the model.
func TestConvertMCPSchemaToADK_CatalogArraysKeepItems(t *testing.T) {
	svc := &mcp.Service{}
	defs := svc.GetToolDefinitions()
	require.NotEmpty(t, defs, "catalogue is empty — the conversion has nothing to prove")

	var arrays int
	for _, def := range defs {
		converted := convertMCPSchemaToADK(def.InputSchema)
		require.NotNil(t, converted, "tool %q: conversion returned nil for a typed input schema", def.Name)

		for name, prop := range converted.Properties {
			if prop.Type != "array" {
				continue
			}
			arrays++
			require.NotNil(t, prop.Items,
				"tool %q property %q reaches the agent as a bare array: no element type",
				def.Name, name)
			require.NotEmpty(t, prop.Items.Type,
				"tool %q property %q has an element schema with no type", def.Name, name)
		}
	}
	require.NotZero(t, arrays, "no array parameters found — the catalogue shape changed")
}
