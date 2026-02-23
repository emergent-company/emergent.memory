// extraction-bench: End-to-end benchmark for document extraction quality.
//
// Generates a synthetic prose document from a hardcoded IMDB-style dataset,
// uploads it via the document API, runs an extraction job, then scores the
// extracted graph objects and relationships against the known ground truth
// using precision and recall metrics.
//
// Usage:
//
//	go run ./cmd/extraction-bench/ \
//	  --host https://api.dev.emergent-company.ai \
//	  --api-key emt_xxx... \
//	  --project-id <uuid>
//
// Optional flags:
//
//	--min-entity-recall    float  Minimum entity recall threshold (default 0.60)
//	--min-entity-precision float  Minimum entity precision threshold (default 0.50)
//	--min-rel-recall       float  Minimum relationship recall threshold (default 0.50)
//	--min-rel-precision    float  Minimum relationship precision threshold (default 0.40)
//	--poll-timeout         int    Seconds to wait for extraction job (default 120)
//	--log-file             string Path to append JSONL run record (default docs/tests/extraction_bench_log.jsonl)
//
// Each run appends one JSON line to --log-file for cross-run comparison.
// Exit code 0 = all thresholds met; exit code 1 = threshold failure or error.
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/emergent-company/emergent/apps/server-go/pkg/sdk"
	"github.com/emergent-company/emergent/apps/server-go/pkg/sdk/documents"
	"github.com/emergent-company/emergent/apps/server-go/pkg/sdk/graph"
)

// ─── Version ─────────────────────────────────────────────────────────────────

const benchVersion = "1.0.0"

// ─── Ground-truth data types ──────────────────────────────────────────────────

// Movie represents a film in the ground-truth dataset.
type Movie struct {
	ID          string
	Title       string
	Year        int
	Genre       string
	RuntimeMins int
}

// Person represents a person (actor, director, writer) in the ground-truth dataset.
type Person struct {
	ID        string
	Name      string
	BirthYear int
}

// Relationship represents a directed relationship between a person and a movie.
// Type is one of: ACTED_IN, DIRECTED, WROTE
type Relationship struct {
	Type     string // ACTED_IN | DIRECTED | WROTE
	PersonID string
	MovieID  string
}

// ─── Hardcoded ground-truth dataset ──────────────────────────────────────────
// Fixed at compile time — never fetched from the network.

var groundTruthMovies = []Movie{
	{ID: "m01", Title: "The Shawshank Redemption", Year: 1994, Genre: "Drama", RuntimeMins: 142},
	{ID: "m02", Title: "The Godfather", Year: 1972, Genre: "Crime", RuntimeMins: 175},
	{ID: "m03", Title: "The Dark Knight", Year: 2008, Genre: "Action", RuntimeMins: 152},
	{ID: "m04", Title: "Schindler's List", Year: 1993, Genre: "Drama", RuntimeMins: 195},
	{ID: "m05", Title: "Pulp Fiction", Year: 1994, Genre: "Crime", RuntimeMins: 154},
	{ID: "m06", Title: "Forrest Gump", Year: 1994, Genre: "Drama", RuntimeMins: 142},
	{ID: "m07", Title: "The Matrix", Year: 1999, Genre: "Sci-Fi", RuntimeMins: 136},
	{ID: "m08", Title: "Goodfellas", Year: 1990, Genre: "Crime", RuntimeMins: 146},
	{ID: "m09", Title: "Fight Club", Year: 1999, Genre: "Drama", RuntimeMins: 139},
	{ID: "m10", Title: "Inception", Year: 2010, Genre: "Sci-Fi", RuntimeMins: 148},
}

var groundTruthPeople = []Person{
	{ID: "p01", Name: "Morgan Freeman", BirthYear: 1937},
	{ID: "p02", Name: "Tim Robbins", BirthYear: 1958},
	{ID: "p03", Name: "Frank Darabont", BirthYear: 1959},
	{ID: "p04", Name: "Marlon Brando", BirthYear: 1924},
	{ID: "p05", Name: "Al Pacino", BirthYear: 1940},
	{ID: "p06", Name: "Francis Ford Coppola", BirthYear: 1939},
	{ID: "p07", Name: "Christian Bale", BirthYear: 1974},
	{ID: "p08", Name: "Christopher Nolan", BirthYear: 1970},
	{ID: "p09", Name: "Liam Neeson", BirthYear: 1952},
	{ID: "p10", Name: "Steven Spielberg", BirthYear: 1946},
	{ID: "p11", Name: "John Travolta", BirthYear: 1954},
	{ID: "p12", Name: "Quentin Tarantino", BirthYear: 1963},
	{ID: "p13", Name: "Tom Hanks", BirthYear: 1956},
	{ID: "p14", Name: "Robert Zemeckis", BirthYear: 1952},
	{ID: "p15", Name: "Keanu Reeves", BirthYear: 1964},
	{ID: "p16", Name: "Lana Wachowski", BirthYear: 1965},
	{ID: "p17", Name: "Ray Liotta", BirthYear: 1954},
	{ID: "p18", Name: "Martin Scorsese", BirthYear: 1942},
	{ID: "p19", Name: "Brad Pitt", BirthYear: 1963},
	{ID: "p20", Name: "David Fincher", BirthYear: 1962},
	{ID: "p21", Name: "Leonardo DiCaprio", BirthYear: 1974},
}

var groundTruthRelationships = []Relationship{
	// The Shawshank Redemption
	{Type: "ACTED_IN", PersonID: "p01", MovieID: "m01"}, // Morgan Freeman
	{Type: "ACTED_IN", PersonID: "p02", MovieID: "m01"}, // Tim Robbins
	{Type: "DIRECTED", PersonID: "p03", MovieID: "m01"}, // Frank Darabont
	{Type: "WROTE", PersonID: "p03", MovieID: "m01"},    // Frank Darabont
	// The Godfather
	{Type: "ACTED_IN", PersonID: "p04", MovieID: "m02"}, // Marlon Brando
	{Type: "ACTED_IN", PersonID: "p05", MovieID: "m02"}, // Al Pacino
	{Type: "DIRECTED", PersonID: "p06", MovieID: "m02"}, // Francis Ford Coppola
	{Type: "WROTE", PersonID: "p06", MovieID: "m02"},    // Francis Ford Coppola
	// The Dark Knight
	{Type: "ACTED_IN", PersonID: "p07", MovieID: "m03"}, // Christian Bale
	{Type: "DIRECTED", PersonID: "p08", MovieID: "m03"}, // Christopher Nolan
	{Type: "WROTE", PersonID: "p08", MovieID: "m03"},    // Christopher Nolan
	// Schindler's List
	{Type: "ACTED_IN", PersonID: "p09", MovieID: "m04"}, // Liam Neeson
	{Type: "DIRECTED", PersonID: "p10", MovieID: "m04"}, // Steven Spielberg
	{Type: "WROTE", PersonID: "p10", MovieID: "m04"},    // Steven Spielberg (screenplay)
	// Pulp Fiction
	{Type: "ACTED_IN", PersonID: "p11", MovieID: "m05"}, // John Travolta
	{Type: "DIRECTED", PersonID: "p12", MovieID: "m05"}, // Quentin Tarantino
	{Type: "WROTE", PersonID: "p12", MovieID: "m05"},    // Quentin Tarantino
	// Forrest Gump
	{Type: "ACTED_IN", PersonID: "p13", MovieID: "m06"}, // Tom Hanks
	{Type: "DIRECTED", PersonID: "p14", MovieID: "m06"}, // Robert Zemeckis
	// The Matrix
	{Type: "ACTED_IN", PersonID: "p15", MovieID: "m07"}, // Keanu Reeves
	{Type: "DIRECTED", PersonID: "p16", MovieID: "m07"}, // Lana Wachowski
	{Type: "WROTE", PersonID: "p16", MovieID: "m07"},    // Lana Wachowski
	// Goodfellas
	{Type: "ACTED_IN", PersonID: "p17", MovieID: "m08"}, // Ray Liotta
	{Type: "DIRECTED", PersonID: "p18", MovieID: "m08"}, // Martin Scorsese
	// Fight Club
	{Type: "ACTED_IN", PersonID: "p19", MovieID: "m09"}, // Brad Pitt
	{Type: "DIRECTED", PersonID: "p20", MovieID: "m09"}, // David Fincher
	// Inception
	{Type: "ACTED_IN", PersonID: "p21", MovieID: "m10"}, // Leonardo DiCaprio
	{Type: "DIRECTED", PersonID: "p08", MovieID: "m10"}, // Christopher Nolan
	{Type: "WROTE", PersonID: "p08", MovieID: "m10"},    // Christopher Nolan
}

// ─── Document generator ───────────────────────────────────────────────────────

// generateDocument produces a deterministic, richly detailed plain-English
// document from the ground-truth dataset. Each movie receives a full essay
// spanning overview, direction, screenplay, cast, production history,
// cinematography, music, critical reception, awards, and box office.
// Off-schema details (cinematographers, composers, budgets, etc.) are woven
// throughout as realistic noise that the extractor should ignore.
// The output is byte-identical on every invocation.
func generateDocument(_ []Movie, _ []Person, _ []Relationship) string {
	// All text is hardcoded — deterministic by construction.
	// Names from the ground-truth dataset are repeated naturally across sections
	// so that fuzzy matching can find them even mid-sentence.
	// Off-schema names (Roger Deakins, Hans Zimmer, etc.) appear only in prose.
	return movieEssays
}

// movieEssays is the synthetic benchmark document — ~2,500 words covering
// 10 films, 21 people, and 29 relationships. Kept compact so LLM extraction
// completes in under 60 seconds. Byte-identical on every run.
const movieEssays = `CINEMA HIGHLIGHTS: TEN ESSENTIAL FILMS
A concise reference guide to landmark cinema.

THE SHAWSHANK REDEMPTION (1994)
Genre: Drama | Runtime: 142 minutes

The Shawshank Redemption (1994) is a drama directed and written by Frank Darabont, based
on a Stephen King novella. Morgan Freeman stars as Red, a veteran prisoner serving a life
sentence, and Tim Robbins plays Andy Dufresne, a banker wrongfully convicted of murder.
Darabont's screenplay captures the unlikely friendship between the two men over nearly two
decades inside Shawshank State Penitentiary. The film earned seven Academy Award nominations.

THE GODFATHER (1972)
Genre: Crime | Runtime: 175 minutes

The Godfather (1972) is a crime film directed by Francis Ford Coppola from a screenplay
he co-wrote. Marlon Brando delivers an iconic performance as Vito Corleone, the aging
patriarch of a powerful Mafia family. Al Pacino plays Michael Corleone, Vito's youngest
son who reluctantly becomes the family's new don. Coppola's direction transformed Mario
Puzo's novel into one of the most acclaimed films ever made. The picture won the Academy
Award for Best Picture.

THE DARK KNIGHT (2008)
Genre: Action | Runtime: 152 minutes

The Dark Knight (2008) is an action film written and directed by Christopher Nolan.
Christian Bale portrays Bruce Wayne — also known as Batman — as he faces a nihilistic
criminal mastermind called the Joker. Nolan co-wrote the screenplay with his brother
Jonathan Nolan. The film grossed over one billion dollars worldwide and is widely
considered among the greatest superhero films ever produced.

SCHINDLER'S LIST (1993)
Genre: Drama | Runtime: 195 minutes

Schindler's List (1993) is a historical drama directed by Steven Spielberg. Liam Neeson
stars as Oskar Schindler, a German industrialist who saved the lives of more than a
thousand Jewish refugees during the Holocaust. Spielberg also wrote the screenplay
adaptation. The film won seven Academy Awards including Best Picture and Best Director,
and is regarded as one of the greatest films in cinema history.

PULP FICTION (1994)
Genre: Crime | Runtime: 154 minutes

Pulp Fiction (1994) is a crime anthology film written and directed by Quentin Tarantino.
John Travolta stars as Vincent Vega, a hitman paired with Jules Winnfield in an
interconnected series of Los Angeles crime vignettes. Tarantino's non-linear screenplay
won the Academy Award for Best Original Screenplay and launched the careers of numerous
cast members. The film's influence on independent cinema is immeasurable.

FORREST GUMP (1994)
Genre: Drama | Runtime: 142 minutes

Forrest Gump (1994) is a drama directed by Robert Zemeckis. Tom Hanks plays the title
character, a kind-hearted man from Alabama who unwittingly influences several defining
events of the twentieth century. The film swept the Academy Awards, winning Best Picture,
Best Director, Best Actor for Hanks, and three other awards. Zemeckis's direction blends
seamlessly with the film's groundbreaking visual effects.

THE MATRIX (1999)
Genre: Sci-Fi | Runtime: 136 minutes

The Matrix (1999) is a science-fiction action film written and directed by Lana Wachowski.
Keanu Reeves stars as Thomas Anderson — known by the hacker alias Neo — a computer
programmer who discovers that reality is a simulated construct controlled by machines.
The Wachowskis wrote an original screenplay that combined Hong Kong action choreography
with philosophical themes drawn from cyberpunk literature. The film won four Academy Awards
and spawned two sequels.

GOODFELLAS (1990)
Genre: Crime | Runtime: 146 minutes

Goodfellas (1990) is a crime drama directed by Martin Scorsese. Ray Liotta stars as Henry
Hill, a half-Irish, half-Sicilian mobster who rises through the ranks of the Lucchese crime
family in New York. Scorsese's kinetic direction and voice-over narration technique became
enormously influential on subsequent crime cinema. The film is consistently listed among
the greatest movies ever made.

FIGHT CLUB (1999)
Genre: Drama | Runtime: 139 minutes

Fight Club (1999) is a psychological thriller directed by David Fincher. Brad Pitt delivers
a charismatic performance as Tyler Durden, an anarchic soap salesman who co-founds an
underground fight club with a disaffected office worker. Fincher's direction adapted Chuck
Palahniuk's novel into a subversive meditation on masculinity and consumer culture. The
film has since attracted a devoted cult following.

INCEPTION (2010)
Genre: Sci-Fi | Runtime: 148 minutes

Inception (2010) is a science-fiction thriller written and directed by Christopher Nolan.
Leonardo DiCaprio stars as Dom Cobb, a professional thief who specialises in entering
people's dreams to steal secrets from their subconscious. Nolan's screenplay introduces
the concept of dream-within-a-dream architecture across multiple levels of reality.
The film earned eight Academy Award nominations, winning four, and grossed nearly nine
hundred million dollars worldwide.

`

// ─── HTTP helpers ─────────────────────────────────────────────────────────────

func apiRequest(method, url, apiKey, projectID string, body io.Reader, contentType string) (*http.Response, error) {
	req, err := http.NewRequest(method, url, body)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+apiKey)
	req.Header.Set("X-Project-ID", projectID)
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	return http.DefaultClient.Do(req)
}

// ─── Document upload ──────────────────────────────────────────────────────────

// uploadDocument uploads the synthetic document using the official SDK.
func uploadDocument(ctx context.Context, client *sdk.Client, content string) (string, error) {
	resp, err := client.Documents.Upload(ctx, &documents.UploadFileInput{
		Filename: "extraction-benchmark.txt",
		Reader:   strings.NewReader(content),
	})
	if err != nil {
		return "", fmt.Errorf("upload failed: %w", err)
	}
	return resp.Document.ID, nil
}

// ─── Extraction job ───────────────────────────────────────────────────────────

// createExtractionJob creates an extraction job using manual source_type,
// passing the document text directly. Returns the job ID.
func createExtractionJob(host, apiKey, projectID, docText string) (string, error) {
	payload := map[string]any{
		"project_id":  projectID,
		"source_type": "manual",
		"source_metadata": map[string]any{
			"text": docText,
		},
		"extraction_config": map[string]any{
			"object_schemas": map[string]any{
				"Movie": map[string]any{
					"name":        "Movie",
					"description": "A feature film",
					"properties": map[string]any{
						"name":    map[string]any{"type": "string", "description": "Title of the movie"},
						"year":    map[string]any{"type": "string", "description": "Release year"},
						"genre":   map[string]any{"type": "string", "description": "Primary genre"},
						"runtime": map[string]any{"type": "string", "description": "Runtime in minutes"},
					},
				},
				"Person": map[string]any{
					"name":        "Person",
					"description": "A person involved in making films",
					"properties": map[string]any{
						"name":      map[string]any{"type": "string", "description": "Full name"},
						"birthYear": map[string]any{"type": "string", "description": "Year of birth"},
					},
				},
			},
			"relationship_schemas": map[string]any{
				"ACTED_IN": map[string]any{
					"name":         "ACTED_IN",
					"description":  "A person acted in a movie",
					"source_types": []string{"Person"},
					"target_types": []string{"Movie"},
				},
				"DIRECTED": map[string]any{
					"name":         "DIRECTED",
					"description":  "A person directed a movie",
					"source_types": []string{"Person"},
					"target_types": []string{"Movie"},
				},
				"WROTE": map[string]any{
					"name":         "WROTE",
					"description":  "A person wrote the screenplay for a movie",
					"source_types": []string{"Person"},
					"target_types": []string{"Movie"},
				},
			},
			"target_types": []string{"Movie", "Person"},
		},
	}

	bodyBytes, err := json.Marshal(payload)
	if err != nil {
		return "", fmt.Errorf("marshal job payload: %w", err)
	}

	resp, err := apiRequest("POST", host+"/api/admin/extraction-jobs", apiKey, projectID,
		bytes.NewReader(bodyBytes), "application/json")
	if err != nil {
		return "", fmt.Errorf("create job request: %w", err)
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusCreated {
		return "", fmt.Errorf("create job failed (HTTP %d): %s", resp.StatusCode, respBody)
	}

	var result map[string]any
	if err := json.Unmarshal(respBody, &result); err != nil {
		return "", fmt.Errorf("parse job response: %w", err)
	}

	data, ok := result["data"].(map[string]any)
	if !ok {
		return "", fmt.Errorf("no data field in job response: %s", respBody)
	}
	jobID, ok := data["id"].(string)
	if !ok || jobID == "" {
		return "", fmt.Errorf("no job ID in response: %s", respBody)
	}
	return jobID, nil
}

// ─── Job polling ──────────────────────────────────────────────────────────────

// pollJob polls the extraction job until it completes, fails, or times out.
// Returns the final status string and any error.
func pollJob(host, apiKey, projectID, jobID string, timeoutSecs int) (string, error) {
	deadline := time.Now().Add(time.Duration(timeoutSecs) * time.Second)
	dots := 0

	for time.Now().Before(deadline) {
		time.Sleep(2 * time.Second)

		resp, err := apiRequest("GET", host+"/api/admin/extraction-jobs/"+jobID, apiKey, projectID, nil, "")
		if err != nil {
			return "", fmt.Errorf("poll request: %w", err)
		}
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()

		var result map[string]any
		if err := json.Unmarshal(body, &result); err != nil {
			return "", fmt.Errorf("parse poll response: %w", err)
		}

		data, _ := result["data"].(map[string]any)
		status, _ := data["status"].(string)

		dots++
		fmt.Printf("\r    polling... %s (%ds elapsed)", strings.Repeat(".", dots%4+1), int(time.Since(deadline.Add(-time.Duration(timeoutSecs)*time.Second)).Seconds()))

		switch status {
		case "completed":
			fmt.Println()
			return "completed", nil
		case "failed":
			fmt.Println()
			errMsg, _ := data["error_message"].(string)
			return "failed", fmt.Errorf("extraction job failed: %s", errMsg)
		}
	}

	fmt.Println()
	return "timeout", fmt.Errorf("extraction job did not complete within %d seconds", timeoutSecs)
}

// ─── Graph queries ────────────────────────────────────────────────────────────

// fetchGraphObjects retrieves graph objects created by a specific extraction job.
// The server-side extraction_job_id filter is not implemented, so we fetch all
// objects and filter client-side by the _extraction_job_id property.
// Retries up to 5 times on transient errors.
func fetchGraphObjects(ctx context.Context, client *sdk.Client, jobID string) ([]map[string]any, error) {
	var resp *graph.SearchObjectsResponse
	var err error
	opts := &graph.ListObjectsOptions{Limit: 500}
	for attempt := range 5 {
		resp, err = client.Graph.ListObjects(ctx, opts)
		if err == nil {
			break
		}
		if attempt < 4 {
			fmt.Printf("    (graph objects fetch error, retrying in 3s: %v)\n", err)
			time.Sleep(3 * time.Second)
		}
	}
	if err != nil {
		return nil, fmt.Errorf("fetch objects: %w", err)
	}

	out := make([]map[string]any, 0, len(resp.Items))
	for _, obj := range resp.Items {
		// Filter to objects from this job only via the _extraction_job_id property
		if extractionJobID, _ := obj.Properties["_extraction_job_id"].(string); extractionJobID != jobID {
			continue
		}
		m := map[string]any{
			"id":           obj.VersionID,
			"entity_id":    obj.EntityID,
			"canonical_id": obj.EntityID,
			"type":         obj.Type,
			"properties":   obj.Properties,
			"key":          obj.Key,
		}
		out = append(out, m)
	}
	return out, nil
}

// fetchGraphRelationships retrieves graph relationships created by a specific extraction job.
// Retries up to 5 times on transient errors.
func fetchGraphRelationships(ctx context.Context, client *sdk.Client, jobID string) ([]map[string]any, error) {
	var resp *graph.SearchRelationshipsResponse
	var err error
	// Relationships don't have an ExtractionJobID filter in the SDK, so fetch all and filter by property
	opts := &graph.ListRelationshipsOptions{Limit: 500}
	for attempt := range 5 {
		resp, err = client.Graph.ListRelationships(ctx, opts)
		if err == nil {
			break
		}
		if attempt < 4 {
			fmt.Printf("    (graph relationships fetch error, retrying in 3s: %v)\n", err)
			time.Sleep(3 * time.Second)
		}
	}
	if err != nil {
		return nil, fmt.Errorf("fetch relationships: %w", err)
	}

	out := make([]map[string]any, 0, len(resp.Items))
	for _, rel := range resp.Items {
		// Filter to relationships from this job only
		if extractionJobID, _ := rel.Properties["_extraction_job_id"].(string); extractionJobID != jobID {
			continue
		}
		m := map[string]any{
			"id":            rel.VersionID,
			"entity_id":     rel.EntityID,
			"canonical_id":  rel.EntityID,
			"type":          rel.Type,
			"src_entity_id": rel.SrcID,
			"dst_entity_id": rel.DstID,
		}
		out = append(out, m)
	}
	return out, nil
}

// ─── Scoring engine ───────────────────────────────────────────────────────────

// fuzzyMatch returns true if either string is a case-insensitive substring of
// the other. This handles minor LLM variations (article drops, punctuation).
func fuzzyMatch(a, b string) bool {
	al, bl := strings.ToLower(a), strings.ToLower(b)
	return strings.Contains(al, bl) || strings.Contains(bl, al)
}

// objectName extracts the primary identifying name from a graph object,
// trying "name" then "title" in Properties.
func objectName(obj map[string]any) string {
	props, _ := obj["properties"].(map[string]any)
	if props != nil {
		if n, ok := props["name"].(string); ok && n != "" {
			return n
		}
		if t, ok := props["title"].(string); ok && t != "" {
			return t
		}
	}
	// Fallback: try top-level key field
	if k, ok := obj["key"].(string); ok && k != "" {
		return k
	}
	return ""
}

// EntityScore holds scoring results for entities.
type EntityScore struct {
	Precision float64
	Recall    float64
	Matched   []string // ground-truth names that were found
	Missing   []string // ground-truth names not found
	Spurious  []string // extracted names not in ground truth
	// entityID → canonical name mapping for matched entities (used in rel scoring)
	matchedObjectIDs map[string]string // extractedEntityID → gtName
}

// scoreEntities compares extracted graph objects to the ground-truth people and movies.
func scoreEntities(extracted []map[string]any, people []Person, movies []Movie) EntityScore {
	// Build combined ground-truth name list
	gtNames := make([]string, 0, len(people)+len(movies))
	for _, p := range people {
		gtNames = append(gtNames, p.Name)
	}
	for _, m := range movies {
		gtNames = append(gtNames, m.Title)
	}

	matchedSet := map[string]bool{} // gtName → matched
	matchedObjectIDs := map[string]string{}

	extractedNames := make([]string, 0, len(extracted))
	spurious := []string{}

	for _, obj := range extracted {
		name := objectName(obj)
		if name == "" {
			continue
		}
		extractedNames = append(extractedNames, name)

		// Check if this extracted object matches any ground-truth name
		foundGT := ""
		for _, gt := range gtNames {
			if fuzzyMatch(name, gt) {
				foundGT = gt
				break
			}
		}
		if foundGT != "" {
			matchedSet[foundGT] = true
			// Store mapping from extracted entity ID to GT name (try entity_id, canonical_id, id)
			eid := extractedEntityID(obj)
			if eid != "" {
				matchedObjectIDs[eid] = foundGT
			}
		} else {
			spurious = append(spurious, name)
		}
	}

	matched := []string{}
	missing := []string{}
	for _, gt := range gtNames {
		if matchedSet[gt] {
			matched = append(matched, gt)
		} else {
			missing = append(missing, gt)
		}
	}

	precision := 0.0
	if len(extractedNames) > 0 {
		precision = float64(len(matched)) / float64(len(extractedNames))
	}
	recall := 0.0
	if len(gtNames) > 0 {
		recall = float64(len(matched)) / float64(len(gtNames))
	}

	return EntityScore{
		Precision:        precision,
		Recall:           recall,
		Matched:          matched,
		Missing:          missing,
		Spurious:         spurious,
		matchedObjectIDs: matchedObjectIDs,
	}
}

func extractedEntityID(obj map[string]any) string {
	if id, ok := obj["entity_id"].(string); ok && id != "" {
		return id
	}
	if id, ok := obj["canonical_id"].(string); ok && id != "" {
		return id
	}
	if id, ok := obj["id"].(string); ok && id != "" {
		return id
	}
	return ""
}

// RelScore holds scoring results for relationships.
type RelScore struct {
	Precision float64
	Recall    float64
	Matched   []string
	Missing   []string
	Spurious  []string
}

// relLabel builds a human-readable relationship label.
func relLabel(personName, relType, movieTitle string) string {
	return fmt.Sprintf("%s %s %s", personName, relType, movieTitle)
}

// normalizeRelType maps LLM-generated relationship type names to the canonical
// ground-truth types (ACTED_IN, DIRECTED, WROTE). The LLM may produce inverted
// forms (DIRECTED_BY, STARS, WRITTEN_BY) or synonyms.
func normalizeRelType(t string) string {
	switch strings.ToUpper(t) {
	case "ACTED_IN", "STARS", "STARRED_IN", "APPEARS_IN", "FEATURED_IN":
		return "ACTED_IN"
	case "DIRECTED", "DIRECTED_BY", "DIRECTED_THE":
		return "DIRECTED"
	case "WROTE", "WRITTEN_BY", "WRITES", "AUTHORED", "WROTE_SCREENPLAY":
		return "WROTE"
	}
	return strings.ToUpper(t)
}

// scoreRelationships compares extracted graph relationships to ground-truth relationships.
// A ground-truth relationship is "matched" if an extracted relationship has the same
// (normalized) type and both endpoints fuzzy-match the expected person and movie,
// regardless of which direction the extracted relationship runs (person→movie or movie→person).
func scoreRelationships(extractedRels []map[string]any, rels []Relationship, entityScore EntityScore,
	people []Person, movies []Movie) RelScore {

	// Build person/movie name lookups
	personByID := map[string]string{}
	for _, p := range people {
		personByID[p.ID] = p.Name
	}
	movieByID := map[string]string{}
	for _, m := range movies {
		movieByID[m.ID] = m.Title
	}

	// For each extracted relationship, resolve src and dst entity names from our matched map.
	// We try both (src, dst) and (dst, src) so inverted relationships still score correctly.
	type relTuple struct{ nameA, nameB, relType string }
	extractedTuples := make([]relTuple, 0, len(extractedRels))

	for _, r := range extractedRels {
		relType, _ := r["type"].(string)
		srcID := relEntityID(r, "src")
		dstID := relEntityID(r, "dst")

		srcName := entityScore.matchedObjectIDs[srcID]
		dstName := entityScore.matchedObjectIDs[dstID]

		// Include if at least one endpoint is in our matched set
		if (srcName != "" || dstName != "") && relType != "" {
			extractedTuples = append(extractedTuples, relTuple{srcName, dstName, normalizeRelType(relType)})
		}
	}

	// Score ground-truth relationships
	matched := []string{}
	missing := []string{}

	for _, gtRel := range rels {
		pName := personByID[gtRel.PersonID]
		mTitle := movieByID[gtRel.MovieID]
		label := relLabel(pName, gtRel.Type, mTitle)
		normType := normalizeRelType(gtRel.Type)

		found := false
		for _, et := range extractedTuples {
			if et.relType == normType &&
				((fuzzyMatch(et.nameA, pName) && fuzzyMatch(et.nameB, mTitle)) ||
					(fuzzyMatch(et.nameB, pName) && fuzzyMatch(et.nameA, mTitle))) {
				found = true
				break
			}
		}
		if found {
			matched = append(matched, label)
		} else {
			missing = append(missing, label)
		}
	}

	// Spurious: extracted tuples that don't match any ground-truth
	spurious := []string{}
	for _, et := range extractedTuples {
		found := false
		for _, gtRel := range rels {
			pName := personByID[gtRel.PersonID]
			mTitle := movieByID[gtRel.MovieID]
			normType := normalizeRelType(gtRel.Type)
			if et.relType == normType &&
				((fuzzyMatch(et.nameA, pName) && fuzzyMatch(et.nameB, mTitle)) ||
					(fuzzyMatch(et.nameB, pName) && fuzzyMatch(et.nameA, mTitle))) {
				found = true
				break
			}
		}
		if !found {
			spurious = append(spurious, relLabel(et.nameA, et.relType, et.nameB))
		}
	}

	precision := 0.0
	if len(extractedTuples) > 0 {
		precision = float64(len(matched)) / float64(len(extractedTuples))
	}
	recall := 0.0
	if len(rels) > 0 {
		recall = float64(len(matched)) / float64(len(rels))
	}

	return RelScore{
		Precision: precision,
		Recall:    recall,
		Matched:   matched,
		Missing:   missing,
		Spurious:  spurious,
	}
}

func relEntityID(r map[string]any, side string) string {
	// Try src_entity_id / dst_entity_id, then src_id / dst_id
	for _, key := range []string{side + "_entity_id", side + "_canonical_id", side + "_id"} {
		if v, ok := r[key].(string); ok && v != "" {
			return v
		}
	}
	return ""
}

// ─── Results output ───────────────────────────────────────────────────────────

type Thresholds struct {
	EntityRecall    float64
	EntityPrecision float64
	RelRecall       float64
	RelPrecision    float64
}

func passSymbol(ok bool) string {
	if ok {
		return "✓"
	}
	return "✗"
}

func printResultsTable(es EntityScore, rs RelScore, th Thresholds) {
	fmt.Println()
	fmt.Println("╔══════════════════════════════════════════════════════════════╗")
	fmt.Println("║          Extraction Benchmark — Quality Results              ║")
	fmt.Println("╠══════════════════════════════════════════════════════════════╣")
	fmt.Printf("║  %-22s  %8s  %9s  %6s  ║\n", "Metric", "Value", "Threshold", "Status")
	fmt.Println("╠══════════════════════════════════════════════════════════════╣")

	printRow := func(name string, value, threshold float64) {
		ok := value >= threshold
		fmt.Printf("║  %-22s  %7.1f%%  %8.1f%%  %6s  ║\n",
			name, value*100, threshold*100, passSymbol(ok))
	}

	printRow("Entity Recall", es.Recall, th.EntityRecall)
	printRow("Entity Precision", es.Precision, th.EntityPrecision)
	printRow("Rel Recall", rs.Recall, th.RelRecall)
	printRow("Rel Precision", rs.Precision, th.RelPrecision)
	fmt.Println("╚══════════════════════════════════════════════════════════════╝")

	fmt.Printf("\nENTITY RESULTS  (matched %d, missing %d, spurious %d)\n",
		len(es.Matched), len(es.Missing), len(es.Spurious))
	if len(es.Matched) > 0 {
		fmt.Printf("  Matched  : %s\n", strings.Join(es.Matched, ", "))
	}
	if len(es.Missing) > 0 {
		fmt.Printf("  Missing  : %s\n", strings.Join(es.Missing, ", "))
	}
	if len(es.Spurious) > 0 {
		fmt.Printf("  Spurious : %s\n", strings.Join(es.Spurious, ", "))
	}

	fmt.Printf("\nRELATIONSHIP RESULTS  (matched %d, missing %d, spurious %d)\n",
		len(rs.Matched), len(rs.Missing), len(rs.Spurious))
	if len(rs.Matched) > 0 {
		fmt.Printf("  Matched  : %s\n", strings.Join(rs.Matched, "\n             "))
	}
	if len(rs.Missing) > 0 {
		fmt.Printf("  Missing  : %s\n", strings.Join(rs.Missing, "\n             "))
	}
	if len(rs.Spurious) > 0 {
		fmt.Printf("  Spurious : %s\n", strings.Join(rs.Spurious, "\n             "))
	}
}

// ─── JSONL run log ────────────────────────────────────────────────────────────

// RunRecord is appended to the log file as a single JSON line per run.
type RunRecord struct {
	Timestamp       time.Time `json:"timestamp"`
	BenchVersion    string    `json:"bench_version"`
	Host            string    `json:"host"`
	ProjectID       string    `json:"project_id"`
	DocID           string    `json:"doc_id"`
	JobID           string    `json:"job_id"`
	EntityRecall    float64   `json:"entity_recall"`
	EntityPrecision float64   `json:"entity_precision"`
	RelRecall       float64   `json:"rel_recall"`
	RelPrecision    float64   `json:"rel_precision"`
	ThresholdsMet   bool      `json:"thresholds_met"`
	DurationSecs    float64   `json:"duration_secs"`
}

// appendRunLog appends one JSON line to the log file, creating it if needed.
func appendRunLog(logFile string, record RunRecord) error {
	if err := os.MkdirAll(filepath.Dir(logFile), 0755); err != nil {
		return fmt.Errorf("create log dir: %w", err)
	}
	f, err := os.OpenFile(logFile, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0644)
	if err != nil {
		return fmt.Errorf("open log file: %w", err)
	}
	defer f.Close()

	line, err := json.Marshal(record)
	if err != nil {
		return fmt.Errorf("marshal record: %w", err)
	}
	_, err = f.Write(append(line, '\n'))
	return err
}

// ─── main ─────────────────────────────────────────────────────────────────────

func main() {
	// ── Flags ──
	host := flag.String("host", "", "Base URL of the target server (required)")
	apiKey := flag.String("api-key", "", "API key for authentication (required)")
	projectID := flag.String("project-id", "", "Project ID to run the benchmark against (required)")
	existingJobID := flag.String("job-id", "", "Skip extraction and score an existing completed job ID")
	minEntityRecall := flag.Float64("min-entity-recall", 0.60, "Minimum entity recall threshold")
	minEntityPrecision := flag.Float64("min-entity-precision", 0.50, "Minimum entity precision threshold")
	minRelRecall := flag.Float64("min-rel-recall", 0.50, "Minimum relationship recall threshold")
	minRelPrecision := flag.Float64("min-rel-precision", 0.40, "Minimum relationship precision threshold")
	pollTimeout := flag.Int("poll-timeout", 120, "Seconds to wait for extraction job completion")
	logFile := flag.String("log-file", "docs/tests/extraction_bench_log.jsonl", "Path to append JSONL run record")

	flag.Parse()

	// ── Validate required flags ──
	missing := []string{}
	if *host == "" {
		missing = append(missing, "--host")
	}
	if *apiKey == "" {
		missing = append(missing, "--api-key")
	}
	if *projectID == "" {
		missing = append(missing, "--project-id")
	}
	if len(missing) > 0 {
		fmt.Fprintf(os.Stderr, "error: missing required flags: %s\n\n", strings.Join(missing, ", "))
		flag.Usage()
		os.Exit(1)
	}

	thresholds := Thresholds{
		EntityRecall:    *minEntityRecall,
		EntityPrecision: *minEntityPrecision,
		RelRecall:       *minRelRecall,
		RelPrecision:    *minRelPrecision,
	}

	startTime := time.Now()
	log.SetFlags(0)
	ctx := context.Background()

	// Initialize SDK client (kept for graph queries)
	client, err := sdk.New(sdk.Config{
		ServerURL:  *host,
		ProjectID:  *projectID,
		HTTPClient: &http.Client{Timeout: 5 * time.Minute},
		Auth:       sdk.AuthConfig{Mode: "apitoken", APIKey: *apiKey},
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: SDK init failed: %v\n", err)
		os.Exit(1)
	}

	// ── Phase 1: Generate document text ──
	var jobID string
	var elapsed time.Duration

	if *existingJobID != "" {
		// Skip extraction, score an already-completed job
		fmt.Printf("[skip] Using existing job %s — skipping extraction phases\n", *existingJobID)
		jobID = *existingJobID
		elapsed = 0
	} else {
		// ── Phase 1: Generate document text ──
		fmt.Println("[1/4] Generating synthetic document...")
		doc := generateDocument(groundTruthMovies, groundTruthPeople, groundTruthRelationships)
		fmt.Printf("    ✓ Generated %d chars of film criticism text\n", len(doc))

		// ── Phase 2: Create extraction job ──
		fmt.Println("[2/4] Creating extraction job...")
		jobID, err = createExtractionJob(*host, *apiKey, *projectID, doc)
		if err != nil {
			fmt.Fprintf(os.Stderr, "    ✗ Job creation failed: %v\n", err)
			os.Exit(1)
		}
		fmt.Printf("    ✓ Job %s queued\n", jobID)

		// ── Phase 3: Poll for completion ──
		fmt.Printf("[3/4] Waiting for extraction to complete (timeout: %ds)...\n", *pollTimeout)
		jobStatus, err := pollJob(*host, *apiKey, *projectID, jobID, *pollTimeout)
		if err != nil {
			fmt.Fprintf(os.Stderr, "    ✗ %v\n", err)
			os.Exit(1)
		}
		elapsed = time.Since(startTime)
		fmt.Printf("    ✓ Completed in %.0fs (status: %s)\n", elapsed.Seconds(), jobStatus)
	}

	// ── Phase 4: Query graph ──
	fmt.Println("[4/4] Querying graph for extracted results...")
	extractedObjects, err := fetchGraphObjects(ctx, client, jobID)
	if err != nil {
		fmt.Fprintf(os.Stderr, "    ✗ Failed to fetch objects: %v\n", err)
		os.Exit(1)
	}
	extractedRels, err := fetchGraphRelationships(ctx, client, jobID)
	if err != nil {
		fmt.Fprintf(os.Stderr, "    ✗ Failed to fetch relationships: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("    ✓ Found %d objects, %d relationships\n", len(extractedObjects), len(extractedRels))

	// ── Scoring ──
	entityScore := scoreEntities(extractedObjects, groundTruthPeople, groundTruthMovies)
	relScore := scoreRelationships(extractedRels, groundTruthRelationships, entityScore,
		groundTruthPeople, groundTruthMovies)

	// ── Print results ──
	printResultsTable(entityScore, relScore, thresholds)

	// ── Evaluate thresholds ──
	thresholdsMet := true
	var failures []string
	if entityScore.Recall < thresholds.EntityRecall {
		thresholdsMet = false
		failures = append(failures, fmt.Sprintf("entity recall %.1f%% < %.1f%%",
			entityScore.Recall*100, thresholds.EntityRecall*100))
	}
	if entityScore.Precision < thresholds.EntityPrecision {
		thresholdsMet = false
		failures = append(failures, fmt.Sprintf("entity precision %.1f%% < %.1f%%",
			entityScore.Precision*100, thresholds.EntityPrecision*100))
	}
	if relScore.Recall < thresholds.RelRecall {
		thresholdsMet = false
		failures = append(failures, fmt.Sprintf("rel recall %.1f%% < %.1f%%",
			relScore.Recall*100, thresholds.RelRecall*100))
	}
	if relScore.Precision < thresholds.RelPrecision {
		thresholdsMet = false
		failures = append(failures, fmt.Sprintf("rel precision %.1f%% < %.1f%%",
			relScore.Precision*100, thresholds.RelPrecision*100))
	}

	// ── Append run log ──
	record := RunRecord{
		Timestamp:       time.Now().UTC(),
		BenchVersion:    benchVersion,
		Host:            *host,
		ProjectID:       *projectID,
		DocID:           jobID, // using jobID as run identifier (no upload docID)
		JobID:           jobID,
		EntityRecall:    entityScore.Recall,
		EntityPrecision: entityScore.Precision,
		RelRecall:       relScore.Recall,
		RelPrecision:    relScore.Precision,
		ThresholdsMet:   thresholdsMet,
		DurationSecs:    elapsed.Seconds(),
	}
	if err := appendRunLog(*logFile, record); err != nil {
		fmt.Fprintf(os.Stderr, "\nwarn: could not write run log: %v\n", err)
	} else {
		fmt.Printf("\nRun record appended to %s\n", *logFile)
	}

	// ── Exit ──
	if !thresholdsMet {
		fmt.Fprintf(os.Stderr, "\n✗ THRESHOLDS NOT MET:\n")
		for _, f := range failures {
			fmt.Fprintf(os.Stderr, "  - %s\n", f)
		}
		os.Exit(1)
	}
	fmt.Println("\n✓ All thresholds met.")
}
