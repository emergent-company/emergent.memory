// Command analyze runs the experiment analyzer against a named experiment,
// a specific test name, or a single run ID, and prints improvement suggestions.
//
// Usage:
//
//	analyze [--json] <experiment>
//	analyze [--json] --test <testName>
//	analyze [--json] --run <runID>
//
// Flags:
//
//	--json   Output raw JSON array of SuggestionRow instead of a table.
//	--test   Analyse all runs for a specific test name.
//	--run    Analyse a single run by its database ID.
//
// Environment:
//
//	GOOGLE_AI_API_KEY   Required. Gemini API key.
//	ANALYZER_MODEL      Optional. Model name (default: gemini-2.0-flash).
//	MEMORY_TEST_ENV     Optional. Named .env overlay (e.g. "mcj-emergent").
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"strings"
	"time"

	framework "github.com/emergent-company/runlog"
)

func main() {
	jsonFlag := flag.Bool("json", false, "Output JSON array instead of a table")
	verboseFlag := flag.Bool("v", false, "Verbose: print agent conversation trace to stderr")
	testFlag := flag.String("test", "", "Analyse all runs for a specific test name")
	runFlag := flag.Int64("run", 0, "Analyse a single run by its database ID")
	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, "Usage: analyze [--json] [-v] [--test <testName>] [--run <runID>] [<experiment>]\n\n")
		fmt.Fprintf(os.Stderr, "Runs the LLM analyzer and prints improvement suggestions.\n\n")
		fmt.Fprintf(os.Stderr, "Modes:\n")
		fmt.Fprintf(os.Stderr, "  analyze <experiment>         Analyse all runs in an experiment\n")
		fmt.Fprintf(os.Stderr, "  analyze --test <testName>    Analyse all runs for a test name\n")
		fmt.Fprintf(os.Stderr, "  analyze --run <runID>        Analyse a single run by ID\n\n")
		fmt.Fprintf(os.Stderr, "Flags:\n")
		flag.PrintDefaults()
	}
	flag.Parse()

	// Determine mode: --run, --test, or positional experiment.
	mode := "experiment"
	if *runFlag != 0 {
		mode = "run"
	} else if *testFlag != "" {
		mode = "test"
	}

	if mode == "experiment" {
		args := flag.Args()
		if len(args) < 1 {
			flag.Usage()
			os.Exit(1)
		}
	}

	// Load .env / named overlay so GOOGLE_AI_API_KEY et al. are available.
	wd, _ := os.Getwd()
	if wd != "" {
		framework.LoadDotEnvFrom(wd)
	}

	// Open the shared RunDB.
	db, err := framework.SharedDB()
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: open database: %v\n", err)
		os.Exit(1)
	}

	// Build the analyzer (validates API key, creates Gemini model + ADK agent).
	analyzer, err := framework.NewAnalyzer(db)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: create analyzer: %v\n", err)
		os.Exit(1)
	}

	// When verbose, print the full agent conversation trace to stderr.
	// Otherwise, still print errors so failures are never silent.
	if *verboseFlag {
		analyzer.OnEvent = func(ev framework.AnalyzerEvent) {
			printAnalyzerEvent(ev)
		}
	} else {
		analyzer.OnEvent = func(ev framework.AnalyzerEvent) {
			if ev.Kind == framework.AEError {
				printAnalyzerEvent(ev)
			}
		}
	}

	// Run with a 5-minute timeout.
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	var suggestions []framework.SuggestionRow
	switch mode {
	case "run":
		fmt.Fprintf(os.Stderr, "Analysing run %d…\n", *runFlag)
		suggestions, err = analyzer.RunByRunID(ctx, *runFlag)
	case "test":
		fmt.Fprintf(os.Stderr, "Analysing test %q…\n", *testFlag)
		suggestions, err = analyzer.RunByTestName(ctx, *testFlag)
	default:
		experiment := flag.Args()[0]
		fmt.Fprintf(os.Stderr, "Analysing experiment %q…\n", experiment)
		suggestions, err = analyzer.Run(ctx, experiment)
	}
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: run analyzer: %v\n", err)
		os.Exit(1)
	}

	if len(suggestions) == 0 {
		fmt.Println("no suggestions generated")
		os.Exit(0)
	}

	if *jsonFlag {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		if err := enc.Encode(suggestions); err != nil {
			fmt.Fprintf(os.Stderr, "error: encode JSON: %v\n", err)
			os.Exit(1)
		}
		os.Exit(0)
	}

	// Human-readable table: priority | category | title
	printTable(suggestions)
}

// printTable renders suggestions as a simple aligned table.
func printTable(suggestions []framework.SuggestionRow) {
	const (
		colPriority = 8
		colCategory = 16
	)

	header := fmt.Sprintf("%-*s  %-*s  %s", colPriority, "PRIORITY", colCategory, "CATEGORY", "TITLE")
	sep := strings.Repeat("-", len(header)+20)
	fmt.Println(sep)
	fmt.Println(header)
	fmt.Println(sep)

	for _, s := range suggestions {
		prio := strings.ToUpper(s.Priority)
		cat := s.Category
		if len(cat) > colCategory {
			cat = cat[:colCategory-1] + "…"
		}
		title := s.Title
		fmt.Printf("%-*s  %-*s  %s\n", colPriority, prio, colCategory, cat, title)
	}
	fmt.Println(sep)
	fmt.Printf("%d suggestion(s)\n", len(suggestions))

	// Print details for each suggestion.
	fmt.Println()
	for i, s := range suggestions {
		fmt.Printf("--- %d. [%s] %s ---\n", i+1, strings.ToUpper(s.Priority), s.Title)
		fmt.Printf("Category:     %s\n", s.Category)
		fmt.Printf("Generated at: %s\n", s.GeneratedAt.UTC().Format("2006-01-02 15:04:05 UTC"))
		if len(s.RunIDs) > 0 {
			ids := make([]string, len(s.RunIDs))
			for j, id := range s.RunIDs {
				ids[j] = fmt.Sprintf("%d", id)
			}
			fmt.Printf("Run IDs:      %s\n", strings.Join(ids, ", "))
		}
		fmt.Printf("\n%s\n\n", s.Body)
	}
}

// printAnalyzerEvent renders an AnalyzerEvent to stderr for verbose tracing.
func printAnalyzerEvent(ev framework.AnalyzerEvent) {
	prefix := ""
	if ev.Author != "" {
		prefix = fmt.Sprintf("[%s] ", ev.Author)
	}

	switch ev.Kind {
	case framework.AEThought:
		fmt.Fprintf(os.Stderr, "%s💭 %s\n", prefix, truncateStr(ev.Content, 200))
	case framework.AEText:
		fmt.Fprintf(os.Stderr, "%s📝 %s\n", prefix, ev.Content)
	case framework.AEToolCall:
		fmt.Fprintf(os.Stderr, "%s🔧 %s\n", prefix, ev.Content)
	case framework.AEToolResult:
		fmt.Fprintf(os.Stderr, "%s📨 %s\n", prefix, ev.Content)
	case framework.AETokenUsage:
		fmt.Fprintf(os.Stderr, "%s📊 %s\n", prefix, ev.Content)
	case framework.AEError:
		fmt.Fprintf(os.Stderr, "%s❌ %s\n", prefix, ev.Content)
	case framework.AETurnComplete:
		fmt.Fprintf(os.Stderr, "%s--- turn complete ---\n", prefix)
	}
}

// truncateStr truncates s to maxLen characters, appending "..." if truncated.
func truncateStr(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen-3] + "..."
}
