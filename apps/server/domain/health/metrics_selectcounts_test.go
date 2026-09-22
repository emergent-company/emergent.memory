package health

import (
	"strings"
	"testing"

	"github.com/emergent-company/emergent.memory/internal/jobs"
)

// TestSelectCounts_SeparatesStaleSweepFailures guards the reporting split: the
// `failed` aggregate must exclude stale-sweep terminal rows and a separate
// `stale_failed` aggregate must report them, using the column passed in.
func TestSelectCounts_SeparatesStaleSweepFailures(t *testing.T) {
	for _, col := range []string{"last_error", "error_message", "cej.last_error", "gej.last_error"} {
		q := selectCounts(col)
		assert := func(cond bool, msg string) {
			t.Helper()
			if !cond {
				t.Errorf("selectCounts(%q): %s\nquery:\n%s", col, msg, q)
			}
		}
		assert(strings.Contains(q, "as stale_failed"), "missing stale_failed aggregate")
		assert(strings.Contains(q, "status = 'failed' AND COALESCE("+col+", '') <> '"+jobs.StaleJobMessage+"'"), "failed aggregate must exclude the stale marker using the given column")
		assert(strings.Contains(q, "status = 'failed' AND "+col+" = '"+jobs.StaleJobMessage+"'"), "stale_failed aggregate must match the stale marker using the given column")
		// processing still covers 'running' for tables that use it.
		assert(strings.Contains(q, "status IN ('processing', 'running')"), "processing aggregate lost the running status")
	}
}
