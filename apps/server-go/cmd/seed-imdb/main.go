package main

import (
	"bufio"
	"compress/gzip"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/emergent-company/emergent/apps/server-go/pkg/sdk"
	"github.com/emergent-company/emergent/apps/server-go/pkg/sdk/graph"
)

const ProjectID = "956e3e88-07c5-462b-9076-50ea7e1e7951"

const stateDir = "/tmp/imdb_seed_state"

// Phase constants for state.json
const (
	phaseObjectsPending = "objects_pending"
	phaseObjectsDone    = "objects_done"
	phaseRelsPending    = "rels_pending"
	phaseDone           = "done"
)

type SeedState struct {
	Phase string `json:"phase"`
}

// loadState reads state.json; returns default if not found.
func loadState() SeedState {
	data, err := os.ReadFile(filepath.Join(stateDir, "state.json"))
	if err != nil {
		return SeedState{Phase: phaseObjectsPending}
	}
	var s SeedState
	if err := json.Unmarshal(data, &s); err != nil {
		return SeedState{Phase: phaseObjectsPending}
	}
	return s
}

func saveState(s SeedState) {
	os.MkdirAll(stateDir, 0755)
	data, _ := json.MarshalIndent(s, "", "  ")
	os.WriteFile(filepath.Join(stateDir, "state.json"), data, 0644)
}

// loadIDMap reads idmap.json from disk.
func loadIDMap() (map[string]string, error) {
	data, err := os.ReadFile(filepath.Join(stateDir, "idmap.json"))
	if err != nil {
		return nil, err
	}
	var m map[string]string
	if err := json.Unmarshal(data, &m); err != nil {
		return nil, err
	}
	return m, nil
}

func saveIDMap(idMap map[string]string) {
	os.MkdirAll(stateDir, 0755)
	data, _ := json.Marshal(idMap)
	os.WriteFile(filepath.Join(stateDir, "idmap.json"), data, 0644)
}

// loadRelsDone reads rels_done.txt into a set of batch indices already completed.
func loadRelsDone() map[int]bool {
	done := make(map[int]bool)
	f, err := os.Open(filepath.Join(stateDir, "rels_done.txt"))
	if err != nil {
		return done
	}
	defer f.Close()
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if idx, err := strconv.Atoi(line); err == nil {
			done[idx] = true
		}
	}
	return done
}

// appendRelDone appends a batch index to rels_done.txt.
func appendRelDone(idx int) {
	f, err := os.OpenFile(filepath.Join(stateDir, "rels_done.txt"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0644)
	if err != nil {
		return
	}
	defer f.Close()
	fmt.Fprintf(f, "%d\n", idx)
}

// appendRelFailed appends a failed batch (as JSON array) to rels_failed.jsonl.
func appendRelFailed(items []graph.CreateRelationshipRequest) {
	f, err := os.OpenFile(filepath.Join(stateDir, "rels_failed.jsonl"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0644)
	if err != nil {
		return
	}
	defer f.Close()
	data, _ := json.Marshal(items)
	f.Write(data)
	f.Write([]byte("\n"))
}

// loadRelsFailed reads rels_failed.jsonl into a list of batches to retry.
func loadRelsFailed() [][]graph.CreateRelationshipRequest {
	var batches [][]graph.CreateRelationshipRequest
	f, err := os.Open(filepath.Join(stateDir, "rels_failed.jsonl"))
	if err != nil {
		return batches
	}
	defer f.Close()
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 10*1024*1024), 10*1024*1024) // 10MB line buffer
	for scanner.Scan() {
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}
		var batch []graph.CreateRelationshipRequest
		if err := json.Unmarshal(line, &batch); err == nil {
			batches = append(batches, batch)
		}
	}
	return batches
}

func main() {
	os.MkdirAll(stateDir, 0755)

	serverURL := os.Getenv("SERVER_URL")
	if serverURL == "" {
		serverURL = "http://mcj-emergent:3002"
	}
	apiKey := os.Getenv("API_KEY")
	if apiKey == "" {
		apiKey = "emt_ec70233facfa29385abfef9bff015df72f08f7205be51f3034b42bf1484d0ec1"
	}

	limit := 0
	if dryRun := os.Getenv("DRY_RUN"); dryRun == "true" || dryRun == "1" {
		limit = 100
		log.Println("DRY_RUN enabled: Limiting to 100 titles.")
	} else if l := os.Getenv("SEED_LIMIT"); l != "" {
		limit, _ = strconv.Atoi(l)
		log.Printf("SEED_LIMIT set: Limiting to %d titles.\n", limit)
	}

	retryFailed := os.Getenv("RETRY_FAILED") == "true" || os.Getenv("RETRY_FAILED") == "1"

	client, err := sdk.New(sdk.Config{
		ServerURL: serverURL, ProjectID: ProjectID,
		HTTPClient: &http.Client{Timeout: 5 * time.Minute},
		Auth:       sdk.AuthConfig{Mode: "apikey", APIKey: apiKey},
	})
	if err != nil {
		log.Fatal(err)
	}

	log.Printf("Starting STATEFUL IMDB Seeder to %s (Project: %s)", serverURL, ProjectID)
	log.Printf("State directory: %s", stateDir)

	// Set up graceful shutdown
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		sig := <-sigCh
		log.Printf("Received signal %s — shutting down gracefully (state preserved)", sig)
		cancel()
	}()

	state := loadState()
	log.Printf("Resuming from phase: %s", state.Phase)

	// RETRY_FAILED mode: just replay failed batches and exit
	if retryFailed {
		log.Println("RETRY_FAILED mode: replaying rels_failed.jsonl only")
		idMap, err := loadIDMap()
		if err != nil {
			log.Fatalf("Cannot load idmap.json for retry: %v", err)
		}
		_ = idMap // idMap is already embedded in the saved relationship requests (src/dst are canonical IDs)

		failedBatches := loadRelsFailed()
		if len(failedBatches) == 0 {
			log.Println("No failed batches to retry.")
			return
		}
		log.Printf("Retrying %d failed batches...", len(failedBatches))

		// Clear the file before retrying — will re-append any that fail again
		os.Remove(filepath.Join(stateDir, "rels_failed.jsonl"))

		retryRelationshipBatches(ctx, client.Graph, failedBatches)
		log.Println("Retry complete.")
		return
	}

	var idMap map[string]string

	// ── Phase: Objects ──────────────────────────────────────────────────────────
	if state.Phase == phaseObjectsPending {
		filteredRatings, filteredVotes := getFilteredTitleIDs(limit)
		titles, titleGenres := getTitleMetadata(filteredRatings, filteredVotes)
		episodes, seasons := getEpisodes(titles)
		roles, targetPersonIDs, characterNames := getPrincipals(titles)
		crewRoles, crewTargetIDs := getCrew(titles)
		for id := range crewTargetIDs {
			targetPersonIDs[id] = true
		}
		akas := getAKAs(titles)
		people := getPeopleMetadata(targetPersonIDs)

		titleAKAs := make(map[string][]string)
		for _, a := range akas {
			titleAKAs[a.TitleID] = append(titleAKAs[a.TitleID], a.LocalizedTitle)
		}

		idMap = ingestObjects(ctx, client.Graph, titles, episodes, seasons, people, titleGenres, characterNames, titleAKAs)

		if ctx.Err() != nil {
			log.Println("Interrupted during object phase — state NOT advanced (will re-do objects on resume)")
			return
		}

		// Persist idMap and advance state
		log.Println("Saving idmap.json...")
		saveIDMap(idMap)
		state.Phase = phaseObjectsDone
		saveState(state)
		log.Printf("Object phase complete — idMap has %d entries, state saved", len(idMap))

		// Proceed directly to relationship phase using already-loaded data
		state.Phase = phaseRelsPending
		saveState(state)
		ingestRelationships(ctx, client.Graph, roles, crewRoles, titleGenres, titles, episodes, seasons, people, idMap)

	} else if state.Phase == phaseObjectsDone || state.Phase == phaseRelsPending {
		// Resume from relationships — load idMap from disk
		log.Println("Object phase already complete — loading idmap.json from disk")
		var err error
		idMap, err = loadIDMap()
		if err != nil {
			log.Fatalf("Failed to load idmap.json: %v — cannot resume. Delete state and restart.", err)
		}
		log.Printf("Loaded idMap with %d entries", len(idMap))

		// Re-load all data for relationship phase
		filteredRatings, filteredVotes := getFilteredTitleIDs(limit)
		titles, titleGenres := getTitleMetadata(filteredRatings, filteredVotes)
		episodes, seasons := getEpisodes(titles)
		roles, targetPersonIDs, _ := getPrincipals(titles)
		crewRoles, crewTargetIDs := getCrew(titles)
		for id := range crewTargetIDs {
			targetPersonIDs[id] = true
		}
		people := getPeopleMetadata(targetPersonIDs)

		ingestRelationships(ctx, client.Graph, roles, crewRoles, titleGenres, titles, episodes, seasons, people, idMap)

	} else if state.Phase == phaseDone {
		log.Println("Seeding already complete (phase=done). Nothing to do.")
		log.Println("To re-seed, delete /tmp/imdb_seed_state/ and restart.")
		return
	}

	if ctx.Err() != nil {
		log.Println("Interrupted — state preserved for resume.")
		return
	}

	state.Phase = phaseDone
	saveState(state)
	log.Println("Seeding complete! Background workers are now generating vectors.")
}

func streamIMDBFile(url string) (io.ReadCloser, *gzip.Reader, error) {
	filename := url[strings.LastIndex(url, "/")+1:]
	cacheDir := "/tmp/imdb_data"
	os.MkdirAll(cacheDir, 0755)
	localPath := filepath.Join(cacheDir, filename)

	if _, err := os.Stat(localPath); os.IsNotExist(err) {
		log.Printf("Downloading %s...", url)
		resp, _ := http.Get(url)
		outFile, _ := os.Create(localPath)
		io.Copy(outFile, resp.Body)
		outFile.Close()
		resp.Body.Close()
	}
	f, _ := os.Open(localPath)
	reader, _ := gzip.NewReader(f)
	return f, reader, nil
}

func getFilteredTitleIDs(limit int) (map[string]float64, map[string]int) {
	closer, reader, _ := streamIMDBFile("https://datasets.imdbws.com/title.ratings.tsv.gz")
	defer closer.Close()
	defer reader.Close()

	scanner := bufio.NewScanner(reader)
	scanner.Scan()
	ratings, votes := make(map[string]float64), make(map[string]int)

	for scanner.Scan() {
		parts := strings.Split(scanner.Text(), "\t")
		if len(parts) != 3 {
			continue
		}
		if v, _ := strconv.Atoi(parts[2]); v > 5000 {
			r, _ := strconv.ParseFloat(parts[1], 64)
			ratings[parts[0]], votes[parts[0]] = r, v

			if limit > 0 && len(ratings) >= limit {
				break
			}
		}
	}
	log.Printf("Filtered to %d high-visibility titles", len(ratings))
	return ratings, votes
}

type Title struct {
	ID, Name, OriginalName, Type string
	StartYear, EndYear, Runtime  int
	Rating                       float64
	Votes                        int
	IsAdult                      bool
}

func getTitleMetadata(ratings map[string]float64, votes map[string]int) (map[string]Title, map[string][]string) {
	closer, reader, _ := streamIMDBFile("https://datasets.imdbws.com/title.basics.tsv.gz")
	defer closer.Close()
	defer reader.Close()

	scanner := bufio.NewScanner(reader)
	scanner.Scan()
	titles, genres := make(map[string]Title), make(map[string][]string)
	validTypes := map[string]bool{"movie": true, "tvSeries": true, "tvMiniSeries": true, "videoGame": true, "tvEpisode": true, "tvMovie": true, "short": true}

	for scanner.Scan() {
		parts := strings.Split(scanner.Text(), "\t")
		if len(parts) < 9 || !validTypes[parts[1]] {
			continue
		}
		if rating, ok := ratings[parts[0]]; ok {
			sy, _ := strconv.Atoi(parts[5])
			ey, _ := strconv.Atoi(parts[6])
			rt, _ := strconv.Atoi(parts[7])

			titles[parts[0]] = Title{
				ID: parts[0], Name: parts[2], OriginalName: parts[3], Type: parts[1],
				StartYear: sy, EndYear: ey, Runtime: rt, Rating: rating, Votes: votes[parts[0]], IsAdult: parts[4] == "1",
			}
			if parts[8] != "\\N" {
				genres[parts[0]] = strings.Split(parts[8], ",")
			}
		}
	}
	return titles, genres
}

type Episode struct {
	ID, ParentID           string
	SeasonNumber, EpNumber int
}

func getEpisodes(titles map[string]Title) (map[string]Episode, map[string]string) {
	closer, reader, _ := streamIMDBFile("https://datasets.imdbws.com/title.episode.tsv.gz")
	defer closer.Close()
	defer reader.Close()

	scanner := bufio.NewScanner(reader)
	scanner.Scan()
	episodes, seasons := make(map[string]Episode), make(map[string]string)

	for scanner.Scan() {
		parts := strings.Split(scanner.Text(), "\t")
		if len(parts) < 4 {
			continue
		}
		if _, ok := titles[parts[0]]; !ok {
			continue
		}

		sn, _ := strconv.Atoi(parts[2])
		en, _ := strconv.Atoi(parts[3])
		episodes[parts[0]] = Episode{ID: parts[0], ParentID: parts[1], SeasonNumber: sn, EpNumber: en}

		if sn > 0 {
			seasonKey := fmt.Sprintf("%s_s%d", parts[1], sn)
			seasons[seasonKey] = parts[1]
		}
	}
	return episodes, seasons
}

type Role struct {
	TitleID, PersonID, Category, Job string
	Characters                       []string
	Ordering                         int
}

func getPrincipals(titles map[string]Title) ([]Role, map[string]bool, map[string]bool) {
	closer, reader, _ := streamIMDBFile("https://datasets.imdbws.com/title.principals.tsv.gz")
	defer closer.Close()
	defer reader.Close()

	scanner := bufio.NewScanner(reader)
	scanner.Scan()
	var roles []Role
	peopleIDs, characters := make(map[string]bool), make(map[string]bool)

	for scanner.Scan() {
		parts := strings.Split(scanner.Text(), "\t")
		if len(parts) < 6 {
			continue
		}
		if _, ok := titles[parts[0]]; !ok {
			continue
		}

		ordering, _ := strconv.Atoi(parts[1])
		job := parts[4]
		if job == "\\N" {
			job = ""
		}
		peopleIDs[parts[2]] = true

		var chars []string
		if parts[5] != "\\N" {
			json.Unmarshal([]byte(parts[5]), &chars)
			for _, c := range chars {
				characters[c] = true
			}
		}

		roles = append(roles, Role{TitleID: parts[0], PersonID: parts[2], Category: parts[3], Job: job, Characters: chars, Ordering: ordering})
	}
	return roles, peopleIDs, characters
}

type CrewRel struct{ TitleID, PersonID, Role string }

func getCrew(titles map[string]Title) ([]CrewRel, map[string]bool) {
	closer, reader, _ := streamIMDBFile("https://datasets.imdbws.com/title.crew.tsv.gz")
	defer closer.Close()
	defer reader.Close()

	scanner := bufio.NewScanner(reader)
	scanner.Scan()
	var crew []CrewRel
	peopleIDs := make(map[string]bool)

	for scanner.Scan() {
		parts := strings.Split(scanner.Text(), "\t")
		if len(parts) < 3 {
			continue
		}
		if _, ok := titles[parts[0]]; !ok {
			continue
		}

		if parts[1] != "\\N" {
			for _, dID := range strings.Split(parts[1], ",") {
				peopleIDs[dID] = true
				crew = append(crew, CrewRel{parts[0], dID, "DIRECTED"})
			}
		}
		if parts[2] != "\\N" {
			for _, wID := range strings.Split(parts[2], ",") {
				peopleIDs[wID] = true
				crew = append(crew, CrewRel{parts[0], wID, "WROTE"})
			}
		}
	}
	return crew, peopleIDs
}

type AKA struct {
	TitleID, LocalizedTitle, Region, Language, ReleaseType string
	Attributes                                             []string
}

func getAKAs(titles map[string]Title) []AKA {
	closer, reader, _ := streamIMDBFile("https://datasets.imdbws.com/title.akas.tsv.gz")
	defer closer.Close()
	defer reader.Close()

	scanner := bufio.NewScanner(reader)
	scanner.Scan()
	var akas []AKA

	for scanner.Scan() {
		parts := strings.Split(scanner.Text(), "\t")
		if len(parts) < 8 {
			continue
		}
		if _, ok := titles[parts[0]]; !ok {
			continue
		}

		var attrs []string
		if parts[6] != "\\N" {
			attrs = strings.Split(parts[6], ",")
		}

		akas = append(akas, AKA{
			TitleID: parts[0], LocalizedTitle: parts[2],
			Region: parts[3], Language: parts[4], ReleaseType: parts[5], Attributes: attrs,
		})
	}
	return akas
}

type Person struct {
	ID, Name              string
	Birth, Death          int
	Professions, KnownFor []string
}

func getPeopleMetadata(targetIDs map[string]bool) map[string]Person {
	closer, reader, _ := streamIMDBFile("https://datasets.imdbws.com/name.basics.tsv.gz")
	defer closer.Close()
	defer reader.Close()

	scanner := bufio.NewScanner(reader)
	scanner.Scan()
	people := make(map[string]Person)

	for scanner.Scan() {
		parts := strings.Split(scanner.Text(), "\t")
		if len(parts) < 6 || !targetIDs[parts[0]] {
			continue
		}
		b, _ := strconv.Atoi(parts[2])
		d, _ := strconv.Atoi(parts[3])

		var profs, kf []string
		if parts[4] != "\\N" {
			profs = strings.Split(parts[4], ",")
		}
		if parts[5] != "\\N" {
			kf = strings.Split(parts[5], ",")
		}

		people[parts[0]] = Person{parts[0], parts[1], b, d, profs, kf}
	}
	return people
}

func ingestObjects(ctx context.Context, client *graph.Client, titles map[string]Title, episodes map[string]Episode, seasons map[string]string, people map[string]Person, titleGenres map[string][]string, characterNames map[string]bool, titleAKAs map[string][]string) map[string]string {
	var items []graph.CreateObjectRequest

	for g := range getUniqueStrings(titleGenres) {
		k := "genre_" + g
		items = append(items, graph.CreateObjectRequest{Type: "Genre", Key: &k, Properties: map[string]any{"name": g}})
	}
	for prof := range getUniqueProfessions(people) {
		k := "prof_" + prof
		items = append(items, graph.CreateObjectRequest{Type: "Profession", Key: &k, Properties: map[string]any{"name": strings.ReplaceAll(prof, "_", " ")}})
	}
	for char := range characterNames {
		k := "char_" + char
		items = append(items, graph.CreateObjectRequest{Type: "Character", Key: &k, Properties: map[string]any{"name": char}})
	}

	for sKey, parentID := range seasons {
		k := sKey
		parts := strings.Split(sKey, "_s")
		sn, _ := strconv.Atoi(parts[1])
		items = append(items, graph.CreateObjectRequest{Type: "Season", Key: &k, Properties: map[string]any{"season_number": sn, "parent_series_id": parentID}})
	}

	for _, t := range titles {
		k := t.ID
		nodeType := "Movie"
		if t.Type == "tvSeries" {
			nodeType = "TVSeries"
		} else if t.Type == "tvMiniSeries" {
			nodeType = "TVMiniSeries"
		} else if t.Type == "videoGame" {
			nodeType = "VideoGame"
		} else if t.Type == "tvMovie" || t.Type == "tvSpecial" {
			nodeType = "TVMovie"
		} else if t.Type == "tvEpisode" {
			nodeType = "TVEpisode"
		} else if t.Type == "short" {
			nodeType = "ShortFilm"
		}

		props := map[string]any{"title": t.Name, "name": t.Name, "original_title": t.OriginalName, "release_year": t.StartYear, "runtime_mins": t.Runtime, "rating": t.Rating, "votes": t.Votes, "is_adult": t.IsAdult}
		if t.EndYear > 0 {
			props["end_year"] = t.EndYear
		}

		if t.Rating >= 9.0 {
			props["rating_tier"] = "Masterpiece"
		} else if t.Rating >= 8.0 {
			props["rating_tier"] = "Excellent"
		} else if t.Rating >= 7.0 {
			props["rating_tier"] = "Good"
		} else if t.Rating >= 5.0 {
			props["rating_tier"] = "Average"
		} else {
			props["rating_tier"] = "Poor"
		}

		if t.Runtime > 0 {
			if t.Runtime < 40 {
				props["duration_category"] = "Short"
			} else if t.Runtime > 140 {
				props["duration_category"] = "Epic"
			} else {
				props["duration_category"] = "FeatureLength"
			}
		}

		if t.StartYear > 1800 {
			props["release_decade"] = fmt.Sprintf("%ds", (t.StartYear/10)*10)
		}

		// Embed AKA localized titles as an array property (avoid self-loop relationships)
		if akaList, ok := titleAKAs[t.ID]; ok && len(akaList) > 0 {
			props["aka_titles"] = akaList
		}

		items = append(items, graph.CreateObjectRequest{Type: nodeType, Key: &k, Properties: props})
	}

	for _, p := range people {
		k := p.ID
		props := map[string]any{"name": p.Name}
		if p.Birth > 0 {
			props["birth_year"] = p.Birth
			props["birth_decade"] = fmt.Sprintf("%ds", (p.Birth/10)*10)
		}
		if p.Death > 0 {
			props["death_year"] = p.Death
		}
		items = append(items, graph.CreateObjectRequest{Type: "Person", Key: &k, Properties: props})
	}

	log.Printf("Uploading %d property-optimized objects...", len(items))
	return bulkUploadObjects(ctx, client, items)
}

func ingestRelationships(ctx context.Context, client *graph.Client, roles []Role, crewRoles []CrewRel, titleGenres map[string][]string, titles map[string]Title, episodes map[string]Episode, seasons map[string]string, people map[string]Person, idMap map[string]string) {
	log.Println("Mapping Keys to Canonical IDs...")
	var items []graph.CreateRelationshipRequest

	for _, r := range roles {
		src, ok1 := idMap[r.PersonID]
		dst, ok2 := idMap[r.TitleID]
		if !ok1 || !ok2 {
			continue
		}

		relType := strings.ToUpper(r.Category)
		if relType == "ACTOR" || relType == "ACTRESS" || relType == "SELF" {
			relType = "ACTED_IN"
		} else if relType == "COMPOSER" {
			relType = "COMPOSED_MUSIC_FOR"
		} else if relType == "PRODUCTION_DESIGNER" {
			relType = "DESIGNED_PRODUCTION_FOR"
		} else if relType == "ARCHIVE_FOOTAGE" || relType == "ARCHIVE_SOUND" {
			relType = "ARCHIVE_APPEARANCE_IN"
		} else if relType == "DIRECTOR" {
			relType = "DIRECTED"
		} else if relType == "WRITER" {
			relType = "WROTE"
		} else if relType == "PRODUCER" {
			relType = "PRODUCED"
		} else if relType == "EDITOR" {
			relType = "EDITED"
		} else if relType == "CINEMATOGRAPHER" {
			relType = "CINEMATOGRAPHER_ON"
		} else if relType == "CASTING_DIRECTOR" {
			relType = "CAST_FOR"
		} else {
			// Skip unknown/unmapped categories rather than sending invalid type
			continue
		}

		props := map[string]any{}
		if r.Job != "" {
			props["job"] = r.Job
		}
		if r.Ordering > 0 {
			props["billing_order"] = r.Ordering
		}

		items = append(items, graph.CreateRelationshipRequest{Type: relType, SrcID: src, DstID: dst, Properties: props})

		for _, char := range r.Characters {
			charDst, ok3 := idMap["char_"+char]
			if ok3 {
				items = append(items, graph.CreateRelationshipRequest{Type: "PLAYED", SrcID: src, DstID: charDst, Properties: map[string]any{}})
				items = append(items, graph.CreateRelationshipRequest{Type: "APPEARS_IN", SrcID: charDst, DstID: dst, Properties: map[string]any{}})
			}
		}
	}

	for _, cr := range crewRoles {
		if src, ok1 := idMap[cr.PersonID]; ok1 {
			if dst, ok2 := idMap[cr.TitleID]; ok2 {
				items = append(items, graph.CreateRelationshipRequest{Type: cr.Role, SrcID: src, DstID: dst, Properties: map[string]any{}})
			}
		}
	}

	for tID := range titles {
		src, ok1 := idMap[tID]
		if !ok1 {
			continue
		}
		for _, g := range titleGenres[tID] {
			if dst, ok2 := idMap["genre_"+g]; ok2 {
				items = append(items, graph.CreateRelationshipRequest{Type: "IN_GENRE", SrcID: src, DstID: dst, Properties: map[string]any{}})
			}
		}
	}

	for pID, p := range people {
		src, ok1 := idMap[pID]
		if !ok1 {
			continue
		}
		for _, prof := range p.Professions {
			if dst, ok2 := idMap["prof_"+prof]; ok2 {
				items = append(items, graph.CreateRelationshipRequest{Type: "HAS_PROFESSION", SrcID: src, DstID: dst, Properties: map[string]any{}})
			}
		}
		for _, kfID := range p.KnownFor {
			if dst, ok2 := idMap[kfID]; ok2 {
				items = append(items, graph.CreateRelationshipRequest{Type: "KNOWN_FOR", SrcID: src, DstID: dst, Properties: map[string]any{}})
			}
		}
	}

	for epID, ep := range episodes {
		epSrc, ok1 := idMap[epID]
		seriesDst, ok2 := idMap[ep.ParentID]
		if ok1 && ok2 {
			items = append(items, graph.CreateRelationshipRequest{Type: "EPISODE_OF", SrcID: epSrc, DstID: seriesDst, Properties: map[string]any{"season_number": ep.SeasonNumber, "episode_number": ep.EpNumber}})
			if ep.SeasonNumber > 0 {
				seasonKey := fmt.Sprintf("%s_s%d", ep.ParentID, ep.SeasonNumber)
				if seasonDst, ok3 := idMap[seasonKey]; ok3 {
					items = append(items, graph.CreateRelationshipRequest{Type: "IN_SEASON", SrcID: epSrc, DstID: seasonDst, Properties: map[string]any{}})
				}
			}
		}
	}
	for sKey, seriesID := range seasons {
		if sSrc, ok1 := idMap[sKey]; ok1 {
			if seriesDst, ok2 := idMap[seriesID]; ok2 {
				items = append(items, graph.CreateRelationshipRequest{Type: "SEASON_OF", SrcID: sSrc, DstID: seriesDst, Properties: map[string]any{}})
			}
		}
	}

	log.Printf("Uploading %d property-enriched relationships...", len(items))
	bulkUploadRelationships(ctx, client, items)
}

func getUniqueStrings(m map[string][]string) map[string]bool {
	u := make(map[string]bool)
	for _, list := range m {
		for _, item := range list {
			u[item] = true
		}
	}
	return u
}
func getUniqueProfessions(m map[string]Person) map[string]bool {
	u := make(map[string]bool)
	for _, p := range m {
		for _, prof := range p.Professions {
			u[prof] = true
		}
	}
	return u
}

const (
	batchSize  = 100 // Max per SDK docs
	numWorkers = 20  // Concurrent upload goroutines
)

func bulkUploadObjects(ctx context.Context, client *graph.Client, items []graph.CreateObjectRequest) map[string]string {
	type batchResult struct {
		batch []graph.CreateObjectRequest
		res   *graph.BulkCreateObjectsResponse
	}

	batches := make(chan []graph.CreateObjectRequest, numWorkers*2)
	results := make(chan batchResult, numWorkers*2)

	// Fan-out: workers
	var wg sync.WaitGroup
	for i := 0; i < numWorkers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for batch := range batches {
				if ctx.Err() != nil {
					return
				}
				res, err := client.BulkCreateObjects(ctx, &graph.BulkCreateObjectsRequest{Items: batch})
				if err != nil {
					log.Printf("  [objects] batch error: %v", err)
					time.Sleep(500 * time.Millisecond)
					// retry once
					res, _ = client.BulkCreateObjects(ctx, &graph.BulkCreateObjectsRequest{Items: batch})
				}
				results <- batchResult{batch, res}
			}
		}()
	}

	// Close results when all workers done
	go func() { wg.Wait(); close(results) }()

	// Feed batches
	go func() {
		for i := 0; i < len(items); i += batchSize {
			if ctx.Err() != nil {
				break
			}
			end := i + batchSize
			if end > len(items) {
				end = len(items)
			}
			batches <- items[i:end]
		}
		close(batches)
	}()

	// Collect results — handle both new objects and conflicts (already-exist = fetch by key)
	idMap := make(map[string]string)
	var mu sync.Mutex
	var uploaded, conflicts, failed atomic.Int64

	// For conflict lookups, we need a separate sequential fetch (by key) per missing item
	type missingKey struct{ objType, key string }
	var missingKeys []missingKey
	var missingMu sync.Mutex

	for br := range results {
		if br.res != nil {
			mu.Lock()
			for idx, result := range br.res.Results {
				key := br.batch[idx].Key
				if key == nil {
					continue
				}
				if result.Object != nil {
					id := result.Object.EntityID
					if id == "" {
						id = result.Object.CanonicalID
					}
					if id == "" {
						id = result.Object.ID
					}
					idMap[*key] = id
					uploaded.Add(1)
				} else if result.Error != nil && strings.Contains(*result.Error, "conflict") {
					// Already exists — need to fetch canonical ID by key
					missingMu.Lock()
					missingKeys = append(missingKeys, missingKey{br.batch[idx].Type, *key})
					missingMu.Unlock()
					conflicts.Add(1)
				} else if result.Error != nil {
					log.Printf("  [objects] error key=%s: %s", *key, *result.Error)
					failed.Add(1)
				}
			}
			mu.Unlock()
		}
		n := uploaded.Load() + conflicts.Load()
		if n%20000 == 0 && n > 0 {
			log.Printf("  ...%d/%d objects processed (new=%d conflicts=%d idMap=%d)", n, len(items), uploaded.Load(), conflicts.Load(), len(idMap))
		}
	}

	// Resolve conflicts: fetch existing objects by key to get their canonical IDs
	if len(missingKeys) > 0 {
		log.Printf("  Resolving %d conflicting keys by lookup...", len(missingKeys))
		sem := make(chan struct{}, numWorkers)
		var resolveWg sync.WaitGroup
		for _, mk := range missingKeys {
			sem <- struct{}{}
			resolveWg.Add(1)
			go func(objType, key string) {
				defer resolveWg.Done()
				defer func() { <-sem }()
				resp, err := client.ListObjects(ctx, &graph.ListObjectsOptions{Type: objType, Key: key, Limit: 1})
				if err == nil && resp != nil && len(resp.Items) > 0 {
					obj := resp.Items[0]
					id := obj.EntityID
					if id == "" {
						id = obj.CanonicalID
					}
					if id == "" {
						id = obj.ID
					}
					mu.Lock()
					idMap[key] = id
					mu.Unlock()
				}
			}(mk.objType, mk.key)
		}
		resolveWg.Wait()
		log.Printf("  Conflict resolution complete — idMap now has %d entries", len(idMap))
	}

	log.Printf("  Objects complete: %d new, %d conflicts resolved, %d errors, %d total mapped", uploaded.Load(), conflicts.Load(), failed.Load(), len(idMap))
	return idMap
}

// bulkUploadRelationships uploads relationship batches with full checkpoint/resume support.
// Each batch is numbered; completed batch indices are written to rels_done.txt.
// Failed batches are appended to rels_failed.jsonl for later retry.
func bulkUploadRelationships(ctx context.Context, client *graph.Client, items []graph.CreateRelationshipRequest) {
	// Load already-completed batch indices
	relsDone := loadRelsDone()

	// Split into numbered batches
	var batches [][]graph.CreateRelationshipRequest
	for i := 0; i < len(items); i += batchSize {
		end := i + batchSize
		if end > len(items) {
			end = len(items)
		}
		batches = append(batches, items[i:end])
	}

	totalBatches := len(batches)
	skipped := len(relsDone)
	log.Printf("Relationship upload: %d total batches, %d already done, %d remaining", totalBatches, skipped, totalBatches-skipped)

	type workItem struct {
		idx   int
		batch []graph.CreateRelationshipRequest
	}

	work := make(chan workItem, numWorkers*2)
	var wg sync.WaitGroup
	var succeeded, failed, skippedCount atomic.Int64

	// Workers
	for i := 0; i < numWorkers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for wi := range work {
				if ctx.Err() != nil {
					return
				}

				res, err := client.BulkCreateRelationships(ctx, &graph.BulkCreateRelationshipsRequest{Items: wi.batch})
				if err != nil {
					log.Printf("  [rels] batch %d error: %v — retrying once", wi.idx, err)
					time.Sleep(500 * time.Millisecond)
					res, err = client.BulkCreateRelationships(ctx, &graph.BulkCreateRelationshipsRequest{Items: wi.batch})
				}

				if err != nil || res == nil {
					log.Printf("  [rels] batch %d failed permanently — saving to rels_failed.jsonl", wi.idx)
					appendRelFailed(wi.batch)
					failed.Add(int64(len(wi.batch)))
					continue
				}

				// Check per-item results for any server-side failures
				batchFailed := false
				for _, r := range res.Results {
					if !r.Success && r.Error != nil {
						log.Printf("  [rels] batch %d item error: %s", wi.idx, *r.Error)
						batchFailed = true
					}
				}

				if batchFailed {
					// Partial failure — save whole batch for inspection
					appendRelFailed(wi.batch)
					failed.Add(int64(res.Failed))
				} else {
					// Full success — mark batch as done
					appendRelDone(wi.idx)
					succeeded.Add(int64(res.Success))
				}

				n := succeeded.Load() + failed.Load() + skippedCount.Load()
				if n%50000 == 0 && n > 0 {
					log.Printf("  ...%d/%d relationships processed (ok=%d fail=%d skip=%d)",
						n*batchSize, len(items), succeeded.Load()*batchSize, failed.Load()*batchSize, skippedCount.Load()*batchSize)
				}
			}
		}()
	}

	// Feed work, skipping completed batches
	go func() {
		for idx, batch := range batches {
			if ctx.Err() != nil {
				break
			}
			if relsDone[idx] {
				skippedCount.Add(1)
				continue
			}
			work <- workItem{idx, batch}
		}
		close(work)
	}()

	wg.Wait()
	log.Printf("  Relationships complete: %d succeeded, %d failed, %d skipped (of %d total batches)",
		succeeded.Load(), failed.Load(), skippedCount.Load(), totalBatches)
}

// retryRelationshipBatches replays a list of pre-loaded batches (from rels_failed.jsonl).
func retryRelationshipBatches(ctx context.Context, client *graph.Client, batches [][]graph.CreateRelationshipRequest) {
	type workItem struct {
		idx   int
		batch []graph.CreateRelationshipRequest
	}

	work := make(chan workItem, numWorkers*2)
	var wg sync.WaitGroup
	var succeeded, failed atomic.Int64

	for i := 0; i < numWorkers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for wi := range work {
				if ctx.Err() != nil {
					return
				}
				res, err := client.BulkCreateRelationships(ctx, &graph.BulkCreateRelationshipsRequest{Items: wi.batch})
				if err != nil {
					time.Sleep(500 * time.Millisecond)
					res, err = client.BulkCreateRelationships(ctx, &graph.BulkCreateRelationshipsRequest{Items: wi.batch})
				}
				if err != nil || res == nil {
					log.Printf("  [retry] batch %d still failing — re-saving to rels_failed.jsonl", wi.idx)
					appendRelFailed(wi.batch)
					failed.Add(int64(len(wi.batch)))
					continue
				}
				batchFailed := false
				for _, r := range res.Results {
					if !r.Success && r.Error != nil {
						log.Printf("  [retry] batch %d item error: %s", wi.idx, *r.Error)
						batchFailed = true
					}
				}
				if batchFailed {
					appendRelFailed(wi.batch)
					failed.Add(int64(res.Failed))
				} else {
					succeeded.Add(int64(res.Success))
				}
			}
		}()
	}

	go func() {
		for idx, batch := range batches {
			if ctx.Err() != nil {
				break
			}
			work <- workItem{idx, batch}
		}
		close(work)
	}()

	wg.Wait()
	log.Printf("  Retry complete: %d succeeded, %d still failed", succeeded.Load(), failed.Load())
}
