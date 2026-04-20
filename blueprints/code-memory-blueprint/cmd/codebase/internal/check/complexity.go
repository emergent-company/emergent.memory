package checkcmd

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/emergent-company/emergent.memory/apps/server/pkg/sdk/graph"
	"github.com/emergent-company/emergent.memory/blueprints/code-memory-blueprint/cmd/codebase/internal/config"
	"github.com/emergent-company/emergent.memory/blueprints/code-memory-blueprint/cmd/codebase/internal/output"
	"github.com/spf13/cobra"
)

type complexityReport struct {
	Domain       string   `json:"domain"`
	Endpoints    int      `json:"endpoints"`
	Methods      int      `json:"methods"`
	SQLQueries   int      `json:"sql_queries"`
	Jobs         int      `json:"jobs"`
	Score        int      `json:"score"`
	Tier         string   `json:"tier"`
	Priority     int      `json:"priority,omitempty"`
	PriorityTier string   `json:"priority_tier,omitempty"`
	Actions      []string `json:"actions,omitempty"`
}

func newComplexityCmd(flagProjectID *string, flagBranch *string, flagFormat *string) *cobra.Command {
	var flagDomain string
	var flagTop int
	var flagMinPriority int
	var flagRecommendations bool

	cmd := &cobra.Command{
		Use:   "complexity",
		Short: "Analyze structural complexity and refactoring priority",
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
			endpoints := listAll("APIEndpoint")
			methods := listAll("Method")
			sqlqueries := listAll("SQLQuery")
			jobs := listAll("Job")
			testedByRels := listAllRels("tested_by")

			reports := make(map[string]*complexityReport)
			getReport := func(domain string) *complexityReport {
				domain = strings.ToLower(domain)
				if r, ok := reports[domain]; ok {
					return r
				}
				r := &complexityReport{Domain: domain}
				reports[domain] = r
				return r
			}

			for _, ep := range endpoints {
				d := getDomain(ep)
				if flagDomain != "" && !strings.EqualFold(d, flagDomain) {
					continue
				}
				getReport(d).Endpoints++
			}
			for _, m := range methods {
				d := getDomain(m)
				if flagDomain != "" && !strings.EqualFold(d, flagDomain) {
					continue
				}
				getReport(d).Methods++
			}
			for _, sq := range sqlqueries {
				d := getDomain(sq)
				if flagDomain != "" && !strings.EqualFold(d, flagDomain) {
					continue
				}
				getReport(d).SQLQueries++
			}
			for _, j := range jobs {
				d := getDomain(j)
				if flagDomain != "" && !strings.EqualFold(d, flagDomain) {
					continue
				}
				getReport(d).Jobs++
			}

			hasTests := make(map[string]bool)
			for _, r := range testedByRels {
				for _, s := range services {
					if s.EntityID == r.SrcID {
						hasTests[getDomain(s)] = true
					}
				}
			}

			var finalReports []*complexityReport
			for _, r := range reports {
				r.Score = r.Endpoints*3 + r.Methods*2 + r.SQLQueries + r.Jobs*2
				switch {
				case r.Score >= 100:
					r.Tier = "critical"
				case r.Score >= 50:
					r.Tier = "high"
				case r.Score >= 20:
					r.Tier = "medium"
				default:
					r.Tier = "low"
				}

				if flagRecommendations {
					penalty := 1.0
					if hasTests[r.Domain] {
						penalty = 0.4
					}
					r.Priority = int(float64(r.Score) * penalty)
					switch {
					case r.Priority >= 80:
						r.PriorityTier = "P0"
					case r.Priority >= 40:
						r.PriorityTier = "P1"
					default:
						r.PriorityTier = "P2"
					}

					if !hasTests[r.Domain] && r.Score > 20 {
						r.Actions = append(r.Actions, "Add tests")
					}
					if r.Endpoints > 15 {
						r.Actions = append(r.Actions, "Split domain")
					}
				}

				if r.Priority >= flagMinPriority {
					finalReports = append(finalReports, r)
				}
			}

			sort.Slice(finalReports, func(i, j int) bool {
				return finalReports[i].Score > finalReports[j].Score
			})

			if flagTop > 0 && flagTop < len(finalReports) {
				finalReports = finalReports[:flagTop]
			}

			if *flagFormat == "json" {
				return output.JSON(finalReports)
			}

			headers := []string{"DOMAIN", "ENDPOINTS", "METHODS", "SQL", "JOBS", "SCORE", "TIER"}
			if flagRecommendations {
				headers = append(headers, "PRIORITY", "P-TIER", "ACTIONS")
			}

			var rows [][]string
			for _, r := range finalReports {
				row := []string{
					r.Domain,
					fmt.Sprintf("%d", r.Endpoints),
					fmt.Sprintf("%d", r.Methods),
					fmt.Sprintf("%d", r.SQLQueries),
					fmt.Sprintf("%d", r.Jobs),
					fmt.Sprintf("%d", r.Score),
					r.Tier,
				}
				if flagRecommendations {
					row = append(row, fmt.Sprintf("%d", r.Priority), r.PriorityTier, strings.Join(r.Actions, ", "))
				}
				rows = append(rows, row)
			}
			output.Table(headers, rows)
			return nil
		},
	}

	cmd.Flags().StringVar(&flagDomain, "domain", "", "Filter to a specific domain")
	cmd.Flags().IntVar(&flagTop, "top", 0, "Show only top N domains")
	cmd.Flags().IntVar(&flagMinPriority, "min-priority", 0, "Minimum priority score")
	cmd.Flags().BoolVar(&flagRecommendations, "recommendations", false, "Add recommendations")
	return cmd
}
