// Package imdb implements the IMDB benchmark e2e suite.
// It downloads IMDB TSV datasets, seeds a project with Movie/Person graph objects
// and relationships, then runs natural-language agent queries to verify the graph.
package imdb

import (
	"bufio"
	"compress/gzip"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	sdk "github.com/emergent-company/emergent/apps/server-go/pkg/sdk"
	"github.com/emergent-company/emergent/apps/server-go/pkg/sdk/graph"
	"github.com/emergent-company/emergent/tools/e2e-suite/suite"
)

// Suite implements the IMDB benchmark.
type Suite struct{}

func (s *Suite) Name() string        { return "imdb" }
func (s *Suite) Description() string { return "Seed IMDB graph data and run agent queries" }

// config holds IMDB-specific env vars.
type config struct {
	DatasetBaseURL string // IMDB_DATASET_URL (default: https://datasets.imdbws.com)
	MinVotes       int    // IMDB_MIN_VOTES (default: 20000)
	AgentDefID     string // IMDB_AGENT_DEF_ID (required)
}

func loadConfig() (*config, error) {
	cfg := &config{
		DatasetBaseURL: getEnv("IMDB_DATASET_URL", "https://datasets.imdbws.com"),
		AgentDefID:     os.Getenv("IMDB_AGENT_DEF_ID"),
	}
	if v := os.Getenv("IMDB_MIN_VOTES"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			cfg.MinVotes = n
		}
	}
	if cfg.MinVotes <= 0 {
		cfg.MinVotes = 20000
	}
	if cfg.AgentDefID == "" {
		return nil, fmt.Errorf("IMDB_AGENT_DEF_ID is required for the imdb suite")
	}
	return cfg, nil
}

func getEnv(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

// Run executes the IMDB suite phases.
func (s *Suite) Run(ctx context.Context, client *sdk.Client, cfg *suite.Config) (*suite.Result, error) {
	result := suite.NewResult(s.Name())

	imdbCfg, err := loadConfig()
	if err != nil {
		return result, err
	}

	// --- Phase 1: check if already seeded ---
	count, err := client.Graph.CountObjects(ctx, &graph.CountObjectsOptions{Type: "Movie"})
	if err != nil {
		return result, fmt.Errorf("counting Movie objects: %w", err)
	}
	fmt.Printf("Movie count in project: %d\n", count)

	if count < 100 {
		fmt.Println("Seeding IMDB data...")
		if err := seedDatabase(ctx, client, imdbCfg); err != nil {
			return result, fmt.Errorf("seeding database: %w", err)
		}
	} else {
		fmt.Println("Data already seeded, skipping seed phase.")
	}

	// --- Phase 2: agent queries ---
	queries := []struct {
		id       string
		name     string
		query    string
		contains []string
	}{
		{
			id:       "actor_intersection",
			name:     "ActorIntersection: Tom Hanks & Meg Ryan",
			query:    "Did Tom Hanks and Meg Ryan ever act in the same movie together? Name the movies.",
			contains: []string{"sleepless in seattle", "you've got mail"},
		},
		{
			id:       "complex_traversal",
			name:     "ComplexTraversal: Spielberg 1990s movies",
			query:    "Find me movies from the 1990s directed by Steven Spielberg.",
			contains: []string{"jurassic park", "schindler"},
		},
		{
			id:       "genre_rating",
			name:     "GenreAndRating: top sci-fi after 2010",
			query:    "What are the top rated Sci-Fi movies released after 2010?",
			contains: []string{"interstellar", "inception", "arrival"},
		},
	}

	for _, q := range queries {
		start := time.Now()
		response, usedTools, err := runAgentQuery(ctx, client, cfg, imdbCfg.AgentDefID, q.query)
		dur := time.Since(start)

		item := suite.ItemResult{
			ID:       q.id,
			Name:     q.name,
			Duration: dur,
		}

		if err != nil {
			item.Status = suite.StatusFailed
			item.Error = err.Error()
			result.AddItem(item)
			continue
		}

		if len(usedTools) == 0 {
			item.Status = suite.StatusFailed
			item.Error = "agent did not use any tools"
			result.AddItem(item)
			continue
		}

		lower := strings.ToLower(response)
		matched := false
		for _, c := range q.contains {
			if strings.Contains(lower, c) {
				matched = true
				break
			}
		}

		if matched {
			item.Status = suite.StatusPassed
		} else {
			item.Status = suite.StatusFailed
			item.Error = fmt.Sprintf("response did not contain any of %v", q.contains)
		}
		result.AddItem(item)
	}

	return result, nil
}

// =============================================================================
// Data types
// =============================================================================

type movieData struct {
	ID          string
	Title       string
	ReleaseYear int
	RuntimeMins int
	Rating      float64
	Genres      []string
}

type personData struct {
	ID        string
	Name      string
	BirthYear int
}

type principalRole struct {
	MovieID   string
	PersonID  string
	Role      string // actor, actress, director, writer
	Character string
}

// =============================================================================
// Dataset streaming helpers
// =============================================================================

func streamIMDBFile(url string) (*http.Response, *gzip.Reader, error) {
	resp, err := http.Get(url) //nolint:noctx
	if err != nil {
		return nil, nil, err
	}
	if resp.StatusCode != http.StatusOK {
		resp.Body.Close()
		return nil, nil, fmt.Errorf("IMDB dataset returned HTTP %d for %s", resp.StatusCode, url)
	}
	reader, err := gzip.NewReader(resp.Body)
	if err != nil {
		resp.Body.Close()
		return nil, nil, err
	}
	return resp, reader, nil
}

func getFilteredMovieIDs(baseURL string, minVotes int) (map[string]float64, error) {
	fmt.Printf("Streaming title.ratings (minVotes=%d)...\n", minVotes)
	resp, reader, err := streamIMDBFile(baseURL + "/title.ratings.tsv.gz")
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	defer reader.Close()

	scanner := bufio.NewScanner(reader)
	scanner.Scan() // skip header

	filtered := make(map[string]float64)
	for scanner.Scan() {
		parts := strings.Split(scanner.Text(), "\t")
		if len(parts) != 3 {
			continue
		}
		votes, err := strconv.Atoi(parts[2])
		if err != nil || votes <= minVotes {
			continue
		}
		rating, _ := strconv.ParseFloat(parts[1], 64)
		filtered[parts[0]] = rating
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	fmt.Printf("  -> %d titles with >%d votes\n", len(filtered), minVotes)
	return filtered, nil
}

func getMovieMetadata(baseURL string, filteredRatings map[string]float64) (map[string]movieData, error) {
	fmt.Println("Streaming title.basics...")
	resp, reader, err := streamIMDBFile(baseURL + "/title.basics.tsv.gz")
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	defer reader.Close()

	scanner := bufio.NewScanner(reader)
	scanner.Scan() // skip header

	movies := make(map[string]movieData)
	for scanner.Scan() {
		parts := strings.Split(scanner.Text(), "\t")
		if len(parts) < 9 {
			continue
		}
		tconst := parts[0]
		rating, ok := filteredRatings[tconst]
		if !ok || parts[1] != "movie" {
			continue
		}
		year, _ := strconv.Atoi(parts[5])
		runtime, _ := strconv.Atoi(parts[7])
		var genres []string
		if parts[8] != `\N` {
			genres = strings.Split(parts[8], ",")
		}
		movies[tconst] = movieData{
			ID:          tconst,
			Title:       parts[2],
			ReleaseYear: year,
			RuntimeMins: runtime,
			Rating:      rating,
			Genres:      genres,
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	fmt.Printf("  -> %d movies\n", len(movies))
	return movies, nil
}

func getPrincipals(baseURL string, movies map[string]movieData) ([]principalRole, map[string]bool, error) {
	fmt.Println("Streaming title.principals...")
	resp, reader, err := streamIMDBFile(baseURL + "/title.principals.tsv.gz")
	if err != nil {
		return nil, nil, err
	}
	defer resp.Body.Close()
	defer reader.Close()

	scanner := bufio.NewScanner(reader)
	scanner.Scan() // skip header

	var roles []principalRole
	personIDs := make(map[string]bool)

	for scanner.Scan() {
		parts := strings.Split(scanner.Text(), "\t")
		if len(parts) < 6 {
			continue
		}
		tconst := parts[0]
		if _, ok := movies[tconst]; !ok {
			continue
		}
		category := parts[3]
		if category != "actor" && category != "actress" && category != "director" && category != "writer" {
			continue
		}
		nconst := parts[2]
		personIDs[nconst] = true
		character := ""
		if parts[5] != `\N` {
			character = strings.Trim(parts[5], `[]"`)
		}
		roles = append(roles, principalRole{
			MovieID:   tconst,
			PersonID:  nconst,
			Role:      category,
			Character: character,
		})
	}
	if err := scanner.Err(); err != nil {
		return nil, nil, err
	}
	fmt.Printf("  -> %d relationships, %d people\n", len(roles), len(personIDs))
	return roles, personIDs, nil
}

func getPeopleMetadata(baseURL string, targetPersonIDs map[string]bool) (map[string]personData, error) {
	fmt.Println("Streaming name.basics...")
	resp, reader, err := streamIMDBFile(baseURL + "/name.basics.tsv.gz")
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	defer reader.Close()

	scanner := bufio.NewScanner(reader)
	scanner.Scan() // skip header

	people := make(map[string]personData)
	for scanner.Scan() {
		parts := strings.Split(scanner.Text(), "\t")
		if len(parts) < 3 {
			continue
		}
		nconst := parts[0]
		if !targetPersonIDs[nconst] {
			continue
		}
		birthYear, _ := strconv.Atoi(parts[2])
		people[nconst] = personData{
			ID:        nconst,
			Name:      parts[1],
			BirthYear: birthYear,
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	fmt.Printf("  -> %d people\n", len(people))
	return people, nil
}

// =============================================================================
// Database seeding
// =============================================================================

func seedDatabase(ctx context.Context, client *sdk.Client, cfg *config) error {
	filteredRatings, err := getFilteredMovieIDs(cfg.DatasetBaseURL, cfg.MinVotes)
	if err != nil {
		return err
	}
	movies, err := getMovieMetadata(cfg.DatasetBaseURL, filteredRatings)
	if err != nil {
		return err
	}
	roles, personIDs, err := getPrincipals(cfg.DatasetBaseURL, movies)
	if err != nil {
		return err
	}
	people, err := getPeopleMetadata(cfg.DatasetBaseURL, personIDs)
	if err != nil {
		return err
	}

	if err := batchInsertEntities(ctx, client, movies, people); err != nil {
		return err
	}
	return batchInsertRelationships(ctx, client, roles)
}

const batchSize = 100

func batchInsertEntities(ctx context.Context, client *sdk.Client, movies map[string]movieData, people map[string]personData) error {
	var items []graph.CreateObjectRequest

	movieCount := 0
	for _, m := range movies {
		if movieCount >= 6000 {
			break
		}
		movieCount++
		key := m.ID
		items = append(items, graph.CreateObjectRequest{
			Type: "Movie",
			Key:  &key,
			Properties: map[string]any{
				"title":        m.Title,
				"name":         m.Title,
				"release_year": m.ReleaseYear,
				"runtime_mins": m.RuntimeMins,
				"rating":       m.Rating,
			},
		})
	}

	personCount := 0
	for _, p := range people {
		if personCount >= 10000 {
			break
		}
		personCount++
		key := p.ID
		items = append(items, graph.CreateObjectRequest{
			Type: "Person",
			Key:  &key,
			Properties: map[string]any{
				"name":       p.Name,
				"birth_year": p.BirthYear,
			},
		})
	}

	fmt.Printf("Inserting %d movies + %d people in batches of %d...\n", movieCount, personCount, batchSize)
	for i := 0; i < len(items); i += batchSize {
		end := i + batchSize
		if end > len(items) {
			end = len(items)
		}
		_, err := client.Graph.BulkCreateObjects(ctx, &graph.BulkCreateObjectsRequest{Items: items[i:end]})
		if err != nil {
			return fmt.Errorf("BulkCreateObjects batch %d: %w", i/batchSize, err)
		}
	}
	fmt.Println("Entity insertion complete.")
	return nil
}

func fetchCanonicalIDsByType(ctx context.Context, client *sdk.Client, objType string) (map[string]string, error) {
	result := make(map[string]string)
	var cursor string

	for {
		opts := &graph.ListObjectsOptions{
			Type:  objType,
			Limit: 1000,
		}
		if cursor != "" {
			opts.Cursor = cursor
		}

		page, err := client.Graph.ListObjects(ctx, opts)
		if err != nil {
			return nil, fmt.Errorf("ListObjects(%s): %w", objType, err)
		}

		for _, item := range page.Items {
			if item.Key == nil {
				continue
			}
			id := item.EntityID
			if id == "" {
				id = item.CanonicalID
			}
			result[*item.Key] = id
		}

		if page.NextCursor == nil || *page.NextCursor == "" {
			break
		}
		cursor = *page.NextCursor
	}
	return result, nil
}

func batchInsertRelationships(ctx context.Context, client *sdk.Client, roles []principalRole) error {
	movieIDs, err := fetchCanonicalIDsByType(ctx, client, "Movie")
	if err != nil {
		return err
	}
	personIDs, err := fetchCanonicalIDsByType(ctx, client, "Person")
	if err != nil {
		return err
	}
	fmt.Printf("Loaded %d movie IDs, %d person IDs from API\n", len(movieIDs), len(personIDs))

	var items []graph.CreateRelationshipRequest
	roleCount := 0
	for _, role := range roles {
		if roleCount >= 20000 {
			break
		}
		srcID, srcOk := personIDs[role.PersonID]
		dstID, dstOk := movieIDs[role.MovieID]
		if !srcOk || !dstOk {
			continue
		}
		relType := "ACTED_IN"
		if role.Role == "director" {
			relType = "DIRECTED"
		} else if role.Role == "writer" {
			relType = "WROTE"
		}
		props := map[string]any{}
		if role.Character != "" {
			props["character_name"] = role.Character
		}
		items = append(items, graph.CreateRelationshipRequest{
			Type:       relType,
			SrcID:      srcID,
			DstID:      dstID,
			Properties: props,
		})
		roleCount++
	}

	fmt.Printf("Inserting %d relationships in batches of %d...\n", len(items), batchSize)
	for i := 0; i < len(items); i += batchSize {
		end := i + batchSize
		if end > len(items) {
			end = len(items)
		}
		_, err := client.Graph.BulkCreateRelationships(ctx, &graph.BulkCreateRelationshipsRequest{Items: items[i:end]})
		if err != nil {
			return fmt.Errorf("BulkCreateRelationships batch %d: %w", i/batchSize, err)
		}
	}
	fmt.Println("Relationship insertion complete.")
	return nil
}

// =============================================================================
// Agent queries
// =============================================================================

func runAgentQuery(ctx context.Context, client *sdk.Client, cfg *suite.Config, agentDefID, query string) (string, []string, error) {
	reqBody := map[string]any{
		"message":           query,
		"agentDefinitionId": agentDefID,
	}
	bodyBytes, _ := json.Marshal(reqBody)

	req, err := http.NewRequestWithContext(ctx, "POST",
		cfg.ServerURL+"/api/chat/stream",
		strings.NewReader(string(bodyBytes)),
	)
	if err != nil {
		return "", nil, err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := client.Do(ctx, req)
	if err != nil {
		return "", nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", nil, fmt.Errorf("agent query returned HTTP %d", resp.StatusCode)
	}

	// Read SSE body
	var sb strings.Builder
	buf := make([]byte, 4096)
	for {
		n, err := resp.Body.Read(buf)
		if n > 0 {
			sb.Write(buf[:n])
		}
		if err != nil {
			break
		}
	}

	events := parseSSEEvents(sb.String())
	var usedTools []string
	var responseBuilder strings.Builder

	for _, evt := range events {
		if evt["type"] == "mcp_tool" {
			if status, _ := evt["status"].(string); status == "started" {
				if toolName, _ := evt["tool"].(string); toolName != "" {
					usedTools = append(usedTools, toolName)
				}
			}
		} else if evt["type"] == "token" {
			if token, _ := evt["token"].(string); token != "" {
				responseBuilder.WriteString(token)
			}
		}
	}

	return responseBuilder.String(), usedTools, nil
}

func parseSSEEvents(body string) []map[string]any {
	var events []map[string]any
	for _, line := range strings.Split(body, "\n") {
		if !strings.HasPrefix(line, "data: ") {
			continue
		}
		data := strings.TrimPrefix(line, "data: ")
		var event map[string]any
		if err := json.Unmarshal([]byte(data), &event); err == nil {
			events = append(events, event)
		}
	}
	return events
}
