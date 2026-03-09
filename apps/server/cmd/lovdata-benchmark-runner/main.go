package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

type BenchmarkResult struct {
	Name        string
	Time        time.Duration
	ToolCalls   int
	ToolsUsed   []string
	Success     bool
	FinalAnswer string
}

func main() {
	queries := []struct {
		Name  string
		Query string
	}{
		{
			Name:  "1_Direct_Retrieval",
			Query: "What ministry administers the 'Working Environment Act' (Arbeidsmiljøloven)?",
		},
		{
			Name:  "2_Property_Filter_Traversal",
			Query: "List 3 regulations that are administered by the Ministry of Finance (Finansdepartementet) and belong to the 'Tax' (Skatt) legal area.",
		},
		{
			Name:  "3_Multi_Hop_External_Intersection",
			Query: "Find a Norwegian law that implements an EU directive related to 'consumer protection'. Name the Norwegian law and the EU directive.",
		},
		{
			Name:  "4_Cross_Reference_Self_Referential",
			Query: "Which Norwegian laws amend the 'Road Traffic Act' (Vegtrafikkloven)? List up to 5.",
		},
		{
			Name:  "5_Complex_Aggregation",
			Query: "Use a direct Cypher-like graph query or aggregation to find which EU directive CELEX ID has the highest number of IMPLEMENTS_EEA relationships. Just name the top CELEX ID.",
		},
	}

	fmt.Println("======================================================")
	fmt.Println("🚀 STARTING LOVDATA GRAPH AGENT BENCHMARK SUITE")
	fmt.Println("======================================================")
	fmt.Println()

	var totalTime time.Duration
	var results []BenchmarkResult

	for i, q := range queries {
		fmt.Printf("⏳ [%d/5] Running: %s\n", i+1, q.Name)
		fmt.Printf("   Query: \"%s\"\n", q.Query)

		start := time.Now()
		result := runQuery(q.Query)
		elapsed := time.Since(start)

		totalTime += elapsed
		result.Name = q.Name
		result.Time = elapsed

		results = append(results, result)

		if result.Success {
			fmt.Printf("   ✅ Success in %v (%d tool calls)\n", elapsed, result.ToolCalls)
		} else {
			fmt.Printf("   ❌ Failed in %v\n", elapsed)
		}
		fmt.Printf("   Tools Used: %v\n", result.ToolsUsed)
		fmt.Printf("   Answer Snippet: %s...\n\n", truncate(result.FinalAnswer, 150))

		// Small breather between queries to not get rate limited by LLM provider
		time.Sleep(2 * time.Second)
	}

	fmt.Println("======================================================")
	fmt.Println("📊 BENCHMARK RESULTS SUMMARY")
	fmt.Println("======================================================")
	fmt.Printf("Total Execution Time: %v\n\n", totalTime)

	for _, r := range results {
		icon := "✅"
		if !r.Success {
			icon = "❌"
		}
		fmt.Printf("%s %-35s | Time: %-10v | Tools: %d\n", icon, r.Name, r.Time, r.ToolCalls)
	}
}

func runQuery(query string) BenchmarkResult {
	reqBody := map[string]any{
		"message": query,
		// Using the agent definition we created for Lovdata
		"agentDefinitionId": "0938a58b-a673-440b-a490-cf692cda3c23",
	}
	bodyBytes, _ := json.Marshal(reqBody)

	// Updated to use mcj-emergent dev server
	req, _ := http.NewRequest("POST", "http://mcj-emergent:3002/api/chat/stream", bytes.NewBuffer(bodyBytes))
	req.Header.Set("Content-Type", "application/json")
	// Using the root/user API key which has chat permissions
	req.Header.Set("X-API-Key", "4334d35abf06be28271d7c5cd5a1cf02a6e9b4c2753db816e48419330e023060")
	// The project ID for Norwegian Law
	req.Header.Set("X-Project-ID", "cfb7d045-a2ac-49b0-9ff4-48e545fec272")

	client := &http.Client{Timeout: 300 * time.Second}
	resp, err := client.Do(req)

	res := BenchmarkResult{
		Success: false,
	}

	if err != nil {
		fmt.Printf("   Error: %v\n", err)
		return res
	}
	if resp.StatusCode != 200 {
		b, _ := io.ReadAll(resp.Body)
		fmt.Printf("   Error: Status %d - %s\n", resp.StatusCode, string(b))
		return res
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	lines := strings.Split(string(body), "\n")

	var finalAnswer strings.Builder
	for _, line := range lines {
		if strings.HasPrefix(line, "data: ") {
			data := strings.TrimPrefix(line, "data: ")
			var evt map[string]any
			if err := json.Unmarshal([]byte(data), &evt); err == nil {
				evtType, _ := evt["type"].(string)
				if evtType == "mcp_tool" {
					status, _ := evt["status"].(string)
					if status == "started" {
						res.ToolCalls++
						toolName, _ := evt["tool"].(string)
						res.ToolsUsed = append(res.ToolsUsed, toolName)
					}
				} else if evtType == "token" {
					token, _ := evt["token"].(string)
					finalAnswer.WriteString(token)
				}
			}
		}
	}

	res.FinalAnswer = finalAnswer.String()
	res.Success = res.FinalAnswer != "" && res.ToolCalls > 0
	return res
}

func truncate(s string, length int) string {
	s = strings.ReplaceAll(s, "\n", " ")
	if len(s) > length {
		return s[:length]
	}
	return s
}
