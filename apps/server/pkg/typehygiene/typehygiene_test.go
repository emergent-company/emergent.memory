package typehygiene

import "testing"

func TestNormalizeKey(t *testing.T) {
	cases := map[string]string{
		"Organization":  "organization",
		"organisation":  "organization",
		"Organisations": "organization",
		"organizations": "organization",
		"Location":      "location",
		"Locations":     "location",
		"Place":         "location",
		"places":        "location",
		"Person":        "person",
		"People":        "person",
		"Company":       "company",
		"Companies":     "company",
		"Status":        "status",
		"Statuses":      "status",
		"Address":       "address",
		"Addresses":     "address",
		"Meeting Note":  "meetingnote",
	}
	for input, want := range cases {
		if got := NormalizeKey(input); got != want {
			t.Errorf("NormalizeKey(%q) = %q, want %q", input, got, want)
		}
	}
}

func TestResolverPrefersExistingName(t *testing.T) {
	resolver := NewResolver("Organization", "Location")

	if got, ok := resolver.Resolve("Organisation"); !ok || got != "Organization" {
		t.Fatalf("Resolve(Organisation) = %q, %v; want Organization, true", got, ok)
	}
	if got, ok := resolver.Resolve("Organizations"); !ok || got != "Organization" {
		t.Fatalf("Resolve(Organizations) = %q, %v; want Organization, true", got, ok)
	}
	if got, ok := resolver.Resolve("Place"); !ok || got != "Location" {
		t.Fatalf("Resolve(Place) = %q, %v; want Location, true", got, ok)
	}
	if _, ok := resolver.Resolve("Gadget"); ok {
		t.Fatal("Gadget must not resolve against existing names")
	}
}

func TestResolverResolveOrAddKeepsFirstSpelling(t *testing.T) {
	resolver := NewResolver()
	first := resolver.ResolveOrAdd("Projects")
	if first != "Project" {
		t.Fatalf("first new name = %q, want Project", first)
	}
	if second := resolver.ResolveOrAdd("project"); second != "Project" {
		t.Fatalf("case variant = %q, want Project", second)
	}
}

func TestDisplaySingular(t *testing.T) {
	cases := map[string]string{
		"Organizations": "Organization",
		"people":        "person",
		"Meeting Notes": "Meeting Note",
		"Organization":  "Organization",
	}
	for input, want := range cases {
		if got := DisplaySingular(input); got != want {
			t.Errorf("DisplaySingular(%q) = %q, want %q", input, got, want)
		}
	}
}
