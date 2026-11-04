package webcrawl

import (
	"strings"
	"testing"
)

func TestToMarkdownTextPassthrough(t *testing.T) {
	body := []byte("just plain text\nline two")
	text, ct, err := ToMarkdown(body, "text/plain", "")
	if err != nil {
		t.Fatal(err)
	}
	if text != string(body) {
		t.Errorf("text = %q, want passthrough", text)
	}
	if ct != "text/plain" {
		t.Errorf("content type = %q, want text/plain", ct)
	}

	md, ct2, err := ToMarkdown(body, "text/markdown", "")
	if err != nil {
		t.Fatal(err)
	}
	if md != string(body) || ct2 != "text/markdown" {
		t.Errorf("markdown passthrough = %q / %q", md, ct2)
	}
}

func TestToMarkdownHTML(t *testing.T) {
	body := []byte(`<html><body><h1>Hello</h1><p>World</p></body></html>`)
	text, ct, err := ToMarkdown(body, "text/html; charset=utf-8", "")
	if err != nil {
		t.Fatal(err)
	}
	if ct != "text/markdown" {
		t.Errorf("content type = %q, want text/markdown", ct)
	}
	if !strings.Contains(text, "Hello") || !strings.Contains(text, "World") {
		t.Errorf("markdown = %q, want Hello and World", text)
	}
}

func TestToMarkdownNarrowsToMainAndDropsChrome(t *testing.T) {
	body := []byte(`<html><body>
<nav>Site Nav</nav>
<header>Header</header>
<main><h1>Title</h1><p>Body text</p><script>var secret=1;</script></main>
<footer>Footer</footer>
<aside>Sidebar</aside>
</body></html>`)
	text, _, err := ToMarkdown(body, "text/html", "")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"Title", "Body text"} {
		if !strings.Contains(text, want) {
			t.Errorf("markdown missing %q: %q", want, text)
		}
	}
	for _, unwanted := range []string{"Site Nav", "Header", "Footer", "Sidebar", "var secret"} {
		if strings.Contains(text, unwanted) {
			t.Errorf("markdown must not contain %q: %q", unwanted, text)
		}
	}
}

func TestToMarkdownCharsetDecoding(t *testing.T) {
	// "café" in ISO-8859-1: é is 0xE9.
	latin1 := []byte("<html><body><p>caf\xe9</p></body></html>")

	t.Run("via content-type charset", func(t *testing.T) {
		text, _, err := ToMarkdown(latin1, "text/html; charset=iso-8859-1", "")
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(text, "café") {
			t.Errorf("markdown = %q, want decoded café", text)
		}
	})

	t.Run("via charset hint", func(t *testing.T) {
		text, _, err := ToMarkdown(latin1, "text/html", "iso-8859-1")
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(text, "café") {
			t.Errorf("markdown = %q, want decoded café", text)
		}
	})

	t.Run("via meta charset sniff", func(t *testing.T) {
		meta := []byte("<html><head><meta charset=\"iso-8859-1\"></head><body><p>caf\xe9</p></body></html>")
		text, _, err := ToMarkdown(meta, "text/html", "")
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(text, "café") {
			t.Errorf("markdown = %q, want decoded café", text)
		}
	})
}

func TestToMarkdownNonHTMLNonText(t *testing.T) {
	body := []byte("%PDF-1.4 binary...")
	text, ct, err := ToMarkdown(body, "application/pdf", "")
	if err != nil {
		t.Fatal(err)
	}
	if text != "" {
		t.Errorf("text = %q, want empty for non-HTML non-text", text)
	}
	if ct != "application/pdf" {
		t.Errorf("content type = %q, want application/pdf", ct)
	}
}
