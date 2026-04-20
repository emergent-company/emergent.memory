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

type logicFinding struct {
	Check  string `json:"check"`
	Domain string `json:"domain"`
	Object string `json:"object"`
	Detail string `json:"detail"`
	Tier   int    `json:"tier"`
}

func newLogicCmd(flagProjectID *string, flagBranch *string, flagFormat *string) *cobra.Command {
	var flagChecks string
	var flagDomain string
	var flagVerbose bool

	cmd := &cobra.Command{
		Use:   "logic",
		Short: "Audit graph for logical consistency and design gaps",
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := config.New(*flagProjectID, *flagBranch)
			if err != nil {
				return err
			}

			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
			defer cancel()

			enabled := make(map[string]bool)
			if flagChecks != "" {
				for _, chk := range strings.Split(flagChecks, ",") {
					enabled[strings.TrimSpace(strings.ToUpper(chk))] = true
				}
			}
			checkEnabled := func(name string) bool {
				if len(enabled) == 0 {
					return true
				}
				return enabled[name]
			}

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

			domains := listAll("Domain")
			endpoints := listAll("APIEndpoint")
			belongsToRels := listAllRels("belongs_to")

			// Build indexes
			epByDomain := make(map[string][]string)
			for _, ep := range endpoints {
				d := strings.ToLower(strProp(ep, "domain"))
				epByDomain[d] = append(epByDomain[d], strProp(ep, "path"))
			}

			svcToDomains := make(map[string]map[string]bool)
			domainToSvcs := make(map[string]map[string]bool)
			domainIDSet := make(map[string]bool)
			for _, d := range domains {
				domainIDSet[d.EntityID] = true
			}
			for _, r := range belongsToRels {
				if domainIDSet[r.DstID] {
					if svcToDomains[r.SrcID] == nil {
						svcToDomains[r.SrcID] = make(map[string]bool)
					}
					svcToDomains[r.SrcID][r.DstID] = true
					if domainToSvcs[r.DstID] == nil {
						domainToSvcs[r.DstID] = make(map[string]bool)
					}
					domainToSvcs[r.DstID][r.SrcID] = true
				}
			}

			var findings []logicFinding
			add := func(check, domain, object, detail string, tier int) {
				if flagDomain != "" && !strings.EqualFold(domain, flagDomain) {
					return
				}
				findings = append(findings, logicFinding{Check: check, Domain: domain, Object: object, Detail: detail, Tier: tier})
			}

			if checkEnabled("DOMAIN_NO_ENDPOINTS") {
				for _, d := range domains {
					name := strings.ToLower(strProp(d, "name"))
					if len(epByDomain[name]) == 0 {
						add("DOMAIN_NO_ENDPOINTS", name, name, "domain has no APIEndpoints", 2)
					}
				}
			}

			if checkEnabled("DOMAIN_NO_SERVICE") {
				for _, d := range domains {
					name := strings.ToLower(strProp(d, "name"))
					if len(domainToSvcs[d.EntityID]) == 0 {
						add("DOMAIN_NO_SERVICE", name, strProp(d, "name"), "domain has no Service linked via belongs_to", 2)
					}
				}
			}

			sort.Slice(findings, func(i, j int) bool {
				if findings[i].Tier != findings[j].Tier {
					return findings[i].Tier < findings[j].Tier
				}
				return findings[i].Check < findings[j].Check
			})

			if *flagFormat == "json" {
				return output.JSON(findings)
			}

			headers := []string{"TIER", "CHECK", "DOMAIN", "OBJECT", "DETAIL"}
			var rows [][]string
			for _, f := range findings {
				rows = append(rows, []string{fmt.Sprintf("T%d", f.Tier), f.Check, f.Domain, f.Object, f.Detail})
			}
			output.Table(headers, rows)
			return nil
		},
	}

	cmd.Flags().StringVar(&flagChecks, "checks", "", "Comma-separated checks to run")
	cmd.Flags().StringVar(&flagDomain, "domain", "", "Filter to a specific domain")
	cmd.Flags().BoolVar(&flagVerbose, "verbose", false, "Include passing checks in output")
	return cmd
}
