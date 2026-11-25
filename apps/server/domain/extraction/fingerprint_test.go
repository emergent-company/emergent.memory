package extraction

import "testing"

func TestHashTypeNamesDeterministic(t *testing.T) {
	if hashTypeNames([]string{"Decision", "Person"}) != hashTypeNames([]string{"Decision", "Person"}) {
		t.Fatal("hash must be deterministic")
	}
	// Order-sensitive: the sorted registry order is stable, but a different set
	// must not collide.
	if hashTypeNames([]string{"Decision", "Person"}) == hashTypeNames([]string{"Decision", "Person", "Meeting"}) {
		t.Fatal("different type sets must produce different fingerprints")
	}
	if hashTypeNames(nil) == "" {
		t.Fatal("empty set must still hash to a non-empty string")
	}
}
