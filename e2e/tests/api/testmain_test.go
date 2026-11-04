// Package api_test — HTTP API end-to-end tests for the Memory server.
//
// These tests run against an external server (MEMORY_TEST_SERVER) and cover
// all HTTP API domains migrated from emergent.memory/apps/server/tests/e2e/.
//
// Required environment variables:
//
//	MEMORY_TEST_SERVER — URL of the Memory server (e.g. http://localhost:3012)
//	MEMORY_TEST_TOKEN  — API key for the Memory server
package api_test

import (
	"os"
	"testing"

	framework "github.com/emergent-company/emergent.memory/e2e/framework"
)

func TestMain(m *testing.M) {
	framework.LoadDotEnv()
	os.Exit(m.Run())
}
