package toolresult

import (
	"strings"
	"testing"
)

func TestLines(t *testing.T) {
	tests := []struct {
		name      string
		text      string
		limit     int
		width     int
		wantText  string
		wantLines int
		wantChars int
	}{
		{
			name:      "fits",
			text:      "one\ntwo",
			limit:     5,
			width:     10,
			wantText:  "one\ntwo",
			wantLines: 0,
			wantChars: 0,
		},
		{
			name:      "too many lines",
			text:      "one\ntwo\nthree",
			limit:     2,
			width:     10,
			wantText:  "one\ntwo",
			wantLines: 1,
			wantChars: 0,
		},
		{
			name:      "line too wide marks the cut",
			text:      "abcdefg",
			limit:     5,
			width:     3,
			wantText:  "abc…",
			wantLines: 0,
			wantChars: 4,
		},
		{
			name:      "empty lines are kept",
			text:      "a\n\nb",
			limit:     5,
			width:     10,
			wantText:  "a\n\nb",
			wantLines: 0,
			wantChars: 0,
		},
		{
			name:      "counts characters not bytes",
			text:      "日本語です",
			limit:     5,
			width:     3,
			wantText:  "日本語…",
			wantLines: 0,
			wantChars: 2,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := Lines(tt.text, tt.limit, tt.width)
			if got.Text != tt.wantText {
				t.Errorf("text = %q, want %q", got.Text, tt.wantText)
			}
			if got.MoreLines != tt.wantLines {
				t.Errorf("moreLines = %d, want %d", got.MoreLines, tt.wantLines)
			}
			if got.MoreCharacters != tt.wantChars {
				t.Errorf("moreCharacters = %d, want %d", got.MoreCharacters, tt.wantChars)
			}
		})
	}
}

// TestLinesNeverLiesAboutACut: a bounded line always says it was cut, and the
// count is the number of characters dropped, not the number of lines.
func TestLinesNeverLiesAboutACut(t *testing.T) {
	got := Lines(strings.Repeat("x", 1000), 1, 10)
	if got.Text != strings.Repeat("x", 10)+"…" {
		t.Errorf("text = %q", got.Text)
	}
	if got.MoreCharacters != 990 {
		t.Errorf("moreCharacters = %d, want 990", got.MoreCharacters)
	}
	if got.MoreLines != 0 {
		t.Errorf("moreLines = %d, want 0", got.MoreLines)
	}
}
