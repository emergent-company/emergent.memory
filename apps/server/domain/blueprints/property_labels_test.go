package blueprints

import (
	"encoding/json"
	"io/fs"
	"os"
	"path"
	"regexp"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// machineKey matches a property key that a reader cannot scan at a glance: a
// snake_case key or one with an internal capital ("homepage_url" → "Homepage
// URL", "tokensUsed" → "Tokens used"). Single lower-case words ("status",
// "title") are already readable and take no label.
var machineKey = regexp.MustCompile(`_|[a-z][A-Z]`)

// TestShippedPackPropertyLabels guards the labels the built-in and example
// blueprint packs ship, because a pack is what most projects' schemas are made
// of: a missing or lazy label here reaches every user of the pack, and only
// renders once the pack is re-applied.
//
// It asserts the three rules the property-label renderer relies on:
//   - a declared title is a non-empty string (an empty one reads as "no label"
//     to the renderer but as "labelled" to a reviewer);
//   - no title repeats its key, ignoring case — that renders the name twice;
//   - titles are unique within a type, so two rows cannot read identically.
//
// It also ratchets the property the feature exists for: every machine-ish key
// (snake_case or internal capital) declares a label, so a new property cannot
// be added to a shipped pack without one.
//
// Decoding goes through decodeManifestFile into PackManifest — the same
// YAML→JSON→manifest path an install uses — and the labels are then re-checked
// in the packSchemaJSON payload the registry stores, so a label cannot pass here
// and still be dropped before the UI sees it.
func TestShippedPackPropertyLabels(t *testing.T) {
	type source struct {
		name string
		fsys fs.FS
		root string
	}
	sources := []source{{name: "embedded builtin packs", fsys: builtinPacksFS, root: "builtin_packs"}}

	// The example packs are installed from the checkout rather than embedded in
	// the binary; guard them when the checkout is present.
	const exampleRoot = "../../../../blueprints"
	if _, err := os.Stat(exampleRoot); err == nil {
		sources = append(sources, source{name: "example packs", fsys: os.DirFS(exampleRoot), root: "."})
	} else {
		t.Logf("example packs not found at %s; guarding embedded packs only", exampleRoot)
	}

	packsChecked := 0
	for _, src := range sources {
		err := fs.WalkDir(src.fsys, src.root, func(p string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if d.IsDir() || !strings.HasSuffix(p, ".yaml") {
				return nil
			}
			// Only schema packs declare properties; a blueprint's project.yaml,
			// agent definitions and skill files live beside them.
			if !strings.Contains(p, "/schemas/") && !strings.Contains(p, "/packs/") {
				return nil
			}
			data, readErr := fs.ReadFile(src.fsys, p)
			require.NoError(t, readErr)
			var pack PackManifest
			require.NoErrorf(t, decodeManifestFile(p, data, &pack), "%s: %s must decode as a pack", src.name, p)
			require.NotEmptyf(t, pack.ObjectTypes, "%s: %s declares no object types", src.name, p)

			for _, objType := range pack.ObjectTypes {
				titles := assertPackPropertyLabels(t, path.Join(src.name, p), objType)

				// The registry stores exactly the JSON packSchemaJSON produces,
				// so check the labels survive that hop too: a label that passes
				// the rules above but is dropped on the way to kb.graph_schemas
				// would never reach the UI.
				objectTypesJSON, _, jsonErr := packSchemaJSON(pack)
				require.NoErrorf(t, jsonErr, "%s: %s must produce a pack schema payload", src.name, p)
				assertTitlesSurviveRegistryJSON(t, path.Join(src.name, p), objectTypesJSON, objType.Name, titles)
			}
			packsChecked++
			return nil
		})
		require.NoError(t, err)
	}

	// A silent walk or a decode that dropped the types would make this guard
	// vacuous.
	require.Greater(t, packsChecked, 5, "expected to guard every schema pack, found %d", packsChecked)
}

func assertPackPropertyLabels(t *testing.T, where string, objType ObjectTypeDef) map[string]string {
	t.Helper()

	titles := make(map[string]string, len(objType.Properties))
	seen := make(map[string]string, len(objType.Properties))
	for key, raw := range objType.Properties {
		subject := where + ": " + objType.Name + "." + key

		def, ok := raw.(map[string]any)
		require.Truef(t, ok, "%s: property definition must be an object, got %T", subject, raw)

		rawTitle, declared := def["title"]
		if !declared {
			if machineKey.MatchString(key) {
				assert.Failf(t, "unlabelled machine-ish property",
					"%s needs a title: %q cannot be read at a glance", subject, key)
			}
			continue
		}

		title, ok := rawTitle.(string)
		require.Truef(t, ok, "%s: title must be a string, got %T", subject, rawTitle)
		trimmed := strings.TrimSpace(title)
		assert.NotEmptyf(t, trimmed, "%s: empty title is not a label", subject)
		assert.Equalf(t, title, trimmed, "%s: title has surrounding whitespace", subject)
		assert.Falsef(t, strings.EqualFold(trimmed, key),
			"%s: title %q repeats its key, which the renderer collapses", subject, title)

		if other, dup := seen[strings.ToLower(trimmed)]; dup {
			assert.Failf(t, "duplicate label in one type", "%s: title %q is already used by %q", subject, title, other)
		}
		seen[strings.ToLower(trimmed)] = key
		titles[key] = title
	}
	return titles
}

// assertTitlesSurviveRegistryJSON checks that the labels just validated are
// still present in the payload handed to the schema registry (kb.graph_schemas
// stores it as jsonb, so whatever packSchemaJSON emits is what the UI reads).
func assertTitlesSurviveRegistryJSON(t *testing.T, where string, raw []byte, typeName string, titles map[string]string) {
	t.Helper()

	var stored []struct {
		Name       string                    `json:"name"`
		Properties map[string]map[string]any `json:"properties"`
	}
	require.NoErrorf(t, json.Unmarshal(raw, &stored), "%s: pack schema payload must be JSON", where)

	var found bool
	for _, objType := range stored {
		if objType.Name != typeName {
			continue
		}
		found = true
		for key, want := range titles {
			assert.Equalf(t, want, objType.Properties[key]["title"],
				"%s: %s.%s lost its label on the way to the registry", where, typeName, key)
		}
	}
	require.Truef(t, found, "%s: type %s is missing from the registry payload", where, typeName)
}
