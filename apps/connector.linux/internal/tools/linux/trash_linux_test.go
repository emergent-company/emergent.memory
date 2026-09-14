//go:build linux

package linux

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"syscall"
	"testing"
)

// withSeams restores the injectable trash seams after a test.
func withSeams(t *testing.T) {
	t.Helper()
	oldRename, oldTopdir, oldUID := renameFn, topdirFn, getuid
	t.Cleanup(func() {
		renameFn, topdirFn, getuid = oldRename, oldTopdir, oldUID
	})
}

var deletionDateRE = regexp.MustCompile(`DeletionDate=\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}\n`)

func TestTrashHomeMovesFileAndWritesInfo(t *testing.T) {
	xdg := t.TempDir()
	t.Setenv("XDG_DATA_HOME", xdg)

	root := t.TempDir()
	dir := filepath.Join(root, "My Docs")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	target := filepath.Join(dir, "a b.txt")
	writeFile(t, target, "payload")

	if err := trashPath(target); err != nil {
		t.Fatalf("trashPath: %v", err)
	}
	if _, err := os.Lstat(target); !os.IsNotExist(err) {
		t.Errorf("target still present after trash (err=%v)", err)
	}

	moved := filepath.Join(xdg, "Trash", "files", "a b.txt")
	got, err := os.ReadFile(moved)
	if err != nil {
		t.Fatalf("read trashed file: %v", err)
	}
	if string(got) != "payload" {
		t.Errorf("trashed content = %q, want payload", got)
	}

	infoPath := filepath.Join(xdg, "Trash", "info", "a b.txt.trashinfo")
	raw, err := os.ReadFile(infoPath)
	if err != nil {
		t.Fatalf("read trashinfo: %v", err)
	}
	body := string(raw)
	if !strings.HasPrefix(body, "[Trash Info]\n") {
		t.Errorf("trashinfo missing header:\n%s", body)
	}
	if want := "Path=" + percentEncodePath(target) + "\n"; !strings.Contains(body, want) {
		t.Errorf("trashinfo = %q, want line %q", body, want)
	}
	if !strings.Contains(body, "%20") {
		t.Errorf("trashinfo path should percent-encode spaces:\n%s", body)
	}
	if !deletionDateRE.MatchString(body) {
		t.Errorf("trashinfo DeletionDate not local no-tz format:\n%s", body)
	}
	if strings.Contains(body, "Z\n") || strings.Contains(body, "+") {
		t.Errorf("trashinfo DeletionDate must not carry a timezone:\n%s", body)
	}
}

func TestTrashCollisionPicksUniqueName(t *testing.T) {
	xdg := t.TempDir()
	t.Setenv("XDG_DATA_HOME", xdg)
	targetDir := filepath.Join(xdg, "Trash", "files")
	if err := os.MkdirAll(targetDir, 0o700); err != nil {
		t.Fatalf("mkdir trash files: %v", err)
	}
	writeFile(t, filepath.Join(targetDir, "note.txt"), "already")

	root := t.TempDir()
	target := filepath.Join(root, "note.txt")
	writeFile(t, target, "new")
	if err := trashPath(target); err != nil {
		t.Fatalf("trashPath: %v", err)
	}
	if _, err := os.Stat(filepath.Join(targetDir, "note.txt.1")); err != nil {
		t.Errorf("expected collision copy at note.txt.1: %v", err)
	}
}

func TestTrashDirectoryAsUnit(t *testing.T) {
	xdg := t.TempDir()
	t.Setenv("XDG_DATA_HOME", xdg)
	root := t.TempDir()
	dir := filepath.Join(root, "project")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	writeFile(t, filepath.Join(dir, "child.txt"), "child")

	if err := trashPath(dir); err != nil {
		t.Fatalf("trashPath: %v", err)
	}
	got, err := os.ReadFile(filepath.Join(xdg, "Trash", "files", "project", "child.txt"))
	if err != nil {
		t.Fatalf("read trashed child: %v", err)
	}
	if string(got) != "child" {
		t.Errorf("child content = %q, want child", got)
	}
}

func TestTrashSymlinkAsLinkKeepsTarget(t *testing.T) {
	xdg := t.TempDir()
	t.Setenv("XDG_DATA_HOME", xdg)
	outside := t.TempDir()
	realFile := filepath.Join(outside, "secret.txt")
	writeFile(t, realFile, "secret")

	root := t.TempDir()
	link := filepath.Join(root, "link")
	if err := os.Symlink(realFile, link); err != nil {
		t.Fatalf("symlink: %v", err)
	}
	if err := trashPath(link); err != nil {
		t.Fatalf("trashPath: %v", err)
	}

	info, err := os.Lstat(filepath.Join(xdg, "Trash", "files", "link"))
	if err != nil {
		t.Fatalf("lstat trashed link: %v", err)
	}
	if info.Mode()&os.ModeSymlink == 0 {
		t.Errorf("trashed entry mode = %v, want symlink", info.Mode())
	}
	if buf, err := os.ReadFile(realFile); err != nil || string(buf) != "secret" {
		t.Errorf("symlink target was disturbed: (%q, %v)", buf, err)
	}
}

func TestTrashRefusesFilesystemRoot(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	if err := trashPath(string(os.PathSeparator)); err == nil {
		t.Fatal("trashPath(/) = nil, want refusal")
	}
}

func TestTrashRefusesTrashAncestor(t *testing.T) {
	xdg := t.TempDir()
	t.Setenv("XDG_DATA_HOME", xdg)
	if err := trashPath(xdg); err == nil {
		t.Fatal("trashPath(ancestor of Trash) = nil, want refusal")
	}
	if _, err := os.Stat(xdg); err != nil {
		t.Errorf("ancestor should be intact: %v", err)
	}
}

func TestTrashEXDEVFallsBackToTopdirPrivate(t *testing.T) {
	xdg := t.TempDir()
	t.Setenv("XDG_DATA_HOME", xdg)
	withSeams(t)

	topdir := t.TempDir()
	dataDir := filepath.Join(topdir, "data")
	if err := os.MkdirAll(dataDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	target := filepath.Join(dataDir, "f.txt")
	writeFile(t, target, "payload")

	topdirFn = func(string) (string, error) { return topdir, nil }
	getuid = func() int { return 12345 }
	var calls int
	renameFn = func(oldPath, newPath string) error {
		calls++
		if calls == 1 {
			return &os.LinkError{Op: "rename", Old: oldPath, New: newPath, Err: syscall.EXDEV}
		}
		return os.Rename(oldPath, newPath)
	}

	if err := trashPathPlatform(target); err != nil {
		t.Fatalf("trashPathPlatform: %v", err)
	}
	// The home-trash info file must be rolled back on EXDEV.
	if pathExists(filepath.Join(xdg, "Trash", "info", "f.txt.trashinfo")) {
		t.Errorf("home trash left a dangling info file after EXDEV")
	}
	moved := filepath.Join(topdir, ".Trash-12345", "files", "f.txt")
	if got, err := os.ReadFile(moved); err != nil || string(got) != "payload" {
		t.Fatalf("topdir moved file = (%q, %v)", got, err)
	}
	rel, err := filepath.Rel(topdir, target)
	if err != nil {
		t.Fatalf("rel: %v", err)
	}
	raw, err := os.ReadFile(filepath.Join(topdir, ".Trash-12345", "info", "f.txt.trashinfo"))
	if err != nil {
		t.Fatalf("read topdir trashinfo: %v", err)
	}
	if want := "Path=" + percentEncodePath(rel) + "\n"; !strings.Contains(string(raw), want) {
		t.Errorf("topdir trashinfo = %q, want relative %q", raw, want)
	}
}

func TestTrashTopdirSharedWhenSticky(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	withSeams(t)

	topdir := t.TempDir()
	target := filepath.Join(topdir, "f.txt")
	writeFile(t, target, "payload")

	shared := filepath.Join(topdir, ".Trash")
	if err := os.Mkdir(shared, 0o700); err != nil {
		t.Fatalf("mkdir shared trash: %v", err)
	}
	if err := os.Chmod(shared, os.ModeSticky|0o777); err != nil {
		t.Fatalf("chmod sticky: %v", err)
	}
	topdirFn = func(string) (string, error) { return topdir, nil }
	getuid = func() int { return 4242 }
	var calls int
	renameFn = func(oldPath, newPath string) error {
		calls++
		if calls == 1 {
			return &os.LinkError{Op: "rename", Old: oldPath, New: newPath, Err: syscall.EXDEV}
		}
		return os.Rename(oldPath, newPath)
	}

	if err := trashPathPlatform(target); err != nil {
		t.Fatalf("trashPathPlatform: %v", err)
	}
	if _, err := os.Stat(filepath.Join(shared, "4242", "files", "f.txt")); err != nil {
		t.Errorf("expected shared topdir trash entry: %v", err)
	}
	if pathExists(filepath.Join(topdir, ".Trash-4242")) {
		t.Errorf("private topdir trash should not be used when shared is sticky")
	}
}

func TestTrashTopdirNeitherUsableErrors(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	withSeams(t)

	topdir := t.TempDir()
	target := filepath.Join(topdir, "f.txt")
	writeFile(t, target, "payload")

	// .Trash exists but is not sticky -> skip shared.
	if err := os.Mkdir(filepath.Join(topdir, ".Trash"), 0o755); err != nil {
		t.Fatalf("mkdir .Trash: %v", err)
	}
	// .Trash-<uid> exists as a file -> MkdirAll fails.
	writeFile(t, filepath.Join(topdir, ".Trash-7777"), "not a dir")

	topdirFn = func(string) (string, error) { return topdir, nil }
	getuid = func() int { return 7777 }
	renameFn = func(oldPath, newPath string) error {
		return &os.LinkError{Op: "rename", Old: oldPath, New: newPath, Err: syscall.EXDEV}
	}

	if err := trashPathPlatform(target); err == nil {
		t.Fatal("trashPathPlatform with no usable topdir trash: expected error, got nil")
	}
	if _, err := os.Stat(target); err != nil {
		t.Errorf("target must be left intact: %v", err)
	}
}

func TestUnescapeMountField(t *testing.T) {
	cases := map[string]string{
		"/mnt/with\\040space": "/mnt/with space",
		"/a\\011b":            "/a\tb",
		"/plain":              "/plain",
	}
	for in, want := range cases {
		if got := unescapeMountField(in); got != want {
			t.Errorf("unescapeMountField(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestMountTopdirFindsAncestor(t *testing.T) {
	dir := t.TempDir()
	got, err := mountTopdir(dir)
	if err != nil {
		t.Fatalf("mountTopdir: %v", err)
	}
	if got == "" || !isSameOrAncestor(got, dir) {
		t.Errorf("mountTopdir(%q) = %q, want an ancestor", dir, got)
	}
}
