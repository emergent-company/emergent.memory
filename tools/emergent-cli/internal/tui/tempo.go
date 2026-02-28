package tui

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

// ── Tempo HTTP types ──────────────────────────────────────────────────────────

type traceSearchResp struct {
	Traces []traceResult `json:"traces"`
}

type traceResult struct {
	TraceID           string  `json:"traceID"`
	RootServiceName   string  `json:"rootServiceName"`
	RootTraceName     string  `json:"rootTraceName"`
	StartTimeUnixNano string  `json:"startTimeUnixNano"`
	DurationMs        float64 `json:"durationMs"`
}

type traceOTLPResp struct {
	Batches []traceOTLPBatch `json:"batches"`
}

type traceOTLPBatch struct {
	ScopeSpans []traceScopeSpans `json:"scopeSpans"`
}

type traceScopeSpans struct {
	Spans []traceSpan `json:"spans"`
}

type traceSpan struct {
	SpanID            string      `json:"spanId"`
	ParentSpanID      string      `json:"parentSpanId"`
	Name              string      `json:"name"`
	StartTimeUnixNano string      `json:"startTimeUnixNano"`
	EndTimeUnixNano   string      `json:"endTimeUnixNano"`
	Attributes        []traceAttr `json:"attributes"`
	Status            struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
	} `json:"status"`
}

type traceAttr struct {
	Key   string `json:"key"`
	Value struct {
		StringValue string `json:"stringValue"`
		IntValue    string `json:"intValue"`
	} `json:"value"`
}

// ── Messages ──────────────────────────────────────────────────────────────────

type tracesLoadedMsg struct {
	traces []traceResult
}

type traceDetailLoadedMsg struct {
	detail *traceOTLPResp
	err    error
}

type tracesErrMsg struct {
	err error
}

// ── HTTP helpers ──────────────────────────────────────────────────────────────

func resolveTempoURL() string {
	if v := os.Getenv("EMERGENT_TEMPO_URL"); v != "" {
		return strings.TrimRight(v, "/")
	}
	return "http://localhost:3200"
}

func doTempoGet(tempoURL, path string, params url.Values) ([]byte, error) {
	u := tempoURL + path
	if len(params) > 0 {
		u += "?" + params.Encode()
	}
	resp, err := http.Get(u) //nolint:gosec
	if err != nil {
		return nil, fmt.Errorf("cannot reach Tempo at %s: %w", u, err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("Tempo returned HTTP %d: %s", resp.StatusCode, body)
	}
	return body, nil
}

func traceAttrValue(attrs []traceAttr, key string) string {
	for _, a := range attrs {
		if a.Key == key {
			if a.Value.StringValue != "" {
				return a.Value.StringValue
			}
			return a.Value.IntValue
		}
	}
	return ""
}

func traceNanoToTime(nano string) time.Time {
	n, _ := strconv.ParseInt(nano, 10, 64)
	return time.Unix(0, n)
}

func traceSpanDurMs(startNano, endNano string) float64 {
	s, _ := strconv.ParseInt(startNano, 10, 64)
	e, _ := strconv.ParseInt(endNano, 10, 64)
	return float64(e-s) / 1e6
}

func traceFmtDur(ms float64) string {
	if ms < 1000 {
		return fmt.Sprintf("%.0fms", ms)
	}
	return fmt.Sprintf("%.2fs", ms/1000)
}

func traceShortID(id string) string {
	if len(id) > 16 {
		return id[:16] + "…"
	}
	return id
}

// ── Async commands ────────────────────────────────────────────────────────────

func loadTraces(tempoURL, projectID string) tea.Cmd {
	return func() tea.Msg {
		since := time.Now().Add(-1 * time.Hour)
		params := url.Values{
			"limit": {"30"},
			"start": {strconv.FormatInt(since.Unix(), 10)},
			"end":   {strconv.FormatInt(time.Now().Unix(), 10)},
		}
		if projectID != "" {
			params.Set("q", fmt.Sprintf(`{span.emergent.project.id="%s"}`, projectID))
		}
		body, err := doTempoGet(tempoURL, "/api/search", params)
		if err != nil {
			return tracesErrMsg{err: err}
		}
		var resp traceSearchResp
		if err := json.Unmarshal(body, &resp); err != nil {
			return tracesErrMsg{err: fmt.Errorf("unexpected Tempo response: %w", err)}
		}
		return tracesLoadedMsg{traces: resp.Traces}
	}
}

func loadTraceDetail(tempoURL, traceID string) tea.Cmd {
	return func() tea.Msg {
		body, err := doTempoGet(tempoURL, "/api/traces/"+traceID, nil)
		if err != nil {
			return traceDetailLoadedMsg{err: err}
		}
		var resp traceOTLPResp
		if err := json.Unmarshal(body, &resp); err != nil {
			return traceDetailLoadedMsg{err: fmt.Errorf("unexpected Tempo response: %w", err)}
		}
		return traceDetailLoadedMsg{detail: &resp}
	}
}
