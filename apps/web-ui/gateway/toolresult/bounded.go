package toolresult

import "strings"

// BoundedText is the first lines of a text, each shortened to a width, with the
// counts of what was left out. A shortened line ends in an ellipsis, so a cut
// never reads as a complete line.
type BoundedText struct {
	Text string
	// MoreLines counts the lines past the limit.
	MoreLines int
	// MoreCharacters counts the characters cut from the lines that were kept.
	MoreCharacters int
}

// Lines bounds text to limit lines of at most width characters. It never lies
// about the cut: both remainders are counted, and a shortened line is marked.
func Lines(text string, limit, width int) BoundedText {
	lines := strings.Split(text, "\n")

	kept := make([]string, 0, min(limit, len(lines)))
	cut := 0
	for i, line := range lines {
		if i >= limit {
			break
		}
		if runes := []rune(line); len(runes) > width {
			cut += len(runes) - width
			kept = append(kept, string(runes[:width])+"…")
			continue
		}
		kept = append(kept, line)
	}

	moreLines := len(lines) - limit
	if moreLines < 0 {
		moreLines = 0
	}
	return BoundedText{Text: strings.Join(kept, "\n"), MoreLines: moreLines, MoreCharacters: cut}
}
