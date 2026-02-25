package cmd

// lovdata.go — "emergent db lovdata" command
//
// Imports the Norwegian law knowledge graph from three public data sources:
//
//   SOURCE 1 — Lovdata public datasets (NLOD 2.0 license)
//     gjeldende-lover.tar.bz2              775 laws
//     gjeldende-sentrale-forskrifter.tar.bz2  ~thousands of regulations
//
//   SOURCE 2 — EUR-Lex (scraped via HTML, public)
//     Full metadata for every EU directive referenced by Norwegian law
//
//   SOURCE 3 — EuroVoc SPARQL (EU Publications Office, public)
//     Human-readable labels for EuroVoc thesaurus concept IDs
//
// Phases:
//   1. (optional) Delete previous lovdata project  --project-id
//   2. Find or create "Norwegian Law" project
//   3. Download / cache Lovdata tar.bz2 archives
//   4. Parse and ingest Lovdata documents            --seed, --workers, --dataset
//   5. Fetch EU directive metadata from EUR-Lex      --skip-eu, --eu-limit
//   6. Fetch EuroVoc concept labels via SPARQL
//   7. Ingest all objects + relationships
//   8. Print timing report
//   9. (optional) Delete the project                --cleanup
//  10. Append JSONL result to log                   --log
//
// Connection for the API (first match wins):
//   1. --server flag
//   2. EMERGENT_SERVER_URL env var
//   3. ~/.emergent/config.yaml server_url field
//
// Examples:
//   emergent db lovdata
//   emergent db lovdata --seed 20 --skip-eu --dataset laws
//   emergent db lovdata --server http://mcj-emergent:3002 --workers 40
//   emergent db lovdata --server http://mcj-emergent:3002 --seed 0 --workers 40

import (
	"archive/tar"
	"bytes"
	"compress/bzip2"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	sdk "github.com/emergent-company/emergent/apps/server-go/pkg/sdk"
	sdkgraph "github.com/emergent-company/emergent/apps/server-go/pkg/sdk/graph"
	"github.com/emergent-company/emergent/apps/server-go/pkg/sdk/projects"
	"github.com/emergent-company/emergent/tools/emergent-cli/internal/config"
	"github.com/spf13/cobra"
	"golang.org/x/net/html"
)

// ─── constants ────────────────────────────────────────────────────────────────

const (
	lovdataCacheDir    = "/tmp/lovdata_data"
	lovdataLawsURL     = "https://api.lovdata.no/v1/publicData/get/gjeldende-lover.tar.bz2"
	lovdataRegsURL     = "https://api.lovdata.no/v1/publicData/get/gjeldende-sentrale-forskrifter.tar.bz2"
	lovdataEurLexBase  = "https://eur-lex.europa.eu/legal-content/EN/ALL/?uri=CELEX:"
	lovdataEurovocSPQL = "https://publications.europa.eu/webapi/rdf/sparql"
	lovdataCellarSPQL  = "https://publications.europa.eu/webapi/rdf/sparql"
	lovdataCellarBase  = "https://publications.europa.eu/resource/cellar/"
	lovdataProjectName = "Norwegian Law"
	lovdataOrgIDDef    = "dcba78f0-fc40-414a-a24d-f9c32b762f15"
)

// ─── flags ────────────────────────────────────────────────────────────────────

var dbLovdataFlags struct {
	seed         int
	workers      int
	batch        int
	dataset      string // "laws" | "regulations" | "both"
	skipEU       bool
	euLimit      int
	cleanup      bool
	logFile      string
	server       string
	projectID    string
	skipDelete   bool
	verbose      bool
	configPath   string
	orgID        string
	downloadOnly bool
	ingestOnly   bool
}

// ─── command definition ───────────────────────────────────────────────────────

var dbLovdataCmd = &cobra.Command{
	Use:   "lovdata",
	Short: "Import the Norwegian law knowledge graph from Lovdata, EUR-Lex and EuroVoc",
	Long: `Import a comprehensive Norwegian law knowledge graph by combining three
public, no-auth data sources into a dedicated Emergent project.

Node types:   Law, Regulation, Ministry, LegalArea, EUDirective, EuroVocConcept
Relationship types:
  ADMINISTERED_BY, IN_LEGAL_AREA, AMENDED_BY, AMENDS, SEE_ALSO,
  HAS_LANGUAGE_VARIANT, REFERENCES, IMPLEMENTS_EEA,
  EU_CITES, EU_MODIFIED_BY, HAS_EUROVOC_DESCRIPTOR

Examples:
  emergent db lovdata
  emergent db lovdata --seed 20 --skip-eu --dataset laws
  emergent db lovdata --server http://mcj-emergent:3002 --workers 40
  emergent db lovdata --server http://mcj-emergent:3002 --seed 0 --workers 40`,
	RunE: runDbLovdata,
}

func init() {
	f := dbLovdataCmd.Flags()
	f.IntVar(&dbLovdataFlags.seed, "seed", 0, "max documents per dataset (0 = all)")
	f.IntVar(&dbLovdataFlags.workers, "workers", 20, "parallel upload workers")
	f.IntVar(&dbLovdataFlags.batch, "batch", 100, "batch size for bulk API calls (max 100)")
	f.StringVar(&dbLovdataFlags.dataset, "dataset", "both", `dataset to import: "laws", "regulations", or "both"`)
	f.BoolVar(&dbLovdataFlags.skipEU, "skip-eu", false, "skip EUR-Lex + EuroVoc enrichment")
	f.IntVar(&dbLovdataFlags.euLimit, "eu-limit", 0, "max EU directives to fetch from EUR-Lex (0 = all)")
	f.BoolVar(&dbLovdataFlags.cleanup, "cleanup", false, "delete the project after the run")
	f.StringVar(&dbLovdataFlags.logFile, "log", "", "JSONL log file to append results to")
	f.StringVar(&dbLovdataFlags.server, "server", "", "Emergent server URL (overrides config)")
	f.StringVar(&dbLovdataFlags.projectID, "project-id", "", "delete this project ID before creating a new one")
	f.BoolVar(&dbLovdataFlags.skipDelete, "skip-delete", false, "skip deleting --project-id even if set")
	f.BoolVarP(&dbLovdataFlags.verbose, "verbose", "v", false, "verbose output")
	f.StringVar(&dbLovdataFlags.configPath, "config-path", "", "path to Emergent config.yaml")
	f.StringVar(&dbLovdataFlags.orgID, "org-id", "", "organisation ID for project creation (default: dev org)")
	f.BoolVar(&dbLovdataFlags.downloadOnly, "download-only", false, "download and parse all data to local cache, skip ingestion")
	f.BoolVar(&dbLovdataFlags.ingestOnly, "ingest-only", false, "skip downloading, use cached data for ingestion")
}

// ─── entry point ──────────────────────────────────────────────────────────────

func runDbLovdata(_ *cobra.Command, _ []string) error {
	ctx := context.Background()

	// ── Resolve server URL ─────────────────────────────────────────────────────
	svrURL := dbLovdataFlags.server
	if svrURL == "" {
		if v := os.Getenv("EMERGENT_SERVER_URL"); v != "" {
			svrURL = v
		}
	}
	if svrURL == "" {
		cfgPath := config.DiscoverPath(dbLovdataFlags.configPath)
		if cfg, err := config.LoadWithEnv(cfgPath); err == nil && cfg.ServerURL != "" {
			svrURL = cfg.ServerURL
		}
	}
	if svrURL == "" {
		svrURL = "http://localhost:3002"
	}

	// ── Resolve API key + org ID ───────────────────────────────────────────────
	apiKey := os.Getenv("EMERGENT_API_KEY")
	orgID := dbLovdataFlags.orgID
	if apiKey == "" || orgID == "" {
		cfgPath := config.DiscoverPath(dbLovdataFlags.configPath)
		if cfg, err := config.LoadWithEnv(cfgPath); err == nil {
			if apiKey == "" {
				apiKey = cfg.APIKey
			}
			if orgID == "" {
				orgID = cfg.OrgID
			}
		}
	}
	if orgID == "" {
		orgID = lovdataOrgIDDef
	}

	// ── Resolve log file ───────────────────────────────────────────────────────
	logFile := dbLovdataFlags.logFile
	if logFile == "" {
		home, _ := os.UserHomeDir()
		logFile = filepath.Join(home, ".emergent", "lovdata_log.jsonl")
	}

	// ── Cap batch size ─────────────────────────────────────────────────────────
	batchSz := dbLovdataFlags.batch
	if batchSz > 100 {
		fmt.Printf("  Note: max batch size is 100; capping from %d\n", batchSz)
		batchSz = 100
	}

	report := &benchReport{
		BenchVersion:  benchVersion,
		ServerVersion: benchFetchServerVersion(svrURL),
		ServerURL:     svrURL,
		SeedLimit:     dbLovdataFlags.seed,
		GitCommit:     benchGitCommit(),
		GoVersion:     "go",
		GOOS:          "",
		GOARCH:        "",
		Hostname:      benchHostname(),
		StartedAt:     time.Now(),
	}

	fmt.Printf("\n%s%sEmergent db lovdata%s\n", diagBold, diagCyan, diagReset)
	fmt.Printf("server=%s (%s)  dataset=%s  seed=%d  workers=%d  skip-eu=%v\n\n",
		svrURL, report.ServerVersion, dbLovdataFlags.dataset, dbLovdataFlags.seed,
		dbLovdataFlags.workers, dbLovdataFlags.skipEU)

	// ── SDK base client ────────────────────────────────────────────────────────
	baseClient, err := benchNewSDK(svrURL, "", apiKey)
	if err != nil {
		return fmt.Errorf("SDK init: %w", err)
	}

	// ── Phase 0: Delete previous project (optional) ────────────────────────────
	if dbLovdataFlags.projectID != "" && !dbLovdataFlags.skipDelete {
		fmt.Printf("Phase 0: Deleting project %s ...\n", dbLovdataFlags.projectID)
		done0 := report.begin("delete_prev_project")
		if err := benchDeleteProject(ctx, baseClient, dbLovdataFlags.projectID); err != nil {
			return fmt.Errorf("delete failed: %w", err)
		}
		done0(fmt.Sprintf("project=%s", dbLovdataFlags.projectID))
	}

	// ── Phase 1: Find or create "Norwegian Law" project ────────────────────────
	fmt.Printf("Phase 1: Resolving project %q ...\n", lovdataProjectName)
	done1 := report.begin("resolve_project")
	projectID, err := lovdataEnsureProject(ctx, baseClient, orgID)
	if err != nil {
		return fmt.Errorf("resolve project: %w", err)
	}
	report.ProjectID = projectID
	done1(fmt.Sprintf("id=%s", projectID))

	projClient, err := benchNewSDK(svrURL, projectID, apiKey)
	if err != nil {
		return fmt.Errorf("SDK re-init: %w", err)
	}

	// ── Phase 2: Download + parse Lovdata archives ─────────────────────────────
	var allDocs []lovDoc
	var directives []*lovEUDirective
	var concepts []*lovEuroVocConcept

	cachePathDocs := filepath.Join(lovdataCacheDir, "parsed_docs.json")
	cachePathDirectives := filepath.Join(lovdataCacheDir, "parsed_directives.json")
	cachePathConcepts := filepath.Join(lovdataCacheDir, "parsed_concepts.json")

	if dbLovdataFlags.ingestOnly {
		fmt.Printf("Phase 2 & 3: Skipping download, loading from cache in %s ...\n", lovdataCacheDir)
		done2 := report.begin("load_cache")

		bDocs, err := os.ReadFile(cachePathDocs)
		if err != nil {
			return fmt.Errorf("read cached docs: %w", err)
		}
		json.Unmarshal(bDocs, &allDocs)

		bDir, err := os.ReadFile(cachePathDirectives)
		if err != nil {
			return fmt.Errorf("read cached directives: %w", err)
		}
		json.Unmarshal(bDir, &directives)

		bConc, err := os.ReadFile(cachePathConcepts)
		if err != nil {
			return fmt.Errorf("read cached concepts: %w", err)
		}
		json.Unmarshal(bConc, &concepts)

		done2(fmt.Sprintf("docs=%d directives=%d concepts=%d", len(allDocs), len(directives), len(concepts)))
	} else {
		fmt.Printf("Phase 2: Loading Lovdata documents (limit=%d, dataset=%s) ...\n",
			dbLovdataFlags.seed, dbLovdataFlags.dataset)
		done2 := report.begin("load_lovdata")

		if dbLovdataFlags.dataset == "laws" || dbLovdataFlags.dataset == "both" {
			fmt.Println("  Downloading laws ...")
			docs, err := lovLoadDataset(lovdataLawsURL, "Law", dbLovdataFlags.seed)
			if err != nil {
				return fmt.Errorf("load laws: %w", err)
			}
			fmt.Printf("  Parsed %d laws\n", len(docs))
			allDocs = append(allDocs, docs...)
		}
		if dbLovdataFlags.dataset == "regulations" || dbLovdataFlags.dataset == "both" {
			fmt.Println("  Downloading regulations ...")
			docs, err := lovLoadDataset(lovdataRegsURL, "Regulation", dbLovdataFlags.seed)
			if err != nil {
				return fmt.Errorf("load regulations: %w", err)
			}
			fmt.Printf("  Parsed %d regulations\n", len(docs))
			allDocs = append(allDocs, docs...)
		}
		done2(fmt.Sprintf("docs=%d", len(allDocs)))

		// ── Phase 3: Fetch EU data ─────────────────────────────────────────────────
		if !dbLovdataFlags.skipEU {
			// Check if cached directives file exists and has content — skip re-fetch if so
			if bDir, err := os.ReadFile(cachePathDirectives); err == nil && len(bDir) > 10 {
				var cached []*lovEUDirective
				if jsonErr := json.Unmarshal(bDir, &cached); jsonErr == nil && len(cached) > 0 {
					directives = cached
					fmt.Printf("Phase 3: Using cached EU directives from %s (count=%d, skipping re-fetch)\n", cachePathDirectives, len(directives))
					if bConc, err2 := os.ReadFile(cachePathConcepts); err2 == nil && len(bConc) > 10 {
						json.Unmarshal(bConc, &concepts)
					}
				}
			}
			if len(directives) == 0 {
				fmt.Println("Phase 3: Fetching EU directive metadata from EUR-Lex ...")
				done3 := report.begin("fetch_eu_data")
				directives, concepts = lovFetchAllEUData(ctx, allDocs, dbLovdataFlags.euLimit)
				done3(fmt.Sprintf("directives=%d eurovoc=%d", len(directives), len(concepts)))
			}
		} else {
			fmt.Println("Phase 3: EU enrichment skipped (--skip-eu)")
		}

		// Save to cache
		fmt.Println("Saving parsed data to cache...")
		os.MkdirAll(lovdataCacheDir, 0755)
		if bDocs, err := json.MarshalIndent(allDocs, "", "  "); err == nil {
			os.WriteFile(cachePathDocs, bDocs, 0644)
		}
		if bDir, err := json.MarshalIndent(directives, "", "  "); err == nil {
			os.WriteFile(cachePathDirectives, bDir, 0644)
		}
		if bConc, err := json.MarshalIndent(concepts, "", "  "); err == nil {
			os.WriteFile(cachePathConcepts, bConc, 0644)
		}

		if dbLovdataFlags.downloadOnly {
			fmt.Println("Data downloaded and cached successfully. Exiting (--download-only).")
			return nil
		}
	}

	// ── Phase 4: Ingest objects ────────────────────────────────────────────────
	fmt.Printf("Phase 4: Ingesting objects ...\n")
	done4 := report.begin("ingest_objects")
	idMap := lovIngestObjects(ctx, projClient.Graph, allDocs, directives, concepts, batchSz, dbLovdataFlags.workers)
	objCount, _ := projClient.Graph.CountObjects(ctx, &sdkgraph.CountObjectsOptions{})
	report.Objects = objCount
	done4(fmt.Sprintf("mapped=%d live=%d", len(idMap), objCount))

	// ── Phase 5: Ingest relationships ─────────────────────────────────────────
	fmt.Println("Phase 5: Ingesting relationships ...")
	done5 := report.begin("ingest_relationships")
	relSucceeded, relFailed := lovIngestRelationships(ctx, projClient.Graph, allDocs, directives, idMap, batchSz, dbLovdataFlags.workers)
	report.RelErrors = relFailed
	if relResp, err := projClient.Graph.ListRelationships(ctx, &sdkgraph.ListRelationshipsOptions{Limit: 1}); err == nil {
		report.Relations = relResp.Total
	}
	done5(fmt.Sprintf("succeeded=%d failed=%d total=%d", relSucceeded, relFailed, report.Relations))

	// ── Print report ───────────────────────────────────────────────────────────
	report.print()

	// ── Phase 6: Cleanup (optional) ───────────────────────────────────────────
	if dbLovdataFlags.cleanup {
		fmt.Printf("\nPhase 6: Cleaning up project %s ...\n", projectID)
		doneClean := report.begin("cleanup_project")
		if err := benchDeleteProject(ctx, baseClient, projectID); err != nil {
			fmt.Printf("%sWARN: cleanup failed: %v%s\n", diagYellow, err, diagReset)
		}
		doneClean()
	}

	benchAppendLog(logFile, report)

	fmt.Printf("\n%sObjects: %d  Relationships: %d  Errors: %d%s\n",
		diagGreen, objCount, report.Relations, relFailed, diagReset)
	fmt.Printf("Project: %s\n", projectID)
	return nil
}

// ─── project helper ───────────────────────────────────────────────────────────

func lovdataEnsureProject(ctx context.Context, client *sdk.Client, orgID string) (string, error) {
	list, err := client.Projects.List(ctx, &projects.ListOptions{OrgID: orgID})
	if err != nil {
		return "", fmt.Errorf("list projects: %w", err)
	}
	for _, p := range list {
		if p.Name == lovdataProjectName {
			fmt.Printf("  Found existing project %q (ID: %s)\n", lovdataProjectName, p.ID)
			return p.ID, nil
		}
	}
	proj, err := client.Projects.Create(ctx, &projects.CreateProjectRequest{
		Name:  lovdataProjectName,
		OrgID: orgID,
	})
	if err != nil {
		return "", fmt.Errorf("create project %q: %w", lovdataProjectName, err)
	}
	fmt.Printf("  Created project %q (ID: %s)\n", proj.Name, proj.ID)
	return proj.ID, nil
}

// ─── data types ───────────────────────────────────────────────────────────────

type lovDoc struct {
	RefID             string
	DocID             string
	LegacyID          string
	Title             string
	ShortTitle        string
	Language          string
	Ministry          string
	LegalArea         string
	LegalSubArea      string
	AllLegalAreas     []string
	AllLegalSubAreas  []string
	DateInForce       string
	LastChangeInForce string
	DateOfPublication string
	AppliesTo         string
	LastChangedByRef  string
	AmendsRefs        []string
	SeeAlsoRefs       []string
	EEAReferences     string
	EUDirectiveIDs    []string
	DocType           string // "Law" | "Regulation"
	References        []string
	// Body content (extracted from <main id="dokument">)
	Content    string         // Full Markdown of law body
	Paragraphs []lovParagraph // Each § as a structured semantic chunk
	EUBodyRefs []string       // eu/XXXXXXXX hrefs from body → CITES_EU_LAW edges
}

// lovParagraph represents a single § (article/paragraph) extracted from the law body.
type lovParagraph struct {
	SectionID    string // e.g. "kapittel-1-paragraf-3"
	ChapterID    string // e.g. "kapittel-1" (empty for unchaptered laws)
	ParagraphNum string // e.g. "§ 1-3"
	Title        string // e.g. "Lovens formål" (may be empty)
	Content      string // Full Markdown text of this §
	Position     int    // ordinal position within the document
}

type lovEUDirective struct {
	DirectiveID     string
	CelexID         string
	FullTitle       string
	ShortTitle      string
	Form            string
	DateOfDocument  string
	DateOfEffect    string
	Author          string
	ResponsibleDG   string
	Content         string // Full Markdown of directive body (from CELLAR XHTML)
	SubjectMatter   string
	DirectoryCode   string
	LegalBasis      string
	ProcedureNum    string
	OJReference     string
	EuroVocIDs      []string
	CitedCELEX      []string
	ModifiedByCELEX []string
}

type lovEuroVocConcept struct {
	ID      string
	LabelEN string
}

// ─── regex ────────────────────────────────────────────────────────────────────

var (
	lovRefPattern       = regexp.MustCompile(`^(?:lov|forskrift|res)/\d{4}-\d{2}-\d{2}`)
	lovDirectivePattern = regexp.MustCompile(`\b(\d{4}/[\d]+/(?:EF|EØF|EU|EEC|EC|EØF))\b`)
	lovCelexPattern     = regexp.MustCompile(`\b(3\d{7}[A-Z]\d+)\b`)
	lovEurovocPattern   = regexp.MustCompile(`eurovoc\.europa\.eu/(\d+)`)
)

// ─── HTML parsing helpers ─────────────────────────────────────────────────────

func lovAttr(n *html.Node, key string) string {
	for _, a := range n.Attr {
		if a.Key == key {
			return a.Val
		}
	}
	return ""
}

func lovText(n *html.Node) string {
	var b strings.Builder
	var walk func(*html.Node)
	walk = func(n *html.Node) {
		if n.Type == html.TextNode {
			b.WriteString(n.Data)
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			walk(c)
		}
	}
	walk(n)
	return b.String()
}

func lovFindAll(n *html.Node, tag string) []*html.Node {
	var results []*html.Node
	var walk func(*html.Node)
	walk = func(n *html.Node) {
		if n.Type == html.ElementNode && n.Data == tag {
			results = append(results, n)
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			walk(c)
		}
	}
	walk(n)
	return results
}

func lovFindFirst(n *html.Node, tag string) *html.Node {
	var result *html.Node
	var walk func(*html.Node)
	walk = func(n *html.Node) {
		if result != nil {
			return
		}
		if n.Type == html.ElementNode && n.Data == tag {
			result = n
			return
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			walk(c)
		}
	}
	walk(n)
	return result
}

func lovAppendUniq(s []string, v string) []string {
	for _, x := range s {
		if x == v {
			return s
		}
	}
	return append(s, v)
}

func lovStripAnchor(s string) string {
	if idx := strings.Index(s, "#"); idx >= 0 {
		return s[:idx]
	}
	return s
}

func lovStrPtr(s string) *string { return &s }

// ─── Body extraction: HTML → Markdown + paragraph chunks ─────────────────────

// lovExtractBody walks <main id="dokument"> and returns:
//   - fullMarkdown: the complete body as Markdown
//   - paragraphs:   one lovParagraph per <article class="legalArticle">
//   - euBodyRefs:   deduplicated eu/XXXXXXXX href codes (without the "eu/" prefix)
func lovExtractBody(main *html.Node) (fullMarkdown string, paragraphs []lovParagraph, euBodyRefs []string) {
	var sb strings.Builder
	euRefSet := make(map[string]bool)
	position := 0
	currentChapterID := ""

	// skipClasses: subtrees we never descend into
	skipClasses := map[string]bool{
		"changesToParent": true,
		"footnotes":       true,
		"tocSubUl":        true,
	}

	// extractText returns all visible text under a node, skipping skip-class subtrees.
	var extractPlainText func(*html.Node) string
	extractPlainText = func(n *html.Node) string {
		if n.Type == html.TextNode {
			return n.Data
		}
		if n.Type == html.ElementNode && skipClasses[lovAttr(n, "class")] {
			return ""
		}
		var parts []string
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			parts = append(parts, extractPlainText(c))
		}
		return strings.Join(parts, "")
	}

	// collectEURefs scans all <a href="eu/..."> links under a node.
	var collectEURefs func(*html.Node)
	collectEURefs = func(n *html.Node) {
		if n.Type == html.ElementNode && n.Data == "a" {
			href := strings.TrimPrefix(lovAttr(n, "href"), "/")
			href = lovStripAnchor(href)
			if strings.HasPrefix(href, "eu/") && len(href) > 3 {
				code := strings.ToUpper(href[3:]) // "eu/32014l0026" → "32014L0026"
				euRefSet[code] = true
			}
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			collectEURefs(c)
		}
	}

	// renderOL renders an <ol> list as numbered Markdown items.
	var renderOL func(*html.Node) string
	renderOL = func(ol *html.Node) string {
		var olsb strings.Builder
		i := 1
		for c := ol.FirstChild; c != nil; c = c.NextSibling {
			if c.Type == html.ElementNode && c.Data == "li" {
				text := strings.TrimSpace(extractPlainText(c))
				if text != "" {
					olsb.WriteString(fmt.Sprintf("%d. %s\n", i, text))
					i++
				}
			}
		}
		return olsb.String()
	}

	// renderUL renders a <ul> list as bullet Markdown items.
	var renderUL func(*html.Node) string
	renderUL = func(ul *html.Node) string {
		var ulsb strings.Builder
		for c := ul.FirstChild; c != nil; c = c.NextSibling {
			if c.Type == html.ElementNode && c.Data == "li" {
				text := strings.TrimSpace(extractPlainText(c))
				if text != "" {
					ulsb.WriteString("- " + text + "\n")
				}
			}
		}
		return ulsb.String()
	}

	// renderArticleContent renders the body of a <article class="legalArticle"> node
	// (skipping the h3 header and changesToParent sub-articles).
	var renderArticleContent func(*html.Node) string
	renderArticleContent = func(n *html.Node) string {
		var asb strings.Builder
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			if c.Type != html.ElementNode {
				continue
			}
			cls := lovAttr(c, "class")
			if skipClasses[cls] {
				continue
			}
			switch cls {
			case "legalArticleHeader":
				// handled separately when building h3
				continue
			case "numberedLegalP", "legalP", "legalPfortsettelse", "leddfortsettelse":
				text := strings.TrimSpace(extractPlainText(c))
				if text != "" {
					asb.WriteString(text + "\n\n")
				}
			case "listArticle":
				text := strings.TrimSpace(extractPlainText(c))
				if text != "" {
					asb.WriteString("- " + text + "\n")
				}
			default:
				// handle nested ol/ul
				if c.Data == "ol" {
					asb.WriteString(renderOL(c))
				} else if c.Data == "ul" && cls != "tocSubUl" {
					asb.WriteString(renderUL(c))
				} else {
					// generic: recurse and collect text
					text := strings.TrimSpace(extractPlainText(c))
					if text != "" {
						asb.WriteString(text + "\n\n")
					}
				}
			}
		}
		return asb.String()
	}

	// walk the <main id="dokument"> children
	var walk func(*html.Node)
	walk = func(n *html.Node) {
		if n.Type != html.ElementNode {
			for c := n.FirstChild; c != nil; c = c.NextSibling {
				walk(c)
			}
			return
		}

		cls := lovAttr(n, "class")
		aid := lovAttr(n, "id")

		// Skip noise subtrees entirely
		if skipClasses[cls] {
			return
		}

		switch {
		case n.Data == "section":
			// Chapter heading — extract number from id like "kapittel-3"
			currentChapterID = aid
			chNum := ""
			if parts := strings.Split(aid, "-"); len(parts) >= 2 {
				chNum = parts[len(parts)-1]
			}
			// Get chapter title from first <h2> child if present
			chTitle := ""
			for c := n.FirstChild; c != nil; c = c.NextSibling {
				if c.Type == html.ElementNode && (c.Data == "h2" || c.Data == "h1") {
					chTitle = strings.TrimSpace(extractPlainText(c))
					break
				}
			}
			if chNum != "" || chTitle != "" {
				heading := "## Kapittel " + chNum
				if chTitle != "" {
					heading += ". " + chTitle
				}
				sb.WriteString("\n" + heading + "\n\n")
			}
			for c := n.FirstChild; c != nil; c = c.NextSibling {
				walk(c)
			}

		case n.Data == "article" && cls == "legalArticle":
			// Paragraph (§) node
			position++
			sectionID := aid

			// Extract header spans: legalArticleValue (§ number) and legalArticleTitle
			paraNum := ""
			paraTitle := ""
			for c := n.FirstChild; c != nil; c = c.NextSibling {
				if c.Type == html.ElementNode && c.Data == "h3" && lovAttr(c, "class") == "legalArticleHeader" {
					for sc := c.FirstChild; sc != nil; sc = sc.NextSibling {
						if sc.Type != html.ElementNode {
							continue
						}
						switch lovAttr(sc, "class") {
						case "legalArticleValue":
							paraNum = strings.TrimSpace(extractPlainText(sc))
						case "legalArticleTitle":
							paraTitle = strings.TrimSpace(extractPlainText(sc))
						}
					}
					break
				}
			}

			// Build h3 heading
			h3 := "### " + paraNum
			if paraTitle != "" {
				h3 += " — " + paraTitle
			}
			sb.WriteString(h3 + "\n\n")

			// Render body content
			bodyText := renderArticleContent(n)
			sb.WriteString(bodyText)

			// Collect EU refs from this paragraph
			collectEURefs(n)

			// Build the LegalParagraph chunk
			displayName := strings.TrimSpace(paraNum)
			if paraTitle != "" {
				displayName += " " + paraTitle
			}
			// Paragraph content = heading + body
			paraContent := strings.TrimSpace(h3 + "\n\n" + bodyText)
			if paraContent != "" && sectionID != "" {
				paragraphs = append(paragraphs, lovParagraph{
					SectionID:    sectionID,
					ChapterID:    currentChapterID,
					ParagraphNum: paraNum,
					Title:        paraTitle,
					Content:      paraContent,
					Position:     position,
				})
			}
			_ = displayName

		case n.Data == "ol":
			sb.WriteString(renderOL(n))

		case n.Data == "ul" && cls != "tocSubUl":
			sb.WriteString(renderUL(n))

		default:
			for c := n.FirstChild; c != nil; c = c.NextSibling {
				walk(c)
			}
		}
	}

	for c := main.FirstChild; c != nil; c = c.NextSibling {
		walk(c)
	}

	// Collect all EU body refs from the whole main (catches refs outside § articles too)
	collectEURefs(main)

	fullMarkdown = strings.TrimSpace(sb.String())

	for code := range euRefSet {
		euBodyRefs = append(euBodyRefs, code)
	}

	return fullMarkdown, paragraphs, euBodyRefs
}

// ─── Lovdata document parser ──────────────────────────────────────────────────

func lovParseDocument(content []byte, docType string) *lovDoc {
	doc := &lovDoc{DocType: docType}

	// Detect language from <html lang="...">
	if idx := strings.Index(string(content), `lang="`); idx >= 0 {
		rest := string(content)[idx+6:]
		if end := strings.Index(rest, `"`); end > 0 {
			doc.Language = rest[:end]
		}
	}

	root, err := html.Parse(strings.NewReader(string(content)))
	if err != nil {
		return nil
	}

	var currentDtClass string
	var walkMeta func(*html.Node)
	walkMeta = func(n *html.Node) {
		if n.Type == html.ElementNode {
			switch n.Data {
			case "dt":
				currentDtClass = lovAttr(n, "class")
			case "dd":
				cls := lovAttr(n, "class")
				if cls != currentDtClass {
					break
				}
				text := strings.TrimSpace(lovText(n))
				switch cls {
				case "refid":
					doc.RefID = text
				case "dokid":
					doc.DocID = text
				case "legacyID":
					doc.LegacyID = text
				case "title":
					doc.Title = text
				case "titleShort":
					doc.ShortTitle = text
				case "dateInForce":
					doc.DateInForce = text
				case "lastChangeInForce":
					doc.LastChangeInForce = text
				case "dateOfPublication":
					doc.DateOfPublication = text
				case "appliesTo":
					doc.AppliesTo = strings.TrimSpace(strings.TrimPrefix(text, "Gjelder for"))
				case "eeaReferences":
					doc.EEAReferences = text
					for _, m := range lovDirectivePattern.FindAllStringSubmatch(text, -1) {
						doc.EUDirectiveIDs = lovAppendUniq(doc.EUDirectiveIDs, strings.ToUpper(m[1]))
					}
				case "ministry":
					for _, li := range lovFindAll(n, "li") {
						mn := strings.TrimSpace(lovText(li))
						if mn != "" && doc.Ministry == "" {
							doc.Ministry = mn
						}
					}
				case "legalArea":
					for _, a := range lovFindAll(n, "a") {
						href := lovAttr(a, "href")
						areaName := strings.TrimSpace(lovText(a))
						if areaName == "" {
							continue
						}
						if strings.Contains(href, ".") {
							doc.AllLegalSubAreas = lovAppendUniq(doc.AllLegalSubAreas, areaName)
							if doc.LegalSubArea == "" {
								doc.LegalSubArea = areaName
							}
						} else {
							doc.AllLegalAreas = lovAppendUniq(doc.AllLegalAreas, areaName)
							if doc.LegalArea == "" {
								doc.LegalArea = areaName
							}
						}
					}
				case "lastChangedBy":
					if a := lovFindFirst(n, "a"); a != nil {
						ref := strings.TrimPrefix(strings.TrimSpace(lovAttr(a, "href")), "/")
						if sp := strings.Index(ref, " fra "); sp > 0 {
							ref = ref[:sp]
						}
						doc.LastChangedByRef = ref
					}
				case "changesToDocuments":
					for _, a := range lovFindAll(n, "a") {
						ref := lovStripAnchor(strings.TrimPrefix(strings.TrimSpace(lovAttr(a, "href")), "/"))
						if lovRefPattern.MatchString(ref) {
							doc.AmendsRefs = lovAppendUniq(doc.AmendsRefs, ref)
						}
					}
				case "miscInformation":
					for _, a := range lovFindAll(n, "a") {
						ref := lovStripAnchor(strings.TrimPrefix(strings.TrimSpace(lovAttr(a, "href")), "/"))
						if lovRefPattern.MatchString(ref) {
							doc.SeeAlsoRefs = lovAppendUniq(doc.SeeAlsoRefs, ref)
						}
					}
				}
			}
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			walkMeta(c)
		}
	}
	walkMeta(root)

	// Cross-references from body text
	refSet := make(map[string]bool)
	var walkRefs func(*html.Node)
	walkRefs = func(n *html.Node) {
		if n.Type == html.ElementNode && n.Data == "a" {
			href := lovStripAnchor(strings.TrimPrefix(lovAttr(n, "href"), "/"))
			if lovRefPattern.MatchString(href) && href != doc.RefID {
				refSet[href] = true
			}
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			walkRefs(c)
		}
	}
	// Find <main id="dokument"> (the real body element — NOT class="documentBody")
	var findMain func(*html.Node) *html.Node
	findMain = func(n *html.Node) *html.Node {
		if n.Type == html.ElementNode && n.Data == "main" && lovAttr(n, "id") == "dokument" {
			return n
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			if r := findMain(c); r != nil {
				return r
			}
		}
		return nil
	}
	if main := findMain(root); main != nil {
		walkRefs(main)
		// Extract full Markdown body + paragraph chunks + EU body cross-references
		doc.Content, doc.Paragraphs, doc.EUBodyRefs = lovExtractBody(main)
	}
	for ref := range refSet {
		doc.References = append(doc.References, ref)
	}

	if doc.RefID == "" {
		return nil
	}
	return doc
}

// ─── Lovdata dataset loader ───────────────────────────────────────────────────

func lovDownload(rawURL string) (string, error) {
	os.MkdirAll(lovdataCacheDir, 0755)
	filename := rawURL[strings.LastIndex(rawURL, "/")+1:]
	localPath := filepath.Join(lovdataCacheDir, filename)
	if _, err := os.Stat(localPath); os.IsNotExist(err) {
		fmt.Printf("  Downloading %s ...\n", rawURL)
		resp, err := http.Get(rawURL) //nolint:noctx
		if err != nil {
			return "", err
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			return "", fmt.Errorf("HTTP %d from %s", resp.StatusCode, rawURL)
		}
		f, err := os.Create(localPath)
		if err != nil {
			return "", err
		}
		_, err = io.Copy(f, resp.Body)
		f.Close()
		if err != nil {
			return "", err
		}
		fmt.Printf("  Saved %s\n", localPath)
	} else {
		fmt.Printf("  Using cached %s\n", filename)
	}
	return localPath, nil
}

func lovLoadDataset(dataURL, docType string, limit int) ([]lovDoc, error) {
	path, err := lovDownload(dataURL)
	if err != nil {
		return nil, err
	}

	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	bzr := bzip2.NewReader(f)
	tr := tar.NewReader(bzr)

	var docs []lovDoc
	var mu sync.Mutex
	done := false

	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("tar read: %w", err)
		}
		if hdr.Typeflag != tar.TypeReg || !strings.HasSuffix(hdr.Name, ".xml") {
			continue
		}
		content, err := io.ReadAll(tr)
		if err != nil {
			continue
		}

		mu.Lock()
		if done {
			mu.Unlock()
			continue
		}
		mu.Unlock()

		d := lovParseDocument(content, docType)
		if d == nil {
			continue
		}

		mu.Lock()
		if limit > 0 && len(docs) >= limit {
			done = true
			mu.Unlock()
			continue
		}
		docs = append(docs, *d)
		mu.Unlock()
	}
	return docs, nil
}

// ─── EUR-Lex / EuroVoc fetching ───────────────────────────────────────────────

var lovHTTPClient = &http.Client{
	Timeout: 30 * time.Second,
	Transport: &http.Transport{
		MaxIdleConns:    10,
		IdleConnTimeout: 30 * time.Second,
	},
}

func lovDirectiveToCELEX(id string) string {
	parts := strings.Split(strings.ToUpper(id), "/")
	if len(parts) < 3 {
		return ""
	}
	num, err := strconv.Atoi(parts[1])
	if err != nil {
		return ""
	}
	return fmt.Sprintf("3%sL%04d", parts[0], num)
}

func lovFetchDirective(ctx context.Context, directiveID string) (*lovEUDirective, error) {
	celexL := lovDirectiveToCELEX(directiveID)
	celexR := strings.Replace(celexL, "L", "R", 1)
	var dir *lovEUDirective
	for _, celex := range []string{celexL, celexR} {
		if celex == "" {
			continue
		}
		req, err := http.NewRequestWithContext(ctx, "GET", lovdataEurLexBase+celex, nil)
		if err != nil {
			continue
		}
		req.Header.Set("User-Agent", "Mozilla/5.0 (compatible; LovdataSeeder/1.0)")
		req.Header.Set("Accept-Language", "en-US,en;q=0.9")
		resp, err := lovHTTPClient.Do(req)
		if err != nil || resp.StatusCode != http.StatusOK {
			if resp != nil {
				resp.Body.Close()
			}
			continue
		}
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		dir = lovParseEURLex(directiveID, celex, body)
		break
	}
	if dir == nil {
		dir = &lovEUDirective{DirectiveID: directiveID, CelexID: celexL}
	}

	// ── CELLAR full-text fetch ─────────────────────────────────────────────────
	// Resolve CELEX → CELLAR UUID via SPARQL, then download the OJ XHTML.
	if celexL != "" {
		uuid := lovCellarUUID(ctx, celexL)
		if uuid == "" && celexR != "" {
			uuid = lovCellarUUID(ctx, celexR)
		}
		if uuid != "" {
			if content, fmt := lovFetchCellarContent(ctx, uuid); len(content) > 0 {
				switch fmt {
				case "legacy":
					dir.Content = lovExtractEUBodyLegacy(content)
				case "formex":
					dir.Content = lovExtractEUBodyFormex(content)
				default: // "oj"
					dir.Content = lovExtractEUBody(content)
				}
			}
		}
	}

	return dir, nil
}

func lovParseEURLex(directiveID, celex string, content []byte) *lovEUDirective {
	dir := &lovEUDirective{DirectiveID: directiveID, CelexID: celex}
	text := string(content)

	if m := regexp.MustCompile(`name="WT\.z_docTitle"\s+content="([^"]+)"`).FindStringSubmatch(text); m != nil {
		dir.FullTitle = m[1]
	}
	for _, m := range lovEurovocPattern.FindAllStringSubmatch(text, -1) {
		dir.EuroVocIDs = lovAppendUniq(dir.EuroVocIDs, m[1])
	}

	root, err := html.Parse(strings.NewReader(text))
	if err == nil {
		var label string
		var walk func(*html.Node)
		walk = func(n *html.Node) {
			if n.Type == html.ElementNode {
				switch n.Data {
				case "th", "dt":
					label = strings.TrimSpace(lovText(n))
				case "td", "dd":
					val := strings.Join(strings.Fields(strings.TrimSpace(lovText(n))), " ")
					switch {
					case strings.Contains(label, "Date of document") && dir.DateOfDocument == "":
						dir.DateOfDocument = val
					case strings.Contains(label, "Date of effect") && dir.DateOfEffect == "":
						if idx := strings.Index(val, ";"); idx > 0 {
							dir.DateOfEffect = strings.TrimSpace(val[:idx])
						} else {
							dir.DateOfEffect = val
						}
					case strings.Contains(label, "Form") && dir.Form == "":
						dir.Form = val
					case strings.Contains(label, "Author") && dir.Author == "":
						dir.Author = val
					case strings.Contains(label, "Responsible body") && dir.ResponsibleDG == "":
						dir.ResponsibleDG = val
					case strings.Contains(label, "Subject matter") && dir.SubjectMatter == "":
						dir.SubjectMatter = val
					case strings.Contains(label, "Directory code") && dir.DirectoryCode == "":
						dir.DirectoryCode = val
					case strings.Contains(label, "Legal basis") && dir.LegalBasis == "":
						dir.LegalBasis = val
					case strings.Contains(label, "Procedure number") && dir.ProcedureNum == "":
						dir.ProcedureNum = val
					case strings.Contains(label, "Instruments cited"):
						for _, m := range lovCelexPattern.FindAllString(val, -1) {
							dir.CitedCELEX = lovAppendUniq(dir.CitedCELEX, m)
						}
					case strings.Contains(label, "Modified by"):
						for _, m := range lovCelexPattern.FindAllString(val, -1) {
							dir.ModifiedByCELEX = lovAppendUniq(dir.ModifiedByCELEX, m)
						}
					}
				}
			}
			for c := n.FirstChild; c != nil; c = c.NextSibling {
				walk(c)
			}
		}
		walk(root)
	}

	if m := regexp.MustCompile(`OJ [A-Z]+ \d+[^<\n]{3,40}`).FindString(text); m != "" {
		dir.OJReference = strings.TrimSpace(m)
	}
	if dir.FullTitle != "" {
		short := dir.FullTitle
		if idx := strings.Index(short, "("); idx > 20 {
			short = strings.TrimSpace(short[:idx])
		}
		if len(short) > 120 {
			short = short[:120]
		}
		dir.ShortTitle = short
	}
	return dir
}

func lovFetchEuroVoc(ctx context.Context, ids []string) (map[string]string, error) {
	if len(ids) == 0 {
		return nil, nil
	}
	vals := make([]string, len(ids))
	for i, id := range ids {
		vals[i] = fmt.Sprintf("<http://eurovoc.europa.eu/%s>", id)
	}
	query := fmt.Sprintf(`PREFIX skos: <http://www.w3.org/2004/02/skos/core#>
SELECT ?id ?label WHERE {
  VALUES ?id { %s }
  ?id skos:prefLabel ?label .
  FILTER(LANG(?label) = "en")
}`, strings.Join(vals, " "))

	form := url.Values{"query": {query}}
	req, err := http.NewRequestWithContext(ctx, "POST", lovdataEurovocSPQL, strings.NewReader(form.Encode()))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/sparql-results+json")

	resp, err := lovHTTPClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	var result struct {
		Results struct {
			Bindings []struct {
				ID    struct{ Value string } `json:"id"`
				Label struct{ Value string } `json:"label"`
			} `json:"bindings"`
		} `json:"results"`
	}
	// Use manual JSON decode
	body, _ := io.ReadAll(resp.Body)
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, err
	}

	labels := make(map[string]string)
	for _, b := range result.Results.Bindings {
		uri := b.ID.Value
		if idx := strings.LastIndex(uri, "/"); idx >= 0 {
			labels[uri[idx+1:]] = b.Label.Value
		}
	}
	return labels, nil
}

// lovCellarUUID resolves a CELEX ID to its CELLAR UUID via the Publications Office SPARQL endpoint.
// Returns "" if not found or on error.
func lovCellarUUID(ctx context.Context, celexID string) string {
	query := fmt.Sprintf(`PREFIX cdm: <http://publications.europa.eu/ontology/cdm#>
PREFIX xsd: <http://www.w3.org/2001/XMLSchema#>
SELECT ?work WHERE {
  ?work cdm:resource_legal_id_celex "%s"^^xsd:string .
} LIMIT 1`, celexID)

	form := url.Values{"query": {query}}
	req, err := http.NewRequestWithContext(ctx, "POST", lovdataCellarSPQL, strings.NewReader(form.Encode()))
	if err != nil {
		return ""
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/sparql-results+json")

	resp, err := lovHTTPClient.Do(req)
	if err != nil || resp.StatusCode != http.StatusOK {
		if resp != nil {
			resp.Body.Close()
		}
		return ""
	}
	defer resp.Body.Close()

	var result struct {
		Results struct {
			Bindings []struct {
				Work struct{ Value string } `json:"work"`
			} `json:"bindings"`
		} `json:"results"`
	}
	body, _ := io.ReadAll(resp.Body)
	if err := json.Unmarshal(body, &result); err != nil || len(result.Results.Bindings) == 0 {
		return ""
	}
	uri := result.Results.Bindings[0].Work.Value
	// Extract UUID from URI: http://publications.europa.eu/resource/cellar/{uuid}
	if idx := strings.LastIndex(uri, "/"); idx >= 0 {
		return uri[idx+1:]
	}
	return ""
}

// lovFetchCellarContent fetches the legislative text for a directive via the CELLAR content API.
// It tries multiple language sequences:
//   - seq 03..26: modern OJ XHTML (eli-subdivision/oj-* classes) or Formex XML (<ACT><ARTICLE>)
//   - seq 01: legacy EUR-Lex HTML (<TXT_TE> element, pre-2005 directives)
//
// Returns the raw bytes and a format identifier: "oj", "formex", or "legacy".
func lovFetchCellarContent(ctx context.Context, cellarUUID string) ([]byte, string) {
	type candidate struct {
		seq    int
		body   []byte
		format string // "oj", "formex"
	}
	var first *candidate
	for seq := 3; seq <= 26; seq++ {
		seqStr := fmt.Sprintf("%02d", seq)
		reqURL := fmt.Sprintf("%s%s.0001.%s/DOC_1", lovdataCellarBase, cellarUUID, seqStr)
		req, err := http.NewRequestWithContext(ctx, "GET", reqURL, nil)
		if err != nil {
			break
		}
		resp, err := lovHTTPClient.Do(req)
		if err != nil {
			break
		}
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		if resp.StatusCode != http.StatusOK || len(body) < 1000 {
			if resp.StatusCode == http.StatusNotFound && seq > 5 {
				break
			}
			continue
		}
		prefix := body[:min(500, len(body))]
		// Detect Formex XML (<ACT ...> or <ANNEX ...> with <ARTICLE> elements)
		if bytes.Contains(prefix, []byte("<ACT")) && bytes.Contains(body, []byte("<ARTICLE")) {
			c := &candidate{seq: seq, body: body, format: "formex"}
			if first == nil {
				first = c
			}
			continue // keep looking for OJ XHTML (prefer it over Formex)
		}
		// OJ XHTML
		if bytes.Contains(prefix, []byte("<html")) || bytes.Contains(prefix, []byte("<?xml")) {
			// Check if it has actual article content
			if !bytes.Contains(body, []byte("eli-subdivision")) &&
				!bytes.Contains(body, []byte("oj-ti-art")) {
				continue // XHTML but no article structure
			}
			// Detect language
			lang := ""
			if idx := bytes.Index(body, []byte(`class="oj-hd-lg">`)); idx >= 0 {
				rest := body[idx+len(`class="oj-hd-lg">`):]
				if end := bytes.Index(rest, []byte("<")); end > 0 && end <= 5 {
					lang = strings.TrimSpace(string(rest[:end]))
				}
			}
			c := &candidate{seq: seq, body: body, format: "oj"}
			if first == nil {
				first = c
			}
			if lang == "EN" {
				return body, "oj"
			}
		}
	}
	if first != nil {
		return first.body, first.format
	}

	// Fallback: seq=01 — older directives use legacy EUR-Lex HTML with <TXT_TE>
	reqURL := fmt.Sprintf("%s%s.0001.01/DOC_1", lovdataCellarBase, cellarUUID)
	req, err := http.NewRequestWithContext(ctx, "GET", reqURL, nil)
	if err == nil {
		resp, err := lovHTTPClient.Do(req)
		if err == nil {
			body, _ := io.ReadAll(resp.Body)
			resp.Body.Close()
			if resp.StatusCode == http.StatusOK && len(body) > 1000 &&
				bytes.Contains(body, []byte("<TXT_TE>")) {
				return body, "legacy"
			}
		}
	}
	return nil, ""
}

// min returns the smaller of two ints.
func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// lovExtractEUBody parses OJ XHTML from CELLAR into a Markdown string.
// It uses the standard OJ CSS class conventions:
//   - eli-main-title / oj-doc-ti → document title (skipped, already in metadata)
//   - eli-subdivision id="art_N" → Article N section
//   - oj-ti-art → "Article N" label
//   - oj-sti-art → article subject title
//   - oj-normal → body paragraph
//   - eli-subdivision id="rct_N" → recital (numbered consideration)
//   - eli-subdivision id="cit_N" → citation
func lovExtractEUBody(xhtml []byte) string {
	root, err := html.Parse(bytes.NewReader(xhtml))
	if err != nil {
		return ""
	}

	var sb strings.Builder

	// extractText walks a node tree and returns plain text, stripping all tags.
	var extractText func(*html.Node) string
	extractText = func(n *html.Node) string {
		if n.Type == html.TextNode {
			return n.Data
		}
		var s strings.Builder
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			s.WriteString(extractText(c))
		}
		return s.String()
	}

	// getAttr returns the value of an attribute on an element node.
	getAttr := func(n *html.Node, key string) string {
		for _, a := range n.Attr {
			if a.Key == key {
				return a.Val
			}
		}
		return ""
	}

	hasClass := func(n *html.Node, cls string) bool {
		return strings.Contains(getAttr(n, "class"), cls)
	}

	// Walk the DOM, extracting structured content.
	var walk func(*html.Node)
	walk = func(n *html.Node) {
		if n.Type != html.ElementNode {
			for c := n.FirstChild; c != nil; c = c.NextSibling {
				walk(c)
			}
			return
		}

		cls := getAttr(n, "class")
		id := getAttr(n, "id")

		switch {
		// Skip OJ header (date, language badge, OJ number)
		case hasClass(n, "oj-hd-date") || hasClass(n, "oj-hd-lg") ||
			hasClass(n, "oj-hd-ti") || hasClass(n, "oj-hd-oj"):
			return

		// Skip document title (already stored in metadata)
		case hasClass(n, "eli-main-title"):
			return

		// Skip preamble block (publisher text before recitals)
		case id == "pbl_1":
			return

		// Article section — emit "## Article N\n"
		case n.Data == "div" && hasClass(n, "eli-subdivision") && strings.HasPrefix(id, "art_"):
			// Find oj-ti-art (Article number) and oj-sti-art (article title) inside
			var artNum, artTitle string
			var findArticleHeaders func(*html.Node)
			findArticleHeaders = func(inner *html.Node) {
				if inner.Type == html.ElementNode {
					ic := getAttr(inner, "class")
					if strings.Contains(ic, "oj-ti-art") {
						artNum = strings.TrimSpace(extractText(inner))
					} else if strings.Contains(ic, "oj-sti-art") {
						artTitle = strings.TrimSpace(extractText(inner))
					}
				}
				for c := inner.FirstChild; c != nil; c = c.NextSibling {
					findArticleHeaders(c)
				}
			}
			findArticleHeaders(n)

			header := artNum
			if artTitle != "" {
				header += " — " + artTitle
			}
			if header != "" {
				sb.WriteString("## " + header + "\n\n")
			}
			// Walk children for paragraph content
			for c := n.FirstChild; c != nil; c = c.NextSibling {
				walk(c)
			}
			return

		// Recital — emit numbered paragraph
		case n.Data == "div" && hasClass(n, "eli-subdivision") && strings.HasPrefix(id, "rct_"):
			text := strings.Join(strings.Fields(strings.TrimSpace(extractText(n))), " ")
			if text != "" {
				// Extract recital number from id (rct_1 → 1)
				num := strings.TrimPrefix(id, "rct_")
				sb.WriteString("(" + num + ") " + text + "\n\n")
			}
			return

		// Skip citation blocks (whereas clauses) — verbose and low semantic value
		case n.Data == "div" && hasClass(n, "eli-subdivision") && strings.HasPrefix(id, "cit_"):
			return

		// Skip article title headers (already handled in art_ block above)
		case hasClass(n, "oj-ti-art") || hasClass(n, "oj-sti-art"):
			return

		// Body paragraphs
		case n.Data == "p" && hasClass(n, "oj-normal"):
			text := strings.Join(strings.Fields(strings.TrimSpace(extractText(n))), " ")
			if text != "" {
				sb.WriteString(text + "\n\n")
			}
			return

		// Signatory / final text
		case hasClass(n, "oj-signatory") || hasClass(n, "oj-final"):
			return

		// Skip footnotes
		case hasClass(n, "oj-note"):
			return

		default:
			// Check if this is a table row (for structured content in some directives)
			if n.Data == "table" && !strings.Contains(cls, "oj-hd") {
				// Render table rows as plain text paragraphs
				var tableText func(*html.Node)
				tableText = func(inner *html.Node) {
					if inner.Type == html.ElementNode && inner.Data == "td" {
						text := strings.Join(strings.Fields(strings.TrimSpace(extractText(inner))), " ")
						if text != "" && len(text) > 10 {
							sb.WriteString(text + "\n\n")
						}
					}
					for c := inner.FirstChild; c != nil; c = c.NextSibling {
						tableText(c)
					}
				}
				tableText(n)
				return
			}
		}

		for c := n.FirstChild; c != nil; c = c.NextSibling {
			walk(c)
		}
	}

	walk(root)
	return strings.TrimSpace(sb.String())
}

// lovExtractEUBodyLegacy parses the older EUR-Lex HTML format (pre-2005 directives).
// These documents contain a <TXT_TE> element with plain <p> paragraphs.
// The content is typically in a non-English EU language but is still structurally useful.
func lovExtractEUBodyLegacy(htmlBody []byte) string {
	// Find the TXT_TE section by byte search — it contains the actual directive text.
	start := bytes.Index(htmlBody, []byte("<TXT_TE>"))
	if start < 0 {
		// Fallback: try TexteOnly div
		start = bytes.Index(htmlBody, []byte(`<div id="TexteOnly">`))
		if start < 0 {
			return ""
		}
	}
	end := bytes.Index(htmlBody[start:], []byte("</TXT_TE>"))
	if end < 0 {
		end = bytes.Index(htmlBody[start:], []byte("</div>"))
	}
	if end < 0 {
		end = len(htmlBody) - start
	}
	chunk := htmlBody[start : start+end]

	root, err := html.Parse(bytes.NewReader(chunk))
	if err != nil {
		return ""
	}

	var sb strings.Builder
	var extractText func(*html.Node) string
	extractText = func(n *html.Node) string {
		if n.Type == html.TextNode {
			return n.Data
		}
		var s strings.Builder
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			s.WriteString(extractText(c))
		}
		return s.String()
	}

	var walk func(*html.Node)
	walk = func(n *html.Node) {
		if n.Type == html.ElementNode {
			switch n.Data {
			case "p":
				txt := strings.Join(strings.Fields(extractText(n)), " ")
				if txt != "" {
					sb.WriteString(txt)
					sb.WriteString("\n\n")
				}
				return // don't recurse into children of <p>
			case "br":
				sb.WriteString("\n")
				return
			case "h1", "h2", "h3":
				txt := strings.Join(strings.Fields(extractText(n)), " ")
				if txt != "" {
					sb.WriteString("## ")
					sb.WriteString(txt)
					sb.WriteString("\n\n")
				}
				return
			}
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			walk(c)
		}
	}
	walk(root)
	return strings.TrimSpace(sb.String())
}

// lovExtractEUBodyFormex parses Formex XML (the EU's native legislative XML format).
// It handles <ACT> documents with <ARTICLE>, <TI.ART>, <STI.ART>, <ALINEA> elements.
// This format is used for directives from roughly 2003-2014 in the CELLAR database.
func lovExtractEUBodyFormex(xmlBody []byte) string {
	text := string(xmlBody)
	var sb strings.Builder

	// Helper: strip XML tags and normalize whitespace
	stripXML := func(s string) string {
		s = regexp.MustCompile(`<[^>]+>`).ReplaceAllString(s, " ")
		return strings.Join(strings.Fields(s), " ")
	}

	// Find all ARTICLE elements
	artRe := regexp.MustCompile(`(?s)<ARTICLE[^>]*>(.*?)</ARTICLE>`)
	tiArtRe := regexp.MustCompile(`(?s)<TI\.ART>(.*?)</TI\.ART>`)
	stiArtRe := regexp.MustCompile(`(?s)<STI\.ART>(.*?)</STI\.ART>`)
	alineaRe := regexp.MustCompile(`(?s)<(?:ALINEA|P)>(.*?)</(?:ALINEA|P)>`)

	for _, artMatch := range artRe.FindAllStringSubmatch(text, -1) {
		artBody := artMatch[1]

		var artNum, artTitle string
		if m := tiArtRe.FindStringSubmatch(artBody); m != nil {
			artNum = stripXML(m[1])
		}
		if m := stiArtRe.FindStringSubmatch(artBody); m != nil {
			artTitle = stripXML(m[1])
		}

		header := artNum
		if header == "" {
			header = "Article"
		}
		if artTitle != "" {
			header += " — " + artTitle
		}
		sb.WriteString("## ")
		sb.WriteString(header)
		sb.WriteString("\n\n")

		for _, alineaMatch := range alineaRe.FindAllStringSubmatch(artBody, -1) {
			t := stripXML(alineaMatch[1])
			if t != "" && len(t) > 5 {
				sb.WriteString(t)
				sb.WriteString("\n\n")
			}
		}
	}
	return strings.TrimSpace(sb.String())
}

func lovFetchAllEUData(ctx context.Context, docs []lovDoc, limit int) ([]*lovEUDirective, []*lovEuroVocConcept) {
	allIDs := make(map[string]bool)
	for _, d := range docs {
		for _, did := range d.EUDirectiveIDs {
			allIDs[did] = true
		}
	}
	ids := make([]string, 0, len(allIDs))
	for id := range allIDs {
		ids = append(ids, id)
	}
	if limit > 0 && len(ids) > limit {
		ids = ids[:limit]
	}
	fmt.Printf("  Fetching %d EU directives from EUR-Lex (max 5 concurrent) ...\n", len(ids))

	sem := make(chan struct{}, 5)
	var mu sync.Mutex
	var directives []*lovEUDirective
	var wg sync.WaitGroup

	for _, id := range ids {
		sem <- struct{}{}
		wg.Add(1)
		go func(did string) {
			defer wg.Done()
			defer func() { <-sem }()
			if ctx.Err() != nil {
				return
			}
			dir, err := lovFetchDirective(ctx, did)
			if err != nil {
				dir = &lovEUDirective{DirectiveID: did, CelexID: lovDirectiveToCELEX(did)}
			} else if dbLovdataFlags.verbose {
				title := dir.FullTitle
				if len(title) > 60 {
					title = title[:60] + "…"
				}
				fmt.Printf("  [EUR-Lex] ✓ %s → %s: %s\n", did, dir.CelexID, title)
			}
			mu.Lock()
			directives = append(directives, dir)
			mu.Unlock()
			time.Sleep(200 * time.Millisecond)
		}(id)
	}
	wg.Wait()

	// Collect EuroVoc IDs
	evIDs := make(map[string]bool)
	for _, dir := range directives {
		for _, id := range dir.EuroVocIDs {
			evIDs[id] = true
		}
	}
	evList := make([]string, 0, len(evIDs))
	for id := range evIDs {
		evList = append(evList, id)
	}
	fmt.Printf("  Fetching %d EuroVoc concept labels via SPARQL ...\n", len(evList))

	evLabels := make(map[string]string)
	for i := 0; i < len(evList); i += 50 {
		end := i + 50
		if end > len(evList) {
			end = len(evList)
		}
		labels, err := lovFetchEuroVoc(ctx, evList[i:end])
		if err != nil {
			fmt.Printf("  [EuroVoc] batch %d-%d error: %v\n", i, end, err)
			continue
		}
		for id, label := range labels {
			evLabels[id] = label
		}
		time.Sleep(100 * time.Millisecond)
	}

	var concepts []*lovEuroVocConcept
	for id, label := range evLabels {
		concepts = append(concepts, &lovEuroVocConcept{ID: id, LabelEN: label})
	}
	fmt.Printf("  Fetched %d/%d EuroVoc labels\n", len(concepts), len(evList))
	return directives, concepts
}

// ─── Graph ingestion ──────────────────────────────────────────────────────────

func lovIngestObjects(ctx context.Context, client *sdkgraph.Client, docs []lovDoc, directives []*lovEUDirective, concepts []*lovEuroVocConcept, batchSz, nWorkers int) map[string]string {
	var items []sdkgraph.CreateObjectRequest

	ministries := make(map[string]bool)
	legalAreas := make(map[string]bool)
	legalSubAreas := make(map[string]string)

	for _, d := range docs {
		if d.Ministry != "" {
			ministries[d.Ministry] = true
		}
		for _, a := range d.AllLegalAreas {
			legalAreas[a] = true
		}
		for _, sa := range d.AllLegalSubAreas {
			if _, exists := legalSubAreas[sa]; !exists {
				legalSubAreas[sa] = d.LegalArea
			}
		}
	}

	for m := range ministries {
		k := "ministry_" + m
		items = append(items, sdkgraph.CreateObjectRequest{
			Type: "Ministry", Key: lovStrPtr(k),
			Properties: map[string]any{"name": m},
		})
	}
	for a := range legalAreas {
		k := "area_" + a
		items = append(items, sdkgraph.CreateObjectRequest{
			Type: "LegalArea", Key: lovStrPtr(k),
			Properties: map[string]any{"name": a},
		})
	}
	for sa, parent := range legalSubAreas {
		k := "subarea_" + sa
		items = append(items, sdkgraph.CreateObjectRequest{
			Type: "LegalArea", Key: lovStrPtr(k),
			Properties: map[string]any{"name": sa, "parent_area": parent},
		})
	}

	for _, d := range docs {
		k := d.RefID
		props := map[string]any{
			"name":   d.Title,
			"title":  d.Title,
			"ref_id": d.RefID,
			"doc_id": d.DocID,
		}
		if d.ShortTitle != "" {
			props["short_title"] = d.ShortTitle
		}
		if d.LegacyID != "" {
			props["legacy_id"] = d.LegacyID
		}
		if d.Language != "" {
			props["language"] = d.Language
		}
		if d.DateInForce != "" {
			props["date_in_force"] = d.DateInForce
			if yr, err := strconv.Atoi(d.DateInForce[:4]); err == nil {
				props["year_in_force"] = yr
				props["decade_in_force"] = fmt.Sprintf("%ds", (yr/10)*10)
			}
		}
		if d.LastChangeInForce != "" {
			props["last_change_in_force"] = d.LastChangeInForce
		}
		if d.DateOfPublication != "" {
			props["date_of_publication"] = d.DateOfPublication
		}
		if d.AppliesTo != "" {
			props["applies_to"] = d.AppliesTo
		}
		if d.EEAReferences != "" {
			props["eea_references"] = d.EEAReferences
		}
		if d.Content != "" {
			props["content"] = d.Content
		}
		items = append(items, sdkgraph.CreateObjectRequest{
			Type: d.DocType, Key: lovStrPtr(k), Properties: props,
		})

		// ── LegalParagraph nodes — one per § article ──────────────────────────
		for _, p := range d.Paragraphs {
			if p.Content == "" || p.SectionID == "" {
				continue
			}
			pKey := d.RefID + "#" + p.SectionID
			pName := p.ParagraphNum
			if p.Title != "" {
				pName += " " + p.Title
			}
			pProps := map[string]any{
				"name":          pName,
				"content":       p.Content,
				"section_id":    p.SectionID,
				"paragraph_num": p.ParagraphNum,
				"law_ref_id":    d.RefID,
				"position":      p.Position,
			}
			if p.ChapterID != "" {
				pProps["chapter_id"] = p.ChapterID
			}
			if p.Title != "" {
				pProps["title"] = p.Title
			}
			items = append(items, sdkgraph.CreateObjectRequest{
				Type: "LegalParagraph", Key: lovStrPtr(pKey), Properties: pProps,
			})
		}
	}

	for _, dir := range directives {
		k := "eu_" + dir.CelexID
		name := dir.ShortTitle
		if name == "" {
			name = dir.CelexID
		}
		props := map[string]any{
			"name":         name,
			"celex_id":     dir.CelexID,
			"directive_id": dir.DirectiveID,
		}
		if dir.FullTitle != "" {
			props["full_title"] = dir.FullTitle
		}
		if dir.Form != "" {
			props["form"] = dir.Form
		}
		if dir.DateOfDocument != "" {
			props["date_of_document"] = dir.DateOfDocument
		}
		if dir.DateOfEffect != "" {
			props["date_of_effect"] = dir.DateOfEffect
		}
		if dir.Author != "" {
			props["author"] = dir.Author
		}
		if dir.SubjectMatter != "" {
			props["subject_matter"] = dir.SubjectMatter
		}
		if dir.OJReference != "" {
			props["oj_reference"] = dir.OJReference
		}
		if dir.Content != "" {
			props["content"] = dir.Content
		}
		items = append(items, sdkgraph.CreateObjectRequest{
			Type: "EUDirective", Key: lovStrPtr(k), Properties: props,
		})
	}

	for _, ev := range concepts {
		k := "eurovoc_" + ev.ID
		items = append(items, sdkgraph.CreateObjectRequest{
			Type: "EuroVocConcept", Key: lovStrPtr(k),
			Properties: map[string]any{
				"name":       ev.LabelEN,
				"eurovoc_id": ev.ID,
				"label_en":   ev.LabelEN,
			},
		})
	}

	fmt.Printf("  Uploading %d objects in batches of %d with %d workers ...\n", len(items), batchSz, nWorkers)
	return benchBulkUploadObjects(ctx, client, items, batchSz, nWorkers)
}

func lovIngestRelationships(ctx context.Context, client *sdkgraph.Client, docs []lovDoc, directives []*lovEUDirective, idMap map[string]string, batchSz, nWorkers int) (int64, int64) {
	var items []sdkgraph.CreateRelationshipRequest

	knownRefs := make(map[string]bool, len(docs))
	for _, d := range docs {
		knownRefs[d.RefID] = true
	}

	dirByID := make(map[string]*lovEUDirective, len(directives))
	for _, dir := range directives {
		dirByID[dir.DirectiveID] = dir
		dirByID[strings.Replace(dir.DirectiveID, "EF", "EU", 1)] = dir
	}

	for _, d := range docs {
		src, ok := idMap[d.RefID]
		if !ok {
			continue
		}

		if d.Ministry != "" {
			if dst, ok2 := idMap["ministry_"+d.Ministry]; ok2 {
				items = append(items, sdkgraph.CreateRelationshipRequest{
					Type: "ADMINISTERED_BY", SrcID: src, DstID: dst, Properties: map[string]any{},
				})
			}
		}
		for _, area := range d.AllLegalAreas {
			if dst, ok2 := idMap["area_"+area]; ok2 {
				items = append(items, sdkgraph.CreateRelationshipRequest{
					Type: "IN_LEGAL_AREA", SrcID: src, DstID: dst, Properties: map[string]any{},
				})
			}
		}
		for _, sub := range d.AllLegalSubAreas {
			if dst, ok2 := idMap["subarea_"+sub]; ok2 {
				items = append(items, sdkgraph.CreateRelationshipRequest{
					Type: "IN_LEGAL_AREA", SrcID: src, DstID: dst, Properties: map[string]any{"level": "sub"},
				})
			}
		}
		if d.LastChangedByRef != "" {
			if dst, ok2 := idMap[d.LastChangedByRef]; ok2 {
				items = append(items, sdkgraph.CreateRelationshipRequest{
					Type: "AMENDED_BY", SrcID: src, DstID: dst,
					Properties: map[string]any{"effective_date": d.LastChangeInForce},
				})
			}
		}
		for _, ref := range d.AmendsRefs {
			if dst, ok2 := idMap[ref]; ok2 {
				items = append(items, sdkgraph.CreateRelationshipRequest{
					Type: "AMENDS", SrcID: src, DstID: dst, Properties: map[string]any{},
				})
			}
		}
		for _, ref := range d.SeeAlsoRefs {
			if knownRefs[ref] {
				if dst, ok2 := idMap[ref]; ok2 {
					items = append(items, sdkgraph.CreateRelationshipRequest{
						Type: "SEE_ALSO", SrcID: src, DstID: dst, Properties: map[string]any{},
					})
				}
			}
		}
		for _, ref := range d.References {
			if knownRefs[ref] {
				if dst, ok2 := idMap[ref]; ok2 {
					items = append(items, sdkgraph.CreateRelationshipRequest{
						Type: "REFERENCES", SrcID: src, DstID: dst, Properties: map[string]any{},
					})
				}
			}
		}
		for _, did := range d.EUDirectiveIDs {
			if dir := dirByID[did]; dir != nil {
				if dst, ok2 := idMap["eu_"+dir.CelexID]; ok2 {
					items = append(items, sdkgraph.CreateRelationshipRequest{
						Type: "IMPLEMENTS_EEA", SrcID: src, DstID: dst,
						Properties: map[string]any{"directive_id": did},
					})
				}
			}
		}
		// HAS_PARAGRAPH: Law/Reg → LegalParagraph
		for _, p := range d.Paragraphs {
			if p.Content == "" || p.SectionID == "" {
				continue
			}
			pKey := d.RefID + "#" + p.SectionID
			if dst, ok2 := idMap[pKey]; ok2 {
				items = append(items, sdkgraph.CreateRelationshipRequest{
					Type: "HAS_PARAGRAPH", SrcID: src, DstID: dst,
					Properties: map[string]any{"position": p.Position},
				})
			}
		}
		// CITES_EU_LAW: Law/Reg → EUDirective (from body eu/ hrefs)
		seenEU := make(map[string]bool)
		for _, euCode := range d.EUBodyRefs {
			euKey := "eu_" + euCode // euCode is already uppercased in lovExtractBody
			if seenEU[euKey] {
				continue
			}
			seenEU[euKey] = true
			if dst, ok2 := idMap[euKey]; ok2 {
				items = append(items, sdkgraph.CreateRelationshipRequest{
					Type: "CITES_EU_LAW", SrcID: src, DstID: dst,
					Properties: map[string]any{},
				})
			}
		}
	}

	// Language variant pairs
	refsByID := make(map[string][]lovDoc)
	for _, d := range docs {
		refsByID[d.RefID] = append(refsByID[d.RefID], d)
	}
	for _, group := range refsByID {
		for i := 0; i < len(group); i++ {
			for j := i + 1; j < len(group); j++ {
				s, ok1 := idMap[group[i].RefID]
				dt, ok2 := idMap[group[j].RefID]
				if ok1 && ok2 && s != dt {
					items = append(items, sdkgraph.CreateRelationshipRequest{
						Type: "HAS_LANGUAGE_VARIANT", SrcID: s, DstID: dt,
						Properties: map[string]any{
							"source_language": group[i].Language,
							"target_language": group[j].Language,
						},
					})
				}
			}
		}
	}

	// EU chain relationships
	for _, dir := range directives {
		src, ok := idMap["eu_"+dir.CelexID]
		if !ok {
			continue
		}
		for _, cited := range dir.CitedCELEX {
			if dst, ok2 := idMap["eu_"+cited]; ok2 {
				items = append(items, sdkgraph.CreateRelationshipRequest{
					Type: "EU_CITES", SrcID: src, DstID: dst, Properties: map[string]any{},
				})
			}
		}
		for _, mod := range dir.ModifiedByCELEX {
			if dst, ok2 := idMap["eu_"+mod]; ok2 {
				items = append(items, sdkgraph.CreateRelationshipRequest{
					Type: "EU_MODIFIED_BY", SrcID: src, DstID: dst, Properties: map[string]any{},
				})
			}
		}
		for _, evID := range dir.EuroVocIDs {
			if dst, ok2 := idMap["eurovoc_"+evID]; ok2 {
				items = append(items, sdkgraph.CreateRelationshipRequest{
					Type: "HAS_EUROVOC_DESCRIPTOR", SrcID: src, DstID: dst, Properties: map[string]any{},
				})
			}
		}
	}

	fmt.Printf("  Uploading %d relationships in batches of %d with %d workers ...\n", len(items), batchSz, nWorkers)
	return benchBulkUploadRelationships(ctx, client, items, batchSz, nWorkers)
}
