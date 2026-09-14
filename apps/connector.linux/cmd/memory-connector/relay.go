package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/emergent-company/emergent.memory/apps/connector.linux/internal/config"
	"github.com/emergent-company/emergent.memory/apps/connector.linux/internal/mgmtapi"
	"github.com/emergent-company/emergent.memory/apps/connector.linux/internal/relay"
	"github.com/emergent-company/emergent.memory/apps/connector.linux/internal/toolreg"
	"github.com/emergent-company/emergent.memory/apps/connector.linux/internal/tools"
)

// runRelay runs the relay client in the foreground. It is a thin wrapper over
// runRelayCommand with locking disabled, keeping relay's behavior unchanged.
func runRelay(args []string, stdout, stderr io.Writer) int {
	return runRelayCommand("relay", args, stdout, stderr, false)
}

// runRelayCommand implements the shared `relay` and `daemon` behavior: parse
// --config, load the config, build the tool registry, and run the relay client
// until SIGINT/SIGTERM. name is used in user-facing messages. When withLock is
// set, an exclusive single-instance lock beside the config file is acquired
// first and released on return; a held lock exits 1.
func runRelayCommand(name string, args []string, stdout, stderr io.Writer, withLock bool) int {
	fs := flag.NewFlagSet(name, flag.ContinueOnError)
	fs.SetOutput(stderr)
	configPath := fs.String("config", config.DefaultConfigPath(), "config file path")
	apiPort := fs.Int("api-port", 0, "loopback management API port (127.0.0.1; 0 disables)")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if fs.NArg() > 0 {
		_, _ = fmt.Fprintf(stderr, "memory-connector %s: unexpected argument %q\n", name, fs.Arg(0))
		return 2
	}
	if *apiPort < 0 || *apiPort > 65535 {
		_, _ = fmt.Fprintf(stderr, "memory-connector relay: invalid --api-port %d\n", *apiPort)
		return 2
	}

	if withLock {
		lockPath := *configPath + ".lock"
		release, err := acquireLock(lockPath)
		if err != nil {
			if errors.Is(err, ErrAlreadyRunning) {
				_, _ = fmt.Fprintf(stderr, "memory-connector %s: already running (lock held: %s)\n", name, lockPath)
			} else {
				_, _ = fmt.Fprintf(stderr, "memory-connector %s: %v\n", name, err)
			}
			return 1
		}
		defer release()
	}

	cfg, err := config.Load(*configPath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			_, _ = fmt.Fprintf(stderr, "memory-connector %s: no config at %s — run 'memory-connector init' first\n", name, *configPath)
		} else {
			_, _ = fmt.Fprintf(stderr, "memory-connector %s: %v\n", name, err)
		}
		return 1
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	log := slog.New(slog.NewTextHandler(stderr, &slog.HandlerOptions{Level: slog.LevelInfo}))

	// Build the built-in (platform/Apple) tool set plus a started mcphost
	// Manager for the hosted local MCP servers, sharing one registry so they
	// are relayed identically. A server that fails to start is logged and
	// skipped; config/duplicate errors abort startup before dialing the hub.
	reg, mgr, note, err := buildRegistry(ctx, cfg, log)
	if err != nil {
		_, _ = fmt.Fprintf(stderr, "memory-connector %s: %v\n", name, err)
		return 1
	}
	reloadable := toolreg.NewReloadable(reg)
	regMgr := &registryManager{registry: reloadable, log: log, manager: mgr}
	defer regMgr.Close()

	registered := reg.List()
	if len(registered) == 0 && note != "" {
		_, _ = fmt.Fprintf(stderr, "memory-connector %s: warning: %s\n", name, note)
	}

	client, err := relay.New(cfg.ServerURL, cfg.Token, cfg.ProjectID, cfg.InstanceID, version, reloadable)
	if err != nil {
		_, _ = fmt.Fprintf(stderr, "memory-connector %s: %v\n", name, err)
		return 1
	}
	regMgr.setClient(client)

	// Optional loopback management API. Bound to 127.0.0.1 only and
	// unauthenticated by design (local-companion trust model); see the
	// mgmtapi package doc for the risk.
	if *apiPort > 0 {
		api := mgmtapi.NewServer(*configPath, cfg, log, regMgr.reload)
		api.SetStatuses(mgr.Status())
		api.SetRuntimeStatus(func() mgmtapi.RuntimeStatus {
			count := 0
			if current := reloadable.Current(); current != nil {
				count = len(current.List())
			}
			return mgmtapi.RuntimeStatus{Connected: client.State().Connected, ToolCount: count}
		})
		addr := mgmtAPIAddr(*apiPort)
		httpServer := &http.Server{
			Addr:              addr,
			Handler:           api.Handler(),
			ReadHeaderTimeout: 5 * time.Second,
		}
		go func() {
			if err := httpServer.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
				_, _ = fmt.Fprintf(stderr, "memory-connector relay: management API: %v\n", err)
			}
		}()
		_, _ = fmt.Fprintf(stdout, "memory-connector relay: management API listening on http://%s\n", addr)
		defer func() {
			shutdownCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			defer cancel()
			_ = httpServer.Shutdown(shutdownCtx)
		}()
	}

	_, _ = fmt.Fprintf(stdout, "memory-connector %s: instance %q connecting to %s (%d tools)\n",
		name, cfg.InstanceID, cfg.ServerURL, len(registered))
	if err := client.Run(ctx); err != nil {
		_, _ = fmt.Fprintf(stderr, "memory-connector %s: %v\n", name, err)
		return 1
	}
	_, _ = fmt.Fprintf(stdout, "memory-connector %s: stopped\n", name)
	return 0
}

// relayToolsProvider selects the effective tool set for a config. It is a
// variable so tests can inject a deterministic provider on any OS, mirroring
// statusToolsProvider.
var relayToolsProvider = tools.Provider

// prepareRelayRegistry resolves the effective tool set for cfg and assembles
// the registry the relay client registers with the hub. Tools named in
// cfg.DisabledTools are marked disabled: they stay resolvable for dispatch
// (calls get a "disabled" error) but are excluded from the hub register payload
// — see toolreg.Registry.MarkDisabled. The returned note explains an empty tool
// set (for example on hosts without osascript).
func prepareRelayRegistry(cfg *config.Config) (*toolreg.Registry, string, error) {
	set, err := relayToolsProvider(cfg)
	if err != nil {
		return nil, "", err
	}
	registry := toolreg.New()
	for _, tool := range set.Tools {
		if err := registry.Register(tool); err != nil {
			return nil, set.Note, fmt.Errorf("register tool %q: %w", tool.Name, err)
		}
	}
	for _, name := range cfg.DisabledTools {
		registry.MarkDisabled(name)
	}
	return registry, set.Note, nil
}

// mgmtAPIAddr returns the loopback-only listen address for the management API.
// Binding 127.0.0.1 (never 0.0.0.0) keeps the unauthenticated API local to the
// connector host.
func mgmtAPIAddr(port int) string {
	return fmt.Sprintf("127.0.0.1:%d", port)
}
