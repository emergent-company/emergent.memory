package blueprints_test

import (
	"os"
	"testing"

	framework "github.com/emergent-company/emergent.memory/e2e/framework"
)

// TestMain loads .env before running any tests.
func TestMain(m *testing.M) {
	framework.LoadDotEnv()
	os.Exit(m.Run())
}
