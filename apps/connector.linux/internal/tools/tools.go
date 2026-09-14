// Package tools selects the effective local tool set for the current platform
// from connector configuration. Linux has a first-class tool set
// (internal/tools/linux) with host info and a root-scoped filesystem; other
// platforms keep the Apple Notes/Reminders tools.
package tools

import (
	"runtime"

	"github.com/emergent-company/memory.web-ui/connector/internal/appletools"
	"github.com/emergent-company/memory.web-ui/connector/internal/config"
	"github.com/emergent-company/memory.web-ui/connector/internal/toolreg"
	"github.com/emergent-company/memory.web-ui/connector/internal/tools/linux"
)

// DisabledTool pairs a stable tool name with the human reason it is absent.
type DisabledTool struct {
	Name   string
	Reason string
}

// Set is the effective tool set selected for a config on a platform. Disabled
// lists the platform tools that are not available and why.
type Set struct {
	Tools    []toolreg.Tool
	Note     string
	Disabled []DisabledTool
}

// Provider selects the effective tool set for cfg on the current platform.
func Provider(cfg *config.Config) (*Set, error) {
	return providerFor(runtime.GOOS, cfg)
}

// providerFor is Provider with an explicit GOOS so the non-Linux fallback is
// testable on any host.
func providerFor(goos string, cfg *config.Config) (*Set, error) {
	if cfg == nil {
		cfg = &config.Config{}
	}
	if goos == "linux" {
		return linuxSet(cfg)
	}
	apple, note := appletools.DefaultProvider()
	return &Set{Tools: apple, Note: note}, nil
}

// linuxSet builds the Linux provider from cfg.Tools.Linux. A configuration
// error (for example a relative root) is returned to the caller.
func linuxSet(cfg *config.Config) (*Set, error) {
	p, err := linux.New(linuxConfigFrom(cfg.Tools.Linux))
	if err != nil {
		return nil, err
	}
	return &Set{
		Tools:    p.Tools(),
		Note:     p.Note(),
		Disabled: linuxDisabled(cfg.Tools.Linux),
	}, nil
}

// linuxConfigFrom maps the connector config schema onto the Linux package's
// package-local config. Host info is enabled unless explicitly disabled; the
// filesystem is omitted when absent or explicitly disabled.
func linuxConfigFrom(lc config.LinuxToolsConfig) linux.Config {
	hostEnabled := lc.HostInfo == nil || lc.HostInfo.Enabled == nil || *lc.HostInfo.Enabled
	out := linux.Config{HostInfo: &hostEnabled}
	if fs := lc.Filesystem; fs != nil && (fs.Enabled == nil || *fs.Enabled) {
		roots := make([]linux.Root, 0, len(fs.Roots))
		for _, r := range fs.Roots {
			roots = append(roots, linux.Root{Path: r.Path, Read: r.Read, Write: r.Write, Delete: r.Delete})
		}
		out.Filesystem = &linux.Filesystem{
			Roots:                roots,
			MaxReadBytes:         fs.MaxReadBytes,
			MaxWriteBytes:        fs.MaxWriteBytes,
			AllowPermanentDelete: fs.AllowPermanentDelete != nil && *fs.AllowPermanentDelete,
		}
	}
	return out
}

// linuxDisabled mirrors linux.New's availability rules to produce per-tool
// reasons for the status report.
func linuxDisabled(lc config.LinuxToolsConfig) []DisabledTool {
	var out []DisabledTool
	if lc.HostInfo != nil && lc.HostInfo.Enabled != nil && !*lc.HostInfo.Enabled {
		out = append(out, DisabledTool{Name: linux.ToolHostInfo, Reason: "host_info disabled in config"})
	}

	fs := lc.Filesystem
	switch {
	case fs == nil:
		out = append(out, allFSDisabled("no filesystem config")...)
	case fs.Enabled != nil && !*fs.Enabled:
		out = append(out, allFSDisabled("filesystem disabled in config")...)
	case len(fs.Roots) == 0:
		out = append(out, allFSDisabled("no filesystem roots configured")...)
	default:
		if !hasCap(fs.Roots, func(r config.RootConfig) bool { return r.Read }) {
			out = append(out,
				DisabledTool{Name: linux.ToolFSList, Reason: "no read-capable root configured"},
				DisabledTool{Name: linux.ToolFSRead, Reason: "no read-capable root configured"},
			)
		}
		if !hasCap(fs.Roots, func(r config.RootConfig) bool { return r.Write }) {
			out = append(out, DisabledTool{Name: linux.ToolFSWrite, Reason: "no write-capable root configured"})
		}
		if !hasCap(fs.Roots, func(r config.RootConfig) bool { return r.Delete }) {
			out = append(out, DisabledTool{Name: linux.ToolFSDelete, Reason: "no delete-capable root configured"})
		}
	}
	return out
}

func allFSDisabled(reason string) []DisabledTool {
	return []DisabledTool{
		{Name: linux.ToolFSList, Reason: reason},
		{Name: linux.ToolFSRead, Reason: reason},
		{Name: linux.ToolFSWrite, Reason: reason},
		{Name: linux.ToolFSDelete, Reason: reason},
	}
}

func hasCap(roots []config.RootConfig, pred func(config.RootConfig) bool) bool {
	for _, r := range roots {
		if pred(r) {
			return true
		}
	}
	return false
}
