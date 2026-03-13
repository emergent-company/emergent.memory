//go:build ignore

// gen is a go generate helper that regenerates the embedded memory-cli-reference
// SKILL.md in-process, without requiring a pre-built binary.
//
// Invoked automatically via:
//
//	go generate ./cmd/...
//
// Or manually:
//
//	go run ./cmd/gen
package main

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"

	"github.com/emergent-company/emergent.memory/tools/cli/internal/cmd"
)

func main() {
	// Resolve output path relative to this file's location so the generator
	// works regardless of the working directory it is called from.
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		fmt.Fprintln(os.Stderr, "gen: could not determine source file path")
		os.Exit(1)
	}
	// file = .../tools/cli/cmd/gen/main.go
	// skillsfs dir = .../tools/cli/internal/skillsfs/skills/memory-cli-reference/SKILL.md
	root := filepath.Join(filepath.Dir(file), "..", "..", "internal", "skillsfs", "skills", "memory-cli-reference", "SKILL.md")
	outFile, err := filepath.Abs(root)
	if err != nil {
		fmt.Fprintf(os.Stderr, "gen: resolve path: %v\n", err)
		os.Exit(1)
	}

	if err := cmd.GenerateDocs(outFile); err != nil {
		fmt.Fprintf(os.Stderr, "gen: %v\n", err)
		os.Exit(1)
	}
}
