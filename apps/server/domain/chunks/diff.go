package chunks

import (
	"errors"
	"fmt"
	"strings"
)

// ErrDiffTooLarge is returned by DiffLines when the two inputs are too large to
// diff within the bounded memory budget.
var ErrDiffTooLarge = errors.New("chunks: diff input too large")

// maxDiffCells bounds the LCS dynamic-programming matrix as a number of cells.
// The matrix holds one int (8 bytes) per cell, so 4,000,000 cells caps the
// allocation at ~32 MiB. Two ~100k-line inputs would need a 100001x100001
// matrix (~80 GiB) and oom-kill the server; such inputs are refused with
// ErrDiffTooLarge instead of being allocated. 4,000,000 cells still covers a
// 2000x2000-line diff, far beyond a chunk-sized revision pair.
const maxDiffCells = 4_000_000

// DiffLines computes a line-level diff between two texts using a longest
// common subsequence, returning ordered equal/add/remove lines. It is
// dependency-free and deterministic, suitable for chunk-sized inputs.
//
// The LCS matrix is O(n*m) in memory, so oversized inputs are refused with
// ErrDiffTooLarge rather than allocated. Callers should surface that as a bad
// request (the inputs are too large to diff).
func DiffLines(oldText, newText string) ([]ChunkDiffLine, error) {
	a := splitDiffLines(oldText)
	b := splitDiffLines(newText)
	n, m := len(a), len(b)

	// Bound the LCS matrix before allocating it. int64 multiplication avoids
	// overflow for large line counts; n,m are capped by addressable memory so
	// the product cannot exceed int64.
	if int64(n+1)*int64(m+1) > maxDiffCells {
		return nil, fmt.Errorf("%w: %d lines vs %d lines", ErrDiffTooLarge, n, m)
	}

	// dp[i][j] = LCS length of a[i:], b[j:]
	dp := make([][]int, n+1)
	for i := range dp {
		dp[i] = make([]int, m+1)
	}
	for i := n - 1; i >= 0; i-- {
		for j := m - 1; j >= 0; j-- {
			switch {
			case a[i] == b[j]:
				dp[i][j] = dp[i+1][j+1] + 1
			case dp[i+1][j] >= dp[i][j+1]:
				dp[i][j] = dp[i+1][j]
			default:
				dp[i][j] = dp[i][j+1]
			}
		}
	}

	out := make([]ChunkDiffLine, 0, n+m)
	i, j := 0, 0
	for i < n && j < m {
		switch {
		case a[i] == b[j]:
			out = append(out, ChunkDiffLine{Type: DiffEqual, Text: a[i]})
			i++
			j++
		case dp[i+1][j] >= dp[i][j+1]:
			out = append(out, ChunkDiffLine{Type: DiffRemove, Text: a[i]})
			i++
		default:
			out = append(out, ChunkDiffLine{Type: DiffAdd, Text: b[j]})
			j++
		}
	}
	for ; i < n; i++ {
		out = append(out, ChunkDiffLine{Type: DiffRemove, Text: a[i]})
	}
	for ; j < m; j++ {
		out = append(out, ChunkDiffLine{Type: DiffAdd, Text: b[j]})
	}
	return out, nil
}

func splitDiffLines(s string) []string {
	if s == "" {
		return nil
	}
	return strings.Split(s, "\n")
}
