package a2ui

import "strings"

// TextFilter removes ```a2ui fenced blocks from a stream of text chunks.
//
// A chat turn's answer is streamed token by token, while a surface is only
// extracted from the finished response (ExtractFromText). Without a filter the
// raw JSONL of a surface — and of a surface that later fails validation — is
// painted into the answer bubble before anyone knows it is a card. Feed every
// chunk through Feed and emit only what it returns.
//
// The filter is line-based, mirroring the extractor: an opening line must be
// exactly ```a2ui and a closing line exactly ``` (surrounding whitespace
// ignored). It holds back the tail it cannot classify yet:
//
//   - a partial line that could still become an opening fence,
//   - a line already started (so it can no longer be a fence), emitted as it
//     arrives,
//   - everything between an opening and its closing fence.
//
// Text held at the end of a stream is dropped by Flush when it belongs to an
// unterminated fence — the same block ExtractFromText strips from the finished
// response, which is re-sent as the authoritative final text.
type TextFilter struct {
	pending   string
	inFence   bool
	lineShown bool // part of the current line was already returned
}

// NewTextFilter returns a filter with no pending text.
func NewTextFilter() *TextFilter { return &TextFilter{} }

// Feed consumes the next chunk and returns the text that is safe to show. It
// returns "" while the chunk only extends a possible fence.
func (f *TextFilter) Feed(chunk string) string {
	f.pending += chunk
	var out strings.Builder
	for {
		idx := strings.IndexByte(f.pending, '\n')
		if idx < 0 {
			f.emitTail(&out)
			return out.String()
		}
		line := f.pending[:idx]
		f.pending = f.pending[idx+1:]

		if f.inFence {
			// Inside a surface: drop until the closing fence.
			if strings.TrimSpace(line) == fenceClose {
				f.inFence = false
				f.lineShown = false
			}
			continue
		}
		if f.lineShown {
			// Continuation of a line that already reached the caller.
			out.WriteString(line)
			out.WriteByte('\n')
			f.lineShown = false
			continue
		}
		if strings.TrimSpace(line) == fenceOpen {
			f.inFence = true
			continue
		}
		out.WriteString(line)
		out.WriteByte('\n')
	}
}

// emitTail handles a trailing partial line (no newline yet): emit it unless it
// is still a candidate for the opening fence or falls inside a surface.
func (f *TextFilter) emitTail(out *strings.Builder) {
	if f.inFence {
		return // hold everything: the surface has not closed yet
	}
	if f.lineShown {
		out.WriteString(f.pending)
		f.pending = ""
		return
	}
	if isFencePrefix(strings.TrimSpace(f.pending)) {
		return // hold: this may still become ```a2ui
	}
	out.WriteString(f.pending)
	f.pending = ""
	f.lineShown = true
}

// Flush returns the tail Feed is still holding, once the response is complete.
// Text inside an unterminated fence is dropped (it is a surface, not an
// answer), and so is a bare opening marker that never received content. Any
// other tail is ordinary text — e.g. the closing ``` of a markdown block that
// ended the message without a trailing newline — and is returned.
func (f *TextFilter) Flush() string {
	pending := f.pending
	f.pending = ""
	f.lineShown = false
	if f.inFence {
		f.inFence = false
		return ""
	}
	if strings.TrimSpace(pending) == fenceOpen {
		return ""
	}
	return pending
}

// isFencePrefix reports whether s is a prefix of the opening fence, or the
// fence itself, so a chunk boundary can split the marker.
func isFencePrefix(s string) bool {
	return len(s) <= len(fenceOpen) && strings.HasPrefix(fenceOpen, s)
}
