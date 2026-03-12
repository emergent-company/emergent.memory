package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"
)

const (
	redditPublicSearchEndpoint = "https://www.reddit.com/search.json"
	redditPublicSubEndpoint    = "https://www.reddit.com/r/%s/%s.json"
	redditDefaultCount         = 10
	redditMaxCount             = 25
	redditUserAgent            = "emergent-memory-agent/1.0 (knowledge graph research tool)"
)

// redditListing is the top-level Reddit API listing response
type redditListing struct {
	Data redditListingData `json:"data"`
}

type redditListingData struct {
	Children []redditChild `json:"children"`
	After    string        `json:"after"`
}

type redditChild struct {
	Data redditPost `json:"data"`
}

type redditPost struct {
	ID          string  `json:"id"`
	Title       string  `json:"title"`
	Selftext    string  `json:"selftext"`
	URL         string  `json:"url"`
	Permalink   string  `json:"permalink"`
	Subreddit   string  `json:"subreddit"`
	Author      string  `json:"author"`
	Score       int     `json:"score"`
	NumComments int     `json:"num_comments"`
	Created     float64 `json:"created_utc"`
	IsSelf      bool    `json:"is_self"`
	Flair       string  `json:"link_flair_text"`
}

// redditSearchResult is the MCP tool result format
type redditSearchResult struct {
	Query       string            `json:"query"`
	Subreddit   string            `json:"subreddit,omitempty"`
	Sort        string            `json:"sort"`
	ResultCount int               `json:"resultCount"`
	Posts       []redditPostEntry `json:"posts"`
}

type redditPostEntry struct {
	Title       string `json:"title"`
	URL         string `json:"url"`
	RedditURL   string `json:"reddit_url"`
	Subreddit   string `json:"subreddit"`
	Author      string `json:"author"`
	Score       int    `json:"score"`
	NumComments int    `json:"num_comments"`
	Flair       string `json:"flair,omitempty"`
	Selftext    string `json:"selftext,omitempty"`
	CreatedUTC  string `json:"created_utc"`
}

var redditSearchDefaultTimeout = 15 * time.Second

// executeRedditSearch executes a Reddit search or subreddit listing using the
// public unauthenticated JSON API — no OAuth credentials required.
func (s *Service) executeRedditSearch(ctx context.Context, _ string, args map[string]any) (*ToolResult, error) {
	// Parse arguments
	query, _ := args["query"].(string)
	subreddit, _ := args["subreddit"].(string)
	sort, _ := args["sort"].(string)
	if sort == "" {
		sort = "hot"
	}
	timeFilter, _ := args["time"].(string)
	if timeFilter == "" {
		timeFilter = "day"
	}

	count := redditDefaultCount
	if c, ok := args["count"].(float64); ok {
		count = int(c)
	}
	if count < 1 {
		count = 1
	}
	if count > redditMaxCount {
		count = redditMaxCount
	}

	if query == "" && subreddit == "" {
		return &ToolResult{
			Content: []ContentBlock{{Type: "text", Text: "Missing required argument: provide either 'query' or 'subreddit'"}},
			IsError: true,
		}, nil
	}

	// Execute search or subreddit listing
	var (
		posts []redditPost
		err   error
	)
	if query != "" {
		posts, err = s.searchRedditPublic(ctx, query, subreddit, sort, timeFilter, count)
	} else {
		posts, err = s.getSubredditPostsPublic(ctx, subreddit, sort, timeFilter, count)
	}

	if err != nil {
		s.log.Error("reddit API call failed", "error", err)
		return &ToolResult{
			Content: []ContentBlock{{Type: "text", Text: fmt.Sprintf("Reddit API error: %s", err.Error())}},
			IsError: true,
		}, nil
	}

	// Transform to result
	entries := make([]redditPostEntry, 0, len(posts))
	for _, p := range posts {
		createdAt := time.Unix(int64(p.Created), 0).UTC().Format(time.RFC3339)
		selftext := p.Selftext
		if len(selftext) > 500 {
			selftext = selftext[:500] + "…"
		}
		if p.Title == "" || p.Author == "[deleted]" {
			continue
		}
		entries = append(entries, redditPostEntry{
			Title:       p.Title,
			URL:         p.URL,
			RedditURL:   "https://www.reddit.com" + p.Permalink,
			Subreddit:   p.Subreddit,
			Author:      p.Author,
			Score:       p.Score,
			NumComments: p.NumComments,
			Flair:       p.Flair,
			Selftext:    selftext,
			CreatedUTC:  createdAt,
		})
	}

	result := redditSearchResult{
		Query:       query,
		Subreddit:   subreddit,
		Sort:        sort,
		ResultCount: len(entries),
		Posts:       entries,
	}

	return s.wrapResult(result)
}

// searchRedditPublic searches Reddit using the public .json API (no auth required).
func (s *Service) searchRedditPublic(ctx context.Context, query, subreddit, sort, timeFilter string, count int) ([]redditPost, error) {
	params := url.Values{}
	params.Set("q", query)
	params.Set("sort", sort)
	params.Set("t", timeFilter)
	params.Set("limit", fmt.Sprintf("%d", count))
	params.Set("type", "link")

	var endpoint string
	if subreddit != "" {
		endpoint = fmt.Sprintf("https://www.reddit.com/r/%s/search.json?%s&restrict_sr=1", subreddit, params.Encode())
	} else {
		endpoint = redditPublicSearchEndpoint + "?" + params.Encode()
	}

	return s.doRedditPublicRequest(ctx, endpoint)
}

// getSubredditPostsPublic gets posts from a subreddit using the public .json API.
func (s *Service) getSubredditPostsPublic(ctx context.Context, subreddit, sort, timeFilter string, count int) ([]redditPost, error) {
	params := url.Values{}
	params.Set("limit", fmt.Sprintf("%d", count))
	if sort == "top" {
		params.Set("t", timeFilter)
	}
	endpoint := fmt.Sprintf(redditPublicSubEndpoint, subreddit, sort) + "?" + params.Encode()
	return s.doRedditPublicRequest(ctx, endpoint)
}

// doRedditPublicRequest makes an unauthenticated GET request to the Reddit public JSON API.
func (s *Service) doRedditPublicRequest(ctx context.Context, endpoint string) ([]redditPost, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("User-Agent", redditUserAgent)
	req.Header.Set("Accept", "application/json")

	client := &http.Client{Timeout: redditSearchDefaultTimeout}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("execute request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusTooManyRequests {
		return nil, fmt.Errorf("rate limited by Reddit API (429) — retry after a delay")
	}
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return nil, fmt.Errorf("Reddit API returned %d: %s", resp.StatusCode, string(body))
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, 5*1024*1024))
	if err != nil {
		return nil, fmt.Errorf("read response: %w", err)
	}

	var listing redditListing
	if err := json.Unmarshal(body, &listing); err != nil {
		return nil, fmt.Errorf("decode listing: %w", err)
	}

	posts := make([]redditPost, 0, len(listing.Data.Children))
	for _, child := range listing.Data.Children {
		posts = append(posts, child.Data)
	}
	return posts, nil
}

// getRedditSearchToolDefinition returns the MCP tool definition for Reddit search
func getRedditSearchToolDefinition() ToolDefinition {
	return ToolDefinition{
		Name:        "web-search-reddit",
		Description: "Search Reddit posts or browse subreddit listings. Returns post titles, URLs, scores, comment counts, and authors. Use 'query' to search all of Reddit, 'subreddit' to browse a specific community, or both to search within a subreddit. No API key required.",
		InputSchema: InputSchema{
			Type: "object",
			Properties: map[string]PropertySchema{
				"query": {
					Type:        "string",
					Description: "Search query string (optional if 'subreddit' is provided)",
				},
				"subreddit": {
					Type:        "string",
					Description: "Subreddit name to search within or browse (without r/ prefix, e.g. 'MachineLearning'). Optional if 'query' is provided.",
				},
				"sort": {
					Type:        "string",
					Description: "Sort order: hot (default), new, top, relevance (for search)",
					Enum:        []string{"hot", "new", "top", "relevance"},
					Default:     "hot",
				},
				"time": {
					Type:        "string",
					Description: "Time filter for 'top' sort: hour, day (default), week, month, year, all",
					Enum:        []string{"hour", "day", "week", "month", "year", "all"},
					Default:     "day",
				},
				"count": {
					Type:        "number",
					Description: "Number of posts to return (default: 10, max: 25)",
					Minimum:     intPtr(1),
					Maximum:     intPtr(25),
					Default:     10,
				},
			},
			Required: []string{},
		},
	}
}
