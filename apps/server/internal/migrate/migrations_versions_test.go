package migrate

import (
	"io/fs"
	"regexp"
	"sort"
	"strconv"
	"testing"

	"github.com/emergent-company/emergent.memory/migrations"
)

// migrationFilenameRe matches the required <5-digit version>_<kebab-or-snake name>.sql form.
var migrationFilenameRe = regexp.MustCompile(`^(\d{5})_[a-z0-9_]+\.sql$`)

// TestEmbeddedMigrationVersions guards the migration set against the failure mode that
// caused core.mcp_share_instances to be missing on some environments: two migrations
// sharing a version number. Goose de-duplicates by version, so a duplicate (or a
// renamed/reused version) means one migration silently never runs. Versions must be
// unique and correctly formatted; gaps are reported but tolerated.
func TestEmbeddedMigrationVersions(t *testing.T) {
	entries, err := fs.ReadDir(migrations.FS, ".")
	if err != nil {
		t.Fatalf("read embedded migrations: %v", err)
	}

	seen := make(map[int64]string, len(entries))
	versions := make([]int64, 0, len(entries))

	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()

		match := migrationFilenameRe.FindStringSubmatch(name)
		if match == nil {
			t.Errorf("migration %q does not match NNNNN_lowercase_name.sql", name)
			continue
		}

		version, err := strconv.ParseInt(match[1], 10, 64)
		if err != nil {
			t.Errorf("migration %q has an invalid version prefix: %v", name, err)
			continue
		}

		if previous, ok := seen[version]; ok {
			t.Errorf("duplicate migration version %05d used by %q and %q: versions must be unique because Goose de-duplicates by version number", version, previous, name)
			continue
		}
		seen[version] = name
		versions = append(versions, version)
	}

	if len(versions) == 0 {
		t.Fatal("no embedded migrations found")
	}

	sort.Slice(versions, func(i, j int) bool { return versions[i] < versions[j] })
	for i := 1; i < len(versions); i++ {
		if versions[i] != versions[i-1]+1 {
			t.Logf("note: gap between migration %05d and %05d (allowed, but confirm no migration was deleted)", versions[i-1], versions[i])
		}
	}

	t.Logf("validated %d unique migrations (%05d..%05d)", len(versions), versions[0], versions[len(versions)-1])
}
