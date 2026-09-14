// Package status defines the connector's structured status document and its
// two renderings: the human text line-for-line compatible with the `status`
// command, and a stable machine-readable JSON document (`status --json`).
package status

import (
	"encoding/json"
	"fmt"
	"strings"
)

// SchemaVersion is the version of the JSON status document. Bump it on any
// breaking change to the JSON shape.
const SchemaVersion = 1

// HubState is the stable, machine-readable hub connection state.
type HubState string

const (
	// HubStateConnected means the instance is registered with the hub.
	HubStateConnected HubState = "connected"
	// HubStateNotConnected means the hub is reachable but the instance is absent.
	HubStateNotConnected HubState = "not_connected"
	// HubStateAuthFailed means the hub rejected the token.
	HubStateAuthFailed HubState = "auth_failed"
	// HubStateUnreachable means the hub could not be reached.
	HubStateUnreachable HubState = "unreachable"
	// HubStateMissingConfig means there is no local config to report on.
	HubStateMissingConfig HubState = "missing_config"
)

// Project identifies the Memory project this connector serves. It is optional.
type Project struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// HubDetail carries the numbers and error text behind the human hub line.
type HubDetail struct {
	HubToolCount   int    `json:"hub_tool_count"`
	LocalToolCount int    `json:"local_tool_count"`
	SessionCount   int    `json:"session_count"`
	Error          string `json:"error,omitempty"`
}

// DisabledTool names a platform tool that is not part of the effective set and
// why it is absent.
type DisabledTool struct {
	Name   string `json:"name"`
	Reason string `json:"reason,omitempty"`
}

// Document is the connector's status snapshot.
type Document struct {
	SchemaVersion int            `json:"schema_version"`
	InstanceID    string         `json:"instance_id"`
	Version       string         `json:"version"`
	Project       *Project       `json:"project,omitempty"`
	Tools         []string       `json:"tools"`
	ToolsNote     string         `json:"tools_note,omitempty"`
	DisabledTools []DisabledTool `json:"disabled_tools,omitempty"`
	HubState      HubState       `json:"hub_state"`
	HubDetail     HubDetail      `json:"hub_detail"`
}

// Text renders the human-readable status, matching the historical `status`
// command output byte-for-byte.
func (d Document) Text() string {
	var b strings.Builder
	fmt.Fprintf(&b, "memory-connector status\n")
	fmt.Fprintf(&b, "instance: %s\n", d.InstanceID)
	fmt.Fprintf(&b, "version: %s\n", d.Version)
	switch {
	case len(d.Tools) > 0:
		fmt.Fprintf(&b, "tools (%d): %s\n", len(d.Tools), strings.Join(d.Tools, ", "))
	case d.ToolsNote != "":
		fmt.Fprintf(&b, "tools: none (%s)\n", d.ToolsNote)
	default:
		fmt.Fprintf(&b, "tools: none\n")
	}
	if len(d.DisabledTools) > 0 {
		fmt.Fprintf(&b, "tools-disabled: %s\n", d.disabledLine())
	}
	fmt.Fprintf(&b, "hub: %s\n", d.hubLine())
	return b.String()
}

// JSON renders the stable machine-readable status document.
func (d Document) JSON() ([]byte, error) {
	clone := d
	if clone.Tools == nil {
		clone.Tools = []string{}
	}
	b, err := json.MarshalIndent(clone, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("status: marshal document: %w", err)
	}
	return append(b, '\n'), nil
}

// disabledLine renders disabled tools as "name (reason), ...".
func (d Document) disabledLine() string {
	parts := make([]string, 0, len(d.DisabledTools))
	for _, dt := range d.DisabledTools {
		if dt.Reason == "" {
			parts = append(parts, dt.Name)
			continue
		}
		parts = append(parts, fmt.Sprintf("%s (%s)", dt.Name, dt.Reason))
	}
	return strings.Join(parts, ", ")
}

// hubLine is the human-readable detail behind HubState.
func (d Document) hubLine() string {
	switch d.HubState {
	case HubStateConnected:
		if d.HubDetail.HubToolCount != d.HubDetail.LocalToolCount {
			return fmt.Sprintf("connected — hub shows %d tool(s) for this instance but %d local (mismatch; restart 'relay' to re-register)", d.HubDetail.HubToolCount, d.HubDetail.LocalToolCount)
		}
		return fmt.Sprintf("connected — hub shows %d tool(s), matches local set", d.HubDetail.HubToolCount)
	case HubStateNotConnected:
		return fmt.Sprintf("not connected — instance not found among %d hub session(s)", d.HubDetail.SessionCount)
	case HubStateAuthFailed:
		return "authentication failed — the server rejected the token"
	case HubStateUnreachable:
		return "unreachable — " + d.HubDetail.Error
	case HubStateMissingConfig:
		return "missing config — run 'memory-connector init' first"
	default:
		return string(d.HubState)
	}
}
