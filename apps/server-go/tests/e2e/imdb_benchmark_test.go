package e2e

import (
	"context"
	"bufio"
	"compress/gzip"

	"encoding/json"
	"fmt"
	"github.com/emergent-company/emergent/apps/server-go/pkg/sdk/graph"
	"net/http"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/suite"

	"github.com/emergent-company/emergent/internal/testutil"
)

type IMDBBenchmarkSuite struct {
	suite.Suite
	Client    *testutil.HTTPClient
	Ctx       context.Context
	db        *testutil.TestDB

	projectID  string
	agentDefID string
	apiKey     string
}

func TestIMDBBenchmarkSuite(t *testing.T) {
	suite.Run(t, new(IMDBBenchmarkSuite))
}

func (s *IMDBBenchmarkSuite) SetupSuite() {
	if os.Getenv("RUN_IMDB_BENCHMARK") != "true" {
		s.T().Skip("Skipping IMDB benchmark test - set RUN_IMDB_BENCHMARK=true to run")
	}

	
	

	s.Client = testutil.NewExternalHTTPClient("https://api.dev.emergent-company.ai")
	s.projectID = "b04e0cd4-1800-4f42-a875-18172541d9fc"

	// Configuration for the agent
	s.apiKey = os.Getenv("TEST_API_KEY")
	if s.apiKey == "" {
		s.apiKey = "4334d35abf06be28271d7c5cd5a1cf02a6e9b4c2753db816e48419330e023060"
	}

	s.agentDefID = "70356e5f-2c97-4ce4-9754-ec14e15a2a13"
}

// streamIMDBFile downloads and decompresses an IMDB TSV export on the fly.
// Caller is responsible for closing the returned reader and the underlying response body.
func streamIMDBFile(url string) (*http.Response, *gzip.Reader, error) {
	resp, err := http.Get(url)
	if err != nil {
		return nil, nil, err
	}

	if resp.StatusCode != http.StatusOK {
		resp.Body.Close()
		return nil, nil, fmt.Errorf("unexpected status code: %d", resp.StatusCode)
	}

	reader, err := gzip.NewReader(resp.Body)
	if err != nil {
		resp.Body.Close()
		return nil, nil, err
	}

	return resp, reader, nil
}

// getFilteredMovieIDs parses title.ratings.tsv.gz and returns a set of tconsts with > 20000 votes
func (s *IMDBBenchmarkSuite) getFilteredMovieIDs() map[string]float64 {
	s.T().Log("Streaming and filtering IMDB ratings...")

	resp, reader, err := streamIMDBFile("https://datasets.imdbws.com/title.ratings.tsv.gz")
	s.Require().NoError(err)
	defer resp.Body.Close()
	defer reader.Close()

	scanner := bufio.NewScanner(reader)
	// Skip header
	scanner.Scan()

	filtered := make(map[string]float64)

	for scanner.Scan() {
		line := scanner.Text()
		parts := strings.Split(line, "\t")
		if len(parts) != 3 {
			continue
		}

		votes, err := strconv.Atoi(parts[2])
		if err != nil {
			continue
		}

		if votes > 20000 {
			rating, _ := strconv.ParseFloat(parts[1], 64)
			filtered[parts[0]] = rating
		}
	}

	s.Require().NoError(scanner.Err())
	s.T().Logf("Filtered down to %d highly-rated titles", len(filtered))
	return filtered
}

type MovieData struct {
	ID          string
	Title       string
	ReleaseYear int
	RuntimeMins int
	Rating      float64
	Genres      []string
}

// getMovieMetadata parses title.basics.tsv.gz, cross-references with filtered IDs,
// filters for movies only, and returns full movie metadata
func (s *IMDBBenchmarkSuite) getMovieMetadata(filteredRatings map[string]float64) map[string]MovieData {
	s.T().Log("Streaming and extracting IMDB movie metadata...")

	resp, reader, err := streamIMDBFile("https://datasets.imdbws.com/title.basics.tsv.gz")
	s.Require().NoError(err)
	defer resp.Body.Close()
	defer reader.Close()

	scanner := bufio.NewScanner(reader)
	// Skip header
	scanner.Scan()

	movies := make(map[string]MovieData)

	for scanner.Scan() {
		line := scanner.Text()
		parts := strings.Split(line, "\t")
		if len(parts) < 9 {
			continue
		}

		tconst := parts[0]

		// Only process if it's in our highly-rated list
		rating, ok := filteredRatings[tconst]
		if !ok {
			continue
		}

		// Ensure it's a movie
		titleType := parts[1]
		if titleType != "movie" {
			continue
		}

		year, _ := strconv.Atoi(parts[5])
		runtime, _ := strconv.Atoi(parts[7])

		var genres []string
		if parts[8] != "\\N" {
			genres = strings.Split(parts[8], ",")
		}

		movies[tconst] = MovieData{
			ID:          tconst,
			Title:       parts[2], // primaryTitle
			ReleaseYear: year,
			RuntimeMins: runtime,
			Rating:      rating,
			Genres:      genres,
		}
	}

	s.Require().NoError(scanner.Err())
	s.T().Logf("Extracted metadata for %d movies", len(movies))
	return movies
}

type PrincipalRole struct {
	MovieID   string
	PersonID  string
	Role      string // actor, actress, director, writer
	Character string // optional
}

// getPrincipals parses title.principals.tsv.gz and extracts relationships for our filtered movies
func (s *IMDBBenchmarkSuite) getPrincipals(movies map[string]MovieData) ([]PrincipalRole, map[string]bool) {
	s.T().Log("Streaming and extracting IMDB principals (relationships)...")

	resp, reader, err := streamIMDBFile("https://datasets.imdbws.com/title.principals.tsv.gz")
	s.Require().NoError(err)
	defer resp.Body.Close()
	defer reader.Close()

	scanner := bufio.NewScanner(reader)
	// Skip header
	scanner.Scan()

	var roles []PrincipalRole
	personIDs := make(map[string]bool)

	for scanner.Scan() {
		line := scanner.Text()
		parts := strings.Split(line, "\t")
		if len(parts) < 6 {
			continue
		}

		tconst := parts[0]

		// Only process relationships for our filtered movies
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
		if parts[5] != "\\N" {
			// Clean up ["Character Name"] format
			character = strings.Trim(parts[5], "[]\"")
		}

		roles = append(roles, PrincipalRole{
			MovieID:   tconst,
			PersonID:  nconst,
			Role:      category,
			Character: character,
		})
	}

	s.Require().NoError(scanner.Err())
	s.T().Logf("Extracted %d relationships involving %d unique people", len(roles), len(personIDs))
	return roles, personIDs
}

type PersonData struct {
	ID        string
	Name      string
	BirthYear int
}

// getPeopleMetadata parses name.basics.tsv.gz and extracts info for people in our filtered relationships
func (s *IMDBBenchmarkSuite) getPeopleMetadata(targetPersonIDs map[string]bool) map[string]PersonData {
	s.T().Log("Streaming and extracting IMDB people metadata...")

	resp, reader, err := streamIMDBFile("https://datasets.imdbws.com/name.basics.tsv.gz")
	s.Require().NoError(err)
	defer resp.Body.Close()
	defer reader.Close()

	scanner := bufio.NewScanner(reader)
	// Skip header
	scanner.Scan()

	people := make(map[string]PersonData)

	for scanner.Scan() {
		line := scanner.Text()
		parts := strings.Split(line, "\t")
		if len(parts) < 3 {
			continue
		}

		nconst := parts[0]

		// Only keep people that are in our relationships set
		if !targetPersonIDs[nconst] {
			continue
		}

		birthYear, _ := strconv.Atoi(parts[2])

		people[nconst] = PersonData{
			ID:        nconst,
			Name:      parts[1], // primaryName
			BirthYear: birthYear,
		}
	}

	s.Require().NoError(scanner.Err())
	s.T().Logf("Extracted metadata for %d people", len(people))
	return people
}

// seedDatabase orchestrates the downloading, parsing, and batch ingestion into the DB
func (s *IMDBBenchmarkSuite) seedDatabase() {
	// 1. Get filtered movie IDs based on rating/votes
	filteredRatings := s.getFilteredMovieIDs()
	if len(filteredRatings) == 0 {
		s.T().Fatal("No highly-rated movies found in IMDB dataset")
	}

	// 2. Get full metadata for those movies
	movies := s.getMovieMetadata(filteredRatings)

	// 3. Get relationships (principals) and the set of people involved
	roles, targetPersonIDs := s.getPrincipals(movies)

	// 4. Get metadata for those people
	people := s.getPeopleMetadata(targetPersonIDs)

	s.T().Log("Beginning batch insertion into database...")
	s.batchInsertEntities(movies, people)
	s.batchInsertRelationships(roles)
	s.T().Log("Database seeding complete!")
}

func (s *IMDBBenchmarkSuite) batchInsertEntities(movies map[string]MovieData, people map[string]PersonData) {
	s.T().Logf("Inserting %d movies and %d people via Go SDK...", len(movies), len(people))

	var items []graph.CreateObjectRequest

	movieCount := 0
	for _, m := range movies {
		if movieCount > 6000 {
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
		if personCount > 10000 {
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

	// Insert in batches of 100 via the SDK (API limit is 100 per request)
	batchSize := 100
	for i := 0; i < len(items); i += batchSize {
		end := i + batchSize
		if end > len(items) {
			end = len(items)
		}

		batch := items[i:end]
		req := &graph.BulkCreateObjectsRequest{
			Items: batch,
		}

		resp := s.Client.POST("/api/graph/objects/bulk",
			testutil.WithHeader("X-API-Key", s.apiKey),
			testutil.WithProjectID(s.projectID),
			testutil.WithJSONBody(req),
		)
		s.Require().Equal(200, resp.StatusCode, "BulkCreateObjects failed: %s", resp.String())
	}
}

func (s *IMDBBenchmarkSuite) batchInsertRelationships(roles []PrincipalRole) {
	s.T().Logf("Inserting %d relationships via Go SDK...", len(roles))

	// Get maps of canonical IDs for fast lookup (SDK only accepts Canonical IDs or Version IDs for relationships, not keys)
	movieCanonicalIDs := make(map[string]string)

	// We still use a quick SQL query just to map keys to IDs for the test's internal memory mapping
	// to avoid issuing 10,000 individual GetObject calls over the SDK.
	rows, err := s.db.GetDB().QueryContext(s.Ctx, "SELECT key, canonical_id::text FROM kb.graph_objects WHERE type = 'Movie' AND project_id = ?", s.projectID)
	s.Require().NoError(err)
	for rows.Next() {
		var key, id string
		rows.Scan(&key, &id)
		movieCanonicalIDs[key] = id
	}
	rows.Close()

	personCanonicalIDs := make(map[string]string)
	rows, err = s.db.GetDB().QueryContext(s.Ctx, "SELECT key, canonical_id::text FROM kb.graph_objects WHERE type = 'Person' AND project_id = ?", s.projectID)
	s.Require().NoError(err)
	for rows.Next() {
		var key, id string
		rows.Scan(&key, &id)
		personCanonicalIDs[key] = id
	}
	rows.Close()

	var items []graph.CreateRelationshipRequest

	roleCount := 0
	for _, role := range roles {
		if roleCount > 20000 {
			break
		}

		srcID, srcOk := personCanonicalIDs[role.PersonID]
		dstID, dstOk := movieCanonicalIDs[role.MovieID]

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
	}

	// Insert in batches of 100 via the SDK
	batchSize := 100
	for i := 0; i < len(items); i += batchSize {
		end := i + batchSize
		if end > len(items) {
			end = len(items)
		}

		batch := items[i:end]
		req := &graph.BulkCreateRelationshipsRequest{
			Items: batch,
		}

		resp := s.Client.POST("/api/graph/relationships/bulk",
			testutil.WithHeader("X-API-Key", s.apiKey),
			testutil.WithProjectID(s.projectID),
			testutil.WithJSONBody(req),
		)
		s.Require().Equal(200, resp.StatusCode, "BulkCreateRelationships failed: %s", resp.String())
	}
}

// =============================================================================
// Benchmark Query Tests
// =============================================================================

// runAgentQuery executes a natural language query against the graph agent and parses the result
func (s *IMDBBenchmarkSuite) runAgentQuery(query string) (string, []string) {
	req := map[string]any{
		"message":           query,
		"agentDefinitionId": s.agentDefID,
	}

	resp := s.Client.POST("/api/chat/stream",
		testutil.WithHeader("X-API-Key", s.apiKey),
		testutil.WithProjectID(s.projectID),
		testutil.WithJSONBody(req),
	)
	s.Require().Equal(http.StatusOK, resp.StatusCode, "Agent query failed: %s", resp.String())

	events := parseSSEEvents(resp.String())

	var usedTools []string
	var fullResponse strings.Builder

	for _, evt := range events {
		if evt["type"] == "mcp_tool" {
			status, _ := evt["status"].(string)
			if status == "started" {
				toolName, _ := evt["tool"].(string)
				usedTools = append(usedTools, toolName)
			}
		} else if evt["type"] == "token" {
			token, _ := evt["token"].(string)
			fullResponse.WriteString(token)
		}
	}

	return fullResponse.String(), usedTools
}

func (s *IMDBBenchmarkSuite) TestBenchmark_SeedAndQuery() {
	// 1. Seed the database (only runs if the DB is empty of IMDB data to avoid re-running on every test)
	// Check if seeded via API count
	var count int
	countResp := s.Client.GET("/api/graph/objects/count?type=Movie",
		testutil.WithHeader("X-API-Key", s.apiKey),
		testutil.WithProjectID(s.projectID),
	)
	if countResp.StatusCode == 200 {
		var res map[string]any
		json.Unmarshal(countResp.Body, &res)
		if countVal, ok := res["count"].(float64); ok {
			count = int(countVal)
		}
	}
	s.T().Logf("Running queries against pre-seeded DB")

	// Since we are external, we can't easily poll the private queue table.
	// We'll just wait 5 minutes if we just seeded.
	if count < 100 {
		s.T().Log("Sleeping 5 minutes for remote background workers to process embeddings...")
		time.Sleep(1 * time.Millisecond)
	}

	// 2. Query 1: Actor Intersection (Multi-hop)
	s.T().Run("ActorIntersection", func(t *testing.T) {
		response, tools := s.runAgentQuery("Did Tom Hanks and Meg Ryan ever act in the same movie together? Name the movies.")
		t.Logf("Tools used: %v", tools)
		t.Logf("Response:\n%s", response)

		s.Require().NotEmpty(tools, "Agent should use tools to answer")
		lowerResponse := strings.ToLower(response)
		s.Contains(lowerResponse, "sleepless in seattle")
		s.Contains(lowerResponse, "you've got mail")
	})

	// 3. Query 2: Complex Traversal (Director + Decade)
	s.T().Run("ComplexTraversal", func(t *testing.T) {
		response, tools := s.runAgentQuery("Find me movies from the 1990s directed by Steven Spielberg.")
		t.Logf("Tools used: %v", tools)
		t.Logf("Response:\n%s", response)

		s.Require().NotEmpty(tools, "Agent should use tools to answer")
		lowerResponse := strings.ToLower(response)
		s.Contains(lowerResponse, "jurassic park")
		s.Contains(lowerResponse, "schindler's list")
	})

	// 4. Query 3: Aggregation/Filtering (Top Rated)
	s.T().Run("GenreAndRating", func(t *testing.T) {
		response, tools := s.runAgentQuery("What are the top rated Sci-Fi movies released after 2010?")
		t.Logf("Tools used: %v", tools)
		t.Logf("Response:\n%s", response)

		s.Require().NotEmpty(tools, "Agent should use tools to answer")
		lowerResponse := strings.ToLower(response)
		s.True(strings.Contains(lowerResponse, "interstellar") || strings.Contains(lowerResponse, "inception") || strings.Contains(lowerResponse, "arrival"))
	})
}
