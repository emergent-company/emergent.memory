package agents

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// Go randomises map iteration order, so these builders that ranged a schema map
// produced a different prompt on every run of the same document. That is
// code-level nondeterminism the pipeline owns (issue #240); sorting makes two
// identical inputs produce identical requests.

func TestBuildEntityExtractionPrompt_TypeOrderIsStable(t *testing.T) {
	schemas := map[string]ObjectSchema{
		"Person":    {Name: "Person", Description: "a person"},
		"Company":   {Name: "Company", Description: "an organisation"},
		"Address":   {Name: "Address", Description: "a place"},
		"Equipment": {Name: "Equipment", Description: "a thing"},
	}

	// Repeat so a randomised map order has many chances to show up.
	for i := 0; i < 20; i++ {
		prompt := BuildEntityExtractionPrompt("document text", schemas, nil, nil)
		require.Contains(t, prompt, "Extract ONLY these types: Address, Company, Equipment, Person\n")
	}
}

func TestBuildEntityNormalizationPrompt_TypeOrderIsStable(t *testing.T) {
	schemas := map[string]ObjectSchema{
		"Person":  {Name: "Person"},
		"Company": {Name: "Company"},
		"Address": {Name: "Address"},
	}

	for i := 0; i < 20; i++ {
		prompt := BuildEntityNormalizationPrompt(nil, schemas, nil, nil, nil)
		address := strings.Index(prompt, "### Address")
		company := strings.Index(prompt, "### Company")
		person := strings.Index(prompt, "### Person")
		require.GreaterOrEqual(t, address, 0)
		require.GreaterOrEqual(t, company, 0)
		require.GreaterOrEqual(t, person, 0)
		require.Less(t, address, company, "schema sections must be sorted")
		require.Less(t, company, person)
	}
}

func TestBuildRelationshipPrompt_TypeOrderIsStable(t *testing.T) {
	schemas := map[string]RelationshipSchema{
		"works_at": {Name: "works_at"},
		"owns":     {Name: "owns"},
		"located":  {Name: "located"},
	}

	for i := 0; i < 20; i++ {
		prompt := BuildRelationshipPrompt(nil, schemas, "document text", nil, nil)
		located := strings.Index(prompt, "### located")
		owns := strings.Index(prompt, "### owns")
		worksAt := strings.Index(prompt, "### works_at")
		require.GreaterOrEqual(t, located, 0)
		require.GreaterOrEqual(t, owns, 0)
		require.GreaterOrEqual(t, worksAt, 0)
		require.Less(t, located, owns, "relationship sections must be sorted")
		require.Less(t, owns, worksAt)
	}
}

func TestBuildEntitySchemaFromMemorySchema_EnumIsSorted(t *testing.T) {
	schemas := map[string]ObjectSchema{
		"Person":  {Name: "Person"},
		"Company": {Name: "Company"},
		"Address": {Name: "Address"},
	}

	for i := 0; i < 20; i++ {
		schema := BuildEntitySchemaFromMemorySchema(schemas)
		require.NotNil(t, schema)
		enum := schema.Properties["entities"].Items.Properties["type"].Enum
		require.Equal(t, []string{"Address", "Company", "Person"}, enum)
	}
}

func TestBuildRelationshipSchemaFromMemorySchema_EnumIsSorted(t *testing.T) {
	schemas := map[string]RelationshipSchema{
		"works_at": {Name: "works_at"},
		"owns":     {Name: "owns"},
		"located":  {Name: "located"},
	}

	for i := 0; i < 20; i++ {
		schema := BuildRelationshipSchemaFromMemorySchema(schemas)
		require.NotNil(t, schema)
		enum := schema.Properties["relationships"].Items.Properties["type"].Enum
		require.Equal(t, []string{"located", "owns", "works_at"}, enum)
	}
}
