package installer

import (
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

// The self-hosted compose topology exists in four places: the CLI's embedded
// template (what `memory install` writes), deploy/self-hosted/docker-compose.yml,
// its dev variant docker-compose.local.yml, and the heredoc inside
// deploy/self-hosted/install-online.sh. They are copies by construction, so a
// healthcheck edit made in one and forgotten in the others silently reintroduces
// the failure the check was added for — which is exactly what happened with the
// object-store readiness race in #2004 (issue #2038).
//
// This test pins them together: every service that appears in more than one
// source must carry the same healthcheck there.

// composeHealthcheckFields are compared as a presence map: a dropped
// `start_period` or `retries` is drift, not an absent-vs-zero detail.
type composeHealthcheck struct {
	Test        any    `yaml:"test"`
	Interval    string `yaml:"interval"`
	Timeout     string `yaml:"timeout"`
	StartPeriod string `yaml:"start_period"`
	Retries     *int   `yaml:"retries"`
	Disable     bool   `yaml:"disable"`
}

type composeService struct {
	Healthcheck *composeHealthcheck `yaml:"healthcheck"`
}

type composeDocument struct {
	Services map[string]composeService `yaml:"services"`
}

type composeSource struct {
	name string
	body string
}

// composeSourceExemptions lists healthchecks that are deliberately different in
// one source, so the test only fails on accidental drift. Every entry needs a
// reason.
var composeSourceExemptions = map[string]map[string]string{
	"deploy/self-hosted/docker-compose.local.yml": {
		"server": "the dev server binds a configurable ${SERVER_PORT:-3002} and probes it with wget",
	},
}

func TestSelfHostedComposeHealthcheckDrift(t *testing.T) {
	root := repoRootWithDeploy(t)
	sources := []composeSource{
		{"CLI template (GetDockerComposeTemplate)", GetDockerComposeTemplate()},
		{"CLI template (GetDockerComposeTemplateWithVersion)", GetDockerComposeTemplateWithVersion("v0.0.0-test")},
		{"deploy/self-hosted/docker-compose.yml", readFileString(t, filepath.Join(root, "deploy", "self-hosted", "docker-compose.yml"))},
		{"deploy/self-hosted/docker-compose.local.yml", readFileString(t, filepath.Join(root, "deploy", "self-hosted", "docker-compose.local.yml"))},
		{"deploy/self-hosted/install-online.sh heredoc", composeHeredocFromInstallScript(t, root)},
	}

	// service -> source name -> canonical healthcheck
	seen := map[string]map[string]map[string]string{}
	for _, src := range sources {
		var doc composeDocument
		if err := yaml.Unmarshal([]byte(src.body), &doc); err != nil {
			t.Fatalf("%s: parse compose document: %v", src.name, err)
		}
		if len(doc.Services) == 0 {
			t.Fatalf("%s: parsed no services — the source is not a compose document", src.name)
		}
		for name, svc := range doc.Services {
			if seen[name] == nil {
				seen[name] = map[string]map[string]string{}
			}
			seen[name][src.name] = canonicalHealthcheck(svc.Healthcheck)
		}
	}

	// The object store is the service #2004 was about: it must be present in
	// every source, otherwise this test could pass by comparing nothing.
	objectStoreSources := seen["seaweedfs"]
	if len(objectStoreSources) != len(sources) {
		t.Fatalf("service seaweedfs is defined in %d of %d compose sources; it must be in all of them so its readiness probe is compared everywhere",
			len(objectStoreSources), len(sources))
	}

	names := make([]string, 0, len(seen))
	for name := range seen {
		names = append(names, name)
	}
	sort.Strings(names)

	for _, service := range names {
		bySource := seen[service]
		if len(bySource) < 2 {
			continue // a service only one source defines cannot drift
		}
		sourceNames := make([]string, 0, len(bySource))
		for src := range bySource {
			sourceNames = append(sourceNames, src)
		}
		sort.Strings(sourceNames)

		var compared []string
		for _, src := range sourceNames {
			if reason, exempt := composeSourceExemptions[src][service]; exempt {
				t.Logf("service %s: skipping %s (exempt: %s)", service, src, reason)
				continue
			}
			compared = append(compared, src)
		}
		if len(compared) < 2 {
			continue
		}

		want := bySource[compared[0]]
		for _, src := range compared[1:] {
			got := bySource[src]
			if reflect.DeepEqual(want, got) {
				continue
			}
			t.Errorf("service %s: healthcheck drifted between %s and %s:\n  %s: %s\n  %s: %s",
				service, compared[0], src,
				compared[0], formatHealthcheck(want),
				src, formatHealthcheck(got))
		}
	}
}

// canonicalHealthcheck reduces a healthcheck to the field set the copies must
// agree on, as strings, keeping absence distinct from an empty value.
func canonicalHealthcheck(hc *composeHealthcheck) map[string]string {
	out := map[string]string{}
	if hc == nil {
		return out
	}
	if test := canonicalTest(hc.Test); test != "" {
		out["test"] = test
	}
	if hc.Interval != "" {
		out["interval"] = hc.Interval
	}
	if hc.Timeout != "" {
		out["timeout"] = hc.Timeout
	}
	if hc.StartPeriod != "" {
		out["start_period"] = hc.StartPeriod
	}
	if hc.Retries != nil {
		out["retries"] = strconv.Itoa(*hc.Retries)
	}
	if hc.Disable {
		out["disable"] = "true"
	}
	return out
}

// canonicalTest renders either accepted YAML shape — a shell string or an argv
// list — as one comparable string.
func canonicalTest(test any) string {
	switch value := test.(type) {
	case nil:
		return ""
	case string:
		return value
	case []any:
		parts := make([]string, 0, len(value))
		for _, part := range value {
			parts = append(parts, fmt.Sprint(part))
		}
		return strings.Join(parts, "\x00")
	default:
		return fmt.Sprint(value)
	}
}

func formatHealthcheck(hc map[string]string) string {
	if len(hc) == 0 {
		return "(none)"
	}
	keys := make([]string, 0, len(hc))
	for key := range hc {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	parts := make([]string, 0, len(keys))
	for _, key := range keys {
		parts = append(parts, key+"="+hc[key])
	}
	return strings.Join(parts, " ")
}

// composeHeredocFromInstallScript extracts the compose document the shell
// installer writes, so the copy embedded in the script is compared too.
func composeHeredocFromInstallScript(t *testing.T, root string) string {
	t.Helper()
	script := readFileString(t, filepath.Join(root, "deploy", "self-hosted", "install-online.sh"))
	const marker = `cat > "${INSTALL_DIR}/docker/docker-compose.yml" <<EOF`
	start := strings.Index(script, marker)
	if start < 0 {
		t.Fatalf("install-online.sh: compose heredoc marker %q not found — if the heredoc was rewritten, update this test so the embedded copy stays covered", marker)
	}
	rest := script[start+len(marker):]
	if newline := strings.IndexByte(rest, '\n'); newline >= 0 {
		rest = rest[newline+1:]
	}
	end := strings.Index(rest, "\nEOF\n")
	if end < 0 {
		t.Fatal("install-online.sh: compose heredoc has no terminating EOF line")
	}
	// The heredoc is shell-expanded (`<<EOF`, not `<<'EOF'`), so a `\$` in the
	// script is a literal `$` in the written file. Undo that escape before
	// parsing, or every compose variable would read as drift.
	return strings.ReplaceAll(rest[:end], `\$`, "$")
}

func readFileString(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return string(data)
}

// repoRootWithDeploy walks up from the test file to the repository root, which is
// where deploy/self-hosted/ lives.
func repoRootWithDeploy(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "deploy", "self-hosted", "docker-compose.yml")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("repository root (deploy/self-hosted) not found above the test file")
		}
		dir = parent
	}
}

// TestInstallScriptComposeUpDropsOrphans pins the shell installer to the same
// `up --remove-orphans` contract as `memory server upgrade`
// (DockerManager.upArgs): a service removed between releases must not keep
// running, holding its port and memory, because one of the two entry points
// forgot the flag (#2039).
func TestInstallScriptComposeUpDropsOrphans(t *testing.T) {
	root := repoRootWithDeploy(t)
	script := readFileString(t, filepath.Join(root, "deploy", "self-hosted", "install-online.sh"))

	checked := 0
	for index, line := range strings.Split(script, "\n") {
		command := strings.TrimSpace(line)
		if !strings.HasPrefix(command, "docker compose") || !strings.Contains(command, "up -d") {
			continue
		}
		checked++
		if !strings.Contains(command, "--remove-orphans") {
			t.Errorf("install-online.sh:%d: `docker compose ... up -d` without --remove-orphans leaves removed services running as orphans: %s", index+1, command)
		}
	}
	if checked == 0 {
		t.Fatal("install-online.sh: no `docker compose ... up -d` invocation found — if the script was restructured, update this test so the orphan contract stays covered")
	}
}
