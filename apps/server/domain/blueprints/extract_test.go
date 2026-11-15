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
