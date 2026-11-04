// Package linux implements the Linux tool set for the connector: host
// information and a root-scoped filesystem. It is deliberately self-contained
// (its configuration types are package-local) and is added to the relay/status
// wiring in a later change.
package linux

import (
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"

	"github.com/emergent-company/emergent.memory/apps/connector.linux/internal/toolreg"
)

// Stable tool names. These are the policy-target keys used for enable/disable
// decisions, so they must not change.
const (
	// ToolHostInfo reports host identity and resource usage.
	ToolHostInfo = "linux-host-info"
	// ToolFSList lists a directory inside an allowed root.
	ToolFSList = "linux-fs-list"
	// ToolFSRead reads a file inside an allowed root.
	ToolFSRead = "linux-fs-read"
	// ToolFSWrite atomically writes a file inside a root that grants Write.
	ToolFSWrite = "linux-fs-write"
	// ToolFSDelete moves a path to the XDG trash inside a root that grants
	// Delete.
	ToolFSDelete = "linux-fs-delete"
)

// defaultMaxBytes is the fallback read/write cap (1 MiB).
const defaultMaxBytes int64 = 1 << 20

// ErrOutsideRoots is returned when a target path is not inside any allowed
// root (after symlink-safe resolution).
var ErrOutsideRoots = errors.New("path is outside the allowed roots")

// Root is one allowed filesystem subtree and the capabilities granted within
// it.
type Root struct {
	Path   string
	Read   bool
	Write  bool
	Delete bool
}

// Filesystem configures the filesystem tools. A nil *Filesystem disables them.
type Filesystem struct {
	Roots                []Root
	MaxReadBytes         int64 // default 1 MiB when <= 0
	MaxWriteBytes        int64 // default 1 MiB when <= 0
	AllowPermanentDelete bool  // allow permanent deletion when trashing is unavailable
}

// Config is the package-local tool configuration.
type Config struct {
	HostInfo   *bool       // nil => ENABLED (default on)
	Filesystem *Filesystem // nil => no filesystem tools
}

// Provider owns the configured tool set and the reasons some tools are absent.
type Provider struct {
	tools    []toolreg.Tool
	note     string
	disabled []string

	roots                []Root
	maxReadBytes         int64
	maxWriteBytes        int64
	allowPermanentDelete bool
	audit                *auditLogger
}

// New validates cfg and builds the provider. Roots must be absolute; they are
// resolved (symlink-safe) at construction. An empty root list is valid but
// yields no filesystem tools. Invalid roots are an error.
func New(cfg Config) (*Provider, error) {
	p := &Provider{audit: newAuditLogger(nil)}
	var notes []string

	if cfg.HostInfo == nil || *cfg.HostInfo {
		p.tools = append(p.tools, newHostInfoTool())
	} else {
		p.disabled = append(p.disabled, ToolHostInfo)
		notes = append(notes, "host info disabled by config")
	}

	if cfg.Filesystem == nil {
		p.disabled = append(p.disabled, ToolFSList, ToolFSRead, ToolFSWrite, ToolFSDelete)
		notes = append(notes, "filesystem tools disabled: no filesystem config")
	} else {
		roots, err := normalizeRoots(cfg.Filesystem.Roots)
		if err != nil {
			return nil, err
		}
		p.roots = roots
		p.maxReadBytes = cfg.Filesystem.MaxReadBytes
		if p.maxReadBytes <= 0 {
			p.maxReadBytes = defaultMaxBytes
		}
		p.maxWriteBytes = cfg.Filesystem.MaxWriteBytes
		if p.maxWriteBytes <= 0 {
			p.maxWriteBytes = defaultMaxBytes
		}
		p.allowPermanentDelete = cfg.Filesystem.AllowPermanentDelete

		if len(roots) == 0 {
			p.disabled = append(p.disabled, ToolFSList, ToolFSRead, ToolFSWrite, ToolFSDelete)
			notes = append(notes, "filesystem tools disabled: no roots configured")
		} else {
			if hasReadRoot(roots) {
				p.tools = append(p.tools, newFSListTool(roots), newFSReadTool(roots, p.maxReadBytes))
			} else {
				p.disabled = append(p.disabled, ToolFSList, ToolFSRead)
				notes = append(notes, "filesystem read tools disabled: no read-capable root configured")
			}
			if hasWriteRoot(roots) {
				p.tools = append(p.tools, newFSWriteTool(roots, p.maxWriteBytes, p.audit))
			} else {
				p.disabled = append(p.disabled, ToolFSWrite)
				notes = append(notes, "filesystem write tools disabled: no write-capable root configured")
			}
			if hasDeleteRoot(roots) {
				p.tools = append(p.tools, newFSDeleteTool(roots, p.allowPermanentDelete, p.audit))
			} else {
				p.disabled = append(p.disabled, ToolFSDelete)
				notes = append(notes, "filesystem delete tools disabled: no delete-capable root configured")
			}
		}
	}

	p.note = strings.Join(notes, "; ")
	return p, nil
}

// WithLogger sets the audit logger used by the mutating tools (write/delete)
// and returns the provider. A nil logger restores slog.Default.
func (p *Provider) WithLogger(logger *slog.Logger) *Provider {
	p.audit.logger = newAuditLogger(logger).logger
	return p
}

// Tools returns the enabled tools (host info first, then filesystem).
func (p *Provider) Tools() []toolreg.Tool {
	return append([]toolreg.Tool(nil), p.tools...)
}

// Note explains, in human terms, why some tools are absent. It is empty when
// every tool is enabled.
func (p *Provider) Note() string { return p.note }

// Disabled returns the stable tool names that are not currently available.
func (p *Provider) Disabled() []string {
	return append([]string(nil), p.disabled...)
}

func hasReadRoot(roots []Root) bool {
	for _, r := range roots {
		if r.Read {
			return true
		}
	}
	return false
}

func hasWriteRoot(roots []Root) bool {
	for _, r := range roots {
		if r.Write {
			return true
		}
	}
	return false
}

func hasDeleteRoot(roots []Root) bool {
	for _, r := range roots {
		if r.Delete {
			return true
		}
	}
	return false
}

// normalizeRoots validates and resolves each configured root. Roots must be
// absolute; resolution is symlink-safe so containment checks compare real
// paths.
func normalizeRoots(roots []Root) ([]Root, error) {
	out := make([]Root, 0, len(roots))
	for _, r := range roots {
		if strings.TrimSpace(r.Path) == "" {
			return nil, errors.New("linux tools: root path must not be empty")
		}
		if !filepath.IsAbs(r.Path) {
			return nil, fmt.Errorf("linux tools: root path %q must be absolute", r.Path)
		}
		resolved, err := resolvePath(r.Path)
		if err != nil {
			return nil, fmt.Errorf("linux tools: resolve root %q: %w", r.Path, err)
		}
		r.Path = resolved
		out = append(out, r)
	}
	return out, nil
}

// resolvePath makes p absolute, cleans it, and resolves symlinks on its
// deepest existing ancestor (so a target that does not exist yet still resolves
// relative to a real parent).
func resolvePath(p string) (string, error) {
	abs, err := filepath.Abs(p)
	if err != nil {
		return "", err
	}
	return resolveExisting(filepath.Clean(abs))
}

// resolveExisting resolves symlinks on the deepest existing ancestor of abs and
// re-joins the non-existent tail. This is what makes scoping symlink-safe while
// still allowing writes to not-yet-existing paths.
func resolveExisting(abs string) (string, error) {
	cur := abs
	var tail []string
	for {
		resolved, err := filepath.EvalSymlinks(cur)
		if err == nil {
			parts := append([]string{resolved}, reverseStrings(tail)...)
			return filepath.Join(parts...), nil
		}
		if !errors.Is(err, os.ErrNotExist) {
			return "", err
		}
		parent := filepath.Dir(cur)
		if parent == cur {
			// Filesystem root itself could not be resolved; fall back to the
			// cleaned absolute path.
			return abs, nil
		}
		tail = append(tail, filepath.Base(cur))
		cur = parent
	}
}

func reverseStrings(in []string) []string {
	out := make([]string, len(in))
	for i, s := range in {
		out[len(in)-1-i] = s
	}
	return out
}

// contains reports whether resolvedTarget is resolvedRoot or below it. It uses
// filepath.Rel (not a string prefix) so /data does not match /database.
func contains(resolvedRoot, resolvedTarget string) bool {
	rel, err := filepath.Rel(resolvedRoot, resolvedTarget)
	if err != nil {
		return false
	}
	if rel == ".." || strings.HasPrefix(rel, ".."+string(os.PathSeparator)) || filepath.IsAbs(rel) {
		return false
	}
	return true
}

// resolveTarget resolves target symlink-safely and returns the first configured
// root that contains it, along with the resolved absolute target. It returns
// ErrOutsideRoots when no root contains the target.
func resolveTarget(roots []Root, target string) (Root, string, error) {
	resolvedTarget, err := resolvePath(target)
	if err != nil {
		return Root{}, "", err
	}
	for _, root := range roots {
		resolvedRoot, err := resolvePath(root.Path)
		if err != nil {
			continue
		}
		if contains(resolvedRoot, resolvedTarget) {
			return root, resolvedTarget, nil
		}
	}
	return Root{}, "", fmt.Errorf("%w: %s", ErrOutsideRoots, target)
}

// requireString returns the non-blank string value for key.
func requireString(args map[string]any, key string) (string, error) {
	v, ok := args[key]
	if !ok || v == nil {
		return "", fmt.Errorf("missing required argument %q", key)
	}
	s, ok := v.(string)
	if !ok {
		return "", fmt.Errorf("argument %q must be a string, got %T", key, v)
	}
	if strings.TrimSpace(s) == "" {
		return "", fmt.Errorf("argument %q must not be empty", key)
	}
	return s, nil
}
