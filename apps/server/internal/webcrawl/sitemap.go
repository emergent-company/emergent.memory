package webcrawl

import (
	"bytes"
	"compress/gzip"
	"encoding/xml"
	"fmt"
	"io"
)

// sitemapGunzipLimit caps the decompressed size of a gzip sitemap. It is
// deliberately separate from the fetcher's transport read cap: a sitemap served
// as a raw gzip file (no Content-Encoding header, so the transport does not
// decompress it) is a small compressed payload that can expand to a huge
// document. Without this bound, io.ReadAll of the gunzip reader would let a gzip
// bomb exhaust memory.
const sitemapGunzipLimit = 8 << 20 // 8 MiB

// sitemapDoc captures the two root forms in one struct: <urlset> has <url><loc>
// entries, <sitemapindex> has <sitemap><loc> entries naming nested sitemaps.
type sitemapDoc struct {
	XMLName xml.Name
	Locs    []string `xml:"url>loc"`
	Subs    []string `xml:"sitemap>loc"`
}

// ParseSitemap parses a sitemap document — plain XML or gzip-compressed —
// returning the <loc> URLs it contains and whether the document is a
// sitemapindex (whose URLs name nested sitemaps rather than pages). An unknown
// root element is an error.
func ParseSitemap(body []byte) (urls []string, isIndex bool, err error) {
	decoded, err := maybeGunzip(body)
	if err != nil {
		return nil, false, err
	}

	var doc sitemapDoc
	if err := xml.Unmarshal(decoded, &doc); err != nil {
		return nil, false, fmt.Errorf("parse sitemap xml: %w", err)
	}

	switch doc.XMLName.Local {
	case "urlset":
		return doc.Locs, false, nil
	case "sitemapindex":
		return doc.Subs, true, nil
	default:
		return nil, false, fmt.Errorf("unrecognized sitemap root element %q", doc.XMLName.Local)
	}
}

// maybeGunzip decompresses gzip-compressed input (detected by the magic bytes)
// and passes everything else through unchanged.
func maybeGunzip(body []byte) ([]byte, error) {
	if len(body) < 2 || body[0] != 0x1f || body[1] != 0x8b {
		return body, nil
	}
	zr, err := gzip.NewReader(bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("open gzip sitemap: %w", err)
	}
	defer func() { _ = zr.Close() }()
	// Read at most one byte past the cap so an over-long document is detected
	// (the single extra byte proves the limit was exceeded) without ever
	// allocating the full decompressed size.
	decoded, err := io.ReadAll(io.LimitReader(zr, sitemapGunzipLimit+1))
	if err != nil {
		return nil, fmt.Errorf("read gzip sitemap: %w", err)
	}
	if len(decoded) > sitemapGunzipLimit {
		return nil, fmt.Errorf("gzip sitemap expands past the %d-byte limit", sitemapGunzipLimit)
	}
	return decoded, nil
}
