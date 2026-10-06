package mcpregistry

import "context"

// DeviceServerConfig is the resolved wire shape of a device-targeted MCP server
// delivered to a device over the relay. It carries DECRYPTED secret env/header
// values, so it MUST never be logged, echoed in an ack/status payload, or passed
// through ToDTO/stripSecretKeys/tool-pool caches. It is built only inside
// Service.resolveDeviceConfig (the sole holder of the encryptor).
type DeviceServerConfig struct {
	Name      string         `json:"name"`
	Transport string         `json:"transport"` // "stdio" | "http" | "sse"
	Enabled   bool           `json:"enabled"`
	Command   string         `json:"command,omitempty"`
	Args      []string       `json:"args"`
	Env       map[string]any `json:"env"`
	URL       string         `json:"url,omitempty"`
	Headers   map[string]any `json:"headers"`
}

// DeviceConfigPusher is the in-process bridge from mcpregistry to the relay.
// It is defined here and implemented by mcprelay (via an fx adapter) so
// mcpregistry never imports mcprelay — a strictly one-way dependency.
//
// Resolve is invoked by the pusher to obtain a device's fully-resolved config
// (decrypted) from mcpregistry; it keeps decryption inside mcpregistry while
// letting the relay drive the per-session fan-out.
type DeviceConfigPusher interface {
	// PushConfigToInstance pushes resolved config to a single opted-in instance.
	// Unregistered or non-opted-in sessions are skipped (no error).
	PushConfigToInstance(ctx context.Context, projectID, instanceID string, servers []DeviceServerConfig)

	// PushConfigToProject pushes resolved config to every opted-in instance of
	// the project. When admin is false, only the requester's own devices
	// (ownerSubject) are targeted; an admin targets all opted-in devices. Each
	// target device's config is resolved via resolve so instance-scoped rows are
	// never leaked across devices.
	PushConfigToProject(ctx context.Context, projectID, ownerSubject string, admin bool, resolve func(ctx context.Context, instanceID string) ([]DeviceServerConfig, error))

	// OwnerSubject returns the owner subject recorded for an instance's session,
	// and whether that instance is currently registered.
	OwnerSubject(projectID, instanceID string) (string, bool)
}
