package ftsquery

import "testing"

func TestRelax(t *testing.T) {
	tests := []struct {
		name   string
		query  string
		want   string
		wantOK bool
	}{
		{
			name:   "document identifier is dropped, text terms survive",
			query:  "aksjeloven lov 1997-06-13-44",
			want:   "aksjeloven lov",
			wantOK: true,
		},
		{
			name:   "composite key collapses to its text part",
			query:  "lov/1997-06-13-44",
			want:   "lov",
			wantOK: true,
		},
		{
			name:   "hyphenated alphabetic token loses its phrase",
			query:  "aksjeloven-lov",
			want:   "aksjeloven lov",
			wantOK: true,
		},
		{
			name:   "hyphenated word drops the numeric half",
			query:  "COVID-19",
			want:   "COVID",
			wantOK: true,
		},
		{
			name:   "hyphenated identifier is still relaxed",
			query:  "state-of-the-art",
			want:   "state of the art",
			wantOK: true,
		},
		{
			name:   "negation operator declines relaxation",
			query:  "foo -bar 2024",
			want:   "",
			wantOK: false,
		},
		{
			name:   "phrase syntax declines relaxation",
			query:  `"foo bar" 2024`,
			want:   "",
			wantOK: false,
		},
		{
			name:   "or operator declines relaxation",
			query:  "foo or bar",
			want:   "",
			wantOK: false,
		},
		{
			name:   "uppercase OR operator declines relaxation",
			query:  "foo OR bar",
			want:   "",
			wantOK: false,
		},
		{
			name:   "pipe operator declines relaxation",
			query:  "foo | bar",
			want:   "",
			wantOK: false,
		},
		{
			name:   "numbers interleaved between words",
			query:  "Kapittel 4 paragraf 2",
			want:   "Kapittel paragraf",
			wantOK: true,
		},
		{
			name:   "non-ascii letters are kept",
			query:  "forskrift ændring 1990",
			want:   "forskrift ændring",
			wantOK: true,
		},
		{
			name:   "nothing to relax when no numeric or hyphenated terms",
			query:  "aksjeloven lov",
			want:   "",
			wantOK: false,
		},
		{
			name:   "whitespace-only difference is not a relaxation",
			query:  "  aksjeloven   lov  ",
			want:   "",
			wantOK: false,
		},
		{
			name:   "numbers only leaves nothing to search for",
			query:  "1997-06-13-44",
			want:   "",
			wantOK: false,
		},
		{
			name:   "pure numeric run",
			query:  "123-456",
			want:   "",
			wantOK: false,
		},
		{
			name:   "empty query",
			query:  "",
			want:   "",
			wantOK: false,
		},
		{
			name:   "whitespace only",
			query:  "   ",
			want:   "",
			wantOK: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := Relax(tt.query)
			if ok != tt.wantOK {
				t.Fatalf("Relax(%q) ok = %v, want %v", tt.query, ok, tt.wantOK)
			}
			if got != tt.want {
				t.Fatalf("Relax(%q) = %q, want %q", tt.query, got, tt.want)
			}
		})
	}
}

func TestDisjoin(t *testing.T) {
	tests := []struct {
		name   string
		query  string
		want   string
		wantOK bool
	}{
		{
			name:   "natural multi-term query disjoins",
			query:  "aksjeloven innbetaling av aksjekapitalen innskudd penger andre formuesgoder",
			want:   "aksjeloven | innbetaling | av | aksjekapitalen | innskudd | penger | andre | formuesgoder",
			wantOK: true,
		},
		{
			name:   "two terms disjoin",
			query:  "innbetaling aksjekapital",
			want:   "innbetaling | aksjekapital",
			wantOK: true,
		},
		{
			name:   "numeric terms are dropped but letter terms survive",
			query:  "aksjeloven lov 1997-06-13-44",
			want:   "aksjeloven | lov",
			wantOK: true,
		},
		{
			name:   "hyphenated word splits into letter terms",
			query:  "aksjeloven-lov",
			want:   "aksjeloven | lov",
			wantOK: true,
		},
		{
			name:   "single term has nothing to disjoin",
			query:  "aksjeloven",
			want:   "",
			wantOK: false,
		},
		{
			name:   "single letter term plus numeric run has nothing to disjoin",
			query:  "aksjeloven 1997-06-13-44",
			want:   "",
			wantOK: false,
		},
		{
			name:   "negation operator declines disjoin",
			query:  "foo -bar",
			want:   "",
			wantOK: false,
		},
		{
			name:   "phrase syntax declines disjoin",
			query:  `"foo bar" baz`,
			want:   "",
			wantOK: false,
		},
		{
			name:   "or operator declines disjoin",
			query:  "foo or bar",
			want:   "",
			wantOK: false,
		},
		{
			name:   "uppercase OR operator declines disjoin",
			query:  "foo OR bar",
			want:   "",
			wantOK: false,
		},
		{
			name:   "pipe operator declines disjoin",
			query:  "foo | bar",
			want:   "",
			wantOK: false,
		},
		{
			name:   "numbers only leaves nothing to disjoin",
			query:  "1997 06 13 44",
			want:   "",
			wantOK: false,
		},
		{
			name:   "empty query",
			query:  "",
			want:   "",
			wantOK: false,
		},
		{
			name:   "whitespace only",
			query:  "   ",
			want:   "",
			wantOK: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := Disjoin(tt.query)
			if ok != tt.wantOK {
				t.Fatalf("Disjoin(%q) ok = %v, want %v", tt.query, ok, tt.wantOK)
			}
			if got != tt.want {
				t.Fatalf("Disjoin(%q) = %q, want %q", tt.query, got, tt.want)
			}
		})
	}
}
