package main

import (
	"bytes"
	"errors"
	"path/filepath"
	"strings"
	"testing"
)

// executeRoot builds an isolated command tree writing to buffers and runs it
// with args, returning captured output and the execution error.
func executeRoot(args ...string) (string, string, error) {
	var out, errBuf bytes.Buffer
	root := newRootCommand(&out, &errBuf)
	root.SetArgs(args)
	err := root.Execute()
	return out.String(), errBuf.String(), err
}

func TestRootHelpListsCommands(t *testing.T) {
	out, _, err := executeRoot("--help")
	if err != nil {
		t.Fatalf("--help: %v", err)
	}
	for _, name := range []string{
		"init", "relay", "daemon", "auth", "projects",
		"install", "uninstall", "status", "completion", "help",
	} {
		if !strings.Contains(out, "\n  "+name) {
			t.Errorf("root --help missing command %q:\n%s", name, out)
		}
	}
}

func TestRootVersionFlag(t *testing.T) {
	out, _, err := executeRoot("--version")
	if err != nil {
		t.Fatalf("--version: %v", err)
	}
	want := "memory-connector " + version + "\n"
	if out != want {
		t.Errorf("--version output = %q, want %q", out, want)
	}
}

func TestRootNoArgsPrintsHelp(t *testing.T) {
	out, _, err := executeRoot()
	if err != nil {
		t.Fatalf("no args: %v", err)
	}
	if !strings.Contains(out, "Available Commands:") || !strings.Contains(out, "Usage:") {
		t.Errorf("no-arg output should be the help menu, got:\n%s", out)
	}
}

func TestCompletionBashEmitsScript(t *testing.T) {
	out, _, err := executeRoot("completion", "bash")
	if err != nil {
		t.Fatalf("completion bash: %v", err)
	}
	if len(strings.TrimSpace(out)) == 0 {
		t.Fatal("completion bash emitted empty output")
	}
	if !strings.Contains(out, "bash completion") {
		t.Errorf("completion bash output does not look like a script:\n%s", out)
	}
}

func TestInitHelpShowsFlags(t *testing.T) {
	out, _, err := executeRoot("init", "--help")
	if err != nil {
		t.Fatalf("init --help: %v", err)
	}
	for _, flagName := range []string{"--server-url", "--token", "--project-id", "--instance-id", "--config"} {
		if !strings.Contains(out, flagName) {
			t.Errorf("init --help missing flag %q:\n%s", flagName, out)
		}
	}
}

func TestAuthHelpListsSubcommands(t *testing.T) {
	out, _, err := executeRoot("auth", "--help")
	if err != nil {
		t.Fatalf("auth --help: %v", err)
	}
	for _, name := range []string{
		"login", "logout", "status", "start", "complete",
		"cancel", "access-token", "import", "list",
	} {
		if !strings.Contains(out, name) {
			t.Errorf("auth --help missing subcommand %q:\n%s", name, out)
		}
	}
}

func TestProjectsHelpListsSubcommands(t *testing.T) {
	out, _, err := executeRoot("projects", "--help")
	if err != nil {
		t.Fatalf("projects --help: %v", err)
	}
	for _, name := range []string{"list", "use", "current"} {
		if !strings.Contains(out, name) {
			t.Errorf("projects --help missing subcommand %q:\n%s", name, out)
		}
	}
}

// TestStatusMissingConfigExitCode verifies the flag reconstruction reaches the
// existing runStatus implementation and that its runtime exit code (1) is
// carried back through cobra.
func TestStatusMissingConfigExitCode(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "missing.yml")
	_, errOut, err := executeRoot("status", "--config", missing)
	var ec exitError
	if !errors.As(err, &ec) || ec.code != 1 {
		t.Fatalf("err = %v, want exitError{1}", err)
	}
	if !strings.Contains(errOut, "no config at "+missing) {
		t.Errorf("stderr = %q, want missing-config message", errOut)
	}
}

func TestParentMissingSubcommandIsUsageError(t *testing.T) {
	for _, name := range []string{"auth", "projects"} {
		t.Run(name, func(t *testing.T) {
			_, errOut, err := executeRoot(name)
			var ec exitError
			if !errors.As(err, &ec) || ec.code != 2 {
				t.Fatalf("err = %v, want exitError{2}", err)
			}
			if !strings.Contains(errOut, "missing subcommand") {
				t.Errorf("stderr = %q, want missing-subcommand message", errOut)
			}
		})
	}
}

func TestUnknownCommandIsUsageError(t *testing.T) {
	_, _, err := executeRoot("bogus")
	if err == nil {
		t.Fatal("unknown command: err = nil, want usage error")
	}
	if _, ok := err.(exitError); ok {
		t.Fatalf("unknown command should be a cobra usage error, got %v", err)
	}
}
