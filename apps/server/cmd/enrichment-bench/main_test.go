package main

import "testing"

func TestMatchEntity(t *testing.T) {
	cases := []struct {
		obj, gt string
		want    bool
	}{
		{"Monica Geller", "Monica Geller", true},
		{"monica geller", "Monica Geller", true},
		{"Monica", "Monica Geller", true}, // shorter name subset, one extra token
		{"Monica Geller (chef)", "Monica Geller", true},
		{"Central Perk", "Central Perk", true},
		{"Central Perk coffeehouse", "Central Perk", true}, // one extra token
		{"Phoebe", "Monica Geller", false},
		{"Ross Geller", "Rachel Green", false},
		{"Carol and Ross's baby", "Carol", false}, // four extra tokens over-matches
		{"Carol", "Carol and Ross's baby", false}, // symmetric
		{"Monica Geller", "Monica", true},         // symmetric of the lenient case
	}
	for _, c := range cases {
		if got := matchEntity(c.obj, c.gt); got != c.want {
			t.Fatalf("matchEntity(%q, %q) = %v, want %v", c.obj, c.gt, got, c.want)
		}
	}
}

func TestScoreEpisode(t *testing.T) {
	cumulative := []entity{
		{Name: "Monica Geller", Type: "Person"},
		{Name: "Central Perk", Type: "Location"},
	}

	// Clean: both found, one object each.
	objects := []graphObject{
		{EntityID: "a", Name: "Monica Geller", Type: "Character"},
		{EntityID: "b", Name: "Central Perk", Type: "Location"},
		{EntityID: "doc", Name: "s01e01.txt", Type: "Document"},
	}
	s := scoreEpisode("s01e01", cumulative, objects, 5)
	if s.Found != 2 || s.Recall != 1.0 {
		t.Fatalf("recall = %v (%d/%d), want 1.0", s.Recall, s.Found, s.Expected)
	}
	if s.FoundExact != 2 || s.RecallExact != 1.0 {
		t.Fatalf("exact recall = %v (%d/%d), want 1.0", s.RecallExact, s.FoundExact, s.Expected)
	}
	if s.Objects != 2 || s.Unmatched != 0 || s.Precision != 1.0 {
		t.Fatalf("precision = %v (unmatched=%d objects=%d), want 1.0", s.Precision, s.Unmatched, s.Objects)
	}
	if s.DuplicateNames != 0 {
		t.Fatalf("duplicates = %d, want 0", s.DuplicateNames)
	}
	if s.Edges != 5 {
		t.Fatalf("edges = %d, want 5", s.Edges)
	}
}

func TestScoreEpisodeDuplicateAndHallucination(t *testing.T) {
	cumulative := []entity{{Name: "Monica Geller", Type: "Person"}}

	// "Monica Geller" extracted twice (two distinct entity ids) = duplicate;
	// "Zorg" matches nothing = hallucination.
	objects := []graphObject{
		{EntityID: "a", Name: "Monica Geller", Type: "Character"},
		{EntityID: "b", Name: "Monica", Type: "Character"},
		{EntityID: "c", Name: "Zorg", Type: "Character"},
	}
	s := scoreEpisode("s01e01", cumulative, objects, 0)
	if s.Found != 1 {
		t.Fatalf("found = %d, want 1", s.Found)
	}
	if s.FoundExact != 1 || s.RecallExact != 1.0 {
		t.Fatalf("found_exact = %d (recall %v), want 1/1.0", s.FoundExact, s.RecallExact)
	}
	if s.DuplicateNames != 1 {
		t.Fatalf("duplicates = %d, want 1 (Monica + Monica Geller both match)", s.DuplicateNames)
	}
	if s.Objects != 3 || s.Unmatched != 1 {
		t.Fatalf("objects=%d unmatched=%d, want 3/1", s.Objects, s.Unmatched)
	}
	if s.Precision < 0.66 || s.Precision > 0.67 {
		t.Fatalf("precision = %v, want ~0.667", s.Precision)
	}
}

func TestScoreEpisodeEmptyObjects(t *testing.T) {
	s := scoreEpisode("s01e01", []entity{{Name: "X"}}, nil, 0)
	if s.Recall != 0 || s.Found != 0 {
		t.Fatalf("recall = %v, want 0", s.Recall)
	}
	if s.Precision != 1.0 {
		t.Fatalf("precision = %v, want 1.0 (no objects, no false positives)", s.Precision)
	}
}

func TestIsDocumentType(t *testing.T) {
	for _, d := range []string{"Document", "document", "Meeting Document", "Episode", "Transcript"} {
		if !isDocumentType(d) {
			t.Fatalf("isDocumentType(%q) = false, want true", d)
		}
	}
	for _, e := range []string{"Person", "Character", "Location", "Organization"} {
		if isDocumentType(e) {
			t.Fatalf("isDocumentType(%q) = true, want false", e)
		}
	}
}
