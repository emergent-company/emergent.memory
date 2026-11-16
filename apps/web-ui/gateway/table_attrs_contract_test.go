package main

// guard: table.TableWithProps must keep its classes in its own class
// attribute, not Attrs — must gate every PR.

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestTemplNoTableClassInAttrsRatchet keeps table.TableWithProps' classes in the
// props that own them.
//
// TableProps emits its own class on the scroll wrapper ("overflow-x-auto ..."),
// so a "class" passed through Attrs renders a SECOND class attribute. A browser
// keeps the first one, so the classes passed through Attrs — `stacked-table`,
// the mobile card layout — silently never applied, on every table that did it.
// TableProps.WrapperClass appends to the wrapper and TableProps.Class to the
// <table>, which is the supported path.
//
// The Ratchet suffix puts this in the required ci job's `go test -run Ratchet
// ./...` selection.
func TestTemplNoTableClassInAttrsRatchet(t *testing.T) {
	files, err := filepath.Glob("*.templ")
	if err != nil {
		t.Fatalf("glob templ files: %v", err)
	}
	if len(files) == 0 {
		t.Fatal("no .templ files found — the scan is not looking at the gateway")
	}
	scanned := 0
	for _, name := range files {
		src, err := os.ReadFile(name)
		if err != nil {
			t.Fatalf("read %s: %v", name, err)
		}
		text := string(src)
		for i := 0; ; {
			j := strings.Index(text[i:], "table.TableWithProps(")
			if j < 0 {
				break
			}
			start := i + j
			scanned++
			if args := balancedCall(text[start:]); strings.Contains(args, `"class":`) {
				t.Errorf("%s: TableWithProps passes \"class\" through Attrs; use WrapperClass (wrapper) or Class (<table>) instead — a duplicate class attribute silently drops the extra classes", name)
			}
			i = start + 1
		}
	}
	if scanned == 0 {
		t.Fatal("no TableWithProps calls found — the scan is not matching the component")
	}
}

// balancedCall returns src up to the parenthesis that closes the call whose
// opening parenthesis is in src (i.e. the argument list), skipping over quoted
// strings so a "(" inside a class or style string does not unbalance it.
func balancedCall(src string) string {
	depth := 0
	var quote byte
	for i := 0; i < len(src); i++ {
		c := src[i]
		if quote != 0 {
			if c == '\\' {
				i++
				continue
			}
			if c == quote {
				quote = 0
			}
			continue
		}
		switch c {
		case '\'', '"', '`':
			quote = c
		case '(':
			depth++
		case ')':
			depth--
			if depth == 0 {
				return src[:i]
			}
		}
	}
	return src
}
