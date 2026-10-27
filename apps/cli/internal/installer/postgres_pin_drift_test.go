package installer

import (
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

// The Postgres pin exists in four places, like the compose topology in
// healthcheck_drift_test.go: the two constants here and the three static copies
// the CLI never rewrites for an existing install. A half-bump fails badly rather
// than loudly:
//
//   - an 18 runtime image with a 17 upgrade image upgrades the cluster to 17
//     while the server expects 18;
//   - a static copy left on pg17 keeps installs made from the compose files on
//     the old major, where no upgrade is ever triggered for them;
//   - an 18 image without PGDATA refuses to start against a volume mounted at
//     the legacy path, and a fresh install would initialise outside the volume —
//     the data would live in the container layer and vanish with it.
//
// Environment is any because the sources mix the mapping form (db) with the
// list form other services use; only the db service is inspected.
type composeDBService struct {
	Image       string   `yaml:"image"`
	Environment any      `yaml:"environment"`
	Volumes     []string `yaml:"volumes"`
}

// envValue reads one variable from either accepted compose environment shape.
func envValue(environment any, key string) string {
	switch env := environment.(type) {
	case map[string]any:
		if value, ok := env[key]; ok {
			return value.(string)
		}
	case []any:
		for _, entry := range env {
			if pair, ok := entry.(string); ok {
				if name, value, found := strings.Cut(pair, "="); found && name == key {
					return value
				}
			}
		}
	}
	return ""
}

func TestSelfHostedPostgresPinDrift(t *testing.T) {
	root := repoRootWithDeploy(t)
	sources := []composeSource{
		{"CLI template (GetDockerComposeTemplate)", GetDockerComposeTemplate()},
		{"deploy/self-hosted/docker-compose.yml", readFileString(t, filepath.Join(root, "deploy", "self-hosted", "docker-compose.yml"))},
		{"deploy/self-hosted/docker-compose.local.yml", readFileString(t, filepath.Join(root, "deploy", "self-hosted", "docker-compose.local.yml"))},
		{"deploy/self-hosted/install-online.sh heredoc", composeHeredocFromInstallScript(t, root)},
	}

	const pgData = "/var/lib/postgresql/data"

	for _, src := range sources {
		var doc struct {
			Services map[string]composeDBService `yaml:"services"`
		}
		if err := yaml.Unmarshal([]byte(src.body), &doc); err != nil {
			t.Fatalf("%s: parse compose document: %v", src.name, err)
		}
		db, ok := doc.Services["db"]
		if !ok {
			t.Fatalf("%s: no db service — the source is not the expected compose document", src.name)
		}

		if db.Image != PostgresImage {
			t.Errorf("%s: db.image = %q, want %q (bump the static copies together with PostgresImage)", src.name, db.Image, PostgresImage)
		}
		if got := envValue(db.Environment, "PGDATA"); got != pgData {
			t.Errorf("%s: db.environment.PGDATA = %q, want %q: the pg18 entrypoint refuses to start when the volume is mounted at the legacy path without it, and a fresh install would initialise outside the volume", src.name, got, pgData)
		}
		mounted := false
		for _, volume := range db.Volumes {
			if strings.HasSuffix(volume, ":"+pgData) {
				mounted = true
				break
			}
		}
		if !mounted {
			t.Errorf("%s: no volume mount ends in %q, so PGDATA names a directory that is not persisted", src.name, pgData)
		}
	}
}

// TestPostgresPinsAgreeOnMajorAndDistro keeps the runtime image, the in-place
// upgrade image and the major-version constant moving together.
func TestPostgresPinsAgreeOnMajorAndDistro(t *testing.T) {
	runtimeMatch := regexp.MustCompile(`:pg(\d+)-`).FindStringSubmatch(PostgresImage)
	if runtimeMatch == nil {
		t.Fatalf("PostgresImage = %q does not look like <repo>:pg<major>-<distro>", PostgresImage)
	}
	runtimeMajor, err := strconv.Atoi(runtimeMatch[1])
	if err != nil {
		t.Fatalf("parse major from PostgresImage %q: %v", PostgresImage, err)
	}
	if runtimeMajor != PostgresMajorVersion {
		t.Errorf("PostgresImage is major %d but PostgresMajorVersion is %d: the upgrade flow decides from the constant, so a half-bump either skips a needed upgrade or upgrades past what the image runs", runtimeMajor, PostgresMajorVersion)
	}

	upgradeMatch := regexp.MustCompile(`:(\d+)-`).FindStringSubmatch(pgUpgradeImage)
	if upgradeMatch == nil {
		t.Fatalf("pgUpgradeImage = %q does not look like <repo>:<major>-<distro>", pgUpgradeImage)
	}
	upgradeMajor, err := strconv.Atoi(upgradeMatch[1])
	if err != nil {
		t.Fatalf("parse major from pgUpgradeImage %q: %v", pgUpgradeImage, err)
	}
	if upgradeMajor != PostgresMajorVersion {
		t.Errorf("pgUpgradeImage is major %d but PostgresMajorVersion is %d", upgradeMajor, PostgresMajorVersion)
	}

	// pg_upgrade rebuilds every index with the upgrade image's libc. If the two
	// images disagree on the distro, the cluster ends up ordered by a collation
	// the running server does not use, and its recorded collation version names
	// a libc neither of them has.
	distro := func(image string) string {
		if i := strings.LastIndex(image, "-"); i >= 0 {
			return image[i+1:]
		}
		return ""
	}
	if runtimeDistro, upgradeDistro := distro(PostgresImage), distro(pgUpgradeImage); runtimeDistro != upgradeDistro {
		t.Errorf("PostgresImage runs %q but pgUpgradeImage is built on %q: keep the distros equal so pg_upgrade rebuilds the indexes under the libc the server actually uses", runtimeDistro, upgradeDistro)
	}
}
