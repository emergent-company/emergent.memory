package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"strings"

	"github.com/emergent-company/emergent.memory/apps/connector.linux/internal/account"
	"github.com/emergent-company/emergent.memory/apps/connector.linux/internal/config"
	"github.com/emergent-company/emergent.memory/apps/connector.linux/internal/project"
)

// projectsSchemaVersion is the schema version of the `projects --json` docs.
const projectsSchemaVersion = 1

// projectsNotSignedIn is the shared "sign in first" hint.
const projectsNotSignedIn = "not signed in — run 'memory-connector auth login' first"

// newProjectManager builds the project manager for an account base dir. It is a
// variable so tests can inject offline API dependencies.
var newProjectManager = func(baseDir string) *project.Manager {
	return project.NewManager(baseDir)
}

// runProjects dispatches the `projects` subcommands.
func runProjects(args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		_, _ = fmt.Fprintln(stderr, "memory-connector projects: missing subcommand (list, use, or current)")
		return 2
	}
	switch args[0] {
	case "list":
		return runProjectsList(args[1:], stdout, stderr)
	case "use":
		return runProjectsUse(args[1:], stdout, stderr)
	case "current":
		return runProjectsCurrent(args[1:], stdout, stderr)
	default:
		_, _ = fmt.Fprintf(stderr, "memory-connector projects: unknown subcommand %q\n", args[0])
		return 2
	}
}

type projectFlags struct {
	configPath string
	server     string
	json       bool
}

// parseProjectFlags parses the shared --config/--server (and optionally --json)
// flags and returns any positional arguments. It accepts flags before or after
// positionals (e.g. `projects use p2 --json`).
func parseProjectFlags(name string, args []string, stderr io.Writer, withJSON bool) (projectFlags, []string, int, bool) {
	fs := flag.NewFlagSet("projects "+name, flag.ContinueOnError)
	fs.SetOutput(stderr)
	configPath := fs.String("config", config.DefaultConfigPath(), "config file path")
	server := fs.String("server", "", "Memory server URL (defaults to config server_url or the active account)")
	var jsonOut *bool
	if withJSON {
		jsonOut = fs.Bool("json", false, "emit machine-readable JSON")
	}
	var positionals []string
	remaining := args
	for {
		if err := fs.Parse(remaining); err != nil {
			return projectFlags{}, nil, 2, false
		}
		rest := fs.Args()
		if len(rest) == 0 {
			break
		}
		positionals = append(positionals, rest[0])
		remaining = rest[1:]
	}
	f := projectFlags{configPath: *configPath, server: *server}
	if jsonOut != nil {
		f.json = *jsonOut
	}
	return f, positionals, 0, true
}

// projectUseFlags extends the shared project flags with the engine-config
// overrides accepted by `projects use`.
type projectUseFlags struct {
	projectFlags
	// instanceID is non-nil only when --instance-id was supplied; nil
	// preserves the existing config's instance id.
	instanceID *string
	// disabledTools is non-nil only when --disabled-tools was supplied; a
	// non-nil empty slice clears the list and nil preserves it.
	disabledTools *[]string
}

// parseProjectUseFlags is parseProjectFlags plus the `use`-only
// --instance-id/--disabled-tools overrides. It accepts flags before or after
// positionals.
func parseProjectUseFlags(args []string, stderr io.Writer) (projectUseFlags, []string, int, bool) {
	fs := flag.NewFlagSet("projects use", flag.ContinueOnError)
	fs.SetOutput(stderr)
	configPath := fs.String("config", config.DefaultConfigPath(), "config file path")
	server := fs.String("server", "", "Memory server URL (defaults to config server_url or the active account)")
	jsonOut := fs.Bool("json", false, "emit machine-readable JSON")
	instanceID := fs.String("instance-id", "", "instance id to write into the config (defaults to preserving the existing value)")
	disabledTools := fs.String("disabled-tools", "", "comma-separated tool names to disable (empty string clears; omit to preserve)")
	var positionals []string
	remaining := args
	for {
		if err := fs.Parse(remaining); err != nil {
			return projectUseFlags{}, nil, 2, false
		}
		rest := fs.Args()
		if len(rest) == 0 {
			break
		}
		positionals = append(positionals, rest[0])
		remaining = rest[1:]
	}

	f := projectUseFlags{
		projectFlags: projectFlags{configPath: *configPath, server: *server, json: *jsonOut},
	}
	set := map[string]bool{}
	fs.Visit(func(fl *flag.Flag) { set[fl.Name] = true })
	if set["instance-id"] {
		f.instanceID = instanceID
	}
	if set["disabled-tools"] {
		list := splitDisabledTools(*disabledTools)
		f.disabledTools = &list
	}
	return f, positionals, 0, true
}

// splitDisabledTools parses a comma-separated disabled-tools list, trimming
// whitespace and dropping empty entries. An empty input yields a non-nil empty
// slice, which clears the list.
func splitDisabledTools(s string) []string {
	list := []string{}
	for _, part := range strings.Split(s, ",") {
		if part = strings.TrimSpace(part); part != "" {
			list = append(list, part)
		}
	}
	return list
}

// resolveProjectServer resolves --server, then config server_url, then the
// active signed-in account.
func resolveProjectServer(explicit, configPath string) (string, error) {
	serverURL, err := resolveAuthServer(explicit, configPath)
	if err != nil || serverURL != "" {
		return serverURL, err
	}
	if active, ok := account.NewManager(account.BaseDirForConfig(configPath)).Active(); ok {
		return active, nil
	}
	return "", nil
}

func projectsSignInError(name string, stderr io.Writer) int {
	_, _ = fmt.Fprintf(stderr, "memory-connector projects %s: %s\n", name, projectsNotSignedIn)
	return 1
}

// projectItem is one entry in the `projects list --json` doc.
type projectItem struct {
	ID     string `json:"id"`
	Name   string `json:"name"`
	OrgID  string `json:"org_id,omitempty"`
	Active bool   `json:"active"`
}

type projectsListDoc struct {
	SchemaVersion int           `json:"schema_version"`
	Server        string        `json:"server"`
	Projects      []projectItem `json:"projects"`
}

type projectRef struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

type projectUseDoc struct {
	SchemaVersion int        `json:"schema_version"`
	Server        string     `json:"server"`
	Project       projectRef `json:"project"`
	Config        string     `json:"config"`
}

type projectCurrentDoc struct {
	SchemaVersion int         `json:"schema_version"`
	Server        string      `json:"server"`
	Project       *projectRef `json:"project,omitempty"`
}

func runProjectsList(args []string, stdout, stderr io.Writer) int {
	f, rest, code, ok := parseProjectFlags("list", args, stderr, true)
	if !ok {
		return code
	}
	if len(rest) > 0 {
		_, _ = fmt.Fprintf(stderr, "memory-connector projects list: unexpected argument %q\n", rest[0])
		return 2
	}
	serverURL, err := resolveProjectServer(f.server, f.configPath)
	if err != nil {
		_, _ = fmt.Fprintf(stderr, "memory-connector projects list: %v\n", err)
		return 1
	}
	if serverURL == "" {
		return projectsSignInError("list", stderr)
	}

	m := newProjectManager(account.BaseDirForConfig(f.configPath))
	projects, err := m.List(context.Background(), serverURL)
	if err != nil {
		if errors.Is(err, project.ErrNotSignedIn) {
			return projectsSignInError("list", stderr)
		}
		_, _ = fmt.Fprintf(stderr, "memory-connector projects list: %v\n", err)
		return 1
	}
	activeID := ""
	if active, ok := m.Active(serverURL); ok {
		activeID = active.ID
	}

	if f.json {
		doc := projectsListDoc{SchemaVersion: projectsSchemaVersion, Server: serverURL, Projects: make([]projectItem, 0, len(projects))}
		for _, p := range projects {
			doc.Projects = append(doc.Projects, projectItem{ID: p.ID, Name: p.Name, OrgID: p.OrgID, Active: p.ID == activeID})
		}
		b, err := json.MarshalIndent(doc, "", "  ")
		if err != nil {
			_, _ = fmt.Fprintf(stderr, "memory-connector projects list: %v\n", err)
			return 1
		}
		_, _ = stdout.Write(append(b, '\n'))
		return 0
	}

	if len(projects) == 0 {
		_, _ = fmt.Fprintln(stdout, "no projects found")
		return 0
	}
	for _, p := range projects {
		marker := "  "
		if p.ID == activeID {
			marker = "* "
		}
		_, _ = fmt.Fprintf(stdout, "%s%s  %s\n", marker, p.ID, p.Name)
	}
	return 0
}

func runProjectsUse(args []string, stdout, stderr io.Writer) int {
	f, rest, code, ok := parseProjectUseFlags(args, stderr)
	if !ok {
		return code
	}
	if len(rest) != 1 {
		_, _ = fmt.Fprintln(stderr, "memory-connector projects use: expected exactly one <id-or-name>")
		return 2
	}
	target := rest[0]

	serverURL, err := resolveProjectServer(f.server, f.configPath)
	if err != nil {
		_, _ = fmt.Fprintf(stderr, "memory-connector projects use: %v\n", err)
		return 1
	}
	if serverURL == "" {
		return projectsSignInError("use", stderr)
	}

	ctx := context.Background()
	m := newProjectManager(account.BaseDirForConfig(f.configPath))
	projects, err := m.List(ctx, serverURL)
	if err != nil {
		if errors.Is(err, project.ErrNotSignedIn) {
			return projectsSignInError("use", stderr)
		}
		_, _ = fmt.Fprintf(stderr, "memory-connector projects use: %v\n", err)
		return 1
	}
	chosen := project.Project{}
	for _, p := range projects {
		if p.ID == target || p.Name == target {
			chosen = p
			break
		}
	}
	if chosen.ID == "" {
		_, _ = fmt.Fprintf(stderr, "memory-connector projects use: no project matching %q\n", target)
		return 1
	}

	if err := m.SetActive(serverURL, chosen.ID, chosen.Name); err != nil {
		_, _ = fmt.Fprintf(stderr, "memory-connector projects use: %v\n", err)
		return 1
	}
	token, err := m.EnsureToken(ctx, serverURL, chosen.ID)
	if err != nil {
		_, _ = fmt.Fprintf(stderr, "memory-connector projects use: %v\n", err)
		return 1
	}
	opts := config.MaterializeOptions{InstanceID: f.instanceID, DisabledTools: f.disabledTools}
	if err := config.MaterializeWithOptions(f.configPath, serverURL, token, chosen.ID, opts); err != nil {
		_, _ = fmt.Fprintf(stderr, "memory-connector projects use: %v\n", err)
		return 1
	}

	if f.json {
		doc := projectUseDoc{
			SchemaVersion: projectsSchemaVersion,
			Server:        serverURL,
			Project:       projectRef{ID: chosen.ID, Name: chosen.Name},
			Config:        f.configPath,
		}
		b, err := json.MarshalIndent(doc, "", "  ")
		if err != nil {
			_, _ = fmt.Fprintf(stderr, "memory-connector projects use: %v\n", err)
			return 1
		}
		_, _ = stdout.Write(append(b, '\n'))
		return 0
	}

	_, _ = fmt.Fprintf(stdout, "memory-connector projects use: active project %s (%s)\n", chosen.Name, chosen.ID)
	_, _ = fmt.Fprintf(stdout, "memory-connector projects: wrote config %s\n", f.configPath)
	return 0
}

func runProjectsCurrent(args []string, stdout, stderr io.Writer) int {
	f, rest, code, ok := parseProjectFlags("current", args, stderr, true)
	if !ok {
		return code
	}
	if len(rest) > 0 {
		_, _ = fmt.Fprintf(stderr, "memory-connector projects current: unexpected argument %q\n", rest[0])
		return 2
	}
	serverURL, err := resolveProjectServer(f.server, f.configPath)
	if err != nil {
		_, _ = fmt.Fprintf(stderr, "memory-connector projects current: %v\n", err)
		return 1
	}
	if serverURL == "" {
		return projectsSignInError("current", stderr)
	}

	m := newProjectManager(account.BaseDirForConfig(f.configPath))
	active, ok := m.Active(serverURL)

	if f.json {
		doc := projectCurrentDoc{SchemaVersion: projectsSchemaVersion, Server: serverURL}
		if ok {
			doc.Project = &projectRef{ID: active.ID, Name: active.Name}
		}
		b, err := json.MarshalIndent(doc, "", "  ")
		if err != nil {
			_, _ = fmt.Fprintf(stderr, "memory-connector projects current: %v\n", err)
			return 1
		}
		_, _ = stdout.Write(append(b, '\n'))
		return 0
	}

	if !ok {
		_, _ = fmt.Fprintln(stdout, "no active project")
		return 0
	}
	_, _ = fmt.Fprintf(stdout, "active project: %s (%s)\n", active.Name, active.ID)
	return 0
}
