package checkcmd

import (
	"context"
	"fmt"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/emergent-company/emergent.memory/apps/server/pkg/sdk/graph"
	"github.com/emergent-company/emergent.memory/blueprints/code-memory-blueprint/cmd/codebase/internal/config"
	"github.com/emergent-company/emergent.memory/blueprints/code-memory-blueprint/cmd/codebase/internal/output"
	"github.com/spf13/cobra"
)

type coverageReport struct {
	Domain      string   `json:"domain"`
	Services    []string `json:"services"`
	Methods     int      `json:"methods"`
	Endpoints   int      `json:"endpoints"`
	TestSuites  int      `json:"test_suites"`
	CoveragePct float64  `json:"coverage_pct"`
	RiskScore   int      `json:"risk_score"`
	Notes       string   `json:"notes"`
}

func newCoverageCmd(flagProjectID *string, flagBranch *string, flagFormat *string) *cobra.Command {
	var flagDomain string
	var flagMinCoverage int
	var flagSort string
	var flagFailOnRisk bool

	cmd := &cobra.Command{
		Use:   "coverage",
		Short: "Audit test coverage and risk across domains",
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := config.New(*flagProjectID, *flagBranch)
			if err != nil {
				return err
			}

			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
			defer cancel()

			// Fetch data
			listAll := func(typ string) []*graph.GraphObject {
				resp, _ := c.Graph.ListObjects(ctx, &graph.ListObjectsOptions{Type: typ})
				if resp == nil {
					return nil
				}
				return resp.Items
			}
			listAllRels := func(typ string) []*graph.GraphRelationship {
				resp, _ := c.Graph.ListRelationships(ctx, &graph.ListRelationshipsOptions{Type: typ})
				if resp == nil {
					return nil
				}
				return resp.Items
			}

			services := listAll("Service")
			methods := listAll("Method")
			endpoints := listAll("APIEndpoint")
			jobs := listAll("Job")
			testedByRels := listAllRels("tested_by")

			// Indexing
			testedByMap := make(map[string][]string)
			for _, r := range testedByRels {
				testedByMap[r.SrcID] = append(testedByMap[r.SrcID], r.DstID)
			}

			reports := make(map[string]*coverageReport)
			getReport := func(domain string) *coverageReport {
				domain = strings.ToLower(domain)
				if r, ok := reports[domain]; ok {
					return r
				}
				r := &coverageReport{Domain: domain}
				reports[domain] = r
				return r
			}

			for _, s := range services {
				d := getDomain(s)
				if flagDomain != "" && !strings.EqualFold(d, flagDomain) {
					continue
				}
				r := getReport(d)
				r.Services = append(r.Services, strProp(s, "name"))
				if tsIDs, ok := testedByMap[s.EntityID]; ok {
					r.TestSuites += len(tsIDs)
					r.CoveragePct = 100
				}
			}

			for _, m := range methods {
				d := getDomain(m)
				if flagDomain != "" && !strings.EqualFold(d, flagDomain) {
					continue
				}
				getReport(d).Methods++
			}

			for _, ep := range endpoints {
				d := getDomain(ep)
				if flagDomain != "" && !strings.EqualFold(d, flagDomain) {
					continue
				}
				getReport(d).Endpoints++
			}

			jobCount := make(map[string]int)
			for _, j := range jobs {
				d := getDomain(j)
				jobCount[d]++
			}

			var finalReports []*coverageReport
			for _, r := range reports {
				score := 0
				if r.CoveragePct < 100 {
					score += 40
				}
				if r.Methods >= 10 {
					score += 50
				} else if r.Methods >= 5 {
					score += 30
				}
				if jobCount[r.Domain] > 0 {
					score += 10
				}
				if score > 100 {
					score = 100
				}
				r.RiskScore = score

				if r.CoveragePct >= float64(flagMinCoverage) {
					finalReports = append(finalReports, r)
				}
			}

			sort.Slice(finalReports, func(i, j int) bool {
				switch flagSort {
				case "coverage":
					return finalReports[i].CoveragePct < finalReports[j].CoveragePct
				case "methods":
					return finalReports[i].Methods > finalReports[j].Methods
				case "risk":
					return finalReports[i].RiskScore > finalReports[j].RiskScore
				default:
					return finalReports[i].Domain < finalReports[j].Domain
				}
			})

			if *flagFormat == "json" {
				return output.JSON(finalReports)
			}

			headers := []string{"DOMAIN", "SERVICES", "METHODS", "ENDPOINTS", "TESTS", "COVERAGE", "RISK", "NOTES"}
			var rows [][]string
			for _, r := range finalReports {
				rows = append(rows, []string{
					r.Domain,
					fmt.Sprintf("%d", len(r.Services)),
					fmt.Sprintf("%d", r.Methods),
					fmt.Sprintf("%d", r.Endpoints),
					fmt.Sprintf("%d", r.TestSuites),
					fmt.Sprintf("%.0f%%", r.CoveragePct),
					fmt.Sprintf("%d", r.RiskScore),
					r.Notes,
				})
			}
			output.Table(headers, rows)

			if flagFailOnRisk {
				for _, r := range finalReports {
					if r.RiskScore >= 60 && r.CoveragePct < 50 {
						os.Exit(2)
					}
				}
			}

			return nil
		},
	}

	cmd.Flags().StringVar(&flagDomain, "domain", "", "Filter to a specific domain")
	cmd.Flags().IntVar(&flagMinCoverage, "min-coverage", 0, "Only show below this %")
	cmd.Flags().StringVar(&flagSort, "sort", "domain", "Sort by: domain, coverage, methods, risk")
	cmd.Flags().BoolVar(&flagFailOnRisk, "fail-on-risk", false, "Exit 2 if high-risk untested domains found")
	return cmd
}

func getDomain(o *graph.GraphObject) string {
	if d, ok := o.Properties["domain"].(string); ok && d != "" {
		return d
	}
	name := strProp(o, "name")
	return strings.ToLower(strings.TrimSuffix(name, "Service"))
}
