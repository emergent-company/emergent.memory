package chunks

import (
	"errors"
	"strings"
	"testing"
)

func TestDiffLinesReplace(t *testing.T) {
	got, err := DiffLines("a\nb\nc", "a\nx\nc")
	if err != nil {
		t.Fatalf("DiffLines: %v", err)
	}
	want := []ChunkDiffLine{
		{Type: DiffEqual, Text: "a"},
		{Type: DiffRemove, Text: "b"},
		{Type: DiffAdd, Text: "x"},
		{Type: DiffEqual, Text: "c"},
	}
	if len(got) != len(want) {
		t.Fatalf("line count mismatch: got %v want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("line %d: got %+v want %+v", i, got[i], want[i])
		}
	}
}

func TestDiffLinesNoChange(t *testing.T) {
	got, err := DiffLines("same\ntext", "same\ntext")
	if err != nil {
		t.Fatalf("DiffLines: %v", err)
	}
	for _, l := range got {
		if l.Type != DiffEqual {
			t.Fatalf("expected all equal, got %+v", l)
		}
	}
	if len(got) != 2 {
		t.Fatalf("expected 2 equal lines, got %d", len(got))
	}
}

func TestDiffLinesAddAndRemove(t *testing.T) {
	got, err := DiffLines("keep", "keep\nadded")
	if err != nil {
		t.Fatalf("DiffLines: %v", err)
	}
	var adds, removes int
	for _, l := range got {
		switch l.Type {
		case DiffAdd:
			adds++
		case DiffRemove:
			removes++
		}
	}
	if adds != 1 || removes != 0 {
		t.Fatalf("expected 1 add / 0 remove, got adds=%d removes=%d (%v)", adds, removes, got)
	}
}

// TestDiffLinesRejectsOversizedInput guards the OOM fix: a diff whose LCS
// matrix would exceed maxDiffCells must be refused with ErrDiffTooLarge instead
// of allocating it. Two 100k-line inputs are chosen because the old code would
// have allocated 100001*100001 ints (~80 GiB) and been oom-killed.
func TestDiffLinesRejectsOversizedInput(t *testing.T) {
	line := strings.Repeat("x", 40)
	big := strings.Repeat(line+"\n", 100_000)

	_, err := DiffLines(big, big)
	if !errors.Is(err, ErrDiffTooLarge) {
		t.Fatalf("expected ErrDiffTooLarge for oversized input, got %v", err)
	}
}

// TestDiffLinesAcceptsInputAtBoundary confirms the cap is a bound, not a blanket
// refusal: a 1000x1000-line pair (1,002,001 cells) is under maxDiffCells and
// must diff successfully.
func TestDiffLinesAcceptsInputAtBoundary(t *testing.T) {
	var oldB, newB strings.Builder
	for i := 0; i < 1000; i++ {
		oldB.WriteString("line\n")
		if i == 500 {
			newB.WriteString("changed\n")
		} else {
			newB.WriteString("line\n")
		}
	}

	got, err := DiffLines(oldB.String(), newB.String())
	if err != nil {
		t.Fatalf("DiffLines under the cap should succeed: %v", err)
	}
	var adds, removes int
	for _, l := range got {
		switch l.Type {
		case DiffAdd:
			adds++
		case DiffRemove:
			removes++
		}
	}
	if adds != 1 || removes != 1 {
		t.Fatalf("expected one changed line (1 add/1 remove), got adds=%d removes=%d", adds, removes)
	}
}
