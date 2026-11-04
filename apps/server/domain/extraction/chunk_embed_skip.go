package extraction

// maxEmbedChars bounds the chunk text length sent to the embedding model. An
// atomic protected segment (for example a fenced code block) that exceeds the
// chunk size but also this bound cannot be embedded without exceeding the
// model context; it is flagged by the chunker and skipped here rather than
// triggering a permanent embedding failure.
const maxEmbedChars = 32000

// skipEmbeddingReason returns a non-empty reason when a chunk must not be
// embedded. Parent chunks are structural context only; oversized atomic
// segments beyond the embedding context are skipped rather than failed.
func skipEmbeddingReason(isParent, oversized bool, textLen int) string {
	if isParent {
		return "parent_chunk"
	}
	if oversized && textLen > maxEmbedChars {
		return "oversized_over_context"
	}
	return ""
}
