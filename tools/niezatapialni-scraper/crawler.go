package main

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/gocolly/colly/v2"
)

// DiscoverEpisodeURLs walks all paginated listing pages on niezatapialni.com,
// returns episode post URLs to scrape. Already-seen URLs are skipped unless
// they appear in retryURLs (from failed_urls.txt in continue mode).
func DiscoverEpisodeURLs(cfg Config, skipSet map[string]bool, retryURLs []string) ([]string, error) {
	// Build a set of retry URLs so we can force-include them.
	retrySet := make(map[string]bool, len(retryURLs))
	for _, u := range retryURLs {
		retrySet[u] = true
	}

	c := colly.NewCollector(
		colly.AllowedDomains("niezatapialni.com", "niezatapialni.pl", "www.niezatapialni.com", "www.niezatapialni.pl"),
		colly.UserAgent("Mozilla/5.0 (compatible; niezatapialni-scraper/1.0)"),
	)

	// Rate limiting: honour cfg.Delay between requests.
	c.Limit(&colly.LimitRule{
		DomainGlob:  "*niezatapialni.*",
		Delay:       cfg.Delay,
		RandomDelay: 0,
	})

	var discovered []string
	seen := make(map[string]bool) // dedup within this discovery run
	totalPages := 0

	// Detect last page number from all ?paged=N hrefs on page 1.
	// The site uses plain <li><a href="?paged=90">90</a></li> with no CSS classes.
	c.OnHTML("a[href]", func(e *colly.HTMLElement) {
		href := e.Attr("href")
		if strings.Contains(href, "paged=") {
			parts := strings.Split(href, "paged=")
			if len(parts) == 2 {
				n, err := strconv.Atoi(strings.TrimRight(parts[1], "/ "))
				if err == nil && n > totalPages {
					totalPages = n
				}
			}
		}
	})

	// Collect post URLs from listing pages.
	c.OnHTML("h1.entry-title a[href], h2.entry-title a[href]", func(e *colly.HTMLElement) {
		href := e.Attr("href")
		if href == "" || seen[href] {
			return
		}
		seen[href] = true

		if retrySet[href] {
			// Always include retry URLs even if in skip set.
			discovered = append(discovered, href)
			return
		}
		if skipSet[href] {
			return
		}
		discovered = append(discovered, href)
	})

	c.OnError(func(r *colly.Response, err error) {
		if r != nil {
			fmt.Printf("WARN: listing page error HTTP %d: %v\n", r.StatusCode, err)
		} else {
			fmt.Printf("WARN: listing page error: %v\n", err)
		}
	})

	// Visit page 1 first to detect total pages.
	baseURL := "https://www.niezatapialni.pl/"
	if err := c.Visit(baseURL); err != nil {
		return nil, fmt.Errorf("visit page 1: %w", err)
	}

	maxPages := totalPages
	if cfg.MaxPages > 0 && cfg.MaxPages < maxPages {
		maxPages = cfg.MaxPages
	}
	fmt.Printf("Discovered %d listing pages\n", maxPages)

	// Visit pages 2..N sequentially.
	for p := 2; p <= maxPages; p++ {
		url := fmt.Sprintf("https://www.niezatapialni.pl/?paged=%d", p)
		if err := c.Visit(url); err != nil {
			fmt.Printf("WARN: skipping page %d: %v\n", p, err)
		}
		time.Sleep(cfg.Delay)
	}

	// Prepend retry URLs so they are processed first.
	result := make([]string, 0, len(retryURLs)+len(discovered))
	for _, u := range retryURLs {
		if !seen[u] { // not already discovered from listing pages
			result = append(result, u)
		}
	}
	result = append(result, discovered...)

	skipped := len(skipSet)
	fmt.Printf("Discovery complete: %d episodes queued, %d skipped (already scraped)\n",
		len(result), skipped)

	return result, nil
}
