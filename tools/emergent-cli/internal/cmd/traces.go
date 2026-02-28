package cmd

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

	"github.com/spf13/cobra"
)

// ── Tempo HTTP API types ──────────────────────────────────────────────────────

type tempoSearchResponse struct {
	Traces []tempoTraceSearchResult `json:"traces"`
}

type tempoTraceSearchResult struct {
	TraceID            string            `json:"traceID"`
	RootServiceName    string            `json:"rootServiceName"`
	RootTraceName      string            `json:"rootTraceName"`
	StartTimeUnixNano  string            `json:"startTimeUnixNano"`
	DurationMs         float64           `json:"durationMs"`
	SpanSets           []tempoSpanSet    `json:"spanSets"`
	SpanSet            *tempoSpanSet     `json:"spanSet"`
	Attributes         map[string]string `json:"attributes"`
}

type tempoSpanSet struct {
	Matched int          `json:"matched"`
	Spans   []tempoSpan  `json:"spans"`
}

type tempoSpan struct {
	SpanID            string            `json:"spanID"`
	StartTimeUnixNano string            `json:"startTimeUnixNano"`
	DurationNanos     string            `json:"durationNanos"`
	Attributes        []tempoAttribute  `json:"attributes"`
}

type tempoAttribute struct {
	Key   string `json:"key"`
	Value struct {
		StringValue string `json:"stringValue"`
		IntValue    string `json:"intValue"`
	} `json:"value"`
}

// OTLP trace response from GET /api/traces/<id>
type otlpTraceResponse struct {
	Batches []otlpBatch `json:"batches"`
}

type otlpBatch struct {
	Resource struct {
		Attributes []tempoAttribute `json:"attributes"`
	} `json:"resource"`
	ScopeSpans []otlpScopeSpans `json:"scopeSpans"`
}

type otlpScopeSpans struct {
	Spans []otlpSpan `json:"spans"`
}

type otlpSpan struct {
	TraceID           string           `json:"traceId"`
	SpanID            string           `json:"spanId"`
	ParentSpanID      string           `json:"parentSpanId"`
	Name              string           `json:"name"`
	Kind              int              `json:"kind"`
	StartTimeUnixNano string           `json:"startTimeUnixNano"`
	EndTimeUnixNano   string           `json:"endTimeUnixNano"`
	Attributes        []tempoAttribute `json:"attributes"`
	Status            struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
	} `json:"status"`
}

// ── Flags ─────────────────────────────────────────────────────────────────────

var (
	tracesTempoURL    string
	tracesListSince   string
	tracesListLimit   int
	tracesSearchSvc   string
	tracesSearchRoute string
	tracesSearchMinDur string
	tracesSearchSince string
	tracesSearchLimit int
)

// ── Commands ──────────────────────────────────────────────────────────────────

var tracesCmd = &cobra.Command{
	Use:   "traces",
	Short: "Query traces from local Grafana Tempo",
	Long: `Query OpenTelemetry traces stored in a local Grafana Tempo instance.

Tempo must be running (docker compose --profile observability up tempo -d).
Configure the Tempo URL via --tempo-url flag or EMERGENT_TEMPO_URL env var.`,
}

var tracesListCmd = &cobra.Command{
	Use:   "list",
	Short: "List recent traces",
	Long:  "List recent traces from Tempo (default: last 1 hour, up to 20 results).",
	RunE:  runTracesList,
}

var tracesSearchCmd = &cobra.Command{
	Use:   "search",
	Short: "Search traces by criteria",
	Long:  "Search traces using TraceQL filters (service, route, min-duration).",
	RunE:  runTracesSearch,
}

var tracesGetCmd = &cobra.Command{
	Use:   "get <traceID>",
	Short: "Fetch a full trace by ID",
	Long:  "Fetch and display a full trace as a span tree.",
	Args:  cobra.ExactArgs(1),
	RunE:  runTracesGet,
}

// ── Helpers ───────────────────────────────────────────────────────────────────

func tempoURL() string {
	if tracesTempoURL != "" {
		return strings.TrimRight(tracesTempoURL, "/")
	}
	if v := os.Getenv("EMERGENT_TEMPO_URL"); v != "" {
		return strings.TrimRight(v, "/")
	}
	return "http://localhost:3200"
}

func tempoGet(path string, params url.Values) ([]byte, error) {
	u := tempoURL() + path
	if len(params) > 0 {
		u += "?" + params.Encode()
	}
	resp, err := http.Get(u) //nolint:gosec
	if err != nil {
		return nil, fmt.Errorf("cannot reach Tempo at %s: %w\nIs Tempo running? docker compose --profile observability up tempo -d", u, err)
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

func parseSince(since string) time.Time {
	d, err := time.ParseDuration(since)
	if err != nil {
		return time.Now().Add(-1 * time.Hour)
	}
	return time.Now().Add(-d)
}

func formatDuration(ms float64) string {
	if ms < 1000 {
		return fmt.Sprintf("%.0fms", ms)
	}
	return fmt.Sprintf("%.2fs", ms/1000)
}

func shortTraceID(id string) string {
	if len(id) > 16 {
		return id[:16] + "…"
	}
	return id
}

func nanoToTime(nano string) time.Time {
	n, _ := strconv.ParseInt(nano, 10, 64)
	return time.Unix(0, n)
}

func attrValue(attrs []tempoAttribute, key string) string {
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

func printTraceTable(traces []tempoTraceSearchResult) {
	if len(traces) == 0 {
		fmt.Println("No traces found.")
		return
	}
	fmt.Printf("%-18s  %-32s  %-8s  %-10s  %s\n",
		"TRACE ID", "ROOT SPAN", "DURATION", "TIMESTAMP", "SERVICE")
	fmt.Println(strings.Repeat("─", 90))
	for _, t := range traces {
		ts := ""
		if t.StartTimeUnixNano != "" {
			ts = nanoToTime(t.StartTimeUnixNano).Format("15:04:05")
		}
		root := t.RootTraceName
		if len(root) > 32 {
			root = root[:31] + "…"
		}
		fmt.Printf("%-18s  %-32s  %-8s  %-10s  %s\n",
			shortTraceID(t.TraceID),
			root,
			formatDuration(t.DurationMs),
			ts,
			t.RootServiceName,
		)
	}
}

// ── Subcommand implementations ────────────────────────────────────────────────

func runTracesList(cmd *cobra.Command, _ []string) error {
	since := parseSince(tracesListSince)
	params := url.Values{
		"limit": {strconv.Itoa(tracesListLimit)},
		"start": {strconv.FormatInt(since.Unix(), 10)},
		"end":   {strconv.FormatInt(time.Now().Unix(), 10)},
	}

	var conditions []string
	if id, err := resolveProjectContext(cmd, ""); err == nil && id != "" {
		conditions = append(conditions, fmt.Sprintf(`.project.id = "%s"`, id))
	}
	if len(conditions) > 0 {
		q := "{ " + strings.Join(conditions, " && ") + " }"
		params.Set("q", q)
	}

	body, err := tempoGet("/api/search", params)
	if err != nil {
		return err
	}
	var resp tempoSearchResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		return fmt.Errorf("unexpected Tempo response: %w", err)
	}
	fmt.Printf("Recent traces (last %s, limit %d)\n\n", tracesListSince, tracesListLimit)
	printTraceTable(resp.Traces)
	return nil
}

func runTracesSearch(cmd *cobra.Command, _ []string) error {
	// Build TraceQL query from flags
	var conditions []string
	if id, err := resolveProjectContext(cmd, ""); err == nil && id != "" {
		conditions = append(conditions, fmt.Sprintf(`.project.id = "%s"`, id))
	}
	if tracesSearchSvc != "" {
		conditions = append(conditions, fmt.Sprintf(`.service.name = "%s"`, tracesSearchSvc))
	}
	if tracesSearchRoute != "" {
		conditions = append(conditions, fmt.Sprintf(`.http.route = "%s"`, tracesSearchRoute))
	}
	if tracesSearchMinDur != "" {
		conditions = append(conditions, fmt.Sprintf(`duration > %s`, tracesSearchMinDur))
	}

	since := parseSince(tracesSearchSince)
	params := url.Values{
		"limit": {strconv.Itoa(tracesSearchLimit)},
		"start": {strconv.FormatInt(since.Unix(), 10)},
		"end":   {strconv.FormatInt(time.Now().Unix(), 10)},
	}
	if len(conditions) > 0 {
		q := "{ " + strings.Join(conditions, " && ") + " }"
		params.Set("q", q)
	}

	body, err := tempoGet("/api/search", params)
	if err != nil {
		return err
	}
	var resp tempoSearchResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		return fmt.Errorf("unexpected Tempo response: %w", err)
	}

	label := "All traces"
	if len(conditions) > 0 {
		label = "{ " + strings.Join(conditions, " && ") + " }"
	}
	fmt.Printf("Search: %s (last %s, limit %d)\n\n", label, tracesSearchSince, tracesSearchLimit)
	printTraceTable(resp.Traces)
	return nil
}

func runTracesGet(_ *cobra.Command, args []string) error {
	traceID := args[0]
	body, err := tempoGet("/api/traces/"+traceID, nil)
	if err != nil {
		return err
	}

	var resp otlpTraceResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		return fmt.Errorf("unexpected Tempo response: %w", err)
	}

	// Collect all spans
	type spanNode struct {
		span     otlpSpan
		children []*spanNode
	}
	nodes := map[string]*spanNode{}
	var roots []*spanNode

	for _, batch := range resp.Batches {
		for _, ss := range batch.ScopeSpans {
			for _, s := range ss.Spans {
				n := &spanNode{span: s}
				nodes[s.SpanID] = n
			}
		}
	}
	for _, n := range nodes {
		if n.span.ParentSpanID == "" {
			roots = append(roots, n)
		} else if parent, ok := nodes[n.span.ParentSpanID]; ok {
			parent.children = append(parent.children, n)
		} else {
			roots = append(roots, n)
		}
	}

	fmt.Printf("Trace: %s\n\n", traceID)

	var printNode func(n *spanNode, indent int)
	printNode = func(n *spanNode, indent int) {
		s := n.span
		startNs, _ := strconv.ParseInt(s.StartTimeUnixNano, 10, 64)
		endNs, _ := strconv.ParseInt(s.EndTimeUnixNano, 10, 64)
		durMs := float64(endNs-startNs) / 1e6

		statusIcon := "✓"
		if s.Status.Code == 2 { // ERROR
			statusIcon = "✗"
		}

		prefix := strings.Repeat("  ", indent)
		fmt.Printf("%s%s %s  [%s]\n", prefix, statusIcon, s.Name, formatDuration(durMs))

		// Print key HTTP attributes
		for _, key := range []string{"http.method", "http.route", "http.status_code", "http.url", "db.statement", "error"} {
			if v := attrValue(s.Attributes, key); v != "" {
				if len(v) > 80 {
					v = v[:79] + "…"
				}
				fmt.Printf("%s    %s: %s\n", prefix, key, v)
			}
		}

		for _, c := range n.children {
			printNode(c, indent+1)
		}
	}

	for _, r := range roots {
		printNode(r, 0)
	}
	return nil
}

// ── Init ──────────────────────────────────────────────────────────────────────

func init() {
	// Persistent: tempo URL applies to all subcommands
	tracesCmd.PersistentFlags().StringVar(&tracesTempoURL, "tempo-url", "", "Tempo query API URL (default: $EMERGENT_TEMPO_URL or http://localhost:3200)")

	// list flags
	tracesListCmd.Flags().StringVar(&tracesListSince, "since", "1h", "Show traces from the last duration (e.g. 30m, 2h, 24h)")
	tracesListCmd.Flags().IntVar(&tracesListLimit, "limit", 20, "Maximum number of traces to return")

	// search flags
	tracesSearchCmd.Flags().StringVar(&tracesSearchSvc, "service", "", "Filter by service name")
	tracesSearchCmd.Flags().StringVar(&tracesSearchRoute, "route", "", "Filter by HTTP route (e.g. /api/kb/documents)")
	tracesSearchCmd.Flags().StringVar(&tracesSearchMinDur, "min-duration", "", "Filter by minimum duration (e.g. 200ms, 1s)")
	tracesSearchCmd.Flags().StringVar(&tracesSearchSince, "since", "1h", "Search within the last duration (e.g. 30m, 2h, 24h)")
	tracesSearchCmd.Flags().IntVar(&tracesSearchLimit, "limit", 20, "Maximum number of results")

	tracesCmd.AddCommand(tracesListCmd)
	tracesCmd.AddCommand(tracesSearchCmd)
	tracesCmd.AddCommand(tracesGetCmd)
	rootCmd.AddCommand(tracesCmd)
}
