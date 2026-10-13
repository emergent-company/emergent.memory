package mcpregistry

import (
	"errors"
	"slices"
	"testing"
)

// TestBuildDeviceInstallDTONpm asserts an npm package becomes an npx stdio
// command, with package/runtime arguments rendered in order, version pinned,
// and required-or-secret env keys declared as placeholders with the server
// created disabled until they are filled.
func TestBuildDeviceInstallDTONpm(t *testing.T) {
	dto, err := buildDeviceInstallDTO(RegistryServer{
		Name:    "io.github.example/thing",
		Version: "9.9.9",
		Packages: []RegistryPackage{
			{
				RegistryType: "npm",
				Identifier:   "@example/thing",
				Version:      "1.2.3",
				RuntimeArguments: []RegistryArgument{
					{Name: "log-level", Value: "info"},
				},
				PackageArguments: []RegistryArgument{
					{Value: "extra"},
				},
				EnvironmentVariables: []RegistryEnvVar{
					{Name: "API_KEY", IsRequired: true, IsSecret: true, Description: " API key "},
					{Name: "OPTIONAL_FLAG"},
				},
			},
		},
	}, "thing")
	if err != nil {
		t.Fatalf("buildDeviceInstallDTO: %v", err)
	}

	if dto.Type != ServerTypeStdio {
		t.Errorf("type = %q, want stdio", dto.Type)
	}
	if dto.Command == nil || *dto.Command != "npx" {
		t.Fatalf("command = %v, want npx", dto.Command)
	}
	wantArgs := []string{"-y", "@example/thing@1.2.3", "--log-level", "info", "extra"}
	if !slices.Equal(dto.Args, wantArgs) {
		t.Errorf("args = %v, want %v", dto.Args, wantArgs)
	}
	if _, ok := dto.Env["API_KEY"]; !ok {
		t.Errorf("env = %v, want API_KEY placeholder", dto.Env)
	}
	if dto.Env["API_KEY"] != "" {
		t.Errorf("API_KEY placeholder = %v, want empty", dto.Env["API_KEY"])
	}
	if !slices.Equal(dto.SecretEnvKeys, []string{"API_KEY"}) {
		t.Errorf("secretEnvKeys = %v, want [API_KEY]", dto.SecretEnvKeys)
	}
	if len(dto.RequiredConfig) != 1 || dto.RequiredConfig[0].Name != "API_KEY" {
		t.Fatalf("requiredConfig = %+v, want just API_KEY", dto.RequiredConfig)
	}
	if dto.RequiredConfig[0].Description != "API key" {
		t.Errorf("requiredConfig description = %q, want trimmed", dto.RequiredConfig[0].Description)
	}
	if dto.Enabled == nil || *dto.Enabled {
		t.Errorf("enabled = %v, want false while required config is unfilled", dto.Enabled)
	}
}

// TestBuildDeviceInstallDTOPypi asserts a pypi package becomes a uvx invocation
// (no npx-specific -y) and that the entry version is used when the package
// carries none.
func TestBuildDeviceInstallDTOPypi(t *testing.T) {
	dto, err := buildDeviceInstallDTO(RegistryServer{
		Name:    "io.github.example/thing",
		Version: "2.0.0",
		Packages: []RegistryPackage{
			{RegistryType: "pypi", Identifier: "example-thing"},
		},
	}, "thing")
	if err != nil {
		t.Fatalf("buildDeviceInstallDTO: %v", err)
	}
	if dto.Command == nil || *dto.Command != "uvx" {
		t.Fatalf("command = %v, want uvx", dto.Command)
	}
	if !slices.Equal(dto.Args, []string{"example-thing@2.0.0"}) {
		t.Errorf("args = %v, want [example-thing@2.0.0]", dto.Args)
	}
	if len(dto.RequiredConfig) != 0 || dto.Enabled != nil {
		t.Errorf("unexpected required config/enabled: %+v / %v", dto.RequiredConfig, dto.Enabled)
	}
}

// TestBuildDeviceInstallDTOPrefersFirstSupportedPackage asserts an unsupported
// package type is skipped in favour of a later npm/pypi one.
func TestBuildDeviceInstallDTOPrefersFirstSupportedPackage(t *testing.T) {
	dto, err := buildDeviceInstallDTO(RegistryServer{
		Name: "io.github.example/thing",
		Packages: []RegistryPackage{
			{RegistryType: "oci", Identifier: "ghcr.io/example/thing"},
			{RegistryType: "npm", Identifier: "@example/thing", Version: "1.0.0"},
		},
	}, "thing")
	if err != nil {
		t.Fatalf("buildDeviceInstallDTO: %v", err)
	}
	if dto.Command == nil || *dto.Command != "npx" {
		t.Fatalf("command = %v, want npx from the npm package", dto.Command)
	}
}

// TestBuildDeviceInstallDTORejectsRemoteOnly asserts an entry with only remotes
// (or only unsupported package types) is rejected: a device install runs a local
// package, so there is nothing to push.
func TestBuildDeviceInstallDTORejectsRemoteOnly(t *testing.T) {
	_, err := buildDeviceInstallDTO(RegistryServer{
		Name:    "io.github.example/remote-only",
		Remotes: []RegistryRemote{{Type: "streamable-http", URL: "https://example.com/mcp"}},
	}, "remote-only")
	if !errors.Is(err, ErrUnsupportedRegistryTransport) {
		t.Fatalf("err = %v, want ErrUnsupportedRegistryTransport", err)
	}

	_, err = buildDeviceInstallDTO(RegistryServer{
		Name:     "io.github.example/oci-only",
		Packages: []RegistryPackage{{RegistryType: "oci", Identifier: "ghcr.io/example/thing"}},
	}, "oci-only")
	if !errors.Is(err, ErrUnsupportedRegistryTransport) {
		t.Fatalf("err = %v, want ErrUnsupportedRegistryTransport", err)
	}
}
