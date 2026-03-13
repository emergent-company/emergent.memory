package cmd

import (
	"bufio"
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/spf13/cobra"
	"github.com/spf13/cobra/doc"
)

var genDocsCmd = &cobra.Command{
	Use:   "gen-docs",
	Short: "Generate CLI reference documentation",
	Long: `Generate full CLI reference as a single clean Markdown file.

Each command and subcommand becomes a section. Global flags and cross-reference
links are stripped to keep the output compact. Suitable for embedding in
AI agent skills or documentation sites.`,
	Hidden: true,
	RunE:   runGenDocs,
}

var genDocsOutput string

// reGlobalFlags matches the "### Options inherited from parent commands" block.
var reGlobalFlags = regexp.MustCompile("(?s)### Options inherited from parent commands\n+```.*?```\n")

// reAutoGen matches the cobra auto-generated footer line.
var reAutoGen = regexp.MustCompile(`(?m)^###### Auto generated.*\n`)

// reSeealsoBlock matches a "### SEE ALSO" section and all its bullet lines.
var reSeealsoBlock = regexp.MustCompile(`(?m)^### SEE ALSO\n(?:(?:\*.*\n)|(?:\n))*`)

// reSeealsoOrphan matches orphan bullet lines that link to .md files (SEE ALSO
// leftovers after the header has been stripped).
var reSeealsoOrphan = regexp.MustCompile(`(?m)^\* \[.*\.md\].*\n`)

// reMultipleBlankLines collapses 3+ consecutive newlines to 2.
var reMultipleBlankLines = regexp.MustCompile(`\n{3,}`)

func cleanMarkdown(raw []byte) []byte {
	s := raw
	s = reGlobalFlags.ReplaceAll(s, nil)
	s = reAutoGen.ReplaceAll(s, nil)
	s = reSeealsoBlock.ReplaceAll(s, nil)
	s = reSeealsoOrphan.ReplaceAll(s, nil)
	s = reMultipleBlankLines.ReplaceAll(s, []byte("\n\n"))
	return bytes.TrimRight(s, "\n")
}

// cliReferenceSkillFrontmatter is the YAML front matter prepended to the generated SKILL.md.
const cliReferenceSkillFrontmatter = `---
name: memory-cli-reference
description: Full Memory CLI command reference with all subcommands and flags. Use when you need exact command syntax, flag names, or usage examples for any ` + "`" + `memory` + "`" + ` CLI command.
metadata:
  author: emergent
  version: "1.0"
---

This skill contains the complete ` + "`" + `memory` + "`" + ` CLI command reference, auto-generated from the binary.

Use this when you need to look up:
- Exact subcommand names (e.g. ` + "`" + `memory agents get-run` + "`" + `, ` + "`" + `memory provider configure-project` + "`" + `)
- Available flags and their types for any command
- Usage examples embedded in the help text
- Which subcommands exist under a parent command

`

// GenerateDocs generates the memory-cli-reference SKILL.md at outFile.
// It runs in-process (no binary required) and is called by go generate.
func GenerateDocs(outFile string) error {
	dir, err := os.MkdirTemp("", "memory-docs-*")
	if err != nil {
		return fmt.Errorf("create temp dir: %w", err)
	}
	defer os.RemoveAll(dir)

	// Disable PersistentPreRunE — it tries to connect to the server.
	rootCmd.PersistentPreRunE = nil

	if err := doc.GenMarkdownTree(rootCmd, dir); err != nil {
		return fmt.Errorf("generate docs: %w", err)
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		return fmt.Errorf("read temp dir: %w", err)
	}

	if err := os.MkdirAll(filepath.Dir(outFile), 0o755); err != nil {
		return fmt.Errorf("create output dir: %w", err)
	}

	out, err := os.Create(outFile)
	if err != nil {
		return fmt.Errorf("create output file: %w", err)
	}
	defer out.Close()

	bw := bufio.NewWriter(out)
	fmt.Fprint(bw, cliReferenceSkillFrontmatter)
	fmt.Fprintln(bw, "# Memory CLI Reference")
	fmt.Fprintln(bw)
	fmt.Fprintln(bw, "Full command reference auto-generated from `memory --help`. Each section covers one command or subcommand with its synopsis, usage, and flags.")
	fmt.Fprintln(bw)
	fmt.Fprintln(bw, "---")
	fmt.Fprintln(bw)

	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		raw, err := os.ReadFile(filepath.Join(dir, entry.Name()))
		if err != nil {
			return fmt.Errorf("read %s: %w", entry.Name(), err)
		}
		cleaned := cleanMarkdown(raw)
		if len(strings.TrimSpace(string(cleaned))) == 0 {
			continue
		}
		if _, err := bw.Write(cleaned); err != nil {
			return fmt.Errorf("write output: %w", err)
		}
		fmt.Fprintln(bw)
		fmt.Fprintln(bw)
	}

	if err := bw.Flush(); err != nil {
		return fmt.Errorf("flush output: %w", err)
	}

	abs, _ := filepath.Abs(outFile)
	fmt.Fprintf(os.Stdout, "Generated: %s\n", abs)
	return nil
}

func runGenDocs(cmd *cobra.Command, args []string) error {
	return GenerateDocs(genDocsOutput)
}

func init() {
	genDocsCmd.Flags().StringVarP(&genDocsOutput, "output", "o", "cli-reference.md", "Output file path")
	rootCmd.AddCommand(genDocsCmd)
}
