package webcrawl

import (
	"bytes"
	"compress/gzip"
	"testing"
)

func TestParseSitemapURLSet(t *testing.T) {
	body := []byte(`<?xml version="1.0" encoding="UTF-8"?>
<urlset xmlns="http://www.sitemaps.org/schemas/sitemap/0.9">
  <url><loc>https://example.com/</loc></url>
  <url><loc>https://example.com/about</loc></url>
</urlset>`)
	urls, isIndex, err := ParseSitemap(body)
	if err != nil {
		t.Fatal(err)
	}
	if isIndex {
		t.Error("urlset must not be reported as index")
	}
	want := []string{"https://example.com/", "https://example.com/about"}
	if len(urls) != len(want) {
		t.Fatalf("urls = %v, want %v", urls, want)
	}
	for i := range want {
		if urls[i] != want[i] {
			t.Errorf("urls[%d] = %q, want %q", i, urls[i], want[i])
		}
	}
}

func TestParseSitemapIndex(t *testing.T) {
	body := []byte(`<?xml version="1.0"?>
<sitemapindex xmlns="http://www.sitemaps.org/schemas/sitemap/0.9">
  <sitemap><loc>https://example.com/sitemap-1.xml</loc></sitemap>
  <sitemap><loc>https://example.com/sitemap-2.xml</loc></sitemap>
</sitemapindex>`)
	urls, isIndex, err := ParseSitemap(body)
	if err != nil {
		t.Fatal(err)
	}
	if !isIndex {
		t.Error("sitemapindex must be reported as index")
	}
	if len(urls) != 2 || urls[0] != "https://example.com/sitemap-1.xml" || urls[1] != "https://example.com/sitemap-2.xml" {
		t.Errorf("urls = %v", urls)
	}
}

func TestParseSitemapGzip(t *testing.T) {
	plain := []byte(`<?xml version="1.0"?>
<urlset xmlns="http://www.sitemaps.org/schemas/sitemap/0.9">
  <url><loc>https://example.com/gz</loc></url>
</urlset>`)
	var buf bytes.Buffer
	zw := gzip.NewWriter(&buf)
	if _, err := zw.Write(plain); err != nil {
		t.Fatal(err)
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}

	urls, isIndex, err := ParseSitemap(buf.Bytes())
	if err != nil {
		t.Fatal(err)
	}
	if isIndex || len(urls) != 1 || urls[0] != "https://example.com/gz" {
		t.Errorf("urls=%v isIndex=%v", urls, isIndex)
	}
}

func TestParseSitemapUnknownRoot(t *testing.T) {
	if _, _, err := ParseSitemap([]byte(`<html><body/></html>`)); err == nil {
		t.Error("unknown root must be an error")
	}
}

func TestParseSitemapGzipOverLimit(t *testing.T) {
	// A gzip payload that decompresses past sitemapGunzipLimit must be rejected
	// with an error (and bounded memory), never panicked or silently truncated.
	plain := bytes.Repeat([]byte{'a'}, sitemapGunzipLimit+1024)
	var buf bytes.Buffer
	zw := gzip.NewWriter(&buf)
	if _, err := zw.Write(plain); err != nil {
		t.Fatal(err)
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}

	if _, _, err := ParseSitemap(buf.Bytes()); err == nil {
		t.Fatal("ParseSitemap over the gunzip limit = nil, want error")
	}
}
