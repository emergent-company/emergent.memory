package blueprints

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"os"
	"path/filepath"
	"testing"
)

type archiveEntry struct {
	name     string
	typeflag byte
	body     []byte
	linkname string
}

func buildArchiveBytes(t *testing.T, entries []archiveEntry, compress bool) []byte {
	t.Helper()
	var buf bytes.Buffer
	var target *tar.Writer
	var gz *gzip.Writer
	if compress {
		gz = gzip.NewWriter(&buf)
		target = tar.NewWriter(gz)
	} else {
		target = tar.NewWriter(&buf)
	}
	for _, e := range entries {
		if err := target.WriteHeader(&tar.Header{
			Name:     e.name,
			Mode:     0o644,
			Size:     int64(len(e.body)),
			Typeflag: e.typeflag,
			Linkname: e.linkname,
		}); err != nil {
			t.Fatalf("write header: %v", err)
		}
		if len(e.body) > 0 {
			if _, err := target.Write(e.body); err != nil {
				t.Fatalf("write body: %v", err)
			}
		}
	}
	if err := target.Close(); err != nil {
		t.Fatalf("close tar: %v", err)
	}
	if gz != nil {
		if err := gz.Close(); err != nil {
			t.Fatalf("close gzip: %v", err)
		}
	}
	return buf.Bytes()
}

func TestExtractCanonicalTarAcceptsRegularFilesAndDirs(t *testing.T) {
	for _, compress := range []bool{false, true} {
		dir := t.TempDir()
		data := buildArchiveBytes(t, []archiveEntry{
			{name: "blueprint.json", typeflag: tar.TypeReg, body: []byte(`{"name":"x","version":"1.0.0"}`)},
			{name: "skills/demo/SKILL.md", typeflag: tar.TypeReg, body: []byte("# demo")},
			{name: "skills/demo", typeflag: tar.TypeDir},
		}, compress)
		if err := ExtractCanonicalTar(bytes.NewReader(data), dir); err != nil {
			t.Fatalf("compress=%v: %v", compress, err)
		}
		if _, err := os.Stat(filepath.Join(dir, "skills/demo/SKILL.md")); err != nil {
			t.Fatalf("expected extracted file: %v", err)
		}
	}
}

func TestExtractCanonicalTarRejectsMaliciousEntries(t *testing.T) {
	cases := map[string][]archiveEntry{
		"absolute":  {{name: "/etc/passwd", typeflag: tar.TypeReg, body: []byte("x")}},
		"traversal": {{name: "../escape", typeflag: tar.TypeReg, body: []byte("x")}},
		"symlink":   {{name: "link", typeflag: tar.TypeSymlink, linkname: "/etc/passwd"}},
		"hardlink":  {{name: "hard", typeflag: tar.TypeLink, linkname: "target"}},
		"fifo":      {{name: "pipe", typeflag: tar.TypeFifo}},
		"device":    {{name: "dev", typeflag: tar.TypeChar}},
	}
	for name, entries := range cases {
		t.Run(name, func(t *testing.T) {
			root := t.TempDir()
			dest := filepath.Join(root, "out")
			if err := os.Mkdir(dest, 0o755); err != nil {
				t.Fatalf("mkdir: %v", err)
			}
			data := buildArchiveBytes(t, entries, false)
			if err := ExtractCanonicalTar(bytes.NewReader(data), dest); err == nil {
				t.Fatalf("expected %s archive to be refused", name)
			}
			if _, err := os.Stat(filepath.Join(root, "escape")); err == nil {
				t.Fatalf("traversal wrote outside the extraction root")
			}
		})
	}
}

func TestExtractArchiveEnforcesEntryAndByteLimits(t *testing.T) {
	dir := t.TempDir()
	data := buildArchiveBytes(t, []archiveEntry{
		{name: "a", typeflag: tar.TypeReg, body: []byte("aaa")},
		{name: "b", typeflag: tar.TypeReg, body: []byte("bbb")},
	}, false)
	if err := extractArchiveSafe(bytes.NewReader(data), dir, 1, 1<<20); err == nil {
		t.Fatalf("expected entry-count limit to refuse the archive")
	}

	dir2 := t.TempDir()
	if err := extractArchiveSafe(bytes.NewReader(data), dir2, 10, 4); err == nil {
		t.Fatalf("expected byte limit to refuse the archive")
	}
}

// GitHub's codeload tarballs open with a PAX global header — a
// `pax_global_header` entry carrying the commit id — and archive/tar returns it
// from Next() like any other entry. Refusing it as a non-regular entry made
// every web-UI "Import from GitHub" fail with
// `unsafe archive: non-regular entries are not allowed`, so a real archive
// shape has to extract, and the metadata must not land on disk.
func TestExtractArchiveSkipsPaxGlobalHeader(t *testing.T) {
	var buf bytes.Buffer
	tw := tar.NewWriter(&buf)
	if err := tw.WriteHeader(&tar.Header{
		Typeflag:   tar.TypeXGlobalHeader,
		Name:       "pax_global_header",
		Format:     tar.FormatPAX,
		PAXRecords: map[string]string{"comment": "a1b2c3d4e5f6"},
	}); err != nil {
		t.Fatalf("write global header: %v", err)
	}
	body := []byte(`{"name":"personal-memory","version":"4.1.1"}`)
	if err := tw.WriteHeader(&tar.Header{
		Name:     "personal-memory-blueprint-main/project.yaml",
		Mode:     0o644,
		Size:     int64(len(body)),
		Typeflag: tar.TypeReg,
	}); err != nil {
		t.Fatalf("write file header: %v", err)
	}
	if _, err := tw.Write(body); err != nil {
		t.Fatalf("write body: %v", err)
	}
	if err := tw.Close(); err != nil {
		t.Fatalf("close tar: %v", err)
	}

	dir := t.TempDir()
	if err := ExtractCanonicalTar(bytes.NewReader(buf.Bytes()), dir); err != nil {
		t.Fatalf("archive with a pax global header was refused: %v", err)
	}
	got, err := os.ReadFile(filepath.Join(dir, "personal-memory-blueprint-main/project.yaml"))
	if err != nil {
		t.Fatalf("expected the regular entry to be extracted: %v", err)
	}
	if string(got) != string(body) {
		t.Fatalf("extracted body = %q, want %q", got, body)
	}
	if _, err := os.Stat(filepath.Join(dir, "pax_global_header")); err == nil {
		t.Fatalf("the pax global header was written to disk as a file")
	}
}
