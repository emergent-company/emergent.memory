package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io/ioutil"
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
			Name:  "1_Direct_Traversal",
			Query: "What movies did Steven Spielberg direct?",
		},
		{
			Name:  "2_Property_Filter_Traversal",
			Query: "Find all actors who have co-starred with Meg Ryan in a movie that has a rating of 6.8 or higher. Name the actor, the movie, and the rating.",
		},
		{
			Name:  "3_Temporal_Intersection",
			Query: "Find me all people who acted in a movie that was released in the exact same year as a movie directed by Steven Spielberg. Name the actor, the movie they acted in, and the Spielberg movie that shares the release year.",
		},
		{
			Name:  "4_Multi_Hop_Vector",
			Query: "Use semantic search to find a movie about space travel, and then tell me what year it was released and what rating it got.",
		},
		{
			Name:  "5_Complex_Aggregation",
			Query: "What is the highest rated movie in the database from 1993, and who acted in it?",
		},
	}

	fmt.Println("======================================================")
	fmt.Println("🚀 STARTING EMERGENT GRAPH AGENT BENCHMARK SUITE")
	fmt.Println("======================================================\n")

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
		
		// Small breather between queries to not get rate limited by Google
		time.Sleep(2 * time.Second)
	}

	fmt.Println("======================================================")
	fmt.Println("📊 BENCHMARK RESULTS SUMMARY")
	fmt.Println("======================================================")
	fmt.Printf("Total Execution Time: %v\n\n", totalTime)
	
	for _, r := range results {
		icon := "✅"
		if !r.Success {
			icon := "❌"
			_ = icon // Avoid unused variable if not compiling, wait icon is already assigned
		}
		fmt.Printf("%s %-30s | Time: %-10v | Tools: %d\n", icon, r.Name, r.Time, r.ToolCalls)
	}
}

func runQuery(query string) BenchmarkResult {
	reqBody := map[string]any{
		"message":           query,
		"agentDefinitionId": "70356e5f-2c97-4ce4-9754-ec14e15a2a13",
	}
	bodyBytes, _ := json.Marshal(reqBody)

	req, _ := http.NewRequest("POST", "http://mcj-emergent:3002/api/chat/stream", bytes.NewBuffer(bodyBytes)) // Updated Server URL
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-API-Key", "emt_ec70233facfa29385abfef9bff015df72f08f7205be51f3034b42bf1484d0ec1") // Updated Token
	req.Header.Set("X-Project-ID", "956e3e88-07c5-462b-9076-50ea7e1e7951") // Updated Project

	client := &http.Client{Timeout: 120 * time.Second}
	resp, err := client.Do(req)
	
	res := BenchmarkResult{
		Success: false,
	}

	if err != nil || resp.StatusCode != 200 {
		return res
	}
	defer resp.Body.Close()

	body, _ := ioutil.ReadAll(resp.Body)
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
