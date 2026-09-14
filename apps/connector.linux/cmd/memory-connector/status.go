package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/emergent-company/memory.web-ui/connector/internal/config"
	"github.com/emergent-company/memory.web-ui/connector/internal/status"
	"github.com/emergent-company/memory.web-ui/connector/internal/toolreg"
	"github.com/emergent-company/memory.web-ui/connector/internal/tools"
)

// statusTimeout bounds the hub sessions check.
const statusTimeout = 10 * time.Second

// statusToolsProvider selects the effective tool set for a config. It is a
// variable so tests can inject a deterministic provider on any OS.
var statusToolsProvider = tools.Provider

// runStatus prints local state and a best-effort hub presence/parity check
// against GET /api/mcp-relay/sessions. It never opens a WebSocket: the hub
// session listing is the connection evidence. Exits 0 even when the instance
// is not connected; non-zero only for config load or usage errors.
func runStatus(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("status", flag.ContinueOnError)
	fs.SetOutput(stderr)
	configPath := fs.String("config", config.DefaultConfigPath(), "config file path")
	jsonOut := fs.Bool("json", false, "emit machine-readable JSON")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if fs.NArg() > 0 {
		_, _ = fmt.Fprintf(stderr, "memory-connector status: unexpected argument %q\n", fs.Arg(0))
		return 2
	}

	cfg, err := config.Load(*configPath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			_, _ = fmt.Fprintf(stderr, "memory-connector status: no config at %s — run 'memory-connector init' first\n", *configPath)
		} else {
			_, _ = fmt.Fprintf(stderr, "memory-connector status: %v\n", err)
		}
		return 1
	}

	set, err := statusToolsProvider(cfg)
	if err != nil {
		_, _ = fmt.Fprintf(stderr, "memory-connector status: %v\n", err)
		return 1
	}
	// Same filter the relay wiring applies: disabled tools are neither
	// registered with the hub nor reported as effective tools.
	tools := toolreg.FilterDisabled(set.Tools, cfg.DisabledTools)
	names := make([]string, 0, len(tools))
	for _, tool := range tools {
		names = append(names, tool.Name)
	}

	ctx, cancel := context.WithTimeout(context.Background(), statusTimeout)
	defer cancel()
	state, detail := hubState(ctx, cfg, len(tools))

	doc := status.Document{
		SchemaVersion: status.SchemaVersion,
		InstanceID:    cfg.InstanceID,
		Version:       version,
		Tools:         names,
		ToolsNote:     set.Note,
		HubState:      state,
		HubDetail:     detail,
		DisabledTools: disabledReport(set.Disabled),
	}
	// JSON-only field: the human text rendering ignores Project, so setting it
	// here keeps the text output byte-identical.
	if cfg.ProjectID != "" {
		doc.Project = &status.Project{ID: cfg.ProjectID}
	}
	if *jsonOut {
		out, err := doc.JSON()
		if err != nil {
			_, _ = fmt.Fprintf(stderr, "memory-connector status: %v\n", err)
			return 1
		}
		_, _ = stdout.Write(out)
		return 0
	}
	_, _ = io.WriteString(stdout, doc.Text())
	return 0
}

// hubState probes the hub's relay sessions and maps the outcome to a stable
// HubState plus the detail needed for the human-readable hub line.
func hubState(ctx context.Context, cfg *config.Config, localToolCount int) (status.HubState, status.HubDetail) {
	sessions, err := config.ListSessions(ctx, &http.Client{Timeout: statusTimeout}, cfg.ServerURL, cfg.Token)
	if err != nil {
		if errors.Is(err, config.ErrAuthFailed) {
			return status.HubStateAuthFailed, status.HubDetail{}
		}
		return status.HubStateUnreachable, status.HubDetail{Error: strings.TrimPrefix(err.Error(), "server unreachable: ")}
	}

	found := false
	hubToolCount, total := 0, 0
	for _, s := range sessions {
		total++
		if s.InstanceID == cfg.InstanceID {
			found = true
			hubToolCount = s.ToolCount
		}
	}
	if !found {
		return status.HubStateNotConnected, status.HubDetail{SessionCount: total}
	}
	return status.HubStateConnected, status.HubDetail{HubToolCount: hubToolCount, LocalToolCount: localToolCount}
}

// disabledReport maps provider-disabled tools into the status document shape.
func disabledReport(disabled []tools.DisabledTool) []status.DisabledTool {
	if len(disabled) == 0 {
		return nil
	}
	out := make([]status.DisabledTool, 0, len(disabled))
	for _, d := range disabled {
		out = append(out, status.DisabledTool{Name: d.Name, Reason: d.Reason})
	}
	return out
}
