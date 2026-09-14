// Command memory-connector relays a local machine's tools to a Memory project
// over the MCP relay. Subcommands:
//
//	memory-connector init       write config and verify connectivity
//	memory-connector relay      run the relay client (register + serve tools)
//	memory-connector daemon     run the relay client under a single-instance lock
//	memory-connector auth       sign in, sign out, or show account status
//	memory-connector projects   list projects and select the active one
//	memory-connector install    install + start the systemd user unit (Linux)
//	memory-connector uninstall  stop + remove the systemd user unit (Linux)
//	memory-connector status     show local state and hub presence
//	memory-connector upgrade    self-update the binary
//
// With no subcommand it prints the version.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"time"

	"github.com/emergent-company/emergent.memory/apps/connector.linux/internal/config"
)

// version is the connector release version reported to users and the hub. It
// is a variable so release builds can inject the tag via
// -ldflags "-X main.version=<X.Y.Z>"; the default covers `go install`/local
// builds.
var version = "0.1.0"

// probeTimeout bounds the init connectivity probe.
const probeTimeout = 15 * time.Second

func main() {
	root := newRootCommand(os.Stdout, os.Stderr)
	if err := root.Execute(); err != nil {
		var ec exitError
		if errors.As(err, &ec) {
			os.Exit(ec.code)
		}
		// Anything else (flag/positional misuse, unknown command) is a usage
		// error: print it ourselves and exit 2. Cobra's own printing is
		// silenced so runX messages are never duplicated.
		_, _ = fmt.Fprintf(os.Stderr, "memory-connector: %v\n", err)
		os.Exit(2)
	}
}

// runInit captures connection settings from flags, writes the config file
// (mode 0600), then probes connectivity with GET /api/mcp-relay/sessions.
// On probe failure the just-written file is removed so no invalid config
// persists. Returns the process exit code.
func runInit(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("init", flag.ContinueOnError)
	fs.SetOutput(stderr)
	serverURL := fs.String("server-url", "", "Memory server base URL (required)")
	token := fs.String("token", "", "project-scoped bearer token (required)")
	projectID := fs.String("project-id", "", "project id (optional when the token is project-scoped)")
	instanceID := fs.String("instance-id", "", fmt.Sprintf("instance id (default %q)", config.DefaultInstanceID()))
	configPath := fs.String("config", config.DefaultConfigPath(), "config file path")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if fs.NArg() > 0 {
		_, _ = fmt.Fprintf(stderr, "memory-connector init: unexpected argument %q\n", fs.Arg(0))
		return 2
	}
	if *serverURL == "" || *token == "" {
		_, _ = fmt.Fprintln(stderr, "memory-connector init: --server-url and --token are required")
		fs.Usage()
		return 2
	}

	cfg := &config.Config{
		ServerURL:  *serverURL,
		Token:      *token,
		ProjectID:  *projectID,
		InstanceID: *instanceID,
	}
	if cfg.InstanceID == "" {
		cfg.InstanceID = config.DefaultInstanceID()
	}

	if err := cfg.Save(*configPath); err != nil {
		_, _ = fmt.Fprintf(stderr, "memory-connector init: %v\n", err)
		return 1
	}

	ctx, cancel := context.WithTimeout(context.Background(), probeTimeout)
	defer cancel()
	probeErr := config.ProbeConnectivity(ctx, &http.Client{Timeout: probeTimeout}, cfg.ServerURL, cfg.Token)
	if probeErr != nil {
		if rmErr := os.Remove(*configPath); rmErr != nil && !errors.Is(rmErr, os.ErrNotExist) {
			_, _ = fmt.Fprintf(stderr, "memory-connector init: removing config: %v\n", rmErr)
		}
		if errors.Is(probeErr, config.ErrAuthFailed) {
			_, _ = fmt.Fprintln(stderr, "memory-connector init: authentication failed: the server rejected the token (wrong or expired token?)")
		} else {
			_, _ = fmt.Fprintf(stderr, "memory-connector init: %v\n", probeErr)
		}
		return 1
	}

	_, _ = fmt.Fprintf(stdout, "memory-connector init: configuration written to %s and connectivity verified — the connector is ready to relay\n", *configPath)
	return 0
}
