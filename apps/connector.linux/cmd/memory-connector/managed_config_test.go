package main

import (
	"bytes"
	"context"
	"errors"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/emergent-company/emergent.memory/apps/connector.linux/internal/account"
	"github.com/emergent-company/emergent.memory/apps/connector.linux/internal/config"
	"github.com/emergent-company/emergent.memory/apps/connector.linux/internal/mcphost"
	"github.com/emergent-company/emergent.memory/apps/connector.linux/internal/relay"
	"github.com/emergent-company/emergent.memory/apps/connector.linux/internal/secretstore"
)

func quietLog() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func TestMergeServersManagedWinsLocalPreserved(t *testing.T) {
	local := []mcphost.ServerConfig{
		{Name: "local-only", Transport: mcphost.TransportStdio, Command: "l"},
		{Name: "shared", Transport: mcphost.TransportStdio, Command: "local-cmd"},
	}
	managed := []mcphost.ServerConfig{
		{Name: "shared", Transport: mcphost.TransportHTTP, URL: "http://managed"},
		{Name: "managed-only", Transport: mcphost.TransportStdio, Command: "m"},
	}
	merged := mergeServers(local, managed)
	wantOrder := []string{"shared", "managed-only", "local-only"}
	if len(merged) != len(wantOrder) {
		t.Fatalf("merged length = %d, want %d: %+v", len(merged), len(wantOrder), merged)
	}
	for i, name := range wantOrder {
		if merged[i].Name != name {
			t.Errorf("merged[%d].Name = %q, want %q (%+v)", i, merged[i].Name, name, merged)
		}
	}
	// Managed wins on the name collision.
	if merged[0].Transport != mcphost.TransportHTTP || merged[0].URL != "http://managed" {
		t.Errorf("managed entry must win the collision: %+v", merged[0])
	}
}

func TestApplyPersistsOnlyAfterSuccessfulReload(t *testing.T) {
	dir := t.TempDir()
	managedPath := filepath.Join(dir, "managed", "servers.yml")
	secrets := secretstore.New(filepath.Join(dir, "managed", "secrets"))

	local := []mcphost.ServerConfig{{Name: "local-only", Transport: mcphost.TransportStdio, Command: "l"}}
	cfg := &config.Config{ServerURL: "https://srv", Token: "t", MCPServers: local}

	var reloaded *config.Config
	reload := func(_ context.Context, c *config.Config) ([]mcphost.ServerStatus, error) {
		reloaded = c
		return []mcphost.ServerStatus{{Name: "github", ToolCount: 2}}, nil
	}
	toolCount := func() int { return 5 }

	a := newManagedConfigApplier(quietLog(), managedPath, secrets, func() *config.Config { return cfg }, reload, toolCount, nil, 0)

	frame := relay.ConfigFrame{ID: "c1", Version: 1, Servers: []mcphost.ServerConfig{
		{Name: "github", Transport: mcphost.TransportStdio, Command: "github-mcp", Env: map[string]string{"TOKEN": "s3cret"}},
	}}
	res, err := a.Apply(context.Background(), frame)
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if res.Applied != 1 || res.Tools != 5 {
		t.Errorf("result = %+v, want applied=1 tools=5", res)
	}

	// The reload saw the merged set (managed + local-only).
	if reloaded == nil {
		t.Fatal("reload not called")
	}
	if len(reloaded.MCPServers) != 2 || reloaded.MCPServers[0].Name != "github" || reloaded.MCPServers[1].Name != "local-only" {
		t.Errorf("reloaded servers = %+v, want [github local-only]", reloaded.MCPServers)
	}

	// Managed file exists, structure present, credential value stripped.
	b, err := os.ReadFile(managedPath)
	if err != nil {
		t.Fatalf("read managed file: %v", err)
	}
	if strings.Contains(string(b), "s3cret") {
		t.Errorf("managed file leaked a credential value:\n%s", b)
	}
	if !strings.Contains(string(b), "github") || !strings.Contains(string(b), "version: 1") {
		t.Errorf("managed file missing structure/version:\n%s", b)
	}

	// Credential value stored in the secret store.
	blob, err := secrets.Load("mcp.github")
	if err != nil {
		t.Fatalf("load secret: %v", err)
	}
	if blob == nil || !strings.Contains(string(blob), "s3cret") {
		t.Errorf("credential not stored in secret store: %q", blob)
	}

	// The local config file is untouched (managed set kept separate).
	if len(cfg.MCPServers) != 1 || cfg.MCPServers[0].Name != "local-only" {
		t.Errorf("local config mutated: %+v", cfg.MCPServers)
	}
}

func TestApplyValidationFailureHasNoSideEffects(t *testing.T) {
	dir := t.TempDir()
	managedPath := filepath.Join(dir, "managed", "servers.yml")
	secrets := secretstore.New(filepath.Join(dir, "managed", "secrets"))
	cfg := &config.Config{ServerURL: "https://srv", Token: "t"}

	reloadCalled := false
	reload := func(context.Context, *config.Config) ([]mcphost.ServerStatus, error) {
		reloadCalled = true
		return nil, nil
	}
	a := newManagedConfigApplier(quietLog(), managedPath, secrets, func() *config.Config { return cfg }, reload, nil, nil, 0)

	frame := relay.ConfigFrame{ID: "c1", Version: 1, Servers: []mcphost.ServerConfig{
		{Name: "bad", Transport: "carrier-pigeon"},
	}}
	_, err := a.Apply(context.Background(), frame)
	if err == nil || !strings.Contains(err.Error(), "unknown transport") {
		t.Fatalf("Apply error = %v, want unknown transport", err)
	}
	if reloadCalled {
		t.Error("reload must not run for an invalid config")
	}
	if _, err := os.Stat(managedPath); !errors.Is(err, os.ErrNotExist) {
		t.Error("managed file must not be written for an invalid config")
	}
}

func TestApplyReloadFailureHasNoSideEffects(t *testing.T) {
	dir := t.TempDir()
	managedPath := filepath.Join(dir, "managed", "servers.yml")
	secrets := secretstore.New(filepath.Join(dir, "managed", "secrets"))
	cfg := &config.Config{ServerURL: "https://srv", Token: "t"}

	reload := func(context.Context, *config.Config) ([]mcphost.ServerStatus, error) {
		return nil, errors.New("tool collision")
	}
	a := newManagedConfigApplier(quietLog(), managedPath, secrets, func() *config.Config { return cfg }, reload, nil, nil, 0)

	frame := relay.ConfigFrame{ID: "c1", Version: 1, Servers: []mcphost.ServerConfig{
		{Name: "github", Transport: mcphost.TransportStdio, Command: "github-mcp"},
	}}
	_, err := a.Apply(context.Background(), frame)
	if err == nil || !strings.Contains(err.Error(), "tool collision") {
		t.Fatalf("Apply error = %v, want tool collision", err)
	}
	if _, err := os.Stat(managedPath); !errors.Is(err, os.ErrNotExist) {
		t.Error("managed file must not be written when reload fails")
	}
	// Version must not advance on failure.
	if a.lastVersion != 0 {
		t.Errorf("lastVersion = %d, want 0 after failed apply", a.lastVersion)
	}
}

func TestApplyStaleVersionRejected(t *testing.T) {
	dir := t.TempDir()
	managedPath := filepath.Join(dir, "managed", "servers.yml")
	secrets := secretstore.New(filepath.Join(dir, "managed", "secrets"))
	cfg := &config.Config{ServerURL: "https://srv", Token: "t"}

	reloadCalled := false
	reload := func(context.Context, *config.Config) ([]mcphost.ServerStatus, error) {
		reloadCalled = true
		return nil, nil
	}
	a := newManagedConfigApplier(quietLog(), managedPath, secrets, func() *config.Config { return cfg }, reload, nil, nil, 5)

	frame := relay.ConfigFrame{ID: "c1", Version: 5, Servers: []mcphost.ServerConfig{
		{Name: "github", Transport: mcphost.TransportStdio, Command: "github-mcp"},
	}}
	_, err := a.Apply(context.Background(), frame)
	if !errors.Is(err, ErrStale) {
		t.Fatalf("Apply error = %v, want ErrStale", err)
	}
	if reloadCalled {
		t.Error("reload must not run for a stale frame")
	}
	if _, err := os.Stat(managedPath); !errors.Is(err, os.ErrNotExist) {
		t.Error("managed file must not be written for a stale frame")
	}
}

func TestApplyLogsNoCredentialValues(t *testing.T) {
	dir := t.TempDir()
	managedPath := filepath.Join(dir, "managed", "servers.yml")
	secrets := secretstore.New(filepath.Join(dir, "managed", "secrets"))
	cfg := &config.Config{ServerURL: "https://srv", Token: "t"}

	var buf bytes.Buffer
	log := slog.New(slog.NewTextHandler(&buf, nil))
	reload := func(context.Context, *config.Config) ([]mcphost.ServerStatus, error) {
		return []mcphost.ServerStatus{{Name: "github", ToolCount: 1}}, nil
	}
	a := newManagedConfigApplier(log, managedPath, secrets, func() *config.Config { return cfg }, reload, nil, nil, 0)

	const secret = "TOP-SECRET-CREDENTIAL"
	frame := relay.ConfigFrame{ID: "c1", Version: 1, Servers: []mcphost.ServerConfig{
		{Name: "github", Transport: mcphost.TransportHTTP, URL: "http://x", Headers: map[string]string{"Authorization": "Bearer " + secret}},
	}}
	if _, err := a.Apply(context.Background(), frame); err != nil {
		t.Fatalf("Apply: %v", err)
	}
	logs := buf.String()
	if strings.Contains(logs, secret) {
		t.Errorf("logs leaked credential value:\n%s", logs)
	}
	if strings.Contains(logs, "Authorization") {
		t.Errorf("logs leaked credential key:\n%s", logs)
	}
}

func TestLoadManagedConfigRoundTrip(t *testing.T) {
	dir := t.TempDir()
	managedPath := filepath.Join(dir, "managed", "servers.yml")
	secrets := secretstore.New(filepath.Join(dir, "managed", "secrets"))

	servers := []mcphost.ServerConfig{
		{Name: "github", Transport: mcphost.TransportStdio, Command: "github-mcp", Env: map[string]string{"TOKEN": "s3cret"}, Headers: map[string]string{"X": "y"}},
	}
	if err := persistManaged(managedPath, servers, 9); err != nil {
		t.Fatalf("persistManaged: %v", err)
	}
	if err := storeManagedSecrets(secrets, servers); err != nil {
		t.Fatalf("storeManagedSecrets: %v", err)
	}

	got, ver, err := loadManagedConfig(managedPath, secrets)
	if err != nil {
		t.Fatalf("loadManagedConfig: %v", err)
	}
	if ver != 9 {
		t.Errorf("version = %d, want 9", ver)
	}
	if len(got) != 1 {
		t.Fatalf("servers length = %d, want 1", len(got))
	}
	if got[0].Env["TOKEN"] != "s3cret" || got[0].Headers["X"] != "y" {
		t.Errorf("recombined server = %+v", got[0])
	}
}

func TestLoadManagedConfigMissingIsNil(t *testing.T) {
	got, ver, err := loadManagedConfig(filepath.Join(t.TempDir(), "missing"), secretstore.New(t.TempDir()))
	if err != nil || got != nil || ver != 0 {
		t.Errorf("loadManagedConfig(missing) = (%v, %d, %v), want (nil, 0, nil)", got, ver, err)
	}
}

// TestManagedConfigDirPrimaryKeepsLegacy pins the backward-compatible location:
// the primary/legacy config file keeps the original `<dir>/memory-connector/
// managed` directory and `managed/servers.yml` file.
func TestManagedConfigDirPrimaryKeepsLegacy(t *testing.T) {
	dir := t.TempDir()
	primary := filepath.Join(dir, "memory-connector.yml")

	wantDir := filepath.Join(dir, "memory-connector", "managed")
	if got := managedConfigDir(primary); got != wantDir {
		t.Errorf("managedConfigDir(primary) = %q, want %q", got, wantDir)
	}
	wantPath := filepath.Join(wantDir, "servers.yml")
	if got := managedConfigPath(primary); got != wantPath {
		t.Errorf("managedConfigPath(primary) = %q, want %q", got, wantPath)
	}
}

// TestManagedConfigDirIsolatedPerConfigFile is the core #1727 regression: two
// sibling config files (primary + per-project) share a parent directory but
// must NOT share a managed set.
func TestManagedConfigDirIsolatedPerConfigFile(t *testing.T) {
	dir := t.TempDir()
	primary := filepath.Join(dir, "memory-connector.yml")
	project := filepath.Join(dir, "memory-connector-proj-abc123.yml")

	primaryDir := managedConfigDir(primary)
	projectDir := managedConfigDir(project)
	if primaryDir == projectDir {
		t.Fatalf("sibling config files share managed dir %q; want distinct dirs", primaryDir)
	}
	if primaryDir != filepath.Join(dir, "memory-connector", "managed") {
		t.Errorf("primary managed dir = %q, want legacy managed/", primaryDir)
	}
	if want := filepath.Join(dir, "memory-connector", "managed-proj-abc123"); projectDir != want {
		t.Errorf("project managed dir = %q, want %q", projectDir, want)
	}
	// The persisted structures must not collide either.
	if managedConfigPath(primary) == managedConfigPath(project) {
		t.Errorf("sibling config files share managed servers.yml path %q", managedConfigPath(primary))
	}
}

// TestManagedConfigDirDistinctProjectsDiffer ensures two different projects
// never share a managed set.
func TestManagedConfigDirDistinctProjectsDiffer(t *testing.T) {
	dir := t.TempDir()
	a := managedConfigDir(filepath.Join(dir, "memory-connector-proj-a.yml"))
	b := managedConfigDir(filepath.Join(dir, "memory-connector-proj-b.yml"))
	if a == b {
		t.Fatalf("distinct projects share managed dir %q", a)
	}
}

// TestManagedConfigDirStableForSameConfig proves the derivation is a pure
// function of the config path: the same config file always resolves to the same
// managed dir.
func TestManagedConfigDirStableForSameConfig(t *testing.T) {
	path := filepath.Join(t.TempDir(), "memory-connector-proj-stable.yml")
	if first, second := managedConfigDir(path), managedConfigDir(path); first != second {
		t.Errorf("managedConfigDir unstable: %q != %q", first, second)
	}
}

// TestManagedDirIsolatedWhileSessionStoreShared ties the two invariants
// together: sibling configs get distinct managed dirs, yet the account-session
// store (derived from the shared parent directory) stays shared.
func TestManagedDirIsolatedWhileSessionStoreShared(t *testing.T) {
	dir := t.TempDir()
	primary := filepath.Join(dir, "memory-connector.yml")
	project := filepath.Join(dir, "memory-connector-proj-xyz.yml")

	if managedConfigDir(primary) == managedConfigDir(project) {
		t.Error("managed dirs must be isolated per config file")
	}
	if account.BaseDirForConfig(primary) != account.BaseDirForConfig(project) {
		t.Error("account-session store must stay shared across sibling config files")
	}
}
