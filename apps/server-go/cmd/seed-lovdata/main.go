package main

// Lovdata + EUR-Lex Knowledge Graph Seeder
//
// Builds the most comprehensive possible Norwegian law knowledge graph by
// combining three public, no-auth data sources:
//
//   SOURCE 1 — Lovdata public datasets (NLOD 2.0 license)
//     https://api.lovdata.no/v1/publicData/list
//     gjeldende-lover.tar.bz2              775 laws
//     gjeldende-sentrale-forskrifter.tar.bz2  ~thousands of regulations
//
//   SOURCE 2 — EUR-Lex (public, scraped via HTML)
//     https://eur-lex.europa.eu/legal-content/EN/ALL/?uri=CELEX:{id}
//     Full metadata for every EU directive/regulation referenced by Norwegian law
//
//   SOURCE 3 — EuroVoc SPARQL (EU Publications Office, public)
//     https://publications.europa.eu/webapi/rdf/sparql
//     Human-readable labels for EuroVoc thesaurus concept IDs
//
// ═══════════════════════════════════════════════════════════════════════════
// GRAPH SCHEMA
// ═══════════════════════════════════════════════════════════════════════════
//
// NODE TYPES:
//
//   Law          Norwegian law (lov)
//     key:       ref_id  e.g. "lov/1997-02-28-19"
//     props:     title, short_title, doc_id, legacy_id, language,
//                date_in_force, year_in_force, decade_in_force,
//                last_change_in_force, date_of_publication,
//                applies_to, eea_references
//
//   Regulation   Norwegian central regulation (sentral forskrift)
//     key:       ref_id  e.g. "forskrift/2023-06-16-898"
//     props:     same as Law
//
//   Ministry     Norwegian government ministry (17 unique)
//     key:       "ministry_" + name
//     props:     name
//
//   LegalArea    Norwegian legal taxonomy node (34 top-level + 242 sub-areas)
//     key:       "area_" + name  /  "subarea_" + name
//     props:     name, parent_area (set for sub-areas)
//
//   EUDirective  EU directive or regulation referenced by Norwegian law (86+)
//     key:       "eu_" + celex_id  e.g. "eu_32009L0103"
//     props:     celex_id, directive_id, full_title, short_title,
//                form, date_of_document, date_of_effect,
//                author, responsible_dg, subject_matter,
//                directory_code, legal_basis, procedure_number,
//                oj_reference
//
//   EuroVocConcept  EuroVoc multilingual thesaurus concept (~600+ unique)
//     key:       "eurovoc_" + id  e.g. "eurovoc_730"
//     props:     eurovoc_id, label_en
//
// RELATIONSHIP TYPES:
//
//   From Lovdata XML metadata (header fields):
//     ADMINISTERED_BY   Law/Reg → Ministry          (every doc)
//     IN_LEGAL_AREA     Law/Reg → LegalArea          (top-level + sub, level prop)
//     AMENDED_BY        Law/Reg → Law/Reg            (lastChangedBy, effective_date)
//     AMENDS            Law/Reg → Law/Reg            (changesToDocuments field)
//     SEE_ALSO          Law/Reg → Law/Reg            (Jf. links in miscInformation)
//     HAS_LANGUAGE_VARIANT  Law → Law               (Bokmål ↔ Nynorsk pairs)
//
//   From Lovdata XML body text:
//     REFERENCES        Law/Reg → Law/Reg            (~37,600 cross-doc hrefs)
//
//   From EUR-Lex:
//     IMPLEMENTS_EEA    Law/Reg → EUDirective        (parsed from eeaReferences)
//     EU_CITES          EUDirective → EUDirective    (instruments cited chain)
//     EU_MODIFIED_BY    EUDirective → EUDirective    (EU amendment chain)
//
//   From EuroVoc SPARQL:
//     HAS_EUROVOC_DESCRIPTOR  EUDirective → EuroVocConcept
//
// ═══════════════════════════════════════════════════════════════════════════
// USAGE
// ═══════════════════════════════════════════════════════════════════════════
//
//   go run ./cmd/seed-lovdata/main.go
//
// Environment variables:
//   SERVER_URL     Emergent API base URL   (default: http://localhost:3002)
//   API_KEY        Emergent API key        (default: dev key)
//   PROJECT_ID     Emergent project UUID   (optional: auto-created if blank)
//   ORG_ID         Org UUID for project creation (default: dev org)
//   DRY_RUN        "true" → limit to 20 docs per dataset + 5 directives
//   SEED_LIMIT     integer max docs per dataset (0 = unlimited)
//   RETRY_FAILED   "true" → replay rels_failed.jsonl only
//   DATASET        "laws" | "regulations" | "both"  (default: "both")
//   SKIP_EU        "true" → skip EUR-Lex + EuroVoc enrichment

import (
	"archive/tar"
	"bufio"
	"compress/bzip2"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/emergent-company/emergent/apps/server-go/pkg/sdk"
	"github.com/emergent-company/emergent/apps/server-go/pkg/sdk/graph"
	"github.com/emergent-company/emergent/apps/server-go/pkg/sdk/projects"
	"golang.org/x/net/html"
)

// ── Constants ─────────────────────────────────────────────────────────────────

const (
	stateDir   = "/tmp/lovdata_seed_state"
	cacheDir   = "/tmp/lovdata_data"
	batchSize  = 100
	numWorkers = 20
)

const (
	lawsURL        = "https://api.lovdata.no/v1/publicData/get/gjeldende-lover.tar.bz2"
	regulationsURL = "https://api.lovdata.no/v1/publicData/get/gjeldende-sentrale-forskrifter.tar.bz2"

	eurLexBaseURL = "https://eur-lex.europa.eu/legal-content/EN/ALL/?uri=CELEX:"
	eurovocSPARQL = "https://publications.europa.eu/webapi/rdf/sparql"
)

// ── State management ──────────────────────────────────────────────────────────

const (
	phaseObjectsPending = "objects_pending"
	phaseObjectsDone    = "objects_done"
	phaseRelsPending    = "rels_pending"
	phaseDone           = "done"
)

type SeedState struct {
	Phase string `json:"phase"`
}

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

func loadRelsDone() map[int]bool {
	done := make(map[int]bool)
	f, err := os.Open(filepath.Join(stateDir, "rels_done.txt"))
	if err != nil {
		return done
	}
	defer f.Close()
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		if idx, err := strconv.Atoi(strings.TrimSpace(scanner.Text())); err == nil {
			done[idx] = true
		}
	}
	return done
}

func appendRelDone(idx int) {
	f, err := os.OpenFile(filepath.Join(stateDir, "rels_done.txt"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0644)
	if err != nil {
		return
	}
	defer f.Close()
	fmt.Fprintf(f, "%d\n", idx)
}

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

const defaultOrgID = "dcba78f0-fc40-414a-a24d-f9c32b762f15"
const projectName = "Norwegian Law"
const projectIDFile = "project_id.txt"

// ensureProject finds or creates the "Norwegian Law" project and returns its ID.
// The resolved ID is persisted to state so resumes reuse the same project.
func ensureProject(ctx context.Context, client *sdk.Client, orgID string) (string, error) {
	// Check cached project ID first
	if data, err := os.ReadFile(filepath.Join(stateDir, projectIDFile)); err == nil {
		if id := strings.TrimSpace(string(data)); id != "" {
			log.Printf("Reusing cached project ID: %s", id)
			return id, nil
		}
	}

	// List existing projects to find by name
	list, err := client.Projects.List(ctx, &projects.ListOptions{OrgID: orgID})
	if err != nil {
		return "", fmt.Errorf("listing projects: %w", err)
	}
	for _, p := range list {
		if p.Name == projectName {
			log.Printf("Found existing project %q (ID: %s)", projectName, p.ID)
			os.WriteFile(filepath.Join(stateDir, projectIDFile), []byte(p.ID), 0644)
			return p.ID, nil
		}
	}

	// Create fresh project
	proj, err := client.Projects.Create(ctx, &projects.CreateProjectRequest{
		Name:  projectName,
		OrgID: orgID,
	})
	if err != nil {
		return "", fmt.Errorf("creating project %q: %w", projectName, err)
	}
	log.Printf("Created new project %q (ID: %s)", proj.Name, proj.ID)
	os.WriteFile(filepath.Join(stateDir, projectIDFile), []byte(proj.ID), 0644)
	return proj.ID, nil
}

func loadRelsFailed() [][]graph.CreateRelationshipRequest {
	var batches [][]graph.CreateRelationshipRequest
	f, err := os.Open(filepath.Join(stateDir, "rels_failed.jsonl"))
	if err != nil {
		return batches
	}
	defer f.Close()
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 10*1024*1024), 10*1024*1024)
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

// ── Domain types ──────────────────────────────────────────────────────────────

type DocType string

const (
	DocTypeLaw        DocType = "Law"
	DocTypeRegulation DocType = "Regulation"
)

// LovdataDoc holds all parsed data from a single Lovdata XML document.
type LovdataDoc struct {
	RefID             string // "lov/1997-02-28-19"
	DocID             string // "NL/lov/1997-02-28-19"
	LegacyID          string // "LOV-1997-02-28-19"
	Title             string
	ShortTitle        string
	Language          string // "no", "nb", "nn"
	Ministry          string
	LegalArea         string
	LegalSubArea      string
	AllLegalAreas     []string // all top-level areas (some laws span multiple)
	AllLegalSubAreas  []string // all sub-areas
	DateInForce       string
	LastChangeInForce string
	DateOfPublication string
	AppliesTo         string   // e.g. "Svalbard only"
	LastChangedByRef  string   // refID of last amending act
	AmendsRefs        []string // refIDs this doc explicitly amends (changesToDocuments)
	SeeAlsoRefs       []string // refIDs from Jf. (see-also) in miscInformation
	EEAReferences     string   // raw EEA text
	EUDirectiveIDs    []string // parsed CELEX-style directive IDs e.g. "2009/103/EF"
	Type              DocType
	References        []string // cross-doc hrefs from body text
}

// EUDirective holds metadata fetched from EUR-Lex for a single directive.
type EUDirective struct {
	DirectiveID     string // "2009/103/EF" as found in Lovdata
	CelexID         string // "32009L0103"
	FullTitle       string
	ShortTitle      string // abbreviated title if available
	Form            string // "Directive", "Regulation", etc.
	DateOfDocument  string
	DateOfEffect    string
	Author          string
	ResponsibleDG   string
	SubjectMatter   string
	DirectoryCode   string
	LegalBasis      string
	ProcedureNum    string
	OJReference     string
	EuroVocIDs      []string // EuroVoc concept IDs e.g. "730"
	CitedCELEX      []string // instruments cited by this directive
	ModifiedByCELEX []string // CELEX IDs that amended this directive
}

// EuroVocConcept is a labelled EuroVoc thesaurus concept.
type EuroVocConcept struct {
	ID      string // "730"
	LabelEN string // "transport accident"
}

// ── Regex patterns ────────────────────────────────────────────────────────────

var (
	// Matches Lovdata document cross-reference hrefs.
	refPattern = regexp.MustCompile(`^(?:lov|forskrift|res)/\d{4}-\d{2}-\d{2}`)

	// Matches EU directive/regulation IDs in EEA reference text.
	// e.g. "2009/103/EF", "2016/679/EU"
	directiveIDPattern = regexp.MustCompile(`\b(\d{4}/[\d]+/(?:EF|EØF|EU|EEC|EC|EØF))\b`)

	// Matches CELEX IDs in EUR-Lex HTML, e.g. "31973L0239"
	celexPattern = regexp.MustCompile(`\b(3\d{7}[A-Z]\d+)\b`)

	// Matches EuroVoc concept IDs in EUR-Lex HTML.
	eurovocPattern = regexp.MustCompile(`eurovoc\.europa\.eu/(\d+)`)
)

// ── Lovdata XML parsing ───────────────────────────────────────────────────────

// parseDocument parses the semantic HTML5/XML produced by Lovdata and returns
// a fully populated LovdataDoc. Returns nil if the document cannot be parsed.
func parseDocument(content []byte, docType DocType) *LovdataDoc {
	doc := &LovdataDoc{Type: docType}

	// Detect document language from <html lang="...">
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

	// ── Metadata from <dl class="data-document-key-info"> ────────────────────
	var currentDtClass string
	var walkMeta func(*html.Node)
	walkMeta = func(n *html.Node) {
		if n.Type == html.ElementNode {
			switch n.Data {
			case "dt":
				currentDtClass = attrVal(n, "class")
			case "dd":
				cls := attrVal(n, "class")
				if cls != currentDtClass {
					break
				}
				text := strings.TrimSpace(textContent(n))
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
					doc.AppliesTo = strings.TrimPrefix(text, "Gjelder for")
					doc.AppliesTo = strings.TrimSpace(doc.AppliesTo)
				case "eeaReferences":
					doc.EEAReferences = text
					// Parse all directive IDs from EEA reference text
					for _, m := range directiveIDPattern.FindAllStringSubmatch(text, -1) {
						doc.EUDirectiveIDs = appendUniq(doc.EUDirectiveIDs, strings.ToUpper(m[1]))
					}
				case "ministry":
					// All <li> elements (some laws have multiple ministries)
					for _, li := range findAll(n, "li") {
						mn := strings.TrimSpace(textContent(li))
						if mn != "" && doc.Ministry == "" {
							doc.Ministry = mn // primary
						}
					}
				case "legalArea":
					// The legalArea <dd> can contain multiple top/sub area pairs.
					// Each pair is: <a href="legal-areas/NN" title="TopArea">TopArea</a> > <a ...>SubArea</a>
					links := findAll(n, "a")
					for i, a := range links {
						href := attrVal(a, "href")
						areaName := strings.TrimSpace(textContent(a))
						if areaName == "" {
							continue
						}
						if strings.Contains(href, ".") {
							// sub-area (e.g. legal-areas/01.02)
							doc.AllLegalSubAreas = appendUniq(doc.AllLegalSubAreas, areaName)
							if doc.LegalSubArea == "" {
								doc.LegalSubArea = areaName
							}
						} else {
							// top-level area
							doc.AllLegalAreas = appendUniq(doc.AllLegalAreas, areaName)
							if doc.LegalArea == "" {
								doc.LegalArea = areaName
							}
						}
						_ = i
					}
				case "lastChangedBy":
					// First <a> href is the primary amending act
					if a := findFirst(n, "a"); a != nil {
						ref := strings.TrimSpace(attrVal(a, "href"))
						ref = strings.TrimPrefix(ref, "/")
						// strip " fra YYYY-MM-DD" suffix if present in href
						if sp := strings.Index(ref, " fra "); sp > 0 {
							ref = ref[:sp]
						}
						doc.LastChangedByRef = ref
					}
				case "changesToDocuments":
					// All <a> hrefs: laws/regulations this doc amends
					for _, a := range findAll(n, "a") {
						ref := strings.TrimSpace(attrVal(a, "href"))
						ref = strings.TrimPrefix(ref, "/")
						ref = stripAnchor(ref)
						if refPattern.MatchString(ref) {
							doc.AmendsRefs = appendUniq(doc.AmendsRefs, ref)
						}
					}
				case "miscInformation":
					// "Jf." links: related laws (see-also)
					for _, a := range findAll(n, "a") {
						ref := strings.TrimSpace(attrVal(a, "href"))
						ref = strings.TrimPrefix(ref, "/")
						ref = stripAnchor(ref)
						if refPattern.MatchString(ref) {
							doc.SeeAlsoRefs = appendUniq(doc.SeeAlsoRefs, ref)
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

	// ── Cross-references from <main class="documentBody"> ────────────────────
	refSet := make(map[string]bool)
	var walkRefs func(*html.Node)
	walkRefs = func(n *html.Node) {
		if n.Type == html.ElementNode && n.Data == "a" {
			href := strings.TrimPrefix(attrVal(n, "href"), "/")
			href = stripAnchor(href)
			if refPattern.MatchString(href) && href != doc.RefID {
				refSet[href] = true
			}
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			walkRefs(c)
		}
	}
	if main := findFirstWithAttr(root, "main", "class", "documentBody"); main != nil {
		walkRefs(main)
	}
	for ref := range refSet {
		doc.References = append(doc.References, ref)
	}

	if doc.RefID == "" {
		return nil
	}
	return doc
}

// ── EUR-Lex fetching ──────────────────────────────────────────────────────────

// directiveIDToCELEX converts a Lovdata-style directive ID like "2009/103/EF"
// to the CELEX number format "32009L0103".
// For directives (L prefix): CELEX = "3" + year + "L" + number (zero-padded to 4)
// For regulations (R prefix): CELEX = "3" + year + "R" + number
// Returns empty string if the format is not recognised.
func directiveIDToCELEX(id string) string {
	// Normalise: "2009/103/EF" → year=2009, num=103, type=L
	parts := strings.Split(strings.ToUpper(id), "/")
	if len(parts) < 3 {
		return ""
	}
	year := parts[0]
	numStr := parts[1]
	suffix := parts[len(parts)-1] // EF, EØF, EU, EEC, EC

	var typeChar string
	switch suffix {
	case "EF", "EØF", "EEC", "EC":
		// Could be directive or regulation — try directive first (L)
		typeChar = "L"
	case "EU":
		typeChar = "L"
	default:
		typeChar = "L"
	}

	num, err := strconv.Atoi(numStr)
	if err != nil {
		return ""
	}
	return fmt.Sprintf("3%s%s%04d", year, typeChar, num)
}

var httpClient = &http.Client{
	Timeout: 30 * time.Second,
	Transport: &http.Transport{
		MaxIdleConns:    10,
		IdleConnTimeout: 30 * time.Second,
	},
}

// fetchEUDirective fetches metadata for a single directive from EUR-Lex.
// It tries both directive (L) and regulation (R) CELEX forms.
func fetchEUDirective(ctx context.Context, directiveID string) (*EUDirective, error) {
	celexL := directiveIDToCELEX(directiveID)
	celexR := ""
	if celexL != "" {
		celexR = strings.Replace(celexL, "L", "R", 1)
	}

	// Try L (directive) first, then R (regulation)
	for _, celex := range []string{celexL, celexR} {
		if celex == "" {
			continue
		}
		dir, err := tryFetchEUDirective(ctx, directiveID, celex)
		if err == nil && dir != nil {
			return dir, nil
		}
	}
	return nil, fmt.Errorf("directive %s (CELEX: %s / %s): not found", directiveID, celexL, celexR)
}

func tryFetchEUDirective(ctx context.Context, directiveID, celex string) (*EUDirective, error) {
	fetchURL := eurLexBaseURL + celex
	req, err := http.NewRequestWithContext(ctx, "GET", fetchURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (compatible; LovdataSeeder/1.0)")
	req.Header.Set("Accept-Language", "en-US,en;q=0.9")

	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	return parseEURLexPage(directiveID, celex, body), nil
}

// parseEURLexPage extracts structured metadata from an EUR-Lex /ALL/ HTML page.
func parseEURLexPage(directiveID, celex string, content []byte) *EUDirective {
	dir := &EUDirective{
		DirectiveID: directiveID,
		CelexID:     celex,
	}

	text := string(content)

	// Full title from WT meta tag
	if m := regexp.MustCompile(`name="WT\.z_docTitle"\s+content="([^"]+)"`).FindStringSubmatch(text); m != nil {
		dir.FullTitle = m[1]
	}

	// EuroVoc IDs
	for _, m := range eurovocPattern.FindAllStringSubmatch(text, -1) {
		dir.EuroVocIDs = appendUniq(dir.EuroVocIDs, m[1])
	}

	// Structured metadata: parse <th>/<td> and <dt>/<dd> pairs
	root, err := html.Parse(strings.NewReader(text))
	if err == nil {
		var currentLabel string
		var walkMeta func(*html.Node)
		walkMeta = func(n *html.Node) {
			if n.Type == html.ElementNode {
				switch n.Data {
				case "th", "dt":
					currentLabel = strings.TrimSpace(textContent(n))
				case "td", "dd":
					val := strings.TrimSpace(textContent(n))
					val = strings.Join(strings.Fields(val), " ") // collapse whitespace
					switch {
					case strings.Contains(currentLabel, "Date of document"):
						if dir.DateOfDocument == "" {
							dir.DateOfDocument = val
						}
					case strings.Contains(currentLabel, "Date of effect"):
						if dir.DateOfEffect == "" && val != "" {
							// Take just the date part before semicolon
							if idx := strings.Index(val, ";"); idx > 0 {
								dir.DateOfEffect = strings.TrimSpace(val[:idx])
							} else {
								dir.DateOfEffect = val
							}
						}
					case strings.Contains(currentLabel, "Form"):
						if dir.Form == "" {
							dir.Form = val
						}
					case strings.Contains(currentLabel, "Author"):
						if dir.Author == "" {
							dir.Author = val
						}
					case strings.Contains(currentLabel, "Responsible body"):
						if dir.ResponsibleDG == "" {
							dir.ResponsibleDG = val
						}
					case strings.Contains(currentLabel, "Subject matter"):
						if dir.SubjectMatter == "" {
							dir.SubjectMatter = val
						}
					case strings.Contains(currentLabel, "Directory code"):
						if dir.DirectoryCode == "" {
							dir.DirectoryCode = val
						}
					case strings.Contains(currentLabel, "Legal basis"):
						if dir.LegalBasis == "" {
							dir.LegalBasis = val
						}
					case strings.Contains(currentLabel, "Procedure number"):
						if dir.ProcedureNum == "" {
							dir.ProcedureNum = val
						}
					case strings.Contains(currentLabel, "Instruments cited"):
						for _, m := range celexPattern.FindAllString(val, -1) {
							dir.CitedCELEX = appendUniq(dir.CitedCELEX, m)
						}
					case strings.Contains(currentLabel, "Modified by"):
						for _, m := range celexPattern.FindAllString(val, -1) {
							dir.ModifiedByCELEX = appendUniq(dir.ModifiedByCELEX, m)
						}
					}
				}
			}
			for c := n.FirstChild; c != nil; c = c.NextSibling {
				walkMeta(c)
			}
		}
		walkMeta(root)
	}

	// Extract OJ reference from the page text
	if m := regexp.MustCompile(`OJ [A-Z]+ \d+[^<\n]{3,40}`).FindString(text); m != "" {
		dir.OJReference = strings.TrimSpace(m)
	}

	// Derive short title from full title (everything up to first "(")
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

// ── EuroVoc SPARQL fetching ───────────────────────────────────────────────────

// fetchEuroVocLabels fetches English labels for a batch of EuroVoc concept IDs
// via the EU Publications Office SPARQL endpoint. Returns id→label map.
func fetchEuroVocLabels(ctx context.Context, ids []string) (map[string]string, error) {
	if len(ids) == 0 {
		return nil, nil
	}

	// Build VALUES clause
	values := make([]string, len(ids))
	for i, id := range ids {
		values[i] = fmt.Sprintf("<http://eurovoc.europa.eu/%s>", id)
	}

	query := fmt.Sprintf(`
PREFIX skos: <http://www.w3.org/2004/02/skos/core#>
SELECT ?id ?label WHERE {
  VALUES ?id { %s }
  ?id skos:prefLabel ?label .
  FILTER(LANG(?label) = "en")
}`, strings.Join(values, " "))

	formData := url.Values{"query": {query}}
	req, err := http.NewRequestWithContext(ctx, "POST", eurovocSPARQL, strings.NewReader(formData.Encode()))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/sparql-results+json")

	resp, err := httpClient.Do(req)
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
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, err
	}

	labels := make(map[string]string)
	for _, b := range result.Results.Bindings {
		// Extract numeric ID from full URI "http://eurovoc.europa.eu/730"
		uri := b.ID.Value
		if idx := strings.LastIndex(uri, "/"); idx >= 0 {
			labels[uri[idx+1:]] = b.Label.Value
		}
	}
	return labels, nil
}

// ── Lovdata data loading ──────────────────────────────────────────────────────

func downloadDataset(url string) (string, error) {
	os.MkdirAll(cacheDir, 0755)
	filename := url[strings.LastIndex(url, "/")+1:]
	localPath := filepath.Join(cacheDir, filename)

	if _, err := os.Stat(localPath); os.IsNotExist(err) {
		log.Printf("Downloading %s ...", url)
		resp, err := http.Get(url)
		if err != nil {
			return "", fmt.Errorf("download %s: %w", url, err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			return "", fmt.Errorf("download %s: HTTP %d", url, resp.StatusCode)
		}
		out, err := os.Create(localPath)
		if err != nil {
			return "", err
		}
		_, err = io.Copy(out, resp.Body)
		out.Close()
		if err != nil {
			return "", err
		}
		log.Printf("Saved %s", localPath)
	} else {
		log.Printf("Using cached %s", localPath)
	}
	return localPath, nil
}

func parseTarBz2(path string, fn func(name string, content []byte)) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()

	bzReader := bzip2.NewReader(f)
	tarReader := tar.NewReader(bzReader)

	for {
		header, err := tarReader.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return fmt.Errorf("tar read: %w", err)
		}
		if header.Typeflag != tar.TypeReg || !strings.HasSuffix(header.Name, ".xml") {
			continue
		}
		content, err := io.ReadAll(tarReader)
		if err != nil {
			return fmt.Errorf("read member %s: %w", header.Name, err)
		}
		fn(header.Name, content)
	}
	return nil
}

func loadDataset(dataURL string, docType DocType, limit int) ([]LovdataDoc, error) {
	path, err := downloadDataset(dataURL)
	if err != nil {
		return nil, err
	}

	var docs []LovdataDoc
	var mu sync.Mutex
	limitReached := false

	err = parseTarBz2(path, func(name string, content []byte) {
		mu.Lock()
		if limitReached {
			mu.Unlock()
			return
		}
		mu.Unlock()

		doc := parseDocument(content, docType)
		if doc == nil {
			return
		}

		mu.Lock()
		defer mu.Unlock()
		if limit > 0 && len(docs) >= limit {
			limitReached = true
			return
		}
		docs = append(docs, *doc)
	})
	return docs, err
}

// ── EU data fetching ──────────────────────────────────────────────────────────

// fetchAllEUData fetches EUR-Lex metadata and EuroVoc labels for all unique
// directive IDs found across the loaded Lovdata documents.
func fetchAllEUData(ctx context.Context, docs []LovdataDoc, limit int) ([]*EUDirective, []*EuroVocConcept) {
	// Collect all unique directive IDs
	allDirectiveIDs := make(map[string]bool)
	for _, d := range docs {
		for _, did := range d.EUDirectiveIDs {
			allDirectiveIDs[did] = true
		}
	}

	ids := make([]string, 0, len(allDirectiveIDs))
	for id := range allDirectiveIDs {
		ids = append(ids, id)
	}

	if limit > 0 && len(ids) > limit {
		ids = ids[:limit]
	}

	log.Printf("Fetching %d EU directives from EUR-Lex...", len(ids))

	// Fetch directives concurrently (but gently — EUR-Lex rate limits)
	sem := make(chan struct{}, 5) // max 5 concurrent EUR-Lex requests
	var mu sync.Mutex
	var directives []*EUDirective
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

			dir, err := fetchEUDirective(ctx, did)
			if err != nil {
				log.Printf("  [EUR-Lex] %s: %v", did, err)
				// Still create a minimal node from what we know
				dir = &EUDirective{
					DirectiveID: did,
					CelexID:     directiveIDToCELEX(did),
				}
			} else {
				log.Printf("  [EUR-Lex] ✓ %s → %s: %s", did, dir.CelexID, truncate(dir.FullTitle, 60))
			}

			mu.Lock()
			directives = append(directives, dir)
			mu.Unlock()

			// Gentle rate limiting
			time.Sleep(200 * time.Millisecond)
		}(id)
	}
	wg.Wait()

	// Collect all unique EuroVoc IDs
	allEuroVocIDs := make(map[string]bool)
	for _, dir := range directives {
		for _, evID := range dir.EuroVocIDs {
			allEuroVocIDs[evID] = true
		}
	}

	evIDs := make([]string, 0, len(allEuroVocIDs))
	for id := range allEuroVocIDs {
		evIDs = append(evIDs, id)
	}

	log.Printf("Fetching %d EuroVoc concept labels via SPARQL...", len(evIDs))

	// Fetch in batches of 50 (SPARQL VALUES clause limit)
	evLabels := make(map[string]string)
	for i := 0; i < len(evIDs); i += 50 {
		end := i + 50
		if end > len(evIDs) {
			end = len(evIDs)
		}
		batch := evIDs[i:end]
		labels, err := fetchEuroVocLabels(ctx, batch)
		if err != nil {
			log.Printf("  [EuroVoc SPARQL] batch %d-%d error: %v", i, end, err)
			continue
		}
		for id, label := range labels {
			evLabels[id] = label
		}
		time.Sleep(100 * time.Millisecond)
	}

	var concepts []*EuroVocConcept
	for id, label := range evLabels {
		concepts = append(concepts, &EuroVocConcept{ID: id, LabelEN: label})
	}
	log.Printf("Fetched labels for %d/%d EuroVoc concepts", len(concepts), len(evIDs))

	return directives, concepts
}

// ── Graph ingestion: objects ──────────────────────────────────────────────────

func ingestObjects(ctx context.Context, client *graph.Client, docs []LovdataDoc, directives []*EUDirective, concepts []*EuroVocConcept) map[string]string {
	var items []graph.CreateObjectRequest

	// ── Ministry nodes ────────────────────────────────────────────────────────
	ministries := make(map[string]bool)
	for _, d := range docs {
		if d.Ministry != "" {
			ministries[d.Ministry] = true
		}
	}
	for ministry := range ministries {
		k := "ministry_" + ministry
		items = append(items, graph.CreateObjectRequest{
			Type: "Ministry", Key: strPtr(k),
			Properties: map[string]any{"name": ministry},
		})
	}

	// ── LegalArea nodes (top-level) ───────────────────────────────────────────
	legalAreas := make(map[string]bool)
	for _, d := range docs {
		for _, a := range d.AllLegalAreas {
			legalAreas[a] = true
		}
	}
	for area := range legalAreas {
		k := "area_" + area
		items = append(items, graph.CreateObjectRequest{
			Type: "LegalArea", Key: strPtr(k),
			Properties: map[string]any{"name": area},
		})
	}

	// ── LegalArea nodes (sub-areas) ───────────────────────────────────────────
	legalSubAreas := make(map[string]string) // subArea → parentArea
	for _, d := range docs {
		// Use the first top-level area as parent for all sub-areas of this doc
		parent := d.LegalArea
		for _, sa := range d.AllLegalSubAreas {
			if _, exists := legalSubAreas[sa]; !exists {
				legalSubAreas[sa] = parent
			}
		}
	}
	for subArea, parentArea := range legalSubAreas {
		k := "subarea_" + subArea
		items = append(items, graph.CreateObjectRequest{
			Type: "LegalArea", Key: strPtr(k),
			Properties: map[string]any{"name": subArea, "parent_area": parentArea},
		})
	}

	// ── Law / Regulation nodes ────────────────────────────────────────────────
	for _, d := range docs {
		k := d.RefID
		props := map[string]any{
			"name":   d.Title,
			"title":  d.Title,
			"ref_id": d.RefID,
			"doc_id": d.DocID,
		}
		if d.LegacyID != "" {
			props["legacy_id"] = d.LegacyID
		}
		if d.ShortTitle != "" {
			props["short_title"] = d.ShortTitle
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
		items = append(items, graph.CreateObjectRequest{
			Type: string(d.Type), Key: strPtr(k), Properties: props,
		})
	}

	// ── EUDirective nodes ─────────────────────────────────────────────────────
	for _, dir := range directives {
		k := "eu_" + dir.CelexID
		props := map[string]any{
			"name":         dir.ShortTitle,
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
		if dir.ResponsibleDG != "" {
			props["responsible_dg"] = dir.ResponsibleDG
		}
		if dir.SubjectMatter != "" {
			props["subject_matter"] = dir.SubjectMatter
		}
		if dir.DirectoryCode != "" {
			props["directory_code"] = dir.DirectoryCode
		}
		if dir.LegalBasis != "" {
			props["legal_basis"] = dir.LegalBasis
		}
		if dir.ProcedureNum != "" {
			props["procedure_number"] = dir.ProcedureNum
		}
		if dir.OJReference != "" {
			props["oj_reference"] = dir.OJReference
		}
		items = append(items, graph.CreateObjectRequest{
			Type: "EUDirective", Key: strPtr(k), Properties: props,
		})
	}

	// ── EuroVocConcept nodes ──────────────────────────────────────────────────
	for _, ev := range concepts {
		k := "eurovoc_" + ev.ID
		items = append(items, graph.CreateObjectRequest{
			Type: "EuroVocConcept", Key: strPtr(k),
			Properties: map[string]any{
				"name":       ev.LabelEN,
				"eurovoc_id": ev.ID,
				"label_en":   ev.LabelEN,
			},
		})
	}

	log.Printf("Uploading %d graph objects total...", len(items))
	return bulkUploadObjects(ctx, client, items)
}

// ── Graph ingestion: relationships ────────────────────────────────────────────

func ingestRelationships(ctx context.Context, client *graph.Client, docs []LovdataDoc, directives []*EUDirective, idMap map[string]string) {
	log.Println("Building relationships...")
	var items []graph.CreateRelationshipRequest

	// Index of known refIDs (limits REFERENCES edges to seeded docs only)
	knownRefs := make(map[string]bool, len(docs))
	for _, d := range docs {
		knownRefs[d.RefID] = true
	}

	// Index of directiveID → EUDirective for IMPLEMENTS_EEA lookup
	directiveByID := make(map[string]*EUDirective, len(directives))
	for _, dir := range directives {
		directiveByID[dir.DirectiveID] = dir
		// Also index by normalised versions (EF = EøF = EC etc.)
		directiveByID[strings.Replace(dir.DirectiveID, "EF", "EU", 1)] = dir
	}

	for _, d := range docs {
		src, ok := idMap[d.RefID]
		if !ok {
			continue
		}

		// ── ADMINISTERED_BY → Ministry ────────────────────────────────────────
		if d.Ministry != "" {
			if dst, ok2 := idMap["ministry_"+d.Ministry]; ok2 {
				items = append(items, graph.CreateRelationshipRequest{
					Type: "ADMINISTERED_BY", SrcID: src, DstID: dst,
					Properties: map[string]any{},
				})
			}
		}

		// ── IN_LEGAL_AREA → all top-level LegalArea nodes ────────────────────
		for _, area := range d.AllLegalAreas {
			if dst, ok2 := idMap["area_"+area]; ok2 {
				items = append(items, graph.CreateRelationshipRequest{
					Type: "IN_LEGAL_AREA", SrcID: src, DstID: dst,
					Properties: map[string]any{},
				})
			}
		}

		// ── IN_LEGAL_AREA → all sub-area LegalArea nodes ─────────────────────
		for _, sub := range d.AllLegalSubAreas {
			if dst, ok2 := idMap["subarea_"+sub]; ok2 {
				items = append(items, graph.CreateRelationshipRequest{
					Type: "IN_LEGAL_AREA", SrcID: src, DstID: dst,
					Properties: map[string]any{"level": "sub"},
				})
			}
		}

		// ── AMENDED_BY → last amending act ───────────────────────────────────
		if d.LastChangedByRef != "" {
			if dst, ok2 := idMap[d.LastChangedByRef]; ok2 {
				items = append(items, graph.CreateRelationshipRequest{
					Type: "AMENDED_BY", SrcID: src, DstID: dst,
					Properties: map[string]any{"effective_date": d.LastChangeInForce},
				})
			}
		}

		// ── AMENDS → documents this law explicitly changed ────────────────────
		for _, ref := range d.AmendsRefs {
			if dst, ok2 := idMap[ref]; ok2 {
				items = append(items, graph.CreateRelationshipRequest{
					Type: "AMENDS", SrcID: src, DstID: dst,
					Properties: map[string]any{},
				})
			}
		}

		// ── SEE_ALSO → Jf. related documents ─────────────────────────────────
		for _, ref := range d.SeeAlsoRefs {
			if !knownRefs[ref] {
				continue
			}
			if dst, ok2 := idMap[ref]; ok2 {
				items = append(items, graph.CreateRelationshipRequest{
					Type: "SEE_ALSO", SrcID: src, DstID: dst,
					Properties: map[string]any{},
				})
			}
		}

		// ── REFERENCES → body cross-references ───────────────────────────────
		for _, ref := range d.References {
			if !knownRefs[ref] {
				continue
			}
			if dst, ok2 := idMap[ref]; ok2 {
				items = append(items, graph.CreateRelationshipRequest{
					Type: "REFERENCES", SrcID: src, DstID: dst,
					Properties: map[string]any{},
				})
			}
		}

		// ── IMPLEMENTS_EEA → EUDirective nodes ───────────────────────────────
		for _, did := range d.EUDirectiveIDs {
			dir := directiveByID[did]
			if dir == nil {
				continue
			}
			euKey := "eu_" + dir.CelexID
			if dst, ok2 := idMap[euKey]; ok2 {
				items = append(items, graph.CreateRelationshipRequest{
					Type: "IMPLEMENTS_EEA", SrcID: src, DstID: dst,
					Properties: map[string]any{"directive_id": did},
				})
			}
		}
	}

	// ── HAS_LANGUAGE_VARIANT: link Nynorsk ↔ Bokmål versions ─────────────────
	// Laws with language="nn" share the same refID as their Bokmål counterpart
	// but with a "-nn" suffix in the filename. Both have the same RefID so we
	// link by finding duplicate RefIDs with different languages.
	refsByID := make(map[string][]LovdataDoc)
	for _, d := range docs {
		refsByID[d.RefID] = append(refsByID[d.RefID], d)
	}
	for _, group := range refsByID {
		if len(group) < 2 {
			continue
		}
		// Link all pairs in the group
		for i := 0; i < len(group); i++ {
			for j := i + 1; j < len(group); j++ {
				src, ok1 := idMap[group[i].RefID]
				dst, ok2 := idMap[group[j].RefID]
				if ok1 && ok2 && src != dst {
					items = append(items, graph.CreateRelationshipRequest{
						Type: "HAS_LANGUAGE_VARIANT", SrcID: src, DstID: dst,
						Properties: map[string]any{
							"source_language": group[i].Language,
							"target_language": group[j].Language,
						},
					})
				}
			}
		}
	}

	// ── EU_CITES: EUDirective → EUDirective (instruments cited) ──────────────
	for _, dir := range directives {
		src, ok := idMap["eu_"+dir.CelexID]
		if !ok {
			continue
		}
		for _, citedCELEX := range dir.CitedCELEX {
			citedKey := "eu_" + citedCELEX
			if dst, ok2 := idMap[citedKey]; ok2 {
				items = append(items, graph.CreateRelationshipRequest{
					Type: "EU_CITES", SrcID: src, DstID: dst,
					Properties: map[string]any{},
				})
			}
		}

		// ── EU_MODIFIED_BY: EUDirective → EUDirective (amendments) ───────────
		for _, modCELEX := range dir.ModifiedByCELEX {
			modKey := "eu_" + modCELEX
			if dst, ok2 := idMap[modKey]; ok2 {
				items = append(items, graph.CreateRelationshipRequest{
					Type: "EU_MODIFIED_BY", SrcID: src, DstID: dst,
					Properties: map[string]any{},
				})
			}
		}

		// ── HAS_EUROVOC_DESCRIPTOR: EUDirective → EuroVocConcept ─────────────
		for _, evID := range dir.EuroVocIDs {
			evKey := "eurovoc_" + evID
			if dst, ok2 := idMap[evKey]; ok2 {
				items = append(items, graph.CreateRelationshipRequest{
					Type: "HAS_EUROVOC_DESCRIPTOR", SrcID: src, DstID: dst,
					Properties: map[string]any{},
				})
			}
		}
	}

	log.Printf("Uploading %d relationships...", len(items))
	bulkUploadRelationships(ctx, client, items)
}

// ── Bulk upload helpers ───────────────────────────────────────────────────────

func bulkUploadObjects(ctx context.Context, client *graph.Client, items []graph.CreateObjectRequest) map[string]string {
	type batchResult struct {
		batch []graph.CreateObjectRequest
		res   *graph.BulkCreateObjectsResponse
	}

	batches := make(chan []graph.CreateObjectRequest, numWorkers*2)
	results := make(chan batchResult, numWorkers*2)

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
					log.Printf("  [objects] batch error: %v — retrying", err)
					time.Sleep(500 * time.Millisecond)
					res, _ = client.BulkCreateObjects(ctx, &graph.BulkCreateObjectsRequest{Items: batch})
				}
				results <- batchResult{batch, res}
			}
		}()
	}
	go func() { wg.Wait(); close(results) }()

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

	idMap := make(map[string]string)
	var mu sync.Mutex
	var uploaded, conflicts, failed atomic.Int64
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
		if n%5000 == 0 && n > 0 {
			log.Printf("  ...%d/%d objects processed (new=%d conflicts=%d idMap=%d)",
				n, len(items), uploaded.Load(), conflicts.Load(), len(idMap))
		}
	}

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
		log.Printf("  Conflict resolution complete — idMap has %d entries", len(idMap))
	}

	log.Printf("  Objects complete: %d new, %d conflicts resolved, %d errors, %d total mapped",
		uploaded.Load(), conflicts.Load(), failed.Load(), len(idMap))
	return idMap
}

func bulkUploadRelationships(ctx context.Context, client *graph.Client, items []graph.CreateRelationshipRequest) {
	relsDone := loadRelsDone()

	var batches [][]graph.CreateRelationshipRequest
	for i := 0; i < len(items); i += batchSize {
		end := i + batchSize
		if end > len(items) {
			end = len(items)
		}
		batches = append(batches, items[i:end])
	}

	totalBatches := len(batches)
	log.Printf("Relationship upload: %d total batches, %d already done, %d remaining",
		totalBatches, len(relsDone), totalBatches-len(relsDone))

	type workItem struct {
		idx   int
		batch []graph.CreateRelationshipRequest
	}
	work := make(chan workItem, numWorkers*2)
	var wg sync.WaitGroup
	var succeeded, failed, skippedCount atomic.Int64

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
					log.Printf("  [rels] batch %d error: %v — retrying", wi.idx, err)
					time.Sleep(500 * time.Millisecond)
					res, err = client.BulkCreateRelationships(ctx, &graph.BulkCreateRelationshipsRequest{Items: wi.batch})
				}
				if err != nil || res == nil {
					log.Printf("  [rels] batch %d failed permanently", wi.idx)
					appendRelFailed(wi.batch)
					failed.Add(int64(len(wi.batch)))
					continue
				}
				batchFailed := false
				for _, r := range res.Results {
					if !r.Success && r.Error != nil {
						log.Printf("  [rels] batch %d item error: %s", wi.idx, *r.Error)
						batchFailed = true
					}
				}
				if batchFailed {
					appendRelFailed(wi.batch)
					failed.Add(int64(res.Failed))
				} else {
					appendRelDone(wi.idx)
					succeeded.Add(int64(res.Success))
				}
				n := succeeded.Load() + failed.Load() + skippedCount.Load()
				if n%100 == 0 && n > 0 {
					log.Printf("  ...%d/%d batches done (ok=%d fail=%d skip=%d)",
						n, totalBatches, succeeded.Load(), failed.Load(), skippedCount.Load())
				}
			}
		}()
	}

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
					appendRelFailed(wi.batch)
					failed.Add(int64(len(wi.batch)))
					continue
				}
				batchFailed := false
				for _, r := range res.Results {
					if !r.Success && r.Error != nil {
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

// ── HTML utilities ────────────────────────────────────────────────────────────

func attrVal(n *html.Node, key string) string {
	for _, a := range n.Attr {
		if a.Key == key {
			return a.Val
		}
	}
	return ""
}

func textContent(n *html.Node) string {
	var b strings.Builder
	var walk func(*html.Node)
	walk = func(node *html.Node) {
		if node.Type == html.TextNode {
			b.WriteString(node.Data)
		}
		for c := node.FirstChild; c != nil; c = c.NextSibling {
			walk(c)
		}
	}
	walk(n)
	return b.String()
}

func findFirst(n *html.Node, tag string) *html.Node {
	if n.Type == html.ElementNode && n.Data == tag {
		return n
	}
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		if found := findFirst(c, tag); found != nil {
			return found
		}
	}
	return nil
}

func findAll(n *html.Node, tag string) []*html.Node {
	var results []*html.Node
	var walk func(*html.Node)
	walk = func(node *html.Node) {
		if node.Type == html.ElementNode && node.Data == tag {
			results = append(results, node)
		}
		for c := node.FirstChild; c != nil; c = c.NextSibling {
			walk(c)
		}
	}
	walk(n)
	return results
}

func findFirstWithAttr(n *html.Node, tag, attrKey, attrValue string) *html.Node {
	if n.Type == html.ElementNode && n.Data == tag {
		for _, a := range n.Attr {
			if a.Key == attrKey && strings.Contains(a.Val, attrValue) {
				return n
			}
		}
	}
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		if found := findFirstWithAttr(c, tag, attrKey, attrValue); found != nil {
			return found
		}
	}
	return nil
}

// ── General utilities ─────────────────────────────────────────────────────────

func strPtr(s string) *string { return &s }

func appendUniq(s []string, v string) []string {
	for _, x := range s {
		if x == v {
			return s
		}
	}
	return append(s, v)
}

func stripAnchor(href string) string {
	// Strip "§5" section anchors: "lov/1997-02-28-19/§5" → "lov/1997-02-28-19"
	if idx := strings.Index(href, "/§"); idx > 0 {
		return href[:idx]
	}
	return href
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}

// ── Main ──────────────────────────────────────────────────────────────────────

func main() {
	os.MkdirAll(stateDir, 0755)

	serverURL := os.Getenv("SERVER_URL")
	if serverURL == "" {
		serverURL = "http://localhost:3002"
	}
	apiKey := os.Getenv("API_KEY")
	if apiKey == "" {
		apiKey = "emt_ec70233facfa29385abfef9bff015df72f08f7205be51f3034b42bf1484d0ec1"
	}
	projectID := os.Getenv("PROJECT_ID") // optional: auto-created if blank

	limit := 0
	euLimit := 0 // max EU directives to fetch (0 = all)
	if v := os.Getenv("DRY_RUN"); v == "true" || v == "1" {
		limit = 20
		euLimit = 5
		log.Println("DRY_RUN: limiting to 20 docs per dataset, 5 EU directives")
	} else if v := os.Getenv("SEED_LIMIT"); v != "" {
		limit, _ = strconv.Atoi(v)
		log.Printf("SEED_LIMIT: %d docs per dataset", limit)
	}

	dataset := os.Getenv("DATASET")
	if dataset == "" {
		dataset = "both"
	}

	skipEU := os.Getenv("SKIP_EU") == "true" || os.Getenv("SKIP_EU") == "1"
	retryFailed := os.Getenv("RETRY_FAILED") == "true" || os.Getenv("RETRY_FAILED") == "1"

	client, err := sdk.New(sdk.Config{
		ServerURL:  serverURL,
		ProjectID:  projectID,
		HTTPClient: &http.Client{Timeout: 5 * time.Minute},
		Auth:       sdk.AuthConfig{Mode: "apikey", APIKey: apiKey},
	})
	if err != nil {
		log.Fatalf("Failed to create SDK client: %v", err)
	}

	// Resolve project ID — find/create "Norwegian Law" project if not specified.
	orgID := os.Getenv("ORG_ID")
	if orgID == "" {
		orgID = defaultOrgID
	}
	if projectID == "" {
		ctx0 := context.Background()
		projectID, err = ensureProject(ctx0, client, orgID)
		if err != nil {
			log.Fatalf("Failed to resolve project: %v", err)
		}
		// Rebuild client with correct project ID
		client, err = sdk.New(sdk.Config{
			ServerURL:  serverURL,
			ProjectID:  projectID,
			HTTPClient: &http.Client{Timeout: 5 * time.Minute},
			Auth:       sdk.AuthConfig{Mode: "apikey", APIKey: apiKey},
		})
		if err != nil {
			log.Fatalf("Failed to recreate SDK client: %v", err)
		}
	}

	log.Printf("Starting Lovdata+EUR-Lex Graph Seeder → %s (project: %s)", serverURL, projectID)
	log.Printf("Dataset: %s | Skip EU: %v | State dir: %s", dataset, skipEU, stateDir)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		sig := <-sigCh
		log.Printf("Received %s — shutting down gracefully (state preserved)", sig)
		cancel()
	}()

	if retryFailed {
		log.Println("RETRY_FAILED: replaying rels_failed.jsonl only")
		failedBatches := loadRelsFailed()
		if len(failedBatches) == 0 {
			log.Println("No failed batches.")
			return
		}
		os.Remove(filepath.Join(stateDir, "rels_failed.jsonl"))
		retryRelationshipBatches(ctx, client.Graph, failedBatches)
		return
	}

	state := loadState()
	log.Printf("Resuming from phase: %s", state.Phase)

	var idMap map[string]string

	if state.Phase == phaseObjectsPending {
		// ── Load Lovdata documents ────────────────────────────────────────────
		var allDocs []LovdataDoc

		if dataset == "laws" || dataset == "both" {
			log.Println("Loading laws dataset...")
			docs, err := loadDataset(lawsURL, DocTypeLaw, limit)
			if err != nil {
				log.Fatalf("Failed to load laws: %v", err)
			}
			log.Printf("Parsed %d laws", len(docs))
			allDocs = append(allDocs, docs...)
		}
		if dataset == "regulations" || dataset == "both" {
			log.Println("Loading regulations dataset...")
			docs, err := loadDataset(regulationsURL, DocTypeRegulation, limit)
			if err != nil {
				log.Fatalf("Failed to load regulations: %v", err)
			}
			log.Printf("Parsed %d regulations", len(docs))
			allDocs = append(allDocs, docs...)
		}
		log.Printf("Total Lovdata documents: %d", len(allDocs))

		// ── Fetch EU data ─────────────────────────────────────────────────────
		var directives []*EUDirective
		var concepts []*EuroVocConcept
		if !skipEU {
			directives, concepts = fetchAllEUData(ctx, allDocs, euLimit)
			log.Printf("EU data: %d directives, %d EuroVoc concepts", len(directives), len(concepts))
		}

		// ── Ingest objects ────────────────────────────────────────────────────
		idMap = ingestObjects(ctx, client.Graph, allDocs, directives, concepts)

		if ctx.Err() != nil {
			log.Println("Interrupted during object phase — state NOT advanced")
			return
		}

		log.Println("Saving idmap.json...")
		saveIDMap(idMap)
		state.Phase = phaseRelsPending
		saveState(state)
		log.Printf("Object phase complete — %d entries in idMap", len(idMap))

		// ── Ingest relationships ───────────────────────────────────────────────
		ingestRelationships(ctx, client.Graph, allDocs, directives, idMap)

	} else if state.Phase == phaseObjectsDone || state.Phase == phaseRelsPending {
		log.Println("Object phase already done — loading idmap.json")
		idMap, err = loadIDMap()
		if err != nil {
			log.Fatalf("Failed to load idmap.json: %v — delete state dir and restart", err)
		}
		log.Printf("Loaded idMap: %d entries", len(idMap))

		var allDocs []LovdataDoc
		if dataset == "laws" || dataset == "both" {
			docs, err := loadDataset(lawsURL, DocTypeLaw, limit)
			if err != nil {
				log.Fatalf("Failed to load laws: %v", err)
			}
			allDocs = append(allDocs, docs...)
		}
		if dataset == "regulations" || dataset == "both" {
			docs, err := loadDataset(regulationsURL, DocTypeRegulation, limit)
			if err != nil {
				log.Fatalf("Failed to load regulations: %v", err)
			}
			allDocs = append(allDocs, docs...)
		}

		var directives []*EUDirective
		if !skipEU {
			directives, _ = fetchAllEUData(ctx, allDocs, euLimit)
		}

		ingestRelationships(ctx, client.Graph, allDocs, directives, idMap)

	} else if state.Phase == phaseDone {
		log.Printf("Seeding already complete. Delete %s to re-seed.", stateDir)
		return
	}

	if ctx.Err() != nil {
		log.Println("Interrupted — state preserved for resume.")
		return
	}

	state.Phase = phaseDone
	saveState(state)
	log.Println("Lovdata + EUR-Lex seeding complete!")
}
