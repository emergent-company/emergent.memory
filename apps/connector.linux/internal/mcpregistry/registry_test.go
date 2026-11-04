package mcpregistry

import (
	"errors"
	"testing"

	"github.com/emergent-company/emergent.memory/apps/connector.linux/internal/mcphost"
)

// runnerProbe restores the package lookPath after a test overrides it.
func runnerProbe(t *testing.T, fn func(string) (string, error)) {
	t.Helper()
	orig := lookPath
	lookPath = fn
	t.Cleanup(func() { lookPath = orig })
}

func TestSuggestConfig(t *testing.T) {
	tests := []struct {
		name     string
		entry    RegistryServer
		override string
		wantCfg  mcphost.ServerConfig
		wantSug  Suggestion
		wantErr  error
	}{
		{
			name: "npm stdio with version",
			entry: RegistryServer{
				Name:        "io.github.ChromeDevTools/chrome-devtools-mcp",
				Version:     "0.1.0",
				HasPackages: true,
				Packages: []RegistryPackage{
					{RegistryType: "npm", Name: "chrome-devtools-mcp", Identifier: "chrome-devtools-mcp", Transport: "stdio"},
				},
				EnvVars: []RegistryEnvVar{{Name: "API_KEY", IsRequired: true, IsSecret: true}},
			},
			wantCfg: mcphost.ServerConfig{
				Name:      "chrome-devtools-mcp",
				Transport: mcphost.TransportStdio,
				Command:   "npx",
				Args:      []string{"-y", "chrome-devtools-mcp@0.1.0"},
				Env:       map[string]string{"API_KEY": ""},
			},
			wantSug: Suggestion{Transport: mcphost.TransportStdio, RequiredEnvKeys: []string{"API_KEY"}},
		},
		{
			name: "npm package version wins over entry version",
			entry: RegistryServer{
				Name:        "io.github.x/y",
				Version:     "1.0.0",
				HasPackages: true,
				Packages: []RegistryPackage{
					{RegistryType: "npm", Name: "y", Identifier: "y", Version: "2.3.4", Transport: "stdio"},
				},
			},
			wantCfg: mcphost.ServerConfig{
				Name:      "y",
				Transport: mcphost.TransportStdio,
				Command:   "npx",
				Args:      []string{"-y", "y@2.3.4"},
				Env:       map[string]string{},
			},
			wantSug: Suggestion{Transport: mcphost.TransportStdio},
		},
		{
			name: "pypi package version wins over entry version",
			entry: RegistryServer{
				Name:        "io.github.x/pytool",
				Version:     "1.0.0",
				HasPackages: true,
				Packages: []RegistryPackage{
					{RegistryType: "pypi", Name: "pytool", Identifier: "pytool", Version: "2.0.0", Transport: "stdio"},
				},
			},
			wantCfg: mcphost.ServerConfig{
				Name:      "pytool",
				Transport: mcphost.TransportStdio,
				Command:   "uvx",
				Args:      []string{"pytool@2.0.0"},
				Env:       map[string]string{},
			},
			wantSug: Suggestion{Transport: mcphost.TransportStdio},
		},
		{
			name: "npm stdio without version",
			entry: RegistryServer{
				Name:        "io.github.x/y",
				HasPackages: true,
				Packages: []RegistryPackage{
					{RegistryType: "npm", Name: "y", Identifier: "y", Transport: "stdio"},
				},
			},
			wantCfg: mcphost.ServerConfig{
				Name:      "y",
				Transport: mcphost.TransportStdio,
				Command:   "npx",
				Args:      []string{"-y", "y"},
				Env:       map[string]string{},
			},
			wantSug: Suggestion{Transport: mcphost.TransportStdio},
		},
		{
			name: "pypi stdio pinned",
			entry: RegistryServer{
				Name:        "io.github.x/pytool",
				Version:     "2.0.0",
				HasPackages: true,
				Packages: []RegistryPackage{
					{RegistryType: "pypi", Name: "pytool", Identifier: "pytool", Transport: "stdio"},
				},
			},
			wantCfg: mcphost.ServerConfig{
				Name:      "pytool",
				Transport: mcphost.TransportStdio,
				Command:   "uvx",
				Args:      []string{"pytool@2.0.0"},
				Env:       map[string]string{},
			},
			wantSug: Suggestion{Transport: mcphost.TransportStdio},
		},
		{
			name: "pypi stdio unpinned",
			entry: RegistryServer{
				Name:        "io.github.x/pytool",
				HasPackages: true,
				Packages: []RegistryPackage{
					{RegistryType: "pypi", Name: "pytool", Identifier: "pytool", Transport: "stdio"},
				},
			},
			wantCfg: mcphost.ServerConfig{
				Name:      "pytool",
				Transport: mcphost.TransportStdio,
				Command:   "uvx",
				Args:      []string{"pytool"},
				Env:       map[string]string{},
			},
			wantSug: Suggestion{Transport: mcphost.TransportStdio},
		},
		{
			name: "npm arguments appended in order",
			entry: RegistryServer{
				Name:        "io.github.x/y",
				Version:     "1.0.0",
				HasPackages: true,
				Packages: []RegistryPackage{
					{
						RegistryType:     "npm",
						Name:             "y",
						Identifier:       "y",
						Transport:        "stdio",
						RuntimeArguments: []string{"--port", "8080"},
						PackageArguments: []string{"serve"},
					},
				},
			},
			wantCfg: mcphost.ServerConfig{
				Name:      "y",
				Transport: mcphost.TransportStdio,
				Command:   "npx",
				Args:      []string{"-y", "y@1.0.0", "--port", "8080", "serve"},
				Env:       map[string]string{},
			},
			wantSug: Suggestion{Transport: mcphost.TransportStdio},
		},
		{
			name: "remote http fallback",
			entry: RegistryServer{
				Name:       "io.github.x/remote",
				HasRemotes: true,
				Remotes: []RegistryRemote{
					{Type: "streamable-http", URL: "https://mcp.example.com/mcp"},
				},
				Headers: []RegistryHeader{{Name: "Authorization", IsRequired: true, IsSecret: true}},
			},
			wantCfg: mcphost.ServerConfig{
				Name:      "remote",
				Transport: mcphost.TransportHTTP,
				URL:       "https://mcp.example.com/mcp",
				Env:       map[string]string{},
				Headers:   map[string]string{"Authorization": ""},
			},
			wantSug: Suggestion{
				Transport:          mcphost.TransportHTTP,
				RequiredHeaderKeys: []string{"Authorization"},
			},
		},
		{
			name: "remote sse fallback",
			entry: RegistryServer{
				Name:       "io.github.x/legacy",
				HasRemotes: true,
				Remotes: []RegistryRemote{
					{Type: "sse", URL: "https://mcp.example.com/sse"},
				},
			},
			wantCfg: mcphost.ServerConfig{
				Name:      "legacy",
				Transport: mcphost.TransportSSE,
				URL:       "https://mcp.example.com/sse",
				Env:       map[string]string{},
				Headers:   map[string]string{},
			},
			wantSug: Suggestion{Transport: mcphost.TransportSSE},
		},
		{
			name: "http preferred over sse",
			entry: RegistryServer{
				Name:       "io.github.x/both",
				HasRemotes: true,
				Remotes: []RegistryRemote{
					{Type: "sse", URL: "https://mcp.example.com/sse"},
					{Type: "streamable-http", URL: "https://mcp.example.com/http"},
				},
			},
			wantCfg: mcphost.ServerConfig{
				Name:      "both",
				Transport: mcphost.TransportHTTP,
				URL:       "https://mcp.example.com/http",
				Env:       map[string]string{},
				Headers:   map[string]string{},
			},
			wantSug: Suggestion{Transport: mcphost.TransportHTTP},
		},
		{
			name: "unsupported no packages no remotes",
			entry: RegistryServer{
				Name: "io.github.x/empty",
			},
			wantErr: ErrUnsupported,
		},
		{
			name: "unsupported non-stdio package only",
			entry: RegistryServer{
				Name:        "io.github.x/oci",
				HasPackages: true,
				Packages: []RegistryPackage{
					{RegistryType: "oci", Name: "img", Identifier: "img", Transport: "stdio"},
				},
			},
			wantErr: ErrUnsupported,
		},
		{
			name: "local name override wins",
			entry: RegistryServer{
				Name:        "io.github.x/y",
				Version:     "1.0.0",
				HasPackages: true,
				Packages: []RegistryPackage{
					{RegistryType: "npm", Name: "y", Identifier: "y", Transport: "stdio"},
				},
			},
			override: "my-server",
			wantCfg: mcphost.ServerConfig{
				Name:      "my-server",
				Transport: mcphost.TransportStdio,
				Command:   "npx",
				Args:      []string{"-y", "y@1.0.0"},
				Env:       map[string]string{},
			},
			wantSug: Suggestion{Transport: mcphost.TransportStdio},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Default probe: runners are present (no warnings) for these cases.
			runnerProbe(t, func(string) (string, error) { return "/bin/runner", nil })

			cfg, sug, err := SuggestConfig(tt.entry, tt.override)
			if tt.wantErr != nil {
				if !errors.Is(err, tt.wantErr) {
					t.Fatalf("err = %v, want %v", err, tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected err: %v", err)
			}
			if cfg.Name != tt.wantCfg.Name || cfg.Transport != tt.wantCfg.Transport ||
				cfg.Command != tt.wantCfg.Command || cfg.URL != tt.wantCfg.URL {
				t.Errorf("cfg = %+v, want %+v", cfg, tt.wantCfg)
			}
			if !equalStrings(cfg.Args, tt.wantCfg.Args) {
				t.Errorf("cfg.Args = %v, want %v", cfg.Args, tt.wantCfg.Args)
			}
			if !equalStringMap(cfg.Env, tt.wantCfg.Env) {
				t.Errorf("cfg.Env = %v, want %v", cfg.Env, tt.wantCfg.Env)
			}
			if !equalStringMap(cfg.Headers, tt.wantCfg.Headers) {
				t.Errorf("cfg.Headers = %v, want %v", cfg.Headers, tt.wantCfg.Headers)
			}
			if sug.Transport != tt.wantSug.Transport {
				t.Errorf("sug.Transport = %q, want %q", sug.Transport, tt.wantSug.Transport)
			}
			if !equalStrings(sug.RequiredEnvKeys, tt.wantSug.RequiredEnvKeys) {
				t.Errorf("sug.RequiredEnvKeys = %v, want %v", sug.RequiredEnvKeys, tt.wantSug.RequiredEnvKeys)
			}
			if !equalStrings(sug.RequiredHeaderKeys, tt.wantSug.RequiredHeaderKeys) {
				t.Errorf("sug.RequiredHeaderKeys = %v, want %v", sug.RequiredHeaderKeys, tt.wantSug.RequiredHeaderKeys)
			}
		})
	}
}

func TestSuggestConfigRunnerMissingIsWarning(t *testing.T) {
	runnerProbe(t, func(cmd string) (string, error) {
		return "", errors.New("not found")
	})

	entry := RegistryServer{
		Name:        "io.github.x/y",
		Version:     "1.0.0",
		HasPackages: true,
		Packages: []RegistryPackage{
			{RegistryType: "npm", Name: "y", Identifier: "y", Transport: "stdio"},
		},
	}

	cfg, sug, err := SuggestConfig(entry, "")
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if cfg.Command != "npx" {
		t.Errorf("cfg.Command = %q, want npx", cfg.Command)
	}
	if len(sug.Warnings) != 1 || sug.Warnings[0] == "" {
		t.Errorf("sug.Warnings = %v, want one non-empty warning", sug.Warnings)
	}
}

func TestSuggestConfigPypiRunnerMissingIsWarning(t *testing.T) {
	runnerProbe(t, func(cmd string) (string, error) {
		if cmd == "uvx" {
			return "", errors.New("not found")
		}
		return "/bin/runner", nil
	})

	entry := RegistryServer{
		Name:        "io.github.x/pytool",
		HasPackages: true,
		Packages: []RegistryPackage{
			{RegistryType: "pypi", Name: "pytool", Identifier: "pytool", Transport: "stdio"},
		},
	}

	_, sug, err := SuggestConfig(entry, "")
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if len(sug.Warnings) != 1 {
		t.Errorf("sug.Warnings = %v, want one warning", sug.Warnings)
	}
}

func TestDeriveLocalName(t *testing.T) {
	tests := []struct{ in, want string }{
		{"io.github.x/y", "y"},
		{"io.github.github/github-mcp-server", "github-mcp-server"},
		{"plainname", "plainname"},
		{"a/b/c", "c"},
	}
	for _, tt := range tests {
		if got := deriveLocalName(tt.in); got != tt.want {
			t.Errorf("deriveLocalName(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func equalStringMap(a, b map[string]string) bool {
	if len(a) != len(b) {
		return false
	}
	for k, v := range a {
		if b[k] != v {
			return false
		}
	}
	return true
}
