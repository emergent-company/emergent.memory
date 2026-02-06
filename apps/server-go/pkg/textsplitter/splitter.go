package textsplitter

import (
	"strings"
	"unicode"
)

type Config struct {
	ChunkSize    int
	ChunkOverlap int
}

func DefaultConfig() Config {
	return Config{
		ChunkSize:    1000,
		ChunkOverlap: 200,
	}
}

func Split(text string, cfg Config) []string {
	if len(text) == 0 {
		return nil
	}

	if cfg.ChunkSize <= 0 {
		cfg.ChunkSize = 1000
	}
	if cfg.ChunkOverlap < 0 {
		cfg.ChunkOverlap = 0
	}
	if cfg.ChunkOverlap >= cfg.ChunkSize {
		cfg.ChunkOverlap = cfg.ChunkSize / 5
	}

	separators := []string{"\n\n", "\n", ". ", " "}
	return splitRecursive(text, separators, cfg)
}

func splitRecursive(text string, separators []string, cfg Config) []string {
	if len(text) <= cfg.ChunkSize {
		trimmed := strings.TrimSpace(text)
		if len(trimmed) == 0 {
			return nil
		}
		return []string{trimmed}
	}

	if len(separators) == 0 {
		return splitBySize(text, cfg)
	}

	sep := separators[0]
	remaining := separators[1:]

	parts := strings.Split(text, sep)
	if len(parts) == 1 {
		return splitRecursive(text, remaining, cfg)
	}

	var chunks []string
	var current strings.Builder

	for i, part := range parts {
		partWithSep := part
		if i < len(parts)-1 && sep != " " {
			partWithSep = part + sep
		}

		if current.Len()+len(partWithSep) > cfg.ChunkSize && current.Len() > 0 {
			chunk := strings.TrimSpace(current.String())
			if len(chunk) > 0 {
				chunks = append(chunks, chunk)
			}

			overlap := getOverlap(current.String(), cfg.ChunkOverlap)
			current.Reset()
			current.WriteString(overlap)
		}

		current.WriteString(partWithSep)
		if sep == " " && i < len(parts)-1 {
			current.WriteString(" ")
		}
	}

	if current.Len() > 0 {
		chunk := strings.TrimSpace(current.String())
		if len(chunk) > 0 {
			chunks = append(chunks, chunk)
		}
	}

	var result []string
	for _, chunk := range chunks {
		if len(chunk) > cfg.ChunkSize {
			result = append(result, splitRecursive(chunk, remaining, cfg)...)
		} else {
			result = append(result, chunk)
		}
	}

	return result
}

func splitBySize(text string, cfg Config) []string {
	var chunks []string
	runes := []rune(text)
	start := 0

	for start < len(runes) {
		end := start + cfg.ChunkSize
		if end > len(runes) {
			end = len(runes)
		}

		for end > start && end < len(runes) && !unicode.IsSpace(runes[end]) {
			end--
		}
		if end == start {
			end = start + cfg.ChunkSize
			if end > len(runes) {
				end = len(runes)
			}
		}

		chunk := strings.TrimSpace(string(runes[start:end]))
		if len(chunk) > 0 {
			chunks = append(chunks, chunk)
		}

		start = end - cfg.ChunkOverlap
		if start < 0 {
			start = 0
		}
		for start < len(runes) && unicode.IsSpace(runes[start]) {
			start++
		}
		if start <= end-cfg.ChunkOverlap {
			start = end
		}
	}

	return chunks
}

func getOverlap(text string, size int) string {
	if size <= 0 || len(text) == 0 {
		return ""
	}

	runes := []rune(text)
	if len(runes) <= size {
		return text
	}

	start := len(runes) - size
	for start < len(runes) && !unicode.IsSpace(runes[start]) {
		start++
	}
	for start < len(runes) && unicode.IsSpace(runes[start]) {
		start++
	}

	if start >= len(runes) {
		return ""
	}

	return string(runes[start:])
}
