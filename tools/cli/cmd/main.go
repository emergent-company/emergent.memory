package main

import (
	"fmt"
	"os"

	"github.com/emergent-company/emergent.memory/tools/cli/internal/cmd"
)

func main() {
	if err := cmd.Execute(); err != nil {
		if cmd.IsAuthError(err) {
			cmd.PrintAuthError()
		} else {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		}
		os.Exit(1)
	}
}
