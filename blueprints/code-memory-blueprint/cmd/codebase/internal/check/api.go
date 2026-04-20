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

type apiFinding struct {
	Check   string `json:"check"`
	Domain  string `json:"domain"`
	Method  string `json:"method"`
	Path    string `json:"path"`
	Handler string `json:"handler"`
	Key     string `json:"key"`
	Detail  string `json:"detail"`
}

func newAPICmd(flagProjectID *string, flagBranch *string, flagFormat *string) *cobra.Command {
	var flagDomain string
	var flagChecks string

	cmd := &cobra.Command{
		Use:   "api",
		Short: "Audit APIEndpoint objects for missing properties or orphans",
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := config.New(*flagProjectID, *flagBranch)
			if err != nil {
				return err
			}

			enabled := make(map[string]bool)
			for _, chk := range strings.Split(flagChecks, ",") {
				enabled[strings.TrimSpace(strings.ToLower(chk))] = true
			}

			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
			defer cancel()

			epsResp, err := c.Graph.ListObjects(ctx, &graph.ListObjectsOptions{Type: "APIEndpoint"})
			if err != nil {
				return err
			}
			eps := epsResp.Items

			relsResp, err := c.Graph.ListRelationships(ctx, &graph.ListRelationshipsOptions{Type: "handles"})
			if err != nil {
				return err
			}
			rels := relsResp.Items

			hasHandles := make(map[string]bool)
			for _, r := range rels {
				hasHandles[r.DstID] = true
			}

			var findings []apiFinding
			byMethodPath := make(map[string][]*graph.GraphObject)

			for _, ep := range eps {
				domain := strProp(ep, "domain")
				if flagDomain != "" && !strings.EqualFold(domain, flagDomain) {
					continue
				}

				method := strings.ToUpper(strProp(ep, "method"))
				path := strProp(ep, "path")
				handler := strProp(ep, "handler")
				file := strProp(ep, "file")
				key := ""
				if ep.Key != nil {
					key = *ep.Key
				}

				f := func(check, detail string) {
					findings = append(findings, apiFinding{
						Check:   check,
						Domain:  domain,
						Method:  method,
						Path:    path,
						Handler: handler,
						Key:     key,
						Detail:  detail,
					})
				}

				if enabled["no_path"] && path == "" {
					f("NO_PATH", "missing path property")
				}
				if enabled["no_method"] && method == "" {
					f("NO_METHOD", "missing method property")
				}
				if enabled["no_handler"] && handler == "" {
					f("NO_HANDLER", "missing handler property")
				}
				if enabled["no_file"] && file == "" {
					f("NO_FILE", "missing file property")
				}
				if enabled["no_domain"] && domain == "" {
					f("NO_DOMAIN", "missing domain property")
				}
				if enabled["orphan"] && !hasHandles[ep.EntityID] {
					f("ORPHAN", "no `handles` relationship to a Service")
				}
				if enabled["duplicate"] && method != "" && path != "" {
					k := method + ":" + path
					byMethodPath[k] = append(byMethodPath[k], ep)
				}
			}

			if enabled["duplicate"] {
				for k, dupEps := range byMethodPath {
					if len(dupEps) <= 1 {
						continue
					}
					parts := strings.SplitN(k, ":", 2)
					method, path := parts[0], parts[1]
					var keys []string
					for _, ep := range dupEps {
						if ep.Key != nil {
							keys = append(keys, *ep.Key)
						}
					}
					detail := fmt.Sprintf("%d endpoints share %s %s: %s", len(dupEps), method, path, strings.Join(keys, ", "))
					for _, ep := range dupEps {
						findings = append(findings, apiFinding{
							Check:   "DUPLICATE",
							Domain:  strProp(ep, "domain"),
							Method:  method,
							Path:    path,
							Handler: strProp(ep, "handler"),
							Key:     deref(ep.Key),
							Detail:  detail,
						})
					}
				}
			}

			sort.Slice(findings, func(i, j int) bool {
				if findings[i].Check != findings[j].Check {
					return findings[i].Check < findings[j].Check
				}
				if findings[i].Domain != findings[j].Domain {
					return findings[i].Domain < findings[j].Domain
				}
				return findings[i].Path < findings[j].Path
			})

			if *flagFormat == "json" {
				return output.JSON(findings)
			}

			headers := []string{"CHECK", "DOMAIN", "METHOD", "PATH", "KEY", "DETAIL"}
			var rows [][]string
			for _, f := range findings {
				rows = append(rows, []string{f.Check, f.Domain, f.Method, f.Path, f.Key, f.Detail})
			}
			output.Table(headers, rows)
			return nil
		},
	}

	cmd.Flags().StringVar(&flagDomain, "domain", "", "Filter to a specific domain")
	cmd.Flags().StringVar(&flagChecks, "checks", "no_path,no_method,no_handler,no_file,no_domain,orphan,duplicate", "Comma-separated checks to run")
	return cmd
}

func strProp(o *graph.GraphObject, key string) string {
	if v, ok := o.Properties[key]; ok {
		if s, ok := v.(string); ok {
			return s
		}
	}
	return ""
}

func deref(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}
