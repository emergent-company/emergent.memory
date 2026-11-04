// Package api_test — graph_property_validation_test.go
//
// Tests for graph object property type validation and coercion.
// Ported from emergent.memory/apps/server/tests/e2e/graph_property_validation_test.go
//
// Note: Tests that rely on template packs/schemas being installed via direct DB
// access are skipped. The property coercion tests require a schema to be active
// for the project which normally is set up via direct DB injection. Since we
// cannot do that in external server mode, all tests in this file are skipped.
package api_test

import (
	"testing"
)

// All property validation tests require a template pack to be linked to the
// project via direct DB access (INSERT into kb.graph_template_packs and
// kb.project_template_packs). This is not possible in external server mode.

func TestGraphPropertyValidation_NumberCoercion(t *testing.T) {
	t.Skip("requires direct DB access to install template pack schema — not available in external server mode")
}

func TestGraphPropertyValidation_BooleanCoercion(t *testing.T) {
	t.Skip("requires direct DB access to install template pack schema — not available in external server mode")
}

func TestGraphPropertyValidation_DateCoercion(t *testing.T) {
	t.Skip("requires direct DB access to install template pack schema — not available in external server mode")
}

func TestGraphPropertyValidation_InvalidNumber(t *testing.T) {
	t.Skip("requires direct DB access to install template pack schema — not available in external server mode")
}

func TestGraphPropertyValidation_InvalidDate(t *testing.T) {
	t.Skip("requires direct DB access to install template pack schema — not available in external server mode")
}

func TestGraphPropertyValidation_MissingRequiredProperty(t *testing.T) {
	t.Skip("requires direct DB access to install template pack schema — not available in external server mode")
}

func TestGraphPropertyValidation_PatchObject_ValidatesProperties(t *testing.T) {
	t.Skip("requires direct DB access to install template pack schema — not available in external server mode")
}

func TestGraphPropertyValidation_PatchObject_InvalidPropertyValue(t *testing.T) {
	t.Skip("requires direct DB access to install template pack schema — not available in external server mode")
}

func TestGraphPropertyValidation_AllowsUnknownProperties(t *testing.T) {
	t.Skip("requires direct DB access to install template pack schema — not available in external server mode")
}
