package graph

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// TestLabelToTypeKey pins the conversion used for human-readable labels
// (inverseLabel, and the documented "Has Employees" → "has_employees" case).
func TestLabelToTypeKey(t *testing.T) {
	cases := map[string]string{
		"hasEmployees":       "has_employees",
		"HasEmployees":       "has_employees",
		"HAS_EMPLOYEES":      "has_employees",
		"has employees":      "has_employees",
		"has-employees":      "has_employees",
		"  Has  Employees  ": "has_employees",
		"EX_SPOUSE_OF":       "ex_spouse_of",
		"ex_spouse_of":       "ex_spouse_of",
		"":                   "",
		"   ":                "",
	}
	for in, want := range cases {
		assert.Equal(t, want, labelToTypeKey(in), "labelToTypeKey(%q)", in)
	}
}

// TestInverseEntry is the mutation test for #198. inverseEntry keys the inverse
// map, and maybeCreateInverse looks that map up with the normalised write type
// while the value it returns is written back through CreateRelationship. Before
// the fix a pack declaring `inverseType: hasEmployees` was keyed "hasemployees"
// against a stored "has_employees", so GetInverseType missed and no inverse
// relationship was ever created.
func TestInverseEntry(t *testing.T) {
	cases := []struct {
		name      string
		relType   string
		schema    relationshipTypeSchema
		wantKey   string
		wantInv   string
		wantDeriv bool
	}{
		{
			name:    "camelCase declared on both sides",
			relType: "hasEmployees",
			schema:  relationshipTypeSchema{InverseType: "hasEmployees"},
			wantKey: "has_employees", wantInv: "has_employees",
		},
		{
			name:    "PascalCase declared inverse",
			relType: "hasEmployees",
			schema:  relationshipTypeSchema{InverseType: "HasEmployees"},
			wantKey: "has_employees", wantInv: "has_employees",
		},
		{
			name:    "snake_case is unchanged",
			relType: "EX_SPOUSE_OF",
			schema:  relationshipTypeSchema{InverseType: "spouse_of"},
			wantKey: "ex_spouse_of", wantInv: "spouse_of",
		},
		{
			name:    "hyphenated type keeps the shape the write path stores",
			relType: "has-employees",
			schema:  relationshipTypeSchema{InverseType: "has-employees"},
			wantKey: "has-employees", wantInv: "has-employees",
		},
		{
			name:      "inverseLabel is human text, converted as a label",
			relType:   "hasEmployees",
			schema:    relationshipTypeSchema{InverseLabel: "Has Employees"},
			wantKey:   "has_employees",
			wantInv:   "has_employees",
			wantDeriv: true,
		},
		{
			name:    "no inverse declared",
			relType: "belongs_to",
			schema:  relationshipTypeSchema{},
		},
		{
			name:    "blank type is skipped",
			relType: "   ",
			schema:  relationshipTypeSchema{InverseType: "anything"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			key, inv, derived := inverseEntry(tc.relType, tc.schema)
			assert.Equal(t, tc.wantKey, key)
			assert.Equal(t, tc.wantInv, inv)
			assert.Equal(t, tc.wantDeriv, derived)
		})
	}
}

// TestInverseEntryKeyMatchesStoredType states the invariant directly: whatever
// a pack declares, the map key must equal the type the write path would store
// for that declaration — that equality is what makes GetInverseType find it.
func TestInverseEntryKeyMatchesStoredType(t *testing.T) {
	for _, declared := range []string{
		"hasEmployees", "HasEmployees", "HAS_EMPLOYEES",
		"has employees", "has-employees", "has_employees",
	} {
		key, _, _ := inverseEntry(declared, relationshipTypeSchema{InverseType: "x"})
		assert.Equal(t, normalizeRelationType(declared), key,
			"inverse-map key drifted from the stored type for %q", declared)
	}
}
