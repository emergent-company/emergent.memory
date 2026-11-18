package main

import (
	"bytes"
	"io"
	"regexp"
	"sort"
	"strings"
	"testing"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

// This file guards the seam between the cobra entry point (`main.go` builds it
// via newRootCommand) and the stdlib `flag.FlagSet` each runX implementation
// parses. Every runX reads argv with its own FlagSet, and cobra only hands it
// the flags the user actually set (`stdlibArgs`), so a flag the cobra leaf does
// NOT register is rejected by cobra with "unknown flag" before runX ever runs.
//
// That is exactly how `--prompt` / `--login-hint` shipped broken once: they
// existed in runAuthStart's FlagSet (and the unit test called runAuthStart
// directly), but the real binary's `auth start` leaf had not registered them, so
// every silent re-auth died with exit 2 and a lifecycle log line. The parity
// tests below compare the two surfaces for every leaf.

// flagHelpLine matches one flag line of the stdlib's default usage output
// ("  -name type\n    \tdescription").
var flagHelpLine = regexp.MustCompile(`(?m)^  -([A-Za-z0-9][A-Za-z0-9_-]*)`)

// allCommands flattens a command tree, depth first.
func allCommands(cmd *cobra.Command) []*cobra.Command {
	out := []*cobra.Command{cmd}
	for _, child := range cmd.Commands() {
		out = append(out, allCommands(child)...)
	}
	return out
}

// stdlibFlagNames returns the flags a runX's own flag.FlagSet registers. Running
// with --help makes the stdlib print its defaults and return the usage exit code
// before any work happens, so this reads the real FlagSet rather than a copy.
func stdlibFlagNames(t *testing.T, run func([]string, io.Writer, io.Writer) int) []string {
	t.Helper()
	var stderr bytes.Buffer
	if code := run([]string{"--help"}, io.Discard, &stderr); code != 2 {
		t.Fatalf("--help exit code = %d, want 2 (usage); output:\n%s", code, stderr.String())
	}
	matches := flagHelpLine.FindAllStringSubmatch(stderr.String(), -1)
	if len(matches) == 0 {
		t.Fatalf("no flags found in --help output:\n%s", stderr.String())
	}
	names := make([]string, 0, len(matches))
	for _, m := range matches {
		names = append(names, m[1])
	}
	sort.Strings(names)
	return names
}

// TestLeafFlagsCoverStdlibFlags proves every flag a leaf's run function reads is
// reachable through the cobra entry point — the direction that shipped broken.
func TestLeafFlagsCoverStdlibFlags(t *testing.T) {
	root := newRootCommand(io.Discard, io.Discard)
	checked := 0
	for _, cmd := range allCommands(root) {
		run, ok := leafRunners[cmd]
		if !ok {
			continue
		}
		checked++
		cobraFlags := map[string]bool{}
		cmd.Flags().VisitAll(func(f *pflag.Flag) { cobraFlags[f.Name] = true })

		var missing []string
		for _, name := range stdlibFlagNames(t, run) {
			if !cobraFlags[name] {
				missing = append(missing, "--"+name)
			}
		}
		if len(missing) > 0 {
			t.Errorf("%s: run function reads flags the cobra leaf does not register: %s\n"+
				"(the real binary rejects these before the run function sees them)",
				cmd.CommandPath(), strings.Join(missing, ", "))
		}
	}
	if checked == 0 {
		t.Fatal("no leaf commands found; leaf() must register its run function")
	}
}

// TestLeafFlagsAreReadByStdlib is the other direction: a flag that cobra
// advertises but the run function never parses is a dead flag (completion offers
// it, and `stdlibArgs` would drop it silently).
func TestLeafFlagsAreReadByStdlib(t *testing.T) {
	root := newRootCommand(io.Discard, io.Discard)
	for _, cmd := range allCommands(root) {
		run, ok := leafRunners[cmd]
		if !ok {
			continue
		}
		read := map[string]bool{}
		for _, name := range stdlibFlagNames(t, run) {
			read[name] = true
		}
		var dead []string
		cmd.Flags().VisitAll(func(f *pflag.Flag) {
			// cobra injects --help at Execute time; it is not a run-function flag.
			if f.Name == "help" {
				return
			}
			if !read[f.Name] {
				dead = append(dead, "--"+f.Name)
			}
		})
		if len(dead) > 0 {
			t.Errorf("%s: cobra registers flags the run function never parses: %s",
				cmd.CommandPath(), strings.Join(dead, ", "))
		}
	}
}
