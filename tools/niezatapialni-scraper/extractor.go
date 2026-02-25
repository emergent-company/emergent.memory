package main

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/go-rod/rod"
	"github.com/go-rod/rod/lib/proto"
	"github.com/gocolly/colly/v2"
)

// ScrapeEpisode fetches a single episode post page and extracts all metadata.
// Metadata (title, date, mp3_url, body) is scraped via colly (fast, no browser).
// Comments are scraped via a headless browser (rod) to render Disqus JS.
// Returns a partial Episode (with nil fields) and nil error on parse failures.
// Returns a non-nil error only on unrecoverable network/HTTP failures.
func ScrapeEpisode(url string, browser *rod.Browser, cfg Config) (Episode, error) {
	ep := Episode{
		PostURL:  url,
		Comments: []Comment{},
	}

	// --- Phase 1: colly for static metadata ---
	c := colly.NewCollector(
		colly.UserAgent("Mozilla/5.0 (compatible; niezatapialni-scraper/1.0)"),
	)
	c.SetRequestTimeout(30 * time.Second)

	var scrapeErr error

	c.OnHTML("h1.entry-title, h2.entry-title", func(e *colly.HTMLElement) {
		if ep.Title == "" {
			ep.Title = strings.TrimSpace(e.Text)
		}
	})

	c.OnHTML("time.entry-date[datetime]", func(e *colly.HTMLElement) {
		if ep.Date == "" {
			raw := e.Attr("datetime")
			if t, err := time.Parse(time.RFC3339, raw); err == nil {
				ep.Date = t.Format("2006-01-02")
			} else if t, err := time.Parse("2006-01-02", raw); err == nil {
				ep.Date = t.Format("2006-01-02")
			} else {
				ep.Date = raw
			}
		}
	})

	c.OnHTML(`a[href]`, func(e *colly.HTMLElement) {
		if ep.MP3URL == nil {
			href := e.Attr("href")
			if strings.HasSuffix(strings.ToLower(href), ".mp3") {
				v := href
				ep.MP3URL = &v
			}
		}
	})

	c.OnHTML(".entry-summary", func(e *colly.HTMLElement) {
		if ep.Description == "" {
			ep.Description = strings.TrimSpace(e.Text)
		}
	})

	c.OnHTML(".entry-content", func(e *colly.HTMLElement) {
		if ep.Body == "" {
			ep.Body = strings.TrimSpace(e.Text)
		}
		if ep.Description == "" {
			ep.Description = strings.TrimSpace(e.ChildText("p"))
		}
	})

	c.OnError(func(r *colly.Response, err error) {
		if r != nil {
			scrapeErr = fmt.Errorf("HTTP %d fetching %s: %w", r.StatusCode, url, err)
		} else {
			scrapeErr = fmt.Errorf("fetch %s: %w", url, err)
		}
	})

	if err := c.Visit(url); err != nil && scrapeErr == nil {
		scrapeErr = fmt.Errorf("visit %s: %w", url, err)
	}
	if scrapeErr != nil {
		return ep, scrapeErr
	}

	// Parse episode number from title.
	if ep.Title != "" {
		ep.EpisodeNumber = parseEpisodeNumber(ep.Title)
	}
	if ep.Title == "" {
		fmt.Printf("WARN: no title found for %s\n", url)
	}
	if ep.MP3URL == nil {
		fmt.Printf("WARN: no mp3_url found for %s\n", url)
	}

	// --- Phase 2: rod headless browser for Disqus comments ---
	comments, err := scrapeDisqusComments(url, browser, cfg)
	if err != nil {
		fmt.Printf("WARN: could not scrape comments for %s: %v\n", url, err)
		// Non-fatal: return episode without comments.
	} else {
		ep.Comments = comments
	}

	return ep, nil
}

// scrapeDisqusComments uses a shared headless browser to render and extract Disqus comments.
func scrapeDisqusComments(pageURL string, browser *rod.Browser, cfg Config) ([]Comment, error) {
	page, err := browser.Page(proto.TargetCreateTarget{URL: "about:blank"})
	if err != nil {
		return nil, fmt.Errorf("create page: %w", err)
	}
	// Apply a strict 45-second timeout to the entire page operation to prevent hangs
	page = page.Timeout(45 * time.Second)
	defer page.Close()

	if err := page.Navigate(pageURL); err != nil {
		return nil, fmt.Errorf("navigate: %w", err)
	}
	if err := page.WaitLoad(); err != nil {
		return nil, fmt.Errorf("wait load: %w", err)
	}

	// Scroll to bottom to trigger Disqus lazy-load.
	page.MustEval(`() => window.scrollTo(0, document.body.scrollHeight)`)
	time.Sleep(6 * time.Second)

	// Find the Disqus embed iframe (src contains "disqus.com/embed/comments/").
	iframes, err := page.Elements("iframe")
	if err != nil {
		return nil, fmt.Errorf("list iframes: %w", err)
	}

	var disqusFrame *rod.Page
	for _, iframe := range iframes {
		src, _ := iframe.Attribute("src")
		if src == nil || !strings.Contains(*src, "disqus.com/embed/comments") {
			continue
		}
		f, err := iframe.Frame()
		if err != nil {
			continue
		}
		// Verify it has loaded comments.
		els, _ := f.Elements(".post-message")
		if len(els) > 0 {
			disqusFrame = f
			break
		}
	}

	if disqusFrame == nil {
		// No comments or Disqus not loaded — return empty, not an error.
		return []Comment{}, nil
	}

	posts, err := disqusFrame.Elements("li.post")
	if err != nil {
		return []Comment{}, nil
	}

	var comments []Comment
	for _, post := range posts {
		// Author: the display name link inside .author.
		var author string
		if el, err := post.Element(".author a"); err == nil {
			author = strings.TrimSpace(el.MustText())
		}

		// Date: title attribute of the relative-time link (e.g. "Friday, May 31, 2019 5:46 AM").
		var date string
		if el, err := post.Element(`a[data-role="relative-time"]`); err == nil {
			if t, _ := el.Attribute("title"); t != nil && *t != "" {
				// Parse "Monday, January 2, 2006 3:04 PM" style.
				if parsed, err := time.Parse("Monday, January 2, 2006 3:04 PM", *t); err == nil {
					date = parsed.UTC().Format(time.RFC3339)
				} else {
					date = *t // store raw if parse fails
				}
			}
		}

		// Body: all paragraph text inside .post-message.
		var bodyParts []string
		if el, err := post.Element(".post-message"); err == nil {
			paras, _ := el.Elements("p")
			for _, p := range paras {
				if t := strings.TrimSpace(p.MustText()); t != "" {
					bodyParts = append(bodyParts, t)
				}
			}
			// Fallback: full text if no paragraphs.
			if len(bodyParts) == 0 {
				if t := strings.TrimSpace(el.MustText()); t != "" {
					bodyParts = []string{t}
				}
			}
		}
		body := strings.Join(bodyParts, "\n")

		if author != "" || body != "" {
			comments = append(comments, Comment{
				Author: author,
				Date:   date,
				Body:   body,
			})
		}
	}

	return comments, nil
}

// parseEpisodeNumber extracts the integer episode number from a title string.
// Handles all observed naming formats:
//   - "NZ615", "NZ 612", "NZ615." — new short format
//   - "Niezatapialni 578", "Niezatapialni578" — old full-word format
//   - "Niezatapialni 566,5" — fractional (takes integer part only)
//
// Returns nil if no number can be extracted (e.g. articles, specials).
func parseEpisodeNumber(title string) *int {
	re := regexp.MustCompile(`(?i)(?:niezatapialni|NZ)\s*(\d+)`)
	m := re.FindStringSubmatch(title)
	if m == nil {
		return nil
	}
	n, err := strconv.Atoi(m[1])
	if err != nil {
		return nil
	}
	return &n
}
