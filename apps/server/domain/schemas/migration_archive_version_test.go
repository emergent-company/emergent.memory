package schemas

import "testing"

// TestResolveArchiveVersion covers the service-level resolution that feeds
// MigrateObject's archive keys. The rollback path queries archives with the
// human pack version, so the archive to_version MUST equal the human version
// returned here, never the schema UUID.
func TestResolveArchiveVersion(t *testing.T) {
	t.Run("resolves to pack human version", func(t *testing.T) {
		versions := map[string]string{
			"11111111-1111-1111-1111-111111111111": "1.0.0",
			"22222222-2222-2222-2222-222222222222": "2.0.0",
		}
		lookup := func(id string) string { return versions[id] }

		from := resolveArchiveVersion("11111111-1111-1111-1111-111111111111", lookup)
		to := resolveArchiveVersion("22222222-2222-2222-2222-222222222222", lookup)

		if from != "1.0.0" {
			t.Fatalf("from_version = %q, want %q", from, "1.0.0")
		}
		if to != "2.0.0" {
			t.Fatalf("to_version = %q, want %q", to, "2.0.0")
		}
		// The archive key the rollback will query must be the human version.
		if to == "22222222-2222-2222-2222-222222222222" {
			t.Fatal("to_version must not be a schema UUID")
		}
	})

	t.Run("falls back to schema ID when lookup fails", func(t *testing.T) {
		lookup := func(string) string { return "" }
		got := resolveArchiveVersion("33333333-3333-3333-3333-333333333333", lookup)
		if got != "33333333-3333-3333-3333-333333333333" {
			t.Fatalf("expected ID fallback, got %q", got)
		}
	})

	t.Run("empty schema ID yields empty key", func(t *testing.T) {
		lookup := func(string) string { return "9.9.9" }
		if got := resolveArchiveVersion("", lookup); got != "" {
			t.Fatalf("expected empty key for empty schema ID, got %q", got)
		}
	})
}
