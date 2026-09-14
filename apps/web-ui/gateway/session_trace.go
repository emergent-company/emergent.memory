package main

import (
	"fmt"
	"math"
	"sort"
	"strconv"
)

// --- OpenTelemetry trace display helpers (session viewer waterfall) ---

// traceRow is one trace span positioned for the waterfall: the span plus its
// tree depth (root = 0, children one deeper). Rows come out in display order.
type traceRow struct {
	span  TraceSpan
	depth int
}

// traceRows turns a flat span list (as returned inline by the agent-run API)
// into waterfall rows: roots first (sorted by start time), then each root's
// descendants depth-first, also ordered by start time. Depth rules: a root
// span (no parentSpanId) is depth 0; a child sits one level below its parent;
// a span whose parent is missing from the set but that is not itself a root is
// an orphan at depth 1. Cycles and unresolvable chains degrade to depth 1
// rather than recursing forever.
func traceRows(spans []TraceSpan) []traceRow {
	if len(spans) == 0 {
		return nil
	}
	byID := make(map[string]int, len(spans))
	for i := range spans {
		byID[spans[i].SpanID] = i
	}

	// depth[i] = -2 while resolving (cycle guard), -1 unresolved.
	depth := make([]int, len(spans))
	for i := range depth {
		depth[i] = -1
	}
	var resolve func(i int) int
	resolve = func(i int) int {
		if depth[i] >= 0 {
			return depth[i]
		}
		if depth[i] == -2 { // cycle: treat as orphan depth
			return 1
		}
		if spans[i].ParentSpanID == "" {
			depth[i] = 0
			return 0
		}
		pi, ok := byID[spans[i].ParentSpanID]
		if !ok { // parent outside the set
			depth[i] = 1
			return 1
		}
		depth[i] = -2
		depth[i] = resolve(pi) + 1
		return depth[i]
	}
	for i := range spans {
		resolve(i)
	}

	// Order by start time; stable so equal-start siblings keep their input order.
	idx := make([]int, len(spans))
	for i := range idx {
		idx[i] = i
	}
	sort.SliceStable(idx, func(a, b int) bool {
		return spans[idx[a]].StartUnixNano < spans[idx[b]].StartUnixNano
	})
	children := make(map[string][]int) // parent span id → child indexes (sorted)
	var roots []int
	for _, i := range idx {
		pid := spans[i].ParentSpanID
		if pid == "" {
			roots = append(roots, i)
			continue
		}
		if _, ok := byID[pid]; !ok {
			roots = append(roots, i) // orphan floats to the top of its own subtree
			continue
		}
		children[pid] = append(children[pid], i)
	}

	rows := make([]traceRow, 0, len(spans))
	var emit func(i int)
	emit = func(i int) {
		rows = append(rows, traceRow{span: spans[i], depth: depth[i]})
		for _, c := range children[spans[i].SpanID] {
			emit(c)
		}
	}
	for _, r := range roots {
		emit(r)
	}
	return rows
}

// formatSpanDuration renders a span's wall-clock duration from Unix-nanosecond
// times. Sub-second spans keep integer millisecond precision ("45ms"), spans
// under a minute render seconds with 2 decimals ("1.23s"), and longer spans
// escalate units via formatRunDuration ("1m", "2h 30m") so e.g. a 75-second
// span renders as "1m" instead of "75.00s". Unparseable/negative durations
// render as "0ms".
func formatSpanDuration(startNano, endNano int64) string {
	ms := float64(endNano-startNano) / 1e6
	if ms < 0 {
		ms = 0
	}
	sec := ms / 1000
	switch {
	case sec < 1:
		return strconv.Itoa(int(math.Round(ms))) + "ms"
	case sec < 60:
		return fmt.Sprintf("%.2fs", sec)
	default:
		return formatRunDuration(sec)
	}
}

// llmTokenLabel renders a call_llm span's input → output token counts. It
// returns (label, false) when both counts are zero (absent); a missing side
// renders as an en dash. The span struct already carries int64 counts.
func llmTokenLabel(inTokens, outTokens int64) (string, bool) {
	if inTokens == 0 && outTokens == 0 {
		return "", false
	}
	in := strconv.FormatInt(inTokens, 10)
	out := strconv.FormatInt(outTokens, 10)
	if inTokens == 0 {
		in = "–"
	}
	if outTokens == 0 {
		out = "–"
	}
	return fmt.Sprintf("%s → %s tokens", in, out), true
}
