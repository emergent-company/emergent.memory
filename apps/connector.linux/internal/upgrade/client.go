// Package upgrade implements self-update for the memory-connector CLI. It
// resolves the newest connector-v* release in the shared emergent.memory monorepo,
// downloads the matching os/arch archive, verifies its .sha256 checksum when
// available, extracts the bare memory-connector binary, and atomically replaces
// the running executable.
//
// The HTTP client and both base URLs are injectable (and overridable via
// environment variables) so callers and tests never need the real network.
package upgrade

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

// Repo is the GitHub repository that publishes connector releases (alongside
// the web UI and gateway releases).
const Repo = "emergent-company/emergent.memory"

const (
	// DefaultAPIBase is the GitHub REST API base.
	DefaultAPIBase = "https://api.github.com"
	// DefaultDownloadBase is the GitHub release asset base. An asset lives at
	// <base>/<tag>/<asset-name>.
	DefaultDownloadBase = "https://github.com/" + Repo + "/releases/download"
	// TagPrefix marks connector releases inside the shared repo, which also
	// publishes tags for other components.
	TagPrefix = "connector-v"
	// BinaryName is the executable name inside the release archive.
	BinaryName = "memory-connector"
)

const (
	// EnvBaseURL overrides the release asset base (mirrors, local smoke tests).
	// It matches the variable the install.sh installer honors.
	EnvBaseURL = "MEMORY_CONNECTOR_BASE_URL"
	// EnvAPIBaseURL overrides the GitHub API base (mirrors, tests).
	EnvAPIBaseURL = "MEMORY_CONNECTOR_API_BASE_URL"
)

// ErrChecksumMismatch is returned when the downloaded archive's SHA-256 does
// not match the published .sha256 file.
var ErrChecksumMismatch = errors.New("checksum mismatch")

// Release is a resolved connector release for the current OS/arch.
type Release struct {
	// Version is the bare semver (for example "0.2.0").
	Version string
	// Tag is the GitHub tag (for example "connector-v0.2.0").
	Tag string
	// AssetName is the os/arch archive name.
	AssetName string
	// AssetURL is the download URL of AssetName.
	AssetURL string
}

// Client resolves and installs connector releases. The zero value is usable;
// NewClient also populates it from the environment.
type Client struct {
	// HTTP performs all requests. Nil means http.DefaultClient.
	HTTP *http.Client
	// APIBase is the GitHub API base. Empty means DefaultAPIBase.
	APIBase string
	// DownloadBase is the release asset base. Empty means DefaultDownloadBase.
	DownloadBase string
}

// NewClient builds a Client honoring the base-URL environment overrides.
func NewClient() *Client {
	return &Client{
		HTTP:         &http.Client{Timeout: 5 * time.Minute},
		APIBase:      envOr(EnvAPIBaseURL, DefaultAPIBase),
		DownloadBase: envOr(EnvBaseURL, DefaultDownloadBase),
	}
}

func envOr(key, def string) string {
	if v := strings.TrimSpace(os.Getenv(key)); v != "" {
		return v
	}
	return def
}

func (c *Client) httpClient() *http.Client {
	if c != nil && c.HTTP != nil {
		return c.HTTP
	}
	return http.DefaultClient
}

func (c *Client) apiBase() string {
	if c != nil && c.APIBase != "" {
		return c.APIBase
	}
	return DefaultAPIBase
}

func (c *Client) downloadBase() string {
	if c != nil && c.DownloadBase != "" {
		return c.DownloadBase
	}
	return DefaultDownloadBase
}

// assetURL builds the canonical asset URL for a tag + asset name.
func (c *Client) assetURL(tag, name string) string {
	return strings.TrimRight(c.downloadBase(), "/") + "/" + tag + "/" + name
}

// ghRelease is the subset of the GitHub releases payload the resolver needs.
type ghRelease struct {
	TagName    string    `json:"tag_name"`
	Draft      bool      `json:"draft"`
	Prerelease bool      `json:"prerelease"`
	Assets     []ghAsset `json:"assets"`
}

type ghAsset struct {
	Name               string `json:"name"`
	BrowserDownloadURL string `json:"browser_download_url"`
}

// Latest lists the repo's releases and returns the highest connector-v* semver
// for the current OS/arch. GitHub orders releases by publish date, not semver,
// so all tags are inspected and the highest is chosen explicitly.
func (c *Client) Latest(ctx context.Context) (*Release, error) {
	url := strings.TrimRight(c.apiBase(), "/") + "/repos/" + Repo + "/releases?per_page=100"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	resp, err := c.httpClient().Do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("release API %s: %s", url, resp.Status)
	}
	var releases []ghRelease
	if err := json.NewDecoder(io.LimitReader(resp.Body, 8<<20)).Decode(&releases); err != nil {
		return nil, fmt.Errorf("decode release API: %w", err)
	}

	var best *ghRelease
	for i := range releases {
		r := &releases[i]
		if r.Draft || !strings.HasPrefix(r.TagName, TagPrefix) {
			continue
		}
		if _, _, ok := parseVersion(r.TagName); !ok {
			continue
		}
		if best == nil || Compare(r.TagName, best.TagName) > 0 {
			best = r
		}
	}
	if best == nil {
		return nil, fmt.Errorf("no %s* release found in %s", TagPrefix, Repo)
	}
	return c.releaseFrom(best.TagName, best.Assets)
}

// ReleaseFor builds the release for a pinned version (or tag) without an API
// call, using the predictable asset URL. The version may be bare ("0.2.0") or
// prefixed ("v0.2.0", "connector-v0.2.0").
func (c *Client) ReleaseFor(v string) (*Release, error) {
	version := Normalize(v)
	if _, _, ok := parseVersion(version); !ok {
		return nil, fmt.Errorf("invalid version %q (want X.Y.Z)", v)
	}
	return c.releaseFrom(TagPrefix+version, nil)
}

// releaseFrom resolves the os/arch asset for tag, preferring the URL reported
// by the API and falling back to the canonical download base.
func (c *Client) releaseFrom(tag string, assets []ghAsset) (*Release, error) {
	version := Normalize(tag)
	name, err := assetName(version, runtime.GOOS, runtime.GOARCH)
	if err != nil {
		return nil, err
	}
	url := ""
	for _, a := range assets {
		if a.Name == name {
			url = a.BrowserDownloadURL
			break
		}
	}
	if url == "" {
		url = c.assetURL(tag, name)
	}
	return &Release{Version: version, Tag: tag, AssetName: name, AssetURL: url}, nil
}

// assetName returns the os/arch archive name, rejecting unsupported platforms.
func assetName(version, goos, goarch string) (string, error) {
	switch goos + "/" + goarch {
	case "linux/amd64", "linux/arm64", "darwin/amd64", "darwin/arm64":
		return fmt.Sprintf("%s_%s_%s_%s.tar.gz", BinaryName, version, goos, goarch), nil
	default:
		return "", fmt.Errorf("unsupported platform %s/%s", goos, goarch)
	}
}

// FetchBinary downloads the release archive, verifies its checksum when the
// .sha256 file is reachable, and returns the extracted memory-connector bytes.
func (c *Client) FetchBinary(ctx context.Context, rel *Release) ([]byte, error) {
	archive, err := c.get(ctx, rel.AssetURL)
	if err != nil {
		return nil, fmt.Errorf("download %s: %w", rel.AssetName, err)
	}
	if err := c.verifyChecksum(ctx, archive, rel.AssetURL+".sha256"); err != nil {
		return nil, err
	}
	bin, err := extractBinary(archive)
	if err != nil {
		return nil, fmt.Errorf("extract %s: %w", rel.AssetName, err)
	}
	return bin, nil
}

// Install fetches rel and atomically replaces the running executable, returning
// the path that was replaced.
func (c *Client) Install(ctx context.Context, rel *Release) (string, error) {
	target, err := Executable()
	if err != nil {
		return "", err
	}
	if resolved, err := filepath.EvalSymlinks(target); err == nil {
		target = resolved
	}
	data, err := c.FetchBinary(ctx, rel)
	if err != nil {
		return "", err
	}
	if err := Replace(target, data); err != nil {
		return "", err
	}
	return target, nil
}

// get performs a GET and returns the body, requiring a 200 response.
func (c *Client) get(ctx context.Context, url string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	resp, err := c.httpClient().Do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("%s: %s", url, resp.Status)
	}
	return io.ReadAll(io.LimitReader(resp.Body, 512<<20))
}

// verifyChecksum checks data against the SHA-256 published at checksumURL. A
// missing or unreachable checksum file is not an error (best-effort, matching
// the installer); a reachable but mismatching checksum is.
func (c *Client) verifyChecksum(ctx context.Context, data []byte, checksumURL string) error {
	body, err := c.get(ctx, checksumURL)
	if err != nil {
		return nil
	}
	fields := strings.Fields(string(body))
	if len(fields) == 0 {
		return nil
	}
	expected := strings.ToLower(fields[0])
	actual := hex.EncodeToString(sum256(data))
	if expected != actual {
		return fmt.Errorf("%w: expected %s, got %s", ErrChecksumMismatch, expected, actual)
	}
	return nil
}

func sum256(data []byte) []byte {
	sum := sha256.Sum256(data)
	return sum[:]
}

// extractBinary reads the first regular entry named memory-connector from a
// gzip-compressed tar stream.
func extractBinary(data []byte) ([]byte, error) {
	gz, err := gzip.NewReader(bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	defer func() { _ = gz.Close() }()
	tr := tar.NewReader(gz)
	for {
		hdr, err := tr.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, err
		}
		if hdr.Typeflag != tar.TypeReg {
			continue
		}
		if filepath.Base(hdr.Name) == BinaryName {
			return io.ReadAll(tr)
		}
	}
	return nil, fmt.Errorf("%s not found in archive", BinaryName)
}
