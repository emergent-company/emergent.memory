package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/spf13/cobra"
)

// ── Tempo HTTP API types ──────────────────────────────────────────────────────

type tempoSearchResponse struct {
	Traces []tempoTraceSearchResult `json:"traces"`
}

type tempoTraceSearchResult struct {
	TraceID           string            `json:"traceID"`
	RootServiceName   string            `json:"rootServiceName"`
	RootTraceName     string            `json:"rootTraceName"`
	StartTimeUnixNano string            `json:"startTimeUnixNano"`
	DurationMs        float64           `json:"durationMs"`
	SpanSets          []tempoSpanSet    `json:"spanSets"`
	SpanSet           *tempoSpanSet     `json:"spanSet"`
	Attributes        map[string]string `json:"attributes"`
}

type tempoSpanSet struct {
	Matched int         `json:"matched"`
	Spans   []tempoSpan `json:"spans"`
}

type tempoSpan struct {
	SpanID            string           `json:"spanID"`
	StartTimeUnixNano string           `json:"startTimeUnixNano"`
	DurationNanos     string           `json:"durationNanos"`
	Attributes        []tempoAttribute `json:"attributes"`
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
	Kind              json.RawMessage  `json:"kind"`
	StartTimeUnixNano string           `json:"startTimeUnixNano"`
	EndTimeUnixNano   string           `json:"endTimeUnixNano"`
	Attributes        []tempoAttribute `json:"attributes"`
	Status            struct {
		Code    json.RawMessage `json:"code"`
		Message string          `json:"message"`
	} `json:"status"`
}

// runTokenUsage is a minimal representation of the tokenUsage field in AgentRunDTO.
type runTokenUsage struct {
	TotalInputTokens  int64   `json:"totalInputTokens"`
	TotalOutputTokens int64   `json:"totalOutputTokens"`
	EstimatedCostUSD  float64 `json:"estimatedCostUsd"`
}

// traceRunInfo holds per-trace agent run metadata fetched from the agent-runs API.
type traceRunInfo struct {
	AgentID   string
	AgentName string // resolved after fetching agents
	Usage     *runTokenUsage
}

// agentRunDTOResponse represents the API response wrapping AgentRunDTO.
type agentRunDTOResponse struct {
	Success bool `json:"success"`
	Data    struct {
		AgentID    string         `json:"agentId"`
		TokenUsage *runTokenUsage `json:"tokenUsage"`
	} `json:"data"`
}

// agentGetResponse is the API response for GET /api/projects/:pid/agents/:id.
type agentGetResponse struct {
	Success bool `json:"success"`
	Data    struct {
		Name string `json:"name"`
	} `json:"data"`
}

// ── Flags ─────────────────────────────────────────────────────────────────────

var (
	tracesListSince     string
	tracesListLimit     int
	tracesListAgentRuns bool
	tracesSearchSvc     string
	tracesSearchRoute   string
	tracesSearchMinDur  string
	tracesSearchSince   string
	tracesSearchLimit   int
	tracesGetDebug      bool
)

// ── Commands ──────────────────────────────────────────────────────────────────

var tracesCmd = &cobra.Command{
	Use:   "traces",
	Short: "Query traces",
	Long: `Query OpenTelemetry traces via the server's built-in Tempo proxy.

Traces are proxied through the configured --server endpoint so no direct
access to Tempo is required.`,
}

var tracesListCmd = &cobra.Command{
	Use:   "list",
	Short: "List recent traces",
	Long:  "List recent traces (default: last 1 hour, up to 20 results).",
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

// tracesGet calls the server's Tempo proxy at /api/traces<path> with auth.
// It uses the SDK client's Do() method so that the correct auth header is set
// regardless of auth mode (standalone X-API-Key vs Bearer token).
func tracesGet(cmd *cobra.Command, path string, params url.Values) ([]byte, error) {
	c, err := getClient(cmd)
	if err != nil {
		return nil, fmt.Errorf("cannot initialise client: %w", err)
	}
	u := strings.TrimRight(c.BaseURL(), "/") + "/api/traces" + path
	if len(params) > 0 {
		u += "?" + params.Encode()
	}
	req, err := http.NewRequest(http.MethodGet, u, nil)
	if err != nil {
		return nil, fmt.Errorf("build request: %w", err)
	}
	resp, err := c.SDK.Do(context.Background(), req)
	if err != nil {
		return nil, fmt.Errorf("cannot reach server traces endpoint at %s: %w", u, err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read response: %w", err)
	}
	if resp.StatusCode == http.StatusServiceUnavailable {
		return nil, fmt.Errorf("tracing is not enabled on the server (OTEL_EXPORTER_OTLP_ENDPOINT not configured)")
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("server returned %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}
	return body, nil
}

// fetchRunInfo calls GET /api/projects/:projectId/agent-runs/:runId and
// returns token usage + agentId. Returns nil on any error (graceful degradation).
func fetchRunInfo(cmd *cobra.Command, projectID, runID string) *traceRunInfo {
	c, err := getClient(cmd)
	if err != nil {
		return nil
	}
	u := strings.TrimRight(c.BaseURL(), "/") +
		"/api/projects/" + projectID + "/agent-runs/" + runID
	req, err := http.NewRequest(http.MethodGet, u, nil)
	if err != nil {
		return nil
	}
	resp, err := c.SDK.Do(context.Background(), req)
	if err != nil {
		return nil
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil
	}
	var dto agentRunDTOResponse
	if err := json.Unmarshal(body, &dto); err != nil {
		return nil
	}
	return &traceRunInfo{
		AgentID: dto.Data.AgentID,
		Usage:   dto.Data.TokenUsage,
	}
}

// fetchRunTokenUsage is a compatibility shim used by runTracesGet.
func fetchRunTokenUsage(cmd *cobra.Command, projectID, runID string) *runTokenUsage {
	if info := fetchRunInfo(cmd, projectID, runID); info != nil {
		return info.Usage
	}
	return nil
}

// fetchAgentName calls GET /api/projects/:pid/agents/:agentId and returns the name.
// Returns "" on any error.
func fetchAgentName(cmd *cobra.Command, projectID, agentID string) string {
	if projectID == "" || agentID == "" {
		return ""
	}
	c, err := getClient(cmd)
	if err != nil {
		return ""
	}
	u := strings.TrimRight(c.BaseURL(), "/") +
		"/api/projects/" + projectID + "/agents/" + agentID
	req, err := http.NewRequest(http.MethodGet, u, nil)
	if err != nil {
		return ""
	}
	resp, err := c.SDK.Do(context.Background(), req)
	if err != nil {
		return ""
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return ""
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return ""
	}
	var ag agentGetResponse
	if err := json.Unmarshal(body, &ag); err != nil {
		return ""
	}
	return ag.Data.Name
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

// fetchRunInfos concurrently fetches agent run info (token usage + agentId) for
// traces that have an agent run ID. Returns a map from traceID → *traceRunInfo.
// Agent names are resolved in a second pass (one request per unique agentId).
// projectID is used as a fallback; per-trace project ID is preferred from
// the span.memory.project.id attribute (populated via TraceQL select()).
func fetchRunInfos(cmd *cobra.Command, projectID string, traces []tempoTraceSearchResult) map[string]*traceRunInfo {
	result := make(map[string]*traceRunInfo)

	var mu sync.Mutex
	var wg sync.WaitGroup

	// agentRunAttr returns the value of memory.agent.run_id or emergent.agent.run_id (legacy).
	agentRunAttr := func(attrs []tempoAttribute) string {
		for _, a := range attrs {
			if a.Key == "memory.agent.run_id" || a.Key == "emergent.agent.run_id" {
				if a.Value.StringValue != "" {
					return a.Value.StringValue
				}
			}
		}
		return ""
	}
	// projectAttr returns the value of memory.project.id or emergent.project.id (legacy).
	projectAttr := func(attrs []tempoAttribute) string {
		for _, a := range attrs {
			if a.Key == "memory.project.id" || a.Key == "emergent.project.id" {
				if a.Value.StringValue != "" {
					return a.Value.StringValue
				}
			}
		}
		return ""
	}

	for _, t := range traces {
		// Check top-level attributes map first (set by Tempo search metadata).
		runID := t.Attributes["memory.agent.run_id"]
		if runID == "" {
			runID = t.Attributes["emergent.agent.run_id"]
		}
		if runID == "" {
			// Also check spanSets for the attribute
			for _, ss := range t.SpanSets {
				for _, sp := range ss.Spans {
					if v := agentRunAttr(sp.Attributes); v != "" {
						runID = v
					}
				}
			}
			if t.SpanSet != nil {
				for _, sp := range t.SpanSet.Spans {
					if v := agentRunAttr(sp.Attributes); v != "" {
						runID = v
					}
				}
			}
		}
		if runID == "" {
			continue
		}

		// Prefer per-trace project ID from attributes over global flag value
		pid := t.Attributes["memory.project.id"]
		if pid == "" {
			pid = t.Attributes["emergent.project.id"]
		}
		if pid == "" {
			// Check spanSets for project ID
			for _, ss := range t.SpanSets {
				for _, sp := range ss.Spans {
					if v := projectAttr(sp.Attributes); v != "" {
						pid = v
					}
				}
			}
			if t.SpanSet != nil {
				for _, sp := range t.SpanSet.Spans {
					if v := projectAttr(sp.Attributes); v != "" {
						pid = v
					}
				}
			}
		}
		if pid == "" {
			pid = projectID
		}
		if pid == "" {
			continue
		}

		traceID := t.TraceID
		wg.Add(1)
		go func(tid, rid, p string) {
			defer wg.Done()
			info := fetchRunInfo(cmd, p, rid)
			if info != nil {
				mu.Lock()
				result[tid] = info
				mu.Unlock()
			}
		}(traceID, runID, pid)
	}
	wg.Wait()

	// Second pass: resolve unique agentIds → names
	type agentKey struct{ pid, aid string }
	// Also build a reverse map from traceID → projectID for per-trace lookups
	traceProject := map[string]string{}
	for _, t := range traces {
		pid := t.Attributes["memory.project.id"]
		if pid == "" {
			pid = t.Attributes["emergent.project.id"]
		}
		if pid == "" {
			pid = projectID
		}
		traceProject[t.TraceID] = pid
	}
	// Resolve agent names concurrently (one per unique agentId)
	var nameMu sync.Mutex
	var nameWg sync.WaitGroup
	// Collect unique agentKeys including per-trace project
	agentKeySet := map[agentKey]bool{}
	for tid, info := range result {
		if info.AgentID == "" {
			continue
		}
		pid := traceProject[tid]
		if pid == "" {
			pid = projectID
		}
		agentKeySet[agentKey{pid: pid, aid: info.AgentID}] = true
	}
	resolvedNames := map[agentKey]string{}
	for k := range agentKeySet {
		k := k
		nameWg.Add(1)
		go func() {
			defer nameWg.Done()
			name := fetchAgentName(cmd, k.pid, k.aid)
			nameMu.Lock()
			resolvedNames[k] = name
			nameMu.Unlock()
		}()
	}
	nameWg.Wait()

	// Apply resolved names back to result
	for tid, info := range result {
		if info.AgentID == "" {
			continue
		}
		pid := traceProject[tid]
		if pid == "" {
			pid = projectID
		}
		info.AgentName = resolvedNames[agentKey{pid: pid, aid: info.AgentID}]
	}

	return result
}

func printTraceTable(traces []tempoTraceSearchResult, runInfos map[string]*traceRunInfo, showCosts bool) {
	if len(traces) == 0 {
		fmt.Println("No traces found.")
		return
	}
	sort.Slice(traces, func(i, j int) bool {
		ni, _ := strconv.ParseInt(traces[i].StartTimeUnixNano, 10, 64)
		nj, _ := strconv.ParseInt(traces[j].StartTimeUnixNano, 10, 64)
		return ni > nj
	})
	if showCosts {
		fmt.Printf("%-32s  %-20s  %-36s  %-8s  %-10s  %-14s  %-14s  %s\n",
			"TRACE ID", "AGENT", "ROOT SPAN", "DURATION", "TIMESTAMP",
			"INPUT TOKENS", "OUTPUT TOKENS", "EST. COST")
		fmt.Println(strings.Repeat("─", 162))
	} else {
		fmt.Printf("%-32s  %-36s  %-8s  %s\n",
			"TRACE ID", "ROOT SPAN", "DURATION", "TIMESTAMP")
		fmt.Println(strings.Repeat("─", 96))
	}
	for _, t := range traces {
		ts := ""
		if t.StartTimeUnixNano != "" {
			ts = nanoToTime(t.StartTimeUnixNano).Format("15:04:05")
		}
		root := t.RootTraceName
		if len(root) > 36 {
			root = root[:35] + "…"
		}

		if showCosts {
			agentName := "—"
			inputTok := "—"
			outputTok := "—"
			cost := "—"
			if info, ok := runInfos[t.TraceID]; ok && info != nil {
				if info.AgentName != "" {
					agentName = info.AgentName
				} else if info.AgentID != "" {
					agentName = info.AgentID[:8] + "…"
				}
				if info.Usage != nil {
					inputTok = strconv.FormatInt(info.Usage.TotalInputTokens, 10)
					outputTok = strconv.FormatInt(info.Usage.TotalOutputTokens, 10)
					cost = fmt.Sprintf("$%.6f", info.Usage.EstimatedCostUSD)
				}
			}
			if len(agentName) > 20 {
				agentName = agentName[:19] + "…"
			}
			fmt.Printf("%-32s  %-20s  %-36s  %-8s  %-10s  %-14s  %-14s  %s\n",
				t.TraceID, agentName, root, formatDuration(t.DurationMs), ts,
				inputTok, outputTok, cost)
		} else {
			fmt.Printf("%-32s  %-36s  %-8s  %s\n",
				t.TraceID, root, formatDuration(t.DurationMs), ts)
		}
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

	projectID, _ := resolveProjectContext(cmd, "")

	// Resolve project name for the header (best effort).
	projectLabel := "all projects"
	if projectID != "" {
		c, err := getClient(cmd)
		if err == nil {
			if p, err := c.SDK.Projects.Get(context.Background(), projectID, nil); err == nil && p != nil && p.Name != "" {
				projectLabel = "project: " + p.Name
			} else {
				projectLabel = "project: " + projectID[:min(8, len(projectID))] + "…"
			}
		}
	}

	var conditions []string
	if projectID != "" {
		conditions = append(conditions, fmt.Sprintf(`.memory.project.id = "%s" || .emergent.project.id = "%s"`, projectID, projectID))
	}
	if tracesListAgentRuns {
		conditions = append(conditions, `rootName = "agent.run"`)
	}
	if len(conditions) > 0 {
		q := "{ " + strings.Join(conditions, " && ") + " }"
		if tracesListAgentRuns {
			q += " | select(span.emergent.agent.run_id, span.emergent.project.id, span.memory.agent.run_id, span.memory.project.id)"
		}
		params.Set("q", q)
	} else if tracesListAgentRuns {
		params.Set("q", `{ rootName = "agent.run" } | select(span.emergent.agent.run_id, span.emergent.project.id, span.memory.agent.run_id, span.memory.project.id)`)
	}

	body, err := tracesGet(cmd, "/search", params)
	if err != nil {
		return err
	}
	var resp tempoSearchResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		return fmt.Errorf("unexpected response: %w", err)
	}

	var runInfos map[string]*traceRunInfo
	if tracesListAgentRuns {
		runInfos = fetchRunInfos(cmd, projectID, resp.Traces)
		fmt.Printf("Agent run traces — %s (last %s, limit %d)\n\n", projectLabel, tracesListSince, tracesListLimit)
	} else {
		fmt.Printf("Recent traces — %s (last %s, limit %d)\n\n", projectLabel, tracesListSince, tracesListLimit)
	}
	printTraceTable(resp.Traces, runInfos, tracesListAgentRuns)
	return nil
}

func runTracesSearch(cmd *cobra.Command, _ []string) error {
	// Build TraceQL query from flags
	var conditions []string
	if id, err := resolveProjectContext(cmd, ""); err == nil && id != "" {
		conditions = append(conditions, fmt.Sprintf(`.memory.project.id = "%s"`, id))
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

	body, err := tracesGet(cmd, "/search", params)
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
	printTraceTable(resp.Traces, nil, false)
	return nil
}

func runTracesGet(cmd *cobra.Command, args []string) error {
	traceID := args[0]
	body, err := tracesGet(cmd, "/"+traceID, nil)
	if err != nil {
		return err
	}

	var resp otlpTraceResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		return fmt.Errorf("unexpected response: %w", err)
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

	// Walk all spans looking for memory.agent.run_id / emergent.agent.run_id to print a token summary.
	projectID, _ := resolveProjectContext(cmd, "")
	if projectID != "" {
		var runID string
	outer:
		for _, batch := range resp.Batches {
			for _, ss := range batch.ScopeSpans {
				for _, s := range ss.Spans {
					if v := attrValue(s.Attributes, "memory.agent.run_id"); v != "" {
						runID = v
						break outer
					}
					if v := attrValue(s.Attributes, "emergent.agent.run_id"); v != "" {
						runID = v
						break outer
					}
				}
			}
		}
		if runID != "" {
			if usage := fetchRunTokenUsage(cmd, projectID, runID); usage != nil {
				fmt.Printf("Tokens: %d in / %d out  Est. Cost: $%.6f\n\n",
					usage.TotalInputTokens, usage.TotalOutputTokens, usage.EstimatedCostUSD)
			}
		}
	}

	var printNode func(n *spanNode, indent int)
	printNode = func(n *spanNode, indent int) {
		s := n.span

		// Filter out ADK-internal "(merged)" bookkeeping spans unless --debug is set.
		if !tracesGetDebug && strings.Contains(s.Name, "(merged)") {
			return
		}

		startNs, _ := strconv.ParseInt(s.StartTimeUnixNano, 10, 64)
		endNs, _ := strconv.ParseInt(s.EndTimeUnixNano, 10, 64)
		durMs := float64(endNs-startNs) / 1e6

		statusIcon := "✓"
		statusCode := strings.Trim(string(s.Status.Code), `"`)
		if statusCode == "2" || statusCode == "STATUS_CODE_ERROR" {
			statusIcon = "✗"
		}

		prefix := strings.Repeat("  ", indent)
		fmt.Printf("%s%s %s  [%s]\n", prefix, statusIcon, s.Name, formatDuration(durMs))

		// Print key HTTP attributes
		for _, key := range []string{"http.method", "http.route", "http.status_code", "http.url", "db.statement", "error", "memory.agent.run_id", "emergent.agent.run_id"} {
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
	// list flags
	tracesListCmd.Flags().StringVar(&tracesListSince, "since", "1h", "Show traces from the last duration (e.g. 30m, 2h, 24h)")
	tracesListCmd.Flags().IntVar(&tracesListLimit, "limit", 20, "Maximum number of traces to return")
	tracesListCmd.Flags().BoolVar(&tracesListAgentRuns, "agent-runs", false, "Filter to agent.run root spans and show token/cost columns")

	// search flags
	tracesSearchCmd.Flags().StringVar(&tracesSearchSvc, "service", "", "Filter by service name")
	tracesSearchCmd.Flags().StringVar(&tracesSearchRoute, "route", "", "Filter by HTTP route (e.g. /api/kb/documents)")
	tracesSearchCmd.Flags().StringVar(&tracesSearchMinDur, "min-duration", "", "Filter by minimum duration (e.g. 200ms, 1s)")
	tracesSearchCmd.Flags().StringVar(&tracesSearchSince, "since", "1h", "Search within the last duration (e.g. 30m, 2h, 24h)")
	tracesSearchCmd.Flags().IntVar(&tracesSearchLimit, "limit", 20, "Maximum number of results")

	// get flags
	tracesGetCmd.Flags().BoolVar(&tracesGetDebug, "debug", false, "Show all spans including internal ADK bookkeeping spans (e.g. merged tool responses)")

	tracesCmd.AddCommand(tracesListCmd)
	tracesCmd.AddCommand(tracesSearchCmd)
	tracesCmd.AddCommand(tracesGetCmd)
	rootCmd.AddCommand(tracesCmd)
}
