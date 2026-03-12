package mcp

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	htmltomarkdown "github.com/JohannesKaufmann/html-to-markdown/v2"
	"golang.org/x/net/html"
)

const (
	webFetchMaxResponseSize = 5 * 1024 * 1024 // 5MB
	webFetchDefaultTimeout  = 30 * time.Second
	webFetchMaxTimeout      = 120 * time.Second

	// Browser-like UA to avoid bot blocks; mirrors what OpenCode uses.
	webFetchUserAgent = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/143.0.0.0 Safari/537.36"
)

// webFetchError returns a ToolResult with IsError=true and the given message.
func webFetchError(msg string) *ToolResult {
	return &ToolResult{
		Content: []ContentBlock{{Type: "text", Text: msg}},
		IsError: true,
	}
}

// executeWebFetch fetches a URL and returns the content in the requested format.
func (s *Service) executeWebFetch(ctx context.Context, args map[string]any) (*ToolResult, error) {
	// --- parse args ---
	rawURL, _ := args["url"].(string)
	if rawURL == "" {
		return webFetchError("Missing required argument: url"), nil
	}
	if !strings.HasPrefix(rawURL, "http://") && !strings.HasPrefix(rawURL, "https://") {
		return webFetchError("URL must start with http:// or https://"), nil
	}

	format, _ := args["format"].(string)
	if format == "" {
		format = "markdown"
	}
	switch format {
	case "markdown", "text", "html":
	default:
		return webFetchError(fmt.Sprintf("Invalid format %q: must be markdown, text, or html", format)), nil
	}

	timeout := webFetchDefaultTimeout
	if t, ok := args["timeout"].(float64); ok && t > 0 {
		d := time.Duration(t) * time.Second
		if d > webFetchMaxTimeout {
			d = webFetchMaxTimeout
		}
		timeout = d
	}

	// --- build Accept header ---
	var acceptHeader string
	switch format {
	case "markdown":
		acceptHeader = "text/markdown;q=1.0, text/x-markdown;q=0.9, text/plain;q=0.8, text/html;q=0.7, */*;q=0.1"
	case "text":
		acceptHeader = "text/plain;q=1.0, text/markdown;q=0.9, text/html;q=0.8, */*;q=0.1"
	case "html":
		acceptHeader = "text/html;q=1.0, application/xhtml+xml;q=0.9, text/plain;q=0.8, */*;q=0.1"
	}

	// --- fetch ---
	body, contentType, err := s.doWebFetch(ctx, rawURL, acceptHeader, timeout)
	if err != nil {
		return webFetchError(fmt.Sprintf("Fetch error: %s", err.Error())), nil
	}

	// --- convert ---
	output, err := convertWebContent(body, contentType, format)
	if err != nil {
		return webFetchError(fmt.Sprintf("Conversion error: %s", err.Error())), nil
	}

	return s.wrapResult(map[string]any{
		"url":          rawURL,
		"format":       format,
		"content_type": contentType,
		"content":      output,
	})
}

// doWebFetch performs the HTTP GET with a browser-like User-Agent and a Cloudflare
// retry (matching the OpenCode webfetch behaviour).
func (s *Service) doWebFetch(ctx context.Context, rawURL, acceptHeader string, timeout time.Duration) ([]byte, string, error) {
	client := &http.Client{Timeout: timeout}

	do := func(ua string) (*http.Response, error) {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
		if err != nil {
			return nil, fmt.Errorf("create request: %w", err)
		}
		req.Header.Set("User-Agent", ua)
		req.Header.Set("Accept", acceptHeader)
		req.Header.Set("Accept-Language", "en-US,en;q=0.9")
		return client.Do(req)
	}

	resp, err := do(webFetchUserAgent)
	if err != nil {
		return nil, "", fmt.Errorf("execute request: %w", err)
	}

	// Cloudflare bot-detection retry — same logic as OpenCode
	if resp.StatusCode == http.StatusForbidden && resp.Header.Get("cf-mitigated") == "challenge" {
		resp.Body.Close()
		resp, err = do("opencode")
		if err != nil {
			return nil, "", fmt.Errorf("execute request (retry): %w", err)
		}
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		preview, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return nil, "", fmt.Errorf("HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(preview)))
	}

	// Guard against oversized responses
	if cl := resp.ContentLength; cl > webFetchMaxResponseSize {
		return nil, "", fmt.Errorf("response too large (%d bytes, limit 5MB)", cl)
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, webFetchMaxResponseSize+1))
	if err != nil {
		return nil, "", fmt.Errorf("read body: %w", err)
	}
	if int64(len(body)) > webFetchMaxResponseSize {
		return nil, "", fmt.Errorf("response too large (exceeds 5MB limit)")
	}

	contentType := resp.Header.Get("Content-Type")
	return body, contentType, nil
}

// convertWebContent converts the raw bytes into the requested format.
func convertWebContent(body []byte, contentType, format string) (string, error) {
	isHTML := strings.Contains(contentType, "text/html")

	switch format {
	case "markdown":
		if isHTML {
			md, err := htmltomarkdown.ConvertString(string(body))
			if err != nil {
				return "", fmt.Errorf("html-to-markdown: %w", err)
			}
			return md, nil
		}
		return string(body), nil

	case "text":
		if isHTML {
			return extractTextFromHTML(string(body))
		}
		return string(body), nil

	case "html":
		return string(body), nil

	default:
		return string(body), nil
	}
}

// extractTextFromHTML strips all HTML tags and returns plain text content,
// skipping script/style nodes. Uses golang.org/x/net/html.
func extractTextFromHTML(src string) (string, error) {
	doc, err := html.Parse(strings.NewReader(src))
	if err != nil {
		return "", fmt.Errorf("parse html: %w", err)
	}

	var sb strings.Builder
	var walk func(*html.Node)
	walk = func(n *html.Node) {
		if n.Type == html.ElementNode {
			switch n.Data {
			case "script", "style", "noscript", "iframe", "object", "embed":
				return // skip entire subtree
			}
		}
		if n.Type == html.TextNode {
			sb.WriteString(n.Data)
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			walk(c)
		}
	}
	walk(doc)

	return strings.TrimSpace(sb.String()), nil
}

// getWebFetchToolDefinition returns the MCP tool definition for webfetch.
func getWebFetchToolDefinition() ToolDefinition {
	return ToolDefinition{
		Name:        "web-fetch",
		Description: "Fetch the content of a URL and return it as markdown, plain text, or raw HTML. Use this when you already know the URL you need to read (retrieval). For finding URLs, use brave_web_search instead.",
		InputSchema: InputSchema{
			Type: "object",
			Properties: map[string]PropertySchema{
				"url": {
					Type:        "string",
					Description: "The URL to fetch (must start with http:// or https://)",
				},
				"format": {
					Type:        "string",
					Description: "Output format: markdown (default), text, or html",
					Enum:        []string{"markdown", "text", "html"},
					Default:     "markdown",
				},
				"timeout": {
					Type:        "number",
					Description: "Timeout in seconds (default: 30, max: 120)",
					Minimum:     intPtr(1),
					Maximum:     intPtr(120),
					Default:     30,
				},
			},
			Required: []string{"url"},
		},
	}
}
