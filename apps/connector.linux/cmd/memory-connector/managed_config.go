package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sync"

	"gopkg.in/yaml.v3"

	"github.com/emergent-company/emergent.memory/apps/connector.linux/internal/config"
	"github.com/emergent-company/emergent.memory/apps/connector.linux/internal/mcphost"
	"github.com/emergent-company/emergent.memory/apps/connector.linux/internal/relay"
	"github.com/emergent-company/emergent.memory/apps/connector.linux/internal/secretstore"
)

// ErrStale is returned when a config frame's version is not newer than the
// last applied version. Its message is the exact wire ack string "stale".
var ErrStale = errors.New("stale")

// managedFileLayout is the on-disk shape of the persisted Memory-managed server
// set. Env and Headers are stripped (credential values live in the secret
// store), so this file never contains secrets.
type managedFileLayout struct {
	Version int64                  `yaml:"version"`
	Servers []mcphost.ServerConfig `yaml:"servers"`
}

// managedConfigApplier applies Memory-managed MCP configuration pushed over
// the relay. It is the connector-side half of the device-mcp-config contract:
// version guard -> validate -> apply via reload -> persist (only on success)
// -> re-register -> ack. It owns the last-applied version and the persisted
// managed set. Locally-configured servers (in the main config file) are never
// mutated: the managed set lives in a separate file, and the effective running
// set merges the two with the managed set winning on name collision.
type managedConfigApplier struct {
	mu  sync.Mutex
	log *slog.Logger

	managedServers []mcphost.ServerConfig
	lastVersion    int64

	current   func() *config.Config
	reload    func(context.Context, *config.Config) ([]mcphost.ServerStatus, error)
	toolCount func() int

	managedPath string
	secrets     secretstore.Store
}

// newManagedConfigApplier builds an applier. current returns the live local
// config (whose MCPServers are the locally-configured set); reload applies a
// fully-merged config live (registryManager.reload); toolCount reports the
// total registered tool count for the ack. managedServers/lastVersion seed the
// in-memory managed state from disk at startup.
func newManagedConfigApplier(
	log *slog.Logger,
	managedPath string,
	secrets secretstore.Store,
	current func() *config.Config,
	reload func(context.Context, *config.Config) ([]mcphost.ServerStatus, error),
	toolCount func() int,
	managedServers []mcphost.ServerConfig,
	lastVersion int64,
) *managedConfigApplier {
	if log == nil {
		log = slog.Default()
	}
	return &managedConfigApplier{
		log:            log,
		managedServers: append([]mcphost.ServerConfig(nil), managedServers...),
		lastVersion:    lastVersion,
		current:        current,
		reload:         reload,
		toolCount:      toolCount,
		managedPath:    managedPath,
		secrets:        secrets,
	}
}

// servers returns the current Memory-managed set (a copy). Callers use it to
// merge managed servers into a reload triggered by a local-config change.
func (a *managedConfigApplier) servers() []mcphost.ServerConfig {
	a.mu.Lock()
	defer a.mu.Unlock()
	return append([]mcphost.ServerConfig(nil), a.managedServers...)
}

// Apply implements relay.ConfigApplier. It never logs the frame or any
// credential value; only server counts and the version are logged.
func (a *managedConfigApplier) Apply(ctx context.Context, frame relay.ConfigFrame) (relay.ConfigResult, error) {
	a.mu.Lock()
	defer a.mu.Unlock()

	// Version guard: reject a not-newer frame as stale.
	if frame.Version <= a.lastVersion {
		return relay.ConfigResult{}, ErrStale
	}

	// Validate the managed set on its own (transport/required fields/duplicate
	// names/header+env legality).
	if err := mcphost.ValidateServers(frame.Servers); err != nil {
		return relay.ConfigResult{}, fmt.Errorf("managed config: %w", err)
	}

	cfg := a.current()
	if cfg == nil {
		return relay.ConfigResult{}, fmt.Errorf("managed config: no active config")
	}

	// Apply through the same live-reload path as the management API. The
	// effective set is local servers merged with the managed set (managed wins
	// on name collision; local-only names are preserved).
	next := *cfg
	next.MCPServers = mergeServers(cfg.MCPServers, frame.Servers)
	statuses, err := a.reload(ctx, &next)
	if err != nil {
		// reload builds the new registry+manager before swapping, so a failed
		// reload leaves both the running config and the persisted set untouched.
		return relay.ConfigResult{}, fmt.Errorf("managed config: %w", err)
	}

	// Persist only after a successful reload, so a bad entry never lands on
	// disk and breaks the next restart.
	if err := persistManaged(a.managedPath, frame.Servers, frame.Version); err != nil {
		return relay.ConfigResult{}, fmt.Errorf("managed config: %w", err)
	}
	if err := storeManagedSecrets(a.secrets, frame.Servers); err != nil {
		return relay.ConfigResult{}, fmt.Errorf("managed config: %w", err)
	}

	a.managedServers = append([]mcphost.ServerConfig(nil), frame.Servers...)
	a.lastVersion = frame.Version

	tools := countTools(statuses)
	if a.toolCount != nil {
		tools = a.toolCount()
	}

	a.log.Info("managed config applied", "servers", len(frame.Servers), "tools", tools, "version", frame.Version)
	return relay.ConfigResult{Applied: len(frame.Servers), Tools: tools}, nil
}

// mergeServers returns the effective server set: the managed set first, then
// any locally-configured servers whose name does not collide with a managed
// server (managed wins on collision).
func mergeServers(local, managed []mcphost.ServerConfig) []mcphost.ServerConfig {
	merged := make([]mcphost.ServerConfig, 0, len(local)+len(managed))
	managedNames := make(map[string]struct{}, len(managed))
	for _, m := range managed {
		managedNames[m.Name] = struct{}{}
		merged = append(merged, m)
	}
	for _, l := range local {
		if _, ok := managedNames[l.Name]; ok {
			continue
		}
		merged = append(merged, l)
	}
	return merged
}

// countTools sums the per-server tool counts of a reload result.
func countTools(statuses []mcphost.ServerStatus) int {
	n := 0
	for _, st := range statuses {
		n += st.ToolCount
	}
	return n
}

// persistManaged atomically writes the managed set (env/headers stripped) and
// version to managedPath, mode 0600.
func persistManaged(managedPath string, servers []mcphost.ServerConfig, version int64) error {
	if err := os.MkdirAll(filepath.Dir(managedPath), 0o700); err != nil {
		return fmt.Errorf("create managed config dir: %w", err)
	}
	layout := managedFileLayout{Version: version, Servers: stripCredentials(servers)}
	b, err := yaml.Marshal(layout)
	if err != nil {
		return fmt.Errorf("marshal managed config: %w", err)
	}
	if err := os.WriteFile(managedPath, b, 0o600); err != nil {
		return fmt.Errorf("write managed config: %w", err)
	}
	return nil
}

// stripCredentials returns a copy of servers with Env and Headers cleared so
// credential values never reach the managed file.
func stripCredentials(servers []mcphost.ServerConfig) []mcphost.ServerConfig {
	out := make([]mcphost.ServerConfig, len(servers))
	copy(out, servers)
	for i := range out {
		out[i].Env = nil
		out[i].Headers = nil
	}
	return out
}

// storeManagedSecrets persists each managed server's env/headers map as a JSON
// blob in the secret store, keyed by server name.
func storeManagedSecrets(store secretstore.Store, servers []mcphost.ServerConfig) error {
	for _, s := range servers {
		blob, err := json.Marshal(struct {
			Env     map[string]string `json:"env,omitempty"`
			Headers map[string]string `json:"headers,omitempty"`
		}{Env: s.Env, Headers: s.Headers})
		if err != nil {
			return fmt.Errorf("marshal managed secrets for %q: %w", s.Name, err)
		}
		if err := store.Save(managedSecretName(s.Name), blob); err != nil {
			return fmt.Errorf("store managed secrets for %q: %w", s.Name, err)
		}
	}
	return nil
}

// managedSecretName maps a server name to a secret-store key. Server names are
// validated (non-blank) but may contain filesystem-hostile characters; those
// are rejected by the secret store and surface as an acked error.
func managedSecretName(serverName string) string {
	return "mcp." + serverName
}

// loadManagedConfig reads the persisted managed set (if any) and recombines
// env/headers from the secret store. A missing file is not an error.
func loadManagedConfig(managedPath string, secrets secretstore.Store) ([]mcphost.ServerConfig, int64, error) {
	b, err := os.ReadFile(managedPath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, 0, nil
		}
		return nil, 0, fmt.Errorf("read managed config: %w", err)
	}
	var layout managedFileLayout
	if err := yaml.Unmarshal(b, &layout); err != nil {
		return nil, 0, fmt.Errorf("parse managed config: %w", err)
	}
	servers := make([]mcphost.ServerConfig, len(layout.Servers))
	for i, s := range layout.Servers {
		servers[i] = s
		if blob, err := secrets.Load(managedSecretName(s.Name)); err == nil && blob != nil {
			var creds struct {
				Env     map[string]string `json:"env"`
				Headers map[string]string `json:"headers"`
			}
			if err := json.Unmarshal(blob, &creds); err != nil {
				return nil, 0, fmt.Errorf("parse managed secrets for %q: %w", s.Name, err)
			}
			servers[i].Env = creds.Env
			servers[i].Headers = creds.Headers
		}
	}
	return servers, layout.Version, nil
}

// managedConfigDir returns the directory holding the persisted managed set and
// its secrets, beside the main config under memory-connector/managed.
func managedConfigDir(configPath string) string {
	return filepath.Join(filepath.Dir(configPath), "memory-connector", "managed")
}

// managedConfigPath returns the managed-set structure file path.
func managedConfigPath(configPath string) string {
	return filepath.Join(managedConfigDir(configPath), "servers.yml")
}

// managedSecretsStore returns the secret store for managed-server credentials.
func managedSecretsStore(configPath string) secretstore.Store {
	return secretstore.New(filepath.Join(managedConfigDir(configPath), "secrets"))
}
