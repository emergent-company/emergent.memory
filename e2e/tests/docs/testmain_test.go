package docs_test

import (
	"os"
	"testing"

	framework "github.com/emergent-company/emergent.memory/e2e/framework"
)

// TestMain loads .env before running any tests so developers can set
// MEMORY_TEST_SERVER, MEMORY_TEST_TOKEN, etc. without exporting them.
func TestMain(m *testing.M) {
	framework.LoadDotEnv()
	os.Exit(m.Run())
}
