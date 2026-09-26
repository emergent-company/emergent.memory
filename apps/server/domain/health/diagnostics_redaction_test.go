package health

import (
	"strings"
	"testing"
)

// TestLongRunningQueriesSQLRedactsQueryText pins the secret-redaction promise of
// the /api/diagnostics long-running-query report: the SQL must expose only the
// query length, never the raw query body (which can embed secrets, user data,
// or connection targets). It guards against a future edit reintroducing the
// raw query text into the diagnostic response.
func TestLongRunningQueriesSQLRedactsQueryText(t *testing.T) {
	if strings.Contains(longRunningQueriesSQL, "left(query") {
		t.Fatalf("longRunningQueriesSQL still selects truncated raw query text: %q", longRunningQueriesSQL)
	}
	if !strings.Contains(longRunningQueriesSQL, "length(query)") {
		t.Fatalf("longRunningQueriesSQL no longer reports query length: %q", longRunningQueriesSQL)
	}
	if strings.Contains(longRunningQueriesSQL, "'query'") {
		t.Fatalf("longRunningQueriesSQL selects a raw 'query' field: %q", longRunningQueriesSQL)
	}
}
