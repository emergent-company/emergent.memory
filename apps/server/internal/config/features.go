package config

// FeatureSet controls which optional domain modules are loaded at startup.
// Each flag maps to a FEATURE_* environment variable.
//
// Defaults preserve existing behaviour: all features on except devtools and chat.
// Set a flag to false to exclude the corresponding fx.Module from the application,
// which removes all associated HTTP routes and background workers.
//
// ⚠  Dependency note: disabling agents implicitly makes mcp and mcpregistry
// non-functional (they reference agent types at runtime). Always disable all
// three together or leave all three enabled.
type FeatureSet struct {
	// Chat enables the /api/chat SSE conversation routes.
	// The admin UI has been moved away from these routes; default is false.
	// Enable with FEATURE_CHAT=true if a client still depends on them.
	Chat bool `env:"FEATURE_CHAT" envDefault:"false"`
	// Agents enables the agent runner, agent definitions, and related MCP tools.
	// Disabling also makes FEATURE_MCP and FEATURE_MCPREGISTRY non-functional.
	Agents bool `env:"FEATURE_AGENTS" envDefault:"true"`

	// MCP enables the Model Context Protocol server and tool dispatch.
	MCP bool `env:"FEATURE_MCP" envDefault:"true"`

	// MCPRegistry enables the MCP server registry and tool-pool management.
	MCPRegistry bool `env:"FEATURE_MCPREGISTRY" envDefault:"true"`

	// MCPRelay enables the MCP relay proxy (bridges external MCP servers).
	MCPRelay bool `env:"FEATURE_MCPRELAY" envDefault:"true"`

	// Sandbox enables the agent sandboxed execution environment.
	Sandbox bool `env:"FEATURE_SANDBOX" envDefault:"true"`

	// SandboxImages enables sandbox Docker image management routes.
	SandboxImages bool `env:"FEATURE_SANDBOXIMAGES" envDefault:"true"`

	// Backups enables scheduled project backup workers and routes.
	Backups bool `env:"FEATURE_BACKUPS" envDefault:"true"`

	// Monitoring enables the run-monitoring domain (agent health alerts).
	Monitoring bool `env:"FEATURE_MONITORING" envDefault:"true"`

	// Tracing enables OpenTelemetry tracing routes and span storage.
	Tracing bool `env:"FEATURE_TRACING" envDefault:"true"`

	// Devtools enables the development-tools endpoints (schema introspection etc.).
	// Defaults to false in production; enable with FEATURE_DEVTOOLS=true.
	Devtools bool `env:"FEATURE_DEVTOOLS" envDefault:"false"`

	// SuperAdmin enables the super-admin management routes.
	SuperAdmin bool `env:"FEATURE_SUPERADMIN" envDefault:"true"`
}
