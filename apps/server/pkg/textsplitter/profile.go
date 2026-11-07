package textsplitter

import "strings"

// Shape is the coarse layout class of a document, used to choose chunking
// parameters per document instead of forcing one strategy on every document in
// a project.
//
// A Shape describes how the text is laid out, not what it is about. Topic
// (legal, medical, HR) rarely changes the best splitting rule, while layout
// (headings, fenced code, table rows, speaker turns) does; so classification
// is deterministic and cheap: no model call, no embedding, reproducible.
type Shape string

const (
	// ShapeProse is running text with no dominant structure.
	ShapeProse Shape = "prose"
	// ShapeMarkdown is a document with heading structure.
	ShapeMarkdown Shape = "markdown"
	// ShapeCodeHeavy is a document whose bulk is fenced code.
	ShapeCodeHeavy Shape = "code_heavy"
	// ShapeTabular is a document dominated by pipe-table rows.
	ShapeTabular Shape = "tabular"
	// ShapeTranscript is a meeting/call transcript with speaker turns or
	// timestamped lines.
	ShapeTranscript Shape = "transcript"
	// ShapeEmail is an email thread with quoted header lines.
	ShapeEmail Shape = "email"
)

// SupportedShapes returns every recognised shape, in registry order.
func SupportedShapes() []Shape {
	return []Shape{
		ShapeProse,
		ShapeMarkdown,
		ShapeCodeHeavy,
		ShapeTabular,
		ShapeTranscript,
		ShapeEmail,
	}
}

// IsSupportedShape reports whether name is a registered shape.
func IsSupportedShape(name string) bool {
	for _, s := range SupportedShapes() {
		if string(s) == name {
			return true
		}
	}
	return false
}

// shapeScanLimit caps how much text classification inspects. The head of a
// document is representative of its layout and a single pass over 512 KiB is
// cheap even for very large documents.
const shapeScanLimit = 512 * 1024

// ShapeSignals carries the raw counts classification used, so a decision can
// be logged and explained.
type ShapeSignals struct {
	Lines             int     // non-empty lines scanned
	Chars             int     // characters in those lines
	Headings          int     // ATX markdown headings
	Fences            int     // fence markers (open + close)
	FencedChars       int     // characters inside fences
	TableRows         int     // lines shaped like pipe-table rows
	SpeakerLines      int     // lines shaped like "Name: text" turns
	SpeakerNames      int     // distinct speaker names seen
	RecurringSpeakers int     // speaker names that appear at least twice
	TimestampLines    int     // lines starting with a clock timestamp
	EmailHeaders      int     // quoted email header lines (From:, To:, ...)
	EmailKinds        int     // distinct email header kinds seen
	TableRatio        float64 // TableRows / Lines
	SpeakerRatio      float64 // SpeakerLines / Lines
	FencedRatio       float64 // FencedChars / Chars
	EmailRatio        float64 // EmailHeaders / Lines
}

// DetectShape classifies text by layout. It is deterministic and allocation
// light: one pass over at most shapeScanLimit bytes.
func DetectShape(text string) (Shape, ShapeSignals) {
	var sig ShapeSignals
	if strings.TrimSpace(text) == "" {
		return ShapeProse, sig
	}
	if len(text) > shapeScanLimit {
		text = text[:shapeScanLimit]
	}

	// Speaker names are counted so a conversation (the same names recurring)
	// is not confused with a list of one-off labels ("Step 1:", "Note:").
	// Capped so a pathological document cannot grow the map without bound.
	const speakerNameCap = 256
	speakerCounts := make(map[string]int, 8)

	inFence := false
	emailKinds := 0
	for _, raw := range strings.Split(text, "\n") {
		line := strings.TrimSpace(raw)
		if line == "" {
			continue
		}
		if isFence(line) {
			sig.Fences++
			inFence = !inFence
			continue
		}
		if inFence {
			sig.FencedChars += len(line) + 1
			continue
		}

		sig.Lines++
		sig.Chars += len(line)
		if isATXHeading(line) {
			sig.Headings++
		}
		if isPipeTableRow(line) {
			sig.TableRows++
		}
		if name := speakerName(line); name != "" {
			sig.SpeakerLines++
			if len(speakerCounts) < speakerNameCap || speakerCounts[name] > 0 {
				speakerCounts[name]++
			}
		}
		if isTimestampLine(line) {
			sig.TimestampLines++
		}
		if bit := emailHeaderKind(line); bit != 0 {
			sig.EmailHeaders++
			if emailKinds&bit == 0 {
				emailKinds |= bit
				sig.EmailKinds++
			}
		}
	}

	sig.SpeakerNames = len(speakerCounts)
	for _, n := range speakerCounts {
		if n >= 2 {
			sig.RecurringSpeakers++
		}
	}
	if sig.Lines > 0 {
		sig.TableRatio = float64(sig.TableRows) / float64(sig.Lines)
		sig.SpeakerRatio = float64(sig.SpeakerLines) / float64(sig.Lines)
		sig.EmailRatio = float64(sig.EmailHeaders) / float64(sig.Lines)
	}
	if total := sig.Chars + sig.FencedChars; total > 0 {
		sig.FencedRatio = float64(sig.FencedChars) / float64(total)
	}

	switch {
	// A thread is more than two header lines: it needs a meaningful share of
	// the document, or enough headers that the body must be long (a 30-message
	// thread has three headers per message but hundreds of body lines).
	// Without this gate a markdown page quoting one header snippet is read as
	// email and loses the structure-aware strategy.
	case sig.EmailKinds >= 2 && sig.EmailHeaders >= 3 &&
		(sig.EmailRatio >= 0.1 || sig.EmailHeaders >= 8):
		return ShapeEmail, sig
	// A table has to dominate the document; markdown docs commonly carry one
	// or two tables without being tabular.
	case sig.TableRows >= 5 && sig.TableRatio >= 0.5:
		return ShapeTabular, sig
	// Timestamped lines are decisive. Bare "Name: text" lines need a quarter
	// of the document AND recurring names: a conversation repeats its
	// speakers, a list of labels does not.
	//
	// A single repeating label is not enough on its own (#271): "Objective:"
	// eight times is a plan, and a glossary of "Term:" lines is a glossary, but
	// both look exactly like a one-speaker monologue from this signal alone. So
	// the long form needs a second distinct name somewhere in the document —
	// with only one label in play we cannot tell a transcript from a labelled
	// list, and prose is the safe reading (it keeps the deployment defaults and
	// cannot mis-split a conversation).
	case sig.TimestampLines >= 5 ||
		(sig.SpeakerLines >= 5 && sig.SpeakerRatio >= 0.25 && sig.RecurringSpeakers >= 2) ||
		(sig.SpeakerLines >= 8 && sig.SpeakerRatio >= 0.25 && sig.RecurringSpeakers >= 1 && sig.SpeakerNames >= 2):
		return ShapeTranscript, sig
	// Three or more fenced blocks, or a document that is one fence plus
	// almost nothing else. The bare ratio is not enough on its own: a stray
	// unclosed fence would make the rest of the document look fenced.
	case (sig.Fences >= 6 && sig.FencedRatio >= 0.3) || (sig.Fences >= 2 && sig.FencedRatio >= 0.6):
		return ShapeCodeHeavy, sig
	case sig.Headings >= 2:
		return ShapeMarkdown, sig
	}
	return ShapeProse, sig
}

// Profile is the chunking setting chosen for a shape.
type Profile struct {
	Shape   Shape
	Config  Config
	Options Options
}

// ProfileFor returns the chunking profile for a shape. Unknown shapes fall
// back to the prose profile.
func ProfileFor(shape Shape) Profile {
	switch shape {
	case ShapeMarkdown:
		// Headings bound chunks, so overlap buys nothing (and the structure
		// packer does not implement it). 1200 chars keeps most sections whole.
		return Profile{
			Shape:   ShapeMarkdown,
			Config:  Config{ChunkSize: 1200, ChunkOverlap: 0},
			Options: Options{Strategy: StrategyMarkdownStructure},
		}
	case ShapeCodeHeavy:
		// A larger budget keeps a snippet together with the prose that
		// introduces it; fences stay atomic either way.
		return Profile{
			Shape:   ShapeCodeHeavy,
			Config:  Config{ChunkSize: 2000, ChunkOverlap: 0},
			Options: Options{Strategy: StrategyMarkdownStructure},
		}
	case ShapeTabular:
		// Line separators keep row groups coherent; a small overlap keeps a
		// header row adjacent to the rows it describes.
		return Profile{
			Shape:   ShapeTabular,
			Config:  Config{ChunkSize: 1200, ChunkOverlap: 120},
			Options: Options{Strategy: StrategyRecursiveCharacter},
		}
	case ShapeTranscript:
		// An exchange runs longer than a paragraph; the overlap keeps an
		// answer attached to the question that prompted it.
		return Profile{
			Shape:   ShapeTranscript,
			Config:  Config{ChunkSize: 1600, ChunkOverlap: 160},
			Options: Options{Strategy: StrategyRecursiveCharacter},
		}
	case ShapeEmail:
		// One message per chunk where possible.
		return Profile{
			Shape:   ShapeEmail,
			Config:  Config{ChunkSize: 1200, ChunkOverlap: 120},
			Options: Options{Strategy: StrategyRecursiveCharacter},
		}
	default:
		// Prose keeps the historical default, so enabling auto-profile is a
		// no-op for the documents that have no structure to exploit.
		return Profile{
			Shape:   ShapeProse,
			Config:  DefaultConfig(),
			Options: Options{Strategy: StrategyRecursiveCharacter},
		}
	}
}

// DefaultProfiles returns the profile for every supported shape in registry
// order, for API discovery.
func DefaultProfiles() []Profile {
	out := make([]Profile, 0, len(SupportedShapes()))
	for _, s := range SupportedShapes() {
		out = append(out, ProfileFor(s))
	}
	return out
}

// --- line shape helpers (all allocation-free enough for one pass) ---

func isPipeTableRow(line string) bool {
	if strings.Count(line, "|") < 2 {
		return false
	}
	return strings.HasPrefix(line, "|") || strings.HasSuffix(line, "|")
}

// speakerName returns the speaker label of a transcript turn ("Alice",
// "Speaker 1", "Dr Smith"), or "" when the line is not a turn.
//
// Labels that are structural rather than people — "Note:", "Step 1:",
// "Chapter 2:" — are rejected: they are how documents label sections, and
// treating them as turns is what makes a notes page look like a transcript.
func speakerName(line string) string {
	i := strings.IndexByte(line, ':')
	if i < 2 || i > 40 {
		return ""
	}
	name := line[:i]
	if strings.ContainsAny(name, ".,;!?()[]{}<>\"'/\\&*|") {
		return ""
	}
	words := strings.Fields(name)
	if len(words) == 0 || len(words) > 3 {
		return ""
	}
	c := name[0]
	upper := c >= 'A' && c <= 'Z'
	digit := c >= '0' && c <= '9'
	if !upper && !digit {
		return ""
	}
	// Every further word must be capitalised or numeric ("Speaker 1",
	// "Dr Smith"). A lowercase continuation means a prose lead-in such as
	// "The following: ...", not a speaker name.
	for _, w := range words[1:] {
		c := w[0]
		if (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') {
			continue
		}
		return ""
	}
	if isLabelPrefix(strings.ToLower(words[0])) {
		return ""
	}
	return name
}

// labelPrefixes are words that introduce a labelled paragraph or list item
// rather than a speaker. Matching is on the first word, so "Step 1" and
// "Chapter 3" are caught by their head word.
var labelPrefixes = map[string]struct{}{
	"action": {}, "agenda": {}, "answer": {}, "appendix": {}, "assumption": {},
	"background": {}, "caution": {}, "caveat": {}, "chapter": {}, "conclusion": {},
	"danger": {}, "definition": {}, "description": {}, "detail": {}, "details": {},
	"example": {}, "exercise": {}, "fig": {}, "figure": {}, "fixme": {}, "goal": {},
	"important": {}, "input": {}, "intro": {}, "introduction": {}, "note": {},
	"notes": {}, "output": {}, "part": {}, "question": {}, "requirement": {},
	"result": {}, "results": {}, "section": {}, "step": {}, "steps": {},
	"summary": {}, "table": {}, "task": {}, "tip": {}, "tips": {}, "todo": {},
	"warning": {}, "warnings": {},
}

func isLabelPrefix(word string) bool {
	_, ok := labelPrefixes[word]
	return ok
}

// isTimestampLine reports whether line begins with a clock timestamp such as
// "00:12:34", "12:34", "[00:01:02]" or "0:12:34 -".
func isTimestampLine(line string) bool {
	s := line
	if strings.HasPrefix(s, "[") {
		s = s[1:]
	} else if strings.HasPrefix(s, "(") {
		s = s[1:]
	}
	i := 0
	for i < len(s) && s[i] >= '0' && s[i] <= '9' {
		i++
	}
	if i == 0 || i > 2 || i >= len(s) || s[i] != ':' {
		return false
	}
	i++
	start := i
	for i < len(s) && s[i] >= '0' && s[i] <= '9' {
		i++
	}
	if i-start != 2 {
		return false
	}
	if i < len(s) && s[i] == ':' {
		i++
		start = i
		for i < len(s) && s[i] >= '0' && s[i] <= '9' {
			i++
		}
		if i-start != 2 {
			return false
		}
	}
	if i >= len(s) {
		return true
	}
	switch s[i] {
	case ' ', ']', ')', '-', '\t':
		return true
	}
	return false
}

const (
	emailFrom = 1 << iota
	emailTo
	emailSubject
	emailDate
	emailCC
)

// emailHeaderKind returns the bit for a quoted email header line, or 0.
func emailHeaderKind(line string) int {
	lower := strings.ToLower(line)
	switch {
	case strings.HasPrefix(lower, "from:"):
		return emailFrom
	case strings.HasPrefix(lower, "to:"):
		return emailTo
	case strings.HasPrefix(lower, "subject:"):
		return emailSubject
	case strings.HasPrefix(lower, "date:"), strings.HasPrefix(lower, "sent:"):
		return emailDate
	case strings.HasPrefix(lower, "cc:"):
		return emailCC
	}
	return 0
}
