package linux

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestPercentEncodePath(t *testing.T) {
	cases := map[string]string{
		"/home/u/My Documents/a+b.txt": "/home/u/My%20Documents/a%2Bb.txt",
		"/data/caf\u00e9.txt":          "/data/caf%C3%A9.txt",
		"/a/b!~*'()_.-":                "/a/b!~*'()_.-",
		"/100%done":                    "/100%25done",
		"/plain/path":                  "/plain/path",
	}
	for in, want := range cases {
		if got := percentEncodePath(in); got != want {
			t.Errorf("percentEncodePath(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestTrashInfoBodyExact(t *testing.T) {
	when := time.Date(2026, 9, 11, 14, 30, 5, 0, time.UTC)
	got := trashInfoBody("/home/u/My%20Documents/report.txt", when)
	want := "[Trash Info]\nPath=/home/u/My%20Documents/report.txt\nDeletionDate=2026-09-11T14:30:05\n"
	if got != want {
		t.Errorf("trashInfoBody =\n%q\nwant:\n%q", got, want)
	}
}

func TestUniqueTrashNameCollision(t *testing.T) {
	filesDir := t.TempDir()
	infoDir := t.TempDir()
	writeFile(t, filepath.Join(filesDir, "note.txt"), "x")

	name, err := uniqueTrashName(filesDir, infoDir, "note.txt")
	if err != nil {
		t.Fatalf("uniqueTrashName: %v", err)
	}
	if name != "note.txt.1" {
		t.Errorf("name = %q, want note.txt.1", name)
	}

	// An existing info file also forces a new name.
	writeFile(t, filepath.Join(filesDir, "note.txt.1"), "x")
	writeFile(t, filepath.Join(infoDir, "note.txt.1.trashinfo"), "x")
	name, err = uniqueTrashName(filesDir, infoDir, "note.txt")
	if err != nil {
		t.Fatalf("uniqueTrashName: %v", err)
	}
	if name != "note.txt.2" {
		t.Errorf("name = %q, want note.txt.2", name)
	}
}

func TestFitTrashNameRespectsNameMax(t *testing.T) {
	long := strings.Repeat("a", 400)
	got := fitTrashName(long, "")
	if len(got)+len(trashInfoSuffix) > trashNameMax {
		t.Errorf("fitTrashName produced %d bytes, exceeds NAME_MAX", len(got)+len(trashInfoSuffix))
	}
}

func TestPathExistsDoesNotFollowSymlink(t *testing.T) {
	dir := t.TempDir()
	dangling := filepath.Join(dir, "dangling")
	if err := os.Symlink(filepath.Join(dir, "nope"), dangling); err != nil {
		t.Fatalf("symlink: %v", err)
	}
	if !pathExists(dangling) {
		t.Error("pathExists should be true for an existing dangling symlink")
	}
}
