package blueprints

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"gopkg.in/yaml.v3"

	"github.com/emergent-company/emergent.memory/pkg/apperror"
)

// GitHub blueprint import limits. These bound the resources a single import can
// consume: the compressed download, the total uncompressed bytes, and the number
// of archive entries. Exceeding any of them aborts the import with 413.
const (
	maxArchiveBytes    int64 = 50 << 20  // 50 MiB compressed tarball
	maxExtractedBytes  int64 = 200 << 20 // 200 MiB total uncompressed
	maxArchiveFiles          = 20000
	githubFetchTimeout       = 60 * time.Second

	githubHost   = "github.com"
	codeloadHost = "codeload.github.com"
)

// codeloadBaseURL is the only origin the server is allowed to download a
// repository tarball from.
const codeloadBaseURL = "https://codeload.github.com"

// manifestFileNames are the optional repo-root manifest descriptors. When one
// is present it is parsed directly as a BlueprintManifest (plus optional
// name/version/description/author metadata); otherwise the directory layout
// used by `memory blueprints` (schemas|packs/, agents/, skills/, seed/) is
// loaded.
var manifestFileNames = []string{"blueprint.json", "blueprint.yaml", "blueprint.yml"}

// orgRepoRe constrains GitHub org and repo path segments. Ref characters are
// validated separately (they may contain '/', e.g. refs/tags/v1).
var orgRepoRe = regexp.MustCompile(`^[A-Za-z0-9._-]+$`)

// refRe constrains a git ref used in the codeload path so it cannot smuggle
// path separators, traversal, query or fragment delimiters.
var refRe = regexp.MustCompile(`^[A-Za-z0-9._/+-]+$`)

// ImportGitHubRequest is the request body for POST /api/blueprints/import.
// Token is optional and is only ever used as an outbound Authorization header;
// it is never persisted, logged, or echoed in responses or errors.
type ImportGitHubRequest struct {
	URL   string `json:"url"`
	Ref   string `json:"ref,omitempty"`
	Token string `json:"token,omitempty"`
}

// GitHubFetcher downloads and safely extracts a GitHub repository tarball. All
// fields are injectable so tests can point the fetch at an httptest server and
// tighten the limits; production uses newGitHubFetcher.
type GitHubFetcher struct {
	client          *http.Client
	codeloadBaseURL string // e.g. "https://codeload.github.com" (no trailing slash)
	codeloadHost    string // host of codeloadBaseURL; every request hop must match
	maxArchiveBytes int64
	maxExtractBytes int64
	maxFiles        int
}

// newGitHubFetcher returns the production fetcher: no redirects to hosts other
// than codeload.github.com, bounded timeout, and the documented limits.
func newGitHubFetcher() *GitHubFetcher {
	f := &GitHubFetcher{
		codeloadBaseURL: codeloadBaseURL,
		codeloadHost:    codeloadHost,
		maxArchiveBytes: maxArchiveBytes,
		maxExtractBytes: maxExtractedBytes,
		maxFiles:        maxArchiveFiles,
	}
	f.client = f.newClient()
	return f
}

// newClient builds the outbound HTTP client with the redirect allowlist policy.
// Split out so tests can construct a fetcher bound to an httptest origin with
// the same redirect policy.
func (f *GitHubFetcher) newClient() *http.Client {
	return &http.Client{
		Timeout: githubFetchTimeout,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			// Do not blindly follow redirects: every hop must stay on the
			// allowlisted codeload host over https. A redirect elsewhere is
			// refused.
			if len(via) >= 10 {
				return errors.New("stopped after 10 redirects")
			}
			if !strings.EqualFold(req.URL.Scheme, "https") {
				return fmt.Errorf("refusing redirect to non-https scheme %q", req.URL.Scheme)
			}
			if !strings.EqualFold(req.URL.Host, f.codeloadHost) {
				return fmt.Errorf("refusing redirect to non-allowlisted host %q", req.URL.Host)
			}
			return nil
		},
	}
}

// ──────────────────────────────────────────────
// Service entry point
// ──────────────────────────────────────────────

// ImportFromGitHub fetches a blueprint from a GitHub repository URL, extracts
// it safely, loads and validates its manifest, and creates + publishes a
// blueprint through the existing service methods. projectID scopes the created
// blueprint ("" = global; the caller is responsible for authorizing a global
// write before calling). The optional token is used for private repos and is
// never stored.
func (s *Service) ImportFromGitHub(ctx context.Context, projectID string, req *ImportGitHubRequest) (*Blueprint, error) {
	if req == nil {
		return nil, apperror.NewBadRequest("url is required")
	}
	org, repo, ref, err := parseGitHubURL(req.URL, req.Ref)
	if err != nil {
		return nil, err
	}

	fetcher := s.fetcher
	if fetcher == nil {
		fetcher = newGitHubFetcher()
	}

	root, cleanup, err := fetcher.Fetch(ctx, org, repo, ref, req.Token)
	if err != nil {
		return nil, err
	}
	defer cleanup()

	desc, err := loadBlueprintDir(root, repo)
	if err != nil {
		return nil, err
	}

	manifest, err := json.Marshal(desc.BlueprintManifest)
	if err != nil {
		return nil, apperror.NewBadRequest("blueprint manifest is not serializable")
	}

	var scope *string
	if projectID != "" {
		scope = &projectID
	}

	bp, err := s.CreateBlueprint(ctx, &CreateBlueprintRequest{
		Name:        desc.Name,
		Version:     desc.Version,
		Description: desc.Description,
		Author:      desc.Author,
		ProjectID:   scope,
		Manifest:    manifest,
	})
	if err != nil {
		return nil, err
	}

	published, err := s.PublishBlueprint(ctx, projectID, bp.ID)
	if err != nil {
		return nil, err
	}
	return published, nil
}

// ──────────────────────────────────────────────
// URL guard
// ──────────────────────────────────────────────

// parseGitHubURL validates a repository URL and returns org, repo, and ref.
// Only https://github.com/<org>/<repo> is accepted, optionally with a
// /tree/<ref> path and/or a #<ref> fragment. An explicit ref argument wins over
// both. Every other host/scheme is rejected with 400.
func parseGitHubURL(rawURL, refOverride string) (org, repo, ref string, err error) {
	rawURL = strings.TrimSpace(rawURL)
	if rawURL == "" {
		return "", "", "", apperror.NewBadRequest("url is required")
	}
	u, perr := url.Parse(rawURL)
	if perr != nil || u.Host == "" {
		return "", "", "", apperror.NewBadRequest("invalid GitHub URL: expected https://github.com/<org>/<repo>[#ref]")
	}
	if u.Scheme != "https" {
		return "", "", "", apperror.NewBadRequest("invalid GitHub URL: only https is allowed")
	}
	if u.User != nil {
		return "", "", "", apperror.NewBadRequest("invalid GitHub URL: credentials are not allowed")
	}
	if err := validatePublicHost(u.Hostname()); err != nil {
		return "", "", "", err
	}
	// Exact host match; a port or any other host is rejected.
	if !strings.EqualFold(u.Host, githubHost) {
		return "", "", "", apperror.NewBadRequest("invalid GitHub URL: only github.com is allowed")
	}

	path := strings.Trim(u.Path, "/")
	parts := strings.Split(path, "/")
	if len(parts) < 2 || parts[0] == "" || parts[1] == "" {
		return "", "", "", apperror.NewBadRequest("invalid GitHub URL: expected https://github.com/<org>/<repo>[#ref]")
	}
	org = parts[0]
	repo = strings.TrimSuffix(parts[1], ".git")
	if !orgRepoRe.MatchString(org) || !orgRepoRe.MatchString(repo) {
		return "", "", "", apperror.NewBadRequest("invalid GitHub URL: malformed org or repo")
	}

	if len(parts) > 2 {
		// Accept /tree/<ref> (and deeper paths, which are part of the ref).
		if parts[2] != "tree" || len(parts) < 4 {
			return "", "", "", apperror.NewBadRequest("invalid GitHub URL: unexpected path")
		}
		ref = strings.Join(parts[3:], "/")
	}
	if frag := u.Fragment; frag != "" {
		ref = frag
	}
	if refOverride != "" {
		ref = refOverride
	}
	if ref == "" {
		ref = "HEAD"
	}
	if !refRe.MatchString(ref) || strings.Contains(ref, "..") || strings.HasPrefix(ref, "/") {
		return "", "", "", apperror.NewBadRequest("invalid GitHub URL: malformed ref")
	}
	return org, repo, ref, nil
}

// validatePublicHost rejects hosts that must never be dialed or fetched on
// behalf of a caller: IP literals, localhost, and (for defence in depth) any
// host that resolves to a private/link-local/loopback/metadata range is handled
// by the exact-host allowlist. This is the host-level SSRF guard.
func validatePublicHost(host string) error {
	if host == "" {
		return apperror.NewBadRequest("invalid GitHub URL: missing host")
	}
	if ip := net.ParseIP(strings.Trim(host, "[]")); ip != nil {
		return apperror.NewBadRequest("invalid GitHub URL: IP-literal hosts are not allowed")
	}
	lower := strings.ToLower(strings.TrimSuffix(host, "."))
	if lower == "localhost" || strings.HasSuffix(lower, ".localhost") ||
		lower == "metadata" || strings.HasSuffix(lower, ".internal") ||
		strings.HasSuffix(lower, ".local") {
		return apperror.NewBadRequest("invalid GitHub URL: internal hosts are not allowed")
	}
	return nil
}

// ──────────────────────────────────────────────
// Fetch + extract
// ──────────────────────────────────────────────

// Fetch downloads the repository tarball for org/repo at ref, extracts it
// safely underneath a fresh temp directory, and returns the blueprint root
// (the single top-level directory of the archive, when present) plus a cleanup
// function. token, when non-empty, is sent as an Authorization header and is
// never logged.
func (f *GitHubFetcher) Fetch(ctx context.Context, org, repo, ref, token string) (string, func(), error) {
	archiveURL := f.codeloadBaseURL + "/" + org + "/" + repo + "/tar.gz/" + ref

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, archiveURL, nil)
	if err != nil {
		return "", func() {}, apperror.NewInternal("failed to build archive request", err)
	}
	if token != "" {
		req.Header.Set("Authorization", "token "+token)
	}

	resp, err := f.client.Do(req)
	if err != nil {
		return "", func() {}, fetchFailed("failed to download repository archive", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		return "", func() {}, fetchFailed("repository or ref not found (private repositories require a token)", nil)
	}
	if resp.StatusCode != http.StatusOK {
		return "", func() {}, fetchFailed(fmt.Sprintf("unexpected HTTP %d from codeload.github.com", resp.StatusCode), nil)
	}

	tmpDir, err := os.MkdirTemp("", "memory-blueprints-import-*")
	if err != nil {
		return "", func() {}, apperror.NewInternal("failed to create temp directory", err)
	}
	cleanup := func() { _ = os.RemoveAll(tmpDir) }

	// Cap the download as it streams to disk so a hostile/oversized tarball
	// cannot exhaust memory or disk.
	archiveFile, err := os.CreateTemp(tmpDir, "archive-*.tar.gz")
	if err != nil {
		cleanup()
		return "", func() {}, apperror.NewInternal("failed to create temp archive", err)
	}
	archivePath := archiveFile.Name()
	n, copyErr := io.Copy(archiveFile, io.LimitReader(resp.Body, f.maxArchiveBytes+1))
	closeErr := archiveFile.Close()
	if copyErr != nil {
		cleanup()
		return "", func() {}, fetchFailed("failed to read repository archive", copyErr)
	}
	if closeErr != nil {
		cleanup()
		return "", func() {}, apperror.NewInternal("failed to store repository archive", closeErr)
	}
	if n > f.maxArchiveBytes {
		cleanup()
		return "", func() {}, archiveTooLarge()
	}

	in, err := os.Open(archivePath)
	if err != nil {
		cleanup()
		return "", func() {}, apperror.NewInternal("failed to open repository archive", err)
	}
	defer in.Close()

	extractDir := filepath.Join(tmpDir, "extracted")
	if err := os.MkdirAll(extractDir, 0o755); err != nil {
		cleanup()
		return "", func() {}, apperror.NewInternal("failed to create extraction directory", err)
	}
	if err := extractTarGzSafe(in, extractDir, f.maxFiles, f.maxExtractBytes); err != nil {
		cleanup()
		return "", func() {}, err
	}

	root, err := findTopLevelDir(extractDir)
	if err != nil {
		cleanup()
		return "", func() {}, apperror.NewBadRequest("invalid repository archive: no blueprint files")
	}
	return root, cleanup, nil
}

// extractTarGzSafe extracts a gzipped tar stream under dest, rejecting path
// traversal, absolute paths, and symlinks/hardlinks, and enforcing entry-count
// and total-byte caps. Regular files and directories only.
func extractTarGzSafe(r io.Reader, dest string, maxFiles int, maxBytes int64) error {
	gz, err := gzip.NewReader(r)
	if err != nil {
		return apperror.NewBadRequest("invalid repository archive: not a gzip stream")
	}
	defer gz.Close()

	tr := tar.NewReader(gz)
	files := 0
	var total int64

	for {
		hdr, err := tr.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return apperror.NewBadRequest("invalid repository archive: corrupt tar")
		}

		name := strings.TrimPrefix(filepath.Clean(hdr.Name), string(os.PathSeparator))
		name = strings.TrimPrefix(name, "./")
		if name == "" || name == "." {
			continue
		}
		// Reject absolute paths and any traversal outside the extraction root.
		if filepath.IsAbs(hdr.Name) || !filepath.IsLocal(name) {
			return apperror.NewBadRequest("unsafe repository archive: path traversal")
		}
		target := filepath.Join(dest, name)
		if target != dest && !strings.HasPrefix(target, dest+string(os.PathSeparator)) {
			return apperror.NewBadRequest("unsafe repository archive: path traversal")
		}

		switch hdr.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(target, 0o755); err != nil {
				return apperror.NewInternal("failed to create archive directory", err)
			}
		case tar.TypeReg, tar.TypeRegA:
			files++
			if files > maxFiles {
				return archiveTooLarge()
			}
			total += hdr.Size
			if total > maxBytes {
				return archiveTooLarge()
			}
			if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
				return apperror.NewInternal("failed to create archive parent directory", err)
			}
			out, err := os.OpenFile(target, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o644)
			if err != nil {
				return apperror.NewInternal("failed to create extracted file", err)
			}
			if _, err := io.Copy(out, io.LimitReader(tr, hdr.Size)); err != nil { //nolint:gosec
				_ = out.Close()
				return apperror.NewInternal("failed to write extracted file", err)
			}
			if err := out.Close(); err != nil {
				return apperror.NewInternal("failed to close extracted file", err)
			}
		case tar.TypeSymlink, tar.TypeLink:
			// No links at all: an escaping symlink/hardlink is the classic
			// archive-escape vector.
			return apperror.NewBadRequest("unsafe repository archive: links are not allowed")
		default:
			// Char/block/fifo and other special entries are ignored.
		}
	}
	return nil
}

// findTopLevelDir returns the single subdirectory of dir (GitHub tarballs wrap
// their contents in <repo>-<ref>/), or dir itself when there is not exactly one.
func findTopLevelDir(dir string) (string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return "", err
	}
	var dirs []string
	for _, e := range entries {
		if e.IsDir() {
			dirs = append(dirs, filepath.Join(dir, e.Name()))
		}
	}
	if len(dirs) == 1 {
		return dirs[0], nil
	}
	return dir, nil
}

// ──────────────────────────────────────────────
// Manifest loading
// ──────────────────────────────────────────────

// blueprintDescriptor is a BlueprintManifest plus the blueprint-record metadata
// (name/version/description/author) derived from the imported repository. The
// embedded manifest fields are inlined on JSON/YAML decode.
type blueprintDescriptor struct {
	Name        string `json:"name,omitempty"`
	Version     string `json:"version,omitempty"`
	Description string `json:"description,omitempty"`
	Author      string `json:"author,omitempty"`
	BlueprintManifest
}

// loadBlueprintDir loads a validated blueprint descriptor from an extracted
// repository root. A repo-root manifest file wins; otherwise the
// `memory blueprints` directory layout is loaded. fallbackName is used as the
// blueprint name when the contents carry none (e.g. a seed-only blueprint) and
// is the repo name.
func loadBlueprintDir(root, fallbackName string) (*blueprintDescriptor, error) {
	for _, name := range manifestFileNames {
		path := filepath.Join(root, name)
		data, err := os.ReadFile(path)
		if err != nil {
			if errors.Is(err, os.ErrNotExist) {
				continue
			}
			return nil, apperror.NewBadRequest("failed to read blueprint manifest")
		}
		var desc blueprintDescriptor
		if err := decodeManifestFile(path, data, &desc); err != nil {
			return nil, apperror.NewBadRequest("invalid blueprint manifest: " + err.Error())
		}
		if err := finalizeDescriptor(&desc, fallbackName); err != nil {
			return nil, err
		}
		return &desc, nil
	}
	return loadBlueprintLayout(root, fallbackName)
}

// loadBlueprintLayout loads the `memory blueprints` directory layout:
// schemas/ (or packs/) packs, agents/, skills/<name>/SKILL.md, and
// seed/{objects,relationships}/*.jsonl.
func loadBlueprintLayout(root, fallbackName string) (*blueprintDescriptor, error) {
	desc := &blueprintDescriptor{}

	packs, err := loadPackDir(resolveSchemasDir(root))
	if err != nil {
		return nil, err
	}
	desc.Packs = packs

	agents, err := loadAgentDir(filepath.Join(root, "agents"))
	if err != nil {
		return nil, err
	}
	desc.Agents = agents

	skills, err := loadSkillDir(filepath.Join(root, "skills"))
	if err != nil {
		return nil, err
	}
	desc.Skills = skills

	objects, err := loadSeedObjects(filepath.Join(root, "seed", "objects"))
	if err != nil {
		return nil, err
	}
	rels, err := loadSeedRelationships(filepath.Join(root, "seed", "relationships"))
	if err != nil {
		return nil, err
	}
	if len(objects) > 0 || len(rels) > 0 {
		desc.Seed = &SeedManifest{Objects: objects, Relationships: rels}
	}

	if err := finalizeDescriptor(desc, fallbackName); err != nil {
		return nil, err
	}
	return desc, nil
}

// finalizeDescriptor derives missing record metadata and validates the manifest.
func finalizeDescriptor(desc *blueprintDescriptor, fallbackName string) error {
	if len(desc.Packs) == 0 && len(desc.Agents) == 0 && len(desc.Skills) == 0 && desc.Seed == nil {
		return apperror.NewBadRequest("invalid blueprint: repository contains no blueprint files")
	}
	for i := range desc.Packs {
		p := &desc.Packs[i]
		if p.Name == "" || p.Version == "" || len(p.ObjectTypes) == 0 {
			return apperror.NewBadRequest("invalid blueprint manifest: each pack requires name, version, and objectTypes")
		}
	}
	for i := range desc.Agents {
		if desc.Agents[i].Name == "" {
			return apperror.NewBadRequest("invalid blueprint manifest: each agent requires a name")
		}
	}
	if desc.Name == "" {
		switch {
		case len(desc.Packs) > 0:
			desc.Name = desc.Packs[0].Name
			desc.Version = desc.Packs[0].Version
			desc.Description = desc.Packs[0].Description
			desc.Author = desc.Packs[0].Author
		case len(desc.Agents) > 0:
			desc.Name = desc.Agents[0].Name
		case len(desc.Skills) > 0:
			desc.Name = desc.Skills[0].Name
		default:
			desc.Name = fallbackName
		}
	}
	if desc.Version == "" {
		desc.Version = "1.0.0"
	}
	if desc.Name == "" || desc.Version == "" {
		return apperror.NewBadRequest("invalid blueprint: could not determine a name and version")
	}
	return nil
}

// resolveSchemasDir prefers schemas/ and falls back to packs/.
func resolveSchemasDir(dir string) string {
	schemasDir := filepath.Join(dir, "schemas")
	if info, err := os.Stat(schemasDir); err == nil && info.IsDir() {
		return schemasDir
	}
	return filepath.Join(dir, "packs")
}

// loadPackDir parses every *.json|*.yaml|*.yml pack file in dir.
func loadPackDir(dir string) ([]PackManifest, error) {
	paths, err := listDataFiles(dir)
	if err != nil {
		return nil, err
	}
	var packs []PackManifest
	for _, path := range paths {
		var p PackManifest
		if err := decodeDataFile(path, &p); err != nil {
			return nil, apperror.NewBadRequest("invalid pack " + filepath.Base(path) + ": " + err.Error())
		}
		packs = append(packs, p)
	}
	return packs, nil
}

// loadAgentDir parses every *.json|*.yaml|*.yml agent file in dir.
func loadAgentDir(dir string) ([]AgentManifest, error) {
	paths, err := listDataFiles(dir)
	if err != nil {
		return nil, err
	}
	var agents []AgentManifest
	for _, path := range paths {
		var a AgentManifest
		if err := decodeDataFile(path, &a); err != nil {
			return nil, apperror.NewBadRequest("invalid agent " + filepath.Base(path) + ": " + err.Error())
		}
		agents = append(agents, a)
	}
	return agents, nil
}

// loadSkillDir parses each <name>/SKILL.md subdirectory under dir.
func loadSkillDir(dir string) ([]SkillManifest, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, apperror.NewBadRequest("failed to read skills directory")
	}
	var skills []SkillManifest
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		path := filepath.Join(dir, e.Name(), "SKILL.md")
		data, err := os.ReadFile(path)
		if err != nil {
			if errors.Is(err, os.ErrNotExist) {
				continue
			}
			return nil, apperror.NewBadRequest("failed to read skill " + e.Name())
		}
		sk, err := parseSkillMarkdown(data)
		if err != nil {
			return nil, apperror.NewBadRequest("invalid skill " + e.Name() + ": " + err.Error())
		}
		skills = append(skills, *sk)
	}
	return skills, nil
}

// loadSeedObjects parses every *.jsonl file in dir into SeedObjectRecord values.
func loadSeedObjects(dir string) ([]SeedObjectRecord, error) {
	var out []SeedObjectRecord
	err := eachJSONLine(dir, func(line []byte) error {
		var rec SeedObjectRecord
		if err := json.Unmarshal(line, &rec); err != nil {
			return err
		}
		if rec.Type == "" {
			return fmt.Errorf("seed object requires a type")
		}
		out = append(out, rec)
		return nil
	})
	return out, err
}

// loadSeedRelationships parses every *.jsonl file in dir into
// SeedRelationshipRecord values.
func loadSeedRelationships(dir string) ([]SeedRelationshipRecord, error) {
	var out []SeedRelationshipRecord
	err := eachJSONLine(dir, func(line []byte) error {
		var rec SeedRelationshipRecord
		if err := json.Unmarshal(line, &rec); err != nil {
			return err
		}
		if rec.Type == "" || rec.SrcKey == "" || rec.DstKey == "" {
			return fmt.Errorf("seed relationship requires type, srcKey, and dstKey")
		}
		out = append(out, rec)
		return nil
	})
	return out, err
}

// ──────────────────────────────────────────────
// Decode helpers
// ──────────────────────────────────────────────

// listDataFiles returns the sorted *.json|*.yaml|*.yml files directly under
// dir. A missing directory is not an error.
func listDataFiles(dir string) ([]string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, apperror.NewBadRequest("failed to read blueprint directory")
	}
	var paths []string
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		switch strings.ToLower(filepath.Ext(e.Name())) {
		case ".json", ".yaml", ".yml":
			paths = append(paths, filepath.Join(dir, e.Name()))
		}
	}
	return paths, nil
}

// decodeDataFile decodes a JSON or YAML file into v. YAML is round-tripped
// through JSON so the camelCase manifest field names decode uniformly.
func decodeDataFile(path string, v any) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	return decodeManifestFile(path, data, v)
}

func decodeManifestFile(path string, data []byte, v any) error {
	if strings.EqualFold(filepath.Ext(path), ".json") {
		return json.Unmarshal(data, v)
	}
	var raw any
	if err := yaml.Unmarshal(data, &raw); err != nil {
		return err
	}
	b, err := json.Marshal(raw)
	if err != nil {
		return err
	}
	return json.Unmarshal(b, v)
}

// parseSkillMarkdown splits a SKILL.md file into YAML frontmatter (decoded into
// a SkillManifest) and the Markdown body (Content).
func parseSkillMarkdown(data []byte) (*SkillManifest, error) {
	const delim = "---"
	trimmed := strings.TrimSpace(string(data))
	if !strings.HasPrefix(trimmed, delim) {
		return nil, errors.New("missing YAML frontmatter")
	}
	rest := strings.TrimPrefix(trimmed, delim)
	rest = strings.TrimPrefix(strings.TrimPrefix(rest, "\r"), "\n")
	closeIdx := strings.Index(rest, "\n"+delim)
	if closeIdx < 0 {
		return nil, errors.New("unterminated YAML frontmatter")
	}
	front := rest[:closeIdx]
	body := strings.TrimPrefix(rest[closeIdx+1+len(delim):], "\r")
	body = strings.TrimPrefix(body, "\n")

	var sk SkillManifest
	if err := decodeManifestFile("SKILL.md", []byte(front), &sk); err != nil {
		return nil, err
	}
	if sk.Name == "" || sk.Description == "" {
		return nil, errors.New("skill requires name and description")
	}
	sk.Content = body
	return &sk, nil
}

// eachJSONLine reads every *.jsonl file in dir and invokes fn for each
// non-empty line. A missing directory is not an error.
func eachJSONLine(dir string, fn func([]byte) error) error {
	entries, err := os.ReadDir(dir)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return apperror.NewBadRequest("failed to read seed directory")
	}
	for _, e := range entries {
		if e.IsDir() || !strings.EqualFold(filepath.Ext(e.Name()), ".jsonl") {
			continue
		}
		data, err := os.ReadFile(filepath.Join(dir, e.Name()))
		if err != nil {
			return apperror.NewBadRequest("failed to read seed file " + e.Name())
		}
		for _, raw := range strings.Split(string(data), "\n") {
			line := strings.TrimSpace(strings.TrimSuffix(raw, "\r"))
			if line == "" {
				continue
			}
			if err := fn([]byte(line)); err != nil {
				return apperror.NewBadRequest("invalid seed record in " + e.Name() + ": " + err.Error())
			}
		}
	}
	return nil
}

// ──────────────────────────────────────────────
// Error helpers (status mapping)
// ──────────────────────────────────────────────

// archiveTooLarge maps archive/extraction limit breaches to HTTP 413.
func archiveTooLarge() *apperror.Error {
	return apperror.New(http.StatusRequestEntityTooLarge, "archive_too_large", "blueprint archive exceeds the size limit")
}

// fetchFailed maps outbound fetch failures (including a redirect off the
// allowlist) to HTTP 502. Constructed directly (rather than via
// WithInternal) so call sites do not grow the apperror Style A lint-ratchet
// count, mirroring apperror.NewDatabase/NewValidation.
func fetchFailed(message string, err error) *apperror.Error {
	return &apperror.Error{
		HTTPStatus: http.StatusBadGateway,
		Code:       "fetch_failed",
		Message:    message,
		Internal:   err,
	}
}
