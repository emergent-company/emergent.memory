package textsplitter

import (
	"fmt"
	"strings"
)

// Strategy identifies a chunking strategy.
type Strategy string

const (
	// StrategyRecursiveCharacter is the original recursive character splitter.
	StrategyRecursiveCharacter Strategy = "recursive_character"
	// StrategyMarkdownStructure splits on markdown structure (headings,
	// paragraphs) while protecting code, math, tables, and image/link blocks.
	StrategyMarkdownStructure Strategy = "markdown_structure"
)

// SupportedStrategies returns the strategies the chunker understands, in
// registry order.
func SupportedStrategies() []Strategy {
	return []Strategy{StrategyRecursiveCharacter, StrategyMarkdownStructure}
}

// IsSupportedStrategy reports whether name is a registered strategy.
func IsSupportedStrategy(name string) bool {
	for _, s := range SupportedStrategies() {
		if string(s) == name {
			return true
		}
	}
	return false
}

// NormalizeStrategy maps an empty strategy to the default.
func NormalizeStrategy(name string) Strategy {
	if name == "" {
		return StrategyRecursiveCharacter
	}
	return Strategy(name)
}

// Chunk is a produced chunk plus the metadata the structure-aware strategies
// record (oversized atomic segments, header carry, parent-child links).
type Chunk struct {
	Text        string
	Index       int
	ParentIndex int // -1 when none
	IsParent    bool
	Oversized   bool
	HeaderCarry string // "" normal, "unknown" when a table header could not be determined
}

// Options controls chunking beyond size/overlap.
type Options struct {
	Strategy    Strategy
	ParentChild bool
}

// SplitDocument chunks text using the configured strategy. It is the
// behavior-preserving entry point for the strategy registry: the default
// (empty) strategy is StrategyRecursiveCharacter and produces exactly what
// Split produces.
func SplitDocument(text string, cfg Config, opts Options) ([]Chunk, error) {
	strategy := NormalizeStrategy(string(opts.Strategy))
	if !IsSupportedStrategy(string(strategy)) {
		return nil, fmt.Errorf("unsupported chunking strategy %q", strategy)
	}
	cfg = normalizeConfig(cfg)

	switch strategy {
	case StrategyRecursiveCharacter:
		if opts.ParentChild {
			return nil, fmt.Errorf("parent-child chunking requires strategy %q", StrategyMarkdownStructure)
		}
		parts := Split(text, cfg)
		out := make([]Chunk, 0, len(parts))
		for i, p := range parts {
			out = append(out, Chunk{Text: p, Index: i, ParentIndex: -1})
		}
		return out, nil
	case StrategyMarkdownStructure:
		return splitMarkdown(text, cfg, opts.ParentChild)
	default:
		return nil, fmt.Errorf("unsupported chunking strategy %q", strategy)
	}
}

func normalizeConfig(cfg Config) Config {
	def := DefaultConfig()
	if cfg.ChunkSize <= 0 {
		cfg.ChunkSize = def.ChunkSize
	}
	if cfg.ChunkOverlap < 0 {
		cfg.ChunkOverlap = 0
	}
	if cfg.ChunkOverlap >= cfg.ChunkSize {
		cfg.ChunkOverlap = cfg.ChunkSize / 5
	}
	return cfg
}

type blockKind int

const (
	blockParagraph blockKind = iota
	blockHeading
	blockCode
	blockMath
	blockTable
)

type block struct {
	kind   blockKind
	text   string
	lines  []string
	header []string
}

// parseBlocks splits markdown into atomic structural blocks.
func parseBlocks(text string) []block {
	lines := strings.Split(text, "\n")
	var blocks []block
	i := 0
	for i < len(lines) {
		trimmed := strings.TrimSpace(lines[i])

		if isFence(trimmed) {
			fence := fenceMarker(trimmed)
			j := i + 1
			for j < len(lines) {
				if strings.HasPrefix(strings.TrimSpace(lines[j]), fence) {
					j++
					break
				}
				j++
			}
			seg := strings.Join(lines[i:j], "\n")
			blocks = append(blocks, block{kind: blockCode, text: seg, lines: lines[i:j]})
			i = j
			continue
		}

		if isMathFence(trimmed) {
			j := i + 1
			if strings.Count(trimmed, "$$") >= 2 {
				j = i + 1
			} else {
				for j < len(lines) {
					if isMathFence(strings.TrimSpace(lines[j])) {
						j++
						break
					}
					j++
				}
			}
			seg := strings.Join(lines[i:j], "\n")
			blocks = append(blocks, block{kind: blockMath, text: seg, lines: lines[i:j]})
			i = j
			continue
		}

		if strings.HasPrefix(trimmed, "|") {
			j := i
			for j < len(lines) && strings.HasPrefix(strings.TrimSpace(lines[j]), "|") {
				j++
			}
			rows := append([]string(nil), lines[i:j]...)
			var header []string
			if len(rows) >= 2 && isTableSeparator(rows[1]) {
				header = []string{rows[0], rows[1]}
			}
			blocks = append(blocks, block{
				kind:   blockTable,
				text:   strings.Join(rows, "\n"),
				lines:  rows,
				header: header,
			})
			i = j
			continue
		}

		if isATXHeading(trimmed) {
			blocks = append(blocks, block{kind: blockHeading, text: trimmed, lines: []string{lines[i]}})
			i++
			continue
		}

		// Paragraph: consume until a blank line or the start of another special block.
		j := i
		var para []string
		for j < len(lines) {
			t := strings.TrimSpace(lines[j])
			if t == "" {
				break
			}
			if isFence(t) || isMathFence(t) || strings.HasPrefix(t, "|") || isATXHeading(t) {
				break
			}
			para = append(para, lines[j])
			j++
		}
		if len(para) == 0 {
			i++
			continue
		}
		blocks = append(blocks, block{kind: blockParagraph, text: strings.TrimSpace(strings.Join(para, "\n")), lines: para})
		i = j
	}
	return blocks
}

func isFence(trimmed string) bool {
	return strings.HasPrefix(trimmed, "```") || strings.HasPrefix(trimmed, "~~~")
}

func fenceMarker(trimmed string) string {
	if strings.HasPrefix(trimmed, "```") {
		return "```"
	}
	return "~~~"
}

func isMathFence(trimmed string) bool {
	return strings.HasPrefix(trimmed, "$$")
}

func isATXHeading(trimmed string) bool {
	if !strings.HasPrefix(trimmed, "#") {
		return false
	}
	n := 0
	for n < len(trimmed) && trimmed[n] == '#' {
		n++
	}
	if n < 1 || n > 6 {
		return false
	}
	return len(trimmed) > n && trimmed[n] == ' '
}

func isTableSeparator(line string) bool {
	t := strings.TrimSpace(line)
	if t == "" {
		return false
	}
	hasDash := false
	for _, r := range t {
		switch r {
		case '|', ':', '-', ' ':
			if r == '-' {
				hasDash = true
			}
		default:
			return false
		}
	}
	return hasDash
}

func splitMarkdown(text string, cfg Config, parentChild bool) ([]Chunk, error) {
	if strings.TrimSpace(text) == "" {
		return nil, nil
	}
	blocks := parseBlocks(text)
	if len(blocks) == 0 {
		return nil, nil
	}

	if parentChild {
		return packSections(blocks, cfg), nil
	}
	return packBlocks(blocks, cfg), nil
}

// packSections groups blocks under their heading and emits a parent chunk per
// section followed by its child chunks. Parent chunks are marked IsParent and
// reference no parent; children reference the parent's index.
type section struct {
	heading string
	blocks  []block
}

func splitSections(blocks []block) []section {
	var sections []section
	cur := section{}
	started := false
	for _, b := range blocks {
		if b.kind == blockHeading {
			if started {
				sections = append(sections, cur)
			}
			cur = section{heading: b.text}
			started = true
			continue
		}
		cur.blocks = append(cur.blocks, b)
		started = true
	}
	if started {
		sections = append(sections, cur)
	}
	return sections
}

// maxParentTextChars bounds the structural parent chunk text. A parent chunk
// carries the entire section (heading plus all its blocks), which can grow
// unbounded; truncating it keeps embedding/context sizes sane.
const maxParentTextChars = 32000

func packSections(blocks []block, cfg Config) []Chunk {
	var out []Chunk
	for _, sec := range splitSections(blocks) {
		parentText := sectionText(sec)
		if strings.TrimSpace(parentText) == "" {
			continue
		}
		parentText = truncateToMaxRunes(parentText, maxParentTextChars)
		parent := Chunk{Text: parentText, Index: len(out), ParentIndex: -1, IsParent: true}
		out = append(out, parent)
		parentIdx := parent.Index

		childBlocks := sec.blocks
		children := packBlocks(childBlocks, cfg)
		for _, c := range children {
			c.Index = len(out)
			c.ParentIndex = parentIdx
			out = append(out, c)
		}
	}
	return out
}

func sectionText(sec section) string {
	var b strings.Builder
	if sec.heading != "" {
		b.WriteString(sec.heading)
	}
	for _, blk := range sec.blocks {
		if b.Len() > 0 {
			b.WriteString("\n\n")
		}
		b.WriteString(blk.text)
	}
	return strings.TrimSpace(b.String())
}

// truncateToMaxRunes shortens s to at most max runes without adding a marker.
// It returns s unchanged when it already fits.
func truncateToMaxRunes(s string, max int) string {
	if max <= 0 {
		return ""
	}
	runes := []rune(s)
	if len(runes) <= max {
		return s
	}
	return string(runes[:max])
}

// packBlocks greedily packs blocks into chunks. Atomic blocks that exceed the
// chunk size are emitted as their own oversized chunk; oversized tables split
// at row boundaries with the header carried into each part.
func packBlocks(blocks []block, cfg Config) []Chunk {
	var out []Chunk
	var cur strings.Builder

	flush := func() {
		t := strings.TrimSpace(cur.String())
		if t != "" {
			out = append(out, Chunk{Text: t, Index: len(out), ParentIndex: -1})
		}
		cur.Reset()
	}
	add := func(seg string) {
		if cur.Len() > 0 {
			cur.WriteString("\n\n")
		}
		cur.WriteString(seg)
	}

	for _, b := range blocks {
		switch b.kind {
		case blockCode, blockMath:
			if len(b.text) > cfg.ChunkSize {
				flush()
				out = append(out, Chunk{Text: b.text, Index: len(out), ParentIndex: -1, Oversized: true})
				continue
			}
			if cur.Len() > 0 && cur.Len()+len(b.text)+2 > cfg.ChunkSize {
				flush()
			}
			add(b.text)

		case blockTable:
			if len(b.text) <= cfg.ChunkSize {
				if cur.Len() > 0 && cur.Len()+len(b.text)+2 > cfg.ChunkSize {
					flush()
				}
				add(b.text)
				continue
			}
			flush()
			for _, part := range splitTable(b, cfg) {
				part.Index = len(out)
				out = append(out, part)
			}

		default: // paragraph, heading
			seg := b.text
			if cur.Len() > 0 && cur.Len()+len(seg)+2 > cfg.ChunkSize {
				flush()
			}
			if len(seg) > cfg.ChunkSize {
				for _, sub := range Split(seg, cfg) {
					flush()
					out = append(out, Chunk{Text: sub, Index: len(out), ParentIndex: -1})
				}
				continue
			}
			add(seg)
		}
	}
	flush()
	return out
}

// splitTable splits an oversized table at row boundaries, carrying the header
// row (and separator) into every part.
func splitTable(b block, cfg Config) []Chunk {
	headerCarry := ""
	var header []string
	body := b.lines
	if len(b.header) >= 2 {
		header = b.header
		body = b.lines[2:]
	} else {
		headerCarry = "unknown"
	}
	if len(body) == 0 {
		return []Chunk{{Text: b.text, Index: 0, ParentIndex: -1, Oversized: true, HeaderCarry: headerCarry}}
	}

	headerLen := 0
	for _, h := range header {
		headerLen += len(h) + 1
	}

	var out []Chunk
	var buf []string
	bufLen := headerLen
	emit := func() {
		rows := append(append([]string(nil), header...), buf...)
		out = append(out, Chunk{
			Text:        strings.Join(rows, "\n"),
			ParentIndex: -1,
			HeaderCarry: headerCarry,
		})
		buf = nil
		bufLen = headerLen
	}
	for _, row := range body {
		if len(buf) > 0 && bufLen+len(row)+1 > cfg.ChunkSize {
			emit()
		}
		buf = append(buf, row)
		bufLen += len(row) + 1
	}
	if len(buf) > 0 {
		emit()
	}
	return out
}
